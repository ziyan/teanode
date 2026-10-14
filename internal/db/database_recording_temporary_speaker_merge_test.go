package db_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// mergeFixture is a capture of two pieces with three temporary speakers: Speaker
// 1 from the first piece, and Speaker 2 and Speaker 3 from letters the second
// piece gave voices that the first piece's sample was not sent for.
type mergeFixture struct {
	*transcriptFixture
	recordingID string
}

func newMergeFixture(test *testing.T) *mergeFixture {
	test.Helper()
	fixture := newTranscriptFixtureOfOrigin(test, models.RecordingOriginCapture)
	chunk, blob := fixture.createChunk(test)
	secondBlob := &models.RecordingBlob{ID: db.RecordingAudioFileID(fixture.transcriptRun.ID, 1), BlobSizeBytes: 128, BlobSHA256: temporarySpeakerSample("second-wave-bytes").BlobSHA256}
	var secondChunk *models.RecordingAudioChunk
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		secondChunk, err = transaction.CreateRecordingAudioChunk(fixture.userId, fixture.transcriptRun.ID, fixture.claim, &models.RecordingAudioChunk{ChunkIndex: 1, TimelineStartMS: 3000, AudioDurationMS: 1000}, secondBlob, 4096)
		if err != nil {
			test.Fatal(err)
		}
	})
	firstAttempt := fixture.beginAttempt(test, chunk, blob)
	recordingId := fixture.media.RecordingID
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.VerifyRecordingBlob(fixture.userId, secondBlob.ID, secondBlob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		secondAttempt, err := transaction.CreateRecordingTranscriptionAttempt(fixture.userId, fixture.transcriptRun.ID, secondChunk.ID, fixture.claim, "USD", nil)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.SubmitRecordingTranscriptionAttempt(fixture.userId, secondAttempt.ID, fixture.claim); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, secondAttempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "X", SegmentStartMS: 3000, SegmentEndMS: 3500, TranscriptText: "Second piece, one voice."},
			{LocalSpeakerID: "Y", SegmentStartMS: 3500, SegmentEndMS: 3900, TranscriptText: "Second piece, another voice."},
		}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.FinishRecordingTranscriptionAttempt(fixture.userId, firstAttempt.ID, fixture.claim, &models.RecordingTranscriptionAttempt{RecordingAttemptState: models.RecordingAttemptComplete}, []*models.RecordingTranscriptSegment{
			{LocalSpeakerID: "A", SegmentStartMS: 2000, SegmentEndMS: 2500, TranscriptText: "First piece."},
		}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdateRecordingTranscriptProgress(fixture.userId, fixture.transcriptRun.ID, fixture.claim, models.RecordingProcessingComplete, "", 1000); err != nil {
			test.Fatal(err)
		}
		for _, created := range []struct {
			attemptID, localSpeakerID, sample string
		}{{firstAttempt.ID, "A", "first"}, {secondAttempt.ID, "X", "second"}, {secondAttempt.ID, "Y", "third"}} {
			if _, err := transaction.CreateRecordingTemporarySpeaker(fixture.userId, &models.RecordingTemporarySpeaker{RecordingID: recordingId, SourceAttemptID: created.attemptID, SourceLocalSpeakerID: created.localSpeakerID, PassageStartMS: 2000, PassageEndMS: 4000}, temporarySpeakerSample(created.sample), 1<<20); err != nil {
				test.Fatal(err)
			}
		}
	})
	return &mergeFixture{transcriptFixture: fixture, recordingID: recordingId}
}

func (self *mergeFixture) timelineNames(test *testing.T) []string {
	test.Helper()
	var names []string
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		for _, segment := range timelineOf(test, transaction, self.userId, self.recordingID) {
			names = append(names, segment.EffectiveSpeakerName+"/"+segment.MergedTemporarySpeakerLabel)
		}
	})
	return names
}

