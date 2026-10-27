package agent

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A coding session's question is told to the person's phones, naming the
// session, and is written into no conversation: the main conversation stays
// as it was, and so do the chat apps that follow it. The computer is told it
// was heard each time it says it, and a question said again is told once.
func TestACodingQuestionIsToldToThePhoneAndNotWrittenIntoTheMainConversation(t *testing.T) {
	fixture := newPhoneFixture(t)
	var mutex sync.Mutex
	var acknowledged []string
	laptop := &fakeComputer{agent: fixture.worker, agentId: fixture.agent.ID, answers: func(action string, args json.RawMessage) (bool, string) {
		if action == "herdr_acknowledge" {
			var asked computer.HerdrArguments
			_ = json.Unmarshal(args, &asked)
			mutex.Lock()
			acknowledged = append(acknowledged, asked.HerdrEventIDs...)
			mutex.Unlock()
		}
		return true, `{}`
	}}
	fixture.worker.AttachComputer(fixture.agent.ID, laptop, ComputerIdentity{Name: "laptop", System: "linux", Home: "~"})

	say := func(eventId string) {
		event := computer.HerdrEvent{
			HerdrEventID: eventId, HerdrEventKind: computer.HerdrEventKindAsking,
			HerdrSession: &computer.HerdrSession{
				PaneID: "p3", PaneName: "notes", CodingAgentKind: computer.CodingAgentKindClaude,
				Question: &computer.HerdrQuestion{
					QuestionFingerprint: "fingerprint-1", HerdrQuestionKind: computer.HerdrQuestionKindToolApproval,
					QuestionText: "Run the tests in [the folder](file:///tmp/x)?",
					Options:      []computer.HerdrQuestionOption{{OptionNumber: 1, OptionLabel: "Yes"}, {OptionNumber: 2, OptionLabel: "No"}},
				},
			},
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Marshal: %s", err)
		}
		fixture.worker.ComputerHerdrChanged(fixture.agent.ID, laptop, encoded)
		fixture.worker.waitGroup.Wait()
	}
	say("event-1")
	say("event-2")

	sent := fixture.relay.to("sealed:a-registration")
	if len(sent) != 1 {
		t.Fatalf("the phone was sent %d notifications, not one", len(sent))
	}
	content := fixture.read(t, sent[0])
	if content.NotificationKind != herdrQuestionNotification || content.Computer != "laptop" || content.PaneID != "p3" ||
		content.QuestionFingerprint != "fingerprint-1" {
		t.Errorf("the notification does not name the session: %+v", content)
	}
	if content.Title != "notes on laptop asks for your approval" || content.Body != "Run the tests in the folder?" {
		t.Errorf("the notification said %q / %q", content.Title, content.Body)
	}
	if len(content.Buttons) != 1 || content.Buttons[0].ButtonAction != "open" {
		t.Errorf("the notification's buttons are %+v, not one that opens the session", content.Buttons)
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		main, err := scheduleConversation(tx, fixture.agent.ID, "")
		if err != nil {
			t.Fatalf("scheduleConversation: %s", err)
		}
		messages, err := tx.ListAgentMessages(main.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		// Not the marker line, not the question in the agent's words, and
		// not the note the drawer drew it from.
		if len(messages) != 0 {
			t.Errorf("the main conversation has %d messages, not none: %+v", len(messages), messages[0])
		}
	})

	mutex.Lock()
	defer mutex.Unlock()
	if len(acknowledged) != 2 || acknowledged[0] != "event-1" || acknowledged[1] != "event-2" {
		t.Errorf("the computer was told it was heard for %v, not for both", acknowledged)
	}
}
