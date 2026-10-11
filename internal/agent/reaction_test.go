package agent_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A reaction reaches whoever watches its conversation, on the instance
// that saw it change and on every other one, as one event of no run that
// names the message, the emoji and who reacted; taking it away is the
// same event with no emoji.
func TestAReactionReachesEveryInstanceWatchingTheConversation(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	newWorker := func(instance string) *agent.Agent {
		worker := agent.New(&agent.Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: instance, Tick: time.Hour})
		worker.Start()
		return worker
	}
	here := newWorker("here")
	defer here.Stop()
	there := newWorker("there")
	defer there.Stop()

	local, unsubscribeLocal := here.SubscribeConversation("conversation-1")
	defer unsubscribeLocal()
	elsewhere, unsubscribeElsewhere := there.SubscribeConversation("conversation-1")
	defer unsubscribeElsewhere()
	unrelated, unsubscribeUnrelated := there.SubscribeConversation("conversation-2")
	defer unsubscribeUnrelated()
	// The LISTEN takes a moment to be in place.
	time.Sleep(300 * time.Millisecond)

	next := func(events <-chan agent.Event, where string) agent.Event {
		t.Helper()
		select {
		case event := <-events:
			return event
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: no event within ten seconds", where)
			return agent.Event{}
		}
	}

	here.PublishReaction("conversation-1", "message-1", "👍", models.ReactorPerson)
	for where, events := range map[string]<-chan agent.Event{"this instance": local, "the other instance": elsewhere} {
		event := next(events, where)
		if event.Kind != agent.EventReacted || event.ConversationID != "conversation-1" || event.MessageID != "message-1" ||
			event.Emoji != "👍" || event.ReactorKind != "person" || event.RunID != "" || event.At.IsZero() {
			t.Fatalf("%s: %+v", where, event)
		}
	}

	here.PublishReaction("conversation-1", "message-1", "", models.ReactorAgent)
	removed := next(elsewhere, "the other instance")
	if removed.Kind != agent.EventReacted || removed.Emoji != "" || removed.ReactorKind != "agent" {
		t.Fatalf("taken away: %+v", removed)
	}
	// What a client reads: the kind, the conversation, the message, who,
	// and the emoji, which is left out when there is none.
	encoded, err := json.Marshal(removed)
	if err != nil {
		t.Fatalf("Marshal: %s", err)
	}
	var shape map[string]any
	_ = json.Unmarshal(encoded, &shape)
	if shape["kind"] != "reacted" || shape["messageId"] != "message-1" || shape["reactorKind"] != "agent" || shape["conversationId"] != "conversation-1" {
		t.Fatalf("the event as JSON: %s", encoded)
	}
	if _, isThere := shape["emoji"]; isThere {
		t.Fatalf("an emoji taken away is no emoji: %s", encoded)
	}

	select {
	case event := <-unrelated:
		t.Fatalf("another conversation heard of it: %+v", event)
	case <-time.After(300 * time.Millisecond):
	}
}
