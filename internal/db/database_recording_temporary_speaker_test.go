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

func temporarySpeakerSample(text string) *models.RecordingBlob {
	digest := sha256.Sum256([]byte(text))
	return &models.RecordingBlob{BlobSizeBytes: 64044, BlobSHA256: hex.EncodeToString(digest[:])}
}

func timelineOf(test *testing.T, transaction db.Transaction, userId, recordingId string) []*models.RecordingTranscriptSegment {
	test.Helper()
	segments, _, _, err := transaction.ReadRecordingTranscriptTimelinePage(userId, recordingId, 0, 250)
	if err != nil {
		test.Fatal(err)
	}
	if err := transaction.ProjectRecordingSpeakerCorrections(userId, segments); err != nil {
		test.Fatal(err)
	}
	return segments
}

// Speakers of a capture get a label of their own across pieces, the person can
// name one, and they leave with the recording.
func TestTemporarySpeakersAreLabeledNamedAndForgottenWithTheRecording(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	recordingId := fixture.media.RecordingID
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "A", SegmentStartMS: 2000, SegmentEndMS: 2500, TranscriptText: "First speaker."},
			{LocalSpeakerID: "B", SegmentStartMS: 2500, SegmentEndMS: 3000, TranscriptText: "Second speaker."},
		}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	create := func(transaction db.Transaction, localSpeakerId, sample string) *models.RecordingTemporarySpeaker {
		speaker, err := transaction.CreateRecordingTemporarySpeaker(fixture.userId, &models.RecordingTemporarySpeaker{RecordingID: recordingId, SourceAttemptID: attempt.ID, SourceLocalSpeakerID: localSpeakerId, PassageStartMS: 2000, PassageEndMS: 4000}, temporarySpeakerSample(sample), 1<<20)
		if err != nil {
			test.Fatal(err)
		}
		return speaker
	}
	var first, second *models.RecordingTemporarySpeaker
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		first = create(transaction, "A", "first")
		second = create(transaction, "B", "second")
		if first.SpeakerLabel != "Speaker 1" || second.SpeakerLabel != "Speaker 2" {
			test.Fatal("speakers were not numbered in the order they were heard", first.SpeakerLabel, second.SpeakerLabel)
		}
		if again := create(transaction, "A", "first"); again.ID != first.ID || again.SpeakerLabel != "Speaker 1" {
			test.Fatal("repeating a request made another speaker")
		}
		segments := timelineOf(test, transaction, fixture.userId, recordingId)
		if len(segments) != 2 || segments[0].TemporarySpeakerLabel != "Speaker 1" || segments[0].EffectiveSpeakerName != "Speaker 1" || segments[1].TemporarySpeakerLabel != "Speaker 2" {
			test.Fatal("passages were not matched to their temporary speakers", segments)
		}
		blobRow, err := transaction.GetRecordingBlob(fixture.userId, first.RecordingBlobID)
		if err != nil || blobRow.IsStoredVerified || blobRow.RecordingBlobPurpose != models.RecordingBlobSpeakerReference {
			test.Fatal("the sample was not reserved as an unwritten speaker reference", err)
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.NameRecordingTemporarySpeaker(fixture.userId, recordingId, "Speaker 9", "Nobody", false)
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("a speaker that does not exist was named", err)
	}
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "temporary-other-owner")
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.NameRecordingTemporarySpeaker(otherOwnerId, recordingId, "Speaker 1", "Intruder", false)
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another account named a speaker", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.NameRecordingTemporarySpeaker(fixture.userId, recordingId, "Speaker 1", "Participant One", false); err != nil {
			test.Fatal(err)
		}
		// Naming again with the same name is harmless.
		if err := transaction.NameRecordingTemporarySpeaker(fixture.userId, recordingId, "Speaker 1", "Participant One", false); err != nil {
			test.Fatal(err)
		}
		segments := timelineOf(test, transaction, fixture.userId, recordingId)
		if segments[0].EffectiveSpeakerName != "Participant One" || !segments[0].HasUserConfirmedSpeaker || segments[0].TemporarySpeakerLabel != "" {
			test.Fatal("naming a speaker did not name its passages", segments[0])
		}
		if segments[1].TemporarySpeakerLabel != "Speaker 2" {
			test.Fatal("naming one speaker changed another")
		}
		speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, recordingId)
		if err != nil || len(speakers) != 2 || speakers[0].SpeakerIdentityID == "" || speakers[1].SpeakerIdentityID != "" {
			test.Fatal("the named speaker was not linked to its identity", err)
		}
		// The person says the second speaker is them; the name defaults to their own.
		if err := transaction.NameRecordingTemporarySpeaker(fixture.userId, recordingId, "Speaker 2", "", true); err != nil {
			test.Fatal(err)
		}
		segments = timelineOf(test, transaction, fixture.userId, recordingId)
		if !segments[1].IsAccountOwner || !segments[1].HasUserConfirmedSpeaker {
			test.Fatal("the person's own speaker was not marked", segments[1])
		}
	})
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, recordingId); err != nil {
			test.Fatal(err)
		}
		blobs, err := transaction.ListRecordingBlobsForCleanup(recordingId, 100)
		if err != nil {
			test.Fatal(err)
		}
		hasSample := false
		for _, blobRow := range blobs {
			hasSample = hasSample || blobRow.ID == first.RecordingBlobID
		}
		if !hasSample {
			test.Fatal("the cleanup would not find the sample's bytes")
		}
	})
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM recording_temporary_speaker") != "0" {
		test.Fatal("temporary speakers outlived their recording")
	}
}

