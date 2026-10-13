package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

const recordingLongest = 40 * time.Minute
const recordingMaximumDurationMS int64 = 7 * 24 * 60 * 60 * 1000

// RecordingTranscriptionAvailability checks current admission without creating work or a model call.
func (self *Agent) RecordingTranscriptionAvailability(transaction db.Transaction, userId string) (bool, string, error) {
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled || configuration.Agent.Models.Transcription == "" || self.settings.Registry == nil {
		return false, "Recording transcription is off. An operator can choose a model with speaker attribution.", nil
	}
	assistant, err := transaction.GetAgentByUser(userId)
	if err != nil {
		return false, "", err
	}
	if assistant == nil || !assistant.Active() {
		return false, "Turn on your personal assistant to process recordings.", nil
	}
	if err := self.recordingAuthority(transaction, &Run{Agent: assistant}, configuration.Agent.Models.Transcription, false); err != nil {
		var deferral *Deferral
		if errors.As(err, &deferral) {
			return false, deferral.Reason, nil
		}
		return false, "", err
	}
	if _, err := self.settings.Registry.TranscribingFor(configuration.Agent.Models.Transcription, true, false); err != nil {
		return false, "The configured recording model is unavailable or lacks speaker attribution and timestamps.", nil
	}
	return true, "", nil
}

// QueueRecordingTranscription retains the requested version and job atomically.
// Initial uploads have one stable version; explicit reprocessing names a new key.
func (self *Agent) QueueRecordingTranscription(transaction db.Transaction, userId, mediaId, clientRunId string, isReprocessingRequested bool) (*models.RecordingTranscriptRun, error) {
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled || self.settings.Registry == nil || configuration.Agent.Models.Transcription == "" {
		return nil, ErrUnavailable
	}
	permissions, err := transaction.EffectivePermissions(userId)
	if err != nil {
		return nil, err
	}
	if !permissions.Has(models.PermissionAgentUse) {
		return nil, ErrUnavailable
	}
	agent, err := transaction.GetAgentByUser(userId)
	if err != nil {
		return nil, err
	}
	if agent == nil || !agent.Active() {
		return nil, ErrUnavailable
	}
	if _, err := self.settings.Registry.TranscribingFor(configuration.Agent.Models.Transcription, true, false); err != nil {
		return nil, err
	}
	transcriptRun, err := transaction.CreateRecordingTranscriptRun(userId, mediaId, agent.ID, clientRunId, configuration.Agent.Models.Transcription, isReprocessingRequested)
	if err != nil {
		return nil, err
	}
	if _, err := self.Enqueue(transaction, models.AgentJobRecording, agent.ID, "", transcriptRun.ID); err != nil {
		return nil, err
	}
	return transcriptRun, nil
}

func (self *Agent) queueRecordings(ctx context.Context) {
	if self.settings.Registry == nil || self.settings.Configuration().Agent.Models.Transcription == "" {
		return
	}
	var candidates []*db.RecordingMediaCandidate
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		candidates, err = transaction.ListRecordingMediaAwaitingTranscription(self.lastRecordingMediaId, 25)
		return err
	}); err != nil {
		log.Warningf("cannot find recording media awaiting processing: %s", err)
		return
	}
	if len(candidates) < 25 {
		self.lastRecordingMediaId = ""
	} else {
		self.lastRecordingMediaId = candidates[len(candidates)-1].RecordingMediaID
	}
	for _, candidate := range candidates {
		err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			_, err := self.QueueRecordingTranscription(transaction, candidate.UserID, candidate.RecordingMediaID, "initial", false)
			return err
		})
		if err != nil && !errors.Is(err, ErrUnavailable) && !errors.Is(err, db.ErrNotFound) && !errors.Is(err, db.ErrAlreadyExists) && !errors.Is(err, db.ErrRecordingDeleted) {
			log.Warningf("cannot queue recording transcription: %s", err)
		}
	}
}

