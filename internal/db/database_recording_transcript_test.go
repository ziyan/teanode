package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

type transcriptFixture struct {
	database      db.Database
	userId        string
	agentId       string
	media         *models.RecordingMedia
	transcriptRun *models.RecordingTranscriptRun
	claim         db.RecordingWorkerClaim
}

func newTranscriptFixture(test *testing.T) *transcriptFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	fixture := &transcriptFixture{database: database, userId: dbtest.CreateUser(test, database, "transcript-owner")}
	originalBytes := []byte("fixture-audio")
	digest := sha256.Sum256(originalBytes)
	digestText := hex.EncodeToString(digest[:])
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		agent, err := transaction.CreateAgent(&models.Agent{UserID: fixture.userId, Name: "Recording assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		fixture.agentId = agent.ID
		source, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "source-fixture", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		fixture.media, err = transaction.CreateRecordingMedia(fixture.userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "media-fixture", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(originalBytes)), OriginalSHA256: digestText, TimelineStartMS: 2000}, 1024, 4096)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutRecordingMediaPart(fixture.userId, &models.RecordingMediaPart{RecordingMediaID: fixture.media.ID, PartIndex: 0, PartSizeBytes: int64(len(originalBytes)), PartSHA256: digestText}); err != nil {
			test.Fatal(err)
		}
		if _, _, err := transaction.CompleteRecordingMedia(fixture.userId, fixture.media.ID, digestText); err != nil {
			test.Fatal(err)
		}
		fixture.transcriptRun, err = transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, agent.ID, "initial", "fixture:diarize", false)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, Kind: models.AgentJobRecording, SubjectID: fixture.transcriptRun.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("fixture-instance", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal("recording job was not claimed", err)
		}
		fixture.claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
	})
	return fixture
}
func (self *transcriptFixture) createChunk(test *testing.T) (*models.RecordingAudioChunk, *models.RecordingBlob) {
	test.Helper()
	digest := sha256.Sum256([]byte("derived-wave-bytes"))
	blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(self.transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: hex.EncodeToString(digest[:])}
	var chunk *models.RecordingAudioChunk
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		var err error
		chunk, err = transaction.CreateRecordingAudioChunk(self.userId, self.transcriptRun.ID, self.claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000}, blob, 4096)
		if err != nil {
			test.Fatal(err)
		}
	})
	return chunk, blob
}
func (self *transcriptFixture) beginAttempt(test *testing.T, chunk *models.RecordingAudioChunk, blob *models.RecordingBlob) *models.RecordingTranscriptionAttempt {
	test.Helper()
	var attempt *models.RecordingTranscriptionAttempt
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		if err := transaction.VerifyRecordingBlob(self.userId, blob.ID, blob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(self.userId, self.transcriptRun.ID, self.claim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
		var err error
		attempt, err = transaction.CreateRecordingTranscriptionAttempt(self.userId, self.transcriptRun.ID, chunk.ID, self.claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if attempt.ActualCostAmount != nil || attempt.EstimatedCostAmount != nil || attempt.HasProviderUsage {
			test.Fatal("an unbilled attempt was shown as a known zero cost")
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(self.userId, attempt.ID, self.claim); err != nil {
			test.Fatal(err)
		}
	})
	return attempt
}

func TestRecordingVersionsClaimsUsageAndImmutableSegments(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	finished := &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete, InputAudioTokens: 20, PromptTokens: 20, CompletionTokens: 5, HasProviderUsage: true, ProviderUsageJSON: json.RawMessage("{\"input_tokens\":20,\"output_tokens\":5}"), ProviderResponseJSON: json.RawMessage("{\"segments\":[]}")}
	segment := &models.RecordingTranscriptSegment{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "A proposal was discussed."}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		retry, err := transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "initial", "fixture:diarize", false)
		if err != nil || retry.ID != fixture.transcriptRun.ID || retry.VersionNumber != 1 {
			test.Fatal("initial admission duplicated a paid version", err)
		}
		isFirstPublication, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, finished, []*models.RecordingTranscriptSegment{segment})
		if err != nil || !isFirstPublication {
			test.Fatal("provider result was not published", err)
		}
		isFirstPublication, err = transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, finished, []*models.RecordingTranscriptSegment{segment})
		if err != nil || isFirstPublication {
			test.Fatal("provider result was published twice", err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil || len(segments) != 1 || segments[0].RecordingTranscriptionAttemptID != attempt.ID {
			test.Fatal("request-local speaker provenance was lost", err)
		}
		attempts, err := transaction.ListRecordingTranscriptionAttempts(fixture.userId, fixture.transcriptRun.ID)
		if err != nil || len(attempts) != 1 || len(attempts[0].ProviderUsageJSON) != 0 || len(attempts[0].ProviderResponseJSON) != 0 {
			test.Fatal("bulk read loaded raw provider payloads", err)
		}
		evidence, err := transaction.GetRecordingTranscriptionAttemptEvidence(fixture.userId, attempt.ID)
		if err != nil || !json.Valid(evidence.ProviderUsageJSON) || !json.Valid(evidence.ProviderResponseJSON) {
			test.Fatal("raw provider evidence was not retained", err)
		}
		if err != nil || len(attempts) != 1 || len(attempts[0].ReferenceBlobIDs) != 0 || !attempts[0].HasProviderUsage {
			test.Fatal("provider usage was not preserved", err)
		}
		reprocessed, err := transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "explicit-rerun", "fixture:diarize", true)
		if err != nil || reprocessed.VersionNumber != 2 {
			test.Fatal("explicit reprocessing did not create a version", err)
		}
		retry, err = transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "explicit-rerun", "fixture:diarize", true)
		if err != nil || retry.ID != reprocessed.ID {
			test.Fatal("lost reprocessing response duplicated the version", err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "transcript-other")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.GetRecordingTranscriptRun(otherOwnerId, fixture.transcriptRun.ID)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another account saw a transcript", err)
	}
	expiredClaim := fixture.claim
	expiredClaim.ClaimID = "expired-claim"
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, expiredClaim, models.RecordingProcessingFailed, "", 1000)
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("expired worker published a result", err)
	}
}

