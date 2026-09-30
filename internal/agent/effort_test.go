package agent

import (
	"strings"
	"testing"
)

// The judgement is read as the model gave it; anything it cannot read, or
// a depth it does not know, is no judgement and leaves the turn as it was.
func TestTheDepthJudgementIsReadOrLeftAlone(t *testing.T) {
	for text, want := range map[string]string{
		`{"depth": "dig", "reason": "they are pushing back on the last answer"}`: depthDig,
		`{"depth": "LOOK", "reason": "one clear answer"}`:                        depthLook,
		`{"depth": "answer", "reason": "a thanks"}`:                              depthAnswer,
		`{"depth": "maximum"}`:                                                   depthAnswer,
		`not an object`:                                                          depthAnswer,
	} {
		if judgement := readDepth(text); judgement.depth != want {
			t.Errorf("%s: %s, want %s", text, judgement.depth, want)
		}
	}
	if judgement := readDepth(`{"depth": "dig", "reason": "a problem to diagnose"}`); judgement.depth != depthDig || judgement.reason != "a problem to diagnose" {
		t.Errorf("the reason is kept to be shown: %q %q", judgement.depth, judgement.reason)
	}
	if effort, research := deepenedTurn(depthAnswer); effort != "" || research {
		t.Errorf("an answer is given at once: %q %v", effort, research)
	}
	if effort, research := deepenedTurn(depthLook); effort != "" || research {
		t.Errorf("a question to look up is answered as before: %q %v", effort, research)
	}
	if effort, research := deepenedTurn(depthDig); effort == "" || !research {
		t.Errorf("a question to dig into is researched and thought about: %q %v", effort, research)
	}
}

// How recall should search is read with the depth: at most two searches,
// each short and said once, and whether the message is about a whole area.
func TestTheRetrievalPlanIsReadAndBounded(t *testing.T) {
	judgement := readDepth(`{"depth": "look", "reason": "when the son was born", "searches": ["son born", "Son Born", "", "` +
		strings.Repeat("a", 200) + `", "birth certificate", "third search"], "isBroad": false}`)
	if strings.Join(judgement.searches, "|") != "son born|birth certificate" || judgement.isBroad {
		t.Errorf("searches %q, broad %v", judgement.searches, judgement.isBroad)
	}
	if broad := readDepth(`{"depth": "dig", "reason": "an assessment of the product", "searches": [], "isBroad": true}`); !broad.isBroad || len(broad.searches) != 0 {
		t.Errorf("a broad question: %+v", broad)
	}
	if older := readDepth(`{"depth": "dig", "reason": "no plan given"}`); older.isBroad || len(older.searches) != 0 {
		t.Errorf("a judgement with no plan plans nothing: %+v", older)
	}
}

// A turn a finished background command woke is not judged: nobody typed
// it, and a judgement of it restated the person's earlier request as a
// second note.
func TestABackgroundWakeIsNotJudged(t *testing.T) {
	// No agent: a judgement would reach for its fast model and fail.
	run := &AskRun{settings: &AskSettings{Surface: backgroundSurface}}
	run.chooseDepth()
	if run.hasDepthNote || run.settings.Effort != "" || run.settings.Research {
		t.Fatalf("a background wake was judged: %+v", run.settings)
	}
}
