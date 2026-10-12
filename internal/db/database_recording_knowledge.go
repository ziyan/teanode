package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// RecordingKnowledgeOperation uses the existing source, document, evidence and revision stores.
type RecordingKnowledgeOperation interface {
	SyncRecordingKnowledge(userId, recordingId string) error
	CheckRecordingKnowledgeEvidence(userId string, documents []*models.AgentDocument) error
	ReconcileDeletedRecordingKnowledge(recordingId string) error
}

func recordingKnowledgeName(recordingId string) string { return "recording:" + recordingId }

func (self *transaction) SyncRecordingKnowledge(userId, recordingId string) error {
	recordingSource, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	ownerAgent, err := self.GetAgentByUser(userId)
	if err != nil || ownerAgent == nil {
		return err
	}
	source, err := self.GetAgentSourceByName(ownerAgent.ID, recordingKnowledgeName(recordingId))
	if err != nil {
		return err
	}
	hasChangedEvidence := source == nil
	if source == nil {
		source, err = self.PutAgentSource(&models.AgentKnowledgeSource{AgentID: ownerAgent.ID, Kind: models.SourceRecording, Name: recordingKnowledgeName(recordingId), Enabled: true, Specification: models.AgentKnowledgeSpecification{Settings: json.RawMessage(fmt.Sprintf("{\"recordingId\":\"%s\"}", recordingId))}})
		if err != nil {
			return err
		}
	} else {
		source, err = self.LockAgentSource(ownerAgent.ID, source.ID)
		if err != nil {
			return err
		}
		if source.Kind != models.SourceRecording {
			return ErrAlreadyExists
		}
		if !source.Enabled {
			return nil
		}
	}
	runs, err := self.ListRecordingTranscriptRuns(userId, recordingId)
	if err != nil {
		return err
	}
	corrections, err := self.ListRecordingSpeakerCorrections(userId, recordingId)
	if err != nil {
		return err
	}
	identities, err := self.ListRecordingSpeakerIdentities(userId)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, identity := range identities {
		names[identity.ID] = identity.ConfirmedName
	}
	latestRuns := map[string]*models.RecordingTranscriptRun{}
	for _, transcriptRun := range runs {
		if transcriptRun.RecordingProcessingState != models.RecordingProcessingComplete {
			continue
		}
		previous := latestRuns[transcriptRun.RecordingMediaID]
		if previous == nil || previous.VersionNumber < transcriptRun.VersionNumber {
			latestRuns[transcriptRun.RecordingMediaID] = transcriptRun
		}
	}
	passStartedAt := time.Now().UTC().Truncate(time.Microsecond)
	for _, transcriptRun := range latestRuns {
		segments, err := self.ListRecordingTranscriptSegments(userId, transcriptRun.ID)
		if err != nil {
			return err
		}
		for _, segment := range segments {
			confirmedName := "unconfirmed speaker " + segment.LocalSpeakerID + " (request " + segment.RecordingTranscriptionAttemptID + ")"
			latestGeneration := int64(-1)
			for _, correction := range corrections {
				if correction.RecordingMediaID != transcriptRun.RecordingMediaID || correction.PassageStartMS > segment.SegmentStartMS || correction.PassageEndMS < segment.SegmentEndMS || correction.SourceGeneration < latestGeneration {
					continue
				}
				latestGeneration = correction.SourceGeneration
				if correction.IsRemoved {
					confirmedName = "unconfirmed speaker " + segment.LocalSpeakerID + " (request " + segment.RecordingTranscriptionAttemptID + ")"
				} else {
					confirmedName = names[correction.SpeakerIdentityID] + " (user confirmed)"
				}
			}
			for _, correction := range corrections {
				if correction.RecordingMediaID == transcriptRun.RecordingMediaID && correction.PassageStartMS < segment.SegmentEndMS && correction.PassageEndMS > segment.SegmentStartMS && (correction.PassageStartMS > segment.SegmentStartMS || correction.PassageEndMS < segment.SegmentEndMS) {
					confirmedName = "partially corrected attribution; selected ranges below record user confirmations"
					break
				}
			}
			title := fmt.Sprintf("%s, transcript version %d, %d-%d MS, %s", recordingSource.RecordingTitle, transcriptRun.VersionNumber, segment.SegmentStartMS, segment.SegmentEndMS, confirmedName)
			meetingTime := "Meeting occurrence time is unknown."
			if recordingSource.RecordedAt != nil {
				meetingTime = "Meeting recorded at " + recordingSource.RecordedAt.UTC().Format(time.RFC3339)
			}
			transcriptText := title + "\n" + meetingTime + "\n" + segment.TranscriptText
			// A selected interval does not imply word timestamps or ownership of the whole segment.
			for _, correction := range corrections {
				if correction.RecordingMediaID != transcriptRun.RecordingMediaID || correction.PassageStartMS >= segment.SegmentEndMS || correction.PassageEndMS <= segment.SegmentStartMS {
					continue
				}
				if correction.PassageStartMS <= segment.SegmentStartMS && correction.PassageEndMS >= segment.SegmentEndMS {
					continue
				}
				attribution := names[correction.SpeakerIdentityID] + " (user confirmed)"
				if correction.IsRemoved {
					attribution = "confirmation removed; attribution uncertain"
				}
				transcriptText += fmt.Sprintf("\nSelected passage correction generation %d, %d-%d MS: %s. Text has segment-level timing only; do not assign the whole statement to this passage.", correction.SourceGeneration, correction.PassageStartMS, correction.PassageEndMS, attribution)
			}
			digest := sha256.Sum256([]byte(transcriptText))
			externalId := transcriptRun.ID + ":" + segment.ID
			previous, err := self.GetAgentDocumentByExternal(source.ID, externalId)
			if err != nil {
				return err
			}
			if previous != nil && previous.Hash != hex.EncodeToString(digest[:]) {
				if err := self.reconcileRecordingDocument(ownerAgent.ID, previous.ID); err != nil {
					return err
				}
			}
			metadata := map[string]any{"recordingId": recordingId, "recordingMediaId": transcriptRun.RecordingMediaID, "recordingTranscriptRunId": transcriptRun.ID, "transcriptVersionNumber": transcriptRun.VersionNumber, "passageStartMS": segment.SegmentStartMS, "passageEndMS": segment.SegmentEndMS, "sourceGeneration": recordingSource.SourceGeneration, "isUntrustedRecording": true, "speakerAttribution": confirmedName}
			if previous != nil && previous.Hash == hex.EncodeToString(digest[:]) {
				for metadataKey, metadataEntry := range previous.Metadata {
					metadata[metadataKey] = metadataEntry
				}
				metadata["sourceGeneration"] = recordingSource.SourceGeneration
			}
			document, err := self.PutAgentDocument(&models.AgentDocument{AgentID: ownerAgent.ID, SourceID: source.ID, ExternalID: externalId, Kind: models.DocumentJournal, Title: title, URL: fmt.Sprintf("/recordings/%s?run=%s&startMS=%d&endMS=%d", recordingId, transcriptRun.ID, segment.SegmentStartMS, segment.SegmentEndMS), Hash: hex.EncodeToString(digest[:]), Bytes: int64(len(transcriptText)), Private: true, HappenedAt: recordingSource.RecordedAt, Metadata: metadata})
			if err != nil {
				return err
			}
			if previous == nil || previous.Hash != document.Hash {
				hasChangedEvidence = true
				if err := self.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: transcriptText}}); err != nil {
					return err
				}
			}
		}
	}
	// Replaced versions can change segmentation; retire every old document not seen by this pass.
	var unseen []*agentDocumentModel
	if err := self.tx.Where("source_id = ? AND seen_at < ?", source.ID, passStartedAt).Find(&unseen).Error; err != nil {
		return err
	}
	for _, document := range unseen {
		hasChangedEvidence = true
		if err := self.reconcileRecordingDocument(ownerAgent.ID, document.ID); err != nil {
			return err
		}
		if err := self.DeleteAgentDocument(ownerAgent.ID, document.ID); err != nil {
			return err
		}
	}
	if hasChangedEvidence {
		return self.RequestAgentSourceRun(source.ID, time.Now())
	}
	return nil
}

