package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A phone reports a capture's progress every few seconds, and a person names a
// speaker from a generation they read earlier. If each report advanced the
// generation, the naming would be refused as stale.
func TestNamingASpeakerDuringCaptureSurvivesProgressReports(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Spoken during the capture."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	var readGeneration int64
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		readGeneration = source.SourceGeneration
		for _, durationMS := range []int64{4000, 8000, 12000} {
			updated, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, readGeneration, models.RecordingCaptureActive, durationMS, nil)
			if err != nil {
				test.Fatal("a progress report was refused", err)
			}
			if updated.SourceGeneration != readGeneration {
				test.Fatal("a progress report advanced the generation", updated.SourceGeneration, readGeneration)
			}
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Participant One", ClientCorrectionID: "live-confirmation", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: readGeneration}); err != nil {
			test.Fatal("naming with the generation read before the progress reports failed", err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		stopped, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, source.SourceGeneration, models.RecordingCaptureStopped, 12000, nil)
		if err != nil {
			test.Fatal(err)
		}
		if stopped.SourceGeneration != source.SourceGeneration+1 {
			test.Fatal("stopping did not advance the generation", stopped.SourceGeneration, source.SourceGeneration)
		}
		again, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, stopped.SourceGeneration, models.RecordingCaptureStopped, 12000, nil)
		if err != nil || again.SourceGeneration != stopped.SourceGeneration {
			test.Fatal("repeating the stop advanced the generation", err)
		}
	})
}

// A capture still being recorded has a partial transcript, so the sweep that
// names recordings must wait for the stop.
func TestSummarySweepSkipsACaptureStillBeingRecorded(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Partial."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		awaiting, err := transaction.ListRecordingsAwaitingSummary("", 10)
		if err != nil || len(awaiting) != 0 {
			test.Fatal("a capture in progress was offered for naming", err, len(awaiting))
		}
		hasUnfinished, err := transaction.HasUnfinishedRecordingTranscription(fixture.userId, fixture.media.RecordingID)
		if err != nil || hasUnfinished {
			test.Fatal("a completed piece was reported as unfinished", err)
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, source.SourceGeneration, models.RecordingCaptureStopped, 4000, nil); err != nil {
			test.Fatal(err)
		}
		awaiting, err = transaction.ListRecordingsAwaitingSummary("", 10)
		if err != nil || len(awaiting) != 1 {
			test.Fatal("a stopped capture was not offered for naming", err, len(awaiting))
		}
	})
}

// finishFixtureAttempt completes an attempt with the given passages.
func (self *transcriptFixture) finishFixtureAttempt(test *testing.T, attempt *models.RecordingTranscriptionAttempt, segments ...*models.RecordingTranscriptSegment) {
	test.Helper()
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(self.userId, attempt.ID, self.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
	})
}

