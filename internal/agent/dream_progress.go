package agent

import (
	"context"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Completed model work may be recorded after cancellation, but a stalled
// database must not keep a dream worker alive indefinitely during shutdown.
func dreamBookkeeping(ctx context.Context, run *Run, apply func(db.Transaction) error) error {
	completionContext, cancelCompletion := context.WithTimeout(context.WithoutCancel(ctx), jobCompletionTimeout)
	defer cancelCompletion()
	return run.Database().TransactionContext(completionContext, apply)
}

func keepDreamPicture(ctx context.Context, run *Run, document *models.AgentDocument, description string) error {
	return dreamBookkeeping(ctx, run, func(transaction db.Transaction) error {
		return transaction.ReplaceAgentChunks(document, chunkText(description))
	})
}

type digestCompletion func(db.Transaction, []*models.AgentDocument, int) error

func dreamDigestCompletion(record *models.AgentDream, budget *dreamBudget, mutex *sync.Mutex) digestCompletion {
	return func(transaction db.Transaction, documents []*models.AgentDocument, filedCount int) error {
		documentIds := make([]string, 0, len(documents))
		for _, document := range documents {
			documentIds = append(documentIds, document.ID)
		}
		if err := transaction.MarkAgentDocumentsDigested(documentIds, time.Now()); err != nil {
			return err
		}
		progress := &models.AgentDream{ID: record.ID, AgentID: record.AgentID, Backlog: record.Backlog, Tokens: budget.spentSoFar()}
		if err := transaction.AdvanceAgentDreamProgress(progress, len(documents), filedCount); err != nil {
			return err
		}
		transaction.AfterCommit(func() {
			mutex.Lock()
			defer mutex.Unlock()
			record.Digested += len(documents)
			record.Filed += filedCount
		})
		return nil
	}
}

func completeDigestWithoutFacts(ctx context.Context, run *Run, documents []*models.AgentDocument, complete digestCompletion) bool {
	if complete == nil {
		return true
	}
	if err := dreamBookkeeping(ctx, run, func(transaction db.Transaction) error {
		return complete(transaction, documents, 0)
	}); err != nil {
		log.Warningf("cannot record a completed dream batch: %s", err)
		return false
	}
	return true
}