func TestRecordingUnknownRemoteOutcomeIsNotAutomaticallyResubmitted(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim)
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("a submitted request was automatically repeated", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptUncertain, AttemptErrorMessage: "response lost"}, nil); err != nil {
			test.Fatal(err)
		}
		retry, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, fixture.transcriptRun.ID, chunk.ID, fixture.claim, "USD", nil)
		if err != nil || retry.ID != attempt.ID || retry.RecordingAttemptState != models.RecordingAttemptUncertain {
			test.Fatal("unknown outcome became another paid request", err)
		}
	})
}

func TestRecordingPreparedAttemptCanRefreshEmptyReferences(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(fixture.userId, blob.ID, blob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		prepared, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, fixture.transcriptRun.ID, chunk.ID, fixture.claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		refreshed, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, fixture.transcriptRun.ID, chunk.ID, fixture.claim, "USD", nil)
		if err != nil || refreshed.ID != prepared.ID || refreshed.RecordingAttemptState != models.RecordingAttemptPrepared || len(refreshed.ReferenceBlobIDs) != 0 {
			test.Fatal("prepared empty reference refresh changed attempt identity", err)
		}
	})
}

func TestRecordingPendingDerivedBytesKeepDeletionReferences(test *testing.T) {
	fixture := newTranscriptFixture(test)
	_, blob := fixture.createChunk(test)
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			return err
		}
		if err := transaction.AdvanceRecordingMediaCleanup(fixture.media.RecordingID, fixture.media.ID, 0); err != nil {
			return err
		}
		return transaction.FinishRecordingCleanup(fixture.media.RecordingID)
	})
	if err == nil {
		test.Fatal("cleanup forgot pending derived bytes")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
		blobs, err := transaction.ListRecordingBlobsForCleanup(fixture.media.RecordingID, 10)
		if err != nil || len(blobs) != 1 || blobs[0].ID != blob.ID || blobs[0].IsStoredVerified {
			test.Fatal("pending derived write lost its cleanup reference", err)
		}
	})
}

