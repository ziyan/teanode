package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// fakeRelay is the relay as a test sees it: what was sent, to which
// registration, and what it answers.
type fakeRelay struct {
	mutex   sync.Mutex
	sent    map[string][]*push.Delivery
	answers error
}

func (self *fakeRelay) Send(_ context.Context, pushRegistration string, delivery *push.Delivery) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sent == nil {
		self.sent = map[string][]*push.Delivery{}
	}
	self.sent[pushRegistration] = append(self.sent[pushRegistration], delivery)
	return self.answers
}

func (self *fakeRelay) to(pushRegistration string) []*push.Delivery {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.sent[pushRegistration]
}

// phoneFixture is a person with an agent and one phone, and a worker that
// sends through a fake relay.
type phoneFixture struct {
	database db.Database
	worker   *Agent
	relay    *fakeRelay
	owner    *models.User
	agent    *models.Agent
	phone    *models.AgentPhone
	key      []byte
}

func newPhoneFixture(t *testing.T) *phoneFixture {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Server.Secret = "a secret for sealing in this test"
	worker := New(&Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	fixture := &phoneFixture{database: database, worker: worker, relay: &fakeRelay{}, key: make([]byte, push.PhoneKeyBytes)}
	worker.phones.override = fixture.relay
	_, _ = rand.Read(fixture.key)
	sealedKey, err := worker.SealSecret(base64.StdEncoding.EncodeToString(fixture.key))
	if err != nil {
		t.Fatalf("SealSecret: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if fixture.owner, err = tx.CreateUser(&models.User{Username: "robin"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if fixture.agent, err = tx.CreateAgent(&models.Agent{UserID: fixture.owner.ID, Enabled: true, Name: "Tea"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if fixture.phone, err = tx.PutAgentPhone(&models.AgentPhone{
			AgentID: fixture.agent.ID, ClientID: "a-phone-client", PhoneName: "A test phone",
			PushRegistration: "sealed:a-registration", PayloadKey: sealedKey,
		}); err != nil {
			t.Fatalf("PutAgentPhone: %s", err)
		}
	})
	// The phone is signed in: its client holds a token.
	if _, err := database.CreateAuthorizedToken(&models.Token{
		UserID: fixture.owner.ID, Name: "A test phone", ClientID: "a-phone-client", ExpiresAt: time.Now().Add(time.Hour),
	}, "a-key-hash", "a-refresh-hash"); err != nil {
		t.Fatalf("CreateAuthorizedToken: %s", err)
	}
	return fixture
}

// read decrypts what a notification says, as the phone would.
func (self *phoneFixture) read(t *testing.T, delivery *push.Delivery) *push.Content {
	t.Helper()
	content, err := push.DecryptContent(self.key, delivery.Sealed)
	if err != nil {
		t.Fatalf("the phone cannot read its notification: %s", err)
	}
	return content
}

// What the agent says on its own after a phone connects reaches the phone,
// once, however many times the loop looks; what it said before does not.
func TestThePhoneIsToldWhatTheAgentSaysOnItsOwnOnce(t *testing.T) {
	fixture := newPhoneFixture(t)
	if err := fixture.worker.writeHerdrQuestion(fixture.agent.ID, "[herdr question] a pane asks", "Claude Code asks whether to deploy now.", "a pane asks"); err != nil {
		t.Fatalf("writeHerdrQuestion: %s", err)
	}
	later := time.Now().Add(time.Minute)
	fixture.worker.pushOwnTurns(context.Background(), later)
	fixture.worker.pushOwnTurns(context.Background(), later)

	sent := fixture.relay.to("sealed:a-registration")
	if len(sent) != 1 {
		t.Fatalf("the phone was sent %d notifications, not one", len(sent))
	}
	content := fixture.read(t, sent[0])
	if content.NotificationKind != models.NotificationMessage || content.Title != "Tea" || content.Body != "Claude Code asks whether to deploy now." {
		t.Errorf("the phone was told %+v", content)
	}
}

