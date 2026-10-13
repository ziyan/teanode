package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingTranscriptPassageOffsetUsesOwnedOriginalVersion(test *testing.T) {
	fixture := newTranscriptFixture(test)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments := make([]*models.RecordingTranscriptSegment, 0, 102)
		for segmentIndex := 0; segmentIndex < 101; segmentIndex++ {
			startMS := int64(2000 + segmentIndex*9)
			segments = append(segments, &models.RecordingTranscriptSegment{LocalSpeakerID: "fixture-speaker", SegmentStartMS: startMS, SegmentEndMS: startMS + 8, TranscriptText: "A synthetic timed passage."})
		}
		segments = append(segments, &models.RecordingTranscriptSegment{LocalSpeakerID: "overlap-speaker", SegmentStartMS: 2900, SegmentEndMS: 2908, TranscriptText: "An overlapping synthetic passage."})
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
		for _, passageBounds := range []struct {
			startMS, endMS int64
			expectedOffset int
		}{{2000, 2008, 0}, {2450, 2458, 50}, {2900, 2908, 100}, {2898, 2910, 99}} {
			segmentOffset, err := transaction.ReadRecordingTranscriptPassageOffset(fixture.userId, fixture.transcriptRun.ID, passageBounds.startMS, passageBounds.endMS)
			if err != nil || segmentOffset != passageBounds.expectedOffset {
				test.Fatal("citation located wrong original passage", err, segmentOffset, passageBounds)
			}
			page, _, err := transaction.ReadRecordingTranscriptPage(fixture.userId, fixture.transcriptRun.ID, segmentOffset, 1)
			if err != nil || len(page) != 1 || page[0].SegmentStartMS >= passageBounds.endMS || page[0].SegmentEndMS <= passageBounds.startMS {
				test.Fatal("offset disagreed with paged transcript order", err)
			}
		}
	})
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "passage-other-owner")
	for _, invalidRequest := range []struct {
		ownerId, runId string
		startMS, endMS int64
		expectedError  error
	}{{fixture.userId, fixture.transcriptRun.ID, -1, 3000, db.ErrInvalidArguments}, {fixture.userId, fixture.transcriptRun.ID, 2000, 2000, db.ErrInvalidArguments}, {fixture.userId, fixture.transcriptRun.ID, 3000, 2000, db.ErrInvalidArguments}, {fixture.userId, fixture.transcriptRun.ID, 5000, 6000, db.ErrNotFound}, {otherOwnerId, fixture.transcriptRun.ID, 2000, 3000, db.ErrNotFound}, {fixture.userId, "missing-fixture-version", 2000, 3000, db.ErrNotFound}} {
		err := fixture.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.ReadRecordingTranscriptPassageOffset(invalidRequest.ownerId, invalidRequest.runId, invalidRequest.startMS, invalidRequest.endMS)
			return err
		})
		if !errors.Is(err, invalidRequest.expectedError) {
			test.Fatal("invalid citation request was not refused", err, invalidRequest)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.ReadRecordingTranscriptPassageOffset(fixture.userId, fixture.transcriptRun.ID, 2000, 3000)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) && !errors.Is(err, db.ErrRecordingDeleted) {
		test.Fatal("deleted original remained readable", err)
	}
}
