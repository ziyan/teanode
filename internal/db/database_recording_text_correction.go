package db

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
)

// RecordingTextCorrectionOperation keeps original recognized speech immutable.
type RecordingTextCorrectionOperation interface {
	ListRecordingTranscriptTextCorrections(userId, recordingId string) ([]*models.RecordingTranscriptTextCorrection, error)
	CorrectRecordingTranscriptText(userId string, input RecordingTranscriptTextCorrectionInput) (*models.RecordingTranscriptTextCorrection, error)
	RemoveRecordingTranscriptTextCorrection(userId, recordingId, correctionId string, expectedSourceGeneration int64) error
	ProjectRecordingTranscriptTextCorrections(userId string, segments []*models.RecordingTranscriptSegment) error
}

type RecordingTranscriptTextCorrectionInput struct {
	RecordingID                  string
	RecordingTranscriptSegmentID string
	ClientCorrectionID           string
	CorrectedTranscriptText      string
	ExpectedSourceGeneration     int64
}

type recordingTextCorrectionModel models.RecordingTranscriptTextCorrection

func (recordingTextCorrectionModel) TableName() string { return "recording_transcript_text_correction" }

func (self *transaction) ListRecordingTranscriptTextCorrections(userId, recordingId string) ([]*models.RecordingTranscriptTextCorrection, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []*recordingTextCorrectionModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("source_generation DESC,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	corrections := make([]*models.RecordingTranscriptTextCorrection, 0, len(rows))
	for _, row := range rows {
		corrections = append(corrections, (*models.RecordingTranscriptTextCorrection)(row))
	}
	return corrections, nil
}
func (self *transaction) CorrectRecordingTranscriptText(userId string, input RecordingTranscriptTextCorrectionInput) (*models.RecordingTranscriptTextCorrection, error) {
	if (strings.TrimSpace(input.ClientCorrectionID) == "" || len(input.ClientCorrectionID) > 128) || strings.TrimSpace(input.CorrectedTranscriptText) == "" || len(input.CorrectedTranscriptText) > 65536 || !utf8.ValidString(input.CorrectedTranscriptText) || strings.ContainsRune(input.CorrectedTranscriptText, 0) {
		return nil, ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return nil, err
	}
	source, err := self.LockRecording(userId, input.RecordingID)
	if err != nil {
		return nil, err
	}
	var repeated recordingTextCorrectionModel
	err = self.tx.Where("recording_id = ? AND client_correction_id = ?", source.ID, input.ClientCorrectionID).Take(&repeated).Error
	if err == nil {
		if repeated.RecordingTranscriptSegmentID != input.RecordingTranscriptSegmentID || repeated.CorrectedTranscriptText != input.CorrectedTranscriptText {
			return nil, ErrAlreadyExists
		}
		return (*models.RecordingTranscriptTextCorrection)(&repeated), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if source.SourceGeneration != input.ExpectedSourceGeneration {
		return nil, ErrAlreadyExists
	}
	var segment recordingSegmentModel
	if err := self.tx.Where("id = ?", input.RecordingTranscriptSegmentID).Take(&segment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, segment.RecordingTranscriptRunID)
	if err != nil {
		return nil, err
	}
	if transcriptRun.RecordingID != source.ID {
		return nil, ErrNotFound
	}
	row := recordingTextCorrectionModel{ID: newID(), RecordingID: source.ID, RecordingMediaID: transcriptRun.RecordingMediaID, RecordingTranscriptRunID: transcriptRun.ID, RecordingTranscriptSegmentID: segment.ID, ClientCorrectionID: input.ClientCorrectionID, PassageStartMS: segment.SegmentStartMS, PassageEndMS: segment.SegmentEndMS, OriginalTranscriptText: segment.TranscriptText, CorrectedTranscriptText: input.CorrectedTranscriptText, SourceGeneration: source.SourceGeneration + 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
		return nil, err
	}
	if err := self.SyncRecordingKnowledge(userId, source.ID); err != nil {
		return nil, err
	}
	return (*models.RecordingTranscriptTextCorrection)(&row), nil
}
func (self *transaction) RemoveRecordingTranscriptTextCorrection(userId, recordingId, correctionId string, expectedSourceGeneration int64) error {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	var row recordingTextCorrectionModel
	if err := self.tx.Where("id = ? AND recording_id = ?", correctionId, recordingId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if row.IsRemoved {
		return nil
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return ErrAlreadyExists
	}
	if err := self.tx.Model(&row).Updates(map[string]any{"is_removed": true, "source_generation": source.SourceGeneration + 1}).Error; err != nil {
		return err
	}
	if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
		return err
	}
	return self.SyncRecordingKnowledge(userId, source.ID)
}

// Exact original IDs preserve edits. A later version requires one exact interval;
// overlapping equal-time passages or changed boundaries never inherit guessed text.
func (self *transaction) ProjectRecordingTranscriptTextCorrections(userId string, segments []*models.RecordingTranscriptSegment) error {
	transcriptRuns := map[string]*models.RecordingTranscriptRun{}
	correctionsByRecording := map[string][]*models.RecordingTranscriptTextCorrection{}
	for _, segment := range segments {
		if segment == nil {
			return ErrInvalidArguments
		}
		segment.EffectiveTranscriptText = segment.TranscriptText
		segment.AppliedTextCorrectionID = ""
		segment.HasUnalignedTextCorrection = false
		transcriptRun := transcriptRuns[segment.RecordingTranscriptRunID]
		if transcriptRun == nil {
			var err error
			transcriptRun, err = self.GetRecordingTranscriptRun(userId, segment.RecordingTranscriptRunID)
			if err != nil {
				return err
			}
			transcriptRuns[transcriptRun.ID] = transcriptRun
		}
		corrections, hasCorrections := correctionsByRecording[transcriptRun.RecordingID]
		if !hasCorrections {
			var err error
			corrections, err = self.ListRecordingTranscriptTextCorrections(userId, transcriptRun.RecordingID)
			if err != nil {
				return err
			}
			correctionsByRecording[transcriptRun.RecordingID] = corrections
		}
		var selected *models.RecordingTranscriptTextCorrection
		for _, correction := range corrections {
			if correction.RecordingTranscriptSegmentID == segment.ID {
				selected = correction
				break
			}
		}
		if selected == nil {
			for _, correction := range corrections {
				if correction.RecordingMediaID != transcriptRun.RecordingMediaID || correction.RecordingTranscriptRunID == transcriptRun.ID || correction.PassageStartMS != segment.SegmentStartMS || correction.PassageEndMS != segment.SegmentEndMS {
					continue
				}
				canProject, err := self.canProjectRecordingTextCorrection(correction, transcriptRun, corrections)
				if err != nil {
					return err
				}
				if canProject {
					selected = correction
				}
				break
			}
		}
		seenOriginalSegments := map[string]bool{}
		for _, correction := range corrections {
			if seenOriginalSegments[correction.RecordingTranscriptSegmentID] {
				continue
			}
			seenOriginalSegments[correction.RecordingTranscriptSegmentID] = true
			if correction.IsRemoved || correction.RecordingMediaID != transcriptRun.RecordingMediaID || correction.RecordingTranscriptRunID == transcriptRun.ID || correction.PassageStartMS >= segment.SegmentEndMS || correction.PassageEndMS <= segment.SegmentStartMS {
				continue
			}
			if selected == nil || correction.PassageStartMS != segment.SegmentStartMS || correction.PassageEndMS != segment.SegmentEndMS {
				segment.HasUnalignedTextCorrection = true
			}
		}
		if selected != nil && !selected.IsRemoved {
			segment.EffectiveTranscriptText = selected.CorrectedTranscriptText
			segment.AppliedTextCorrectionID = selected.ID
		}
	}
	return nil
}

func (self *transaction) canProjectRecordingTextCorrection(correction *models.RecordingTranscriptTextCorrection, transcriptRun *models.RecordingTranscriptRun, corrections []*models.RecordingTranscriptTextCorrection) (bool, error) {
	for _, transcriptRunId := range []string{correction.RecordingTranscriptRunID, transcriptRun.ID} {
		var intervalSegmentCount int64
		if err := self.tx.Model(&recordingSegmentModel{}).Where("recording_transcript_run_id = ? AND segment_start_ms = ? AND segment_end_ms = ?", transcriptRunId, correction.PassageStartMS, correction.PassageEndMS).Count(&intervalSegmentCount).Error; err != nil {
			return false, err
		}
		if intervalSegmentCount != 1 {
			return false, nil
		}
	}
	seenOriginalSegments := map[string]bool{}
	for _, candidate := range corrections {
		if seenOriginalSegments[candidate.RecordingTranscriptSegmentID] {
			continue
		}
		seenOriginalSegments[candidate.RecordingTranscriptSegmentID] = true
		if !candidate.IsRemoved && candidate.RecordingMediaID == correction.RecordingMediaID && candidate.PassageStartMS == correction.PassageStartMS && candidate.PassageEndMS == correction.PassageEndMS && candidate.RecordingTranscriptSegmentID != correction.RecordingTranscriptSegmentID {
			return false, nil
		}
	}
	return true, nil
}
