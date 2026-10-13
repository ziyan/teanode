package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/lib/pq"
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
	SyncRecordingKnowledgeStep(userId, recordingId string, progress *RecordingKnowledgeSyncProgress) (bool, error)
	QueueRecordingKnowledgeSync(userId, recordingId string) error
	ListRecordingsAwaitingKnowledgeSync(pageSize int) ([]*models.Recording, error)
	ResumeRecordingLearning(userId, recordingId string, expectedSourceGeneration int64) (*models.RecordingLearningView, error)
	CheckRecordingKnowledgeEvidence(userId string, documents []*models.AgentDocument) error
	ReconcileDeletedRecordingKnowledge(recordingId string) error
}

// RecordingKnowledgeSyncProgress carries one pass over a recording's passages
// across the transactions that make it, so that a long recording is read in
// slices and the recording's row lock is never held for the whole pass.
// A zero value starts a pass.
type RecordingKnowledgeSyncProgress struct {
	// PassStartedAt is when the pass began: a document the pass did not write
	// since then is no longer in the transcript.
	PassStartedAt time.Time
	// RunID is the transcript run being read, and SegmentCursorStartMS and
	// SegmentCursorID the last passage of it that was.
	RunID                string
	IsRunFinished        bool
	SegmentCursorStartMS int64
	SegmentCursorID      string
	// HasStarted is whether the first slice has run, and SourceGeneration the
	// recording's generation then: the one the finished pass is recorded as
	// having synced, so a change made during the pass leaves it behind.
	HasStarted       bool
	SourceGeneration int64
	// HasChangedEvidence is whether any slice of the pass wrote a document
	// that differs from the one before it.
	HasChangedEvidence bool
}

// recordingKnowledgeSliceSegmentCount is how many passages one slice of a pass
// reads, and so how long a slice holds the recording's lock.
const recordingKnowledgeSliceSegmentCount = 250

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

// SyncRecordingKnowledgeStep reads one slice of a sync pass and says whether the
// pass is finished. A caller that cannot hold the recording's lock for a whole
// pass (the background job) calls it in a transaction per slice.
func (self *transaction) SyncRecordingKnowledgeStep(userId, recordingId string, progress *RecordingKnowledgeSyncProgress) (bool, error) {
	return self.syncRecordingKnowledgeStep(userId, recordingId, false, progress, 1)
}

// syncRecordingKnowledgeStep reads slices of a pass and, when the pass is
// finished, records the generation it started at on the recording.
func (self *transaction) syncRecordingKnowledgeStep(userId, recordingId string, shouldResumeReading bool, progress *RecordingKnowledgeSyncProgress, maximumPageCount int) (bool, error) {
	isFinished, err := self.syncRecordingKnowledgeSlices(userId, recordingId, shouldResumeReading, progress, maximumPageCount)
	if err != nil || !isFinished || !progress.HasStarted {
		return isFinished, err
	}
	// Never moves the marker back: a pass that started earlier than another finished.
	return true, self.tx.Model(&recordingModel{}).Where("id = ? AND (knowledge_synced_generation IS NULL OR knowledge_synced_generation < ?)", recordingId, progress.SourceGeneration).Update("knowledge_synced_generation", progress.SourceGeneration).Error
}

// QueueRecordingKnowledgeSync asks for the recording's knowledge to be synced
// in the background, after the request's transaction commits. One job is open
// per recording, so several requests in a row queue one sync, and the sync
// reads what is stored when it runs, so repeating it is harmless.
func (self *transaction) QueueRecordingKnowledgeSync(userId, recordingId string) error {
	recordingSource, err := self.GetRecording(userId, recordingId)
	if err != nil {
		return err
	}
	// The sync leaves a capture still being recorded alone, and stopping it syncs once.
	if recordingSource.RecordingOrigin == models.RecordingOriginCapture && recordingSource.RecordingCaptureState != models.RecordingCaptureStopped {
		return nil
	}
	ownerAgent, err := self.GetAgentByUser(userId)
	if err != nil || ownerAgent == nil {
		return err
	}
	// From here the sweep watches the recording: a change that lands while the
	// job runs leaves the generation ahead of the marker and is queued again.
	if err := self.tx.Model(&recordingModel{}).Where("id = ? AND knowledge_synced_generation IS NULL", recordingId).Update("knowledge_synced_generation", 0).Error; err != nil {
		return err
	}
	_, err = self.EnqueueAgentJob(&models.AgentJob{AgentID: ownerAgent.ID, Kind: models.AgentJobRecordingKnowledge, SubjectID: recordingId})
	return err
}

