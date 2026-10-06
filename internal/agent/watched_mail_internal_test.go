package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// watchedSkillForTest is a mail skill with the two read tools the watch
// runs, written the way the published one is.
const watchedSkillForTest = `---
name: gmail
description: "Mail through a command on the person's own computer"
tools:
  - name: gmail_search
    description: Search the person's mail.
    type: workflow
    actionField: action
    parameters:
      type: object
      properties:
        action: {type: string, enum: ["search"]}
        query: {type: string}
      required: ["action", "query"]
    actions:
      search:
        - name: search
          type: shell
          command: [gog, gmail, search, "{{query}}", --max, "25", --json, --no-input]
          timeout: 60
  - name: gmail_read
    description: Read a thread or a message.
    type: workflow
    actionField: action
    parameters:
      type: object
      properties:
        action: {type: string, enum: ["thread", "message"]}
        id: {type: string}
      required: ["action", "id"]
    actions:
      thread:
        - name: thread
          type: shell
          command: [gog, gmail, thread, get, "{{id}}", --full, --json, --no-input]
          timeout: 60
      message:
        - name: message
          type: shell
          command: [gog, gmail, get, "{{id}}", --json, --no-input]
          timeout: 60
---
`

func TestWatchSinceStartsBeforeTheNewestAndNeverTooFarBack(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	if got := watchSince(nil, now); !got.Equal(now.Add(-watchFirstLookBack)) {
		t.Fatalf("the first look reaches back a little: %s", got)
	}
	latest := now.Add(-time.Hour)
	if got := watchSince(&latest, now); !got.Equal(latest.Add(-watchOverlap)) {
		t.Fatalf("a later look overlaps the last: %s", got)
	}
	longAgo := now.Add(-10 * 24 * time.Hour)
	if got := watchSince(&longAgo, now); !got.Equal(now.Add(-alertFreshness)) {
		t.Fatalf("never further back than an alert is news: %s", got)
	}
	query := gmailWatchQuery(now)
	if !strings.HasPrefix(query, fmt.Sprintf("after:%d ", now.Unix())) || strings.Contains(query, "in:inbox") || !strings.Contains(query, "-in:sent") {
		t.Fatalf("archived mail is searched, sent mail is not: %q", query)
	}
}

func TestParseTheSkillsAnswers(t *testing.T) {
	threads, err := parseGmailSearch(map[string]any{"text": `{"nextPageToken":"x","threads":[{"id":"t1","subject":"Hello","labels":["UNREAD"],"messageCount":1}]}`})
	if err != nil || len(threads) != 1 || threads[0].ID != "t1" || threads[0].MessageCount != 1 {
		t.Fatalf("the threads: %+v %v", threads, err)
	}
	if _, err := parseGmailSearch(map[string]any{"text": "[ended 1 on laptop]\nnot signed in"}); err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("a complaint is said as the error: %v", err)
	}
	messages, err := parseGmailThread(map[string]any{"text": `{"thread":{"id":"t2","messages":[{"id":"m1","internalDate":"1772625600000","labelIds":["SENT"]},{"id":"m2","internalDate":"1772629200000","labelIds":["INBOX"]}]}}`})
	if err != nil || len(messages) != 2 || !messages[0].isWrittenByThePerson() || messages[1].isWrittenByThePerson() || !messages[1].at().Equal(time.UnixMilli(1772629200000)) {
		t.Fatalf("the thread's messages: %+v %v", messages, err)
	}
	message, err := parseGmailMessage(map[string]any{"text": `{"body":"<html><body><p>The school closes at noon.</p><script>x()</script></body></html>","headers":{"from":"Office <office@school.example.org>","subject":"Early closing","date":"Wed, 4 Mar 2026 12:00:00 +0000","to":"robin@example.com"},"message":{"id":"m2","internalDate":"1772629200000","labelIds":["UNREAD","CATEGORY_UPDATES"]}}`})
	if err != nil {
		t.Fatalf("parseGmailMessage: %s", err)
	}
	read := watchedMessageContext(message, 1000)
	if read.From != "Office <office@school.example.org>" || read.Subject != "Early closing" || strings.Contains(read.Text, "<p>") || strings.Contains(read.Text, "x()") || !strings.Contains(read.Text, "closes at noon") {
		t.Fatalf("the message as the sorting reads it: %+v", read)
	}
	if facts := strings.Join(read.Facts, "; "); !strings.Contains(facts, "not in the inbox") || !strings.Contains(facts, "CATEGORY_UPDATES") {
		t.Fatalf("an archived message says so: %q", facts)
	}
	if _, err := parseGmailMessage(map[string]any{"text": `{"body":""}`}); err == nil {
		t.Fatal("an answer with no message in it is refused")
	}
}

