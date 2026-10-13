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
	isOwnerByIdentityID := map[string]bool{}
	for _, identity := range identities {
		confirmedNames[identity.ID] = identity.ConfirmedName
		isOwnerByIdentityID[identity.ID] = identity.IsAccountOwner
	}
	transcriptRuns := map[string]*models.RecordingTranscriptRun{}
	recognizedByAttemptID := map[string]map[string]string{}
	correctionsByRecording := map[string][]*models.RecordingSpeakerCorrection{}
	for _, segment := range segments {
		if segment == nil {
			return ErrInvalidArguments
		}
		segment.EffectiveSpeakerIdentityID = ""
		segment.EffectiveSpeakerName = segment.LocalSpeakerID
		segment.HasUserConfirmedSpeaker = false
		segment.IsRecognizedSpeaker = false
		segment.IsAccountOwner = false
		segment.HasUnalignedSpeakerCorrection = false
		segment.AppliedSpeakerCorrectionID = ""
		recognized, isRead := recognizedByAttemptID[segment.RecordingTranscriptionAttemptID]
		if !isRead {
			var err error
			if recognized, err = self.recognizedRecordingSpeakers(userId, segment.RecordingTranscriptionAttemptID, confirmedNames); err != nil {
				return err
			}
			recognizedByAttemptID[segment.RecordingTranscriptionAttemptID] = recognized
		}
		// A speaker the transcription named after a voice sample it was sent
		// is that sample's identity, until the person says otherwise below.
		if identityId := recognized[segment.LocalSpeakerID]; identityId != "" {
			segment.EffectiveSpeakerIdentityID = identityId
			segment.EffectiveSpeakerName = confirmedNames[identityId]
			segment.IsRecognizedSpeaker = true
			segment.IsAccountOwner = isOwnerByIdentityID[identityId]
		}
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
			segment.IsRecognizedSpeaker = false
			segment.IsAccountOwner = false
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
			segment.IsAccountOwner = isOwnerByIdentityID[correction.SpeakerIdentityID]
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

// recognizedRecordingSpeakers maps the speaker labels of one transcription
// attempt to the identities whose voice samples that attempt sent: the
// provider labels a speaker it matched to a sample with the name the sample
// was sent under, which is the identity's confirmed name.
func (self *transaction) recognizedRecordingSpeakers(userId, attemptId string, confirmedNames map[string]string) (map[string]string, error) {
	var attempt recordingAttemptModel
	if err := self.tx.Select("id,reference_blob_ids").Where("id = ?", attemptId).Take(&attempt).Error; err != nil {
		return nil, err
	}
	if len(attempt.ReferenceBlobIDs) == 0 {
		return nil, nil
	}
	var identityIds []string
	if err := self.tx.Model(&recordingVoiceReferenceModel{}).
		Joins("JOIN recording ON recording.id = recording_voice_reference.recording_id").
		Where("recording.user_id = ? AND recording_voice_reference.recording_blob_id IN ?", userId, attempt.ReferenceBlobIDs).
		Distinct().Pluck("recording_voice_reference.speaker_identity_id", &identityIds).Error; err != nil {
		return nil, err
	}
	recognized := map[string]string{}
	for _, identityId := range identityIds {
		if name := confirmedNames[identityId]; name != "" {
			recognized[name] = identityId
		}
	}
	return recognized, nil
}