// ListRecordingsAwaitingKnowledgeSync lists recordings whose generation is ahead
// of the one their knowledge was synced at and that have no sync queued or
// running, so a sync that missed a change, or a queue that was lost, is asked
// for again. A recording nothing has asked to sync has no marker and is left alone.
func (self *transaction) ListRecordingsAwaitingKnowledgeSync(pageSize int) ([]*models.Recording, error) {
	if pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidArguments
	}
	var rows []*recordingModel
	if err := self.tx.Where(`user_id IS NOT NULL AND deleted_at IS NULL AND knowledge_synced_generation IS NOT NULL AND knowledge_synced_generation < source_generation
		AND (recording_origin <> ? OR recording_capture_state = ?)
		AND EXISTS (SELECT 1 FROM agent WHERE agent.user_id = recording.user_id)
		AND NOT EXISTS (SELECT 1 FROM agent_job WHERE agent_job.kind = ? AND agent_job.subject_id = recording.id AND agent_job.status IN ?)`,
		models.RecordingOriginCapture, models.RecordingCaptureStopped, models.AgentJobRecordingKnowledge, []string{string(models.AgentJobQueued), string(models.AgentJobRunning)}).
		Order("updated_at, id").Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	sources := make([]*models.Recording, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, recordingFromModel(row))
	}
	return sources, nil
}

func (self *transaction) syncRecordingKnowledge(userId, recordingId string, shouldResumeReading bool) error {
	progress := &RecordingKnowledgeSyncProgress{}
	for {
		isFinished, err := self.syncRecordingKnowledgeStep(userId, recordingId, shouldResumeReading, progress, 0)
		if err != nil || isFinished {
			return err
		}
	}
}

