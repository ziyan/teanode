package db

import (
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordingPreviewOperation reserves browser playback derivatives before their
// bytes are written, using the existing recording quota and cleanup references.
type RecordingPreviewOperation interface {
	ReserveRecordingPreview(userId, mediaId string, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingBlob, error)
	ReadRecordingPreview(userId, mediaId string) (*models.RecordingBlob, error)
}

// RecordingPreviewFileID gives one versioned playback derivative per original.
func RecordingPreviewFileID(mediaId string) string {
	return "recording-preview-v1-" + mediaId
}

func (self *transaction) ReadRecordingPreview(userId, mediaId string) (*models.RecordingBlob, error) {
	media, err := self.GetRecordingMedia(userId, mediaId)
	if err != nil {
		return nil, err
	}
	if media.RecordingUploadState != models.RecordingUploadComplete || media.VerifiedAt == nil {
		return nil, ErrInvalidArguments
	}
	blob, err := self.GetRecordingBlob(userId, RecordingPreviewFileID(mediaId))
	if err != nil {
		return nil, err
	}
	if blob.RecordingID != media.RecordingID || blob.RecordingMediaID != media.ID || blob.RecordingBlobPurpose != models.RecordingBlobAudioPreview {
		return nil, ErrInvalidArguments
	}
	return blob, nil
}

func (self *transaction) ReserveRecordingPreview(userId, mediaId string, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingBlob, error) {
	if mediaId == "" || blob == nil || blob.ID != RecordingPreviewFileID(mediaId) || len(blob.ID) > 128 || blob.BlobSizeBytes <= 0 || !validRecordingSHA256(blob.BlobSHA256) || maximumAccountBytes <= 0 {
		return nil, ErrInvalidArguments
	}
	// Serialize account quota changes before taking any recording lock, matching
	// original uploads, derived audio chunks, and recording deletion.
	var owner userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).Take(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	media, err := self.GetRecordingMedia(userId, mediaId)
	if err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, media.RecordingID); err != nil {
		return nil, err
	}
	if media.RecordingUploadState != models.RecordingUploadComplete || media.VerifiedAt == nil || (blob.RecordingID != "" && blob.RecordingID != media.RecordingID) || (blob.RecordingMediaID != "" && blob.RecordingMediaID != media.ID) || (blob.RecordingBlobPurpose != "" && blob.RecordingBlobPurpose != models.RecordingBlobAudioPreview) {
		return nil, ErrInvalidArguments
	}
	previous, err := self.ReadRecordingPreview(userId, mediaId)
	if err == nil {
		if previous.BlobSizeBytes != blob.BlobSizeBytes || previous.BlobSHA256 != blob.BlobSHA256 {
			return nil, ErrAlreadyExists
		}
		return previous, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var originalBytes, derivedBytes int64
	if err := self.tx.Table("recording_media").Select("COALESCE(SUM(original_size_bytes), 0)").Joins("JOIN recording ON recording.id = recording_media.recording_id").Where("recording.user_id = ?", userId).Scan(&originalBytes).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Table("recording_blob").Select("COALESCE(SUM(blob_size_bytes), 0)").Joins("JOIN recording ON recording.id = recording_blob.recording_id").Where("recording.user_id = ?", userId).Scan(&derivedBytes).Error; err != nil {
		return nil, err
	}
	if originalBytes > maximumAccountBytes-blob.BlobSizeBytes || derivedBytes > maximumAccountBytes-blob.BlobSizeBytes-originalBytes {
		return nil, ErrTooMuchAsked
	}
	reserved := *blob
	reserved.RecordingID = media.RecordingID
	reserved.RecordingMediaID = media.ID
	reserved.RecordingBlobPurpose = models.RecordingBlobAudioPreview
	reserved.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	reserved.IsStoredVerified = false
	row := recordingBlobModel(reserved)
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &reserved, nil
}
