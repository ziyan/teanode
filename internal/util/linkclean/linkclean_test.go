package linkclean_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/util/linkclean"
)

type sharedCase struct {
	Input   string `json:"input"`
	Cleaned string `json:"cleaned"`
	Display string `json:"display"`
}

// The same cases are read by the dashboard's and the iPhone app's tests, so
// the three implementations answer alike.
func TestSharedCases(t *testing.T) {
	content, err := os.ReadFile("testdata/links.json")
	if err != nil {
		t.Fatalf("cannot read the shared cases: %s", err)
	}
	var cases []sharedCase
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatalf("cannot parse the shared cases: %s", err)
	}
	if len(cases) == 0 {
		t.Fatal("no shared cases")
	}
	for _, found := range cases {
		if cleaned := linkclean.Clean(found.Input); cleaned != found.Cleaned {
			t.Errorf("Clean(%q) = %q, want %q", found.Input, cleaned, found.Cleaned)
		}
		if display := linkclean.DisplayText(found.Input); display != found.Display {
			t.Errorf("DisplayText(%q) = %q, want %q", found.Input, display, found.Display)
		}
	}
}

func TestDisplayTextIsNeverLongerThanTheLimit(t *testing.T) {
	long := "https://example.com/aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffffgggggggggg"
	display := linkclean.DisplayText(long)
	if count := len([]rune(display)); count != linkclean.DisplayLength {
		t.Errorf("DisplayText cut to %d characters (%q), want %d", count, display, linkclean.DisplayLength)
	}
}