func (self *Agent) recordingAuthority(transaction db.Transaction, run *Run, providerModelName string, shouldCheckBudget bool) error {
	configuration := self.settings.Configuration()
	currentAgent, err := transaction.GetAgent(run.Agent.ID)
	if err != nil {
		return err
	}
	owner, err := transaction.GetUser(run.Agent.UserID)
	if err != nil {
		return err
	}
	permissions, err := transaction.EffectivePermissions(run.Agent.UserID)
	if err != nil {
		return err
	}
	if currentAgent == nil || !currentAgent.Active() || owner == nil || owner.Disabled() || !permissions.Has(models.PermissionAgentUse) || !configuration.Agent.Enabled || configuration.Agent.Models.Transcription == "" {
		return &Deferral{Until: time.Now().Add(time.Minute), Reason: "recording transcription is paused until its account, agent and provider are available"}
	}
	providerName, modelName, hasProvider := strings.Cut(providerModelName, ":")
	provider := configuration.Agent.Provider(providerName)
	if !hasProvider || provider == nil || !provider.IsEnabled() || !provider.Models.Admits(modelName) {
		return &Deferral{Until: time.Now().Add(time.Minute), Reason: "recording transcription is paused until its account, agent and provider are available"}
	}
	if shouldCheckBudget {
		return RequireBudget(transaction, configuration, currentAgent, owner, time.Now())
	}
	return nil
}

func (self *Agent) recordingProgress(ctx context.Context, run *Run, transcriptRun *models.RecordingTranscriptRun, processingState models.RecordingProcessingState, errorMessage string, durationMS int64) error {
	return self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return transaction.UpdateRecordingTranscriptProgress(run.Agent.UserID, transcriptRun.ID, db.RecordingWorkerClaim{JobID: run.Job.ID, ClaimID: run.Job.ClaimID}, processingState, errorMessage, durationMS)
	})
}

func (self *Agent) prepareRecordingAudio(ctx context.Context, run *Run, transcriptRun *models.RecordingTranscriptRun, selection *llm.TranscriptionSelection, files storage.StreamFiles) error {
	if selection.Capabilities.MaximumAudioBytes <= 44 {
		return fmt.Errorf("recording: provider audio size bound is unavailable")
	}
	chunkDurationMS := min(int64(10*60*1000), (min(selection.Capabilities.MaximumAudioBytes, 600*recording.AudioSampleRateHz*2+44)-44)*1000/(recording.AudioSampleRateHz*2))
	if chunkDurationMS < 1000 {
		return fmt.Errorf("recording: provider audio size bound is too small")
	}
	decoder, err := recording.NewAudioDecoder(ctx)
	if err != nil {
		return self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingFailed, err.Error(), transcriptRun.DerivedDurationMS)
	}
	if err := self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingPreparing, "", transcriptRun.DerivedDurationMS); err != nil {
		return err
	}
	var media *models.RecordingMedia
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		media, err = transaction.GetRecordingMedia(run.Agent.UserID, transcriptRun.RecordingMediaID)
		return err
	}); err != nil {
		return err
	}
	original, err := recording.PrivateMediaFile()
	if err != nil {
		return err
	}
	defer func() { _ = original.Close() }()
	digest := sha256.New()
	if err := recording.New(self.settings.Database, files).CopyRange(ctx, run.Agent.UserID, media.ID, io.MultiWriter(original, digest), 0, media.OriginalSizeBytes); err != nil {
		return err
	}
	if hex.EncodeToString(digest.Sum(nil)) != transcriptRun.OriginalSHA256 {
		return self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingFailed, "original media failed its integrity check", transcriptRun.DerivedDurationMS)
	}
	maximumAccountBytes := self.settings.Configuration().Agent.Limits.MaxRecordingAccountBytes.Bytes()
	if maximumAccountBytes == 0 {
		maximumAccountBytes = 64 << 30
	}
	if maximumAccountBytes > math.MaxInt64 {
		return fmt.Errorf("recording: account limit exceeds supported byte sizes")
	}
	claim := db.RecordingWorkerClaim{JobID: run.Job.ID, ClaimID: run.Job.ClaimID}
	var publicationError error
	durationMS, err := decoder.Decode(ctx, original, media.TimelineStartMS, recordingMaximumDurationMS, chunkDurationMS, func(decoded *recording.DecodedAudioChunk) error {
		chunkDigest := sha256.Sum256(decoded.WaveBytes)
		blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(transcriptRun.ID, decoded.ChunkIndex), BlobSizeBytes: int64(len(decoded.WaveBytes)), BlobSHA256: hex.EncodeToString(chunkDigest[:])}
		chunk := &models.RecordingAudioChunk{ChunkIndex: decoded.ChunkIndex, TimelineStartMS: decoded.TimelineStartMS, AudioDurationMS: decoded.AudioDurationMS}
		publicationError = self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			if err := self.recordingAuthority(transaction, run, transcriptRun.ProviderModelName, false); err != nil {
				return err
			}
			_, err := transaction.CreateRecordingAudioChunk(run.Agent.UserID, transcriptRun.ID, claim, chunk, blob, int64(maximumAccountBytes))
			return err
		})
		if publicationError != nil {
			return publicationError
		}
		publicationError = self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			if _, err := transaction.LockRecording(run.Agent.UserID, transcriptRun.RecordingID); err != nil {
				return err
			}
			if _, err := transaction.GetRecordingBlob(run.Agent.UserID, blob.ID); err != nil {
				return err
			}
			if err := files.PutFileStream(ctx, blob.ID, bytes.NewReader(decoded.WaveBytes), blob.BlobSizeBytes); err != nil {
				return err
			}
			return transaction.VerifyRecordingBlob(run.Agent.UserID, blob.ID, blob.BlobSHA256)
		})
		return publicationError
	})
	if err != nil {
		if publicationError != nil || ctx.Err() != nil {
			return err
		}
		return self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingFailed, err.Error(), transcriptRun.DerivedDurationMS)
	}
	if err := self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingTranscribing, "", durationMS); err != nil {
		return err
	}
	transcriptRun.DerivedDurationMS = durationMS
	transcriptRun.RecordingProcessingState = models.RecordingProcessingTranscribing
	return nil
}

