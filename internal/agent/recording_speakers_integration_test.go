package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type failingRecordingReferenceStore struct {
	storage.Storage
	storage.StreamFiles
}

func (self *failingRecordingReferenceStore) PutFileStream(ctx context.Context, blobId string, content io.ReadSeeker, sizeBytes int64) error {
	return errors.New("fixture reference storage failure")
}

func TestRecordingCurationRetriesReservedBytesAndKeepsRevocation(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "curation-owner")
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	files := store.(storage.StreamFiles)
	configuration := &config.Configuration{}
	worker := New(&Settings{Database: database, Storage: store, Configuration: func() *config.Configuration { return configuration }})
	var source *models.Recording
	var media *models.RecordingMedia
	var transcriptRun *models.RecordingTranscriptRun
	var claim db.RecordingWorkerClaim
	var attempt *models.RecordingTranscriptionAttempt
	samples := make([]byte, 16000*2*6)
	for sampleIndex := 0; sampleIndex < len(samples)/2; sampleIndex++ {
		if sampleIndex%16000 > 2000 {
			sample := int16(4000 * math.Sin(float64(sampleIndex)*0.1))
			binary.LittleEndian.PutUint16(samples[sampleIndex*2:], uint16(sample))
		}
	}
	waveBytes := recordingReferenceWave(samples)
	digest := sha256.Sum256(waveBytes)
	digestText := hex.EncodeToString(digest[:])
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ownerAgent, err := transaction.CreateAgent(&models.Agent{UserID: userId, Name: "Fixture recording assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: "curation-fixture", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		media, err = transaction.CreateRecordingMedia(userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "curation-original", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(waveBytes)), OriginalSHA256: digestText}, 1<<20, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutRecordingMediaPart(userId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: media.OriginalSizeBytes, PartSHA256: digestText}); err != nil {
			test.Fatal(err)
		}
		if _, _, err := transaction.CompleteRecordingMedia(userId, media.ID, digestText); err != nil {
			test.Fatal(err)
		}
		transcriptRun, err = transaction.CreateRecordingTranscriptRun(userId, media.ID, ownerAgent.ID, "initial", "fixture:diarize", false)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: ownerAgent.ID, Kind: models.AgentJobRecording, SubjectID: transcriptRun.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("curation-fixture-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal(err)
		}
		claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
		blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(transcriptRun.ID, 0), BlobSizeBytes: int64(len(waveBytes)), BlobSHA256: digestText}
		chunk, err := transaction.CreateRecordingAudioChunk(userId, transcriptRun.ID, claim, &models.RecordingAudioChunk{ChunkIndex: 0, AudioDurationMS: 6000}, blob, 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		if err := files.PutFileStream(test.Context(), blob.ID, bytes.NewReader(waveBytes), int64(len(waveBytes))); err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(userId, blob.ID, digestText); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingTranscribing, "", 6000); err != nil {
			test.Fatal(err)
		}
		attempt, err = transaction.CreateRecordingTranscriptionAttempt(userId, transcriptRun.ID, chunk.ID, claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(userId, attempt.ID, claim); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(userId, attempt.ID, claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 0, SegmentEndMS: 6000, TranscriptText: "A synthetic fixture statement."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingComplete, "", 6000); err != nil {
			test.Fatal(err)
		}
	})
	if err := worker.CurateRecordingSpeakers(test.Context(), userId, source.ID); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(userId, source.ID)
		if err != nil || len(references) != 0 {
			test.Fatal("unconfirmed model speaker produced a reference", err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		current, err := transaction.GetRecording(userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Confirmed Fixture", ClientCorrectionID: "fixture-name", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: current.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
	})
	worker.settings.Storage = &failingRecordingReferenceStore{Storage: store, StreamFiles: files}
	if err := worker.CurateRecordingSpeakers(test.Context(), userId, source.ID); err == nil {
		test.Fatal("fixture storage failure was not returned")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(userId, source.ID)
		if err != nil || len(references) != 1 {
			test.Fatal("failed byte write lost durable cleanup reservation", err)
		}
		usable, err := transaction.ListUsableRecordingVoiceReferences(userId, 4)
		if err != nil || len(usable) != 0 {
			test.Fatal("unwritten sample became a usable reference", err)
		}
	})
	worker.settings.Storage = store
	if err := worker.CurateRecordingSpeakers(test.Context(), userId, source.ID); err != nil {
		test.Fatal(err)
	}
	var referenceBlobId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(userId, source.ID)
		if err != nil || len(references) != 1 {
			test.Fatal("reference retry duplicated reservation", err)
		}
		referenceBlobId = references[0].RecordingBlobID
		usable, err := transaction.ListUsableRecordingVoiceReferences(userId, 4)
		if err != nil || len(usable) != 1 {
			test.Fatal("successful retry did not publish verified reference", err)
		}
		current, err := transaction.GetRecording(userId, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RevokeRecordingVoiceReference(userId, source.ID, references[0].ID, current.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	if err := worker.CurateRecordingSpeakers(test.Context(), userId, source.ID); err != nil {
		test.Fatal(err)
	}
	if _, err := files.OpenFileRange(test.Context(), referenceBlobId, 0, 1); err == nil {
		test.Fatal("revoked reference bytes remain readable")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(userId, source.ID)
		if err != nil || len(references) != 1 || !references[0].IsRevoked || references[0].RecordingBlobID != "" {
			test.Fatal("revocation did not remain after byte cleanup", err)
		}
	})
}
