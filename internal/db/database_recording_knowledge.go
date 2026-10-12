package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordingKnowledgeOperation uses the existing source, document, evidence and revision stores.
type RecordingKnowledgeOperation interface {
	ListRecordingsAwaitingKnowledgeInitialization(afterRecordingId string, pageSize int) ([]*models.Recording, error)
	InitializeRecordingKnowledge(userId, recordingId string) (bool, error)
	ReadRecordingLearning(userId, recordingId string) (*models.RecordingLearningView, error)
	SyncRecordingKnowledge(userId, recordingId string) error
	ResumeRecordingLearning(userId, recordingId string, expectedSourceGeneration int64) (*models.RecordingLearningView, error)
	CheckRecordingKnowledgeEvidence(userId string, documents []*models.AgentDocument) error
	ReconcileDeletedRecordingKnowledge(recordingId string) error
}

type recordingKnowledgeSettings struct {
	RecordingID    string `json:"recordingId"`
	RecordingTitle string `json:"recordingTitle"`
}

func recordingKnowledgeName(recordingId string) string { return "recording:" + recordingId }

// recordingKnowledgeInitializationCandidates excludes retained mappings from Stop reading.
// This is SQL-only recovery; enabled model configuration is checked by the existing workers.
func (self *transaction) recordingKnowledgeInitializationCandidates() *gorm.DB {
	return self.tx.Model(&recordingModel{}).
		Joins(`JOIN "user" AS owner ON owner.id = recording.user_id`).
		Joins(`JOIN agent ON agent.user_id = recording.user_id`).
		Where("recording.deleted_at IS NULL AND owner.disabled_at IS NULL").
		Where(`EXISTS (SELECT 1 FROM recording_transcript_run AS transcript_run
			JOIN recording_transcript_segment AS segment ON segment.recording_transcript_run_id = transcript_run.id
			WHERE transcript_run.recording_id = recording.id AND transcript_run.recording_processing_state = ?)`, models.RecordingProcessingComplete).
		Where(`EXISTS (SELECT 1 FROM user_group JOIN group_role ON group_role.group_id = user_group.group_id
			JOIN role_permission ON role_permission.role_id = group_role.role_id
			WHERE user_group.user_id = recording.user_id AND role_permission.permission_key = ?)`, models.PermissionAgentUse).
		Where(`NOT EXISTS (SELECT 1 FROM agent_source WHERE agent_source.agent_id = agent.id
			AND (agent_source.name = 'recording:' || recording.id
			OR (agent_source.kind = ? AND agent_source.specification->'settings'->>'recordingId' = recording.id)))`, models.SourceRecording).
		Where("NOT EXISTS (SELECT 1 FROM recording_knowledge_document WHERE recording_knowledge_document.recording_id = recording.id)")
}

// ListRecordingsAwaitingKnowledgeInitialization discovers completed versions from before source support.
func (self *transaction) ListRecordingsAwaitingKnowledgeInitialization(afterRecordingId string, pageSize int) ([]*models.Recording, error) {
	if pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidArguments
	}
	var sourceRows []*recordingModel
	if err := self.recordingKnowledgeInitializationCandidates().Select("recording.*").Where("recording.id > ?", afterRecordingId).Order("recording.id").Limit(pageSize).Find(&sourceRows).Error; err != nil {
		return nil, err
	}
	sources := make([]*models.Recording, 0, len(sourceRows))
	for _, sourceRow := range sourceRows {
		sources = append(sources, recordingFromModel(sourceRow))
	}
	return sources, nil
}

// InitializeRecordingKnowledge does not undo an explicit pause or Stop reading.
func (self *transaction) InitializeRecordingKnowledge(userId, recordingId string) (bool, error) {
	if _, err := self.LockRecording(userId, recordingId); err != nil {
		return false, err
	}
	var candidateCount int64
	if err := self.recordingKnowledgeInitializationCandidates().Where("recording.id = ? AND recording.user_id = ?", recordingId, userId).Count(&candidateCount).Error; err != nil || candidateCount == 0 {
		return false, err
	}
	if err := self.SyncRecordingKnowledge(userId, recordingId); err != nil {
		return false, err
	}
	return true, nil
}

func (self *transaction) SyncRecordingKnowledge(userId, recordingId string) error {
	return self.syncRecordingKnowledge(userId, recordingId, false)
}

func (self *transaction) ResumeRecordingLearning(userId, recordingId string, expectedSourceGeneration int64) (*models.RecordingLearningView, error) {
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return nil, err
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return nil, ErrAlreadyExists
	}
	if err := self.syncRecordingKnowledge(userId, recordingId, true); err != nil {
		return nil, err
	}
	return self.ReadRecordingLearning(userId, recordingId)
}

