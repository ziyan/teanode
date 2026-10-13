package db

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm/clause"
)

// RecordingReferenceOperation tracks dependent attribution after a reference changes.
type RecordingReferenceOperation interface {
	ListRecordingReferenceInvalidations(userId, recordingId string) ([]*models.RecordingReferenceInvalidation, error)
	ListRecordingsAwaitingReferenceReconciliation(pageSize int) ([]*models.Recording, error)
	ReconcileRecordingReferenceInvalidations(userId, recordingId string) error
}

type recordingReferenceInvalidationModel models.RecordingReferenceInvalidation

func (recordingReferenceInvalidationModel) TableName() string {
	return "recording_reference_invalidation"
}

// Source mutation locks all affected account recordings before taking graph locks.
func (self *transaction) lockRecordingReferenceDependents(userId, recordingId string) error {
	return self.lockRecordingReferenceDependentSources(userId, []string{recordingId})
}
func (self *transaction) lockRecordingReferenceDependentSources(userId string, referenceRecordingIds []string) error {
	var recordingIds []string
	if err := self.tx.Raw(`SELECT DISTINCT run.recording_id FROM recording_transcription_attempt a
  JOIN recording_transcript_run run ON run.id=a.recording_transcript_run_id
  JOIN recording r ON r.id=run.recording_id
  WHERE r.user_id = ? AND r.deleted_at IS NULL AND a.recording_attempt_state IN ('submitted','complete','uncertain')
   AND EXISTS (SELECT 1 FROM recording_voice_reference v WHERE v.recording_id IN ?
    AND v.recording_blob_id IS NOT NULL AND a.reference_blob_ids @> jsonb_build_array(v.recording_blob_id))
  UNION SELECT id FROM recording WHERE id IN ? AND user_id = ? AND deleted_at IS NULL`, userId, referenceRecordingIds, referenceRecordingIds, userId).Scan(&recordingIds).Error; err != nil {
		return err
	}
	var lockedSources []*recordingModel
	return self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ? AND user_id = ? AND deleted_at IS NULL", recordingIds, userId).Order("id").Find(&lockedSources).Error
}
func (self *transaction) invalidateRecordingVoiceReferences(userId string, references []*models.RecordingVoiceReference, invalidationMessage string) error {
	for _, reference := range references {
		if reference.RecordingBlobID == "" {
			continue
		}
		var confirmedIdentity recordingSpeakerIdentityModel
		if err := self.tx.Where("id = ? AND user_id = ?", reference.SpeakerIdentityID, userId).Take(&confirmedIdentity).Error; err != nil {
			return err
		}
		var affectedAttempts []*recordingAttemptModel
		// Only admission or generated output can carry conditioning evidence. Prepared
		// attempts will instead refresh references before their first paid submission.
		if err := self.tx.Select("a.id,a.recording_transcript_run_id").Table("recording_transcription_attempt a").Joins("JOIN recording_transcript_run run ON run.id=a.recording_transcript_run_id").Joins("JOIN recording r ON r.id=run.recording_id").Where("r.user_id = ? AND r.deleted_at IS NULL AND a.recording_attempt_state IN ('submitted','complete','uncertain') AND a.reference_blob_ids @> jsonb_build_array(?::text)", userId, reference.RecordingBlobID).Find(&affectedAttempts).Error; err != nil {
			return err
		}
		for _, attempt := range affectedAttempts {
			transcriptRun, err := self.GetRecordingTranscriptRun(userId, attempt.RecordingTranscriptRunID)
			if err != nil {
				return err
			}
			row := recordingReferenceInvalidationModel{ID: newID(), RecordingID: transcriptRun.RecordingID, RecordingTranscriptRunID: transcriptRun.ID, RecordingTranscriptionAttemptID: attempt.ID, ReferenceBlobID: reference.RecordingBlobID, ReferenceRecordingID: reference.RecordingID, ReferenceTranscriptRunID: reference.RecordingTranscriptRunID, SpeakerIdentityID: reference.SpeakerIdentityID, InvalidatedConfirmedName: confirmedIdentity.ConfirmedName, ReferencePassageStartMS: reference.PassageStartMS, ReferencePassageEndMS: reference.PassageEndMS, ReferenceSourceGeneration: reference.SourceGeneration, InvalidationMessage: invalidationMessage, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
			if err := self.tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "recording_transcription_attempt_id"}, {Name: "reference_blob_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
func (self *transaction) ListRecordingReferenceInvalidations(userId, recordingId string) ([]*models.RecordingReferenceInvalidation, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []*recordingReferenceInvalidationModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("created_at,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	invalidations := make([]*models.RecordingReferenceInvalidation, 0, len(rows))
	for _, row := range rows {
		invalidations = append(invalidations, (*models.RecordingReferenceInvalidation)(row))
	}
	return invalidations, nil
}
func (self *transaction) ListRecordingsAwaitingReferenceReconciliation(pageSize int) ([]*models.Recording, error) {
	if pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidArguments
	}
	var rows []*recordingModel
	if err := self.tx.Where("user_id IS NOT NULL AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM recording_reference_invalidation i WHERE i.recording_id=recording.id AND NOT i.is_reconciled)").Order("updated_at,id").Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	sources := make([]*models.Recording, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, recordingFromModel(row))
	}
	return sources, nil
}
func (self *transaction) ReconcileRecordingReferenceInvalidations(userId, recordingId string) error {
	if _, err := self.LockRecording(userId, recordingId); err != nil {
		return err
	}
	var pendingCount int64
	if err := self.tx.Model(&recordingReferenceInvalidationModel{}).Where("recording_id = ? AND NOT is_reconciled", recordingId).Count(&pendingCount).Error; err != nil {
		return err
	}
	if pendingCount == 0 {
		return nil
	}
	if err := self.advanceRecordingSpeakerGeneration(recordingId); err != nil {
		return err
	}
	if err := self.QueueRecordingKnowledgeSync(userId, recordingId); err != nil {
		return err
	}
	return self.tx.Model(&recordingReferenceInvalidationModel{}).Where("recording_id = ? AND NOT is_reconciled", recordingId).Update("is_reconciled", true).Error
}
