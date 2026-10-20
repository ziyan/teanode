package db

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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
	NameRecordingTemporarySpeakerAsContact(userId, recordingId, speakerLabel, confirmedName string, isAccountOwner bool, addressBookId, contactId string) error
	MergeRecordingTemporarySpeaker(userId, recordingId, speakerLabel, intoSpeakerLabel string) error
	SeparateRecordingTemporarySpeaker(userId, recordingId, speakerLabel string) error
	MarkRecordingTemporarySpeakerMergeChecked(userId, speakerId string) error
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
	MergedIntoSpeakerID  *string `gorm:"column:merged_into_temporary_speaker_id"`
	MergeCheckedAt       *time.Time
}

func (recordingTemporarySpeakerModel) TableName() string { return "recording_temporary_speaker" }

func recordingTemporarySpeakerFromModel(row *recordingTemporarySpeakerModel) *models.RecordingTemporarySpeaker {
	return &models.RecordingTemporarySpeaker{ID: row.ID, RecordingID: row.RecordingID, SpeakerLabel: row.SpeakerLabel, RecordingBlobID: optionalString(row.RecordingBlobID), SourceAttemptID: row.SourceAttemptID, SourceLocalSpeakerID: row.SourceLocalSpeakerID, PassageStartMS: row.PassageStartMS, PassageEndMS: row.PassageEndMS, SpeakerIdentityID: optionalString(row.SpeakerIdentityID), CreatedAt: row.CreatedAt.UTC(), MergedIntoTemporarySpeakerID: optionalString(row.MergedIntoSpeakerID), MergeCheckedAt: row.MergeCheckedAt}
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
	return self.NameRecordingTemporarySpeakerAsContact(userId, recordingId, speakerLabel, confirmedName, isAccountOwner, "", "")
}

// NameRecordingTemporarySpeakerAsContact is NameRecordingTemporarySpeaker that
// also ties the name to a contact of the person's, when addressBookId and
// contactId are given: a name already tied to another contact is tied to this
// one instead. Neither leaves the name's tie as it is.
func (self *transaction) NameRecordingTemporarySpeakerAsContact(userId, recordingId, speakerLabel, confirmedName string, isAccountOwner bool, addressBookId, contactId string) error {
	if (addressBookId == "") != (contactId == "") {
		return ErrInvalidArguments
	}
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
	speaker, err := self.recordingTemporarySpeakerNamed(source.ID, speakerLabel)
	if err != nil {
		return err
	}
	// A label that was merged is the voice of the speaker it was merged into,
	// so naming either names both.
	if speaker, err = self.recordingTemporarySpeakerRoot(speaker); err != nil {
		return err
	}
	appearances, err := self.recordingTemporarySpeakerAppearances(source.ID, speaker)
	if err != nil {
		return err
	}
	if len(appearances) == 0 {
		return ErrNotFound
	}
	var mergedSpeakers []*recordingTemporarySpeakerModel
	if err := self.tx.Where("merged_into_temporary_speaker_id = ?", speaker.ID).Order("created_at, id").Find(&mergedSpeakers).Error; err != nil {
		return err
	}
	for _, mergedSpeaker := range mergedSpeakers {
		mergedAppearances, err := self.recordingTemporarySpeakerAppearances(source.ID, mergedSpeaker)
		if err != nil {
			return err
		}
		appearances = append(appearances, withMergedSpeakerID(mergedAppearances, mergedSpeaker.ID)...)
	}
	if err := self.writeTemporarySpeakerCorrections(userId, source, speaker.ID, appearances, confirmedName, isAccountOwner); err != nil {
		return err
	}
	var identity recordingSpeakerIdentityModel
	if err := self.tx.Where("user_id = ? AND confirmed_name = ?", userId, confirmedName).Take(&identity).Error; err != nil {
		return err
	}
	if err := self.tx.Model(&recordingTemporarySpeakerModel{}).Where("id = ?", speaker.ID).Update("speaker_identity_id", identity.ID).Error; err != nil {
		return err
	}
	if contactId == "" {
		return nil
	}
	return self.tieRecordingSpeakerToContact(userId, identity.ID, addressBookId, contactId)
}

// recordingSpeakerAppearance is one piece a temporary speaker was heard in and
// the label that piece's provider answer gave it.
type recordingSpeakerAppearance struct {
	RecordingTranscriptionAttemptID string
	LocalSpeakerID                  string
	// MergedSpeakerID is the label merged into the one that carries the name,
	// empty for the appearances of the speaker that carries it.
	MergedSpeakerID string
}

func (self *transaction) recordingTemporarySpeakerNamed(recordingId, speakerLabel string) (*recordingTemporarySpeakerModel, error) {
	var speaker recordingTemporarySpeakerModel
	if err := self.tx.Where("recording_id = ? AND speaker_label = ?", recordingId, speakerLabel).Take(&speaker).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &speaker, nil
}