func (self *mergeFixture) mergedLabels(test *testing.T) map[string]string {
	test.Helper()
	labels := map[string]string{}
	rows := dbtest.QueryString(test, self.database, `SELECT COALESCE(string_agg(speaker.speaker_label || '>' || COALESCE(target.speaker_label, ''), ',' ORDER BY speaker.speaker_label), '') FROM recording_temporary_speaker speaker LEFT JOIN recording_temporary_speaker target ON target.id = speaker.merged_into_temporary_speaker_id WHERE speaker.recording_id = '`+self.recordingID+`'`)
	for _, pair := range strings.Split(rows, ",") {
		label, mergedInto, _ := strings.Cut(pair, ">")
		labels[label] = mergedInto
	}
	return labels
}

func (self *mergeFixture) merge(test *testing.T, label, intoLabel string) error {
	test.Helper()
	return self.database.Transaction(func(transaction db.Transaction) error {
		return transaction.MergeRecordingTemporarySpeaker(self.userId, self.recordingID, label, intoLabel)
	})
}

func (self *mergeFixture) separate(test *testing.T, label string) error {
	test.Helper()
	return self.database.Transaction(func(transaction db.Transaction) error {
		return transaction.SeparateRecordingTemporarySpeaker(self.userId, self.recordingID, label)
	})
}

func (self *mergeFixture) name(test *testing.T, label, confirmedName string) {
	test.Helper()
	dbtest.RunTransactionOn(test, self.database, func(transaction db.Transaction) {
		if err := transaction.NameRecordingTemporarySpeaker(self.userId, self.recordingID, label, confirmedName, false); err != nil {
			test.Fatal(err)
		}
	})
}

func (self *mergeFixture) activeCorrectionCount(test *testing.T) string {
	test.Helper()
	return dbtest.QueryString(test, self.database, "SELECT COUNT(*)::text FROM recording_speaker_correction WHERE recording_id = '"+self.recordingID+"' AND NOT is_removed")
}

// A label merged into an earlier one reads as it, keeps its own label for the
// person to separate from, and separating makes it read as itself again.
func TestAMergedTemporarySpeakerReadsAsTheOneItWasMergedInto(test *testing.T) {
	fixture := newMergeFixture(test)
	if err := fixture.merge(test, "Speaker 2", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	names := fixture.timelineNames(test)
	if len(names) != 3 || names[0] != "Speaker 1/" || names[1] != "Speaker 1/Speaker 2" || names[2] != "Speaker 3/" {
		test.Fatal("a merged label did not read as the earlier one", names)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		for _, segment := range timelineOf(test, transaction, fixture.userId, fixture.recordingID) {
			if segment.MergedTemporarySpeakerLabel != "" && segment.TemporarySpeakerLabel != "Speaker 1" {
				test.Fatal("a merged passage does not say which speaker it reads as", segment.TemporarySpeakerLabel)
			}
		}
	})
	if err := fixture.separate(test, "Speaker 2"); err != nil {
		test.Fatal(err)
	}
	names = fixture.timelineNames(test)
	if names[1] != "Speaker 2/" {
		test.Fatal("separating did not give the label back", names)
	}
}