// syncRecordingKnowledgeSlices reads up to maximumPageCount pages of passages
// (all of them when it is zero) and, once there are none left, retires what the
// pass did not see. It repeats everything before the first page on each call,
// so that a slice sees the corrections and runs as they are now.
func (self *transaction) syncRecordingKnowledgeSlices(userId, recordingId string, shouldResumeReading bool, progress *RecordingKnowledgeSyncProgress, maximumPageCount int) (bool, error) {
	recordingSource, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return false, err
	}
	if !progress.HasStarted {
		progress.HasStarted, progress.SourceGeneration = true, recordingSource.SourceGeneration
	}
	// The transcript of a capture still being recorded is partial and changes
	// with every piece. Stopping the capture syncs it once.
	if recordingSource.RecordingOrigin == models.RecordingOriginCapture && recordingSource.RecordingCaptureState != models.RecordingCaptureStopped {
		return true, nil
	}
	ownerAgent, err := self.GetAgentByUser(userId)
	if err != nil || ownerAgent == nil {
		return false, err
	}
	source, err := self.GetAgentSourceByName(ownerAgent.ID, recordingKnowledgeName(recordingId))
	if err != nil {
		return false, err
	}
	sourceSettings, err := json.Marshal(recordingKnowledgeSettings{RecordingID: recordingId, RecordingTitle: recordingSource.RecordingTitle})
	if err != nil {
		return false, err
	}
	hasChangedEvidence := progress.HasChangedEvidence || source == nil
	if source == nil {
		var retainedDocumentCount int64
		if err := self.tx.Model(&recordingKnowledgeDocumentModel{}).Where("recording_id = ?", recordingId).Count(&retainedDocumentCount).Error; err != nil {
			return false, err
		}
		if retainedDocumentCount > 0 {
			if err := self.reconcileRetainedRecordingKnowledge(recordingId); err != nil {
				return false, err
			}
			if !shouldResumeReading {
				return true, nil
			}
		}
		var hasLearnablePassage bool
		if err := self.tx.Raw(`SELECT EXISTS (SELECT 1 FROM recording_transcript_run transcript_run
			JOIN recording_transcript_segment segment ON segment.recording_transcript_run_id = transcript_run.id
			WHERE transcript_run.recording_id = ? AND transcript_run.recording_processing_state = ?)`, recordingId, models.RecordingProcessingComplete).Scan(&hasLearnablePassage).Error; err != nil {
			return false, err
		}
		if !hasLearnablePassage {
			return true, nil
		}

		source, err = self.PutAgentSource(&models.AgentKnowledgeSource{AgentID: ownerAgent.ID, Kind: models.SourceRecording, Name: recordingKnowledgeName(recordingId), Enabled: true, Specification: models.AgentKnowledgeSpecification{Settings: json.RawMessage(sourceSettings)}})
		if err != nil {
			return false, err
		}
	} else {
		source, err = self.LockAgentSource(ownerAgent.ID, source.ID)
		if err != nil {
			return false, err
		}
		if source.Kind != models.SourceRecording {
			return false, ErrAlreadyExists
		}
		var previousSettings recordingKnowledgeSettings
		if err := json.Unmarshal(source.Specification.Settings, &previousSettings); err != nil || previousSettings.RecordingID != recordingId || previousSettings.RecordingTitle != recordingSource.RecordingTitle {
			source.Specification.Settings = sourceSettings
			source, err = self.PutAgentSource(source)
			if err != nil {
				return false, err
			}
			hasChangedEvidence = true
		}
		if !source.Enabled {
			return true, self.reconcileRetainedRecordingKnowledge(recordingId)
		}
	}
	runs, err := self.ListRecordingTranscriptRuns(userId, recordingId)
	if err != nil {
		return false, err
	}
	corrections, err := self.ListRecordingSpeakerCorrections(userId, recordingId)
	if err != nil {
		return false, err
	}
	invalidations, err := self.ListRecordingReferenceInvalidations(userId, recordingId)
	if err != nil {
		return false, err
	}
	identities, err := self.ListRecordingSpeakerIdentities(userId)
	if err != nil {
		return false, err
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
	if progress.PassStartedAt.IsZero() {
		progress.PassStartedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	passStartedAt := progress.PassStartedAt
	// A fixed order, so that a pass resumed in another transaction continues where the last one stopped.
	sortedRuns := make([]*models.RecordingTranscriptRun, 0, len(latestRuns))
	for _, transcriptRun := range latestRuns {
		sortedRuns = append(sortedRuns, transcriptRun)
	}
	sort.Slice(sortedRuns, func(left, right int) bool { return sortedRuns[left].ID < sortedRuns[right].ID })
	pageCount := 0
	latestRunRows := make([]recordingRunModel, 0, len(latestRuns))
	for _, transcriptRun := range latestRuns {
		latestRunRows = append(latestRunRows, recordingRunModel{RecordingTranscriptRun: *transcriptRun})
	}
	handoffCutoffs, err := self.recordingHandoffCutoffs(recordingId, latestRunRows)
	if err != nil {
		return false, err
	}
	for _, transcriptRun := range sortedRuns {
		if transcriptRun.ID < progress.RunID || (transcriptRun.ID == progress.RunID && progress.IsRunFinished) {
			continue
		}
		// The follower of a piece re-read its last passage, so the piece's own copy is not evidence.
		segmentLimitMS := int64(math.MaxInt64)
		if cutoffMS, isCut := handoffCutoffs[transcriptRun.ID]; isCut {
			segmentLimitMS = cutoffMS
		}
		segmentCursorStartMS := int64(-1)
		segmentCursorId := ""
		if transcriptRun.ID == progress.RunID {
			segmentCursorStartMS, segmentCursorId = progress.SegmentCursorStartMS, progress.SegmentCursorID
		}
		for {
			if maximumPageCount > 0 && pageCount >= maximumPageCount {
				progress.RunID, progress.IsRunFinished = transcriptRun.ID, false
				progress.SegmentCursorStartMS, progress.SegmentCursorID = segmentCursorStartMS, segmentCursorId
				progress.HasChangedEvidence = hasChangedEvidence
				return false, nil
			}
			pageCount++
			changedDocumentIds := []string{}
			var segmentRows []*recordingSegmentModel
			if err := self.tx.Where("recording_transcript_run_id = ? AND segment_start_ms < ? AND (segment_start_ms > ? OR (segment_start_ms = ? AND id > ?))", transcriptRun.ID, segmentLimitMS, segmentCursorStartMS, segmentCursorStartMS, segmentCursorId).Order("segment_start_ms,id").Limit(recordingKnowledgeSliceSegmentCount).Find(&segmentRows).Error; err != nil {
				return false, err
			}
			segments := make([]*models.RecordingTranscriptSegment, 0, len(segmentRows))
			for _, segmentRow := range segmentRows {
				segments = append(segments, (*models.RecordingTranscriptSegment)(segmentRow))
			}
			if err := self.ProjectRecordingTranscriptTextCorrections(userId, segments); err != nil {
				return false, err
			}
			if err := self.ProjectRecordingSpeakerCorrections(userId, segments); err != nil {
				return false, err
			}
			for _, segmentRow := range segmentRows {
				segment := (*models.RecordingTranscriptSegment)(segmentRow)
				confirmedName := "unconfirmed speaker " + segment.LocalSpeakerID + " (request " + segment.RecordingTranscriptionAttemptID + ")"
				if segment.TemporarySpeakerLabel != "" {
					confirmedName = "unconfirmed speaker " + segment.TemporarySpeakerLabel + " (the same voice across this recording, name not confirmed)"
				}
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
				} else if segment.IsRecognizedSpeaker && !hasInvalidatedAttribution {
					confirmedName = segment.EffectiveSpeakerName + " (recognized by voice, not confirmed by the user)"
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
					if segment.AppliedTextCorrectionOrigin == models.TextCorrectionOriginGlossary {
						transcriptText += "\nRecognized text corrected automatically to match a name or term the user keeps; the user has not reviewed it. Original generated text remains in transcript version history."
					} else {
						transcriptText += "\nRecognized text corrected by the user; original generated text remains in transcript version history."
					}
				}
				// A selected interval does not imply word timestamps or ownership of the whole segment.
				for _, correction := range corrections {
					isMatched, _, err := self.recordingSpeakerCorrectionMatchesSegment(correction, transcriptRun, segment)
					if err != nil {
						return false, err
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
				externalId := transcriptRun.ID + ":" + segment.ID
				previous, err := self.GetAgentDocumentByExternal(source.ID, externalId)
				if err != nil {
					return false, err
				}
				chunkText := transcriptText
				digest := sha256.Sum256([]byte(transcriptText))
				if previous != nil && previous.Hash != hex.EncodeToString(digest[:]) {
					// Read before the page's reconcile strikes the facts that cite the old words.
					chunkText, err = self.withRetiredRecordingNotes(ownerAgent.ID, previous, transcriptText)
					if err != nil {
						return false, err
					}
					changedDocumentIds = append(changedDocumentIds, previous.ID)
				}
				metadata := map[string]any{"recordingId": recordingId, "recordingMediaId": transcriptRun.RecordingMediaID, "recordingTranscriptRunId": transcriptRun.ID, "transcriptVersionNumber": transcriptRun.VersionNumber, "passageStartMS": segment.SegmentStartMS, "passageEndMS": segment.SegmentEndMS, "sourceGeneration": recordingSource.SourceGeneration, "isUntrustedRecording": true, "speakerAttribution": confirmedName}
				metadata["appliedTextCorrectionId"] = segment.AppliedTextCorrectionID
				metadata["appliedTextCorrectionOrigin"] = string(segment.AppliedTextCorrectionOrigin)
				metadata["effectiveSpeakerIdentityId"] = segment.EffectiveSpeakerIdentityID
				metadata["hasUserConfirmedSpeaker"] = segment.HasUserConfirmedSpeaker
				metadata["hasUnalignedSpeakerCorrection"] = segment.HasUnalignedSpeakerCorrection
				if previous != nil && previous.Hash == hex.EncodeToString(digest[:]) {
					for metadataKey, metadataEntry := range previous.Metadata {
						metadata[metadataKey] = metadataEntry
					}
					metadata["sourceGeneration"] = recordingSource.SourceGeneration
					metadata["appliedTextCorrectionId"] = segment.AppliedTextCorrectionID
					metadata["appliedTextCorrectionOrigin"] = string(segment.AppliedTextCorrectionOrigin)
					metadata["effectiveSpeakerIdentityId"] = segment.EffectiveSpeakerIdentityID
					metadata["hasUserConfirmedSpeaker"] = segment.HasUserConfirmedSpeaker
					metadata["hasUnalignedSpeakerCorrection"] = segment.HasUnalignedSpeakerCorrection
				}
				passageHappenedAt := recordingSource.RecordedAt
				if passageHappenedAt != nil {
					passageTime := passageHappenedAt.Add(time.Duration(segment.SegmentStartMS) * time.Millisecond)
					passageHappenedAt = &passageTime
				}
				document, err := self.PutAgentDocument(&models.AgentDocument{AgentID: ownerAgent.ID, SourceID: source.ID, ExternalID: externalId, Kind: models.DocumentJournal, Title: title, URL: fmt.Sprintf("/recordings/%s?version=%s&startMS=%d&endMS=%d", recordingId, transcriptRun.ID, segment.SegmentStartMS, segment.SegmentEndMS), Hash: hex.EncodeToString(digest[:]), Bytes: int64(len(chunkText)), Private: true, HappenedAt: passageHappenedAt, Metadata: metadata})
				if err != nil {
					return false, err
				}
				if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&recordingKnowledgeDocumentModel{RecordingID: recordingId, AgentID: ownerAgent.ID, AgentDocumentID: document.ID}).Error; err != nil {
					return false, err
				}
				if previous == nil || previous.Hash != document.Hash {
					hasChangedEvidence = true
					if err := self.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: chunkText}}); err != nil {
						return false, err
					}
				}
			}
			// One query for every document of the page whose words changed, not one each.
			if err := self.reconcileRecordingDocuments(ownerAgent.ID, changedDocumentIds); err != nil {
				return false, err
			}
			if len(segmentRows) < recordingKnowledgeSliceSegmentCount {
				break
			}
			lastSegment := segmentRows[len(segmentRows)-1]
			segmentCursorStartMS = lastSegment.SegmentStartMS
			segmentCursorId = lastSegment.ID
		}
		progress.RunID, progress.IsRunFinished = transcriptRun.ID, true
	}
	unalignedChanged, err := self.syncRecordingUnalignedTextCorrections(userId, recordingSource, ownerAgent.ID, source.ID, latestRuns)
	if err != nil {
		return false, err
	}
	hasChangedEvidence = hasChangedEvidence || unalignedChanged
	unalignedSpeakersChanged, err := self.syncRecordingUnalignedSpeakerCorrections(userId, recordingSource, ownerAgent.ID, source.ID, latestRuns, corrections, names)
	if err != nil {
		return false, err
	}
	hasChangedEvidence = hasChangedEvidence || unalignedSpeakersChanged
	// Replaced versions can change segmentation; retire every old document not seen by this pass.
	var unseen []*agentDocumentModel
	if err := self.tx.Where("source_id = ? AND seen_at < ?", source.ID, passStartedAt).Find(&unseen).Error; err != nil {
		return false, err
	}
	unseenDocumentIds := make([]string, 0, len(unseen))
	for _, document := range unseen {
		hasChangedEvidence = true
		unseenDocumentIds = append(unseenDocumentIds, document.ID)
	}
	if err := self.reconcileRecordingDocuments(ownerAgent.ID, unseenDocumentIds); err != nil {
		return false, err
	}
	for _, documentId := range unseenDocumentIds {
		if err := self.DeleteAgentDocument(ownerAgent.ID, documentId); err != nil {
			return false, err
		}
	}
	progress.HasChangedEvidence = hasChangedEvidence
	if hasChangedEvidence {
		return true, self.RequestAgentSourceRun(source.ID, time.Now())
	}
	return true, nil
}