// runRecording never enters Ask, a conversation, speech synthesis or tool execution.
func (self *Agent) runRecording(ctx context.Context, run *Run) error {
	ctx, cancelRecording := context.WithCancelCause(ctx)
	defer cancelRecording(nil)
	release := self.holdRunningWork("recording-"+run.Job.SubjectID, cancelRecording)
	defer release()
	var transcriptRun *models.RecordingTranscriptRun
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		transcriptRun, err = transaction.GetRecordingTranscriptRun(run.Agent.UserID, run.Job.SubjectID)
		return err
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrRecordingDeleted) {
			return nil
		}
		return err
	}
	switch transcriptRun.RecordingProcessingState {
	case models.RecordingProcessingComplete, models.RecordingProcessingFailed, models.RecordingProcessingCanceled, models.RecordingProcessingUncertain:
		return nil
	}
	files, isStreaming := self.settings.Storage.(storage.StreamFiles)
	if !isStreaming {
		return fmt.Errorf("recording: streaming storage is unavailable")
	}
	if self.settings.Registry == nil {
		return &Deferral{Until: time.Now().Add(time.Minute), Reason: "recording transcription is paused until its account, agent and provider are available"}
	}
	selection, err := self.settings.Registry.TranscribingFor(transcriptRun.ProviderModelName, true, false)
	if err != nil {
		return self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingFailed, err.Error(), transcriptRun.DerivedDurationMS)
	}
	if transcriptRun.RecordingProcessingState != models.RecordingProcessingTranscribing {
		if err := self.prepareRecordingAudio(ctx, run, transcriptRun, selection, files); err != nil {
			return err
		}
		if transcriptRun.RecordingProcessingState != models.RecordingProcessingTranscribing {
			return nil
		}
	}
	var chunks []*models.RecordingAudioChunk
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		chunks, err = transaction.ListRecordingAudioChunks(run.Agent.UserID, transcriptRun.ID)
		return err
	}); err != nil {
		return err
	}
	for _, chunk := range chunks {
		shouldContinue, err := self.transcribeRecordingChunk(ctx, run, transcriptRun, selection, files, chunk)
		if err != nil {
			return err
		}
		if !shouldContinue {
			return nil
		}
	}
	var isCaptureFinished bool
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := transaction.UpdateRecordingTranscriptProgress(run.Agent.UserID, transcriptRun.ID, db.RecordingWorkerClaim{JobID: run.Job.ID, ClaimID: run.Job.ClaimID}, models.RecordingProcessingComplete, "", transcriptRun.DerivedDurationMS); err != nil {
			return err
		}
		if err := transaction.QueueRecordingSpeakerCuration(run.Agent.UserID, transcriptRun.RecordingID); err != nil {
			return err
		}
		source, err := transaction.GetRecording(run.Agent.UserID, transcriptRun.RecordingID)
		if err != nil {
			return err
		}
		// A piece of a capture that is still being recorded is partial: the
		// memory sync re-reads every segment and the naming job reads the
		// whole transcript, so repeating both after every piece would cost a
		// model call per piece for text that is about to change. Stopping the
		// capture does both once (FinishRecordingCapture).
		isCaptureFinished = isRecordingCaptureFinished(source)
		if !isCaptureFinished {
			return nil
		}
		return transaction.SyncRecordingKnowledge(run.Agent.UserID, transcriptRun.RecordingID)
	}); err != nil {
		return err
	}
	self.WakeRecordingSpeakerCuration(run.Agent.UserID, transcriptRun.RecordingID)
	if isCaptureFinished {
		self.queueRecordingSummary(ctx, run.Agent.UserID, transcriptRun.RecordingID)
	}
	return nil
}