// watchedMailbox is the person's mail as the fake computer serves it: two
// threads, one of a single message and one where the person wrote first
// and somebody answered.
type watchedMailbox struct {
	mutex    sync.Mutex
	commands []string
	now      time.Time
}

func (self *watchedMailbox) answer(action string, args json.RawMessage) (bool, string) {
	if action != "shell" {
		return false, "only commands"
	}
	var call struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &call)
	// Each word arrives quoted; the words are what is matched.
	call.Command = strings.ReplaceAll(call.Command, "'", "")
	self.mutex.Lock()
	self.commands = append(self.commands, call.Command)
	self.mutex.Unlock()
	milliseconds := func(ago time.Duration) string { return fmt.Sprint(self.now.Add(-ago).UnixMilli()) }
	var printed string
	switch {
	case strings.Contains(call.Command, "gmail search"):
		printed = `{"threads":[{"id":"t1","subject":"Early closing","labels":["UNREAD"],"messageCount":1},{"id":"t2","subject":"Re: Saturday","labels":["INBOX"],"messageCount":2}]}`
	case strings.Contains(call.Command, "thread get"):
		printed = `{"thread":{"id":"t2","messages":[{"id":"t2","internalDate":"` + milliseconds(30*time.Minute) + `","labelIds":["SENT"]},{"id":"m3","internalDate":"` + milliseconds(5*time.Minute) + `","labelIds":["INBOX"]}]}}`
	case strings.Contains(call.Command, "gmail get") && strings.Contains(call.Command, "t1"):
		printed = `{"body":"The school closes at noon today because of the storm. Please collect your child by 12:15.","headers":{"from":"Office <office@school.example.org>","subject":"Early closing","date":"today"},"message":{"id":"t1","internalDate":"` + milliseconds(10*time.Minute) + `","labelIds":["UNREAD","CATEGORY_UPDATES"]}}`
	case strings.Contains(call.Command, "gmail get") && strings.Contains(call.Command, "m3"):
		printed = `{"body":"Saturday works, see you then.","headers":{"from":"Sam <sam@friends.example.net>","subject":"Re: Saturday","date":"today"},"message":{"id":"m3","internalDate":"` + milliseconds(5*time.Minute) + `","labelIds":["INBOX"]}}`
	default:
		return false, "unexpected command: " + call.Command
	}
	encoded, _ := json.Marshal(map[string]any{"stdout": printed, "stderr": "", "exitCode": 0})
	return true, string(encoded)
}

func (self *watchedMailbox) commandCount(fragment string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := 0
	for _, command := range self.commands {
		if strings.Contains(command, fragment) {
			count++
		}
	}
	return count
}

