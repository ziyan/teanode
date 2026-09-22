package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Committed progress survives a restart without finishing the dream.
func TestADreamWritesDownWhatItHasReadBeforeItIsOver(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "fixture-owner", Name: "Fixture Owner"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		person, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Fixture"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}

		dream, err := tx.StartAgentDream(&models.AgentDream{AgentID: person.ID, StartedAt: time.Now()})
		if err != nil {
			test.Fatalf("StartAgentDream: %s", err)
		}

		dream.Backlog = 10
		dream.Tokens = 200
		if err := tx.AdvanceAgentDreamProgress(dream, 2, 1); err != nil {
			test.Fatal(err)
		}
		// An older token snapshot must not lower already committed usage.
		dream.Tokens = 100
		if err := tx.AdvanceAgentDreamProgress(dream, 1, 1); err != nil {
			test.Fatal(err)
		}

		dreams, err := tx.ListAgentDreams(person.ID, 10)
		if err != nil || len(dreams) != 1 {
			test.Fatalf("ListAgentDreams: %v %s", dreams, err)
		}
		written := dreams[0]
		if written.Digested != 3 || written.Filed != 2 {
			test.Fatalf("what it had read is on the row: %d read, %d filed", written.Digested, written.Filed)
		}
		if written.Tokens != 200 {
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