// A merge follows the speaker's chain to its root, and a label with others
// merged into it takes them along when it is merged itself.
func TestMergingFollowsTheChainToItsRoot(test *testing.T) {
	fixture := newMergeFixture(test)
	if err := fixture.merge(test, "Speaker 3", "Speaker 2"); err != nil {
		test.Fatal(err)
	}
	if err := fixture.merge(test, "Speaker 2", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	labels := fixture.mergedLabels(test)
	if labels["Speaker 1"] != "" || labels["Speaker 2"] != "Speaker 1" || labels["Speaker 3"] != "Speaker 1" {
		test.Fatal("the label merged earlier did not follow its root", labels)
	}
	if names := fixture.timelineNames(test); names[1] != "Speaker 1/Speaker 2" || names[2] != "Speaker 1/Speaker 3" {
		test.Fatal("both merged labels did not read as the root", names)
	}
	// Merging into a label that is itself merged lands on the root.
	if err := fixture.separate(test, "Speaker 3"); err != nil {
		test.Fatal(err)
	}
	if err := fixture.merge(test, "Speaker 3", "Speaker 2"); err != nil {
		test.Fatal(err)
	}
	if labels := fixture.mergedLabels(test); labels["Speaker 3"] != "Speaker 1" {
		test.Fatal("a merge into a merged label did not land on the root", labels)
	}
}

func TestMergingAndSeparatingRefuseWhatCannotBeDone(test *testing.T) {
	fixture := newMergeFixture(test)
	for _, refused := range []struct {
		description, label, intoLabel string
	}{
		{"a label into itself", "Speaker 2", "Speaker 2"},
		{"a label that does not exist", "Speaker 9", "Speaker 1"},
		{"into a label that does not exist", "Speaker 2", "Speaker 9"},
		{"an empty label", "", "Speaker 1"},
	} {
		if err := fixture.merge(test, refused.label, refused.intoLabel); err == nil || errors.Is(err, db.ErrAlreadyExists) {
			test.Fatal("merged "+refused.description, err)
		}
	}
	if err := fixture.separate(test, "Speaker 2"); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("separated a label that is not merged", err)
	}
	if err := fixture.merge(test, "Speaker 2", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	if err := fixture.merge(test, "Speaker 1", "Speaker 2"); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("merged a speaker into one that is merged into it", err)
	}
	if err := fixture.merge(test, "Speaker 2", "Speaker 3"); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("merged a label that is already merged", err)
	}
	fixture.name(test, "Speaker 3", "Participant Three")
	if err := fixture.merge(test, "Speaker 3", "Speaker 1"); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("merged a named speaker into another", err)
	}
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "merge-other-owner")
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.MergeRecordingTemporarySpeaker(otherOwnerId, fixture.recordingID, "Speaker 3", "Speaker 1")
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another account merged a speaker", err)
	}
}

// Naming the root names the label merged into it, and undoing the naming from a
// passage of either takes the name off both.
func TestNamingTheRootNamesTheMergedLabelToo(test *testing.T) {
	fixture := newMergeFixture(test)
	if err := fixture.merge(test, "Speaker 2", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	fixture.name(test, "Speaker 1", "Participant One")
	if names := fixture.timelineNames(test); names[0] != "Participant One/" || names[1] != "Participant One/Speaker 2" || names[2] != "Speaker 3/" {
		test.Fatal("naming the root did not name the merged label", names)
	}
	// The merged label's passage is confirmed, as a named passage is.
	if count := fixture.activeCorrectionCount(test); count != "2" {
		test.Fatal("naming did not write a correction for each label's piece", count)
	}
	var correctionId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		for _, segment := range timelineOf(test, transaction, fixture.userId, fixture.recordingID) {
			if segment.MergedTemporarySpeakerLabel == "Speaker 2" {
				if !segment.HasUserConfirmedSpeaker {
					test.Fatal("the merged passage is not confirmed", segment)
				}
				correctionId = segment.AppliedSpeakerCorrectionID
			}
		}
		source, err := transaction.GetRecording(fixture.userId, fixture.recordingID)
		if err != nil {
			test.Fatal(err)
		}
		undone, err := transaction.UndoRecordingSpeakerNaming(fixture.userId, fixture.recordingID, correctionId, source.SourceGeneration)
		if err != nil || undone != 2 {
			test.Fatal("undoing from the merged passage did not reach both labels", undone, err)
		}
	})
	if names := fixture.timelineNames(test); names[0] != "Speaker 1/" || names[1] != "Speaker 1/Speaker 2" {
		test.Fatal("the name stayed after the undo", names)
	}
	// Naming by the merged label names the root.
	fixture.name(test, "Speaker 2", "Participant One")
	if names := fixture.timelineNames(test); names[0] != "Participant One/" || names[1] != "Participant One/Speaker 2" {
		test.Fatal("naming a merged label did not name its root", names)
	}
}

