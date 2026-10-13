// Package recording keeps passive evidence separate from conversation commands.
package recording

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Store coordinates account-owned manifests with the existing blob store.
type Store struct {
	database db.Database
	files    storage.StreamFiles

	maintenanceMutex         sync.Mutex
	lastKnowledgeRecordingId string
	lastRevokedReferenceId   string
	lastImportedMediaId      string
}

// New requires range-capable storage rather than buffering an entire import.
func New(database db.Database, files storage.StreamFiles) *Store {
	return &Store{database: database, files: files}
}

// ReconcileReferences retires changed attribution even when no agent worker exists.
func (self *Store) ReconcileReferences(ctx context.Context, pageSize int) error {
	var sources []*models.Recording
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		sources, err = transaction.ListRecordingsAwaitingReferenceReconciliation(pageSize)
		return err
	}); err != nil {
		return err
	}
	for _, source := range sources {
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			return transaction.ReconcileRecordingReferenceInvalidations(source.UserID, source.ID)
		}); err != nil {
			return err
		}
	}
	return nil
}

// PutPart publishes a receipt only after the exact bytes are durably stored.
// The caller bounds its request body to RecordingUploadPartBytes before calling.
func (self *Store) PutPart(ctx context.Context, userId, mediaId string, partIndex int, partSha256 string, content []byte) (*models.RecordingMediaPart, error) {
	if len(content) == 0 || int64(len(content)) > models.RecordingUploadPartBytes {
		return nil, db.ErrInvalidArguments
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != partSha256 {
		return nil, db.ErrInvalidArguments
	}
	var receipt *models.RecordingMediaPart
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		media, err := transaction.GetRecordingMedia(userId, mediaId)
		if err != nil {
			return err
		}
		if _, err := transaction.LockRecording(userId, media.RecordingID); err != nil {
			return err
		}
		media, err = transaction.GetRecordingMedia(userId, mediaId)
		if err != nil {
			return err
		}
		if media.PartSizeBytes(partIndex) != int64(len(content)) {
			return db.ErrInvalidArguments
		}
		parts, err := transaction.ListRecordingMediaParts(userId, mediaId)
		if err != nil {
			return err
		}
		for _, part := range parts {
			if part.PartIndex == partIndex {
				if part.PartSHA256 != partSha256 || part.PartSizeBytes != int64(len(content)) {
					return db.ErrAlreadyExists
				}
				// A retry can repair lost or corrupted storage using only bytes
				// matching the immutable receipt. It cannot replace the original.
				if err := self.verifyPart(ctx, part); err != nil {
					if err := ctx.Err(); err != nil {
						return err
					}
					if err := self.files.PutFileStream(ctx, db.RecordingPartFileID(mediaId, partIndex), bytes.NewReader(content), int64(len(content))); err != nil {
						return err
					}
				}
				receipt = part
				return nil
			}
		}
		if media.RecordingUploadState != models.RecordingUploadPending {
			return db.ErrAlreadyExists
		}
		// The same recording lock excludes deletion and conflicting part retries.
		// Deterministic identifiers retain failed writes for eventual cleanup.
		if err := self.files.PutFileStream(ctx, db.RecordingPartFileID(mediaId, partIndex), bytes.NewReader(content), int64(len(content))); err != nil {
			return err
		}
		receipt, err = transaction.PutRecordingMediaPart(userId, &models.RecordingMediaPart{RecordingMediaID: mediaId, PartIndex: partIndex, PartSizeBytes: int64(len(content)), PartSHA256: partSha256})
		return err
	})
	return receipt, err
}

func (self *Store) verifyPart(ctx context.Context, part *models.RecordingMediaPart) error {
	stream, err := self.files.OpenFileRange(ctx, db.RecordingPartFileID(part.RecordingMediaID, part.PartIndex), 0, part.PartSizeBytes)
	if err != nil {
		return err
	}
	partDigest := sha256.New()
	copiedBytes, err := io.Copy(partDigest, stream)
	closeError := stream.Close()
	if err != nil {
		return err
	}
	if closeError != nil {
		return closeError
	}
	if copiedBytes != part.PartSizeBytes || hex.EncodeToString(partDigest.Sum(nil)) != part.PartSHA256 {
		return fmt.Errorf("recording: stored part failed integrity verification")
	}
	return nil
}