// Only a piece of a capture can be stitched to the piece before it, only a
// little before its own start, and the second look replaces what the piece
// heard on its own over the same stretch.
func TestAHandoffIsOnlyForAPieceOfACaptureAndReplacesWhatTheSecondLookCovers(test *testing.T) {
	imported := newTranscriptFixture(test)
	importedChunk, importedBlob := imported.createChunk(test)
	importedAttempt := imported.beginAttempt(test, importedChunk, importedBlob)
	imported.finishFixtureAttempt(test, importedAttempt, &models.RecordingTranscriptSegment{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Imported."})
	err := imported.database.Transaction(func(transaction db.Transaction) error {
		return transaction.PublishRecordingTranscriptRun(imported.userId, imported.transcriptRun.ID, imported.claim, &db.RecordingHandoffPublication{HandoffStartMS: 1000, HandoffEndMS: 2500, HandoffAttemptID: importedAttempt.ID})
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("an import was handed off", err)
	}

	captured := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := captured.createChunk(test)
	ownAttempt := captured.beginAttempt(test, chunk, blob)
	captured.finishFixtureAttempt(test, ownAttempt,
		&models.RecordingTranscriptSegment{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 2400, TranscriptText: "ship it on Friday."},
		&models.RecordingTranscriptSegment{LocalSpeakerID: "speaker-one", SegmentStartMS: 2600, SegmentEndMS: 3000, TranscriptText: "Agreed."})
	// The second look is made after the piece's own audio, so it cannot be
	// made in the middle of preparing, and it begins before the piece does.
	handoffBlob := &models.RecordingBlob{ID: db.RecordingAudioFileID(captured.transcriptRun.ID, 1), BlobSizeBytes: 128, BlobSHA256: blob.BlobSHA256}
	var handoffChunk *models.RecordingAudioChunk
	for name, startMS := range map[string]int64{"at the piece's own start": 2000, "after the piece's start": 2500, "before the start of the timeline": -1} {
		err := captured.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.CreateRecordingAudioChunk(captured.userId, captured.transcriptRun.ID, captured.claim, &models.RecordingAudioChunk{ChunkIndex: 1, TimelineStartMS: startMS, AudioDurationMS: 1000}, handoffBlob, 4096)
			return err
		})
		if !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("a second look "+name+" was accepted", err)
		}
	}
	dbtest.RunTransactionOn(test, captured.database, func(transaction db.Transaction) {
		var err error
		handoffChunk, err = transaction.CreateRecordingAudioChunk(captured.userId, captured.transcriptRun.ID, captured.claim, &models.RecordingAudioChunk{ChunkIndex: 1, TimelineStartMS: 1500, AudioDurationMS: 1000}, handoffBlob, 4096)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.VerifyRecordingBlob(captured.userId, handoffBlob.ID, handoffBlob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
	})
	var handoffAttempt *models.RecordingTranscriptionAttempt
	dbtest.RunTransactionOn(test, captured.database, func(transaction db.Transaction) {
		var err error
		handoffAttempt, err = transaction.CreateRecordingTranscriptionAttempt(captured.userId, captured.transcriptRun.ID, handoffChunk.ID, captured.claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(captured.userId, handoffAttempt.ID, captured.claim); err != nil {
			test.Fatal(err)
		}
	})
	captured.finishFixtureAttempt(test, handoffAttempt, &models.RecordingTranscriptSegment{LocalSpeakerID: "speaker-two", SegmentStartMS: 1500, SegmentEndMS: 2400, TranscriptText: "We should ship it on Friday."})
	for name, publication := range map[string]*db.RecordingHandoffPublication{
		"at the piece's own start":      {HandoffStartMS: 2000, HandoffEndMS: 2500, HandoffAttemptID: handoffAttempt.ID},
		"before the timeline":           {HandoffStartMS: -1, HandoffEndMS: 2500, HandoffAttemptID: handoffAttempt.ID},
		"ending before the piece":       {HandoffStartMS: 1500, HandoffEndMS: 2000, HandoffAttemptID: handoffAttempt.ID},
		"naming an attempt that is not": {HandoffStartMS: 1500, HandoffEndMS: 2500, HandoffAttemptID: "missing"},
	} {
		err := captured.database.Transaction(func(transaction db.Transaction) error {
			return transaction.PublishRecordingTranscriptRun(captured.userId, captured.transcriptRun.ID, captured.claim, publication)
		})
		if !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("a publication "+name+" was accepted", err)
		}
	}
	for repeat := 0; repeat < 2; repeat++ {
		dbtest.RunTransactionOn(test, captured.database, func(transaction db.Transaction) {
			if err := transaction.PublishRecordingTranscriptRun(captured.userId, captured.transcriptRun.ID, captured.claim, &db.RecordingHandoffPublication{HandoffStartMS: 1500, HandoffEndMS: 2500, HandoffAttemptID: handoffAttempt.ID}); err != nil {
				test.Fatal("the publication was refused on a repeat", repeat, err)
			}
		})
	}
	dbtest.RunTransactionOn(test, captured.database, func(transaction db.Transaction) {
		run, err := transaction.GetRecordingTranscriptRun(captured.userId, captured.transcriptRun.ID)
		if err != nil || run.HandoffStartMS == nil || *run.HandoffStartMS != 1500 || run.PublishedAt == nil {
			test.Fatal("the handoff was not kept", err, run)
		}
		segments, err := transaction.ListRecordingTranscriptSegments(captured.userId, captured.transcriptRun.ID)
		if err != nil || len(segments) != 2 || segments[0].TranscriptText != "We should ship it on Friday." || segments[1].TranscriptText != "Agreed." {
			test.Fatal("the piece's own passages over the second look were not replaced", err, segments)
		}
	})
}

// Pieces are listed in the order they were recorded with the latest version of
// each, and a piece is pending only while a job is going to work on it.
func TestRecordingPiecesArePendingOnlyWhileAJobWorksOnThem(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	var recordingId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		pieces, err := transaction.ListRecordingPieces(fixture.userId, fixture.media.RecordingID)
		if err != nil || len(pieces) != 1 || pieces[0].RecordingTranscriptRunID != fixture.transcriptRun.ID || !pieces[0].HasLiveJob || !pieces[0].IsPending() || pieces[0].PublishedAt != nil {
			test.Fatal("a piece with a running job was not pending", err, pieces)
		}
		recordingId = fixture.media.RecordingID
	})
	// The job gives up on it: nothing is going to publish it, so nothing waits.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishAgentJob(fixture.claim.JobID, fixture.claim.ClaimID, &db.AgentJobOutcome{JobStatus: models.AgentJobDead, FinishedAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
		pieces, err := transaction.ListRecordingPieces(fixture.userId, recordingId)
		if err != nil || len(pieces) != 1 || pieces[0].IsPending() {
			test.Fatal("a piece whose job died still held up the pieces after it", err, pieces)
		}
	})
}

