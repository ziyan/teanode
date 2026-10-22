package browser

import (
	"strings"
	"testing"
)

// A steps call is judged by the steps it lists, one level down, so a steps
// call inside one is refused before any step runs: it would otherwise be
// judged a read while its inner writes ran.
func TestNestedStepsAreRefusedBeforeAnythingRuns(t *testing.T) {
	run := newReachRun("https://example.org/")
	_, err := runOnTab(t, run, `{"action":"steps","target":"tab","steps":[{"action":"snapshot","target":"tab"},{"action":"steps","target":"tab","steps":[{"action":"evaluate","target":"tab","expression":"1"}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "cannot be nested") {
		t.Fatalf("nested steps were not refused: %v", err)
	}
	if asked := run.first.actionsAsked(); len(asked) != 0 {
		t.Fatalf("the person's browser was asked %v", asked)
	}
}

// Steps that are not nested still run in order.
func TestFlatStepsStillRun(t *testing.T) {
	run := newReachRun("https://example.org/")
	if _, err := runOnTab(t, run, `{"action":"steps","target":"tab","steps":[{"action":"snapshot","target":"tab"},{"action":"snapshot","target":"tab"}]}`); err != nil {
		t.Fatalf("flat steps failed: %s", err)
	}
	if asked := run.first.actionsAsked(); len(asked) != 2 {
		t.Fatalf("the person's browser was asked %v", asked)
	}
}
