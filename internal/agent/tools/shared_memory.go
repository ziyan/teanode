package tools

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// SharedPageMarker begins a tool result that carries words from a page
// another person shared. The line is in the result the model reads and in
// the message the conversation stores, which is how the run that files what a
// conversation taught can tell shared words from the person's own: it refuses
// a fact whose only evidence is text after the marker, because shared
// knowledge is read where it lives and never copied.
const SharedPageMarker = "[from a page another person shared with you: cite it, never write it down]"

// SharedRecordingMarker begins a tool result that carries words from a
// recording another person shared, which are read where they live and never
// copied, like a shared page. The run that files what a conversation taught
// treats what follows it as it treats what follows SharedPageMarker.
const SharedRecordingMarker = "[from a recording another person shared with you: cite it, never write it down]"

// TextFromSharedPages is the part of a stored tool result that came from
// shared pages or shared recordings: everything from the first marker on, or
// empty when the result has none. A result that mixes the person's own pages
// with shared ones puts the shared ones last, after the marker.
func TextFromSharedPages(content string) string {
	start := -1
	for _, marker := range []string{SharedPageMarker, SharedRecordingMarker} {
		if at := strings.Index(content, marker); at >= 0 && (start < 0 || at+len(marker) < start) {
			start = at + len(marker)
		}
	}
	if start < 0 {
		return ""
	}
	return content[start:]
}

// SharedMemory is what a run offers the memory tool to read the pages other
// people shared with the person. A run that is not a conversation with the
// person, or that is a dream, does not implement it or answers
// CanReadSharedPages with false, and the tool reads the person's own graph
// alone.
//
// Every method reads through internal/agent/shared_memory.go, the only code
// that reads another agent's graph.
type SharedMemory interface {
	// CanReadSharedPages says whether this run may read them: the person
	// is present, the run is not a dream and not a chat of several people,
	// and they may use circles.
	CanReadSharedPages() bool

	// SharedPages are the roots shared with the person, by owner and path.
	SharedPages(ctx context.Context) ([]*models.SharedRoot, error)

	// ReadSharedPage is one page of one owner's graph as the person is
	// shown it, or nil when none is shared with them there.
	ReadSharedPage(ctx context.Context, ownerUserId, path string) (*models.SharedPage, error)

	// SearchSharedPages finds pages and facts of shared pages by words.
	SearchSharedPages(ctx context.Context, query string, limit int) ([]*models.SharedHit, error)

	// RestsOnSharedPages says whether a fact about to be noted repeats what
	// a shared page showed this conversation, and nothing the person said:
	// the same test the run that files a conversation applies afterwards,
	// applied to the memory tool's own note.
	RestsOnSharedPages(ctx context.Context, text string) bool
}

// SharedPageRefusal is what a note resting on a shared page is refused
// with, by the memory tool and by the run that files a conversation.
const SharedPageRefusal = "this came from a page another person shared; cite it instead of writing it down"