// A sample is only sent for the recording it was cut from, and the number of
// speakers one recording can have is bounded.
func TestTemporarySpeakersAreBoundedAndBelongToOneRecording(test *testing.T) {
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := fixture.createChunk(test)
	attempt := fixture.beginAttempt(test, chunk, blob)
	segments := []*models.RecordingTranscriptSegment{}
	for index := 0; index < 13; index++ {
		segments = append(segments, &models.RecordingTranscriptSegment{LocalSpeakerID: string(rune('A' + index)), SegmentStartMS: 2000, SegmentEndMS: 2100, TranscriptText: "Words."})
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, attempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, segments); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	var limitError error
	for index := 0; index < 13; index++ {
		limitError = fixture.database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.CreateRecordingTemporarySpeaker(fixture.userId, &models.RecordingTemporarySpeaker{RecordingID: fixture.media.RecordingID, SourceAttemptID: attempt.ID, SourceLocalSpeakerID: string(rune('A' + index)), PassageStartMS: 2000, PassageEndMS: 4000}, temporarySpeakerSample(string(rune('a'+index))), 1<<30)
			return err
		})
		if limitError != nil {
			break
		}
	}
	if !errors.Is(limitError, db.ErrTooMuchAsked) {
		test.Fatal("a recording could have an unbounded number of speakers", limitError)
	}
	// An import has no temporary speakers.
	imported := newTranscriptFixture(test)
	importedChunk, importedBlob := imported.createChunk(test)
	importedAttempt := imported.beginAttempt(test, importedChunk, importedBlob)
	dbtest.RunTransactionOn(test, imported.database, func(transaction db.Transaction) {
		if _, err := transaction.FinishRecordingTranscriptionAttempt(imported.userId, importedAttempt.ID, imported.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{{LocalSpeakerID: "A", SegmentStartMS: 2000, SegmentEndMS: 2100, TranscriptText: "Words."}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(imported.userId, imported.transcriptRun.ID, imported.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
	})
	err := imported.database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingTemporarySpeaker(imported.userId, &models.RecordingTemporarySpeaker{RecordingID: imported.media.RecordingID, SourceAttemptID: importedAttempt.ID, SourceLocalSpeakerID: "A", PassageStartMS: 2000, PassageEndMS: 4000}, temporarySpeakerSample("import"), 1<<20)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("a speaker was made for a recording that is not a capture", err)
	}
}
