package agent

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A night that could not ask says so, rather than saying nothing was
// missing.
//
// Every way of failing used to come back as answered, on the reasoning
// that reporting every question as a gap is worse than reporting none.
// Both are wrong about the same thing: a question the night never really
// put to the graph is neither answered nor a gap, and a night whose model
// was unreachable read as a night with nothing missing.
func TestAFailedRehearsalIsUnknownAndNotAGap(t *testing.T) {
	for _, each := range []struct {
		name  string
		said  string
		shown int
		want  rehearsalOutcome
	}{
		{"an answer that is not an object", "I had a look and they mostly do", 5, rehearsalUnknown},
		{"a no is the graph having been asked and having nothing", `{"answered": false}`, 5, rehearsalGap},
		{"and an object that does not claim an answer is a no", `{"result": "not really"}`, 5, rehearsalGap},
		{"a yes naming the note it came from", `{"answered": true, "facts": [2]}`, 5, rehearsalAnswered},
		{"a yes that can point at nothing", `{"answered": true, "facts": []}`, 5, rehearsalUnknown},
		{"a yes naming a note it was not shown", `{"answered": true, "facts": [9]}`, 5, rehearsalUnknown},
		{"and a number nobody counts from", `{"answered": true, "facts": [0]}`, 5, rehearsalUnknown},
	} {
		if got := rehearsalVerdict(each.said, each.shown); got != each.want {
			t.Errorf("%s: got %d, want %d", each.name, got, each.want)
		}
	}
}

// A night that has spent its allowance stops asking, and what it did not
// ask about is unknown rather than answered. The judge is a model call
// like any other and the budget is what keeps a night from eating the
// day.
func TestRehearsalWithNothingLeftToSpendIsUnknown(t *testing.T) {
	worker := &Agent{}
	spent := &dreamBudget{allowed: 1000, spent: 1000}
	facts := []*models.AgentFact{{Text: "the portal is the site customers log in to"}}
	if got := worker.factsAnswer(context.Background(), &Run{}, spent, "what is the portal?", facts); got != rehearsalUnknown {
		t.Fatalf("a question nothing was spent on is unknown, not %d", got)
	}
}
