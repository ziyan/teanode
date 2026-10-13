package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestRecordingMetadataDateAndTitleAreFreeOwnedChanges(test *testing.T) {
	fixture := newTranscriptFixture(test)
	meetingTime := time.Date(2031, 3, 4, 5, 6, 0, 0, time.FixedZone("fixture-offset", 3600))
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
		if err != nil || source.RecordedAt != nil {
			test.Fatal("upload time invented a meeting date", err)
		}
		updated, err := transaction.UpdateRecordingMetadata(fixture.userId, source.ID, source.SourceGeneration, new("Fixture meeting"), &meetingTime)
		if err != nil || updated.RecordedAt == nil || !updated.RecordedAt.Equal(meetingTime) || updated.RecordedAt.Location() != time.UTC {
			test.Fatal("actual occurrence time lost", err)
		}
		source, err = transaction.GetRecording(fixture.userId, source.ID)
		if err != nil || source.RecordingTitle != "Fixture meeting" || !source.RecordedAt.Equal(meetingTime) {
			test.Fatal("metadata not durable", err)
		}
		cleared, err := transaction.UpdateRecordingMetadata(fixture.userId, source.ID, source.SourceGeneration, &source.RecordingTitle, nil)
		if err != nil || cleared.RecordedAt != nil {
			test.Fatal("unknown occurrence time not retained", err)
		}
		runs, err := transaction.ListRecordingTranscriptRuns(fixture.userId, source.ID)
		if err != nil || len(runs) != 1 {
			test.Fatal("metadata triggered another paid version", err)
		}
		media, err := transaction.GetRecordingMedia(fixture.userId, fixture.media.ID)
		if err != nil || media.OriginalSHA256 != fixture.media.OriginalSHA256 {
			test.Fatal("metadata changed original bytes", err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.UpdateRecordingMetadata(fixture.userId, fixture.media.RecordingID, 1, new("Stale edit"), nil)
		return err
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("stale metadata edit overwrote generation", err)
	}
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "metadata-other")
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.UpdateRecordingMetadata(otherOwnerId, fixture.media.RecordingID, 3, new("Foreign edit"), nil)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign metadata edit admitted", err)
	}
}