// isRecordingCaptureFinished says whether a recording's capture has ended, so
// its transcript no longer grows: an import, or a capture the person stopped.
func isRecordingCaptureFinished(source *models.Recording) bool {
	return source.RecordingCaptureState == models.RecordingCaptureStopped || source.RecordingCaptureState == models.RecordingCaptureImported
}

// FinishRecordingCapture does, once, the work runs of an unfinished capture
// skipped: the memory sync and the naming job. The caller has just moved the
// capture to stopped. When some piece is still being transcribed, nothing is
// done here, because that piece's own completion sees a stopped capture and
// does the same.
func (self *Agent) FinishRecordingCapture(transaction db.Transaction, userId, recordingId string) error {
	hasUnfinishedPiece, err := transaction.HasUnfinishedRecordingTranscription(userId, recordingId)
	if err != nil || hasUnfinishedPiece {
		return err
	}
	runs, err := transaction.ListRecordingTranscriptRuns(userId, recordingId)
	if err != nil {
		return err
	}
	hasCompletedRun := false
	for _, transcriptRun := range runs {
		hasCompletedRun = hasCompletedRun || transcriptRun.RecordingProcessingState == models.RecordingProcessingComplete
	}
	if !hasCompletedRun {
		return nil
	}
	if err := transaction.SyncRecordingKnowledge(userId, recordingId); err != nil {
		return err
	}
	return self.QueueRecordingSummary(transaction, userId, recordingId)
}

