package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordingWorkerClaim keeps expired workers from publishing a transcript.
type RecordingWorkerClaim struct {
	JobID   string
	ClaimID string
}

// RecordingTranscriptOperation versions generated evidence and tracks derived blobs.
type RecordingTranscriptOperation interface {
	GetLatestRecordingChunkAttempt(userId, chunkId string) (*models.RecordingTranscriptionAttempt, error)
	GetRecordingTranscriptionAttemptEvidence(userId, attemptId string) (*models.RecordingTranscriptionAttempt, error)
	CancelRecordingTranscriptRun(userId, runId string) (*models.RecordingTranscriptRun, error)
	ReadRecordingTranscriptPage(userId, runId string, segmentOffset, pageSize int) ([]*models.RecordingTranscriptSegment, int64, error)
	ListRecordingMediaAwaitingTranscription(afterMediaId string, limit int) ([]*RecordingMediaCandidate, error)
	CreateRecordingTranscriptRun(userId, mediaId, agentId, clientRunId, providerModelName string, isReprocessingRequested bool) (*models.RecordingTranscriptRun, error)
	GetRecordingTranscriptRun(userId, runId string) (*models.RecordingTranscriptRun, error)
	ListRecordingTranscriptRuns(userId, recordingId string) ([]*models.RecordingTranscriptRun, error)
	UpdateRecordingTranscriptProgress(userId, runId string, claim RecordingWorkerClaim, processingState models.RecordingProcessingState, errorMessage string, derivedDurationMS int64) error
	CreateRecordingAudioChunk(userId, runId string, claim RecordingWorkerClaim, chunk *models.RecordingAudioChunk, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingAudioChunk, error)
	ListRecordingAudioChunks(userId, runId string) ([]*models.RecordingAudioChunk, error)
	GetRecordingBlob(userId, blobId string) (*models.RecordingBlob, error)
	VerifyRecordingBlob(userId, blobId, verifiedSha256 string) error
	ListRecordingBlobsForCleanup(recordingId string, limit int) ([]*models.RecordingBlob, error)
	DeleteRecordingBlobReference(recordingId, blobId string) error
	CreateRecordingTranscriptionAttempt(userId, runId, chunkId string, claim RecordingWorkerClaim, currencyCode string, referenceBlobIds []string) (*models.RecordingTranscriptionAttempt, error)
	ListRecordingTranscriptionAttempts(userId, runId string) ([]*models.RecordingTranscriptionAttempt, error)
	SubmitRecordingTranscriptionAttempt(userId, attemptId string, claim RecordingWorkerClaim) error
	FinishRecordingTranscriptionAttempt(userId, attemptId string, claim RecordingWorkerClaim, finished *models.RecordingTranscriptionAttempt, segments []*models.RecordingTranscriptSegment) (bool, error)
	ListRecordingTranscriptSegments(userId, runId string) ([]*models.RecordingTranscriptSegment, error)
}

