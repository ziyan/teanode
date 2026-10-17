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
// that have been in the trash longer than the operator allows, and blobs no
// file refers to. Each is one transaction, so that a restart in the middle
// finds the rest where it was left. One kind failing does not hold up the
// others.
func (self *Store) Cleanup(ctx context.Context, batchSize int) error {
	if batchSize < 1 || batchSize > 500 {
		return db.ErrInvalidArguments
	}
	return errors.Join(self.cleanUploads(ctx, batchSize), self.cleanTrash(ctx, batchSize), self.cleanBlobs(ctx, batchSize))
}

func (self *Store) cleanUploads(ctx context.Context, batchSize int) error {
	for range batchSize {
		hasFoundUpload := false
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			upload, err := transaction.LockPhotoUploadForCleanup(time.Now().Add(-idleUploadRetention))
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

func (self *Store) cleanTrash(ctx context.Context, batchSize int) error {
	retention := self.configuration().Photos.TrashRetention.Duration()
	if retention <= 0 {
		return nil
	}
	for range batchSize {
		hasFoundPhoto := false
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			photos, err := transaction.ListPhotosTrashedBefore(time.Now().Add(-retention), 1)
			if err != nil || len(photos) == 0 {
				return err
			}
			hasFoundPhoto = true
			return self.deleteTrashedPhotos(ctx, transaction, photos[0].UserID, photos)
		}); err != nil {
			return err
		}
		if !hasFoundPhoto {
			return nil
		}
	}
	return nil
}

func (self *Store) cleanBlobs(ctx context.Context, batchSize int) error {
	for range batchSize {
		// Marked in a transaction of its own, so that the mark is kept
		// whatever happens to the deletion that follows: a file arriving
		// with the same bytes in between sees it, and stores them again
		// instead of trusting bytes that may be gone.
		var marked *models.PhotoBlob
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			marked, err = transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-unreferencedBlobRetention))
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

// deleteTrashedPhotos deletes forever photos that are in the trash, with
// their previews: the rows of a preview go with the photo and its bytes
// would be left without a name to find them by, so they are deleted first,
// in the same transaction. The photos' own files are deleted by the blob
// cleanup once nothing refers to them.
func (self *Store) deleteTrashedPhotos(ctx context.Context, transaction db.Transaction, userId string, photos []*models.Photo) error {
	photoIds := make([]string, 0, len(photos))
	for _, photo := range photos {
		photoIds = append(photoIds, photo.ID)
	}
	previews, err := transaction.ListPhotoPreviews(userId, photoIds)
	if err != nil {
		return err
	}
	for _, preview := range previews {
		if err := self.files.DeleteFileEverywhere(ctx, models.PhotoPreviewFileID(preview.PhotoID, preview.PhotoPreviewSize)); err != nil {
			return err
		}
	}
	_, err = transaction.DeletePhotos(userId, photoIds)
	return err
}

// DeletePhotos deletes forever the named photos that are in the trash, in
// the caller's transaction, and says how many there were.
func (self *Store) DeletePhotos(ctx context.Context, transaction db.Transaction, userId string, photoIds []string) (int, error) {
	if len(photoIds) == 0 {
		return 0, nil
	}
	photos, err := transaction.ListTrashedPhotos(userId, photoIds, len(photoIds))
	if err != nil || len(photos) == 0 {
		return 0, err
	}
	return len(photos), self.deleteTrashedPhotos(ctx, transaction, userId, photos)
}

// EmptyTrash deletes forever everything in the person's trash, in the
// caller's transaction, and says how many photos that was.
func (self *Store) EmptyTrash(ctx context.Context, transaction db.Transaction, userId string) (int, error) {
	deletedCount := 0
	for {
		photos, err := transaction.ListTrashedPhotos(userId, nil, 100)
		if err != nil || len(photos) == 0 {
			return deletedCount, err
		}
		if err := self.deleteTrashedPhotos(ctx, transaction, userId, photos); err != nil {
			return deletedCount, err
		}
		deletedCount += len(photos)
	}
}
