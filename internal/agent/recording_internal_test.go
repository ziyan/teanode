package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type recordingFixtureTranscriber struct {
	callCount          int
	hasProviderFailure bool
}

func (self *recordingFixtureTranscriber) TranscriptionCapabilities(string) llm.TranscriptionCapabilities {
	return llm.TranscriptionCapabilities{CanTranscribe: true, HasSpeakerDiarization: true, HasSegmentTimestamps: true, MaximumAudioBytes: 25 << 20}
}
func (self *recordingFixtureTranscriber) Transcribe(_ context.Context, request *llm.TranscriptionRequest) (*llm.TranscriptionResponse, error) {
	self.callCount++
	if self.hasProviderFailure {
		return nil, errors.New("fixture connection lost after submission")
	}
	return &llm.TranscriptionResponse{Model: request.Model, Segments: []llm.TranscriptSegment{{SpeakerID: "local-speaker", StartMS: 100, EndMS: min(900, request.MediaDurationMS), TranscriptText: "Deploy the fixture."}, {SpeakerID: "local-speaker", StartMS: 900, EndMS: min(950, request.MediaDurationMS), TranscriptText: " "}}, ProviderResponseJSON: json.RawMessage("{}"), TranscriptionUsage: llm.TranscriptionUsage{HasProviderUsage: true, InputAudioTokens: 14, Usage: llm.Usage{PromptTokens: 14, CompletionTokens: 4}}}, nil
}

