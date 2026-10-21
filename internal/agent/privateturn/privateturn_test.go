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
