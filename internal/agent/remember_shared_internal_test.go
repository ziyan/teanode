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

// Words read from a recording another person shared are shared words too: a
// fact that only repeats them is refused, as one from a shared page is, and
// the person's own words beside them are theirs.
func TestTheWriterTellsSharedRecordingWordsFromTheirOwn(t *testing.T) {
	t.Parallel()

	shared := sharedWordsIn([]*models.AgentMessage{
		{ID: "m1", Role: "user", Content: "What did they decide about the release?"},
		{ID: "m2", Role: "tool", Content: tools.SharedRecordingMarker + "\n" + `{"recording":{"passages":[{"speakerName":"Alex","transcriptText":"We should ship the release on Friday after the review."}]}}`},
	})
	if len(shared.texts) != 1 {
		t.Fatalf("a marked result: %q", shared.texts)
	}
	shown := map[string]string{"m1": "What did they decide about the release?"}
	theirWords := map[string]bool{"m1": true}
	if !shared.holds(RememberedFact{Quote: "We should ship the release on Friday after the review"}, shown, theirWords) {
		t.Error("a quote of a shared recording is refused")
	}
	if shared.holds(RememberedFact{Quote: "What did they decide about the release"}, shown, theirWords) {
		t.Error("words the person said are theirs to have written down")
	}
	if got := tools.TextFromSharedPages("own\n" + tools.SharedRecordingMarker + "\ntheirs"); got != "\ntheirs" {
		t.Errorf("everything after the marker: %q", got)
	}
}