func (self *Agent) transcribeRecordingChunk(ctx context.Context, run *Run, transcriptRun *models.RecordingTranscriptRun, selection *llm.TranscriptionSelection, files storage.StreamFiles, chunk *models.RecordingAudioChunk) (bool, error) {
	claim := db.RecordingWorkerClaim{JobID: run.Job.ID, ClaimID: run.Job.ClaimID}
	var attempt *models.RecordingTranscriptionAttempt
	var blob *models.RecordingBlob
	configuration := self.settings.Configuration()
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		attempt, err = transaction.GetLatestRecordingChunkAttempt(run.Agent.UserID, chunk.ID)
		return err
	}); err != nil {
		return false, err
	}
	if attempt != nil {
		if attempt.RecordingAttemptState == models.RecordingAttemptComplete {
			return true, nil
		}
		if attempt.RecordingAttemptState == models.RecordingAttemptSubmitted || attempt.RecordingAttemptState == models.RecordingAttemptUncertain {
			return false, self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingUncertain, "a previous provider request has an unknown outcome and was not repeated", transcriptRun.DerivedDurationMS)
		}
	}
	speakerReferences, referenceBlobIds, err := self.recordingSpeakerReferences(ctx, run.Agent.UserID, selection.Capabilities, files)
	if err != nil {
		return false, err
	}
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		attempt, err = transaction.CreateRecordingTranscriptionAttempt(run.Agent.UserID, transcriptRun.ID, chunk.ID, claim, configuration.Agent.CurrencyOf(), referenceBlobIds)
		if err != nil {
			return err
		}
		blob, err = transaction.GetRecordingBlob(run.Agent.UserID, chunk.RecordingBlobID)
		return err
	}); err != nil {
		return false, err
	}
	if attempt.RecordingAttemptState == models.RecordingAttemptComplete {
		return true, nil
	}
	if attempt.RecordingAttemptState == models.RecordingAttemptSubmitted || attempt.RecordingAttemptState == models.RecordingAttemptUncertain {
		return false, self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingUncertain, "a previous provider request has an unknown outcome and was not repeated", transcriptRun.DerivedDurationMS)
	}
	if !blob.IsStoredVerified || blob.BlobSizeBytes > selection.Capabilities.MaximumAudioBytes {
		return false, fmt.Errorf("recording: derived audio is not verified or exceeds the provider bound")
	}
	stream, err := files.OpenFileRange(ctx, blob.ID, 0, blob.BlobSizeBytes)
	if err != nil {
		return false, err
	}
	audioBytes, readError := io.ReadAll(io.LimitReader(stream.ReadCloser, blob.BlobSizeBytes+1))
	_ = stream.Close()
	if readError != nil {
		return false, readError
	}
	digest := sha256.Sum256(audioBytes)
	if int64(len(audioBytes)) != blob.BlobSizeBytes || hex.EncodeToString(digest[:]) != blob.BlobSHA256 {
		return false, fmt.Errorf("recording: derived audio failed its integrity check")
	}
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := self.recordingAuthority(transaction, run, transcriptRun.ProviderModelName, true); err != nil {
			return err
		}
		confirmedNames := make([]string, 0, len(speakerReferences))
		for _, reference := range speakerReferences {
			confirmedNames = append(confirmedNames, reference.SpeakerName)
		}
		if err := transaction.CheckRecordingVoiceReferenceNames(run.Agent.UserID, referenceBlobIds, confirmedNames); err != nil {
			if errors.Is(err, db.ErrAlreadyExists) {
				return &Deferral{Until: time.Now().Add(time.Minute), Reason: "speaker references changed before submission"}
			}
			return err
		}
		return transaction.SubmitRecordingTranscriptionAttempt(run.Agent.UserID, attempt.ID, claim)
	}); err != nil {
		var deferral *Deferral
		if errors.As(err, &deferral) {
			if progressError := self.recordingProgress(ctx, run, transcriptRun, models.RecordingProcessingTranscribing, deferral.Reason, transcriptRun.DerivedDurationMS); progressError != nil {
				return false, progressError
			}
		}
		return false, err
	}
	response, providerError := selection.Transcriber.Transcribe(ctx, &llm.TranscriptionRequest{Model: selection.Model, MediaFilename: "recording.wav", MediaContentType: "audio/wav", MediaBytes: audioBytes, MediaDurationMS: chunk.AudioDurationMS, IsSpeakerDiarizationRequired: true, SpeakerReferences: speakerReferences})
	finished := &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptUncertain, AttemptErrorMessage: "provider request did not return a verified transcript; it was not repeated"}
	var segments []*models.RecordingTranscriptSegment
	var usage llm.Usage
	if providerError == nil && response != nil {
		finished.RecordingAttemptState = models.RecordingAttemptComplete
		finished.AttemptErrorMessage = ""
		reported := response.TranscriptionUsage
		usage = reported.Usage
		finished.InputAudioTokens = int64(reported.InputAudioTokens)
		finished.InputTextTokens = int64(reported.InputTextTokens)
		finished.PromptTokens = int64(usage.PromptTokens)
		finished.CompletionTokens = int64(usage.CompletionTokens)
		finished.ProviderAudioDurationMS = reported.AudioDurationMS
		finished.HasProviderUsage = reported.HasProviderUsage
		finished.ProviderUsageJSON = reported.ProviderUsageJSON
		finished.ProviderResponseJSON = response.ProviderResponseJSON
		providerName, modelName, _ := strings.Cut(transcriptRun.ProviderModelName, ":")
		if provider := configuration.Agent.Provider(providerName); provider != nil && reported.HasProviderUsage && (reported.AudioDurationMS == 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
			pricing := provider.PricingFor(modelName)
			if pricing.Input > 0 || pricing.Output > 0 {
				estimatedCostAmount := configuration.Agent.CostOf(transcriptRun.ProviderModelName, usage.PromptTokens, usage.CompletionTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
				finished.EstimatedCostAmount = &estimatedCostAmount
			}
		}
		for _, segment := range response.Segments {
			segments = append(segments, &models.RecordingTranscriptSegment{LocalSpeakerID: segment.SpeakerID, SegmentStartMS: chunk.TimelineStartMS + segment.StartMS, SegmentEndMS: chunk.TimelineStartMS + segment.EndMS, TranscriptText: segment.TranscriptText})
		}
	}
	completionContext, cancelCompletion := context.WithTimeout(context.WithoutCancel(ctx), jobCompletionTimeout)
	defer cancelCompletion()
	if err := self.settings.Database.TransactionContext(completionContext, func(transaction db.Transaction) error {
		isFirstPublication, err := transaction.FinishRecordingTranscriptionAttempt(run.Agent.UserID, attempt.ID, claim, finished, segments)
		if err != nil {
			return err
		}
		if isFirstPublication {
			return RecordUsageIn(transaction, run.Agent.ID, "", transcriptRun.ProviderModelName, string(models.AgentJobRecording), usage)
		}
		return nil
	}); err != nil {
		return false, err
	}
	var currentRun *models.RecordingTranscriptRun
	if err := self.settings.Database.TransactionContext(completionContext, func(transaction db.Transaction) (err error) {
		currentRun, err = transaction.GetRecordingTranscriptRun(run.Agent.UserID, transcriptRun.ID)
		return err
	}); err != nil {
		return false, err
	}
	if currentRun.RecordingProcessingState == models.RecordingProcessingCanceled {
		return false, nil
	}
	if finished.RecordingAttemptState != models.RecordingAttemptComplete {
		return false, self.recordingProgress(completionContext, run, transcriptRun, models.RecordingProcessingUncertain, finished.AttemptErrorMessage, transcriptRun.DerivedDurationMS)
	}
	return true, nil
}

