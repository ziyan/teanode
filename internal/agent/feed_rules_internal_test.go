package agent

import (
	"strings"
	"testing"
)

// A turn that may post a card is told what a good card is, in the same words
// the feed tool gives.
func TestAGoalTurnIsToldTheRulesOfACard(t *testing.T) {
	for name, lines := range map[string]string{"goal": goalFeedLines, "goal on set times": goalOnTimesFeedLines} {
		for _, expected := range []string{
			"what happened, why it matters to them, and one or two next steps", "this turn's tool results", "provenance", "call feed list first", "never rewrite a card into a different subject",
			"resolve that card with a short note", "materially worse", "no ids or tool names", "the title say what the body says", "Silence is the default",
		} {
			if !strings.Contains(lines, expected) {
				t.Errorf("the %s lines do not say %q", name, expected)
			}
		}
		if strings.ContainsAny(lines, "\u2014\u2013") {
			t.Errorf("the %s lines have a dash", name)
		}
	}
}
