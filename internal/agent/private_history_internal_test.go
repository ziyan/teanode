package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/llm"
)

// A private turn becomes one marker for what the agent said and its tools
// answered; the person's words stay; the marker stands in for the last stored
// message of the stretch, so a compaction through it still says where the
// transcript resumes. A turn cut in two by a compaction is judged by all of
// it.
func TestAPrivateTurnBecomesOneMarker(t *testing.T) {
	history := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "read me the shed note", SourceID: "m1"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "passwords", Arguments: `{"action":"read_note"}`}}, SourceID: "m2"},
		{Role: llm.RoleTool, ToolCallID: "c1", Name: "passwords", Content: `{"note_text":"0000-invented"}`, SourceID: "m3"},
		{Role: llm.RoleAssistant, Content: "It says 0000-invented.", SourceID: "m4"},
		{Role: llm.RoleUser, Content: "thanks, and what day is it?", SourceID: "m5"},
		{Role: llm.RoleAssistant, Content: "Tuesday.", SourceID: "m6"},
	}
	isPrivate := privateMessages(history)
	hidden := withoutPrivateHistory(history, isPrivate)
	if len(hidden) != 4 {
		t.Fatalf("the history has %d messages, want the person's two, one marker and the answer: %+v", len(hidden), hidden)
	}
	if hidden[1].Role != llm.RoleAssistant || hidden[1].Content != tools.HiddenPrivateWords || hidden[1].SourceID != "m4" {
		t.Errorf("the marker is %+v", hidden[1])
	}
	if hidden[0].Content != history[0].Content || hidden[3].Content != "Tuesday." {
		t.Errorf("what the person wrote or a later turn changed: %+v", hidden)
	}

	// The part before a cut that falls after the tool's answer is still
	// private, since the whole history decides.
	older := withoutPrivateHistory(history[:3], isPrivate)
	for _, message := range older {
		if message.Role != llm.RoleUser && message.Content != tools.HiddenPrivateWords {
			t.Errorf("the older part of a private turn was kept: %+v", message)
		}
	}
}