func TestRecordingChunkRetriesPreserveUsageEvidenceAndUnknownOutcomes(test *testing.T) {
	for _, hasProviderFailure := range []bool{false, true} {
		test.Run(map[bool]string{false: "success", true: "unknown"}[hasProviderFailure], func(test *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(test)
			defer closeDatabase()
			files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
			if err != nil {
				test.Fatal(err)
			}
			defer func() { _ = files.Close() }()
			configuration := config.Default()
			configuration.Agent.Enabled = true
			configuration.Agent.Models.Transcription = "fixture:speaker-model"
			configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai"}}
			ownerId := dbtest.CreateUser(test, database, "recording-worker-owner")
			var owner *models.User
			var assistant *models.Agent
			var transcriptRun *models.RecordingTranscriptRun
			var chunk *models.RecordingAudioChunk
			var job *models.AgentJob
			content := []byte("derived fixture bytes")
			digest := sha256.Sum256(content)
			digestText := hex.EncodeToString(digest[:])
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				role, err := transaction.CreateRole(&models.Role{Name: "Recording access", Permissions: []models.Permission{models.PermissionAgentUse}})
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.CreateGroup(&models.Group{Name: "Recording people", RoleIDs: []string{role.ID}, UserIDs: []string{ownerId}}); err != nil {
					test.Fatal(err)
				}
				owner, err = transaction.GetUser(ownerId)
				if err != nil {
					test.Fatal(err)
				}
				assistant, err = transaction.CreateAgent(&models.Agent{UserID: ownerId, Enabled: true, Name: "Recording assistant"})
				if err != nil {
					test.Fatal(err)
				}
				source, err := transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "fixture-source", RecordingOrigin: models.RecordingOriginAudioImport})
				if err != nil {
					test.Fatal(err)
				}
				media, err := transaction.CreateRecordingMedia(ownerId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "fixture-media", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(content)), OriginalSHA256: digestText}, 1024, 4096)
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.PutRecordingMediaPart(ownerId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: int64(len(content)), PartSHA256: digestText}); err != nil {
					test.Fatal(err)
				}
				if _, _, err := transaction.CompleteRecordingMedia(ownerId, media.ID, digestText); err != nil {
					test.Fatal(err)
				}
				transcriptRun, err = transaction.CreateRecordingTranscriptRun(ownerId, media.ID, assistant.ID, "initial", "fixture:speaker-model", false)
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: assistant.ID, Kind: models.AgentJobRecording, SubjectID: transcriptRun.ID}); err != nil {
					test.Fatal(err)
				}
			})
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				jobs, err := transaction.ClaimAgentJobs("fixture-worker", 1, time.Now())
				if err != nil || len(jobs) != 1 {
					test.Fatal("claim failed", err)
				}
				job = jobs[0]
				claim := db.RecordingWorkerClaim{JobID: job.ID, ClaimID: job.ClaimID}
				if err := transaction.UpdateRecordingTranscriptProgress(ownerId, transcriptRun.ID, claim, models.RecordingProcessingPreparing, "", 0); err != nil {
					test.Fatal(err)
				}
				blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(transcriptRun.ID, 0), BlobSizeBytes: int64(len(content)), BlobSHA256: digestText}
				chunk, err = transaction.CreateRecordingAudioChunk(ownerId, transcriptRun.ID, claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 0, AudioDurationMS: 1000}, blob, 4096)
				if err != nil {
					test.Fatal(err)
				}
				if err := files.PutFile(test.Context(), blob.ID, content); err != nil {
					test.Fatal(err)
				}
				if err := transaction.VerifyRecordingBlob(ownerId, blob.ID, digestText); err != nil {
					test.Fatal(err)
				}
				if err := transaction.UpdateRecordingTranscriptProgress(ownerId, transcriptRun.ID, claim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
					test.Fatal(err)
				}
			})
			transcriptRun.DerivedDurationMS = 1000
			transcriptRun.RecordingProcessingState = models.RecordingProcessingTranscribing
			worker := &Agent{settings: &Settings{Database: database, Storage: files, Configuration: func() *config.Configuration { return configuration }}}
			run := &Run{Job: job, Agent: assistant, Owner: owner, settings: worker.settings}
			provider := &recordingFixtureTranscriber{hasProviderFailure: hasProviderFailure}
			selection := &llm.TranscriptionSelection{Transcriber: provider, Name: "fixture:speaker-model", Model: "speaker-model", Capabilities: provider.TranscriptionCapabilities("speaker-model")}
			configuration.Agent.Limits.DailyTokensPerAgent = 1
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				if err := RecordUsageIn(transaction, assistant.ID, "", "fixture:speaker-model", "fixture", llm.Usage{PromptTokens: 1}); err != nil {
					test.Fatal(err)
				}
			})
			shouldContinue, budgetError := worker.transcribeRecordingChunk(test.Context(), run, transcriptRun, selection, files.(storage.StreamFiles), chunk, "")
			var deferral *Deferral
			if shouldContinue || !errors.As(budgetError, &deferral) || provider.callCount != 0 {
				test.Fatal("exhausted budget submitted paid transcription", budgetError)
			}
			configuration.Agent.Limits.DailyTokensPerAgent = 200000
			for attemptIndex := 0; attemptIndex < 2; attemptIndex++ {
				if hasProviderFailure && attemptIndex == 1 {
					if err := worker.runRecording(test.Context(), run); err != nil {
						test.Fatal(err)
					}
					continue
				}
				shouldContinue, err := worker.transcribeRecordingChunk(test.Context(), run, transcriptRun, selection, files.(storage.StreamFiles), chunk, "")
				if err != nil || shouldContinue == hasProviderFailure {
					test.Fatal("unexpected chunk outcome", shouldContinue, err)
				}
			}
			if provider.callCount != 1 {
				test.Fatal("retry repeated a potentially paid request")
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				attempts, err := transaction.ListRecordingTranscriptionAttempts(ownerId, transcriptRun.ID)
				if err != nil || len(attempts) != 1 || attempts[0].ActualCostAmount != nil || attempts[0].EstimatedCostAmount != nil {
					test.Fatal("billing provenance was guessed", err)
				}
				segments, err := transaction.ListRecordingTranscriptSegments(ownerId, transcriptRun.ID)
				if err != nil {
					test.Fatal(err)
				}
				usage, err := transaction.SumAgentUsage(assistant.ID, time.Now().Add(-time.Hour))
				if err != nil {
					test.Fatal(err)
				}
				if !hasProviderFailure && (len(segments) != 1 || segments[0].TranscriptText != "Deploy the fixture." || usage.Calls != 2 || usage.PromptTokens != 15) {
					test.Fatal("evidence or usage was duplicated or lost")
				}
				if hasProviderFailure && (len(segments) != 0 || usage.Calls != 2) {
					test.Fatal("unknown provider result was invented")
				}
			})
			if dbtest.QueryString(test, database, "SELECT COUNT(*)::text FROM agent_message") != "0" {
				test.Fatal("recorded speech became a conversation command")
			}
		})
	}
}
