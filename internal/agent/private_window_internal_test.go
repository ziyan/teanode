package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The filing run takes the messages after its mark. A mark can land between
// the assistant message that made the private call and the answer that quotes
// what it returned, and a message the person steers into the turn can end a
// batch there. The turn is judged by the whole conversation, not by the part
// after the mark, so the answer is not filed.
func TestAFilingWindowThatStartsAfterThePrivateCallLeavesOutTheAnswer(t *testing.T) {
	message := func(id string, role llm.Role, content, toolName, arguments string) *models.AgentMessage {
		found := &models.AgentMessage{ID: id, Role: string(role), Content: content}
		if toolName != "" {
			found.ToolCalls = append(found.ToolCalls, models.AgentToolCall{ID: id + toolName, Name: toolName, Arguments: arguments})
		}
		return found
	}
	messages := []*models.AgentMessage{
		message("u1", llm.RoleUser, "Read me the shed note", "", ""),
		message("a1", llm.RoleAssistant, "Reading it", "passwords", `{"action":"read_note","item_id":"x"}`),
		message("t1", llm.RoleTool, `{"note_text":"The padlock opens with 0000."}`, "", ""),
		message("u2", llm.RoleUser, "Also, what day is it?", "", ""),
		message("a2", llm.RoleAssistant, "The padlock opens with 0000. It is Tuesday.", "", ""),
		message("u3", llm.RoleUser, "Thanks", "", ""),
		message("a3", llm.RoleAssistant, "You are welcome.", "", ""),
	}
	// u2 was steered into the turn: it follows a tool message.
	afterMark := messages[2:]
	for _, kept := range worthReadingIn(messages, afterMark) {
		if kept.ID == "a2" {
			t.Errorf("the answer that quotes the note (%q) would be filed into memory", kept.Content)
		}
	}
	kept := map[string]bool{}
	for _, each := range worthReadingIn(messages, afterMark) {
		kept[each.ID] = true
	}
	for _, id := range []string{"u2", "u3", "a3"} {
		if !kept[id] {
			t.Errorf("%s is not part of the private turn and should be filed", id)
		}
	}
	for _, each := range withoutPrivateTurnsIn(messages, afterMark) {
		if each.ID == "t1" {
			t.Errorf("the lessons window would read the note itself")
		}
	}
}