func (self *transaction) syncRecordingKnowledge(userId, recordingId string, shouldResumeReading bool) error {
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
	sourceSettings, err := json.Marshal(recordingKnowledgeSettings{RecordingID: recordingId, RecordingTitle: recordingSource.RecordingTitle})
	if err != nil {
		return err
	}
	hasChangedEvidence := source == nil
	if source == nil {
		var retainedDocumentCount int64
		if err := self.tx.Model(&recordingKnowledgeDocumentModel{}).Where("recording_id = ?", recordingId).Count(&retainedDocumentCount).Error; err != nil {
			return err
		}
		if retainedDocumentCount > 0 {
			if err := self.reconcileRetainedRecordingKnowledge(recordingId); err != nil {
				return err
			}
			if !shouldResumeReading {
				return nil
			}
		}
		var hasLearnablePassage bool
		if err := self.tx.Raw(`SELECT EXISTS (SELECT 1 FROM recording_transcript_run transcript_run
			JOIN recording_transcript_segment segment ON segment.recording_transcript_run_id = transcript_run.id
			WHERE transcript_run.recording_id = ? AND transcript_run.recording_processing_state = ?)`, recordingId, models.RecordingProcessingComplete).Scan(&hasLearnablePassage).Error; err != nil {
			return err
		}
		if !hasLearnablePassage {
			return nil
		}

		source, err = self.PutAgentSource(&models.AgentKnowledgeSource{AgentID: ownerAgent.ID, Kind: models.SourceRecording, Name: recordingKnowledgeName(recordingId), Enabled: true, Specification: models.AgentKnowledgeSpecification{Settings: json.RawMessage(sourceSettings)}})
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
		var previousSettings recordingKnowledgeSettings
		if err := json.Unmarshal(source.Specification.Settings, &previousSettings); err != nil || previousSettings.RecordingID != recordingId || previousSettings.RecordingTitle != recordingSource.RecordingTitle {
			source.Specification.Settings = sourceSettings
			source, err = self.PutAgentSource(source)
			if err != nil {
				return err
			}
			hasChangedEvidence = true
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
			if err := self.ProjectRecordingSpeakerCorrections(userId, segments); err != nil {
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
				if segment.HasUserConfirmedSpeaker {
					confirmedName = segment.EffectiveSpeakerName + " (user confirmed)"
				} else if segment.HasUnalignedSpeakerCorrection {
					confirmedName = "speaker correction is partial or cannot be unambiguously aligned; attribution uncertain"
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
					isMatched, _, err := self.recordingSpeakerCorrectionMatchesSegment(correction, transcriptRun, segment)
					if err != nil {
						return err
					}
					if !isMatched || (segment.RecordingTranscriptionAttemptID != correction.RecordingTranscriptionAttemptID && hasCompetingRecordingSpeakerCorrections(correction, corrections)) {
						continue
					}
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
				metadata["effectiveSpeakerIdentityId"] = segment.EffectiveSpeakerIdentityID
				metadata["hasUserConfirmedSpeaker"] = segment.HasUserConfirmedSpeaker
				metadata["hasUnalignedSpeakerCorrection"] = segment.HasUnalignedSpeakerCorrection
				if previous != nil && previous.Hash == hex.EncodeToString(digest[:]) {
					for metadataKey, metadataEntry := range previous.Metadata {
						metadata[metadataKey] = metadataEntry
					}
					metadata["sourceGeneration"] = recordingSource.SourceGeneration
					metadata["appliedTextCorrectionId"] = segment.AppliedTextCorrectionID
					metadata["effectiveSpeakerIdentityId"] = segment.EffectiveSpeakerIdentityID
					metadata["hasUserConfirmedSpeaker"] = segment.HasUserConfirmedSpeaker
					metadata["hasUnalignedSpeakerCorrection"] = segment.HasUnalignedSpeakerCorrection
				}
				passageHappenedAt := recordingSource.RecordedAt
				if passageHappenedAt != nil {
					passageTime := passageHappenedAt.Add(time.Duration(segment.SegmentStartMS) * time.Millisecond)
					passageHappenedAt = &passageTime
				}
				document, err := self.PutAgentDocument(&models.AgentDocument{AgentID: ownerAgent.ID, SourceID: source.ID, ExternalID: externalId, Kind: models.DocumentJournal, Title: title, URL: fmt.Sprintf("/recordings/%s?version=%s&startMS=%d&endMS=%d", recordingId, transcriptRun.ID, segment.SegmentStartMS, segment.SegmentEndMS), Hash: hex.EncodeToString(digest[:]), Bytes: int64(len(transcriptText)), Private: true, HappenedAt: passageHappenedAt, Metadata: metadata})
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
	unalignedSpeakersChanged, err := self.syncRecordingUnalignedSpeakerCorrections(userId, recordingSource, ownerAgent.ID, source.ID, latestRuns, corrections, names)
	if err != nil {
		return err
	}
	hasChangedEvidence = hasChangedEvidence || unalignedSpeakersChanged
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
	if err := self.tx.Where("kind = ? AND (name = ? OR specification->'settings'->>'recordingId' = ?)", models.SourceRecording, recordingKnowledgeName(recordingId), recordingId).Find(&sources).Error; err != nil {
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
		recordingId, _ := document.Metadata["recordingId"].(string)
		if source == nil || !source.Enabled || source.Kind != models.SourceRecording || source.Name != recordingKnowledgeName(recordingId) {
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
	if err != nil {
		return nil, err
	}
	if learning.KnowledgeSource == nil {
		if err := self.tx.Raw("SELECT EXISTS (SELECT 1 FROM recording_knowledge_document WHERE recording_id = ? AND agent_id = ?)", recordingId, ownerAgent.ID).Scan(&learning.HasStoppedReading).Error; err != nil {
			return nil, err
		}
		return learning, nil
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
