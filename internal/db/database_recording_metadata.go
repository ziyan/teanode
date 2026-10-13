package db

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// RecordingMetadataOperation edits source metadata without requesting transcription.
type RecordingMetadataOperation interface {
	UpdateRecordingMetadata(userId, recordingId string, expectedSourceGeneration int64, recordingTitle *string, recordedAt *time.Time) (*models.Recording, error)
	ListRecordingsAwaitingSummary(afterRecordingId string, limit int) ([]*models.Recording, error)
	MarkRecordingSummaryQueued(userId, recordingId string) error
	StoreGeneratedRecordingSummary(userId, recordingId, recordingTitle, recordingSummary string) error
	SetRecordingRecordedAtIfUnset(userId, recordingId string, recordedAt time.Time) (bool, error)
}

// SetRecordingRecordedAtIfUnset fills an occurrence date read from the
// original, and never replaces one the person or the client already set.
func (self *transaction) SetRecordingRecordedAtIfUnset(userId, recordingId string, recordedAt time.Time) (bool, error) {
	if recordedAt.IsZero() {
		return false, ErrInvalidArguments
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return false, err
	}
	if source.RecordedAt != nil {
		return false, nil
	}
	if err := self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Updates(map[string]any{"recorded_at": recordedAt.UTC().Truncate(time.Microsecond), "source_generation": source.SourceGeneration + 1, "updated_at": time.Now().UTC().Truncate(time.Microsecond)}).Error; err != nil {
		return false, err
	}
	return true, self.SyncRecordingKnowledge(userId, recordingId)
}

func (self *transaction) UpdateRecordingMetadata(userId, recordingId string, expectedSourceGeneration int64, recordingTitle *string, recordedAt *time.Time) (*models.Recording, error) {
	if (recordingTitle != nil && len(*recordingTitle) > 255) || (recordedAt != nil && recordedAt.IsZero()) {
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
	updates := map[string]any{"recorded_at": recordedAt, "source_generation": expectedSourceGeneration + 1, "updated_at": time.Now().UTC().Truncate(time.Microsecond)}
	// An absent title leaves the title and who wrote it alone. A typed title is
	// never replaced by a generated one; clearing it hands the name back to generation.
	if recordingTitle != nil {
		trimmedTitle := strings.TrimSpace(*recordingTitle)
		updates["recording_title"] = trimmedTitle
		updates["recording_title_source"] = models.RecordingTitleSourceUser
		if trimmedTitle == "" {
			updates["recording_title_source"] = models.RecordingTitleSourceNone
		}
	}
	if err := self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := self.SyncRecordingKnowledge(userId, recordingId); err != nil {
		return nil, err
	}
	return self.GetRecording(userId, recordingId)
}

// StoreGeneratedRecordingSummary keeps what the server wrote about a recording.
// The title is stored only while the person has not typed one. It does not
// advance the source generation: it is not an edit a client could be racing.
func (self *transaction) StoreGeneratedRecordingSummary(userId, recordingId, recordingTitle, recordingSummary string) error {
	recordingTitle = strings.TrimSpace(recordingTitle)
	recordingSummary = strings.TrimSpace(recordingSummary)
	if len(recordingTitle) > 255 {
		return ErrInvalidArguments
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	updates := map[string]any{"recording_summary": recordingSummary}
	if source.RecordingTitleSource != models.RecordingTitleSourceUser && recordingTitle != "" {
		updates["recording_title"] = recordingTitle
		updates["recording_title_source"] = models.RecordingTitleSourceGenerated
	}
	if err := self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Updates(updates).Error; err != nil {
		return err
	}
	if _, isTitled := updates["recording_title"]; isTitled {
		return self.SyncRecordingKnowledge(userId, recordingId)
	}
	return nil
}

// ListRecordingsAwaitingSummary finds recordings with a completed transcript
// whose generation was never queued (or was cleared to run again), for the sweep
// that names recordings made before the server did. Only titles the agent wrote,
// or none, are offered: a title the person typed is never replaced, so a user
// title is never offered. Queueing marks them, so each is offered once and never again.
func (self *transaction) ListRecordingsAwaitingSummary(afterRecordingId string, limit int) ([]*models.Recording, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	query := self.tx.Where("deleted_at IS NULL AND user_id IS NOT NULL AND recording_title_source IN ? AND recording_summary_queued_at IS NULL", []models.RecordingTitleSource{models.RecordingTitleSourceNone, models.RecordingTitleSourceGenerated}).
		Where("EXISTS (SELECT 1 FROM recording_transcript_run WHERE recording_transcript_run.recording_id = recording.id AND recording_transcript_run.recording_processing_state = ?)", models.RecordingProcessingComplete).
		Order("id").Limit(limit)
	if afterRecordingId != "" {
		query = query.Where("id > ?", afterRecordingId)
	}
	var rows []*recordingModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	recordings := make([]*models.Recording, 0, len(rows))
	for _, row := range rows {
		recordings = append(recordings, recordingFromModel(row))
	}
	return recordings, nil
}

// MarkRecordingSummaryQueued records that generation was queued, without touching the source generation.
func (self *transaction) MarkRecordingSummaryQueued(userId, recordingId string) error {
	result := self.tx.Model(&recordingModel{}).Where("id = ? AND user_id = ?", recordingId, userId).Update("recording_summary_queued_at", time.Now().UTC().Truncate(time.Microsecond))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
