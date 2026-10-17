package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

func TestTheWriterTellsSharedWordsFromTheirOwn(t *testing.T) {
	t.Parallel()

	shared := sharedWordsIn([]*models.AgentMessage{
		{ID: "m1", Role: "user", Content: "Grandma sees the doctor on Tuesday at three, I think."},
		{ID: "m2", Role: "tool", Content: "people/mine\n#1 Their own page says nothing of this."},
		{ID: "m3", Role: "tool", Content: "people/mine (person)\n" + tools.SharedPageMarker + "\n#3 Sees Dr Okafor every Tuesday at three o'clock."},
		{ID: "m4", Role: "assistant", Content: "Here you go. She goes to Dr Okafor every Tuesday (people/a#3 (shared by Ada)). Anything else?"},
	})
	if len(shared.texts) != 2 {
		t.Fatalf("a marked result and a cited sentence: %q", shared.texts)
	}
	if shared.texts[1] != "She goes to Dr Okafor every Tuesday (people/a#3 (shared by Ada)" {
		t.Errorf("the sentence a citation ends: %q", shared.texts[1])
	}

	shown := map[string]string{
		"m1": "Grandma sees the doctor on Tuesday at three, I think.",
		"m4": "Here you go. She goes to Dr Okafor every Tuesday (people/a#3 (shared by Ada)). Anything else?",
	}
	theirWords := map[string]bool{"m1": true}
	for name, check := range map[string]struct {
		wanted RememberedFact
		holds  bool
	}{
		"a quote of a shared tool result":   {RememberedFact{Quote: "Sees Dr Okafor every Tuesday at three o'clock", MessageID: "m4"}, true},
		"a quote of a cited sentence":       {RememberedFact{Quote: "She goes to Dr Okafor every Tuesday", MessageID: "m4"}, true},
		"no quote, the fact's own words":    {RememberedFact{Text: "Sees Dr Okafor every Tuesday at three o'clock."}, true},
		"words the person said":             {RememberedFact{Quote: "sees the doctor on Tuesday at three", MessageID: "m1"}, false},
		"a quote too short to say anything": {RememberedFact{Quote: "Tuesday", Text: "Tuesday"}, false},
		"words from nowhere shared":         {RememberedFact{Quote: "likes tea in the afternoon every day"}, false},
	} {
		if got := shared.holds(check.wanted, shown, theirWords); got != check.holds {
			t.Errorf("%s: refused %v, want %v", name, got, check.holds)
		}
	}
	var none *sharedWords
	if none.holds(RememberedFact{Quote: "Sees Dr Okafor every Tuesday at three o'clock"}, shown, theirWords) {
		t.Error("a reading that was shown nothing shared refuses nothing")
	}
}

func TestTextFromSharedPagesIsWhatFollowsTheMarker(t *testing.T) {
	t.Parallel()

	if got := tools.TextFromSharedPages("own page\n#1 mine"); got != "" {
		t.Errorf("no marker, no shared text: %q", got)
	}
	if got := tools.TextFromSharedPages("own page\n" + tools.SharedPageMarker + "\n#1 theirs"); got != "\n#1 theirs" {
		t.Errorf("everything after the marker: %q", got)
	}
}
