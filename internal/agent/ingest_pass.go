package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// walksAWholeTree says whether a source's pass ends by having seen
// everything the source holds.
//
// Only a computer or an archive does: its pages walk a tree from one end
// to the other, so what it did not name it no longer has. Sent mail
// walks backwards through time and stops when the run is over, and a
// source read that way must never have anything taken from it.
func walksAWholeTree(source *models.AgentKnowledgeSource) bool {
	return source.Kind == models.SourceComputer || source.Kind == models.SourceArchive
}

// markPassStart notes when this pass over the tree began, and answers
// it; the zero time means this pass may not delete anything.
//
// A pass is many jobs long, so the time is kept in the cursor, which is
// written down after every page. Only a pass that starts at the top of
// the tree gets one: one that resumes mid-tree keeps what its first page
// wrote, and one that was already mid-tree when this was built -- or on
// the upgrade that added it -- gets none, and leaves the sweeping to the
// next pass, which will start at the top.
func markPassStart(source *models.AgentKnowledgeSource, cursor map[string]any, now time.Time) time.Time {
	if !walksAWholeTree(source) {
		return time.Time{}
	}
	if said, ok := cursor[cursorPassStarted].(string); ok && said != "" {
		started, err := time.Parse(time.RFC3339, said)
		if err != nil {
			log.Warningf("source %q says its pass began at %q, which is not a time", source.ID, said)
			return time.Time{}
		}
		return started
	}
	if after, _ := cursor["after"].(string); after != "" {
		return time.Time{}
	}
	// To the second, and so a little earlier than the pass really began:
	// what that costs is that a document last seen within the same second
	// survives one more pass, and what it buys is that nothing filed in
	// that second is mistaken for something the pass did not see.
	started := now.Truncate(time.Second)
	cursor[cursorPassStarted] = started.Format(time.RFC3339)
	cursor[cursorPassSeen] = 0
	cursor[cursorPassRefused] = 0
	return started
}

// sweepUnseen removes what the source no longer has, now that a pass has
// walked its tree to the end.
//
// Until this, nothing ever took a document away. A file deleted from a
// checkout, a page deleted from a wiki, a chat export converted to
// records under new names: the row stayed, its passages stayed, and both
// went on being searched and dreamed over. Every entry a pass is shown
// -- filed, unchanged, or refused, because a thing the source holds and
// cannot read is still a thing it holds -- has its seen time written; so
// what is still older than the time this pass began is what the source
// stopped reporting.
func (self *Agent) sweepUnseen(ctx context.Context, source *models.AgentKnowledgeSource, cursor map[string]any, startedPass time.Time, counts *db.SourceCounts) {
	// However this ends, the next pass over this source starts its own.
	defer func() {
		delete(cursor, cursorPassStarted)
		delete(cursor, cursorPassSeen)
		delete(cursor, cursorPassRefused)
	}()
	if startedPass.IsZero() {
		return
	}
	// What this pass refused is what the row says, not every pass added
	// together.
	if _, kept := cursor[cursorPassRefused]; kept {
		counts.Refused = countInCursor(cursor, cursorPassRefused)
	}
	// A pass shown nothing at all is not somebody deleting everything
	// they own. It is a folder nothing mounted, or a checkout moved, and
	// the answer to either is to wait for the next pass rather than to
	// empty the source.
	if seen := countInCursor(cursor, cursorPassSeen); seen <= 0 {
		log.Debugf("source %q reached the end of its tree having been shown nothing; leaving what it holds alone", source.ID)
		return
	}
	removed := 0
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if err := lockIngestSource(tx, source); err != nil {
			return err
		}
		removed, err = tx.DeleteAgentDocumentsUnseen(source.ID, startedPass)
		return err
	}); err != nil {
		log.Warningf("cannot remove what source %q no longer holds: %s", source.ID, err)
		return
	}
	if removed == 0 {
		return
	}
	// Off the source's own count, which is what the person is shown, the
	// same way the documents this pass filed went on to it.
	counts.Documents -= removed
	if counts.Documents < 0 {
		counts.Documents = 0
	}
	log.Infof("source %q no longer has %d document(s); removed them with their passages", source.ID, removed)
}

// partWayThroughTree says whether the cursor stopped in the middle of a
// tree: a pass with pages read and the end not reached yet.
//
// Type-asserted rather than compared against "". The cursor comes back
// from the database as JSON, so a key no page ever wrote is nil, and nil
// is not the empty string: every source looked mid-tree, so one whose
// computer had gone away was made due again in fifteen seconds for ever,
// whether or not it had anything to resume.
func partWayThroughTree(cursor map[string]any) bool {
	after, _ := cursor["after"].(string)
	before, _ := cursor["before"].(string)
	return after != "" || before != ""
}

// countInCursor is a number the cursor is keeping. It comes back from
// the database as JSON, so what was written as an int is read as a
// float.
func countInCursor(cursor map[string]any, key string) int {
	switch value := cursor[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return 0
}