func (self *transaction) reconcileRecordingDocument(agentId, documentId string) error {
	var facts []*agentFactModel
	if err := self.tx.Where("agent_id = ? AND evidence @> ?::jsonb", agentId, fmt.Sprintf("[{\"id\":\"%s\",\"kind\":\"document\"}]", documentId)).Find(&facts).Error; err != nil {
		return err
	}
	for _, row := range facts {
		fact, err := row.toModel()
		if err != nil {
			return err
		}
		if _, err := self.StrikeAgentFact(agentId, fact.ID, "recording evidence was corrected or deleted; awaiting reconciliation"); err != nil {
			return err
		}
		// Existing overview invalidation preserves page prose and its history until the normal rewrite.
		if err := self.ClearAgentNodeOverviewInputs(agentId, fact.NodeID); err != nil {
			return err
		}
	}
	var overviewPages []*agentNodeModel
	if err := self.tx.Where("agent_id = ? AND overview_evidence @> ?::jsonb", agentId, fmt.Sprintf("[{\"id\":\"%s\",\"kind\":\"document\"}]", documentId)).Find(&overviewPages).Error; err != nil {
		return err
	}
	for _, page := range overviewPages {
		if err := self.ClearAgentNodeOverviewInputs(agentId, page.ID); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) ReconcileDeletedRecordingKnowledge(recordingId string) error {
	var sources []*agentSourceModel
	if err := self.tx.Where("kind = ? AND name = ?", models.SourceRecording, recordingKnowledgeName(recordingId)).Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		if _, err := self.LockAgentSource(source.AgentID, source.ID); err != nil {
			return err
		}
		var documents []*agentDocumentModel
		if err := self.tx.Where("source_id = ?", source.ID).Find(&documents).Error; err != nil {
			return err
		}
		for _, document := range documents {
			if err := self.reconcileRecordingDocument(source.AgentID, document.ID); err != nil {
				return err
			}
		}
		if err := self.DeleteAgentSource(source.AgentID, source.ID); err != nil {
			return err
		}
	}
	return nil
}

