package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm/clause"
)

// RecordingKnowledgeOperation uses the existing source, document, evidence and revision stores.
type RecordingKnowledgeOperation interface {
	ReadRecordingLearning(userId, recordingId string) (*models.RecordingLearningView, error)
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
		if err := self.reconcileRetainedRecordingKnowledge(recordingId); err != nil {
			return err
		}
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
			return self.reconcileRetainedRecordingKnowledge(recordingId)
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
	invalidations, err := self.ListRecordingReferenceInvalidations(userId, recordingId)
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
		segmentCursorStartMS := int64(-1)
		segmentCursorId := ""
		for {
			var segmentRows []*recordingSegmentModel
			if err := self.tx.Where("recording_transcript_run_id = ? AND (segment_start_ms > ? OR (segment_start_ms = ? AND id > ?))", transcriptRun.ID, segmentCursorStartMS, segmentCursorStartMS, segmentCursorId).Order("segment_start_ms,id").Limit(250).Find(&segmentRows).Error; err != nil {
				return err
			}
			segments := make([]*models.RecordingTranscriptSegment, 0, len(segmentRows))
			for _, segmentRow := range segmentRows {
				segments = append(segments, (*models.RecordingTranscriptSegment)(segmentRow))
			}
			if err := self.ProjectRecordingTranscriptTextCorrections(userId, segments); err != nil {
				return err
			}
			for _, segmentRow := range segmentRows {
				segment := (*models.RecordingTranscriptSegment)(segmentRow)
				confirmedName := "unconfirmed speaker " + segment.LocalSpeakerID + " (request " + segment.RecordingTranscriptionAttemptID + ")"
				hasInvalidatedAttribution := false
				for _, invalidation := range invalidations {
					if invalidation.RecordingTranscriptionAttemptID == segment.RecordingTranscriptionAttemptID {
						hasInvalidatedAttribution = true
						confirmedName = "automatic attribution uncertain because a conditioning voice reference changed"
						break
					}
				}
				latestGeneration := int64(-1)
				for _, correction := range corrections {
					if correction.RecordingMediaID != transcriptRun.RecordingMediaID || correction.PassageStartMS > segment.SegmentStartMS || correction.PassageEndMS < segment.SegmentEndMS || correction.SourceGeneration < latestGeneration {
						continue
					}
					latestGeneration = correction.SourceGeneration
					if correction.IsRemoved {
						confirmedName = "unconfirmed speaker " + segment.LocalSpeakerID + " (request " + segment.RecordingTranscriptionAttemptID + ")"
						if hasInvalidatedAttribution {
							confirmedName = "automatic attribution uncertain because a conditioning voice reference changed"
						}
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
				transcriptText := title + "\n" + meetingTime + "\n" + segment.EffectiveTranscriptText
				if hasInvalidatedAttribution {
					transcriptText += "\nThe generated attribution used a voice reference whose confirmation changed or was revoked. Treat automatic speaker attribution as uncertain. Explicit user passage confirmations remain evidence; no new transcription has been requested."
				}
				if segment.AppliedTextCorrectionID != "" {
					transcriptText += "\nRecognized text corrected by the user; original generated text remains in transcript version history."
				}
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
				metadata["appliedTextCorrectionId"] = segment.AppliedTextCorrectionID
				if previous != nil && previous.Hash == hex.EncodeToString(digest[:]) {
					for metadataKey, metadataEntry := range previous.Metadata {
						metadata[metadataKey] = metadataEntry
					}
					metadata["sourceGeneration"] = recordingSource.SourceGeneration
					metadata["appliedTextCorrectionId"] = segment.AppliedTextCorrectionID
				}
				passageHappenedAt := recordingSource.RecordedAt
				if passageHappenedAt != nil {
					passageTime := passageHappenedAt.Add(time.Duration(segment.SegmentStartMS) * time.Millisecond)
					passageHappenedAt = &passageTime
				}
				document, err := self.PutAgentDocument(&models.AgentDocument{AgentID: ownerAgent.ID, SourceID: source.ID, ExternalID: externalId, Kind: models.DocumentJournal, Title: title, URL: fmt.Sprintf("/recordings/%s?run=%s&startMS=%d&endMS=%d", recordingId, transcriptRun.ID, segment.SegmentStartMS, segment.SegmentEndMS), Hash: hex.EncodeToString(digest[:]), Bytes: int64(len(transcriptText)), Private: true, HappenedAt: passageHappenedAt, Metadata: metadata})
				if err != nil {
					return err
				}
				if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&recordingKnowledgeDocumentModel{RecordingID: recordingId, AgentID: ownerAgent.ID, AgentDocumentID: document.ID}).Error; err != nil {
					return err
				}
				if previous == nil || previous.Hash != document.Hash {
					hasChangedEvidence = true
					if err := self.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: transcriptText}}); err != nil {
						return err
					}
				}
			}
			if len(segmentRows) < 250 {
				break
			}
			lastSegment := segmentRows[len(segmentRows)-1]
			segmentCursorStartMS = lastSegment.SegmentStartMS
			segmentCursorId = lastSegment.ID
		}
	}
	unalignedChanged, err := self.syncRecordingUnalignedTextCorrections(userId, recordingSource, ownerAgent.ID, source.ID, latestRuns)
	if err != nil {
		return err
	}
	hasChangedEvidence = hasChangedEvidence || unalignedChanged
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
	if err := self.tx.Where("agent_id = ? AND NOT dormant AND superseded_by IS NULL AND evidence @> ?::jsonb", agentId, fmt.Sprintf("[{\"id\":\"%s\",\"kind\":\"document\"}]", documentId)).Find(&facts).Error; err != nil {
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
	if err := self.reconcileRetainedRecordingKnowledge(recordingId); err != nil {
		return err
	}
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

func (self *transaction) ReadRecordingLearning(userId, recordingId string) (*models.RecordingLearningView, error) {
	source, err := self.GetRecording(userId, recordingId)
	if err != nil {
		return nil, err
	}
	learning := &models.RecordingLearningView{SourceGeneration: source.SourceGeneration}
	if err := self.tx.Raw(`SELECT speaker_curated_generation < source_generation
  AND EXISTS (SELECT 1 FROM recording_speaker_correction c WHERE c.recording_id=recording.id)
  FROM recording WHERE id = ? AND user_id = ?`, recordingId, userId).Scan(&learning.IsCurationPending).Error; err != nil {
		return nil, err
	}
	ownerAgent, err := self.GetAgentByUser(userId)
	if err != nil || ownerAgent == nil {
		return learning, err
	}
	learning.HasAgent = true
	learning.KnowledgeSource, err = self.GetAgentSourceByName(ownerAgent.ID, recordingKnowledgeName(recordingId))
	if err != nil || learning.KnowledgeSource == nil {
		return learning, err
	}
	if learning.KnowledgeSource.Kind != models.SourceRecording {
		return nil, ErrNotFound
	}
	var counts struct {
		PendingDocumentCount          int64
		DigestedDocumentCount         int64
		LastPublishedSourceGeneration int64
	}
	if err := self.tx.Raw(`SELECT COUNT(*) FILTER (WHERE NOT jsonb_exists(metadata,'digested')) AS pending_document_count,
  COUNT(*) FILTER (WHERE jsonb_exists(metadata,'digested')) AS digested_document_count,
  COALESCE(MIN((metadata->>'sourceGeneration')::bigint),0) AS last_published_source_generation
  FROM agent_document WHERE agent_id = ? AND source_id = ?`, ownerAgent.ID, learning.KnowledgeSource.ID).Scan(&counts).Error; err != nil {
		return nil, err
	}
	learning.PendingDocumentCount = counts.PendingDocumentCount
	learning.DigestedDocumentCount = counts.DigestedDocumentCount
	learning.LastPublishedSourceGeneration = counts.LastPublishedSourceGeneration
	return learning, nil
}

type recordingKnowledgeDocumentModel struct {
	RecordingID     string
	AgentID         string
	AgentDocumentID string
}

func (recordingKnowledgeDocumentModel) TableName() string { return "recording_knowledge_document" }
func (self *transaction) reconcileRetainedRecordingKnowledge(recordingId string) error {
	documentCursorId := ""
	for {
		var references []*recordingKnowledgeDocumentModel
		if err := self.tx.Where("recording_id = ? AND agent_document_id > ?", recordingId, documentCursorId).Order("agent_document_id").Limit(250).Find(&references).Error; err != nil {
			return err
		}
		for _, reference := range references {
			if err := self.reconcileRecordingDocument(reference.AgentID, reference.AgentDocumentID); err != nil {
				return err
			}
		}
		if len(references) < 250 {
			return nil
		}
		documentCursorId = references[len(references)-1].AgentDocumentID
	}
}
