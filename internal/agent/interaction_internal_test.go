package agent

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A question nobody answers while the turn waits is left open rather than
// failed: the turn ends, the card stays in the store, and an answer given
// later starts a new turn that carries the question and the answer.
func TestAQuestionAnsweredLateCarriesOnInANewTurn(t *testing.T) {
	saved := interactionLiveWait
	interactionLiveWait = 200 * time.Millisecond
	defer func() { interactionLiveWait = saved }()

	database, release := dbtest.AcquireDatabase(t)
	defer release()
	arguments, _ := json.Marshal(map[string]any{"question": "Which color for the shed?", "choices": []string{"Red", "Blue"}})
	askRound := fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_color","function":{"name":"ask_user","arguments":%q}}]},"finish_reason":"tool_calls"}]}`, string(arguments))
	provider := scriptedProvider([]string{askRound, saidByModel("I will wait for you."), saidByModel("Blue it is.")})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, Surface: "drawer", LastAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	})
	operations := &digestSplitOperations{}
	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation, Message: "paint the shed", Surface: "drawer"})
	if err != nil {
		t.Fatal(err)
	}
	drainTurn(turn)

	var interaction *models.AgentInteraction
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		open, err := tx.ListOpenAgentInteractions(conversation.ID)
		if err != nil || len(open) != 1 || open[0].InteractionText != "Which color for the shed?" || len(open[0].InteractionChoices) != 2 {
			t.Fatalf("the question is kept open: %+v %v", open, err)
		}
		interaction = open[0]
		messages, _ := tx.ListAgentMessages(conversation.ID, nil)
		last := messages[len(messages)-1]
		if last.Content != "I will wait for you." {
			t.Fatalf("the turn ended when nobody answered: %+v", last)
		}
		if isClaimed, err := tx.ClaimAgentInteraction(interaction.ID, "Blue"); err != nil || !isClaimed {
			t.Fatalf("a late answer claims it: %v %v", isClaimed, err)
		}
		if isClaimed, _ := tx.ClaimAgentInteraction(interaction.ID, "Red"); isClaimed {
			t.Fatal("and nobody claims it twice")
		}
	})
	resumed, err := worker.ResumeInteraction(&ResumeSettings{Interaction: interaction, Answer: "Blue", Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation}, "[chat about it]")
	if err != nil || resumed == nil {
		t.Fatalf("ResumeInteraction: %v", err)
	}
	drainTurn(resumed)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, _ := tx.ListAgentMessages(conversation.ID, nil)
		var answering, answered bool
		for _, message := range messages {
			if message.Role == "user" && message.Content == AnsweringMarker+" Which color for the shed?\n\nBlue" {
				answering = true
			}
			if message.Content == "Blue it is." {
				answered = true
			}
		}
		if !answering || !answered {
			t.Fatalf("a new turn carries the question and the answer on: %+v", messages)
		}
	})

	// Chatting about it instead resumes nothing.
	if chat, err := worker.ResumeInteraction(&ResumeSettings{Interaction: interaction, Answer: "[chat about it]"}, "[chat about it]"); err != nil || chat != nil {
		t.Fatalf("chatting about it starts no turn: %v %v", chat, err)
	}
}

// An approval given after its turn ended lets the new turn make exactly
// that call once: the same tool with the same arguments, however they are
// spaced; not other arguments, and not a second time.
func TestALateApprovalAllowsExactlyThatCallOnce(t *testing.T) {
	run := &AskRun{settings: &AskSettings{PreApproved: map[string]bool{
		approvalKey("mail_send", json.RawMessage(`{"draft_id":"d1","to":"someone@example.net"}`)): true,
	}}}
	if run.takePreApproval("mail_send", json.RawMessage(`{"to": "someone@example.net", "draft_id": "d1"}`)) != true {
		t.Fatal("the approved call, its keys in another order, runs")
	}
	if run.takePreApproval("mail_send", json.RawMessage(`{"draft_id":"d1","to":"someone@example.net"}`)) {
		t.Fatal("once")
	}
	run.settings.PreApproved[approvalKey("mail_send", json.RawMessage(`{"draft_id":"d1"}`))] = true
	if run.takePreApproval("mail_send", json.RawMessage(`{"draft_id":"d2"}`)) || run.takePreApproval("mail_act", json.RawMessage(`{"draft_id":"d1"}`)) {
		t.Fatal("not with other arguments, and not another tool")
	}
}

// drainTurn waits for a turn to finish.
func drainTurn(turn *AskRun) {
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	for range events {
	}
}
