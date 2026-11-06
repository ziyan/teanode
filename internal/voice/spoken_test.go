package voice

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSpokenTextIsWhatWouldBeSaid(t *testing.T) {
	for written, want := range map[string]string{
		"**Leave at 9** and take the [scenic road](https://example.com/road).": "Leave at 9 and take the scenic road.",
		"### Arrival\n\n- Park in front\n- Lock the car":                       "Arrival\n\nPark in front\nLock the car",
		"See https://example.com/a?b=c for more.":                              "See for more.",
		"1. First\n2. Second":                                                  "First\nSecond",
		"| Day | High |\n| --- | --- |\n| Saturday | 15 |":                     "Day, High\nSaturday, 15",
		"Run `make test` now":                                                  "Run make test now",
		"> quoted words":                                                       "quoted words",
		"before\n\n---\n\nafter":                                               "before\n\nafter",
		"It is *really* good, and 2 * 3 is 6.":                                 "It is really good, and 2 * 3 is 6.",
		"![a cabin](https://example.com/c.png)":                                "a cabin",
		"```\nsome code\n```":                                                  "some code",
		"":                                                                     "",
	} {
		if got := SpokenText(written); got != want {
			t.Errorf("%q: %q, want %q", written, got, want)
		}
	}
}

func TestSpokenPiecesStartShortAndLoseNothing(t *testing.T) {
	sentence := "This sentence is about as long as an ordinary sentence in an answer. "
	text := strings.Repeat(sentence, 80)
	pieces := SpokenPieces(text)
	if len(pieces) < 3 {
		t.Fatalf("%d pieces", len(pieces))
	}
	if len(pieces[0]) > firstPieceBytes {
		t.Fatalf("the first piece is %d bytes", len(pieces[0]))
	}
	for index, piece := range pieces {
		if len(piece) > pieceBytes || len(piece) > MaximumAnswerTextLength {
			t.Errorf("piece %d is %d bytes", index, len(piece))
		}
	}
	if got := strings.Join(pieces, " "); got != strings.TrimSpace(text) {
		t.Fatalf("words were lost:\n%q", got)
	}
}

// Chinese and Japanese end sentences with marks of their own, and a run with
// no punctuation at all is still cut between characters.
func TestSpokenPiecesCutOtherScripts(t *testing.T) {
	if pieces := SpokenPieces(strings.Repeat("今天天气很好。", 60)); len(pieces) < 2 {
		t.Fatalf("%d pieces", len(pieces))
	}
	for _, piece := range SpokenPieces(strings.Repeat("字", 2000)) {
		if !utf8.ValidString(piece) || len(piece) > pieceBytes {
			t.Fatalf("a piece of %d bytes, valid %v", len(piece), utf8.ValidString(piece))
		}
	}
}

func TestSpokenPiecesOfNothingAreNone(t *testing.T) {
	for _, text := range []string{"", "   ", "https://example.com/only-a-link", "---"} {
		if pieces := SpokenPieces(text); len(pieces) != 0 {
			t.Errorf("%q: %q", text, pieces)
		}
	}
}
