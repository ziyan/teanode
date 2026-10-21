package privateturn

import (
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A call is private by the tool's name and, for Passwords, by the action it
// names: listing, reading a note and making up a password are, and the rest of
// the tool is not. Health is private whatever it is asked.
func TestWhichCallsArePrivate(t *testing.T) {
	for name, test := range map[string]struct {
		call models.AgentToolCall
		want bool
	}{
		"health":                      {models.AgentToolCall{Name: "health", Arguments: `{"action":"summary"}`}, true},
		"a named health action":       {models.AgentToolCall{Name: "health_summary"}, true},
		"listing passwords":           {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"list"}`}, true},
		"reading a note":              {models.AgentToolCall{Name: "passwords_read_note"}, true},
		"making up a password":        {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"generate"}`}, true},
		"another passwords action":    {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"status"}`}, false},
		"a tool of another name":      {models.AgentToolCall{Name: "mail_search", Arguments: `{"action":"list"}`}, false},
		"arguments that are not JSON": {models.AgentToolCall{Name: "passwords", Arguments: `list`}, false},
	} {
		if got := IsToolCall(test.call); got != test.want {
			t.Errorf("%s: IsToolCall = %v, want %v", name, got, test.want)
		}
	}
}

// A message steered into a turn does not end it, so the answer after it is
// private too, and the next message of the person starts a turn that is not.
func TestTurnsRunFromOneMessageOfThePersonToTheNext(t *testing.T) {
	message := func(role string, calls ...models.AgentToolCall) *models.AgentMessage {
		return &models.AgentMessage{Role: role, ToolCalls: calls}
	}
	messages := []*models.AgentMessage{
		message("user"),
		message("assistant", models.AgentToolCall{Name: "passwords", Arguments: `{"action":"read_note"}`}),
		message("tool"),
		message("user"),
		message("assistant"),
		message("user"),
		message("assistant"),
	}
	got := Turns(messages)
	want := []bool{true, true, true, true, true, false, false}
	if !slices.Equal(got, want) {
		t.Errorf("Turns = %v, want %v", got, want)
	}
}

func TestToolNamesAreThePrivateTools(t *testing.T) {
	if got := ToolNames(); !slices.Equal(got, []string{"health", "passwords"}) {
		t.Errorf("ToolNames = %v", got)
	}
}

// The answer of a subagent whose run called a private tool holds what the call
// showed, and reaches the parent as an ordinary tool result: the parent's turn
// is private. A subagent that called none leaves the turn as it was, and so
// does an answer that merely mentions the key.
func TestATurnWhoseSubagentCalledAPrivateToolIsPrivate(t *testing.T) {
	turn := func(answer string) []*models.AgentMessage {
		return []*models.AgentMessage{
			{Role: "user", Content: "what is in my wifi note?"},
			{Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "1", Name: "subagent", Arguments: `{"prompt":"find the note"}`}}},
			{Role: "tool", ToolCallID: "1", Name: "subagent", Content: answer},
			{Role: "assistant", Content: "Here is what the note says."},
			{Role: "user", Content: "thanks, what is the weather?"},
			{Role: "assistant", Content: "Sunny."},
		}
	}
	for name, test := range map[string]struct {
		answer string
		want   []bool
	}{
		"a private call":              {`{"answer":"the note says: invented text","run":"r","tool_calls":2,"has_private_call":true}`, []bool{true, true, true, true, false, false}},
		"no private call":             {`{"answer":"sunny","run":"r","tool_calls":2,"has_private_call":false}`, []bool{false, false, false, false, false, false}},
		"an old answer":               {`{"answer":"sunny","run":"r","tool_calls":2}`, []bool{false, false, false, false, false, false}},
		"words not a key":             {`{"answer":"has_private_call is true","run":"r","tool_calls":2}`, []bool{false, false, false, false, false, false}},
		"not JSON":                    {`has_private_call`, []bool{false, false, false, false, false, false}},
		"a key that is not a boolean": {`{"has_private_call":"yes"}`, []bool{false, false, false, false, false, false}},
	} {
		if got := Turns(turn(test.answer)); !slices.Equal(got, test.want) {
			t.Errorf("%s: Turns = %v, want %v", name, got, test.want)
		}
	}

	// Another tool's result with the key is not a subagent's answer.
	other := turn(`{"has_private_call":true}`)
	other[2].Name = "mail_search"
	if got := Turns(other); slices.Contains(got, true) {
		t.Errorf("another tool: Turns = %v", got)
	}
}
