package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
)

func TestAccurateWordsAreTheBaseTextOfAPassage(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "A palette arrived.", "Left alone.")
	// The second pass runs before the run completes.
	dbtest.Exec(test, fixture.database, `UPDATE recording_transcript_run SET recording_processing_state = 'transcribing'`)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SetRecordingAccurateWords(fixture.userId, fixture.transcriptRun.ID, fixture.claim, map[string]string{segments[0].ID: "A pallet arrived."}); err != nil {
			test.Fatal(err)
		}
		// Words already stored are not replaced by a repeat.
		if err := transaction.SetRecordingAccurateWords(fixture.userId, fixture.transcriptRun.ID, fixture.claim, map[string]string{segments[0].ID: "Other words."}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SetRecordingAccurateWords(fixture.userId, fixture.transcriptRun.ID, fixture.claim, map[string]string{segments[1].ID: " "}); err == nil {
			test.Fatal("blank words were stored")
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		read, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(fixture.userId, read); err != nil {
			test.Fatal(err)
		}
		if read[0].EffectiveTranscriptText != "A pallet arrived." || read[0].TranscriptText != "A palette arrived." || read[1].EffectiveTranscriptText != "Left alone." {
			test.Fatalf("the base text is %q and %q", read[0].EffectiveTranscriptText, read[1].EffectiveTranscriptText)
		}
		if read[0].SegmentStartMS != segments[0].SegmentStartMS || read[0].SegmentEndMS != segments[0].SegmentEndMS || read[0].LocalSpeakerID != segments[0].LocalSpeakerID {
			test.Fatal("storing the words moved the passage")
		}
	})
}

func TestAccurateWordsCannotBeStoredOnAnotherRunsSegment(test *testing.T) {
	fixture, segments := completeGlossaryFixture(test, "One.")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SetRecordingAccurateWords(fixture.userId, fixture.transcriptRun.ID+"-other", fixture.claim, map[string]string{segments[0].ID: "Words."}); err == nil {
			test.Fatal("words were stored through a run that does not exist")
		}
	})
}

func TestAccurateWordsMigrationAddsAndDropsTheColumn(test *testing.T) {
	fixture, _ := completeGlossaryFixture(test, "Tea node is up.")
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0178_recording_accurate_words" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("there is no migration 0178_recording_accurate_words")
	}
	const columnQuery = `SELECT COUNT(*)::text FROM information_schema.columns WHERE table_name = 'recording_transcript_segment' AND column_name = 'accurate_transcript_text'`
	dbtest.Exec(test, fixture.database, migration.ReverseSQL)
	if columns := dbtest.QueryString(test, fixture.database, columnQuery); columns != "0" {
		test.Fatalf("the reverse removes the column: %s left", columns)
	}
	dbtest.Exec(test, fixture.database, migration.SQL)
	if columns := dbtest.QueryString(test, fixture.database, columnQuery); columns != "1" {
		test.Fatalf("the migration adds the column: %s", columns)
	}
	if empty := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM recording_transcript_segment WHERE accurate_transcript_text = ''`); empty != "1" {
		test.Fatalf("existing passages start without accurate words: %s", empty)
	}
}
