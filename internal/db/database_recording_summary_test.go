package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// completeFixtureTranscript finishes the fixture's transcript with two speakers
// and returns the attempt that holds them.
func completeFixtureTranscript(test *testing.T, fixture *transcriptFixture) *models.RecordingTranscriptionAttempt {
	test.Helper()
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		segments := []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "speaker-one", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "We should ship on Friday."},
			{LocalSpeakerID: "speaker-two", SegmentStartMS: 2000, SegmentEndMS: 3000, TranscriptText: "Agreed, I will write the notes."},
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	return attempt
}

func TestRecordingTitleSourceFollowsWhoWroteTheTitle(test *testing.T) {
	fixture := newTranscriptFixture(test)
	recordingId := fixture.media.RecordingID
	read := func() *models.Recording {
		var source *models.Recording
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			var err error
			if source, err = transaction.GetRecording(fixture.userId, recordingId); err != nil {
				test.Fatal(err)
			}
		})
		return source
	}
	if source := read(); source.RecordingTitleSource != models.RecordingTitleSourceNone || source.RecordingTitle != "" {
		test.Fatalf("a recording created without a title starts with none: %+v", source)
	}
	store := func(title, summary string) {
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			if err := transaction.StoreGeneratedRecordingSummary(fixture.userId, recordingId, title, summary); err != nil {
				test.Fatal(err)
			}
		})
	}
	update := func(title *string, recordedAt *time.Time) {
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			source, err := transaction.GetRecording(fixture.userId, recordingId)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.UpdateRecordingMetadata(fixture.userId, recordingId, source.SourceGeneration, title, recordedAt); err != nil {
				test.Fatal(err)
			}
		})
	}
	store("Release planning", "They agreed to ship on Friday.")
	if source := read(); source.RecordingTitle != "Release planning" || source.RecordingTitleSource != models.RecordingTitleSourceGenerated || source.RecordingSummary != "They agreed to ship on Friday." {
		test.Fatalf("a generated title is stored as generated: %+v", source)
	}
	store("Another name", "A second summary.")
	if source := read(); source.RecordingTitle != "Another name" || source.RecordingSummary != "A second summary." {
		test.Fatalf("a generated title is replaced by a newer generation: %+v", source)
	}
	meetingTime := time.Date(2031, 3, 4, 5, 6, 0, 0, time.UTC)
	update(nil, &meetingTime)
	if source := read(); source.RecordingTitle != "Another name" || source.RecordingTitleSource != models.RecordingTitleSourceGenerated || source.RecordedAt == nil {
		test.Fatalf("changing only the date leaves the title and its source alone: %+v", source)
	}
	update(new("My own name"), &meetingTime)
	if source := read(); source.RecordingTitle != "My own name" || source.RecordingTitleSource != models.RecordingTitleSourceUser {
		test.Fatalf("a typed title is a user title: %+v", source)
	}
	store("Generated again", "A third summary.")
	if source := read(); source.RecordingTitle != "My own name" || source.RecordingSummary != "A third summary." {
		test.Fatalf("a user title survives generation while the summary is replaced: %+v", source)
	}
	update(new(""), &meetingTime)
	if source := read(); source.RecordingTitle != "" || source.RecordingTitleSource != models.RecordingTitleSourceNone {
		test.Fatalf("clearing the title hands the name back to generation: %+v", source)
	}
	store("Named at last", "A fourth summary.")
	if source := read(); source.RecordingTitle != "Named at last" || source.RecordingTitleSource != models.RecordingTitleSourceGenerated {
		test.Fatalf("a cleared title is filled by generation: %+v", source)
	}
}

