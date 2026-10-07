package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// watchingSkillForTest is a skill with two watches, written the way the
// published ones are: mail listed by thread and read in full, and charges
// listed with everything in the list.
const watchingSkillForTest = `---
name: papers
description: "Mail and charges through a command on the person's own computer"
tools:
  - name: papers_new_mail
    description: Threads with new mail since a moment.
    type: shell
    command: [papers, mail, --after, "{{since_epoch}}"]
    parameters: {type: object, properties: {since_epoch: {type: string}}, required: ["since_epoch"]}
  - name: papers_read_thread
    description: One thread as text.
    type: shell
    command: [papers, thread, "{{id}}"]
    parameters: {type: object, properties: {id: {type: string}}, required: ["id"]}
  - name: papers_new_charges
    description: Charges since a day.
    type: shell
    command: [papers, charges, --from, "{{since_date}}"]
    parameters: {type: object, properties: {since_date: {type: string}}, required: ["since_date"]}
watches:
  - name: new_mail
    description: mail that arrived, archived or not
    kind: mail
    list: {tool: papers_new_mail}
    read: {tool: papers_read_thread}
  - name: new_charges
    description: charges on the person's cards
    kind: item
    overlap: 72h
    guidance: A charge they would not expect is worth telling now; their usual shops are not.
    list: {tool: papers_new_charges}
---
`

// watchedWorld is the person's mail and charges as the fake computer
// serves them; after the first look a reply, a new thread and a charge
// arrive.
type watchedWorld struct {
	mutex    sync.Mutex
	commands []string
	isLater  bool
	now      time.Time
}

func (self *watchedWorld) answer(action string, args json.RawMessage) (bool, string) {
	var call struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &call)
	command := strings.ReplaceAll(call.Command, "'", "")
	self.mutex.Lock()
	self.commands = append(self.commands, command)
	isLater := self.isLater
	self.mutex.Unlock()
	at := func(ago time.Duration) string { return self.now.Add(-ago).UTC().Format(time.RFC3339) }
	var printed string
	switch {
	case strings.HasPrefix(command, "papers mail"):
		threads := []map[string]any{{"id": "t1", "version": "1", "at": at(3 * time.Hour), "from": "Sam <sam@friends.example.net>", "title": "Saturday"}}
		if isLater {
			threads = []map[string]any{
				{"id": "t1", "version": "2", "at": at(5 * time.Minute), "from": "Sam <sam@friends.example.net>", "title": "Re: Saturday"},
				{"id": "t2", "version": "1", "at": at(2 * time.Minute), "from": "Office <office@school.example.org>", "title": "Early closing", "text": "labels: UNREAD"},
			}
		}
		encoded, _ := json.Marshal(threads)
		printed = string(encoded)
	case command == "papers thread t1":
		printed = "From: Sam\n\nSaturday works, see you then."
	case command == "papers thread t2":
		printed = "From: Office\n\nThe school closes at noon today because of the storm. Please collect your child by 12:15."
	case strings.HasPrefix(command, "papers charges"):
		charges := `[{"id":"c1","at":"` + self.now.UTC().Format(time.DateOnly) + `","from":"Corner Grocer","title":"-42.10 USD","text":"a purchase at Corner Grocer"}]`
		if isLater {
			charges = `[{"id":"c1","at":"` + self.now.UTC().Format(time.DateOnly) + `","from":"Corner Grocer","title":"-42.10 USD","text":"a purchase at Corner Grocer"},{"id":"c2","at":"` + self.now.UTC().Format(time.DateOnly) + `","from":"Faraway Electronics","title":"-1899.00 USD","text":"a purchase at Faraway Electronics"}]`
		}
		printed = charges
	default:
		return false, "unexpected command: " + command
	}
	encoded, _ := json.Marshal(map[string]any{"stdout": printed, "stderr": "", "exitCode": 0})
	return true, string(encoded)
}

func (self *watchedWorld) count(prefix string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := 0
	for _, command := range self.commands {
		if strings.HasPrefix(command, prefix) {
			count++
		}
	}
	return count
}