// CheckRecordingKnowledgeEvidence locks sources before graph pages and rejects stale model output.
func (self *transaction) CheckRecordingKnowledgeEvidence(userId string, documents []*models.AgentDocument) error {
	recordingIds := map[string]bool{}
	for _, document := range documents {
		if isRecording, _ := document.Metadata["isUntrustedRecording"].(bool); isRecording {
			recordingId, _ := document.Metadata["recordingId"].(string)
			if recordingId == "" {
				return ErrInvalidArguments
			}
			recordingIds[recordingId] = true
		}
	}
	sortedRecordingIds := make([]string, 0, len(recordingIds))
	for recordingId := range recordingIds {
		sortedRecordingIds = append(sortedRecordingIds, recordingId)
	}
	sort.Strings(sortedRecordingIds)
	for _, recordingId := range sortedRecordingIds {
		if _, err := self.LockRecording(userId, recordingId); err != nil {
			return err
		}
	}
	for _, document := range documents {
		if isRecording, _ := document.Metadata["isUntrustedRecording"].(bool); !isRecording {
			continue
		}
		source, err := self.LockAgentSource(document.AgentID, document.SourceID)
		if err != nil {
			return err
		}
		if source == nil || !source.Enabled || source.Kind != models.SourceRecording {
			return ErrAlreadyExists
		}
		current, err := self.GetAgentDocument(document.AgentID, document.ID)
		if err != nil {
			return err
		}
		if current == nil || current.SourceID != document.SourceID || current.Hash != document.Hash {
			return ErrAlreadyExists
		}
	}
	return nil
}
