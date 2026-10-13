package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

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

// Only a piece of a capture can start reading earlier than its own audio, and
// only a little, and only the way it said it would when a job starts over.
func TestAHandoffIsOnlyForAPieceOfACaptureAndIsNotChangedLater(test *testing.T) {
	imported := newTranscriptFixture(test)
	err := imported.database.Transaction(func(transaction db.Transaction) error {
		return transaction.SetRecordingTranscriptRunHandoff(imported.userId, imported.transcriptRun.ID, imported.claim, 1000)
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("an import was handed off", err)
	}
	captured := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	for name, handoffStartMS := range map[string]int64{"at the piece's own start": 2000, "after the piece's start": 2500, "before the start of the timeline": -1} {
		err := captured.database.Transaction(func(transaction db.Transaction) error {
			return transaction.SetRecordingTranscriptRunHandoff(captured.userId, captured.transcriptRun.ID, captured.claim, handoffStartMS)
		})
		if !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("a handoff "+name+" was accepted", err)
		}
	}
	dbtest.RunTransactionOn(test, captured.database, func(transaction db.Transaction) {
		for repeat := 0; repeat < 2; repeat++ {
			if err := transaction.SetRecordingTranscriptRunHandoff(captured.userId, captured.transcriptRun.ID, captured.claim, 500); err != nil {
				test.Fatal("the same handoff was refused on a repeat", err)
			}
		}
		if err := transaction.SetRecordingTranscriptRunHandoff(captured.userId, captured.transcriptRun.ID, captured.claim, 900); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatal("a different handoff replaced the first", err)
		}
		run, err := transaction.GetRecordingTranscriptRun(captured.userId, captured.transcriptRun.ID)
		if err != nil || run.HandoffStartMS == nil || *run.HandoffStartMS != 500 {
			test.Fatal("the handoff was not kept", err)
		}
	})
	// The first chunk of that run then starts where the handoff does.
	digest := sha256.Sum256([]byte("derived-wave-bytes"))
	blob := &models.RecordingBlob{ID: db.RecordingAudioFileID(captured.transcriptRun.ID, 0), BlobSizeBytes: 128, BlobSHA256: hex.EncodeToString(digest[:])}
	err = captured.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingAudioChunk(captured.userId, captured.transcriptRun.ID, captured.claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 2000, AudioDurationMS: 1000}, blob, 4096)
		return err
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("a first chunk ignored the handoff", err)
	}
	dbtest.RunTransactionOn(test, captured.database, func(transaction db.Transaction) {
		if _, err := transaction.CreateRecordingAudioChunk(captured.userId, captured.transcriptRun.ID, captured.claim, &models.RecordingAudioChunk{ChunkIndex: 0, TimelineStartMS: 500, AudioDurationMS: 1500}, blob, 4096); err != nil {
			test.Fatal(err)
		}
	})
}
