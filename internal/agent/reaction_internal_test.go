package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The model reads a reaction as a short note after the message it is on:
// the person's on its answer, its own on the person's words. A message
// nobody reacted to reads as it always did.
func TestTheHistoryNotesReactionsAfterTheirMessages(t *testing.T) {
	stored := []*models.AgentMessage{
		{ID: "m1", Role: "user", Content: "thanks, that fixed it", Reactions: []models.AgentMessageReaction{{Emoji: "🙏", ReactorKind: models.ReactorAgent}}},
		{ID: "m2", Role: "assistant", Content: "Glad it worked.", Reactions: []models.AgentMessageReaction{{Emoji: "👍", ReactorKind: models.ReactorPerson}}},
		{ID: "m3", Role: "user", Content: "one more thing"},
		{ID: "m4", Role: "assistant", Content: "Go on.", Reactions: []models.AgentMessageReaction{
			{Emoji: "❤️", ReactorKind: models.ReactorPerson}, {Emoji: "🎉", ReactorKind: models.ReactorAgent},
		}},
	}
	history := historyOf(stored, "")
	if len(history) != 4 {
		t.Fatalf("four messages: %+v", history)
	}
	expected := []struct {
		role    llm.Role
		content string
	}{
		{llm.RoleUser, "thanks, that fixed it\n\n(You reacted 🙏)"},
		{llm.RoleAssistant, "Glad it worked.\n\n(The person reacted 👍)"},
		{llm.RoleUser, "one more thing"},
		{llm.RoleAssistant, "Go on.\n\n(The person reacted ❤️; you reacted 🎉)"},
	}
	for index, want := range expected {
		if history[index].Role != want.role || history[index].Content != want.content {
			t.Errorf("message %d: %q %q, want %q %q", index, history[index].Role, history[index].Content, want.role, want.content)
		}
	}
	if strings.Contains(withReactionNote("", nil), "(") {
		t.Error("no reactions, no note")
	}
}