func TestRecordingSummarySweepOffersEachUntitledTranscribedRecordingOnce(test *testing.T) {
	fixture := newTranscriptFixture(test)
	list := func() []*models.Recording {
		var recordings []*models.Recording
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			var err error
			if recordings, err = transaction.ListRecordingsAwaitingSummary("", 10); err != nil {
				test.Fatal(err)
			}
		})
		return recordings
	}
	if len(list()) != 0 {
		test.Fatal("a recording with no completed transcript is not offered")
	}
	completeFixtureTranscript(test, fixture)
	if recordings := list(); len(recordings) != 1 || recordings[0].ID != fixture.media.RecordingID {
		test.Fatalf("an untitled recording with a completed transcript is offered: %+v", recordings)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.MarkRecordingSummaryQueued(fixture.userId, fixture.media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	if len(list()) != 0 {
		test.Fatal("a recording is offered once, however the job ends")
	}
	// A title the agent wrote is offered again once its queued mark is cleared,
	// and a title the person typed never is.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.StoreGeneratedRecordingSummary(fixture.userId, fixture.media.RecordingID, "Generated title", "A summary."); err != nil {
			test.Fatal(err)
		}
	})
	if len(list()) != 0 {
		test.Fatal("a generated recording that was queued is not offered")
	}
	dbtest.Exec(test, fixture.database, `UPDATE recording SET recording_summary_queued_at = NULL`)
	if recordings := list(); len(recordings) != 1 {
		test.Fatalf("a generated recording with its queued mark cleared is offered again: %+v", recordings)
	}
	dbtest.Exec(test, fixture.database, `UPDATE recording SET recording_title_source = 'user'`)
	if len(list()) != 0 {
		test.Fatal("a recording with a typed title is never offered")
	}
}

func TestRecordingImportDurationFollowsTheLastVerifiedOriginal(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "duration-owner")
	originalBytes := []byte("fixture-audio")
	digest := sha256.Sum256(originalBytes)
	digestText := hex.EncodeToString(digest[:])
	complete := func(origin models.RecordingOrigin, clientRecordingId string, timelineStartMS, mediaDurationMS int64) string {
		var recordingId string
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			source, err := transaction.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: clientRecordingId, RecordingOrigin: origin})
			if err != nil {
				test.Fatal(err)
			}
			recordingId = source.ID
			media, err := transaction.CreateRecordingMedia(userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "media-" + clientRecordingId, OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(originalBytes)), OriginalSHA256: digestText, TimelineStartMS: timelineStartMS, MediaDurationMS: mediaDurationMS}, 1024, 4096)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.PutRecordingMediaPart(userId, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: int64(len(originalBytes)), PartSHA256: digestText}); err != nil {
				test.Fatal(err)
			}
			if _, _, err := transaction.CompleteRecordingMedia(userId, media.ID, digestText); err != nil {
				test.Fatal(err)
			}
		})
		return recordingId
	}
	durationOf := func(recordingId string) int64 {
		var timelineDurationMS int64
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			source, err := transaction.GetRecording(userId, recordingId)
			if err != nil {
				test.Fatal(err)
			}
			timelineDurationMS = source.TimelineDurationMS
		})
		return timelineDurationMS
	}
	if durationMS := durationOf(complete(models.RecordingOriginAudioImport, "imported", 2000, 60000)); durationMS != 62000 {
		test.Fatalf("an import is as long as the end of its original on the timeline: %d", durationMS)
	}
	if durationMS := durationOf(complete(models.RecordingOriginCapture, "captured", 0, 60000)); durationMS != 0 {
		test.Fatalf("a capture reports its own length: %d", durationMS)
	}
}

func TestRecordingImportDurationMigrationBackfillsOnlyEmptyLengths(test *testing.T) {
	fixture := newTranscriptFixture(test)
	dbtest.Exec(test, fixture.database, `UPDATE recording_media SET media_duration_ms = 30000`)
	dbtest.Exec(test, fixture.database, `UPDATE recording SET timeline_duration_ms = 0`)
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0171_recording_import_duration" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("there is no migration 0171_recording_import_duration")
	}
	dbtest.Exec(test, fixture.database, migration.SQL)
	if length := dbtest.QueryString(test, fixture.database, `SELECT timeline_duration_ms::text FROM recording`); length != "32000" {
		test.Fatalf("an empty length is filled from the verified original: %s", length)
	}
	dbtest.Exec(test, fixture.database, `UPDATE recording SET timeline_duration_ms = 5000`)
	dbtest.Exec(test, fixture.database, migration.SQL)
	if length := dbtest.QueryString(test, fixture.database, `SELECT timeline_duration_ms::text FROM recording`); length != "5000" {
		test.Fatalf("a length that was set is kept: %s", length)
	}
	dbtest.Exec(test, fixture.database, `UPDATE recording SET recording_title_source = 'generated', recording_summary_queued_at = now()`)
	dbtest.Exec(test, fixture.database, migration.SQL)
	if queued := dbtest.QueryString(test, fixture.database, `SELECT (recording_summary_queued_at IS NULL)::text FROM recording`); queued != "true" {
		test.Fatal("a generated recording is queued for a new summary")
	}
	dbtest.Exec(test, fixture.database, migration.ReverseSQL)
}

