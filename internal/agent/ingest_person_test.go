package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/models"
)

// A source that knows which posts are the person's but not their name
// marks them @you, and the server names them, so the night reads the
// conversation as one they were in.
func TestAPostMarkedYouIsThePersons(t *testing.T) {
	owner := &models.User{Username: "sam"}
	entry := computer.ScanEntry{
		Kind: "chat", Hash: "unchanged-hash",
		Text:     "Codex conversation in /work/garden-app\n\n10:00 @you: Why is the build slow?\n10:01 Codex: The cache is cleared.",
		Metadata: map[string]any{"participants": []any{"@you", "Codex"}},
	}
	namePerson(&entry, owner)
	if entry.Text != "Codex conversation in /work/garden-app\n\n10:00 sam: Why is the build slow?\n10:01 Codex: The cache is cleared." {
		t.Fatalf("the person's turns are theirs: %q", entry.Text)
	}
	if participants := entry.Metadata["participants"].([]any); participants[0] != "sam" || participants[1] != "Codex" {
		t.Fatalf("and they are among who was in it: %+v", participants)
	}
	if entry.Hash != "unchanged-hash" {
		t.Fatalf("the hash is the reader's, so the next pass still matches: %q", entry.Hash)
	}
}