// withRetiredRecordingNotes gives a corrected passage the notes its correction
// retires, so the next dream can file what still holds in corrected form instead
// of reading a lone sentence that has lost the context the old note carried.
// It only adds notes at the moment the plain passage text differs from the
// stored document, before reconcileRecordingDocument strikes them, and lists
// only live notes. The stored hash is always that of the plain text, so a later
// sync of an unchanged passage compares equal and neither re-reads nor strikes
// the corrected notes filed since.
func (self *transaction) withRetiredRecordingNotes(agentId string, previous *models.AgentDocument, passageText string) (string, error) {
	if previous == nil || previous.Hash == recordingPassageHash(passageText) {
		return passageText, nil
	}
	evidence := fmt.Sprintf("[{\"id\":\"%s\",\"kind\":\"document\"}]", previous.ID)
	var retiringFacts []*agentFactModel
	if err := self.tx.Where("agent_id = ? AND NOT dormant AND superseded_by IS NULL AND evidence @> ?::jsonb", agentId, evidence).Order("id").Limit(10).Find(&retiringFacts).Error; err != nil {
		return "", err
	}
	return appendRetiredRecordingNotes(passageText, retiringFacts), nil
}

func appendRetiredRecordingNotes(passageText string, retiredFacts []*agentFactModel) string {
	if len(retiredFacts) == 0 {
		return passageText
	}
	passageText += "\nBefore the latest correction this passage supported these notes, now retired. File the corrected version of any that still holds with the corrected text or attribution; do not restore what the correction contradicts:"
	for _, retiredFact := range retiredFacts {
		passageText += "\n- " + retiredFact.Text
	}
	return passageText
}

