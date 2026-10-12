package db

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm/clause"
)

// Unaligned confirmations stay attached to their original request-local passage.
func (self *transaction) syncRecordingUnalignedSpeakerCorrections(userId string, recordingSource *models.Recording, agentId, sourceId string, latestRuns map[string]*models.RecordingTranscriptRun, corrections []*models.RecordingSpeakerCorrection, confirmedNames map[string]string) (bool, error) {
	seenBindings := map[string]bool{}
	hasChangedEvidence := false
	for correctionIndex := len(corrections) - 1; correctionIndex >= 0; correctionIndex-- {
		correction := corrections[correctionIndex]
		externalId := fmt.Sprintf("speaker-correction:%s:%s:%d:%d", correction.RecordingTranscriptionAttemptID, correction.LocalSpeakerID, correction.PassageStartMS, correction.PassageEndMS)
		if seenBindings[externalId] {
			continue
		}
		seenBindings[externalId] = true
		if correction.IsRemoved {
			continue
		}
		var originalAttempt recordingAttemptModel
		if err := self.tx.Select("id,recording_transcript_run_id").Where("id = ?", correction.RecordingTranscriptionAttemptID).Take(&originalAttempt).Error; err != nil {
			return false, err
		}
		latestRun := latestRuns[correction.RecordingMediaID]
		if latestRun != nil && latestRun.ID == originalAttempt.RecordingTranscriptRunID {
			continue
		}
		var originalPassages []*recordingSegmentModel
		if err := self.tx.Where("recording_transcription_attempt_id = ? AND local_speaker_id = ? AND segment_start_ms <= ? AND segment_end_ms >= ?", correction.RecordingTranscriptionAttemptID, correction.LocalSpeakerID, correction.PassageStartMS, correction.PassageEndMS).Limit(2).Find(&originalPassages).Error; err != nil {
			return false, err
		}
		if latestRun != nil && len(originalPassages) == 1 {
			var latestPassages []*recordingSegmentModel
			if err := self.tx.Where("recording_transcript_run_id = ? AND segment_start_ms = ? AND segment_end_ms = ?", latestRun.ID, originalPassages[0].SegmentStartMS, originalPassages[0].SegmentEndMS).Limit(2).Find(&latestPassages).Error; err != nil {
				return false, err
			}
			if len(latestPassages) == 1 {
				isMatched, _, err := self.recordingSpeakerCorrectionMatchesSegment(correction, latestRun, (*models.RecordingTranscriptSegment)(latestPassages[0]))
				if err != nil {
					return false, err
				}
				if isMatched && !hasCompetingRecordingSpeakerCorrections(correction, corrections) {
					continue
				}
			}
		}
		title := fmt.Sprintf("%s, original speaker confirmation, %d-%d MS, %s", recordingSource.RecordingTitle, correction.PassageStartMS, correction.PassageEndMS, confirmedNames[correction.SpeakerIdentityID])
		transcriptText := title + "\nThe user confirmed this original request-local speaker for the selected interval. Alignment to the latest generated transcript is uncertain. Do not attribute a merged passage or another generated speaker to this person."
		if len(originalPassages) == 1 {
			originalSegment := (*models.RecordingTranscriptSegment)(originalPassages[0])
			if err := self.ProjectRecordingTranscriptTextCorrections(userId, []*models.RecordingTranscriptSegment{originalSegment}); err != nil {
				return false, err
			}
			transcriptText += "\nOriginal segment evidence: " + originalSegment.EffectiveTranscriptText
			if correction.PassageStartMS > originalSegment.SegmentStartMS || correction.PassageEndMS < originalSegment.SegmentEndMS {
				transcriptText += "\nOnly the selected interval is confirmed. Text has segment-level timing; do not assign the whole statement to this selection."
			}
		} else {
			transcriptText += "\nOriginal passage wording cannot be uniquely resolved; consult generated version evidence."
		}
		if recordingSource.RecordedAt == nil {
			transcriptText += "\nMeeting occurrence time is unknown."
		} else {
			transcriptText += "\nMeeting recorded at " + recordingSource.RecordedAt.UTC().Format(time.RFC3339)
		}
		digest := sha256.Sum256([]byte(transcriptText))
		digestText := hex.EncodeToString(digest[:])
		previous, err := self.GetAgentDocumentByExternal(sourceId, externalId)
		if err != nil {
			return false, err
		}
		if previous != nil && previous.Hash != digestText {
			if err := self.reconcileRecordingDocument(agentId, previous.ID); err != nil {
				return false, err
			}
		}
		metadata := map[string]any{"recordingId": recordingSource.ID, "recordingMediaId": correction.RecordingMediaID, "recordingTranscriptRunId": originalAttempt.RecordingTranscriptRunID, "recordingSpeakerCorrectionId": correction.ID, "passageStartMS": correction.PassageStartMS, "passageEndMS": correction.PassageEndMS, "sourceGeneration": recordingSource.SourceGeneration, "isUntrustedRecording": true, "isSpeakerAlignmentUncertain": true, "speakerAttribution": confirmedNames[correction.SpeakerIdentityID] + " (user confirmed original passage)"}
		if previous != nil && previous.Hash == digestText {
			for metadataKey, metadataEntry := range previous.Metadata {
				metadata[metadataKey] = metadataEntry
			}
			metadata["sourceGeneration"] = recordingSource.SourceGeneration
			metadata["recordingSpeakerCorrectionId"] = correction.ID
		}
		happenedAt := recordingSource.RecordedAt
		if happenedAt != nil {
			passageTime := happenedAt.Add(time.Duration(correction.PassageStartMS) * time.Millisecond)
			happenedAt = &passageTime
		}
		document, err := self.PutAgentDocument(&models.AgentDocument{AgentID: agentId, SourceID: sourceId, ExternalID: externalId, Kind: models.DocumentJournal, Title: title, URL: fmt.Sprintf("/recordings/%s?run=%s&startMS=%d&endMS=%d", recordingSource.ID, originalAttempt.RecordingTranscriptRunID, correction.PassageStartMS, correction.PassageEndMS), Hash: digestText, Bytes: int64(len(transcriptText)), Private: true, HappenedAt: happenedAt, Metadata: metadata})
		if err != nil {
			return false, err
		}
		if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&recordingKnowledgeDocumentModel{RecordingID: recordingSource.ID, AgentID: agentId, AgentDocumentID: document.ID}).Error; err != nil {
			return false, err
		}
		if previous == nil || previous.Hash != digestText {
			hasChangedEvidence = true
			if err := self.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: transcriptText}}); err != nil {
				return false, err
			}
		}
	}
	return hasChangedEvidence, nil
}
