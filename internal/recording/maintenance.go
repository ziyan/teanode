package recording

import (
	"context"
	"errors"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// InitializeKnowledge discovers transcripts completed before automatic source ingestion existed.
func (self *Store) InitializeKnowledge(ctx context.Context, pageSize int) error {
	self.maintenanceMutex.Lock()
	defer self.maintenanceMutex.Unlock()
	var sources []*models.Recording
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		sources, err = transaction.ListRecordingsAwaitingKnowledgeInitialization(self.lastKnowledgeRecordingId, pageSize)
		return err
	}); err != nil {
		return err
	}
	for _, source := range sources {
		self.lastKnowledgeRecordingId = source.ID
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			_, err := transaction.InitializeRecordingKnowledge(source.UserID, source.ID)
			return err
		}); err != nil && !errors.Is(err, db.ErrNotFound) && !errors.Is(err, db.ErrRecordingDeleted) {
			return err
		}
	}
	if len(sources) < pageSize {
		self.lastKnowledgeRecordingId = ""
	}
	return nil
}

// DeleteRevokedReferences keeps byte cleanup independent of enabled model workers.
func (self *Store) DeleteRevokedReferences(ctx context.Context, pageSize int) error {
	self.maintenanceMutex.Lock()
	defer self.maintenanceMutex.Unlock()
	var candidates []*db.RecordingVoiceReferenceCleanupCandidate
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		candidates, err = transaction.ListRevokedRecordingVoiceReferencesForCleanup(self.lastRevokedReferenceId, pageSize)
		return err
	}); err != nil {
		return err
	}
	for _, candidate := range candidates {
		self.lastRevokedReferenceId = candidate.Reference.ID
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			reference, err := transaction.LockRevokedRecordingVoiceReferenceForCleanup(candidate.UserID, candidate.Reference.RecordingID, candidate.Reference.ID)
			if err != nil || reference == nil || reference.RecordingBlobID == "" {
				return err
			}
			if err := self.files.DeleteFileEverywhere(ctx, reference.RecordingBlobID); err != nil {
				return err
			}
			return transaction.DeleteRevokedRecordingVoiceReference(candidate.UserID, reference.RecordingID, reference.ID)
		}); err != nil && !errors.Is(err, db.ErrNotFound) && !errors.Is(err, db.ErrRecordingDeleted) {
			return err
		}
	}
	if len(candidates) < pageSize {
		self.lastRevokedReferenceId = ""
	}
	return nil
}