// Complete verifies the whole original and publishes completion exactly once.
// The returned boolean distinguishes the first publication from a lost-response retry.
func (self *Store) Complete(ctx context.Context, userId, mediaId string) (*models.RecordingMedia, bool, error) {
	var media *models.RecordingMedia
	var parts []*models.RecordingMediaPart
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, err = transaction.GetRecordingMedia(userId, mediaId)
		if err != nil {
			return err
		}
		parts, err = transaction.ListRecordingMediaParts(userId, mediaId)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	if media.RecordingUploadState == models.RecordingUploadComplete {
		return media, false, nil
	}
	if len(parts) != media.PartCount {
		return nil, false, db.ErrInvalidArguments
	}
	wholeDigest := sha256.New()
	for partIndex, part := range parts {
		if part.PartIndex != partIndex || part.PartSizeBytes != media.PartSizeBytes(partIndex) {
			return nil, false, db.ErrInvalidArguments
		}
		stream, err := self.files.OpenFileRange(ctx, db.RecordingPartFileID(mediaId, partIndex), 0, part.PartSizeBytes)
		if err != nil {
			return nil, false, err
		}
		partDigest := sha256.New()
		copiedBytes, err := io.Copy(io.MultiWriter(wholeDigest, partDigest), stream)
		closeError := stream.Close()
		if err != nil {
			return nil, false, err
		}
		if closeError != nil {
			return nil, false, closeError
		}
		if copiedBytes != part.PartSizeBytes || hex.EncodeToString(partDigest.Sum(nil)) != part.PartSHA256 {
			return nil, false, fmt.Errorf("recording: stored part failed integrity verification")
		}
	}
	verifiedSha256 := hex.EncodeToString(wholeDigest.Sum(nil))
	if verifiedSha256 != media.OriginalSHA256 {
		return nil, false, fmt.Errorf("%w: original digest does not match", db.ErrInvalidArguments)
	}
	var isFirstCompletion bool
	err = self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, isFirstCompletion, err = transaction.CompleteRecordingMedia(userId, mediaId, verifiedSha256)
		return err
	})
	if err == nil && isFirstCompletion {
		self.recordImportedContainer(ctx, userId, mediaId)
	}
	return media, isFirstCompletion, err
}

// CopyRange streams a bounded portion of the original across transport parts.
func (self *Store) CopyRange(ctx context.Context, userId, mediaId string, writer io.Writer, offsetBytes, sizeBytes int64) error {
	var media *models.RecordingMedia
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, err = transaction.GetRecordingMedia(userId, mediaId)
		return err
	})
	if err != nil {
		return err
	}
	if media.RecordingUploadState != models.RecordingUploadComplete || offsetBytes < 0 || sizeBytes <= 0 || offsetBytes > media.OriginalSizeBytes || sizeBytes > media.OriginalSizeBytes-offsetBytes {
		return db.ErrInvalidArguments
	}
	for remainingBytes := sizeBytes; remainingBytes > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Recheck ownership and deletion between bounded reads without holding a
		// transaction open while a slow playback client consumes the original.
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			_, err := transaction.GetRecordingMedia(userId, mediaId)
			return err
		}); err != nil {
			return err
		}
		partIndex := int(offsetBytes / models.RecordingUploadPartBytes)
		partOffsetBytes := offsetBytes % models.RecordingUploadPartBytes
		portionBytes := min(remainingBytes, media.PartSizeBytes(partIndex)-partOffsetBytes)
		stream, err := self.files.OpenFileRange(ctx, db.RecordingPartFileID(mediaId, partIndex), partOffsetBytes, portionBytes)
		if err != nil {
			return err
		}
		copiedBytes, err := io.Copy(writer, stream)
		closeError := stream.Close()
		if err != nil {
			return err
		}
		if closeError != nil {
			return closeError
		}
		if copiedBytes != portionBytes {
			return io.ErrUnexpectedEOF
		}
		remainingBytes -= copiedBytes
		offsetBytes += copiedBytes
	}
	return nil
}

// Cleanup removes a bounded number of original parts, including uncommitted
// writes. Metadata survives storage failures so deletion can resume after restart.
func (self *Store) Cleanup(ctx context.Context, maximumPartCount int) error {
	if maximumPartCount < 1 || maximumPartCount > 100 {
		return db.ErrInvalidArguments
	}
	for deletedPartCount := 0; deletedPartCount < maximumPartCount; {
		hasFoundRecording := false
		err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			recordings, err := transaction.ListRecordingsForCleanup(1)
			if err != nil || len(recordings) == 0 {
				return err
			}
			hasFoundRecording = true
			recording := recordings[0]
			blobs, err := transaction.ListRecordingBlobsForCleanup(recording.ID, 1)
			if err != nil {
				return err
			}
			if len(blobs) > 0 {
				blob := blobs[0]
				if err := self.files.DeleteFileEverywhere(ctx, blob.ID); err != nil {
					return err
				}
				if err := transaction.DeleteRecordingBlobReference(recording.ID, blob.ID); err != nil {
					return err
				}
				deletedPartCount++
				return nil
			}
			mediaFiles, err := transaction.ListRecordingMediaForCleanup(recording.ID)
			if err != nil {
				return err
			}
			for _, media := range mediaFiles {
				if media.CleanupNextPartIndex < media.PartCount {
					partIndex := media.CleanupNextPartIndex
					if err := self.files.DeleteFileEverywhere(ctx, db.RecordingPartFileID(media.ID, partIndex)); err != nil {
						return err
					}
					if err := transaction.AdvanceRecordingMediaCleanup(recording.ID, media.ID, partIndex); err != nil {
						return err
					}
					deletedPartCount++
					return nil
				}
			}
			deletedPartCount++ // Bound empty manifests as well as blob deletion.
			return transaction.FinishRecordingCleanup(recording.ID)
		})
		if err != nil {
			return err
		}
		if !hasFoundRecording {
			return nil
		}
	}
	return nil
}
