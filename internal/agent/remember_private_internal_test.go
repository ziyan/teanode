package agent

import (
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A call of a private tool is one that read Health, or that listed the items,
// read a note or made up a password in the person's Passwords: the names, sites
// and user names of the items are not for memory or for a later run either.
func TestWhichToolCallsArePrivate(t *testing.T) {
	for name, test := range map[string]struct {
		call      models.AgentToolCall
		isPrivate bool
	}{
		"health":                          {models.AgentToolCall{Name: "health", Arguments: `{"action":"types"}`}, true},
		"a health action by its name":     {models.AgentToolCall{Name: "health_samples"}, true},
		"read a note":                     {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"read_note","item_id":"x"}`}, true},
		"read a note, shouted":            {models.AgentToolCall{Name: "passwords", Arguments: `{"action":" READ_NOTE "}`}, true},
		"read a note by its action name":  {models.AgentToolCall{Name: "passwords_read_note"}, true},
		"generate":                        {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"generate"}`}, true},
		"list":                            {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"list"}`}, true},
		"list by its action name":         {models.AgentToolCall{Name: "passwords_list"}, true},
		"fill a login (a browser action)": {models.AgentToolCall{Name: "browser", Arguments: `{"action":"fill_login"}`}, false},
		"passwords with no arguments":     {models.AgentToolCall{Name: "passwords"}, false},
		"passwords with broken ones":      {models.AgentToolCall{Name: "passwords", Arguments: `{"action":`}, false},
		"the word in another field":       {models.AgentToolCall{Name: "passwords", Arguments: `{"action":"show","item_id":"read_note"}`}, false},
		"another tool":                    {models.AgentToolCall{Name: "weather", Arguments: `{"action":"read_note"}`}, false},
		"a tool that starts the same":     {models.AgentToolCall{Name: "passwordsafe", Arguments: `{"action":"read_note"}`}, false},
	} {
		if got := isPrivateToolCall(test.call); got != test.isPrivate {
			t.Errorf("%s: isPrivateToolCall = %v, want %v", name, got, test.isPrivate)
		}
	}
}

// A turn that read a note in Passwords is kept out of the filing run exactly as
// a turn that read Health is: what the person said stays, what the agent said
// and its tools answered does not. That holds for a turn that only listed the
// items too.
func TestAFilingRunSkipsATurnThatReadANote(t *testing.T) {
	message := func(id string, role llm.Role, content, toolName, arguments string) *models.AgentMessage {
		found := &models.AgentMessage{ID: id, Role: string(role), Content: content}
		if toolName != "" {
			found.ToolCalls = append(found.ToolCalls, models.AgentToolCall{ID: id + toolName, Name: toolName, Arguments: arguments})
		}
		return found
	}
	messages := []*models.AgentMessage{
		message("u1", llm.RoleUser, "What can you use in my passwords?", "", ""),
		message("a1", llm.RoleAssistant, "Let me look", "passwords", `{"action":"list"}`),
		message("t1", llm.RoleTool, `{"item_list":[{"item_name":"Shed"}]}`, "", ""),
		message("a2", llm.RoleAssistant, "Your shed note and one login", "", ""),
		message("u2", llm.RoleUser, "Read me the shed note", "", ""),
		message("a3", llm.RoleAssistant, "Reading it", "passwords", `{"action":"read_note","item_id":"x"}`),
		message("t3", llm.RoleTool, `{"note_text":"The padlock opens with 0000."}`, "", ""),
		message("a4", llm.RoleAssistant, "The padlock opens with 0000.", "", ""),
		message("u3", llm.RoleUser, "Thanks, I moved to a new flat", "", ""),
		message("a5", llm.RoleAssistant, "Noted, congratulations", "", ""),
	}

	var got []string
	for _, each := range worthReading(messages) {
		got = append(got, each.ID)
	}
	want := []string{"u1", "u2", "u3", "a5"}
	if !slices.Equal(got, want) {
		t.Fatalf("worthReading kept %v, want %v", got, want)
	}
}

// A conversation is not described from the turn that read a note or listed the
// items, and is from one that did neither.
func TestAConversationIsNotDescribedFromATurnThatReadANote(t *testing.T) {
	read := []*models.AgentMessage{
		{ID: "u1", Role: string(llm.RoleUser), Content: "Read me the shed note"},
		{ID: "a1", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "passwords", Arguments: `{"action":"read_note"}`}}},
		{ID: "t1", Role: string(llm.RoleTool), Content: `{"note_text":"0000"}`},
		{ID: "a2", Role: string(llm.RoleAssistant), Content: "It says 0000"},
	}
	if !isPrivateRead(read, 3) || !isPrivateRead(read, 0) {
		t.Error("a turn that read a note is private from any message of it")
	}
	listed := []*models.AgentMessage{read[0], {ID: "a1", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "passwords", Arguments: `{"action":"list"}`}}}, read[2], read[3]}
	if !isPrivateRead(listed, 3) {
		t.Error("a turn that listed the items is private")
	}
	other := []*models.AgentMessage{read[0], {ID: "a1", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "weather", Arguments: `{"action":"today"}`}}}, read[2], read[3]}
	if isPrivateRead(other, 3) {
		t.Error("a turn that used another tool is not private")
	}
}
