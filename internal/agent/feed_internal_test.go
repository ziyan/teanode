package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// What travels between instances fits a notification: the text is cut
// first, then the arguments, on a character boundary, with a mark.
func TestFitForRelayCutsWhatIsTooLong(t *testing.T) {
	long := strings.Repeat("é", 6000)
	event := fitForRelay(Event{Kind: EventToolResult, RunID: "run", Text: long, Arguments: strings.Repeat("a", 3000)})
	encoded, _ := json.Marshal(relayed{Instance: "here", Event: event})
	if len(encoded) > relayPayloadLimit {
		t.Fatalf("the payload is %d bytes, over %d", len(encoded), relayPayloadLimit)
	}
	if !strings.HasSuffix(event.Text, "…") || !strings.HasPrefix(event.Text, "éé") || strings.Contains(event.Text, "�") {
		t.Fatalf("the text is cut on a character with a mark: %q…", event.Text[:12])
	}
	if event.Arguments != strings.Repeat("a", 3000) {
		t.Fatal("the arguments were not too long once the text was cut")
	}
	short := fitForRelay(Event{Kind: EventText, Text: "hello"})
	if short.Text != "hello" {
		t.Fatalf("what fits is left alone: %q", short.Text)
	}
}