// A phone that chose not to be told something is not told it.
func TestAPhoneIsToldOnlyWhatItChose(t *testing.T) {
	fixture := newPhoneFixture(t)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.UpdateAgentPhoneSettings(fixture.agent.ID, fixture.phone.ID, "A test phone", []string{models.NotificationApproval}); err != nil {
			t.Fatalf("UpdateAgentPhoneSettings: %s", err)
		}
	})
	if err := fixture.worker.writeHerdrQuestion(fixture.agent.ID, "[herdr question] a pane asks", "A question nobody asked to hear about.", "a pane asks"); err != nil {
		t.Fatalf("writeHerdrQuestion: %s", err)
	}
	fixture.worker.pushOwnTurns(context.Background(), time.Now().Add(time.Minute))
	if sent := fixture.relay.to("sealed:a-registration"); len(sent) != 0 {
		t.Errorf("a phone that wants approvals only was sent %d messages", len(sent))
	}
}

// Apple saying the phone is gone clears its registration, so nothing more is
// sent until the app registers again.
func TestAPhoneAppleSaysIsGoneIsNotSentToAgain(t *testing.T) {
	fixture := newPhoneFixture(t)
	fixture.relay.answers = push.ErrGone
	phone := fixture.phone
	if err := fixture.worker.NotifyPhone(context.Background(), phone, &push.Content{NotificationKind: "test", Title: "Tea", Body: "Hello"}); err == nil {
		t.Fatal("a gone phone was reported reached")
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		stored, err := tx.GetAgentPhone(fixture.agent.ID, phone.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetAgentPhone: %v %s", stored, err)
		}
		if stored.CanBeNotified() || stored.LastDeliveryError == "" {
			t.Errorf("the gone phone is still registered (%v) or its failure unrecorded (%q)", stored.CanBeNotified(), stored.LastDeliveryError)
		}
	})
}

// A phone signed out some other way, its tokens revoked or every session
// ended, is sent nothing, though its row is still there.
func TestASignedOutPhoneIsNotSentTo(t *testing.T) {
	fixture := newPhoneFixture(t)
	if _, err := fixture.database.RevokeClientTokens(fixture.owner.ID, "a-phone-client", time.Now()); err != nil {
		t.Fatalf("RevokeClientTokens: %s", err)
	}
	if err := fixture.worker.writeHerdrQuestion(fixture.agent.ID, "[herdr question] a pane asks", "Said after the phone was signed out.", "a pane asks"); err != nil {
		t.Fatalf("writeHerdrQuestion: %s", err)
	}
	fixture.worker.pushOwnTurns(context.Background(), time.Now().Add(time.Minute))
	if sent := fixture.relay.to("sealed:a-registration"); len(sent) != 0 {
		t.Errorf("a signed-out phone was sent %d notifications", len(sent))
	}
}

// The app going to the background hides the app at once, whatever it said a
// moment before, while another tab still shown keeps the person present; and
// a dashboard left untouched on a desk does not keep the phone quiet.
func TestPresenceIsKeptPerTabAndPhone(t *testing.T) {
	worker := &Agent{}
	now := time.Now()
	worker.ReportPresence("u", "phone", true, 0, now)
	if !worker.isWatching("u", time.Time{}, now) {
		t.Fatal("the app in front was not taken as watching")
	}
	worker.ReportPresence("u", "phone", false, 0, now.Add(time.Second))
	if worker.isWatching("u", time.Time{}, now.Add(2*time.Second)) {
		t.Error("the app in the background was still taken as watching")
	}
	worker.ReportPresence("u", "tab", true, 0, now)
	worker.ReportPresence("u", "other-tab", false, 0, now.Add(time.Second))
	if !worker.isWatching("u", time.Time{}, now.Add(2*time.Second)) {
		t.Error("a hidden tab hid a shown one")
	}
	worker.ReportPresence("u", "tab", true, 300, now.Add(3*time.Second))
	if worker.isWatching("u", time.Time{}, now.Add(4*time.Second)) {
		t.Error("a tab untouched for five minutes kept the phone quiet")
	}
}

