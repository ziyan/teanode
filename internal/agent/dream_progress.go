package agent

import (
	"context"

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

func noteDreamProgress(ctx context.Context, run *Run, progress *models.AgentDream) error {
	return dreamBookkeeping(ctx, run, func(transaction db.Transaction) error {
		return transaction.NoteAgentDreamProgress(progress)
	})
}

func keepDreamPicture(ctx context.Context, run *Run, document *models.AgentDocument, description string) error {
	return dreamBookkeeping(ctx, run, func(transaction db.Transaction) error {
		return transaction.ReplaceAgentChunks(document, chunkText(description))
	})
}
