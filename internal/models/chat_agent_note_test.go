package models

import "testing"

// The note a person is told when they ask somebody else's agent and may not
// names the owner as well as the agent, and one written before it did still
// reads.
func TestTheChatAgentNoteNamesWhoseAgentItIs(t *testing.T) {
	detail := ChatAgentDetail("Agent", "Fixture Owner")
	note := ParseChatAgentNote(detail)
	if note == nil || note.AgentName != "Agent" || note.OwnerName != "Fixture Owner" {
		t.Fatalf("the note: %+v", note)
	}
	if got := NoteText(NoteChatAgentOwnerOnly, detail); got != "Only Fixture Owner can ask Fixture Owner's agent in this chat." {
		t.Errorf("the note in English: %q", got)
	}
	if ParseChatAgentNote("Agent") != nil || ParseChatAgentNote(`{"agentName":"Agent"}`) != nil {
		t.Error("a detail that names no owner was read as one that does")
	}
	if got := NoteText(NoteChatAgentOwnerOnly, "Agent"); got != "Only Agent's owner can ask it in this chat." {
		t.Errorf("a note written before the owner was named: %q", got)
	}
}