// A look finds two new messages, one in a thread the person started, and
// sorts each once: the school's closing is a candidate that carries the
// message, the friend's answer is not, and the person's own message is
// never read. The alert job tells the closing. A second look over the
// same mail sorts nothing again.
func TestWatchSortsNewMailOnceAndTheAlertJobTellsIt(t *testing.T) {
	provider := &alertModel{answers: []string{
		`{"category":"personal","priority":"normal","needs_reply":false,"research":false,"extract":false,"summary":"Saturday is on","action_items":[],"alert_signal":"none","alert_reason":""}`,
		`{"category":"notification","priority":"high","needs_reply":false,"research":false,"extract":false,"summary":"School closes at noon","action_items":["Collect the child by 12:15"],"alert_signal":"now","alert_reason":"the school closes at noon today"}`,
		`{"alerts":[{"subject_key":"school early closing","is_urgent":true,"candidate_ids":["c1"],"alert_text":"The school is closing at noon today because of the storm; pick-up is by 12:15."}],"dropped":[]}`,
	}}
	server := provider.serve(t)
	fixture := newAlertFixtureWith(t, server.URL, zoneAtHour(t, 12))
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentSkill(&models.AgentSkill{Name: gmailSkillName, Enabled: true, Content: watchedSkillForTest}); err != nil {
			t.Fatalf("PutAgentSkill: %s", err)
		}
	})
	mailbox := &watchedMailbox{now: time.Now()}
	fixture.worker.AttachComputer(fixture.agent.ID, &fakeComputer{agent: fixture.worker, agentId: fixture.agent.ID, answers: mailbox.answer}, ComputerIdentity{Name: "laptop", System: "linux", Home: "/home/robin"})

	// The tick queues the look itself: the skill is on, alerts are on and
	// a computer is attached.
	if err := fixture.worker.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	fixture.worker.Wait()
	if mailbox.commandCount("gmail search") != 1 || mailbox.commandCount("gmail get") != 2 || mailbox.commandCount("thread get") != 1 || provider.callCount() != 2 {
		t.Fatalf("one search, one thread, two messages, two sortings: %q, %d calls", mailbox.commands, provider.callCount())
	}
	waiting := fixture.waiting(t)
	if len(waiting) != 1 {
		t.Fatalf("one candidate: %+v", waiting)
	}
	candidate := waiting[0]
	if candidate.CandidateKind != models.AlertCandidateWatched || candidate.AlertSignal != models.AlertSignalNow || candidate.WatchedMessageID != "t1" ||
		candidate.WatchedSkillName != gmailSkillName || candidate.WatchedSubject != "Early closing" || candidate.WatchedMailCategory != "notification" ||
		candidate.WatchedMessageAt == nil || !strings.Contains(candidate.WatchedMessageText, "closes at noon") || candidate.MailID != "" {
		t.Fatalf("the closing, with what the alert job reads: %+v", candidate)
	}

	// The look queued the alert job to gather for a moment first.
	if jobs := fixture.alertJobs(t); len(jobs) != 1 || jobs[0].Status != models.AgentJobQueued {
		t.Fatalf("the alert job is queued: %+v", jobs)
	}
	if err := fixture.worker.TickAt(t.Context(), time.Now().Add(alertGather+time.Minute)); err != nil {
		t.Fatalf("TickAt: %s", err)
	}
	fixture.worker.Wait()
	alerts := fixture.alerts(t)
	if len(alerts) != 1 || len(alerts[0].CandidateIDs) != 1 || alerts[0].CandidateIDs[0] != candidate.ID || !alerts[0].IsUrgent ||
		len(alerts[0].CoveredSenderAddresses) != 1 || alerts[0].CoveredSenderAddresses[0] != "office@school.example.org" {
		t.Fatalf("one alert, about the closing, from the school's address: %+v", alerts)
	}
	if !strings.Contains(provider.prompts[2], "read through the gmail skill") || !strings.Contains(provider.prompts[2], "collect your child") {
		t.Fatalf("the decision read the message from the candidate: %s", provider.prompts[2])
	}
	var covered []*AlertCovered
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if covered, err = coveredByAlert(tx, alerts[0]); err != nil {
			t.Fatalf("coveredByAlert: %s", err)
		}
	})
	if len(covered) != 1 || covered[0].Subject != "Early closing" || covered[0].FromAddress != "office@school.example.org" {
		t.Fatalf("the alert covers the closing: %+v", covered)
	}

	// The same mail again: searched, and nothing sorted twice.
	calls := provider.callCount()
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agent.ID, Kind: models.AgentJobWatch, SubjectID: gmailSkillName}); err != nil {
			t.Fatalf("EnqueueAgentJob: %s", err)
		}
	})
	if err := fixture.worker.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	fixture.worker.Wait()
	if mailbox.commandCount("gmail search") != 2 || mailbox.commandCount("gmail get") != 2 || provider.callCount() != calls {
		t.Fatalf("searched again, nothing read or sorted again: %q, %d calls", mailbox.commands, provider.callCount()-calls)
	}
}

// A sender the person muted is written down and dropped at once.
func TestWatchedCandidateFromAMutedSenderIsDropped(t *testing.T) {
	fixture := newAlertFixture(t)
	now := time.Now()
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.CreateAgentAlertMute(&models.AgentAlertMute{AgentID: fixture.agent.ID, MuteScope: models.AlertMuteDomain, MuteTarget: "school.example.org"}); err != nil {
			t.Fatalf("CreateAgentAlertMute: %s", err)
		}
		watched := &models.AgentWatchedMail{AgentID: fixture.agent.ID, SkillName: gmailSkillName, WatchedMessageID: "t9", WatchedMessageAt: now}
		if err := tx.AddAgentWatchedMail(watched); err != nil {
			t.Fatalf("AddAgentWatchedMail: %s", err)
		}
		messageContext := &MessageContext{From: "Office <office@school.example.org>", Subject: "Picture day", Text: "Bring a smile."}
		insight := &models.MailInsight{Category: "notification", AlertSignal: models.AlertSignalSoon, AlertReason: "picture day tomorrow"}
		if err := noteWatchedCandidate(tx, fixture.agent.ID, gmailSkillName, watched, messageContext, insight, now); err != nil {
			t.Fatalf("noteWatchedCandidate: %s", err)
		}
	})
	if waiting := fixture.waiting(t); len(waiting) != 0 {
		t.Fatalf("muted: %+v", waiting)
	}
	if jobs := fixture.alertJobs(t); len(jobs) != 0 {
		t.Fatalf("nothing to decide: %+v", jobs)
	}
}