// recordingTemporarySpeakerRoot follows the speakers a label was merged into to
// the one that was not merged into another. A merge points at the root, so the
// chain is normally one step; the walk is bounded in case a row was edited by hand.
func (self *transaction) recordingTemporarySpeakerRoot(speaker *recordingTemporarySpeakerModel) (*recordingTemporarySpeakerModel, error) {
	for range recordingTemporarySpeakerLimit {
		if speaker.MergedIntoSpeakerID == nil {
			return speaker, nil
		}
		var target recordingTemporarySpeakerModel
		if err := self.tx.Where("id = ?", *speaker.MergedIntoSpeakerID).Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		speaker = &target
	}
	return nil, ErrInvalidArguments
}

// recordingTemporarySpeakerAppearances lists every piece the speaker was heard
// in: the one its sample was cut from, under the label that piece gave it, and
// each piece it was sent to, under the temporary label the provider answered with.
func (self *transaction) recordingTemporarySpeakerAppearances(recordingId string, speaker *recordingTemporarySpeakerModel) ([]recordingSpeakerAppearance, error) {
	var appearances []recordingSpeakerAppearance
	if err := self.tx.Raw(`SELECT DISTINCT attempt.id AS recording_transcription_attempt_id, segment.local_speaker_id
  FROM recording_transcript_run run
  JOIN recording_transcription_attempt attempt ON attempt.recording_transcript_run_id = run.id
  JOIN recording_transcript_segment segment ON segment.recording_transcription_attempt_id = attempt.id
  WHERE run.recording_id = @recordingId AND attempt.recording_attempt_state = @complete AND (
   (attempt.id = @sourceAttemptId AND segment.local_speaker_id = @sourceLocalSpeakerId)
   OR (segment.local_speaker_id = @speakerLabel AND attempt.reference_blob_ids @> jsonb_build_array(CAST(@blobId AS text))))
  ORDER BY attempt.id, segment.local_speaker_id`,
		map[string]any{"recordingId": recordingId, "complete": models.RecordingAttemptComplete, "sourceAttemptId": speaker.SourceAttemptID, "sourceLocalSpeakerId": speaker.SourceLocalSpeakerID, "speakerLabel": speaker.SpeakerLabel, "blobId": optionalString(speaker.RecordingBlobID)}).Scan(&appearances).Error; err != nil {
		return nil, err
	}
	return appearances, nil
}

func withMergedSpeakerID(appearances []recordingSpeakerAppearance, mergedSpeakerId string) []recordingSpeakerAppearance {
	for index := range appearances {
		appearances[index].MergedSpeakerID = mergedSpeakerId
	}
	return appearances
}

// writeTemporarySpeakerCorrections names the appearances with the name, each
// as the ordinary correction of its piece. The corrections are keyed by the
// speaker that carries the name, so undoing that speaker's naming finds the
// ones written for the labels merged into it as well.
func (self *transaction) writeTemporarySpeakerCorrections(userId string, source *models.Recording, namedSpeakerId string, appearances []recordingSpeakerAppearance, confirmedName string, isAccountOwner bool) error {
	nameDigest := sha256.Sum256([]byte(confirmedName))
	// The account and the recording are locked by the caller, so each piece is corrected
	// without taking the locks again. Each correction advances the generation.
	for _, appearance := range appearances {
		mergedKey := ""
		if appearance.MergedSpeakerID != "" {
			mergedKey = "-" + appearance.MergedSpeakerID
		}
		if err := self.tx.Raw("SELECT source_generation FROM recording WHERE id = ?", source.ID).Scan(&source.SourceGeneration).Error; err != nil {
			return err
		}
		if _, err := self.correctRecordingSpeakerLocked(userId, source, RecordingSpeakerCorrectionInput{
			RecordingID: source.ID, RecordingTranscriptionAttemptID: appearance.RecordingTranscriptionAttemptID, LocalSpeakerID: appearance.LocalSpeakerID,
			// The key names the speaker, the piece and the name, so repeating a request is harmless and renaming is a new correction.
			// A label merged into the speaker adds its own ID, since two labels of one piece would otherwise share a key.
			ClientCorrectionID: temporarySpeakerKeyPrefix + namedSpeakerId + "-" + appearance.RecordingTranscriptionAttemptID + mergedKey + "-" + hex.EncodeToString(nameDigest[:4]),
			ConfirmedName:      confirmedName, IsAccountOwner: isAccountOwner, ExpectedSourceGeneration: source.SourceGeneration, PassageStartMS: -1, PassageEndMS: -1,
		}); err != nil {
			return err
		}
	}
	return nil
}

