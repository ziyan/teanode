package db

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
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

	// ApplyRecordingGlossaryCorrections makes the fixes the term correction
	// pass proposed that are still allowed, and answers with the ones it made.
	ApplyRecordingGlossaryCorrections(userId, recordingId string, proposals []RecordingGlossaryCorrectionInput) ([]*models.RecordingTranscriptTextCorrection, error)
	ProjectRecordingTranscriptTextCorrections(userId string, segments []*models.RecordingTranscriptSegment) error
}

type RecordingTranscriptTextCorrectionInput struct {
	RecordingID                  string
	RecordingTranscriptSegmentID string
	ClientCorrectionID           string
	CorrectedTranscriptText      string
	ExpectedSourceGeneration     int64
}

// RecordingGlossaryCorrectionInput is one fix the term correction pass proposes
// for a passage: OriginalPhrase, found in the passage's current text, becomes
// CorrectedPhrase, which spells GlossaryTerm.
type RecordingGlossaryCorrectionInput struct {
	RecordingTranscriptSegmentID string
	OriginalPhrase               string
	CorrectedPhrase              string
	GlossaryTerm                 string
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
	row := recordingTextCorrectionModel{ID: newID(), RecordingID: source.ID, RecordingMediaID: transcriptRun.RecordingMediaID, RecordingTranscriptRunID: transcriptRun.ID, RecordingTranscriptSegmentID: segment.ID, ClientCorrectionID: input.ClientCorrectionID, PassageStartMS: segment.SegmentStartMS, PassageEndMS: segment.SegmentEndMS, OriginalTranscriptText: segment.TranscriptText, CorrectedTranscriptText: input.CorrectedTranscriptText, SourceGeneration: source.SourceGeneration + 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), TextCorrectionOrigin: models.TextCorrectionOriginPerson}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	// A person's words win: a fix the glossary made to this passage is
	// replaced by what they wrote.
	if err := self.supersedeRecordingGlossaryCorrections(source.ID, transcriptRun.RecordingMediaID, segment.ID, segment.SegmentStartMS, segment.SegmentEndMS, false); err != nil {
		return nil, err
	}
	if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
		return nil, err
	}
	if err := self.QueueRecordingKnowledgeSync(userId, source.ID); err != nil {
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
	// Taking a glossary fix away restores the recognized text, and with it
	// the earlier fixes of the passage that this one had folded into itself.
	// The person has undone them as well.
	if row.TextCorrectionOrigin == models.TextCorrectionOriginGlossary {
		if err := self.tx.Model(&recordingTextCorrectionModel{}).
			Where("recording_id = ? AND text_correction_origin = ? AND is_folded AND source_generation < ? AND "+recordingTextCorrectionOverlapCondition,
				append([]any{recordingId, models.TextCorrectionOriginGlossary, row.SourceGeneration}, recordingTextCorrectionOverlapArguments(row.RecordingTranscriptSegmentID, row.RecordingMediaID, row.PassageStartMS, row.PassageEndMS)...)...).
			Update("is_folded", false).Error; err != nil {
			return err
		}
	}
	if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
		return err
	}
	return self.QueueRecordingKnowledgeSync(userId, source.ID)
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
		if segment.AccurateTranscriptText != "" {
			segment.EffectiveTranscriptText = segment.AccurateTranscriptText
		}
		segment.AppliedTextCorrectionID = ""
		segment.AppliedTextCorrectionOrigin = ""
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
			segment.AppliedTextCorrectionOrigin = selected.TextCorrectionOrigin
		}
		segment.IsFiller = models.IsFillerTranscriptText(segment.EffectiveTranscriptText)
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

// recordingTextCorrectionOverlapCondition selects the corrections of one
// passage in any transcript version of the recording: the same segment, or a
// segment of the same media whose interval overlaps (or equals) the passage's.
const recordingTextCorrectionOverlapCondition = "(recording_transcript_segment_id = ? OR (recording_media_id = ? AND ((passage_start_ms < ? AND passage_end_ms > ?) OR (passage_start_ms = ? AND passage_end_ms = ?))))"