// A label merged into a speaker that is already named is named the way naming
// would have, and separating takes off only what the merge wrote.
// The provider made the merge, not the person, so a label merged into a named
// speaker reads as the name without a correction confirming it.
func TestMergingIntoANamedSpeakerReadsAsTheNameWithoutConfirmingIt(test *testing.T) {
	fixture := newMergeFixture(test)
	fixture.name(test, "Speaker 1", "Participant One")
	if count := fixture.activeCorrectionCount(test); count != "1" {
		test.Fatal("naming one speaker heard in one piece wrote the wrong corrections", count)
	}
	if err := fixture.merge(test, "Speaker 2", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	if names := fixture.timelineNames(test); names[1] != "Participant One/Speaker 2" {
		test.Fatal("a label merged into a named speaker does not read as the name", names)
	}
	if count := fixture.activeCorrectionCount(test); count != "1" {
		test.Fatal("the merge wrote a correction as if the person had confirmed it", count)
	}
	if err := fixture.separate(test, "Speaker 2"); err != nil {
		test.Fatal(err)
	}
	if count := fixture.activeCorrectionCount(test); count != "1" {
		test.Fatal("separating removed the earlier speaker's own naming", count)
	}
	names := fixture.timelineNames(test)
	if names[0] != "Participant One/" || names[1] != "Speaker 2/" || names[2] != "Speaker 3/" {
		test.Fatal("separating changed the wrong passages", names)
	}
}

func TestAMergeCheckIsRecordedOnce(test *testing.T) {
	fixture := newMergeFixture(test)
	speakerId := dbtest.QueryString(test, fixture.database, "SELECT id FROM recording_temporary_speaker WHERE recording_id = '"+fixture.recordingID+"' AND speaker_label = 'Speaker 2'")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.MarkRecordingTemporarySpeakerMergeChecked(fixture.userId, speakerId); err != nil {
			test.Fatal(err)
		}
		speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, fixture.recordingID)
		if err != nil {
			test.Fatal(err)
		}
		for _, speaker := range speakers {
			if (speaker.MergeCheckedAt != nil) != (speaker.SpeakerLabel == "Speaker 2") {
				test.Fatal("the check was recorded for the wrong speaker", speaker.SpeakerLabel)
			}
		}
	})
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.MarkRecordingTemporarySpeakerMergeChecked(fixture.userId, speakerId)
	})
	if !errors.Is(err, db.ErrAlreadyExists) {
		test.Fatal("a speaker's check was claimed twice", err)
	}
	otherOwnerId := dbtest.CreateUser(test, fixture.database, "check-other-owner")
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		return transaction.MarkRecordingTemporarySpeakerMergeChecked(otherOwnerId, speakerId)
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatal("another account marked a speaker checked", err)
	}
}

// Two labels of one piece merged into the same speaker are each named, and
// separating one leaves the other named.
func TestTwoLabelsOfOnePieceMergedIntoANamedSpeakerAreNamedAndSeparatedApart(test *testing.T) {
	fixture := newMergeFixture(test)
	if err := fixture.merge(test, "Speaker 2", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	if err := fixture.merge(test, "Speaker 3", "Speaker 1"); err != nil {
		test.Fatal(err)
	}
	fixture.name(test, "Speaker 1", "Participant One")
	if count := fixture.activeCorrectionCount(test); count != "3" {
		test.Fatal("each label of the piece was not named under its own key", count)
	}
	if names := fixture.timelineNames(test); names[0] != "Participant One/" || names[1] != "Participant One/Speaker 2" || names[2] != "Participant One/Speaker 3" {
		test.Fatal("both merged labels do not read as the name", names)
	}
	if err := fixture.separate(test, "Speaker 3"); err != nil {
		test.Fatal(err)
	}
	if count := fixture.activeCorrectionCount(test); count != "2" {
		test.Fatal("separating one label removed more than its own correction", count)
	}
	if names := fixture.timelineNames(test); names[1] != "Participant One/Speaker 2" || names[2] != "Speaker 3/" {
		test.Fatal("separating one label changed the other", names)
	}
}
