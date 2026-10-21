package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
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

// A compaction that falls inside a private turn leaves the agent's answer that
// quotes the note at the start of the history, without the call that makes it
// private. A run the person did not start must still not read it, and neither
// must the note a later compaction writes from that history.
func TestACompactionInsideAPrivateTurnStillHidesTheAnswer(t *testing.T) {
	stored := []*models.AgentMessage{
		{ID: "m1", Role: "user", Content: "read me the shed note"},
		{ID: "m2", Role: "assistant", ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "passwords", Arguments: `{"action":"read_note","item_id":"x"}`}}},
		{ID: "m3", Role: "tool", ToolCallID: "c1", Name: "passwords", Content: `{"note_text":"the alarm code is 0000-invented"}`},
		{ID: "m4", Role: "assistant", Content: "It says: the alarm code is 0000-invented."},
		{ID: "m5", Role: "compaction", Content: "earlier: the person asked for a note"},
		{ID: "m6", Role: "user", Content: "thanks"},
		{ID: "m7", Role: "assistant", Content: "welcome"},
	}
	// The compaction stands in for m1 to m3: the cut fell before the answer.
	history := historyOf(stored, "m3")

	for _, surface := range []string{"goal", "mail", "telegram", "agent_command", ""} {
		run := &AskRun{settings: &AskSettings{Surface: surface, Headless: true}}
		if run.IsStartedByPerson() {
			t.Fatalf("a run on %q counts as the person's", surface)
		}
		for _, message := range run.historyForRun(history) {
			if strings.Contains(message.Content, "0000-invented") {
				t.Errorf("%q: the run reads the note text: %+v", surface, message)
			}
		}
	}

	// What a later compaction writes its note from.
	older := withoutPrivateHistory(history, privateMessages(history))
	for _, message := range older {
		if strings.Contains(message.Content, "0000-invented") {
			t.Errorf("the compaction would read the note text: %+v", message)
		}
	}

	// The person's own run reads all of it, and the words after the turn stay.
	person := &AskRun{settings: &AskSettings{Surface: "drawer", Message: "and now"}}
	if !person.IsStartedByPerson() {
		t.Fatal("a drawer turn is the person's")
	}
	if shown := person.historyForRun(history); len(shown) != len(history) || !strings.Contains(shown[1].Content, "0000-invented") {
		t.Errorf("the person's own run was given %+v", shown)
	}
	last := history[len(history)-1]
	if last.IsFromPrivateTurn || last.Content != "welcome" {
		t.Errorf("a message of a later turn was marked private: %+v", last)
	}
}
