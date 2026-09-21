package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A batch the night could not make sense of is counted, not lost.
//
// An answer that will not parse used to mark its documents read anyway, on
// the reasoning that a batch which blocks blocks every night after it.
// That much is true. What it left out is that the batch is then gone --
// read, by a night that read nothing of it, with nothing anywhere saying
// so and nothing to find it by.
func TestADocumentTheNightCouldNotReadIsCountedAndFindable(test *testing.T) {
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
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: person.ID, Name: "notes", Kind: models.SourceComputer,
			Specification: models.AgentKnowledgeSpecification{Path: "/notes", Computer: "gen7"},
		})
		if err != nil {
			test.Fatalf("PutAgentSource: %s", err)
		}
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: person.ID, SourceID: source.ID, ExternalID: "one",
			Kind: models.DocumentFile, Title: "one.txt", Bytes: 900,
		})
		if err != nil {
			test.Fatalf("PutAgentDocument: %s", err)
		}

		// Nothing has gone wrong with it yet.
		given, err := tx.AgentDocumentsGivenUpOn([]string{document.ID})
		if err != nil {
			test.Fatalf("AgentDocumentsGivenUpOn: %s", err)
		}
		if given[document.ID] != 0 {
			test.Fatalf("a document nobody has failed on is at 0, not %d", given[document.ID])
		}

		// One night that could not read the answer.
		if err := tx.MarkAgentDocumentsGarbled([]string{document.ID}, time.Now()); err != nil {
			test.Fatalf("MarkAgentDocumentsGarbled: %s", err)
		}
		if given, err = tx.AgentDocumentsGivenUpOn([]string{document.ID}); err != nil {
			test.Fatalf("AgentDocumentsGivenUpOn: %s", err)
		}
		if given[document.ID] != 1 {
			test.Fatalf("one failure is one, not %d", given[document.ID])
		}

		// And a second, which is what tells the night to stop trying.
		if err := tx.MarkAgentDocumentsGarbled([]string{document.ID}, time.Now()); err != nil {
			test.Fatalf("MarkAgentDocumentsGarbled: %s", err)
		}
		if given, err = tx.AgentDocumentsGivenUpOn([]string{document.ID}); err != nil {
			test.Fatalf("AgentDocumentsGivenUpOn: %s", err)
		}
		if given[document.ID] != 2 {
			test.Fatalf("two failures are two, not %d -- the count has to grow or nothing ever stops", given[document.ID])
		}

		// And none of that marked it read, which is the other half: the
		// count says what happened, and something else decides.
		read, err := tx.CountAgentDocumentsReading(person.ID, nil)
		if err != nil {
			test.Fatalf("CountAgentDocumentsReading: %s", err)
		}
		if read.Read != 0 || read.Waiting != 1 {
			test.Fatalf("counting a failure is not reading it: %d read, %d waiting", read.Read, read.Waiting)
		}
	})
}