func recordingPassageHash(passageText string) string {
	digest := sha256.Sum256([]byte(passageText))
	return hex.EncodeToString(digest[:])
}

func (self *transaction) reconcileRecordingDocument(agentId, documentId string) error {
	return self.reconcileRecordingDocuments(agentId, []string{documentId})
}

// reconcileRecordingDocuments strikes the facts that cite any of the documents
// and clears the overview of every page that does, in a few statements however
// many documents there are: a long recording changes thousands at once, and a
// query per document over the agent's facts took a minute.
func (self *transaction) reconcileRecordingDocuments(agentId string, documentIds []string) error {
	const batchSize = 500
	for batchStart := 0; batchStart < len(documentIds); batchStart += batchSize {
		batch := documentIds[batchStart:min(batchStart+batchSize, len(documentIds))]
		var facts []*agentFactModel
		if err := self.tx.Where(`agent_id = ? AND NOT dormant AND superseded_by IS NULL AND EXISTS (
			SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(evidence) = 'array' THEN evidence ELSE '[]'::jsonb END) AS cited
			WHERE cited->>'kind' = 'document' AND cited->>'id' = ANY(?))`, agentId, pq.Array(batch)).Find(&facts).Error; err != nil {
			return err
		}
		clearedNodeIds := map[string]bool{}
		for _, row := range facts {
			fact, err := row.toModel()
			if err != nil {
				return err
			}
			if _, err := self.StrikeAgentFact(agentId, fact.ID, "recording evidence was corrected or deleted; awaiting reconciliation"); err != nil {
				return err
			}
			// Existing overview invalidation preserves page prose and its history until the normal rewrite.
			if !clearedNodeIds[fact.NodeID] {
				clearedNodeIds[fact.NodeID] = true
				if err := self.ClearAgentNodeOverviewInputs(agentId, fact.NodeID); err != nil {
					return err
				}
			}
		}
		var overviewPages []*agentNodeModel
		if err := self.tx.Where(`agent_id = ? AND EXISTS (
			SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(overview_evidence) = 'array' THEN overview_evidence ELSE '[]'::jsonb END) AS cited
			WHERE cited->>'kind' = 'document' AND cited->>'id' = ANY(?))`, agentId, pq.Array(batch)).Find(&overviewPages).Error; err != nil {
			return err
		}
		for _, page := range overviewPages {
			if !clearedNodeIds[page.ID] {
				clearedNodeIds[page.ID] = true
				if err := self.ClearAgentNodeOverviewInputs(agentId, page.ID); err != nil {
					return err
				}
			}
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
		documentIds := make([]string, 0, len(documents))
		for _, document := range documents {
			documentIds = append(documentIds, document.ID)
		}
		if err := self.reconcileRecordingDocuments(source.AgentID, documentIds); err != nil {
			return err
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
		documentIdsByAgentID := map[string][]string{}
		for _, reference := range references {
			documentIdsByAgentID[reference.AgentID] = append(documentIdsByAgentID[reference.AgentID], reference.AgentDocumentID)
		}
		for agentId, documentIds := range documentIdsByAgentID {
			if err := self.reconcileRecordingDocuments(agentId, documentIds); err != nil {
				return err
			}
		}
		if len(references) < 250 {
			return nil
		}
		documentCursorId = references[len(references)-1].AgentDocumentID
	}
}
