package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
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
		if err != nil || len(attempts) != 1 || !json.Valid(attempts[0].ProviderUsageJSON) || len(attempts[0].ReferenceBlobIDs) != 0 || !attempts[0].HasProviderUsage {
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
