package db

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
)

// recordingTemporarySpeakerLimit bounds the speakers one recording can have, so
// a long capture with a noisy room cannot create them without end. Each piece
// can add the voices it did not recognize, so a sixteen-minute meeting of nine
// people already needed more than twelve.
const recordingTemporarySpeakerLimit = 100

// RecordingTemporarySpeakerOperation keeps the speakers of a recording that is
// still being captured, until the person names them.
type RecordingTemporarySpeakerOperation interface {
	ListRecordingTemporarySpeakers(userId, recordingId string) ([]*models.RecordingTemporarySpeaker, error)
	CreateRecordingTemporarySpeaker(userId string, speaker *models.RecordingTemporarySpeaker, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingTemporarySpeaker, error)
	NameRecordingTemporarySpeaker(userId, recordingId, speakerLabel, confirmedName string, isAccountOwner bool) error
}

type recordingTemporarySpeakerModel struct {
	ID                   string
	RecordingID          string
	SpeakerLabel         string
	RecordingBlobID      *string
	SourceAttemptID      string
	SourceLocalSpeakerID string
	PassageStartMS       int64
	PassageEndMS         int64
	SpeakerIdentityID    *string
	CreatedAt            time.Time
}

func (recordingTemporarySpeakerModel) TableName() string { return "recording_temporary_speaker" }

func recordingTemporarySpeakerFromModel(row *recordingTemporarySpeakerModel) *models.RecordingTemporarySpeaker {
	return &models.RecordingTemporarySpeaker{ID: row.ID, RecordingID: row.RecordingID, SpeakerLabel: row.SpeakerLabel, RecordingBlobID: optionalString(row.RecordingBlobID), SourceAttemptID: row.SourceAttemptID, SourceLocalSpeakerID: row.SourceLocalSpeakerID, PassageStartMS: row.PassageStartMS, PassageEndMS: row.PassageEndMS, SpeakerIdentityID: optionalString(row.SpeakerIdentityID), CreatedAt: row.CreatedAt.UTC()}
}

// ListRecordingTemporarySpeakers lists a recording's speakers in the order they
// were first heard, each with the timeline millisecond it was last heard: in the
// passage its sample was cut from, or under its label in a piece it was sent to.
func (self *transaction) ListRecordingTemporarySpeakers(userId, recordingId string) ([]*models.RecordingTemporarySpeaker, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []*recordingTemporarySpeakerModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	speakers := make([]*models.RecordingTemporarySpeaker, 0, len(rows))
	for _, row := range rows {
		speakers = append(speakers, recordingTemporarySpeakerFromModel(row))
	}
	if len(speakers) == 0 {
		return speakers, nil
	}
	var heard []struct {
		ID             string
		LastHeardEndMS int64
		SpokenMS       int64
	}
	if err := self.tx.Raw(`SELECT speaker.id, COALESCE(MAX(segment.segment_end_ms), 0) AS last_heard_end_ms,
  COALESCE(SUM(segment.segment_end_ms - segment.segment_start_ms), 0) AS spoken_ms
  FROM recording_temporary_speaker speaker
  JOIN recording_transcript_run run ON run.recording_id = speaker.recording_id
  JOIN recording_transcription_attempt attempt ON attempt.recording_transcript_run_id = run.id
  JOIN recording_transcript_segment segment ON segment.recording_transcription_attempt_id = attempt.id
  WHERE speaker.recording_id = ? AND (
   (attempt.id = speaker.source_attempt_id AND segment.local_speaker_id = speaker.source_local_speaker_id)
   OR (segment.local_speaker_id = speaker.speaker_label AND speaker.recording_blob_id IS NOT NULL AND attempt.reference_blob_ids @> jsonb_build_array(speaker.recording_blob_id)))
  GROUP BY speaker.id`, recordingId).Scan(&heard).Error; err != nil {
		return nil, err
	}
	lastHeard := map[string]int64{}
	spoken := map[string]int64{}
	for _, row := range heard {
		lastHeard[row.ID] = row.LastHeardEndMS
		spoken[row.ID] = row.SpokenMS
	}
	for _, speaker := range speakers {
		speaker.LastHeardEndMS = lastHeard[speaker.ID]
		speaker.SpokenMS = spoken[speaker.ID]
	}
	return speakers, nil
}

