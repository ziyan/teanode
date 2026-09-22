package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type dreamAuditKey struct{}

type blockedDreamDatabase struct {
	db.Database
	test      *testing.T
	hasWaited bool
}

func (self *blockedDreamDatabase) TransactionContext(ctx context.Context, apply func(db.Transaction) error) error {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || time.Until(deadline) > jobCompletionTimeout {
		self.test.Error("dream bookkeeping can wait without a completion deadline")
		return context.DeadlineExceeded
	}
	if ctx.Err() != nil || ctx.Value(dreamAuditKey{}) != "fixture-audit" {
		self.test.Error("bookkeeping lost its audit context or inherited cancellation")
		return context.Canceled
	}
	// Simulate waiting for a connection before the transaction callback starts.
	<-ctx.Done()
	self.hasWaited = true
	return ctx.Err()
}

func TestDreamBookkeepingBoundsAStalledDatabase(test *testing.T) {
	for _, operation := range []string{"read", "garbled", "progress", "decline", "picture"} {
		test.Run(operation, func(test *testing.T) {
			test.Parallel()
			database := &blockedDreamDatabase{test: test}
			run := &Run{settings: &Settings{Database: database}}
			ctx, cancel := context.WithCancel(context.WithValue(test.Context(), dreamAuditKey{}, "fixture-audit"))
			cancel()
			startedAt := time.Now()
			switch operation {
			case "read":
				markRead(ctx, run, []*models.AgentDocument{{ID: "fixture-document"}})
			case "garbled":
				if (&Agent{}).givingUpOn(ctx, run, []*models.AgentDocument{{ID: "fixture-document"}}) {
					test.Error("failed bookkeeping let the dream abandon an unread document")
				}
			case "decline":
				declineAttachments(ctx, run, []string{"fixture-document"}, "fixture decision")
			case "picture":
				if err := keepDreamPicture(ctx, run, &models.AgentDocument{}, "fixture description"); !errors.Is(err, context.DeadlineExceeded) {
					test.Errorf("picture error = %v", err)
				}
			case "progress":
				if completeDigestWithoutFacts(ctx, run, nil, func(db.Transaction, []*models.AgentDocument, int) error { return nil }) {
					test.Error("failed progress write completed the batch")
				}
			}
			if !database.hasWaited {
				test.Error("bookkeeping did not wait for database acquisition to time out")
			}
			if elapsed := time.Since(startedAt); elapsed > jobCompletionTimeout+5*time.Second {
				test.Errorf("bookkeeping took %s", elapsed)
			}
		})
	}
}

func TestDreamBookkeepingPersistsAfterCancellation(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	var document *models.AgentDocument
	var record *models.AgentDream
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		document, err = transaction.PutAgentDocument(&models.AgentDocument{AgentID: source.AgentID, SourceID: source.ID, ExternalID: "fixture-document", Kind: models.DocumentFile, Title: "Fixture", Hash: "fixture-hash"})
		if err != nil {
			test.Fatal(err)
		}
		record, err = transaction.StartAgentDream(&models.AgentDream{AgentID: source.AgentID, StartedAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
	})
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	documents := []*models.AgentDocument{document}
	if worker.givingUpOn(ctx, run, documents) {
		test.Fatal("first malformed answer abandoned the document")
	}
	if !worker.givingUpOn(ctx, run, documents) {
		test.Fatal("second malformed answer was not recorded")
	}
	declineAttachments(ctx, run, []string{document.ID}, "fixture decision")
	if err := keepDreamPicture(ctx, run, document, "Fixture picture description."); err != nil {
		test.Fatal(err)
	}
	var mutex sync.Mutex
	if !completeDigestWithoutFacts(ctx, run, documents, dreamDigestCompletion(record, &dreamBudget{}, &mutex)) {
		test.Fatal("completed batch did not persist after cancellation")
	}
	if readCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document WHERE metadata ? 'digested'`); readCount != "1" {
		test.Fatalf("read documents = %s", readCount)
	}
	if chunkCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_chunk`); chunkCount != "1" {
		test.Fatalf("picture chunks = %s", chunkCount)
	}
	if declinedCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document WHERE metadata ? 'declined'`); declinedCount != "1" {
		test.Fatalf("declined documents = %s", declinedCount)
	}
	if digestedCount := dbtest.QueryString(test, database, `SELECT digested::text FROM agent_dream`); digestedCount != "1" {
		test.Fatalf("dream progress = %s", digestedCount)
	}
}
