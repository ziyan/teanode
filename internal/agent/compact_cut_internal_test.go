package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/llm"
)

// The tail kept word for word never starts after the message the turn
// began with, however much the turn has gathered since: a long check-in
// followed by long tool results used to be cut past, and the model,
// left with tool results and no request, ended the turn with nothing.
func TestTheCutKeepsTheTurnsOwnMessage(t *testing.T) {
	long := strings.Repeat("a fact drafted for the check, at length. ", 400)
	history := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "An earlier question."},
		{Role: llm.RoleAssistant, Content: "An earlier answer."},
		{Role: llm.RoleUser, Content: "[speaking first] It is time for a memory check. " + long},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "memory_check", Arguments: `{"action":"draft"}`}}},
		{Role: llm.RoleTool, ToolCallID: "call_1", Content: long},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call_2", Name: "ask_user", Arguments: `{"question":"Right?"}`}}},
		{Role: llm.RoleTool, ToolCallID: "call_2", Content: `{"answer":"Not sure"}`},
	}
	if cut := compactCut(history, 40); cut > 2 {
		t.Fatalf("the cut is at %d, past the turn's own message at 2", cut)
	}
	// With nothing but the turn itself, nothing is cut at all.
	if cut := compactCut(history[2:], 40); cut != 0 {
		t.Fatalf("the turn alone is kept whole, not cut at %d", cut)
	}
}
