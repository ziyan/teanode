package db_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func saveLiveUtterance(test *testing.T, fixture *transcriptFixture, utteranceId string, startMS, endMS int64, text string) error {
	test.Helper()
	return fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.SaveRecordingLiveUtterance(fixture.userId, fixture.media.RecordingID, &models.RecordingLiveUtterance{UtteranceID: utteranceId, UtteranceStartMS: startMS, UtteranceEndMS: endMS, TranscriptText: text})
	})
}

func listLiveUtteranceTexts(test *testing.T, fixture *transcriptFixture) string {
	test.Helper()
	var texts []string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		utterances, err := transaction.ListRecordingLiveUtterances(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		for _, utterance := range utterances {
			texts = append(texts, utterance.TranscriptText)
		}
	})
	return strings.Join(texts, "|")
}

func countLiveUtterances(test *testing.T, fixture *transcriptFixture) string {
	test.Helper()
	return dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM recording_live_utterance")
}

func deleteSettledLiveUtterances(test *testing.T, fixture *transcriptFixture) {
	test.Helper()
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.DeleteSettledRecordingLiveUtterances(100); err != nil {
			test.Fatal(err)
		}
	})
}

// Live words are kept only while the capture is going, are shown only where
// no completed run covers them, and are deleted once the saved audio has
// been transcribed all the way to the end of the capture.
func TestLiveUtterancesAreShownUntilACompletedRunCoversThem(test *testing.T) {
	fixture := newTranscriptFixture(test)
	recordingId := fixture.media.RecordingID
	// The fixture's media is an original from 2000 ms for 10 seconds.
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording_media SET media_duration_ms = 10000 WHERE id = '%s'", fixture.media.ID))

	if err := saveLiveUtterance(test, fixture, "item-a", 0, 1000, "Imported recordings have no live words."); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("a recording that is not being captured keeps no live words: %v", err)
	}
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = 'active' WHERE id = '%s'", recordingId))
	for _, invalid := range []struct {
		utteranceId string
		start, end  int64
		text        string
	}{{"", 0, 1, "no id"}, {"item", -1, 1, "negative"}, {"item", 5, 4, "backwards"}, {"item", 0, 1, "  "}, {"item", 0, 1, strings.Repeat("a", db.RecordingLiveUtteranceTextBytesMaximum+1)}, {"item", 0, 1, "nul\x00"}} {
		if err := saveLiveUtterance(test, fixture, invalid.utteranceId, invalid.start, invalid.end, invalid.text); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("%q should be refused: %v", invalid.text, err)
		}
	}
	for _, utterance := range []struct {
		utteranceId string
		start, end  int64
		text        string
	}{{"item-c", 20000, 21000, "Last."}, {"item-a", 0, 1000, "First."}, {"item-b", 2500, 3500, "Second, heard."}, {"item-b", 2500, 3600, "Second, heard again."}} {
		if err := saveLiveUtterance(test, fixture, utterance.utteranceId, utterance.start, utterance.end, utterance.text); err != nil {
			test.Fatal(err)
		}
	}
	if texts := listLiveUtteranceTexts(test, fixture); texts != "First.|Second, heard again.|Last." {
		test.Fatalf("in timeline order, a repeated utterance replaced: %s", texts)
	}
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "live-other")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.ListRecordingLiveUtterances(otherOwnerId, recordingId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("another account reads live words: %v", err)
	}

	// The run of the original from 2000 to 12000 completes: what it covers
	// goes, what it does not stays.
	completeTimelineFixture(test, fixture, "Saved.")
	if texts := listLiveUtteranceTexts(test, fixture); texts != "First.|Last." {
		test.Fatalf("covered words are hidden: %s", texts)
	}

	// Not stopped: nothing is deleted. Stopped but the originals stop short
	// of the capture: nothing is deleted, the last piece has not arrived.
	deleteSettledLiveUtterances(test, fixture)
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = 'active', timeline_duration_ms = 30000 WHERE id = '%s'", recordingId))
	deleteSettledLiveUtterances(test, fixture)
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = 'stopped' WHERE id = '%s'", recordingId))
	deleteSettledLiveUtterances(test, fixture)
	if count := countLiveUtterances(test, fixture); count != "3" {
		test.Fatalf("live words were deleted too early: %s", count)
	}
	// Stopped long ago, the audio is evidently not being transcribed.
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET updated_at = now() - interval '25 hours' WHERE id = '%s'", recordingId))
	deleteSettledLiveUtterances(test, fixture)
	if count := countLiveUtterances(test, fixture); count != "0" {
		test.Fatalf("stale live words stayed: %s", count)
	}
}

func TestLiveUtterancesGoOnceTheSavedAudioReachesTheEndOfTheCapture(test *testing.T) {
	fixture := newTranscriptFixture(test)
	recordingId := fixture.media.RecordingID
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording_media SET media_duration_ms = 10000 WHERE id = '%s'", fixture.media.ID))
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = 'paused' WHERE id = '%s'", recordingId))
	if err := saveLiveUtterance(test, fixture, "item-a", 11000, 12000, "Near the end."); err != nil {
		test.Fatal(err)
	}
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = 'stopped', timeline_duration_ms = 12000 WHERE id = '%s'", recordingId))
	// The run has not completed.
	deleteSettledLiveUtterances(test, fixture)
	if count := countLiveUtterances(test, fixture); count != "1" {
		test.Fatalf("deleted before its run completed: %s", count)
	}
	completeTimelineFixture(test, fixture, "Saved.")
	deleteSettledLiveUtterances(test, fixture)
	if count := countLiveUtterances(test, fixture); count != "0" {
		test.Fatalf("kept after the capture was transcribed: %s", count)
	}
}

func TestLiveUtterancesGoWithTheRecording(test *testing.T) {
	fixture := newTranscriptFixture(test)
	dbtest.Exec(test, fixture.database, fmt.Sprintf("UPDATE recording SET recording_capture_state = 'active' WHERE id = '%s'", fixture.media.RecordingID))
	if err := saveLiveUtterance(test, fixture, "item-a", 0, 1000, "Words."); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	if count := countLiveUtterances(test, fixture); count != "0" {
		test.Fatalf("live words outlived their recording: %s", count)
	}
}
