package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

func TestRecordingUploadDecodeAndWorkerEvidencePipeline(test *testing.T) {
	if _, err := recording.NewAudioDecoder(test.Context()); err != nil {
		if os.Getenv("TEANODE_RECORDING_DECODER_REQUIRED") == "1" {
			test.Fatal(err)
		}
		test.Skip("descriptor-capable local media readers are unavailable")
	}
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	files := store.(storage.StreamFiles)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Models.Transcription = "fixture:speaker-model"
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai"}}
	userId := dbtest.CreateUser(test, database, "pipeline-owner")
	var owner *models.User
	var assistant *models.Agent
	var source *models.Recording
	var media *models.RecordingMedia
	var transcriptRun *models.RecordingTranscriptRun
	var job *models.AgentJob
	samples := make([]byte, 16000*2*5/2)
	for sampleIndex := 0; sampleIndex < len(samples)/2; sampleIndex++ {
		binary.LittleEndian.PutUint16(samples[sampleIndex*2:], uint16(1000))
	}
	originalBytes := recordingReferenceWave(samples)
	digest := sha256.Sum256(originalBytes)
	digestText := hex.EncodeToString(digest[:])
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "Recording pipeline", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Pipeline accounts", RoleIDs: []string{role.ID}, UserIDs: []string{userId}}); err != nil {
			test.Fatal(err)
		}
		owner, err = transaction.GetUser(userId)
		if err != nil {
			test.Fatal(err)
		}
		assistant, err = transaction.CreateAgent(&models.Agent{UserID: userId, Name: "Pipeline assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: "pipeline-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		media, err = transaction.CreateRecordingMedia(userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "pipeline-media", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(originalBytes)), OriginalSHA256: digestText, TimelineStartMS: 7000}, 1<<20, 2<<20)
		if err != nil {
			test.Fatal(err)
		}
	})
	mediaStore := recording.New(database, files)
	if _, err := mediaStore.PutPart(test.Context(), userId, media.ID, 0, digestText, originalBytes); err != nil {
		test.Fatal(err)
	}
	if _, isFirstCompletion, err := mediaStore.Complete(test.Context(), userId, media.ID); err != nil || !isFirstCompletion {
		test.Fatal("whole original was not verified", err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		transcriptRun, err = transaction.CreateRecordingTranscriptRun(userId, media.ID, assistant.ID, "initial", "fixture:speaker-model", false)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: assistant.ID, Kind: models.AgentJobRecording, SubjectID: transcriptRun.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("pipeline-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal("worker claim unavailable", err)
		}
		job = jobs[0]
	})
	worker := New(&Settings{Database: database, Storage: store, Configuration: func() *config.Configuration { return configuration }})
	run := &Run{Job: job, Agent: assistant, Owner: owner, settings: worker.settings}
	provider := &recordingFixtureTranscriber{}
	capabilities := provider.TranscriptionCapabilities("speaker-model")
	capabilities.MaximumAudioBytes = 32044
	selection := &llm.TranscriptionSelection{Transcriber: provider, Name: "fixture:speaker-model", Model: "speaker-model", Capabilities: capabilities}
	if err := worker.prepareRecordingAudio(test.Context(), run, transcriptRun, selection, files); err != nil {
		test.Fatal(err)
	}
	var chunks []*models.RecordingAudioChunk
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		chunks, err = transaction.ListRecordingAudioChunks(userId, transcriptRun.ID)
		if err != nil || len(chunks) != 3 || transcriptRun.DerivedDurationMS != 2500 {
			test.Fatal("decoder did not retain bounded aligned chunks", err)
		}
	})
	for _, chunk := range chunks {
		for retryIndex := 0; retryIndex < 2; retryIndex++ {
			if shouldContinue, err := worker.transcribeRecordingChunk(test.Context(), run, transcriptRun, selection, files, chunk); err != nil || !shouldContinue {
				test.Fatal("worker could not publish or reuse decoded evidence", err)
			}
		}
	}
	if provider.callCount != 3 {
		test.Fatal("completed chunks were retranscribed")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, db.RecordingWorkerClaim{JobID: job.ID, ClaimID: job.ClaimID}, models.RecordingProcessingComplete, "", transcriptRun.DerivedDurationMS); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SyncRecordingKnowledge(userId, source.ID); err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(userId, transcriptRun.ID)
		if err != nil || len(segments) != 3 || segments[0].SegmentStartMS != 7100 || segments[2].SegmentStartMS != 9100 || segments[2].SegmentEndMS != 9500 {
			test.Fatal("provider timestamps lost original timeline alignment", err)
		}
		usage, err := transaction.SumAgentUsage(assistant.ID, time.Now().Add(-time.Hour))
		if err != nil || usage.Calls != 3 || usage.PromptTokens != 42 {
			test.Fatal("worker usage was duplicated or missing", err)
		}
	})
	var playedBytes bytes.Buffer
	if err := mediaStore.CopyRange(test.Context(), userId, media.ID, &playedBytes, 0, media.OriginalSizeBytes); err != nil || !bytes.Equal(playedBytes.Bytes(), originalBytes) {
		test.Fatal("processing changed original playback bytes", err)
	}
	if dbtest.QueryString(test, database, "SELECT COUNT(*)::text FROM agent_document") != "3" || dbtest.QueryString(test, database, "SELECT COUNT(*)::text FROM agent_message") != "0" {
		test.Fatal("speech was not retained exclusively as indexed evidence")
	}
}