// Publishing a piece makes the waiting jobs of the next pieces due, and only
// those within the window.
func TestWakingRecordingJobsMakesTheNextPiecesDue(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	later := time.Now().Add(time.Hour)
	var waitingJobIds []string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		// The first piece's job goes back to waiting; four more pieces follow.
		if _, err := transaction.FinishAgentJob(fixture.claim.JobID, fixture.claim.ClaimID, &db.AgentJobOutcome{JobStatus: models.AgentJobQueued, NotBefore: &later, FinishedAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
		waitingJobIds = append(waitingJobIds, fixture.claim.JobID)
		for pieceIndex := 1; pieceIndex <= 4; pieceIndex++ {
			digest := sha256.Sum256([]byte("fixture-audio"))
			digestText := hex.EncodeToString(digest[:])
			media, err := transaction.CreateRecordingMedia(fixture.userId, &models.RecordingMedia{RecordingID: fixture.media.RecordingID, ClientMediaID: "later-" + string(rune('a'+pieceIndex)), OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: 13, OriginalSHA256: digestText, TimelineStartMS: 2000 + int64(pieceIndex)*10000}, 1024, 4096)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.PutRecordingMediaPart(fixture.userId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: 13, PartSHA256: digestText}); err != nil {
				test.Fatal(err)
			}
			if _, _, err := transaction.CompleteRecordingMedia(fixture.userId, media.ID, digestText); err != nil {
				test.Fatal(err)
			}
			transcriptRun, err := transaction.CreateRecordingTranscriptRun(fixture.userId, media.ID, fixture.agentId, "initial", "fixture:diarize", false)
			if err != nil {
				test.Fatal(err)
			}
			job, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agentId, Kind: models.AgentJobRecording, SubjectID: transcriptRun.ID, NotBefore: &later})
			if err != nil {
				test.Fatal(err)
			}
			waitingJobIds = append(waitingJobIds, job.ID)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		wokenCount, err := transaction.WakeRecordingTranscriptionJobs(fixture.userId, fixture.media.RecordingID, 3)
		if err != nil || wokenCount != 3 {
			test.Fatal("the first three pieces' jobs were not made due", err, wokenCount)
		}
		for index, jobId := range waitingJobIds {
			job, err := transaction.GetAgentJob(jobId)
			if err != nil {
				test.Fatal(err)
			}
			isDue := job.NotBefore != nil && !job.NotBefore.After(time.Now())
			if isDue != (index < 3) {
				test.Fatal("the wrong jobs were made due", index, isDue)
			}
		}
	})
}