// CancelRecordingTranscription stops local work only after its durable state commits.
func (self *Agent) CancelRecordingTranscription(transaction db.Transaction, userId, runId string) (*models.RecordingTranscriptRun, error) {
	transcriptRun, err := transaction.CancelRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	transaction.AfterCommit(func() { self.cancelRunningWork("recording-" + runId) })
	return transcriptRun, nil
}

func (self *Agent) queueRecordingSpeakerCuration(ctx context.Context) {
	var sources []*models.Recording
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		sources, err = transaction.ListRecordingsAwaitingSpeakerCuration(25)
		return err
	}); err != nil {
		log.Warningf("cannot find recording speakers awaiting curation: %s", err)
		return
	}
	for _, source := range sources {
		self.WakeRecordingSpeakerCuration(source.UserID, source.ID)
	}
}

// RetryRecordingTranscription preserves completed chunks and refuses unknown paid outcomes.
func (self *Agent) RetryRecordingTranscription(transaction db.Transaction, userId, runId string) (*models.RecordingTranscriptRun, error) {
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled || configuration.Agent.Models.Transcription == "" || self.settings.Registry == nil {
		return nil, ErrUnavailable
	}
	transcriptRun, err := transaction.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return nil, err
	}
	assistant, err := transaction.GetAgentByUser(userId)
	if err != nil {
		return nil, err
	}
	if assistant == nil || !assistant.Active() {
		return nil, ErrUnavailable
	}
	owner, err := transaction.GetUser(userId)
	if err != nil {
		return nil, err
	}
	if err := self.recordingAuthority(transaction, &Run{Agent: assistant, Owner: owner}, transcriptRun.ProviderModelName, false); err != nil {
		return nil, err
	}
	if _, err := self.settings.Registry.TranscribingFor(transcriptRun.ProviderModelName, true, false); err != nil {
		return nil, err
	}
	transcriptRun, err = transaction.RetryRecordingTranscriptRun(userId, runId, assistant.ID)
	if err != nil {
		return nil, err
	}
	if _, err := self.Enqueue(transaction, models.AgentJobRecording, assistant.ID, "", transcriptRun.ID); err != nil {
		return nil, err
	}
	return transcriptRun, nil
}
