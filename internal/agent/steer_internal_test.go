package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A turn takes what it is handed until it decides to end; handed something
// as it decides, it goes on instead; once it has ended, a message is a
// turn of its own.
func TestSteeringClosesOnlyWhenNothingWaits(t *testing.T) {
	running := &AskRun{}
	message := &AskRun{}
	message.ctx, message.cancel = context.WithCancel(context.Background())
	defer message.cancel()
	if !running.steer(message) {
		t.Fatal("a running turn takes a message")
	}
	if !running.closeToSteering() {
		t.Fatal("a turn with a message waiting goes one round more")
	}
	running.mutex.Lock()
	running.steering = nil
	running.mutex.Unlock()
	if running.closeToSteering() {
		t.Fatal("a turn with nothing waiting ends")
	}
	if running.steer(message) {
		t.Fatal("a turn that has decided to end takes nothing more")
	}
}

// A conversation's feed replays a running turn once: a message steered into
// it keeps that turn's events for whoever follows the message, and those
// copies are not replayed beside the turn's own.
func TestTheFeedReplaysASteeredTurnOnce(t *testing.T) {
	conversation := &models.AgentConversation{ID: "conversation-1"}
	running := &AskRun{ID: "01RUNNING", settings: &AskSettings{Conversation: conversation}}
	running.events = []Event{
		{Kind: EventAsked, RunID: running.ID, Text: "Import the statement"},
		{Kind: EventText, RunID: running.ID, Text: "Importing."},
	}
	message := &AskRun{ID: "01STEERED", settings: &AskSettings{Conversation: conversation}}
	message.events = []Event{{Kind: EventAsked, RunID: message.ID, Text: "And the other bank"}}
	message.relay(Event{Kind: EventText, RunID: running.ID, Text: "Importing."})
	worker := &Agent{runs: map[string]*AskRun{running.ID: running, message.ID: message}}

	feed, unsubscribe := worker.SubscribeConversation(conversation.ID)
	defer unsubscribe()
	var replayed []string
	for len(feed) > 0 {
		event := <-feed
		replayed = append(replayed, event.RunID+" "+string(event.Kind)+" "+event.Text)
	}
	want := []string{
		"01RUNNING asked Import the statement",
		"01RUNNING text Importing.",
		"01STEERED asked And the other bank",
	}
	if strings.Join(replayed, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the feed replayed:\n%s\nnot:\n%s", strings.Join(replayed, "\n"), strings.Join(want, "\n"))
	}
	if len(message.events) != 2 {
		t.Fatalf("the steered message's own followers lost the turn's events: %+v", message.events)
	}
}
