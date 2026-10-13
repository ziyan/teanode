package db

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// RecordingMetadataOperation edits source metadata without requesting transcription.
type RecordingMetadataOperation interface {
	UpdateRecordingMetadata(userId, recordingId string, expectedSourceGeneration int64, recordingTitle string, recordedAt *time.Time) (*models.Recording, error)
}

func (self *transaction) UpdateRecordingMetadata(userId, recordingId string, expectedSourceGeneration int64, recordingTitle string, recordedAt *time.Time) (*models.Recording, error) {
	if len(recordingTitle) > 255 || (recordedAt != nil && recordedAt.IsZero()) {
		return nil, ErrInvalidArguments
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return nil, err
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return nil, ErrAlreadyExists
	}
	if recordedAt != nil {
		utcTime := recordedAt.UTC().Truncate(time.Microsecond)
		recordedAt = &utcTime
	}
	if err := self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Updates(map[string]any{"recording_title": strings.TrimSpace(recordingTitle), "recorded_at": recordedAt, "source_generation": expectedSourceGeneration + 1, "updated_at": time.Now().UTC().Truncate(time.Microsecond)}).Error; err != nil {
		return nil, err
	}
	if err := self.SyncRecordingKnowledge(userId, recordingId); err != nil {
		return nil, err
	}
	return self.GetRecording(userId, recordingId)
}
