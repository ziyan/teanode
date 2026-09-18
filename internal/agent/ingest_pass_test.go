package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A pass over a tree writes down when it began, on its first page and
// only there: every page after it reads back what that one wrote, because
// the time the sweep at the end compares against has to be the time the
// walk started and not the time it finished.
func TestAPassWritesDownWhenItBegan(t *testing.T) {
	source := &models.AgentKnowledgeSource{ID: "one", Kind: models.SourceArchive}
	cursor := map[string]any{}

	began := time.Now()
	started := markPassStart(source, cursor, began)
	if started.IsZero() || started.After(began) {
		t.Fatalf("the first page of a pass began at %v, not %v", started, began)
	}
	if cursor[cursorPassStarted] != started.Format(time.RFC3339) {
		t.Fatalf("the cursor says the pass began at %v", cursor[cursorPassStarted])
	}

	// The second page, an hour later and halfway down the tree.
	cursor["after"] = "posts/dev/general.jsonl"
	if again := markPassStart(source, cursor, began.Add(time.Hour)); !again.Equal(started) {
		t.Fatalf("the second page moved the pass's start to %v", again)
	}

	// And the same after the cursor has been through the database, where
	// it is JSON and nothing keeps its Go type.
	stored := map[string]any{cursorPassStarted: cursor[cursorPassStarted], "after": "posts/dev/general.jsonl"}
	if again := markPassStart(source, stored, began.Add(2*time.Hour)); !again.Equal(started) {
		t.Fatalf("a cursor read back from the database began at %v", again)
	}
}

// A pass that was already halfway down the tree when nothing had written
// a start -- the pass running across the upgrade that added this -- takes
// nothing, and leaves the sweeping to the next one, which starts at the
// top.
func TestAPassAlreadyHalfwayDownTakesNothing(t *testing.T) {
	source := &models.AgentKnowledgeSource{ID: "one", Kind: models.SourceComputer}
	cursor := map[string]any{"after": "src/main.go"}
	if started := markPassStart(source, cursor, time.Now()); !started.IsZero() {
		t.Fatalf("a pass resumed mid-tree with nothing written down began at %v", started)
	}
	if _, written := cursor[cursorPassStarted]; written {
		t.Fatalf("it wrote a start down anyway")
	}
}

// Only a source whose pass walks a whole tree may have anything taken
// from it. Sent mail walks backwards through time and stops when the run
// is over, so what it did not name this time it may simply not have
// reached.
func TestOnlyAWholeTreeMayLoseDocuments(t *testing.T) {
	for _, kind := range []models.AgentKnowledgeKind{models.SourceComputer, models.SourceArchive} {
		cursor := map[string]any{}
		if started := markPassStart(&models.AgentKnowledgeSource{ID: "one", Kind: kind}, cursor, time.Now()); started.IsZero() {
			t.Fatalf("a %s source walks its whole tree", kind)
		}
	}
	for _, kind := range []models.AgentKnowledgeKind{models.SourceSent, models.SourceWeb, models.SourceSkill} {
		cursor := map[string]any{}
		if started := markPassStart(&models.AgentKnowledgeSource{ID: "one", Kind: kind}, cursor, time.Now()); !started.IsZero() {
			t.Fatalf("a %s source does not walk a whole tree and may not lose anything", kind)
		}
		if _, written := cursor[cursorPassStarted]; written {
			t.Fatalf("a %s source wrote a pass start down", kind)
		}
	}
}

// A pass shown nothing at all takes nothing: an empty answer is a folder
// nothing mounted, not somebody deleting everything they own. Either way
// the pass's marks are cleared, so the next one starts its own.
func TestAPassShownNothingTakesNothing(t *testing.T) {
	agent := &Agent{}
	source := &models.AgentKnowledgeSource{ID: "one", Kind: models.SourceArchive}

	// Nothing seen: the sweep answers before it ever reaches a database,
	// which is what this agent without one proves.
	cursor := map[string]any{cursorPassStarted: "2026-09-16T00:00:00Z", cursorPassSeen: float64(0)}
	counts := db.SourceCounts{Documents: 7}
	agent.sweepUnseen(context.Background(), source, cursor, time.Now(), &counts)
	if counts.Documents != 7 {
		t.Fatalf("a pass shown nothing changed the count to %d", counts.Documents)
	}
	for _, key := range []string{cursorPassStarted, cursorPassSeen} {
		if _, left := cursor[key]; left {
			t.Fatalf("%q was left on the cursor", key)
		}
	}

	// And a pass with no start -- one resumed mid-tree across an upgrade
	// -- takes nothing however much it saw.
	cursor = map[string]any{cursorPassSeen: float64(4000)}
	agent.sweepUnseen(context.Background(), source, cursor, time.Time{}, &counts)
	if counts.Documents != 7 {
		t.Fatalf("a pass with no start changed the count to %d", counts.Documents)
	}
}

// What the cursor is keeping comes back from the database as JSON, so a
// number written as an int is read as a float.
func TestACountSurvivesTheCursor(t *testing.T) {
	if count := countInCursor(map[string]any{cursorPassSeen: 12}, cursorPassSeen); count != 12 {
		t.Fatalf("written this run: %d", count)
	}
	if count := countInCursor(map[string]any{cursorPassSeen: float64(12)}, cursorPassSeen); count != 12 {
		t.Fatalf("read back from the database: %d", count)
	}
	if count := countInCursor(map[string]any{cursorPassSeen: "twelve"}, cursorPassSeen); count != 0 {
		t.Fatalf("something that is not a number: %d", count)
	}
	if count := countInCursor(map[string]any{}, cursorPassSeen); count != 0 {
		t.Fatalf("nothing written down at all: %d", count)
	}
}

// A cursor that has never been written is not mid-tree.
//
// It used to be compared against the empty string, and a map value that
// nothing wrote is nil rather than "": every source whose computer was
// detached was told it had a tree half read, kept "there is more" on its
// row, and came round again every fifteen seconds until the computer was
// back -- for a laptop that is closed for the weekend, for days.
func TestACursorWithNothingInItIsNotMidTree(t *testing.T) {
	if partWayThroughTree(map[string]any{}) {
		t.Fatalf("a source that has read nothing is at the beginning, not the middle")
	}
	if partWayThroughTree(map[string]any{cursorPassSeen: 12}) {
		t.Fatalf("what a pass counted says nothing about where it got to")
	}
	if partWayThroughTree(map[string]any{"after": ""}) {
		t.Fatalf("a cursor cleared at the end of the tree is at the beginning again")
	}
	if !partWayThroughTree(map[string]any{"after": "posts/dev/general.jsonl"}) {
		t.Fatalf("a files source that stopped part way down has more to read")
	}
	if !partWayThroughTree(map[string]any{"before": "2026-01-01T00:00:00Z"}) {
		t.Fatalf("and so does a sent source paging back through the years")
	}
}