func TestThePhoneIsToldOfAnswersNobodyIsThereToRead(t *testing.T) {
	main := &models.AgentConversation{ID: "main-1", Kind: models.AgentConversationMain}
	side := &models.AgentConversation{ID: "side-1", Kind: models.AgentConversationNamed}
	run := func(surface string, conversation *models.AgentConversation, message string) *AskRun {
		return &AskRun{settings: &AskSettings{Surface: surface, Conversation: conversation, Message: message}}
	}
	cases := []struct {
		name   string
		run    *AskRun
		isTold bool
	}{
		{"asked from the app", run("ios", side, "What is on today?"), true},
		{"asked in the main chat on the dashboard", run("drawer", main, "Draft the reply."), true},
		{"asked in the main chat from the command line", run("cli", main, "Summarize the week."), false},
		{"asked in the main chat over the API", run("api", main, "Summarize the week."), false},
		{"asked in a side chat on the dashboard", run("drawer", side, "Draft the reply."), false},
		{"asked on a call", run("voice", main, "What is on today?"), false},
		{"asked from a linked chat", run("telegram", main, "What is on today?"), false},
		{"the agent's own turn", run("drawer", main, models.ScheduleMarker+" the morning brief"), false},
	}
	for _, testCase := range cases {
		if isTold := testCase.run.isAnswerForPhone(); isTold != testCase.isTold {
			t.Errorf("%s: told %v, want %v", testCase.name, isTold, testCase.isTold)
		}
	}
	subagent := run("drawer", main, "Look into it.")
	subagent.settings.subagentDepth = 1
	if subagent.isAnswerForPhone() {
		t.Error("a subagent's answer was told to the phone")
	}
}

// Typing a question sends no presence report, so a turn the person asked a
// moment ago counts as their latest input: a report from before they typed
// that already says they were idle does not send the answer to the phone.
func TestAskingInTheDrawerCountsAsBeingThere(t *testing.T) {
	worker := &Agent{}
	now := time.Now()
	worker.ReportPresence("u", "tab", true, 110, now.Add(-50*time.Second))
	asked := &AskRun{agent: worker, lastWroteAt: now.Add(-10 * time.Second), settings: &AskSettings{
		Agent:   &models.Agent{UserID: "u"},
		Surface: "drawer",
		Message: "Draft the reply.",
	}}
	if worker.isWatching("u", time.Time{}, now) {
		t.Fatal("the report alone was taken as watching; the test proves nothing")
	}
	if !asked.isWatched(now) {
		t.Error("somebody who asked in the drawer ten seconds ago was judged away")
	}
	if asked.isWatched(now.Add(3 * time.Minute)) {
		t.Error("a question asked minutes ago kept a tab that stopped reporting as watched")
	}
	worker.ReportPresence("u", "tab", false, 0, now)
	if asked.isWatched(now.Add(time.Second)) {
		t.Error("a question just asked kept a hidden tab as watched")
	}

	worker.ReportPresence("u", "tab", true, 110, now.Add(-50*time.Second))
	ownTurn := &AskRun{agent: worker, lastWroteAt: now.Add(-10 * time.Second), settings: &AskSettings{
		Agent:   &models.Agent{UserID: "u"},
		Surface: "drawer",
		Message: models.ScheduleMarker + " the morning brief",
	}}
	if ownTurn.isWatched(now) {
		t.Error("the agent's own turn was taken as the person typing")
	}
}

// Each tab names itself afresh when it loads, so a person reloading over and
// over keeps at most presenceReportersAtMost reporters, losing the oldest.
func TestPresenceKeepsABoundedNumberOfReporters(t *testing.T) {
	worker := &Agent{}
	now := time.Now()
	for index := range presenceReportersAtMost + 10 {
		worker.ReportPresence("u", fmt.Sprintf("tab-%d", index), index == 0, 0, now.Add(time.Duration(index)*time.Millisecond))
	}
	reporters := worker.presence.reports["u"]
	if len(reporters) != presenceReportersAtMost {
		t.Fatalf("kept %d reporters, want %d", len(reporters), presenceReportersAtMost)
	}
	if _, isKept := reporters["tab-0"]; isKept {
		t.Error("the oldest reporter was kept")
	}
	if _, isKept := reporters[fmt.Sprintf("tab-%d", presenceReportersAtMost+9)]; !isKept {
		t.Error("the newest reporter was dropped")
	}
	worker.ReportPresence("u", "tab-20", true, 0, now.Add(time.Second))
	if len(worker.presence.reports["u"]) != presenceReportersAtMost {
		t.Error("a known reporter reporting again pushed another out")
	}
}
