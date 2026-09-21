package decide

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Against the real service, when a key is there for it.
//
// Skipped without one, so the suite is the same everywhere; run it by
// pointing TEANODE_TEST_DECIDE_KEY at a key.
func TestAgainstTheRealServiceWhenThereIsAKey(test *testing.T) {
	key := strings.TrimSpace(os.Getenv("TEANODE_TEST_DECIDE_KEY"))
	if key == "" {
		test.Skip("no TEANODE_TEST_DECIDE_KEY")
	}
	client, err := New("https://api.typesafe.ai/v1", key, "jev-latest", 20*time.Second)
	if err != nil {
		test.Fatalf("New: %s", err)
	}
	state := "file: Screenshot 2024-03-11 at 14.22.08.png (412 KB, image/png)\n" +
		"channel: #deploys, thread \"staging is 500ing\"\n" +
		"said with it: \"this is what I get when I hit the endpoint\""
	began := time.Now()
	answers, err := client.Ask(context.Background(), state, map[string]Question{
		"worth_opening": {
			Instructions: "Is this picture worth sending to a model to be described?",
			Choices: map[string]string{
				"true":  "It likely holds words or detail nobody wrote down: an error, a log, a terminal, a diagram, a document",
				"false": "It is decoration, a photograph of people or a place, a logo, or furniture",
			},
		},
		"kind": {
			Instructions: "What kind of picture is it?",
			Choices: map[string]string{
				"screenshot": "A screen, a terminal, a log, an error",
				"document":   "A scan or photograph of a written document",
				"diagram":    "A drawing, chart or diagram",
				"photo":      "A photograph of people, a place or a thing",
			},
		},
	})
	if err != nil {
		test.Fatalf("Ask: %s", err)
	}
	test.Logf("answered in %v", time.Since(began).Round(time.Millisecond))
	for name, answer := range answers {
		test.Logf("  %-14s yes=%.2f choice=%-10s confidence=%.2f", name, answer.Yes, answer.Choice, answer.Confidence)
	}
	// A yes-or-no carries its certainty in Yes and leaves Confidence at
	// zero, which is why this asks about Yes.
	if answers["worth_opening"].Confidence != 0 {
		test.Errorf("a yes-or-no came back with a confidence of %v", answers["worth_opening"].Confidence)
	}
	if answers["worth_opening"].Yes < 0.5 {
		test.Errorf("a screenshot of an error was judged not worth opening: %v", answers["worth_opening"].Yes)
	}
	if answers["kind"].Choice != "screenshot" {
		test.Errorf("a screenshot was called %q", answers["kind"].Choice)
	}
}