func recordingTextCorrectionOverlapArguments(segmentId, recordingMediaId string, segmentStartMS, segmentEndMS int64) []any {
	return []any{segmentId, recordingMediaId, segmentEndMS, segmentStartMS, segmentStartMS, segmentEndMS}
}

// supersedeRecordingGlossaryCorrections marks the glossary fixes on a passage,
// by segment or by the same interval of the same media in another version, as
// removed. Their source generation stays as it was, so the fix or the person's
// words written next sort newer than they do. A removed glossary fix is also
// what stops the pass from making the same fix again, unless isFolded says it
// was removed only because a later fix of the passage took it in.
func (self *transaction) supersedeRecordingGlossaryCorrections(recordingId, recordingMediaId, segmentId string, segmentStartMS, segmentEndMS int64, isFolded bool) error {
	return self.tx.Model(&recordingTextCorrectionModel{}).
		Where("recording_id = ? AND text_correction_origin = ? AND NOT is_removed AND (recording_transcript_segment_id = ? OR (recording_media_id = ? AND passage_start_ms = ? AND passage_end_ms = ?))",
			recordingId, models.TextCorrectionOriginGlossary, segmentId, recordingMediaId, segmentStartMS, segmentEndMS).
		Updates(map[string]any{"is_removed": true, "is_folded": isFolded}).Error
}

// hasRecordingPersonCorrection says whether the person has corrected this
// passage, in this version or, by the same interval, in another.
func (self *transaction) hasRecordingPersonCorrection(recordingId, recordingMediaId, segmentId string, segmentStartMS, segmentEndMS int64) (bool, error) {
	var count int64
	err := self.tx.Model(&recordingTextCorrectionModel{}).
		Where("recording_id = ? AND text_correction_origin = ? AND NOT is_removed AND (recording_transcript_segment_id = ? OR (recording_media_id = ? AND passage_start_ms = ? AND passage_end_ms = ?))",
			recordingId, models.TextCorrectionOriginPerson, segmentId, recordingMediaId, segmentStartMS, segmentEndMS).Count(&count).Error
	return count > 0, err
}

// isRecordingGlossaryCorrectionRejected says whether the person undid a
// glossary fix for this term on this passage before: one removed, not folded
// into a later fix, with the same glossary term on the same segment or an
// overlapping interval of the same media. The words of the fix do not matter,
// so a rerun that reads the misheard words a little differently ("T node is"
// after "T node") does not bring back what was undone. Removed because the
// person rewrote the passage counts as undone too.
func (self *transaction) isRecordingGlossaryCorrectionRejected(recordingId, recordingMediaId, segmentId string, segmentStartMS, segmentEndMS int64, glossaryTerm string) (bool, error) {
	var count int64
	err := self.tx.Model(&recordingTextCorrectionModel{}).
		Where("recording_id = ? AND text_correction_origin = ? AND is_removed AND NOT is_folded AND LOWER(glossary_term) = LOWER(?) AND "+recordingTextCorrectionOverlapCondition,
			append([]any{recordingId, models.TextCorrectionOriginGlossary, glossaryTerm}, recordingTextCorrectionOverlapArguments(segmentId, recordingMediaId, segmentStartMS, segmentEndMS)...)...).Count(&count).Error
	return count > 0, err
}

