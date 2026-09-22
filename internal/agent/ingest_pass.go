package agent

import (
	"context"
	"maps"
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

type ingestCompletion struct {
	Cursor map[string]any
	Counts db.SourceCounts
}

func shouldSweepIngestPass(cursor map[string]any, startedPass, now time.Time) bool {
	return !startedPass.IsZero() && !startedPass.After(now) && countInCursor(cursor, cursorPassSeen) > 0
}

// completeIngestPass commits sweeping, counts and cursor reset together.
// Its returned cursor is usable only after the transaction succeeds.
func (self *Agent) completeIngestPass(ctx context.Context, source *models.AgentKnowledgeSource, cursor map[string]any, startedPass time.Time, counts db.SourceCounts) (*ingestCompletion, error) {
	completion := &ingestCompletion{Cursor: maps.Clone(cursor), Counts: counts}
	err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := lockIngestSource(transaction, source); err != nil {
			return err
		}
		if walksAWholeTree(source) && shouldSweepIngestPass(cursor, startedPass, time.Now()) {
			if _, err := transaction.DeleteAgentDocumentsUnseen(source.ID, startedPass); err != nil {
				return err
			}
		}
		if _, exists := cursor[cursorPassRefused]; exists && !startedPass.IsZero() {
			completion.Counts.Refused = countInCursor(cursor, cursorPassRefused)
		}
		documents, chunks, err := transaction.CountAgentSourceDocuments(source.ID)
		if err != nil {
			return err
		}
		completion.Counts.Documents, completion.Counts.Chunks = documents, chunks
		for _, key := range []string{"after", "before", cursorKnownID, cursorKnownSent, cursorPassStarted, cursorPassSeen, cursorPassRefused} {
			delete(completion.Cursor, key)
		}
		// A crash before embedding/final bookkeeping must still leave the source due.
		nextRun := time.Now().Add(ingestAgain)
		return transaction.MarkAgentSourceRun(source.ID, completion.Cursor, completion.Counts, true, "", &nextRun)
	})
	if err != nil {
		return nil, err
	}
	return completion, nil
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
