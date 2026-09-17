package computer

import (
	"strings"
	"testing"
	"time"
)

// momentOf is a post's time, written the way a test reads it.
func momentOf(clock string) time.Time {
	when, err := time.Parse(time.RFC3339, clock)
	if err != nil {
		panic(err)
	}
	return when
}

// A chat is cut into threads and windows, not posts: two million posts
// would be two million vectors of "ok" and "thanks". This is the part
// that belongs to no chat app, so the posts are handed over directly
// rather than read out of anybody's export.
func TestChatIsCutIntoThreadsAndWindows(t *testing.T) {
	// A thread (a root with two replies), then two posts a day apart,
	// which are two windows.
	posts := []chatPost{
		{ID: "p1", Replied: true, At: momentOf("2026-08-14T09:00:00Z"), Author: "alice", Text: "Why is the queue backing up?"},
		{ID: "p2", Thread: "p1", At: momentOf("2026-08-14T09:01:00Z"), Author: "bob", Text: "The consumer died."},
		{ID: "p3", Thread: "p1", At: momentOf("2026-08-14T09:02:00Z"), Author: "alice", Text: "Restarted it."},
		{ID: "p4", At: momentOf("2026-08-15T09:00:00Z"), Author: "alice", Text: "Standalone one."},
		{ID: "p5", At: momentOf("2026-08-16T09:00:00Z"), Author: "bob", Text: "Much later one."},
	}

	entries := chatUnits("posts/engineering/backend.jsonl", "backend", posts, false)
	if len(entries) != 3 {
		t.Fatalf("a thread and two windows: %d entries %+v", len(entries), entries)
	}
	var thread ScanEntry
	for _, entry := range entries {
		if strings.Contains(entry.Text, "queue backing up") {
			thread = entry
		}
	}
	if thread.ExternalID != "posts/engineering/backend.jsonl#p1" {
		t.Fatalf("a thread is filed under its root: %q", thread.ExternalID)
	}
	if !strings.Contains(thread.Text, "The consumer died.") || !strings.Contains(thread.Text, "Restarted it.") {
		t.Fatalf("a thread is its root and its replies: %q", thread.Text)
	}
	if !strings.Contains(thread.Text, "alice:") || !strings.Contains(thread.Text, "bob:") {
		t.Fatalf("with who said what: %q", thread.Text)
	}
	participants, _ := thread.Metadata["participants"].([]string)
	if len(participants) != 2 {
		t.Fatalf("and who was in it: %+v", thread.Metadata)
	}
	if thread.Metadata["posts"] != 3 || thread.Metadata["channel"] != "backend" {
		t.Fatalf("and where and how much: %+v", thread.Metadata)
	}
	if thread.HappenedAt == nil || !thread.HappenedAt.Equal(momentOf("2026-08-14T09:00:00Z")) {
		t.Fatalf("a unit happened when its first post did: %+v", thread.HappenedAt)
	}
}

// A silence ends a window, so an exchange in the morning and another in
// the afternoon are two units rather than one long one.
func TestASilenceEndsAWindow(t *testing.T) {
	posts := []chatPost{
		{ID: "d1", At: momentOf("2026-08-14T10:00:00Z"), Author: "carol", Text: "The new header is up."},
		{ID: "d2", At: momentOf("2026-08-14T10:05:00Z"), Author: "dave", Text: "It is too tall on a phone."},
		{ID: "d3", At: momentOf("2026-08-14T12:05:00Z"), Author: "carol", Text: "Cut it to forty eight."},
		{ID: "d4", At: momentOf("2026-08-14T12:10:00Z"), Author: "dave", Text: "Much better."},
	}

	entries := chatUnits("chat.jsonl", "design", posts, false)
	if len(entries) != 2 {
		t.Fatalf("two windows either side of the silence: %+v", entries)
	}
	for _, entry := range entries {
		if entry.Metadata["posts"] != 2 {
			t.Fatalf("each window is the two posts either side of it: %+v", entry.Metadata)
		}
	}
}

// A conversation only the person can see is read -- it is their own
// archive on their own machine -- and marked, so a citation can say
// where it came from.
func TestAPrivateConversationIsMarked(t *testing.T) {
	posts := []chatPost{
		{ID: "p1", At: momentOf("2026-08-14T09:00:00Z"), Author: "alice", Text: "We are raising in March."},
	}

	entries := chatUnits("chat.jsonl", "founders", posts, true)
	if len(entries) != 1 {
		t.Fatalf("it is read: %+v", entries)
	}
	if !entries[0].Private {
		t.Fatalf("and marked private: %+v", entries[0])
	}
	if entries[0].Text == "" {
		t.Fatalf("with its words: %+v", entries[0])
	}
}
