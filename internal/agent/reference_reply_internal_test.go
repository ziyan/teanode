package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A message the person replies to is read from the conversation when the
// turn is kept: its role and words come from the stored message, not from
// what the dashboard sent, and the turn the model reads says what is
// being answered. One from another conversation is dropped, a line the
// dashboard had no id for keeps the words the person saw, and a turn
// answers one message.
func TestAReplyIsReadFromTheConversation(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	conversation := fixture.conversation(t)
	elsewhere := fixture.conversation(t)
	var answer, stranger *models.AgentMessage
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if answer, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: "The ferry leaves at nine and the bus at ten.\n<!--suggestions:[\"Thanks\"]-->"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		if stranger, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: elsewhere.ID, Role: "assistant", Content: "Something said in another conversation."}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
	})

	keep := func(references []models.AgentReference) []models.AgentReference {
		t.Helper()
		settings := &AskSettings{Agent: fixture.agent, Conversation: conversation, Message: "Which is quicker?", Surface: "phone", References: references}
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			if _, err := keepPersonTurn(tx, settings); err != nil {
				t.Fatalf("keepPersonTurn: %s", err)
			}
		})
		return settings.References
	}

	kept := keep([]models.AgentReference{
		{AgentMessageID: answer.ID, QuotedRole: "user", QuotedText: "Something the page made up"},
		// A turn answers one message: the second is left out.
		{AgentMessageID: answer.ID},
	})
	if len(kept) != 1 {
		t.Fatalf("references: %+v", kept)
	}
	reply := kept[0]
	if reply.QuotedRole != "assistant" || reply.QuotedText != "The ferry leaves at nine and the bus at ten." {
		t.Errorf("the reply is not the stored message: %+v", reply)
	}
	turn := userTurn(context.Background(), nil, "Which is quicker?", nil, nil, kept)
	for _, want := range []string{"<references>", "replying to your earlier answer (agent_message_id " + answer.ID + ")", "The ferry leaves at nine", "Which is quicker?"} {
		if !strings.Contains(turn.Content, want) {
			t.Errorf("the turn lacks %q:\n%s", want, turn.Content)
		}
	}
	if strings.Contains(turn.Content, "made up") || strings.Contains(turn.Content, "suggestions") {
		t.Errorf("the turn carries what the page sent or the suggestions line:\n%s", turn.Content)
	}

	if kept := keep([]models.AgentReference{{AgentMessageID: stranger.ID}}); len(kept) != 0 {
		t.Errorf("a message of another conversation was kept: %+v", kept)
	}

	kept = keep([]models.AgentReference{{QuotedRole: "user", QuotedText: "  " + strings.Repeat("long words ", 200)}})
	if len(kept) != 1 || kept[0].QuotedRole != "user" || len([]rune(kept[0].QuotedText)) > replyQuoteCharacters+1 {
		t.Errorf("a line without an id: %+v", kept)
	}
	turn = userTurn(context.Background(), nil, "I meant that", nil, nil, kept)
	if !strings.Contains(turn.Content, "replying to their own earlier message") {
		t.Errorf("the turn does not say whose message it answers:\n%s", turn.Content)
	}
}