func TestRecordingParentsAreResolvedFromOwnedRun(test *testing.T) {
	fixture := newTranscriptFixture(test)
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "other-recording-owner")
	var otherAgent *models.Agent
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		otherAgent, err = transaction.CreateAgent(&models.Agent{UserID: otherOwnerId, Name: "Other assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, otherAgent.ID, "wrong-agent", "fixture:diarize", true)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign agent admitted to owned media", err)
	}
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingTranscriptRun(otherOwnerId, fixture.media.ID, otherAgent.ID, "foreign-media", "fixture:diarize", false)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign media admitted", err)
	}
	digest := sha256.Sum256([]byte("derived"))
	var chunk *models.RecordingAudioChunk
	var blob *models.RecordingBlob
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		chunk, err = transaction.CreateRecordingAudioChunk(fixture.userId, fixture.transcriptRun.ID, fixture.claim,
			&models.RecordingAudioChunk{RecordingTranscriptRunID: "foreign-run", RecordingBlobID: "foreign-blob", ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000},
			&models.RecordingBlob{ID: db.RecordingAudioFileID(fixture.transcriptRun.ID, 0), RecordingID: "foreign-source", RecordingMediaID: "foreign-media", BlobSizeBytes: 64, BlobSHA256: hex.EncodeToString(digest[:])}, 4096)
		if err != nil {
			test.Fatal(err)
		}
		blob, err = transaction.GetRecordingBlob(fixture.userId, chunk.RecordingBlobID)
		if err != nil {
			test.Fatal(err)
		}
		if blob.RecordingID != fixture.media.RecordingID || blob.RecordingMediaID != fixture.media.ID || chunk.RecordingTranscriptRunID != fixture.transcriptRun.ID {
			test.Fatal("caller supplied parent identifiers were trusted")
		}
	})
	fixture.beginAttempt(test, chunk, blob)
	var otherRun *models.RecordingTranscriptRun
	var otherClaim db.RecordingWorkerClaim
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		otherRun, err = transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "second-version", "fixture:diarize", true)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agentId, Kind: models.AgentJobRecording, SubjectID: otherRun.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("second-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal("second claim failed", err)
		}
		otherClaim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, otherRun.ID, otherClaim, models.RecordingProcessingTranscribing, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, otherRun.ID, chunk.ID, otherClaim, "USD", nil)
		return err
	})
	if err == nil {
		test.Fatal("another version's chunk admitted to attempt")
	}
}

type recordingCleanupFailure struct {
	storage.StreamFiles
	shouldFailDelete bool
}

func (self *recordingCleanupFailure) DeleteFileEverywhere(ctx context.Context, fileId string) error {
	if err := self.StreamFiles.DeleteFileEverywhere(ctx, fileId); err != nil {
		return err
	}
	if self.shouldFailDelete {
		return errors.New("fixture mirror unavailable")
	}
	return nil
}
func TestRecordingAccountDeletionRetriesOriginalAndDerivedCleanup(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	fixture.beginAttempt(test, chunk, blob)
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = files.Close() }()
	originalId := db.RecordingPartFileID(fixture.media.ID, 0)
	for _, fileId := range []string{originalId, blob.ID} {
		if err := files.PutFile(test.Context(), fileId, []byte("fixture bytes")); err != nil {
			test.Fatal(err)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteUser(fixture.userId); err != nil {
			test.Fatal(err)
		}
	})
	failingFiles := &recordingCleanupFailure{StreamFiles: files.(storage.StreamFiles), shouldFailDelete: true}
	if err := recording.New(fixture.database, failingFiles).Cleanup(test.Context(), 10); err == nil {
		test.Fatal("mirror deletion failure was forgotten")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		blobs, err := transaction.ListRecordingBlobsForCleanup(fixture.media.RecordingID, 10)
		if err != nil || len(blobs) != 1 {
			test.Fatal("failed cleanup lost derived ledger", err)
		}
	})
	// A fresh coordinator needs no active agent or account to resume cleanup.
	if err := recording.New(fixture.database, files.(storage.StreamFiles)).Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	for _, fileId := range []string{originalId, blob.ID} {
		if _, err := files.GetFile(test.Context(), fileId); !errors.Is(err, storage.ErrNotFound) {
			test.Fatal("account deletion retained speech", err)
		}
	}
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM recording_transcript_run") != "0" {
		test.Fatal("account deletion retained derived versions")
	}
}

func TestRecordingCancellationRetainsUsageButRejectsFurtherSegments(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		canceledRun, err := transaction.CancelRecordingTranscriptRun(fixture.userId, fixture.transcriptRun.ID)
		if err != nil || canceledRun.RecordingProcessingState != models.RecordingProcessingCanceled {
			test.Fatal("cancellation not durable", err)
		}
		finished := &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete, PromptTokens: 9, HasProviderUsage: true}
		segments := []*models.RecordingTranscriptSegment{{LocalSpeakerID: "local", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Evidence after cancellation."}}
		isFirstPublication, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, finished, segments)
		if err != nil || !isFirstPublication {
			test.Fatal("in-flight usage was lost", err)
		}
		storedSegments, segmentCount, err := transaction.ReadRecordingTranscriptPage(fixture.userId, fixture.transcriptRun.ID, 0, 20)
		if err != nil || len(storedSegments) != 0 || segmentCount != 0 {
			test.Fatal("canceled transcript published new segments", err)
		}
		attempts, err := transaction.ListRecordingTranscriptionAttempts(fixture.userId, fixture.transcriptRun.ID)
		if err != nil || len(attempts) != 1 || attempts[0].PromptTokens != 9 || !attempts[0].HasProviderUsage {
			test.Fatal("reported usage missing", err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim)
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("canceled request resubmitted", err)
	}
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "cancellation-other")
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CancelRecordingTranscriptRun(otherOwnerId, fixture.transcriptRun.ID)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign cancellation admitted", err)
	}
}