func (self *transaction) ApplyRecordingGlossaryCorrections(userId, recordingId string, proposals []RecordingGlossaryCorrectionInput) ([]*models.RecordingTranscriptTextCorrection, error) {
	if len(proposals) == 0 {
		return nil, nil
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return nil, err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return nil, err
	}
	latestRunIds := self.tx.Model(&recordingRunModel{}).Select("DISTINCT ON (recording_media_id) id").Where("recording_id = ? AND recording_processing_state = ?", recordingId, models.RecordingProcessingComplete).Order("recording_media_id, version_number DESC")
	// Each fix takes the next source generation, so that of two fixes to one
	// passage the later sorts newer and is the one that reads through.
	generation := source.SourceGeneration
	var applied []*models.RecordingTranscriptTextCorrection
	for _, proposal := range proposals {
		if proposal.OriginalPhrase == "" || proposal.CorrectedPhrase == "" || proposal.GlossaryTerm == "" || proposal.OriginalPhrase == proposal.CorrectedPhrase {
			continue
		}
		// The segment must belong to the latest finished version of this recording.
		var segment recordingSegmentModel
		if err := self.tx.Where("id = ? AND recording_transcript_run_id IN (?)", proposal.RecordingTranscriptSegmentID, latestRunIds).Take(&segment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		transcriptRun, err := self.GetRecordingTranscriptRun(userId, segment.RecordingTranscriptRunID)
		if err != nil {
			return nil, err
		}
		hasPerson, err := self.hasRecordingPersonCorrection(source.ID, transcriptRun.RecordingMediaID, segment.ID, segment.SegmentStartMS, segment.SegmentEndMS)
		if err != nil {
			return nil, err
		}
		if hasPerson {
			continue
		}
		isRejected, err := self.isRecordingGlossaryCorrectionRejected(source.ID, transcriptRun.RecordingMediaID, segment.ID, segment.SegmentStartMS, segment.SegmentEndMS, proposal.GlossaryTerm)
		if err != nil {
			return nil, err
		}
		if isRejected {
			continue
		}
		// The phrase is looked for in what the passage reads as now, which
		// may already carry an earlier fix of this pass.
		projected := []*models.RecordingTranscriptSegment{(*models.RecordingTranscriptSegment)(&segment)}
		if err := self.ProjectRecordingTranscriptTextCorrections(userId, projected); err != nil {
			return nil, err
		}
		effectiveText := projected[0].EffectiveTranscriptText
		correctedText, replacedCount := models.ReplaceWholePhrase(effectiveText, proposal.OriginalPhrase, proposal.CorrectedPhrase)
		if replacedCount == 0 || len(correctedText) > 65536 {
			continue
		}
		// An earlier fix of this passage is folded into the new one, which
		// starts from the text that fix produced. Folded is not undone.
		if err := self.supersedeRecordingGlossaryCorrections(source.ID, transcriptRun.RecordingMediaID, segment.ID, segment.SegmentStartMS, segment.SegmentEndMS, true); err != nil {
			return nil, err
		}
		row := recordingTextCorrectionModel{ID: newID(), RecordingID: source.ID, RecordingMediaID: transcriptRun.RecordingMediaID, RecordingTranscriptRunID: transcriptRun.ID, RecordingTranscriptSegmentID: segment.ID, ClientCorrectionID: glossaryClientCorrectionID(segment.ID, proposal, generation+1), PassageStartMS: segment.SegmentStartMS, PassageEndMS: segment.SegmentEndMS, OriginalTranscriptText: segment.TranscriptText, CorrectedTranscriptText: correctedText, SourceGeneration: generation + 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), TextCorrectionOrigin: models.TextCorrectionOriginGlossary, GlossaryTerm: proposal.GlossaryTerm, OriginalPhrase: proposal.OriginalPhrase, CorrectedPhrase: proposal.CorrectedPhrase}
		if err := self.tx.Create(&row).Error; err != nil {
			return nil, err
		}
		if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
			return nil, err
		}
		generation++
		applied = append(applied, (*models.RecordingTranscriptTextCorrection)(&row))
	}
	if len(applied) == 0 {
		return nil, nil
	}
	if err := self.QueueRecordingKnowledgeSync(userId, source.ID); err != nil {
		return nil, err
	}
	return applied, nil
}

// glossaryClientCorrectionID is a stable name for one fix, unique among the
// recording's corrections, so a pass repeated against the same text names the
// same fix the same way.
func glossaryClientCorrectionID(segmentId string, proposal RecordingGlossaryCorrectionInput, sourceGeneration int64) string {
	digest := sha256.Sum256([]byte(proposal.OriginalPhrase + "\x00" + proposal.CorrectedPhrase + "\x00" + strconv.FormatInt(sourceGeneration, 10)))
	return "glossary-" + segmentId + "-" + hex.EncodeToString(digest[:8])
}
