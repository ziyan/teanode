package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func completeTimelineFixture(test *testing.T, fixture *transcriptFixture, transcriptText string) {
	test.Helper()
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "local", SegmentStartMS: 2000, SegmentEndMS: 2400, TranscriptText: transcriptText}, {LocalSpeakerID: "local", SegmentStartMS: 2500, SegmentEndMS: 2900, TranscriptText: "Second passage."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
}

func TestRecordingTimelineUsesLatestCompletedVersionsAndBoundedOwnedPages(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeTimelineFixture(test, fixture, "First version.")
	finishRecordingFixtureJob(test, fixture)
	next := &transcriptFixture{database: fixture.database, userId: fixture.userId, agentId: fixture.agentId, media: fixture.media}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		next.transcriptRun, err = transaction.CreateRecordingTranscriptRun(fixture.userId, fixture.media.ID, fixture.agentId, "timeline-rerun", "fixture:diarize", true)
		if err != nil {
			test.Fatal(err)
		}
		segments, transcriptRuns, segmentCount, err := transaction.ReadRecordingTranscriptTimelinePage(fixture.userId, fixture.media.RecordingID, 0, 1)
		if err != nil || segmentCount != 2 || len(segments) != 1 || segments[0].TranscriptText != "First version." || len(transcriptRuns) != 1 || transcriptRuns[0].ID != fixture.transcriptRun.ID {
			test.Fatal("pending rerun hid completed timeline evidence", err)
		}
		if _, err := transaction.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agentId, Kind: models.AgentJobRecording, SubjectID: next.transcriptRun.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("timeline-worker", 1, time.Now())
		if err != nil || len(jobs) != 1 {
			test.Fatal(err)
		}
		next.claim = db.RecordingWorkerClaim{JobID: jobs[0].ID, ClaimID: jobs[0].ClaimID}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, next.transcriptRun.ID, next.claim, models.RecordingProcessingPreparing, "", 0); err != nil {
			test.Fatal(err)
		}
	})
	completeTimelineFixture(test, next, "Second version.")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments, transcriptRuns, segmentCount, err := transaction.ReadRecordingTranscriptTimelinePage(fixture.userId, fixture.media.RecordingID, 0, 1)
		if err != nil || segmentCount != 2 || len(segments) != 1 || segments[0].TranscriptText != "Second version." || len(transcriptRuns) != 1 || transcriptRuns[0].ID != next.transcriptRun.ID || transcriptRuns[0].RecordingMediaID != fixture.media.ID {
			test.Fatal("timeline mixed superseded versions or lost media provenance", err)
		}
		segments, _, segmentCount, err = transaction.ReadRecordingTranscriptTimelinePage(fixture.userId, fixture.media.RecordingID, 1, 1)
		if err != nil || segmentCount != 2 || len(segments) != 1 || segments[0].SegmentStartMS != 2500 {
			test.Fatal("timeline pagination repeated its first passage", err)
		}
		original, _, err := transaction.ReadRecordingTranscriptPage(fixture.userId, fixture.transcriptRun.ID, 0, 10)
		if err != nil || len(original) != 2 || original[0].TranscriptText != "First version." {
			test.Fatal("aggregate timeline replaced immutable original version", err)
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "timeline-other")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, _, _, err := transaction.ReadRecordingTranscriptTimelinePage(otherOwnerId, fixture.media.RecordingID, 0, 10)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign account read aggregate transcript", err)
	}
	for _, pageSize := range []int{0, 251} {
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			_, _, _, err := transaction.ReadRecordingTranscriptTimelinePage(fixture.userId, fixture.media.RecordingID, 0, pageSize)
			return err
		})
		if !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("unbounded timeline page accepted", err)
		}
	}
}
