package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
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

type recordingCurationFixture struct {
	database db.Database
	worker   *Agent
	userId   string
	source   *models.Recording
	files    storage.StreamFiles
	store    storage.Storage
	attempt  *models.RecordingTranscriptionAttempt
}

func newRecordingCurationFixture(test *testing.T, passageCount int) *recordingCurationFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
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
	samples := make([]byte, 16000*2*6*passageCount)
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
		media, err = transaction.CreateRecordingMedia(userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "curation-original", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(waveBytes)), OriginalSHA256: digestText}, 16<<20, 16<<20)
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
		chunk, err := transaction.CreateRecordingAudioChunk(userId, transcriptRun.ID, claim, &models.RecordingAudioChunk{ChunkIndex: 0, AudioDurationMS: int64(6000 * passageCount)}, blob, 16<<20)
		if err != nil {
			test.Fatal(err)
		}
		if err := files.PutFileStream(test.Context(), blob.ID, bytes.NewReader(waveBytes), int64(len(waveBytes))); err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(userId, blob.ID, digestText); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingTranscribing, "", int64(6000*passageCount)); err != nil {
			test.Fatal(err)
		}
		attempt, err = transaction.CreateRecordingTranscriptionAttempt(userId, transcriptRun.ID, chunk.ID, claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(userId, attempt.ID, claim); err != nil {
			test.Fatal(err)
		}
		generatedPassages := make([]*models.RecordingTranscriptSegment, 0, passageCount)
		for passageIndex := 0; passageIndex < passageCount; passageIndex++ {
			localSpeakerId := "speaker-one"
			if passageIndex > 1 {
				localSpeakerId = fmt.Sprintf("speaker-%d", passageIndex)
			}
			generatedPassages = append(generatedPassages, &models.RecordingTranscriptSegment{LocalSpeakerID: localSpeakerId, SegmentStartMS: int64(passageIndex * 6000), SegmentEndMS: int64((passageIndex + 1) * 6000), TranscriptText: "A synthetic fixture statement."})
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(userId, attempt.ID, claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, generatedPassages); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(userId, transcriptRun.ID, claim, models.RecordingProcessingComplete, "", int64(6000*passageCount)); err != nil {
			test.Fatal(err)
		}
	})
	return &recordingCurationFixture{database: database, worker: worker, userId: userId, source: source, files: files, store: store, attempt: attempt}
}

func TestRecordingCurationRetriesReservedBytesAndKeepsRevocation(test *testing.T) {
	fixture := newRecordingCurationFixture(test, 1)
	database, worker, userId, source, files, store, attempt := fixture.database, fixture.worker, fixture.userId, fixture.source, fixture.files, fixture.store, fixture.attempt

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

func TestRecordingCuratedReferenceSelectionCapsOwnershipAndRevocation(test *testing.T) {
	fixture := newRecordingCurationFixture(test, 6)
	for passageIndex := 0; passageIndex < 6; passageIndex++ {
		if passageIndex == 1 {
			continue
		}
		localSpeakerId := "speaker-one"
		if passageIndex > 1 {
			localSpeakerId = fmt.Sprintf("speaker-%d", passageIndex)
		}
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
			if err != nil {
				test.Fatal(err)
			}
			_, err = transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: fixture.attempt.ID, LocalSpeakerID: localSpeakerId, ConfirmedName: fmt.Sprintf("Fixture Participant %d", passageIndex), ClientCorrectionID: fmt.Sprintf("name-%d", passageIndex), PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration})
			if err != nil {
				test.Fatal(err)
			}
		})
	}
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	capabilities := llm.TranscriptionCapabilities{HasSpeakerReferences: true, MaximumSpeakerReferenceCount: 10}
	selected, blobIds, err := fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, capabilities, fixture.files)
	if err != nil || len(selected) != 4 || len(blobIds) != 4 {
		test.Fatal("reference selection exceeded or missed the four-name cap", err, len(selected))
	}
	confirmedNames := make([]string, 0, len(selected))
	seenNames := map[string]bool{}
	for _, reference := range selected {
		if seenNames[reference.SpeakerName] {
			test.Fatal("same person selected twice")
		}
		seenNames[reference.SpeakerName] = true
		confirmedNames = append(confirmedNames, reference.SpeakerName)
	}
	capabilities.MaximumSpeakerReferenceCount = 2
	smaller, _, err := fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, capabilities, fixture.files)
	if err != nil || len(smaller) != 2 {
		test.Fatal("provider reference limit ignored", err, len(smaller))
	}
	capabilities.MaximumSpeakerReferenceCount = 0
	absent, _, err := fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, capabilities, fixture.files)
	if err != nil || len(absent) != 0 {
		test.Fatal("zero model capability admitted references", err)
	}
	capabilities.MaximumSpeakerReferenceCount = 4
	capabilities.HasSpeakerReferences = false
	absent, _, err = fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, capabilities, fixture.files)
	if err != nil || len(absent) != 0 {
		test.Fatal("unsupported model admitted references", err)
	}
	capabilities.HasSpeakerReferences = true
	otherUserId := dbtest.CreateUser(test, fixture.database, "reference-other")
	absent, _, err = fixture.worker.recordingSpeakerReferences(test.Context(), otherUserId, capabilities, fixture.files)
	if err != nil || len(absent) != 0 {
		test.Fatal("other account selected voice evidence", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, blobIds, confirmedNames); err != nil {
			test.Fatal(err)
		}
		references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
		if err != nil || len(references) != 6 {
			test.Fatal("clean group did not curate several references", err, len(references))
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		for _, reference := range references {
			if reference.RecordingBlobID == blobIds[0] {
				if err := transaction.RevokeRecordingVoiceReference(fixture.userId, source.ID, reference.ID, source.SourceGeneration); err != nil {
					test.Fatal(err)
				}
				break
			}
		}
	})
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.CheckRecordingVoiceReferenceNames(fixture.userId, blobIds, confirmedNames)
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("revocation during sample reads admitted stale conditioning", err)
	}
}

func TestRecordingIdentityRenameKeepsCleanSamplesAndExplicitRefusals(test *testing.T) {
	fixture := newRecordingCurationFixture(test, 1)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: fixture.attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Fixture Participant", ClientCorrectionID: "rename-confirmation", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	var originalReference *models.RecordingVoiceReference
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
		if err != nil || len(references) != 1 {
			test.Fatal(err, len(references))
		}
		originalReference = references[0]
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RenameRecordingSpeaker(fixture.userId, source.ID, originalReference.SpeakerIdentityID, "Renamed Fixture Participant", source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal("rename recovery rejected retained clip generation", err)
	}
	capabilities := llm.TranscriptionCapabilities{HasSpeakerReferences: true, MaximumSpeakerReferenceCount: 4}
	selected, blobIds, err := fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, capabilities, fixture.files)
	if err != nil || len(selected) != 1 || selected[0].SpeakerName != "Renamed Fixture Participant" || blobIds[0] != originalReference.RecordingBlobID {
		test.Fatal("renamed identity lost its clean sample", err, len(selected))
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.CheckRecordingVoiceReferenceNames(fixture.userId, blobIds, []string{"Renamed Fixture Participant"}); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RevokeRecordingVoiceReference(fixture.userId, source.ID, originalReference.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RenameRecordingSpeaker(fixture.userId, source.ID, originalReference.SpeakerIdentityID, "Another Fixture Name", source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	selected, _, err = fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, capabilities, fixture.files)
	if err != nil || len(selected) != 0 {
		test.Fatal("renaming revived an explicitly rejected sample", err)
	}
}
