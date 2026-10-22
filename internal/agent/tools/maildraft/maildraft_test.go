package maildraft

import (
	"strings"
	"testing"
)

// Mail keeps less of an HTML page than a browser does, and the draft says
// which part of what was written it will not keep, so the model revises it
// then and the prompt of every turn does not carry the rules.
func TestHTMLProblemsNameWhatMailDoesNotKeep(t *testing.T) {
	t.Parallel()

	if problems := htmlProblems(`<h1>Summary</h1><table><tr><td style="color:#333">Total</td></tr></table>`, nil); len(problems) != 0 {
		t.Fatalf("a plain styled table has nothing wrong with it: %v", problems)
	}

	problems := htmlProblems(`<script>draw()</script><div style="display: flex; color: var(--accent)"><img src="https://example.test/chart.png"></div>`, []string{"chart.svg"})
	joined := strings.Join(problems, "\n")
	for _, expected := range []string{"no script runs", "no CSS variable", "flex and grid", "another server", "SVG (chart.svg)"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("the problems do not say %q:\n%s", expected, joined)
		}
	}
	if len(problems) != 5 {
		t.Errorf("each problem is said once, got %d: %v", len(problems), problems)
	}
}
