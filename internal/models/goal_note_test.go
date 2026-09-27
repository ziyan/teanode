package models

import "testing"

func TestGoalChangeNote(t *testing.T) {
	none := &AgentConversation{}
	set := &AgentConversation{Goal: "tidy the inbox", GoalState: GoalWorking}
	changed := &AgentConversation{Goal: "tidy the archive", GoalState: GoalWorking}
	met := &AgentConversation{Goal: "tidy the inbox", GoalState: GoalMet, GoalNote: "nothing left to file"}
	metQuietly := &AgentConversation{Goal: "tidy the inbox", GoalState: GoalMet}
	waiting := &AgentConversation{Goal: "tidy the inbox", GoalState: GoalWaiting, GoalNote: "which folder?"}

	cases := []struct {
		name          string
		before, after *AgentConversation
		want          string
	}{
		{"nothing to nothing", none, none, ""},
		{"set", none, set, "Goal set: tidy the inbox"},
		{"set on a new conversation", nil, set, "Goal set: tidy the inbox"},
		{"changed", set, changed, "Goal changed: tidy the archive"},
		{"cleared", set, none, "Goal cleared: tidy the inbox"},
		{"met with a note", set, met, "Goal met: nothing left to file"},
		{"met without a note", set, metQuietly, "Goal met: tidy the inbox"},
		{"met again is nothing", met, met, ""},
		{"waiting is the bar's", set, waiting, ""},
		{"saved unchanged", set, set, ""},
		{"set again after met", met, set, "Goal set again: tidy the inbox"},
		{"a new goal after met", met, changed, "Goal set: tidy the archive"},
	}
	for _, testCase := range cases {
		kind, detail := GoalChangeNote(testCase.before, testCase.after)
		got := ""
		if kind != "" {
			got = NoteText(kind, detail)
		}
		if got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// A note of a kind reads in English for whoever has no kinds: the kind's
// words, then the detail, and a compaction without the note it stands for.
func TestNoteText(t *testing.T) {
	for _, testCase := range []struct {
		kind   AgentNoteKind
		detail string
		want   string
	}{
		{NoteStopped, "", "stopped"},
		{NoteStopped, "the daily budget is spent", "stopped: the daily budget is spent"},
		{NoteDepth, "a problem to diagnose", "looking into this carefully: a problem to diagnose"},
		{NoteCompacted, "Decided: the regatta is on the 21st.", "the earlier conversation was compacted into a note"},
		{NoteGoalStalled, "24", "Goal stalled: 24 turns since you last wrote and it is not met. Write to keep going, or clear or change it."},
		{"", "written as it is", "written as it is"},
	} {
		if got := NoteText(testCase.kind, testCase.detail); got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.kind, got, testCase.want)
		}
	}
}