// MergeRecordingTemporarySpeaker makes a label read as the voice of an earlier
// one: the provider said they are the same person. The label's passages show
// the earlier label, or its name once it has one, and its sample is no longer
// sent. When the earlier speaker is already named, its passages read as that
// name through the projection, as recognized rather than confirmed: the
// provider made the merge, not the person, so no correction is written for it.
func (self *transaction) MergeRecordingTemporarySpeaker(userId, recordingId, speakerLabel, intoSpeakerLabel string) error {
	if speakerLabel == "" || intoSpeakerLabel == "" || speakerLabel == intoSpeakerLabel {
		return ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	if err := self.lockRecordingReferenceDependents(userId, recordingId); err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	speaker, err := self.recordingTemporarySpeakerNamed(source.ID, speakerLabel)
	if err != nil {
		return err
	}
	target, err := self.recordingTemporarySpeakerNamed(source.ID, intoSpeakerLabel)
	if err != nil {
		return err
	}
	// A named speaker keeps its name and a merged one is already someone's.
	if speaker.SpeakerIdentityID != nil || speaker.MergedIntoSpeakerID != nil {
		return ErrInvalidArguments
	}
	root, err := self.recordingTemporarySpeakerRoot(target)
	if err != nil {
		return err
	}
	if root.ID == speaker.ID {
		return ErrInvalidArguments
	}
	// Whatever was merged into this speaker follows it, so a pointer never has a chain of two.
	if err := self.tx.Model(&recordingTemporarySpeakerModel{}).Where("id = ? OR merged_into_temporary_speaker_id = ?", speaker.ID, speaker.ID).Update("merged_into_temporary_speaker_id", root.ID).Error; err != nil {
		return err
	}
	return self.advanceRecordingSpeakerGeneration(source.ID)
}

// SeparateRecordingTemporarySpeaker undoes a merge: the label reads as its own
// speaker again, and the corrections the merge wrote for it under the earlier
// speaker's name are removed. The earlier speaker's own naming is left alone.
func (self *transaction) SeparateRecordingTemporarySpeaker(userId, recordingId, speakerLabel string) error {
	if speakerLabel == "" {
		return ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	if err := self.lockRecordingReferenceDependents(userId, recordingId); err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	speaker, err := self.recordingTemporarySpeakerNamed(source.ID, speakerLabel)
	if err != nil {
		return err
	}
	if speaker.MergedIntoSpeakerID == nil {
		return ErrInvalidArguments
	}
	appearances, err := self.recordingTemporarySpeakerAppearances(source.ID, speaker)
	if err != nil {
		return err
	}
	for _, appearance := range appearances {
		// Only the corrections the merge wrote carry the earlier speaker's key.
		if err := self.tx.Model(&recordingSpeakerCorrectionModel{}).
			Where("recording_id = ? AND NOT is_removed AND recording_transcription_attempt_id = ? AND local_speaker_id = ? AND client_correction_id LIKE ?", source.ID, appearance.RecordingTranscriptionAttemptID, appearance.LocalSpeakerID, temporarySpeakerKeyPrefix+*speaker.MergedIntoSpeakerID+"-%").
			Update("is_removed", true).Error; err != nil {
			return err
		}
	}
	if err := self.tx.Model(&recordingTemporarySpeakerModel{}).Where("id = ?", speaker.ID).Update("merged_into_temporary_speaker_id", nil).Error; err != nil {
		return err
	}
	return self.advanceRecordingSpeakerGeneration(source.ID)
}

// MarkRecordingTemporarySpeakerMergeChecked claims the provider's check of
// whether the speaker is an earlier one, so it is asked once: the first call
// wins and a later one, from another curation running at the same time, gets
// ErrAlreadyExists.
func (self *transaction) MarkRecordingTemporarySpeakerMergeChecked(userId, speakerId string) error {
	var recordingId string
	if err := self.tx.Raw(`SELECT recording.id FROM recording_temporary_speaker speaker JOIN recording ON recording.id = speaker.recording_id WHERE speaker.id = ? AND recording.user_id = ? AND recording.deleted_at IS NULL`, speakerId, userId).Scan(&recordingId).Error; err != nil {
		return err
	}
	if recordingId == "" {
		return ErrNotFound
	}
	claimed := self.tx.Exec(`UPDATE recording_temporary_speaker SET merge_checked_at = ? WHERE id = ? AND merge_checked_at IS NULL`, time.Now().UTC().Truncate(time.Microsecond), speakerId)
	if claimed.Error != nil {
		return claimed.Error
	}
	if claimed.RowsAffected == 0 {
		return ErrAlreadyExists
	}
	return nil
}

// temporarySpeakerKeyPrefix starts the client correction ID of every correction
// written by naming a temporary speaker: the prefix, the speaker's ID, the
// piece's attempt ID, the ID of the label merged into the speaker when the
// correction is for that label's passages, then the first bytes of the name's
// digest in hex.
const temporarySpeakerKeyPrefix = "temporary-speaker-"

// TemporarySpeakerNamingOf reads the temporary speaker's ID and the name digest
// out of a client correction ID, or returns empty strings when the key did not
// come from naming a temporary speaker.
func TemporarySpeakerNamingOf(clientCorrectionId string) (string, string) {
	rest, isTemporary := strings.CutPrefix(clientCorrectionId, temporarySpeakerKeyPrefix)
	if !isTemporary {
		return "", ""
	}
	// IDs are ULIDs and the digest is hex, so none of the parts holds a hyphen.
	parts := strings.Split(rest, "-")
	if len(parts) != 3 && len(parts) != 4 || slices.Contains(parts, "") {
		return "", ""
	}
	return parts[0], parts[len(parts)-1]
}