// judgingModel answers each call by what its prompt is about, so that
// watches judged at once get the right answers whatever their order.
type judgingModel struct {
	mutex   sync.Mutex
	prompts []string
}

func (self *judgingModel) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		prompt := string(body)
		answer := `{"category":"personal","priority":"normal","needs_reply":false,"research":false,"extract":false,"summary":"nothing","action_items":[],"alert_signal":"none","alert_reason":""}`
		switch {
		case strings.Contains(prompt, "Decide which of the candidates"):
			answer = `{"alerts":[{"subject_key":"school early closing","is_urgent":true,"candidate_ids":["c1"],"alert_text":"The school is closing at noon today; pick-up is by 12:15."},{"subject_key":"unexpected electronics charge","is_urgent":false,"candidate_ids":["c2"],"alert_text":"A charge of 1,899 dollars at an electronics shop went through on your card."}],"dropped":[]}`
		case strings.Contains(prompt, "closes at noon"):
			answer = `{"category":"notification","priority":"high","needs_reply":false,"research":false,"extract":false,"summary":"School closes at noon","action_items":[],"alert_signal":"now","alert_reason":"the school closes at noon today"}`
		case strings.Contains(prompt, "Faraway Electronics"):
			answer = `{"alert_signal":"soon","alert_reason":"a large charge at a shop they do not usually use","summary":"1,899 dollars at an electronics shop"}`
		case strings.Contains(prompt, "Corner Grocer"):
			answer = `{"alert_signal":"none","alert_reason":"","summary":"groceries"}`
		}
		var parsed struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &parsed)
		self.mutex.Lock()
		if parsed.Stream {
			self.prompts = append(self.prompts, prompt)
		}
		self.mutex.Unlock()
		content, _ := json.Marshal(answer)
		if !parsed.Stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{}}`, content)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\ndata: [DONE]\n\n", content)
	}))
	t.Cleanup(server.Close)
	return server
}

func (self *judgingModel) callCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.prompts)
}

func (self *judgingModel) judged(fragment string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, prompt := range self.prompts {
		if strings.Contains(prompt, fragment) {
			return true
		}
	}
	return false
}

// The first look of each watch only takes note of what is there. The next
// finds a reply in a thread already seen (a new version), a new thread and
// a new charge: the reply and the groceries are judged and left, the
// closing and the strange charge become candidates, judged by the mail
// prompt and by the watch's own guidance, and the alert job tells both.
// A third look over the same items judges nothing again.
func TestSkillWatchesNoteFirstThenJudgeWhatIsNew(t *testing.T) {
	provider := &judgingModel{}
	server := provider.serve(t)
	fixture := newAlertFixtureWith(t, server.URL, zoneAtHour(t, 12))
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentSkill(&models.AgentSkill{Name: "papers", Enabled: true, Content: watchingSkillForTest}); err != nil {
			t.Fatalf("PutAgentSkill: %s", err)
		}
	})
	world := &watchedWorld{now: time.Now()}
	fixture.worker.AttachComputer(fixture.agent.ID, &fakeComputer{agent: fixture.worker, agentId: fixture.agent.ID, answers: world.answer}, ComputerIdentity{Name: "laptop", System: "linux", Home: "/home/robin"})
	look := func() {
		t.Helper()
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			for _, watchName := range []string{"new_mail", "new_charges"} {
				if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agent.ID, Kind: models.AgentJobWatch, SubjectID: watchSubject("papers", watchName)}); err != nil {
					t.Fatalf("EnqueueAgentJob: %s", err)
				}
			}
		})
		if err := fixture.worker.Tick(t.Context()); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		fixture.worker.Wait()
	}

	// The tick queues both watches itself on its first pass.
	if err := fixture.worker.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	fixture.worker.Wait()
	if world.count("papers mail") != 1 || world.count("papers charges") != 1 || world.count("papers thread") != 0 || provider.callCount() != 0 {
		t.Fatalf("a first look lists and judges nothing: %q, %d calls", world.commands, provider.callCount())
	}

	// A first look that found nothing still counts as one: a watch on an
	// empty source judges the first item that arrives.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		latest, err := tx.LatestAgentWatchLookedAt(fixture.agent.ID, "papers", "new_mail")
		if err != nil || latest == nil {
			t.Fatalf("the first look is noted: %v %v", latest, err)
		}
	})

	world.mutex.Lock()
	world.isLater = true
	world.mutex.Unlock()
	look()
	if world.count("papers thread t1") != 1 || world.count("papers thread t2") != 1 || provider.callCount() != 3 {
		t.Fatalf("the reply, the new thread and the new charge judged: %q, %d calls", world.commands, provider.callCount())
	}
	if provider.judged("Corner Grocer") || !provider.judged("Faraway Electronics") {
		t.Fatal("the charge already noted is not judged again, the new one is")
	}
	waiting := fixture.waiting(t)
	if len(waiting) != 2 {
		t.Fatalf("two candidates: %+v", waiting)
	}
	byItem := map[string]*models.AgentAlertCandidate{}
	for _, candidate := range waiting {
		byItem[candidate.WatchedItemID] = candidate
	}
	closing, charge := byItem["t2"], byItem["c2"]
	if closing == nil || closing.WatchedWatchName != "new_mail" || closing.WatchedCategory != "notification" || closing.AlertSignal != models.AlertSignalNow ||
		!strings.Contains(closing.WatchedItemText, "closes at noon") || !strings.Contains(closing.WatchedItemText, "labels: UNREAD") {
		t.Fatalf("the closing, judged as mail, with the list's line and the thread: %+v", closing)
	}
	if charge == nil || charge.WatchedWatchName != "new_charges" || charge.WatchedCategory != "new_charges" || charge.AlertSignal != models.AlertSignalSoon ||
		charge.WatchedSender != "Faraway Electronics" || !strings.Contains(charge.WatchedItemText, "a purchase at Faraway Electronics") {
		t.Fatalf("the charge, judged with the watch's guidance: %+v", charge)
	}
	if !provider.judged("their usual shops are not") {
		t.Fatal("the item's judgement carries the watch's guidance")
	}

	if err := fixture.worker.TickAt(t.Context(), time.Now().Add(alertGather+time.Minute)); err != nil {
		t.Fatalf("TickAt: %s", err)
	}
	fixture.worker.Wait()
	alerts := fixture.alerts(t)
	if len(alerts) != 2 {
		t.Fatalf("both are told: %+v", alerts)
	}
	var covered []*AlertCovered
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		for _, alert := range alerts {
			found, err := coveredByAlert(tx, alert)
			if err != nil {
				t.Fatalf("coveredByAlert: %s", err)
			}
			covered = append(covered, found...)
		}
	})
	if len(covered) != 2 {
		t.Fatalf("each alert covers its item: %+v", covered)
	}

	calls := provider.callCount()
	look()
	if provider.callCount() != calls || world.count("papers thread") != 2 {
		t.Fatalf("nothing judged or read again: %d calls, %q", provider.callCount()-calls, world.commands)
	}
}

func TestWatchedItemMomentsAreReadInTheUsualForms(t *testing.T) {
	for written, want := range map[string]time.Time{
		"2026-03-04T05:06:07Z": time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		"2026-03-04 05:06":     time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC),
		"2026-03-04":           time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
		"1772600767":           time.Unix(1772600767, 0),
		"1772600767000":        time.UnixMilli(1772600767000),
	} {
		if got := watchedItemAt(written); !got.Equal(want) {
			t.Errorf("%q is %s, want %s", written, got, want)
		}
	}
	if !watchedItemAt("yesterday").IsZero() {
		t.Error("what does not read is the zero time")
	}
	now := time.Now()
	if since := watchSince(nil, now, time.Hour); !since.Equal(now.Add(-time.Hour)) {
		t.Errorf("a first look starts an overlap back: %s", since)
	}
	latest := now.Add(-3 * time.Hour)
	if since := watchSince(&latest, now, 72*time.Hour); !since.Equal(latest.Add(-72 * time.Hour)) {
		t.Errorf("a later look starts an overlap before the newest: %s", since)
	}
	if subject := watchSubject("papers", "new_mail"); subject != "papers/new_mail" {
		t.Errorf("the subject: %s", subject)
	}
	_ = skills.WatchKindMail
}
