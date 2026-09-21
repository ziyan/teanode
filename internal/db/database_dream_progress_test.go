package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A night the server restarts under still says what it read.
//
// A dream counted what it read and filed in memory and wrote the count
// once, at the end. A restart therefore lost it entirely, and the row the
// person sees said "Cut short" and nothing else -- while the documents
// that night read were marked read and the facts it filed were on their
// pages. The work survived; only the account of it did not.
func TestADreamWritesDownWhatItHasReadBeforeItIsOver(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		person, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}

		dream, err := tx.StartAgentDream(&models.AgentDream{AgentID: person.ID, StartedAt: time.Now()})
		if err != nil {
			test.Fatalf("StartAgentDream: %s", err)
		}

		// Two batches in, and the server is about to go.
		dream.Digested = 240
		dream.Filed = 31
		dream.Backlog = 150000
		dream.Tokens = 412000
		if err := tx.NoteAgentDreamProgress(dream); err != nil {
			test.Fatalf("NoteAgentDreamProgress: %s", err)
		}

		dreams, err := tx.ListAgentDreams(person.ID, 10)
		if err != nil || len(dreams) != 1 {
			test.Fatalf("ListAgentDreams: %v %s", dreams, err)
		}
		written := dreams[0]
		if written.Digested != 240 || written.Filed != 31 {
			test.Fatalf("what it had read is on the row: %d read, %d filed", written.Digested, written.Filed)
		}
		if written.Tokens != 412000 {
			test.Fatalf("and what it had spent: %d", written.Tokens)
		}
		// And it is not over. A progress note that finished the dream
		// would end the night from the inside, and the phases after the
		// reading would file their work against a dream already closed.
		if written.FinishedAt != nil {
			test.Fatalf("the dream is still going, not finished at %v", written.FinishedAt)
		}
	})
}