// GetLatestRecordingChunkAttempt avoids loading current references for already submitted evidence.
func (self *transaction) GetLatestRecordingChunkAttempt(userId, chunkId string) (*models.RecordingTranscriptionAttempt, error) {
	var chunk recordingChunkModel
	if err := self.tx.Where("id = ?", chunkId).Take(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := self.GetRecordingTranscriptRun(userId, chunk.RecordingTranscriptRunID); err != nil {
		return nil, err
	}
	var attempt recordingAttemptModel
	err := self.tx.Omit("provider_usage_json", "provider_response_json").Where("recording_audio_chunk_id = ?", chunkId).Order("attempt_index DESC").Take(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return recordingAttemptFromModel(&attempt), nil
}

type recordingRunModel struct {
	models.RecordingTranscriptRun
	AgentID *string
}

func (recordingRunModel) TableName() string { return "recording_transcript_run" }

type recordingBlobModel models.RecordingBlob

func (recordingBlobModel) TableName() string { return "recording_blob" }

type recordingChunkModel models.RecordingAudioChunk

func (recordingChunkModel) TableName() string { return "recording_audio_chunk" }

type recordingAttemptModel struct {
	ID                       string                       `json:"id"`
	RecordingTranscriptRunID string                       `json:"recordingTranscriptRunId"`
	RecordingAudioChunkID    string                       `json:"recordingAudioChunkId"`
	AttemptIndex             int                          `json:"attemptIndex"`
	CreatedAt                time.Time                    `json:"createdAt"`
	SubmittedAt              *time.Time                   `json:"submittedAt"`
	CompletedAt              *time.Time                   `json:"completedAt"`
	RecordingAttemptState    models.RecordingAttemptState `json:"recordingAttemptState"`
	ProviderModelName        string                       `json:"providerModelName"`
	AttemptErrorMessage      string                       `json:"attemptErrorMessage"`
	InputAudioTokens         int64                        `json:"inputAudioTokens"`
	InputTextTokens          int64                        `json:"inputTextTokens"`
	PromptTokens             int64                        `json:"promptTokens"`
	CompletionTokens         int64                        `json:"completionTokens"`
	ProviderAudioDurationMS  int64                        `json:"providerAudioDurationMS"`
	HasProviderUsage         bool                         `json:"hasProviderUsage"`
	EstimatedCostAmount      *float64                     `json:"estimatedCostAmount"`
	ActualCostAmount         *float64                     `json:"actualCostAmount"`
	CurrencyCode             string                       `json:"currencyCode"`
	ProviderUsageJSON        json.RawMessage              `gorm:"serializer:json" json:"-"`
	ProviderResponseJSON     json.RawMessage              `gorm:"serializer:json" json:"-"`
	ReferenceBlobIDs         []string                     `gorm:"serializer:json" json:"referenceBlobIds"`
}

func (recordingAttemptModel) TableName() string { return "recording_transcription_attempt" }

type recordingSegmentModel models.RecordingTranscriptSegment

func (recordingSegmentModel) TableName() string { return "recording_transcript_segment" }

func recordingRunFromModel(row *recordingRunModel) *models.RecordingTranscriptRun {
	transcriptRun := row.RecordingTranscriptRun
	transcriptRun.AgentID = optionalString(row.AgentID)
	transcriptRun.CreatedAt = transcriptRun.CreatedAt.UTC()
	transcriptRun.UpdatedAt = transcriptRun.UpdatedAt.UTC()
	if transcriptRun.CompletedAt != nil {
		completedAt := transcriptRun.CompletedAt.UTC()
		transcriptRun.CompletedAt = &completedAt
	}
	return &transcriptRun
}
func recordingAttemptFromModel(row *recordingAttemptModel) *models.RecordingTranscriptionAttempt {
	attempt := models.RecordingTranscriptionAttempt(*row)
	attempt.CreatedAt = attempt.CreatedAt.UTC()
	if attempt.SubmittedAt != nil {
		submittedAt := attempt.SubmittedAt.UTC()
		attempt.SubmittedAt = &submittedAt
	}
	if attempt.CompletedAt != nil {
		completedAt := attempt.CompletedAt.UTC()
		attempt.CompletedAt = &completedAt
	}
	attempt.ProviderUsageJSON = row.ProviderUsageJSON
	attempt.ProviderResponseJSON = row.ProviderResponseJSON
	attempt.ReferenceBlobIDs = row.ReferenceBlobIDs
	return &attempt
}

func (self *transaction) CreateRecordingTranscriptRun(userId, mediaId, agentId, clientRunId, providerModelName string, isReprocessingRequested bool) (*models.RecordingTranscriptRun, error) {
	if clientRunId == "" || len(clientRunId) > 128 || strings.TrimSpace(providerModelName) == "" || len(providerModelName) > 255 {
		return nil, ErrInvalidArguments
	}
	media, err := self.GetRecordingMedia(userId, mediaId)
	if err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, media.RecordingID); err != nil {
		return nil, err
	}
	media, err = self.GetRecordingMedia(userId, mediaId)
	if err != nil {
		return nil, err
	}
	if media.RecordingUploadState != models.RecordingUploadComplete {
		return nil, ErrInvalidArguments
	}
	agent, err := self.GetAgent(agentId)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.UserID != userId {
		return nil, ErrNotFound
	}
	var previous recordingRunModel
	err = self.tx.Where("recording_media_id = ? AND client_run_id = ?", mediaId, clientRunId).Take(&previous).Error
	if err == nil {
		if previous.ProviderModelName != providerModelName || previous.IsReprocessingRequested != isReprocessingRequested {
			return nil, ErrAlreadyExists
		}
		return recordingRunFromModel(&previous), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var versionNumber int
	if err := self.tx.Model(&recordingRunModel{}).Where("recording_media_id = ?", mediaId).Select("COALESCE(MAX(version_number),0)").Scan(&versionNumber).Error; err != nil {
		return nil, err
	}
	if (!isReprocessingRequested && versionNumber > 0) || versionNumber >= 100 {
		return nil, ErrAlreadyExists
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	transcriptRun := models.RecordingTranscriptRun{ID: newID(), RecordingID: media.RecordingID, RecordingMediaID: mediaId, AgentID: agentId, ClientRunID: clientRunId, CreatedAt: now, UpdatedAt: now, VersionNumber: versionNumber + 1, RecordingProcessingState: models.RecordingProcessingQueued, ProviderModelName: providerModelName, OriginalSHA256: media.OriginalSHA256, IsReprocessingRequested: isReprocessingRequested}
	row := recordingRunModel{RecordingTranscriptRun: transcriptRun, AgentID: new(agentId)}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &transcriptRun, nil
}

func (self *transaction) GetRecordingTranscriptRun(userId, runId string) (*models.RecordingTranscriptRun, error) {
	var row recordingRunModel
	if err := self.tx.Where("id = ?", runId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := self.GetRecording(userId, row.RecordingID); err != nil {
		return nil, err
	}
	return recordingRunFromModel(&row), nil
}
func (self *transaction) ListRecordingTranscriptRuns(userId, recordingId string) ([]*models.RecordingTranscriptRun, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []recordingRunModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	transcriptRuns := make([]*models.RecordingTranscriptRun, 0, len(rows))
	for index := range rows {
		transcriptRuns = append(transcriptRuns, recordingRunFromModel(&rows[index]))
	}
	return transcriptRuns, nil
}

// A running job holds no database lock during a provider request. Each publication
// must prove its claim still belongs to it under the source lock.
func (self *transaction) lockRecordingWorker(userId, runId string, claim RecordingWorkerClaim) (*models.RecordingTranscriptRun, error) {
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, transcriptRun.RecordingID); err != nil {
		return nil, err
	}
	transcriptRun, err = self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	if claim.JobID == "" || claim.ClaimID == "" {
		return nil, ErrInvalidArguments
	}
	var job agentJobModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND claim_id = ? AND status = ? AND subject_id = ? AND agent_id = ? AND kind = ?", claim.JobID, claim.ClaimID, models.AgentJobRunning, runId, transcriptRun.AgentID, models.AgentJobRecording).Take(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return transcriptRun, nil
}

func (self *transaction) UpdateRecordingTranscriptProgress(userId, runId string, claim RecordingWorkerClaim, processingState models.RecordingProcessingState, errorMessage string, derivedDurationMS int64) error {
	transcriptRun, err := self.lockRecordingWorker(userId, runId, claim)
	if err != nil {
		return err
	}
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingComplete || transcriptRun.RecordingProcessingState == models.RecordingProcessingFailed || transcriptRun.RecordingProcessingState == models.RecordingProcessingCanceled {
		return ErrAlreadyExists
	}
	if derivedDurationMS < transcriptRun.DerivedDurationMS || len(errorMessage) > 4096 {
		return ErrInvalidArguments
	}
	switch processingState {
	case models.RecordingProcessingPreparing, models.RecordingProcessingTranscribing, models.RecordingProcessingComplete, models.RecordingProcessingFailed, models.RecordingProcessingUncertain, models.RecordingProcessingCanceled:
	default:
		return ErrInvalidArguments
	}
	if processingState == models.RecordingProcessingComplete {
		var chunkCount, incompleteCount int64
		if err := self.tx.Model(&recordingChunkModel{}).Where("recording_transcript_run_id = ?", runId).Count(&chunkCount).Error; err != nil {
			return err
		}
		if chunkCount == 0 || derivedDurationMS <= 0 {
			return ErrInvalidArguments
		}
		if err := self.tx.Model(&recordingChunkModel{}).Where("recording_transcript_run_id = ? AND NOT EXISTS (SELECT 1 FROM recording_transcription_attempt WHERE recording_transcription_attempt.recording_audio_chunk_id = recording_audio_chunk.id AND recording_attempt_state = ?)", runId, models.RecordingAttemptComplete).Count(&incompleteCount).Error; err != nil {
			return err
		}
		if incompleteCount != 0 {
			return ErrInvalidArguments
		}
	}
	updates := map[string]any{"recording_processing_state": processingState, "processing_error_message": errorMessage, "derived_duration_ms": derivedDurationMS, "updated_at": time.Now().UTC().Truncate(time.Microsecond)}
	if processingState == models.RecordingProcessingComplete {
		updates["completed_at"] = time.Now().UTC().Truncate(time.Microsecond)
	}
	return self.tx.Model(&recordingRunModel{}).Where("id = ?", runId).Updates(updates).Error
}

func (self *transaction) GetRecordingBlob(userId, blobId string) (*models.RecordingBlob, error) {
	var row recordingBlobModel
	if err := self.tx.Where("id = ?", blobId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := self.GetRecording(userId, row.RecordingID); err != nil {
		return nil, err
	}
	blob := models.RecordingBlob(row)
	blob.CreatedAt = blob.CreatedAt.UTC()
	return &blob, nil
}

func (self *transaction) CreateRecordingAudioChunk(userId, runId string, claim RecordingWorkerClaim, chunk *models.RecordingAudioChunk, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingAudioChunk, error) {
	if chunk == nil || blob == nil || chunk.ChunkIndex < 0 || chunk.TimelineStartMS < 0 || chunk.AudioDurationMS <= 0 || chunk.TimelineStartMS > math.MaxInt64-chunk.AudioDurationMS || blob.ID == "" || len(blob.ID) > 128 || blob.BlobSizeBytes <= 0 || !validRecordingSHA256(blob.BlobSHA256) || maximumAccountBytes <= 0 {
		return nil, ErrInvalidArguments
	}
	// Match initialization's owner-before-recording lock order for quota reservations.
	var owner userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).Take(&owner).Error; err != nil {
		return nil, err
	}
	transcriptRun, err := self.lockRecordingWorker(userId, runId, claim)
	if err != nil {
		return nil, err
	}
	if blob.ID != RecordingAudioFileID(runId, chunk.ChunkIndex) {
		return nil, ErrInvalidArguments
	}
	if transcriptRun.RecordingProcessingState != models.RecordingProcessingPreparing {
		return nil, ErrAlreadyExists
	}
	var previous recordingChunkModel
	err = self.tx.Where("recording_transcript_run_id = ? AND chunk_index = ?", runId, chunk.ChunkIndex).Take(&previous).Error
	if err == nil {
		previousBlob, err := self.GetRecordingBlob(userId, previous.RecordingBlobID)
		if err != nil {
			return nil, err
		}
		if previousBlob.ID != blob.ID || previousBlob.BlobSHA256 != blob.BlobSHA256 || previousBlob.BlobSizeBytes != blob.BlobSizeBytes || previous.TimelineStartMS != chunk.TimelineStartMS || previous.AudioDurationMS != chunk.AudioDurationMS {
			return nil, ErrAlreadyExists
		}
		kept := models.RecordingAudioChunk(previous)
		return &kept, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	chunks, err := self.ListRecordingAudioChunks(userId, runId)
	if err != nil {
		return nil, err
	}
	if chunk.ChunkIndex != len(chunks) || len(chunks) >= 1000 {
		return nil, ErrInvalidArguments
	}
	media, err := self.GetRecordingMedia(userId, transcriptRun.RecordingMediaID)
	if err != nil {
		return nil, err
	}
	expectedStartMS := media.TimelineStartMS
	if len(chunks) > 0 {
		previousChunk := chunks[len(chunks)-1]
		expectedStartMS = previousChunk.TimelineStartMS + previousChunk.AudioDurationMS
	}
	if chunk.TimelineStartMS != expectedStartMS {
		return nil, ErrInvalidArguments
	}
	var originalBytes, derivedBytes int64
	if err := self.tx.Table("recording_media").Select("COALESCE(SUM(original_size_bytes),0)").Joins("JOIN recording ON recording.id = recording_media.recording_id").Where("recording.user_id = ?", userId).Scan(&originalBytes).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Table("recording_blob").Select("COALESCE(SUM(blob_size_bytes),0)").Joins("JOIN recording ON recording.id = recording_blob.recording_id").Where("recording.user_id = ?", userId).Scan(&derivedBytes).Error; err != nil {
		return nil, err
	}
	if originalBytes > maximumAccountBytes-blob.BlobSizeBytes || derivedBytes > maximumAccountBytes-blob.BlobSizeBytes-originalBytes {
		return nil, ErrTooMuchAsked
	}
	createdBlob := *blob
	createdBlob.RecordingID = transcriptRun.RecordingID
	createdBlob.RecordingMediaID = transcriptRun.RecordingMediaID
	createdBlob.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	createdBlob.RecordingBlobPurpose = models.RecordingBlobAudio
	createdBlob.IsStoredVerified = false
	blobRow := recordingBlobModel(createdBlob)
	if err := self.tx.Create(&blobRow).Error; err != nil {
		return nil, err
	}
	createdChunk := *chunk
	createdChunk.ID = newID()
	createdChunk.RecordingTranscriptRunID = runId
	createdChunk.RecordingBlobID = createdBlob.ID
	row := recordingChunkModel(createdChunk)
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &createdChunk, nil
}

func (self *transaction) ListRecordingAudioChunks(userId, runId string) ([]*models.RecordingAudioChunk, error) {
	if _, err := self.GetRecordingTranscriptRun(userId, runId); err != nil {
		return nil, err
	}
	var rows []recordingChunkModel
	if err := self.tx.Where("recording_transcript_run_id = ?", runId).Order("chunk_index").Find(&rows).Error; err != nil {
		return nil, err
	}
	chunks := make([]*models.RecordingAudioChunk, 0, len(rows))
	for _, row := range rows {
		chunk := models.RecordingAudioChunk(row)
		chunks = append(chunks, &chunk)
	}
	return chunks, nil
}

func (self *transaction) VerifyRecordingBlob(userId, blobId, verifiedSha256 string) error {
	blob, err := self.GetRecordingBlob(userId, blobId)
	if err != nil {
		return err
	}
	if _, err := self.LockRecording(userId, blob.RecordingID); err != nil {
		return err
	}
	if blob.BlobSHA256 != verifiedSha256 {
		return ErrInvalidArguments
	}
	return self.tx.Model(&recordingBlobModel{}).Where("id = ?", blobId).Update("is_stored_verified", true).Error
}
func (self *transaction) ListRecordingBlobsForCleanup(recordingId string, limit int) ([]*models.RecordingBlob, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	var rows []recordingBlobModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	blobs := make([]*models.RecordingBlob, 0, len(rows))
	for _, row := range rows {
		blob := models.RecordingBlob(row)
		blobs = append(blobs, &blob)
	}
	return blobs, nil
}
func (self *transaction) DeleteRecordingBlobReference(recordingId, blobId string) error {
	var source recordingModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND (deleted_at IS NOT NULL OR user_id IS NULL)", recordingId).Take(&source).Error; err != nil {
		return err
	}
	return self.tx.Where("id = ? AND recording_id = ?", blobId, recordingId).Delete(&recordingBlobModel{}).Error
}

// CreateRecordingTranscriptionAttempt reuses an unsent or completed attempt.
// A submitted call from a dead worker is uncertain, never automatically resubmitted.
func (self *transaction) CreateRecordingTranscriptionAttempt(userId, runId, chunkId string, claim RecordingWorkerClaim, currencyCode string, referenceBlobIds []string) (*models.RecordingTranscriptionAttempt, error) {
	transcriptRun, err := self.lockRecordingWorker(userId, runId, claim)
	if err != nil {
		return nil, err
	}
	if transcriptRun.RecordingProcessingState != models.RecordingProcessingTranscribing || len(currencyCode) != 3 || len(referenceBlobIds) > 4 {
		return nil, ErrInvalidArguments
	}
	var chunk recordingChunkModel
	if err := self.tx.Where("id = ? AND recording_transcript_run_id = ?", chunkId, runId).Take(&chunk).Error; err != nil {
		return nil, err
	}
	blob, err := self.GetRecordingBlob(userId, chunk.RecordingBlobID)
	if err != nil {
		return nil, err
	}
	if !blob.IsStoredVerified {
		return nil, ErrInvalidArguments
	}
	for _, referenceId := range referenceBlobIds {
		reference, err := self.GetRecordingBlob(userId, referenceId)
		if err != nil {
			return nil, err
		}
		if !reference.IsStoredVerified || reference.RecordingBlobPurpose != models.RecordingBlobSpeakerReference {
			return nil, ErrInvalidArguments
		}
	}
	if referenceBlobIds == nil {
		referenceBlobIds = []string{}
	}
	var previous recordingAttemptModel
	err = self.tx.Where("recording_audio_chunk_id = ?", chunkId).Order("attempt_index DESC").Take(&previous).Error
	attemptIndex := 1
	if err == nil {
		if previous.RecordingAttemptState != models.RecordingAttemptFailed {
			if previous.RecordingAttemptState == models.RecordingAttemptPrepared {
				// No request has been submitted; current confirmations may replace stale references.
				previous.ReferenceBlobIDs = referenceBlobIds
				if err := self.tx.Model(&previous).Select("reference_blob_ids").Updates(&previous).Error; err != nil {
					return nil, err
				}
			}
			return recordingAttemptFromModel(&previous), nil
		}
		attemptIndex = previous.AttemptIndex + 1
		if attemptIndex > 6 {
			return nil, ErrTooMuchAsked
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	attempt := models.RecordingTranscriptionAttempt{ID: newID(), RecordingTranscriptRunID: runId, RecordingAudioChunkID: chunkId, AttemptIndex: attemptIndex, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), RecordingAttemptState: models.RecordingAttemptPrepared, ProviderModelName: transcriptRun.ProviderModelName, CurrencyCode: currencyCode, ReferenceBlobIDs: referenceBlobIds}
	row := recordingAttemptModel(attempt)
	row.ProviderUsageJSON = json.RawMessage("{}")
	row.ProviderResponseJSON = json.RawMessage("{}")
	row.ReferenceBlobIDs = referenceBlobIds
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}
func (self *transaction) ListRecordingTranscriptionAttempts(userId, runId string) ([]*models.RecordingTranscriptionAttempt, error) {
	if _, err := self.GetRecordingTranscriptRun(userId, runId); err != nil {
		return nil, err
	}
	var rows []recordingAttemptModel
	if err := self.tx.Omit("provider_usage_json", "provider_response_json").Where("recording_transcript_run_id = ?", runId).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	attempts := make([]*models.RecordingTranscriptionAttempt, 0, len(rows))
	for index := range rows {
		attempts = append(attempts, recordingAttemptFromModel(&rows[index]))
	}
	return attempts, nil
}
func (self *transaction) lockRecordingAttempt(userId, attemptId string, claim RecordingWorkerClaim) (*models.RecordingTranscriptionAttempt, *models.RecordingTranscriptRun, error) {
	var row recordingAttemptModel
	if err := self.tx.Where("id = ?", attemptId).Take(&row).Error; err != nil {
		return nil, nil, err
	}
	transcriptRun, err := self.lockRecordingWorker(userId, row.RecordingTranscriptRunID, claim)
	if err != nil {
		return nil, nil, err
	}
	if err := self.tx.Where("id = ?", attemptId).Take(&row).Error; err != nil {
		return nil, nil, err
	}
	return recordingAttemptFromModel(&row), transcriptRun, nil
}
func (self *transaction) SubmitRecordingTranscriptionAttempt(userId, attemptId string, claim RecordingWorkerClaim) error {
	attempt, transcriptRun, err := self.lockRecordingAttempt(userId, attemptId, claim)
	if err != nil {
		return err
	}
	if attempt.RecordingAttemptState != models.RecordingAttemptPrepared || transcriptRun.RecordingProcessingState != models.RecordingProcessingTranscribing {
		return ErrAlreadyExists
	}
	return self.tx.Model(&recordingAttemptModel{}).Where("id = ?", attemptId).Updates(map[string]any{"recording_attempt_state": models.RecordingAttemptSubmitted, "submitted_at": time.Now().UTC().Truncate(time.Microsecond)}).Error
}
func (self *transaction) FinishRecordingTranscriptionAttempt(userId, attemptId string, claim RecordingWorkerClaim, finished *models.RecordingTranscriptionAttempt, segments []*models.RecordingTranscriptSegment) (bool, error) {
	if finished == nil {
		return false, ErrInvalidArguments
	}
	attempt, transcriptRun, err := self.lockRecordingAttempt(userId, attemptId, claim)
	if err != nil {
		return false, err
	}
	if attempt.RecordingAttemptState == models.RecordingAttemptComplete {
		return false, nil
	}
	if attempt.RecordingAttemptState != models.RecordingAttemptSubmitted {
		return false, ErrAlreadyExists
	}
	switch finished.RecordingAttemptState {
	case models.RecordingAttemptComplete, models.RecordingAttemptFailed, models.RecordingAttemptUncertain:
	default:
		return false, ErrInvalidArguments
	}
	if len(finished.AttemptErrorMessage) > 4096 || finished.InputAudioTokens < 0 || finished.InputTextTokens < 0 || finished.PromptTokens < 0 || finished.CompletionTokens < 0 || finished.ProviderAudioDurationMS < 0 || finished.ActualCostAmount != nil {
		return false, ErrInvalidArguments
	}
	if finished.EstimatedCostAmount != nil && (math.IsNaN(*finished.EstimatedCostAmount) || math.IsInf(*finished.EstimatedCostAmount, 0) || *finished.EstimatedCostAmount < 0) {
		return false, ErrInvalidArguments
	}
	if finished.RecordingAttemptState != models.RecordingAttemptComplete && len(segments) > 0 {
		return false, ErrInvalidArguments
	}
	if len(segments) > 20000 {
		return false, ErrTooMuchAsked
	}
	var chunk recordingChunkModel
	if err := self.tx.Where("id = ?", attempt.RecordingAudioChunkID).Take(&chunk).Error; err != nil {
		return false, err
	}
	// Cancellation keeps billing evidence but publishes no additional transcript.
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingCanceled {
		segments = nil
	}
	transcriptBytes := 0
	for _, segment := range segments {
		if segment == nil || segment.SegmentStartMS < chunk.TimelineStartMS || segment.SegmentEndMS < segment.SegmentStartMS || segment.SegmentEndMS > chunk.TimelineStartMS+chunk.AudioDurationMS || segment.LocalSpeakerID == "" || len(segment.LocalSpeakerID) > 255 || len(segment.TranscriptText) > 65536 {
			return false, ErrInvalidArguments
		}
		transcriptBytes += len(segment.TranscriptText)
		if transcriptBytes > 2<<20 {
			return false, ErrTooMuchAsked
		}
		createdSegment := *segment
		createdSegment.ID = newID()
		createdSegment.RecordingTranscriptRunID = transcriptRun.ID
		createdSegment.RecordingTranscriptionAttemptID = attemptId
		row := recordingSegmentModel(createdSegment)
		if err := self.tx.Create(&row).Error; err != nil {
			return false, err
		}
	}
	usageJSON, responseJSON := finished.ProviderUsageJSON, finished.ProviderResponseJSON
	if len(usageJSON) == 0 {
		usageJSON = json.RawMessage("{}")
	}
	if len(responseJSON) == 0 {
		responseJSON = json.RawMessage("{}")
	}
	if !json.Valid(usageJSON) || !json.Valid(responseJSON) || len(usageJSON) > 1<<20 || len(responseJSON) > 32<<20 {
		return false, ErrInvalidArguments
	}
	if err := self.tx.Model(&recordingAttemptModel{}).Where("id = ?", attemptId).Updates(map[string]any{
		"recording_attempt_state": finished.RecordingAttemptState, "completed_at": time.Now().UTC().Truncate(time.Microsecond), "attempt_error_message": finished.AttemptErrorMessage,
		"input_audio_tokens": finished.InputAudioTokens, "input_text_tokens": finished.InputTextTokens, "prompt_tokens": finished.PromptTokens, "completion_tokens": finished.CompletionTokens,
		"provider_audio_duration_ms": finished.ProviderAudioDurationMS, "has_provider_usage": finished.HasProviderUsage, "estimated_cost_amount": finished.EstimatedCostAmount,
		"provider_usage_json": string(usageJSON), "provider_response_json": string(responseJSON),
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}
func (self *transaction) ListRecordingTranscriptSegments(userId, runId string) ([]*models.RecordingTranscriptSegment, error) {
	if _, err := self.GetRecordingTranscriptRun(userId, runId); err != nil {
		return nil, err
	}
	var rows []recordingSegmentModel
	if err := self.tx.Where("recording_transcript_run_id = ?", runId).Order("segment_start_ms, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	segments := make([]*models.RecordingTranscriptSegment, 0, len(rows))
	for _, row := range rows {
		segment := models.RecordingTranscriptSegment(row)
		segments = append(segments, &segment)
	}
	return segments, nil
}

// RecordingAudioFileID keeps pending derived writes reachable through their ledger.
func RecordingAudioFileID(runId string, chunkIndex int) string {
	return fmt.Sprintf("recording-audio-%d-%s", chunkIndex, runId)
}

// RecordingMediaCandidate is an owned completed original with no initial version.
type RecordingMediaCandidate struct {
	UserID           string
	RecordingMediaID string
}

func (self *transaction) ListRecordingMediaAwaitingTranscription(afterMediaId string, limit int) ([]*RecordingMediaCandidate, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	var candidates []*RecordingMediaCandidate
	query := self.tx.Table("recording_media AS media").Select("recording.user_id, media.id AS recording_media_id").Joins("JOIN recording ON recording.id = media.recording_id").Joins("JOIN agent ON agent.user_id = recording.user_id").Where("recording.deleted_at IS NULL AND recording.user_id IS NOT NULL AND media.recording_upload_state = ? AND agent.enabled AND agent.operator_disabled_at IS NULL", models.RecordingUploadComplete).Where("NOT EXISTS (SELECT 1 FROM recording_transcript_run WHERE recording_transcript_run.recording_media_id = media.id)").Order("media.id").Limit(limit)
	if afterMediaId != "" {
		query = query.Where("media.id > ?", afterMediaId)
	}
	err := query.Scan(&candidates).Error
	return candidates, err
}

func (self *transaction) CancelRecordingTranscriptRun(userId, runId string) (*models.RecordingTranscriptRun, error) {
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, transcriptRun.RecordingID); err != nil {
		return nil, err
	}
	transcriptRun, err = self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingCanceled {
		return transcriptRun, nil
	}
	if transcriptRun.RecordingProcessingState == models.RecordingProcessingComplete {
		return nil, ErrAlreadyExists
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := self.tx.Model(&recordingRunModel{}).Where("id = ?", runId).Updates(map[string]any{"recording_processing_state": models.RecordingProcessingCanceled, "processing_error_message": "", "updated_at": now, "completed_at": now}).Error; err != nil {
		return nil, err
	}
	return self.GetRecordingTranscriptRun(userId, runId)
}

func (self *transaction) ReadRecordingTranscriptPage(userId, runId string, segmentOffset, pageSize int) ([]*models.RecordingTranscriptSegment, int64, error) {
	if segmentOffset < 0 || pageSize < 1 || pageSize > 250 {
		return nil, 0, ErrInvalidArguments
	}
	if _, err := self.GetRecordingTranscriptRun(userId, runId); err != nil {
		return nil, 0, err
	}
	var segmentCount int64
	if err := self.tx.Model(&recordingSegmentModel{}).Where("recording_transcript_run_id = ?", runId).Count(&segmentCount).Error; err != nil {
		return nil, 0, err
	}
	var rows []recordingSegmentModel
	if err := self.tx.Where("recording_transcript_run_id = ?", runId).Order("segment_start_ms, id").Offset(segmentOffset).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	segments := make([]*models.RecordingTranscriptSegment, 0, len(rows))
	for _, row := range rows {
		segment := models.RecordingTranscriptSegment(row)
		segments = append(segments, &segment)
	}
	return segments, segmentCount, nil
}

// GetRecordingTranscriptionAttemptEvidence reads one owned receipt; bulk views omit raw provider payloads.
func (self *transaction) GetRecordingTranscriptionAttemptEvidence(userId, attemptId string) (*models.RecordingTranscriptionAttempt, error) {
	var row recordingAttemptModel
	if err := self.tx.Where("id = ?", attemptId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := self.GetRecordingTranscriptRun(userId, row.RecordingTranscriptRunID); err != nil {
		return nil, err
	}
	return recordingAttemptFromModel(&row), nil
}