func TestRecordingAccountOwnerIsOnePerAccountAndMarksSegments(test *testing.T) {
	fixture := newTranscriptFixture(test)
	attempt := completeFixtureTranscript(test, fixture)
	confirm := func(clientCorrectionId, localSpeakerId, name string, isAccountOwner bool) {
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			source, err := transaction.GetRecording(fixture.userId, fixture.media.RecordingID)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: source.ID, RecordingTranscriptionAttemptID: attempt.ID, LocalSpeakerID: localSpeakerId, ConfirmedName: name, IsAccountOwner: isAccountOwner, ClientCorrectionID: clientCorrectionId, PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
				test.Fatal(err)
			}
		})
	}
	ownerFlags := func() map[string]bool {
		flags := map[string]bool{}
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			segments, err := transaction.ListRecordingTranscriptSegments(fixture.userId, fixture.transcriptRun.ID)
			if err != nil {
				test.Fatal(err)
			}
			if err := transaction.ProjectRecordingSpeakerCorrections(fixture.userId, segments); err != nil {
				test.Fatal(err)
			}
			for _, segment := range segments {
				flags[segment.LocalSpeakerID+"="+segment.EffectiveSpeakerName] = segment.IsAccountOwner
			}
		})
		return flags
	}
	// The default name is the account's display name, which is the username when there is no name.
	confirm("me-one", "speaker-one", "", true)
	if flags := ownerFlags(); !flags["speaker-one=transcript-owner"] || flags["speaker-two=speaker-two"] {
		test.Fatalf("the speaker marked as the person carries the flag and takes their name: %+v", flags)
	}
	confirm("me-two", "speaker-two", "Second Voice", true)
	flags := ownerFlags()
	if flags["speaker-one=transcript-owner"] || !flags["speaker-two=Second Voice"] {
		test.Fatalf("marking another speaker moves the flag: %+v", flags)
	}
	if count := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM recording_speaker_identity WHERE is_account_owner`); count != "1" {
		test.Fatalf("an account has at most one owner identity, found %s", count)
	}
	err := fixture.database.(interface{ RawExec(string) error }).RawExec(`UPDATE recording_speaker_identity SET is_account_owner = true WHERE confirmed_name = 'transcript-owner'`)
	if err == nil {
		test.Fatal("the unique index admitted a second owner for one account")
	}
}

func TestRecordingSummaryMigrationBackfillsTitleSourceAndReverses(test *testing.T) {
	fixture := newTranscriptFixture(test)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "titled-source", RecordingTitle: "Typed before", RecordingOrigin: models.RecordingOriginCapture}); err != nil {
			test.Fatal(err)
		}
	})
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0170_recording_summary" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("there is no migration 0170_recording_summary")
	}
	dbtest.Exec(test, fixture.database, migration.ReverseSQL)
	if columns := dbtest.QueryString(test, fixture.database, `SELECT COUNT(*)::text FROM information_schema.columns WHERE (table_name = 'recording' AND column_name IN ('recording_summary', 'recording_title_source', 'recording_summary_queued_at')) OR (table_name = 'recording_speaker_identity' AND column_name = 'is_account_owner')`); columns != "0" {
		test.Fatalf("the reverse removes the columns: %s left", columns)
	}
	dbtest.Exec(test, fixture.database, migration.SQL)
	if sources := dbtest.QueryString(test, fixture.database, `SELECT string_agg(recording_title_source, ',' ORDER BY client_recording_id) FROM recording`); sources != "none,user" {
		test.Fatalf("titled recordings backfill as user and untitled as none: %s", sources)
	}
}

func TestRecordingTranscriptRunsReportTheirSegmentCount(test *testing.T) {
	fixture := newTranscriptFixture(test)
	completeFixtureTranscript(test, fixture)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		transcriptRuns, err := transaction.ListRecordingTranscriptRuns(fixture.userId, fixture.media.RecordingID)
		if err != nil {
			test.Fatal(err)
		}
		if len(transcriptRuns) != 1 || transcriptRuns[0].SegmentCount != 2 {
			test.Fatalf("a listed run reports how many passages it holds: %+v", transcriptRuns)
		}
	})
}
