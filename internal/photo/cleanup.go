package photo

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Cleanup deletes what the library no longer needs, at most batchSize of
// each kind in a pass: the parts of uploads that failed or went idle, photos
// that were deleted forever or have been in the trash longer than the
// operator allows (in batches of deletionBatchSize), and blobs no file refers
// to, and groups of faces that no face is in. Each batch is its own transaction, so that a restart in the middle finds
// the rest where it was left. One kind failing does not hold up the others.
func (self *Store) Cleanup(ctx context.Context, batchSize int) error {
	if batchSize < 1 || batchSize > 500 {
		return db.ErrInvalidArguments
	}
	return errors.Join(self.cleanUploads(ctx, batchSize), self.cleanTrash(ctx, batchSize), self.cleanBlobs(ctx, batchSize), self.cleanFaceGroups(ctx))
}

func (self *Store) cleanUploads(ctx context.Context, batchSize int) error {
	for range batchSize {
		hasFoundUpload := false
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			upload, err := transaction.LockPhotoUploadForCleanup(self.now().Add(-idleUploadRetention))
			if errors.Is(err, db.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			hasFoundUpload = true
			parts, err := transaction.ListPhotoUploadPartsForCleanup(upload.ID)
			if err != nil {
				return err
			}
			// The part after the last receipt may have been written and
			// never recorded, and is deleted by name with the rest.
			for partIndex := 0; partIndex <= len(parts); partIndex++ {
				if err := self.files.DeleteFileEverywhere(ctx, models.PhotoUploadPartFileID(upload.ID, partIndex)); err != nil {
					return err
				}
			}
			return transaction.DeletePhotoUpload(upload.ID)
		}); err != nil {
			return err
		}
		if !hasFoundUpload {
			return nil
		}
	}
	return nil
}

// deletionBatchSize is how many photos one transaction of cleanup deletes.
// Deleting a photo costs object store calls, which are not worth holding
// row locks across for more than this many.
const deletionBatchSize = 100

// cleanTrash deletes forever the photos a person deleted forever, and those
// that have been in the trash longer than the operator allows. The person's
// request only marks photos, so that a trash of thousands is not one
// request's transaction; the work is here, batchSize batches of
// deletionBatchSize photos a pass, and what a pass leaves the next one takes.
func (self *Store) cleanTrash(ctx context.Context, batchSize int) error {
	var expiredBefore time.Time
	if retention := self.configuration().Photos.TrashRetention.Duration(); retention > 0 {
		expiredBefore = self.now().Add(-retention)
	}
	for range batchSize {
		deletedCount, err := self.deletePhotoBatch(ctx, expiredBefore)
		if err != nil || deletedCount < deletionBatchSize {
			return err
		}
	}
	return nil
}

// deletePhotoBatch deletes one batch of photos in three steps, each of which
// leaves the photos marked for deletion, and so found again by the next pass,
// if it fails: taking the photos (which marks those that expired), deleting
// their previews' bytes, and deleting their rows. The bytes go before the rows
// and outside any transaction. A photo that is marked is hidden from every
// listing and cannot be restored, so previews deleted under a row that
// remains are never seen, and there is no commit to wait for. The photos' own
// files are deleted by the blob cleanup once nothing refers to them.
func (self *Store) deletePhotoBatch(ctx context.Context, expiredBefore time.Time) (int, error) {
	var photos []*models.Photo
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		photos, err = transaction.ClaimPhotosForDeletion(expiredBefore, deletionBatchSize)
		return err
	}); err != nil {
		return 0, err
	}
	if len(photos) == 0 {
		return 0, nil
	}
	photoIds := make([]string, 0, len(photos))
	for _, photo := range photos {
		photoIds = append(photoIds, photo.ID)
		// By every name a preview can have, not by the previews that have a
		// row: processing may have stored one and not yet recorded it.
		for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge, models.PhotoPreviewVideo} {
			if err := self.files.DeleteFileEverywhere(ctx, models.PhotoPreviewFileID(photo.ID, photoPreviewSize)); err != nil {
				return 0, err
			}
		}
	}
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.DeleteMarkedPhotos(photoIds)
		return err
	}); err != nil {
		return 0, err
	}
	return len(photos), nil
}
func (self *Store) cleanBlobs(ctx context.Context, batchSize int) error {
	for range batchSize {
		// Marked in a transaction of its own, so that the mark is kept
		// whatever happens to the deletion that follows: a file arriving
		// with the same bytes in between sees it, and stores them again
		// instead of trusting bytes that may be gone.
		var marked *models.PhotoBlob
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			marked, err = transaction.MarkUnreferencedPhotoBlobDeleting(self.now().Add(-unreferencedBlobRetention))
			return err
		}); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil
			}
			return err
		}
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			blob, err := transaction.LockPhotoBlobForDeletion(marked.BlobSHA256)
			if errors.Is(err, db.ErrNotFound) || (err == nil && blob == nil) {
				// A file refers to it after all, or it is already gone.
				return nil
			}
			if err != nil {
				return err
			}
			if err := self.files.DeleteFileEverywhere(ctx, models.PhotoBlobFileID(blob.BlobSHA256)); err != nil {
				return err
			}
			return transaction.DeletePhotoBlob(blob.BlobSHA256)
		}); err != nil {
			return err
		}
	}
	return nil
}
