package apigraph

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
)

// feed is a turn's events, delivered and then ended the way a finished run
// ends: the channel closes.
func feed(events ...agent.Event) <-chan agent.Event {
	channel := make(chan agent.Event, len(events))
	for _, event := range events {
		channel <- event
	}
	close(channel)
	return channel
}

func TestTheAnswerIsWhatTheAgentSaid(test *testing.T) {
	said := mcpCollect(context.Background(), feed(
		agent.Event{Kind: agent.EventToolCall, Tool: "mail_search"},
		agent.Event{Kind: agent.EventToolResult, Tool: "mail_search"},
		agent.Event{Kind: agent.EventMessage, Text: "Three of them, all from the printer."},
		agent.Event{Kind: agent.EventDone},
	), "Robin", "conversation-1")

	if said != "Three of them, all from the printer." {
		test.Fatalf("the answer came back as %q", said)
	}
}

// The pieces of an answer are what a drawer draws as it arrives. They are
// only used where no whole message followed, which is a turn that was cut
// short: without this the caller gets nothing at all where the words were
// on the wire.
func TestThePiecesAreUsedWhenNoWholeAnswerFollowed(test *testing.T) {
	said := mcpCollect(context.Background(), feed(
		agent.Event{Kind: agent.EventText, Text: "Half a sen"},
		agent.Event{Kind: agent.EventText, Text: "tence."},
	), "Robin", "conversation-1")

	if said != "Half a sentence." {
		test.Fatalf("the answer came back as %q", said)
	}
}

// A turn that stopped for the person cannot be answered from a harness.
// Saying so is what lets the caller tell its person where to go; without
// it the call comes back looking like an answer that simply stopped.
func TestATurnWaitingForThePersonSaysSo(test *testing.T) {
	said := mcpCollect(context.Background(), feed(
		agent.Event{Kind: agent.EventMessage, Text: "I have written the reply."},
		agent.Event{Kind: agent.EventConfirmation, Tool: "mail_send"},
		agent.Event{Kind: agent.EventDone},
	), "Robin Example", "conversation-1")

	if !strings.Contains(said, "I have written the reply.") {
		test.Fatalf("what it had said was thrown away: %q", said)
	}
	if !strings.Contains(said, "Robin Example") || !strings.Contains(said, "dashboard") {
		test.Fatalf("the person was not told where to answer: %q", said)
	}
}

func TestAQuestionForThePersonCarriesTheQuestion(test *testing.T) {
	said := mcpCollect(context.Background(), feed(
		agent.Event{Kind: agent.EventQuestion, Text: "Which of the two addresses?"},
		agent.Event{Kind: agent.EventDone},
	), "Robin", "conversation-1")

	if !strings.Contains(said, "Which of the two addresses?") {
		test.Fatalf("the question was lost: %q", said)
	}
}

func TestAFailedTurnSaysWhy(test *testing.T) {
	said := mcpCollect(context.Background(), feed(
		agent.Event{Kind: agent.EventError, Error: "the model provider refused"},
		agent.Event{Kind: agent.EventDone},
	), "Robin", "conversation-1")

	if !strings.Contains(said, "the model provider refused") {
		test.Fatalf("the reason was lost: %q", said)
	}
}

// A turn that says nothing at all is not silence: a caller that gets an
// empty string cannot tell it apart from a broken call.
func TestATurnThatSaidNothingSaysThat(test *testing.T) {
	said := mcpCollect(context.Background(), feed(agent.Event{Kind: agent.EventDone}), "Robin", "conversation-1")
	if strings.TrimSpace(said) == "" {
		test.Fatal("a turn that said nothing came back as nothing")
	}
}

// Outlasting the wait hands back what there is and says how to come back
// for the rest, naming the conversation. Without the name the caller has
// nothing to ask with.
func TestOutlastingTheWaitNamesTheConversationToComeBackTo(test *testing.T) {
	// A channel that never delivers and never closes is a turn still
	// working, with the wait shortened so that finding out takes no time.
	was := mcpAskWait
	mcpAskWait = 50 * time.Millisecond
	defer func() { mcpAskWait = was }()

	never := make(chan agent.Event)
	done := make(chan string, 1)
	go func() { done <- mcpCollect(context.Background(), never, "Robin", "conversation-7") }()

	select {
	case said := <-done:
		if !strings.Contains(said, "conversation-7") {
			test.Fatalf("the conversation was not named: %q", said)
		}
		if !strings.Contains(said, mcpAskName) {
			test.Fatalf("how to come back was not said: %q", said)
		}
		if !strings.Contains(said, "still working") {
			test.Fatalf("the caller was not told the turn goes on: %q", said)
		}
	case <-time.After(10 * time.Second):
		test.Fatal("the wait did not end")
	}
}

// The caller going away mid-turn is not the turn failing. What comes back
// says where the answer will be, because the turn carries on.
func TestTheCallerGoingAwayPointsAtTheConversation(test *testing.T) {
	never := make(chan agent.Event)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	said := mcpCollect(ctx, never, "Robin", "conversation-9")
	if !strings.Contains(said, "conversation-9") {
		test.Fatalf("the conversation was not named: %q", said)
	}
}
