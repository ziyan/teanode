package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func (self *Agent) reconcileRecordingReferences(ctx context.Context, now time.Time) {
	if self.settings.Database == nil || now.Sub(self.lastRecordingReferenceReconciliation) < time.Minute {
		return
	}
	self.lastRecordingReferenceReconciliation = now
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var sources []*models.Recording
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		sources, err = transaction.ListRecordingsAwaitingReferenceReconciliation(25)
		return err
	}); err != nil {
		log.Warningf("cannot find recording references awaiting reconciliation: %s", err)
		return
	}
	for _, source := range sources {
		if err := self.ReconcileRecordingReferenceInvalidations(ctx, source.UserID, source.ID); err != nil {
			log.Warningf("cannot reconcile recording references: %s", err)
		}
		if ctx.Err() != nil {
			return
		}
	}
}
