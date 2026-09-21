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
		if got := GoalChangeNote(testCase.before, testCase.after); got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}
