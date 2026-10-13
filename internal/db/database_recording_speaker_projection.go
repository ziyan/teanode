package db

import (
	"fmt"
	"github.com/ziyan/teanode/internal/models"
)

// ProjectRecordingSpeakerCorrections uses original request-local bindings before interval alignment.
func (self *transaction) ProjectRecordingSpeakerCorrections(userId string, segments []*models.RecordingTranscriptSegment) error {
	identities, err := self.ListRecordingSpeakerIdentities(userId)
	if err != nil {
		return err
	}
	confirmedNames := map[string]string{}
	for _, identity := range identities {
		confirmedNames[identity.ID] = identity.ConfirmedName
	}
	transcriptRuns := map[string]*models.RecordingTranscriptRun{}
	correctionsByRecording := map[string][]*models.RecordingSpeakerCorrection{}
	for _, segment := range segments {
		if segment == nil {
			return ErrInvalidArguments
		}
		segment.EffectiveSpeakerIdentityID = ""
		segment.EffectiveSpeakerName = segment.LocalSpeakerID
		segment.HasUserConfirmedSpeaker = false
		segment.HasUnalignedSpeakerCorrection = false
		segment.AppliedSpeakerCorrectionID = ""
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
			corrections, err = self.ListRecordingSpeakerCorrections(userId, transcriptRun.RecordingID)
			if err != nil {
				return err
			}
			correctionsByRecording[transcriptRun.RecordingID] = corrections
		}
		latestGeneration := int64(-1)
		for _, correction := range corrections {
			isMatched, isUnaligned, err := self.recordingSpeakerCorrectionMatchesSegment(correction, transcriptRun, segment)
			if err != nil {
				return err
			}
			if isMatched && segment.RecordingTranscriptionAttemptID != correction.RecordingTranscriptionAttemptID && hasCompetingRecordingSpeakerCorrections(correction, corrections) {
				isMatched, isUnaligned = false, true
			}
			if isUnaligned {
				segment.HasUnalignedSpeakerCorrection = true
			}
			if !isMatched || correction.SourceGeneration < latestGeneration {
				continue
			}
			latestGeneration = correction.SourceGeneration
			segment.EffectiveSpeakerIdentityID = ""
			segment.EffectiveSpeakerName = segment.LocalSpeakerID
			segment.HasUserConfirmedSpeaker = false
			segment.AppliedSpeakerCorrectionID = ""
			if correction.PassageStartMS > segment.SegmentStartMS || correction.PassageEndMS < segment.SegmentEndMS {
				segment.HasUnalignedSpeakerCorrection = true
				continue
			}
			if correction.IsRemoved {
				continue
			}
			segment.EffectiveSpeakerIdentityID = correction.SpeakerIdentityID
			segment.EffectiveSpeakerName = confirmedNames[correction.SpeakerIdentityID]
			segment.HasUserConfirmedSpeaker = true
			segment.AppliedSpeakerCorrectionID = correction.ID
		}
	}
	return nil
}

// Request-local IDs never cross an attempt. Later versions additionally require
// one exact source passage and one exact destination passage, including overlap checks.
func (self *transaction) recordingSpeakerCorrectionMatchesSegment(correction *models.RecordingSpeakerCorrection, transcriptRun *models.RecordingTranscriptRun, segment *models.RecordingTranscriptSegment) (bool, bool, error) {
	if correction.RecordingMediaID != transcriptRun.RecordingMediaID || correction.PassageStartMS >= segment.SegmentEndMS || correction.PassageEndMS <= segment.SegmentStartMS {
		return false, false, nil
	}
	var originalAttempt recordingAttemptModel
	if err := self.tx.Select("id,recording_transcript_run_id").Where("id = ?", correction.RecordingTranscriptionAttemptID).Take(&originalAttempt).Error; err != nil {
		return false, false, err
	}
	if originalAttempt.RecordingTranscriptRunID == transcriptRun.ID {
		return segment.RecordingTranscriptionAttemptID == correction.RecordingTranscriptionAttemptID && segment.LocalSpeakerID == correction.LocalSpeakerID, false, nil
	}
	var originalPassages []*recordingSegmentModel
	if err := self.tx.Where("recording_transcription_attempt_id = ? AND local_speaker_id = ? AND segment_start_ms <= ? AND segment_end_ms >= ?", correction.RecordingTranscriptionAttemptID, correction.LocalSpeakerID, correction.PassageStartMS, correction.PassageEndMS).Limit(2).Find(&originalPassages).Error; err != nil {
		return false, false, err
	}
	if len(originalPassages) != 1 || originalPassages[0].SegmentStartMS != segment.SegmentStartMS || originalPassages[0].SegmentEndMS != segment.SegmentEndMS {
		return false, true, nil
	}
	for _, transcriptRunId := range []string{originalAttempt.RecordingTranscriptRunID, transcriptRun.ID} {
		var passageCount int64
		if err := self.tx.Model(&recordingSegmentModel{}).Where("recording_transcript_run_id = ? AND segment_start_ms = ? AND segment_end_ms = ?", transcriptRunId, segment.SegmentStartMS, segment.SegmentEndMS).Count(&passageCount).Error; err != nil {
			return false, false, err
		}
		if passageCount != 1 {
			return false, true, nil
		}
	}
	return true, false, nil
}

func hasCompetingRecordingSpeakerCorrections(correction *models.RecordingSpeakerCorrection, corrections []*models.RecordingSpeakerCorrection) bool {
	seenBindings := map[string]bool{}
	for correctionIndex := len(corrections) - 1; correctionIndex >= 0; correctionIndex-- {
		candidate := corrections[correctionIndex]
		bindingKey := candidate.RecordingTranscriptionAttemptID + ":" + candidate.LocalSpeakerID + ":" + fmt.Sprint(candidate.PassageStartMS) + ":" + fmt.Sprint(candidate.PassageEndMS)
		if seenBindings[bindingKey] {
			continue
		}
		seenBindings[bindingKey] = true
		if !candidate.IsRemoved && candidate.RecordingMediaID == correction.RecordingMediaID && candidate.PassageStartMS == correction.PassageStartMS && candidate.PassageEndMS == correction.PassageEndMS && candidate.SpeakerIdentityID != correction.SpeakerIdentityID && (candidate.RecordingTranscriptionAttemptID != correction.RecordingTranscriptionAttemptID || candidate.LocalSpeakerID != correction.LocalSpeakerID) {
			return true
		}
	}
	return false
}