// CreateRecordingTemporarySpeaker reserves a speaker and its sample's cleanup
// reference before the sample's bytes are written, and gives the speaker the
// next label of the recording. The passage and the speaker label in the
// attempt it came from identify it, so repeating the call returns the same speaker.
// A speaker with no clean passage to cut a sample from is still given a label,
// with a nil blob: it reads as "Speaker N" like the others and can be named,
// but no sample of it is sent with the next pieces.
func (self *transaction) CreateRecordingTemporarySpeaker(userId string, speaker *models.RecordingTemporarySpeaker, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingTemporarySpeaker, error) {
	if speaker == nil || speaker.SourceAttemptID == "" || speaker.SourceLocalSpeakerID == "" || len(speaker.SourceLocalSpeakerID) > 255 {
		return nil, ErrInvalidArguments
	}
	if blob != nil && (speaker.PassageStartMS < 0 || speaker.PassageEndMS-speaker.PassageStartMS < 2000 || speaker.PassageEndMS-speaker.PassageStartMS > 10000 || blob.BlobSizeBytes <= 0 || blob.BlobSizeBytes > 320044 || !validRecordingSHA256(blob.BlobSHA256)) {
		return nil, ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return nil, err
	}
	source, err := self.LockRecording(userId, speaker.RecordingID)
	if err != nil {
		return nil, err
	}
	var attempt recordingAttemptModel
	if err := self.tx.Select("id,recording_transcript_run_id,recording_attempt_state").Where("id = ?", speaker.SourceAttemptID).Take(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, attempt.RecordingTranscriptRunID)
	if err != nil {
		return nil, err
	}
	if transcriptRun.RecordingID != source.ID || source.RecordingOrigin != models.RecordingOriginCapture {
		return nil, ErrNotFound
	}
	if attempt.RecordingAttemptState != models.RecordingAttemptComplete || transcriptRun.RecordingProcessingState != models.RecordingProcessingComplete {
		return nil, ErrInvalidArguments
	}
	var existing recordingTemporarySpeakerModel
	err = self.tx.Where("source_attempt_id = ? AND source_local_speaker_id = ?", speaker.SourceAttemptID, speaker.SourceLocalSpeakerID).Take(&existing).Error
	if err == nil {
		return recordingTemporarySpeakerFromModel(&existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var labels []string
	if err := self.tx.Model(&recordingTemporarySpeakerModel{}).Where("recording_id = ?", source.ID).Pluck("speaker_label", &labels).Error; err != nil {
		return nil, err
	}
	if len(labels) >= recordingTemporarySpeakerLimit {
		return nil, ErrTooMuchAsked
	}
	// The next number after the highest taken, so a label is never reused.
	highestNumber := 0
	for _, label := range labels {
		if number, err := strconv.Atoi(strings.TrimPrefix(label, "Speaker ")); err == nil {
			highestNumber = max(highestNumber, number)
		}
	}
	created := *speaker
	created.ID = newID()
	created.SpeakerLabel = fmt.Sprintf("Speaker %d", highestNumber+1)
	created.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	created.SpeakerIdentityID = ""
	if blob == nil {
		created.RecordingBlobID = ""
		created.PassageStartMS, created.PassageEndMS = 0, 0
		row := recordingTemporarySpeakerModel{ID: created.ID, RecordingID: created.RecordingID, SpeakerLabel: created.SpeakerLabel, SourceAttemptID: created.SourceAttemptID, SourceLocalSpeakerID: created.SourceLocalSpeakerID, CreatedAt: created.CreatedAt}
		if err := self.tx.Create(&row).Error; err != nil {
			return nil, err
		}
		return &created, nil
	}
	var reservedBytes int64
	if err := self.tx.Raw(`SELECT COALESCE((SELECT SUM(original_size_bytes) FROM recording_media m JOIN recording r ON r.id=m.recording_id WHERE r.user_id = ?),0) + COALESCE((SELECT SUM(blob_size_bytes) FROM recording_blob b JOIN recording r ON r.id=b.recording_id WHERE r.user_id = ?),0)`, userId, userId).Scan(&reservedBytes).Error; err != nil {
		return nil, err
	}
	if maximumAccountBytes <= 0 || blob.BlobSizeBytes > maximumAccountBytes-reservedBytes {
		return nil, ErrTooMuchAsked
	}
	created.RecordingBlobID = "recording-temporary-speaker-" + created.ID
	derived := *blob
	derived.ID = created.RecordingBlobID
	derived.RecordingID = source.ID
	derived.RecordingMediaID = transcriptRun.RecordingMediaID
	derived.RecordingBlobPurpose = models.RecordingBlobSpeakerReference
	derived.CreatedAt = created.CreatedAt
	derived.IsStoredVerified = false
	if err := self.tx.Create((*recordingBlobModel)(&derived)).Error; err != nil {
		return nil, err
	}
	row := recordingTemporarySpeakerModel{ID: created.ID, RecordingID: created.RecordingID, SpeakerLabel: created.SpeakerLabel, RecordingBlobID: &created.RecordingBlobID, SourceAttemptID: created.SourceAttemptID, SourceLocalSpeakerID: created.SourceLocalSpeakerID, PassageStartMS: created.PassageStartMS, PassageEndMS: created.PassageEndMS, CreatedAt: created.CreatedAt}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

// NameRecordingTemporarySpeaker gives a temporary speaker the name the person
// supplied. It writes the ordinary speaker corrections for every piece the
// speaker was heard in, so everything downstream (the projection, the memory
// sync, the voice samples kept across recordings) sees a normal confirmation,
// and links the speaker to the identity so later pieces are sent its sample
// under the confirmed name.
func (self *transaction) NameRecordingTemporarySpeaker(userId, recordingId, speakerLabel, confirmedName string, isAccountOwner bool) error {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	confirmedName = strings.TrimSpace(confirmedName)
	if isAccountOwner && confirmedName == "" {
		confirmedName = strings.TrimSpace(owner.Name)
		if confirmedName == "" {
			confirmedName = owner.Username
		}
	}
	if !validConfirmedSpeakerName(confirmedName) || speakerLabel == "" {
		return ErrInvalidArguments
	}
	if err := self.lockRecordingReferenceDependents(userId, recordingId); err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	var speaker recordingTemporarySpeakerModel
	if err := self.tx.Where("recording_id = ? AND speaker_label = ?", source.ID, speakerLabel).Take(&speaker).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	// Every piece the speaker was heard in: the one its sample was cut from,
	// under the label that piece gave it, and each piece it was sent to, under
	// the temporary label the provider answered with.
	var appearances []struct {
		RecordingTranscriptionAttemptID string
		LocalSpeakerID                  string
	}
	if err := self.tx.Raw(`SELECT DISTINCT attempt.id AS recording_transcription_attempt_id, segment.local_speaker_id
  FROM recording_transcript_run run
  JOIN recording_transcription_attempt attempt ON attempt.recording_transcript_run_id = run.id
  JOIN recording_transcript_segment segment ON segment.recording_transcription_attempt_id = attempt.id
  WHERE run.recording_id = @recordingId AND attempt.recording_attempt_state = @complete AND (
   (attempt.id = @sourceAttemptId AND segment.local_speaker_id = @sourceLocalSpeakerId)
   OR (segment.local_speaker_id = @speakerLabel AND attempt.reference_blob_ids @> jsonb_build_array(CAST(@blobId AS text))))
  ORDER BY attempt.id, segment.local_speaker_id`,
		map[string]any{"recordingId": source.ID, "complete": models.RecordingAttemptComplete, "sourceAttemptId": speaker.SourceAttemptID, "sourceLocalSpeakerId": speaker.SourceLocalSpeakerID, "speakerLabel": speaker.SpeakerLabel, "blobId": optionalString(speaker.RecordingBlobID)}).Scan(&appearances).Error; err != nil {
		return err
	}
	if len(appearances) == 0 {
		return ErrNotFound
	}
	nameDigest := sha256.Sum256([]byte(confirmedName))
	for _, appearance := range appearances {
		current, err := self.GetRecording(userId, source.ID)
		if err != nil {
			return err
		}
		if _, err := self.CorrectRecordingSpeaker(userId, RecordingSpeakerCorrectionInput{
			RecordingID: source.ID, RecordingTranscriptionAttemptID: appearance.RecordingTranscriptionAttemptID, LocalSpeakerID: appearance.LocalSpeakerID,
			// The key names the speaker, the piece and the name, so repeating a request is harmless and renaming is a new correction.
			ClientCorrectionID: "temporary-speaker-" + speaker.ID + "-" + appearance.RecordingTranscriptionAttemptID + "-" + hex.EncodeToString(nameDigest[:4]),
			ConfirmedName:      confirmedName, IsAccountOwner: isAccountOwner, ExpectedSourceGeneration: current.SourceGeneration, PassageStartMS: -1, PassageEndMS: -1,
		}); err != nil {
			return err
		}
	}
	var identity recordingSpeakerIdentityModel
	if err := self.tx.Where("user_id = ? AND confirmed_name = ?", userId, confirmedName).Take(&identity).Error; err != nil {
		return err
	}
	return self.tx.Model(&recordingTemporarySpeakerModel{}).Where("id = ?", speaker.ID).Update("speaker_identity_id", identity.ID).Error
}
