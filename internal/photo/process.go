package photo

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Process gives previews and metadata to up to batchSize photos waiting for
// them. A photo is claimed in a transaction of its own before any work is
// done on it, so several instances running this share the waiting photos
// instead of repeating each other's work, and a claim older than
// processingClaimRetention is the work of an instance that has gone and is
// taken again.
func (self *Store) Process(ctx context.Context, batchSize int) error {
	var photos []*models.Photo
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		photos, err = transaction.ClaimPhotosForProcessing(batchSize, time.Now().Add(-processingClaimRetention))
		return err
	}); err != nil {
		return err
	}
	var failures []error
	for _, photo := range photos {
		processError := self.processPhoto(ctx, photo)
		if err := ctx.Err(); err != nil {
			// Stopping is not the photo's fault. Its claim runs out and it
			// is taken again.
			return errors.Join(append(failures, err)...)
		}
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			if processError != nil {
				return transaction.MarkPhotoProcessingFailed(photo.ID, processError.Error(), maximumProcessingAttemptCount)
			}
			return transaction.MarkPhotoReady(photo.ID)
		}); err != nil {
			failures = append(failures, err)
		}
		if processError != nil {
			log.Warningf("cannot process photo %s: %s", photo.ID, processError)
		}
	}
	return errors.Join(failures...)
}

// processPhoto is where a claimed photo gets the metadata read from its file
// and the previews made from it. Nothing is done yet, so a photo is ready as
// soon as it is claimed, with no previews and nothing read from the file.
func (self *Store) processPhoto(ctx context.Context, photo *models.Photo) error {
	return nil
}
