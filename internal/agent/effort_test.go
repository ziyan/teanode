package agent

import "testing"

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
		if depth, _ := readDepth(text); depth != want {
			t.Errorf("%s: %s, want %s", text, depth, want)
		}
	}
	if depth, reason := readDepth(`{"depth": "dig", "reason": "a problem to diagnose"}`); depth != depthDig || reason != "a problem to diagnose" {
		t.Errorf("the reason is kept to be shown: %q %q", depth, reason)
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
