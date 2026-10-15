package agent

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// fiveVoiceCapture runs a capture in which the provider never sees a voice it
// was not sent a sample for, so it answers with a fresh letter for each: two
// voices in each of the first three pieces make six labels, and the fourth
// piece's first voice is heard again under a letter because only four samples
// fit in a request. That voice is the one a check may merge into an earlier
// label. It returns the labels whose samples the fourth piece was not sent.
func fiveVoiceCapture(test *testing.T, fixture *liveRecordingFixture) []string {
	test.Helper()
	fixture.provider.hasMergeChecks = true
	pieceIndex := 0
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		defer func() { pieceIndex++ }()
		if pieceIndex == 3 {
			return []fakeTranscriptionSegment{{SpeakerLabel: "G", StartMS: 200, EndMS: 4200, Text: "A voice heard before."}, {SpeakerLabel: "Speaker 5", StartMS: 4300, EndMS: 7400, Text: "Another voice."}}
		}
		first, second := string(rune('A'+2*pieceIndex)), string(rune('B'+2*pieceIndex))
		return []fakeTranscriptionSegment{{SpeakerLabel: first, StartMS: 200, EndMS: 4200, Text: "The first voice."}, {SpeakerLabel: second, StartMS: 4300, EndMS: 7400, Text: "The second voice."}}
	}
	for range 3 {
		fixture.transcribe(fixture.addPiece(8000, 0))
		fixture.curate()
	}
	if checks := fixture.provider.mergeCheckRequests(); len(checks) != 0 {
		test.Fatal("labels whose voices were all sent were asked about", len(checks))
	}
	fixture.transcribe(fixture.addPiece(8000, 0))
	sent := fixture.provider.requestAt(fixture.provider.requestCount() - 1).KnownSpeakerNames
	if len(sent) != 4 {
		test.Fatal("the fourth piece was not sent four samples", sent)
	}
	var unsent []string
	for number := 1; number <= 6; number++ {
		if label := "Speaker " + string(rune('0'+number)); !slices.Contains(sent, label) {
			unsent = append(unsent, label)
		}
	}
	slices.Sort(unsent)
	return unsent
}

// curate runs the curation a person's confirmation would, after the one a
// finished piece starts in the background is over, so a test sees one at a time.
func (self *liveRecordingFixture) curate() {
	self.test.Helper()
	for range 600 {
		if _, isRunning := recordingCurationInFlight.Load(recordingCurationKey{Worker: self.worker, RecordingID: self.source.ID}); !isRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := self.worker.CurateRecordingSpeakers(self.test.Context(), self.userId, self.source.ID); err != nil {
		self.test.Fatal(err)
	}
}

// recordingUsageCount is how many model calls the agent has been charged for
// recordings so far.
func (self *liveRecordingFixture) recordingUsageCount() string {
	self.test.Helper()
	return dbtest.QueryString(self.test, self.database, `SELECT COALESCE(SUM("values"[5]), 0)::text FROM agent_usage WHERE agent_id = '`+self.assistant.ID+`' AND kind = 'recording'`)
}

// A voice heard under a fresh letter because its sample did not fit in the
// request is asked about once, against the samples that were left out, and the
// provider's answer under an earlier label merges the new one into it.
func TestANewLabelOfAnEarlierVoiceIsMergedIntoIt(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	unsent := fiveVoiceCapture(test, fixture)
	if len(unsent) != 2 {
		test.Fatal("two samples should have been left out of the fourth piece", unsent)
	}
	fixture.provider.checkAnswer = func(request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		return []fakeTranscriptionSegment{{SpeakerLabel: unsent[0], StartMS: 100, EndMS: request.AudioDurationMS - 100, Text: "A voice."}}
	}
	fixture.curate()
	checks := fixture.provider.mergeCheckRequests()
	if len(checks) != 1 {
		test.Fatal("the new label was not asked about exactly once", len(checks))
	}
	names := slices.Clone(checks[0].KnownSpeakerNames)
	slices.Sort(names)
	if !slices.Equal(names, unsent) {
		test.Fatal("the check was not sent the samples left out of the piece", names, unsent)
	}
	// Every request, the four pieces and the check, is charged once.
	if charged := fixture.recordingUsageCount(); charged != strconv.Itoa(fixture.provider.requestCount()) {
		test.Fatal("the check was not charged to the agent like a piece is", charged, fixture.provider.requestCount())
	}
	segments := fixture.timeline()
	var merged *models.RecordingTranscriptSegment
	for _, segment := range segments {
		if segment.MergedTemporarySpeakerLabel != "" {
			merged = segment
		}
	}
	if merged == nil || merged.MergedTemporarySpeakerLabel != "Speaker 7" || merged.EffectiveSpeakerName != unsent[0] || merged.TemporarySpeakerLabel != unsent[0] {
		test.Fatal("the new label does not read as the earlier one", merged)
	}
	// Asked once: curating again does not ask again, and the merged label is not sent as a sample.
	fixture.curate()
	if len(fixture.provider.mergeCheckRequests()) != 1 {
		test.Fatal("a checked label was asked about again")
	}
	// The provider answers with the samples it is sent, so no new label is made.
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		names := slices.Clone(request.KnownSpeakerNames)
		slices.Sort(names)
		return []fakeTranscriptionSegment{{SpeakerLabel: names[0], StartMS: 200, EndMS: 4200, Text: "The first voice."}, {SpeakerLabel: names[1], StartMS: 4300, EndMS: 7400, Text: "The second voice."}}
	}
	fixture.transcribe(fixture.addPiece(8000, 0))
	if sent := fixture.provider.requestAt(fixture.provider.requestCount() - 1).KnownSpeakerNames; slices.Contains(sent, "Speaker 7") {
		test.Fatal("a merged label's sample was sent again", sent)
	}
	// Naming the earlier label names the merged one, and separating gives the label back.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.NameRecordingTemporarySpeaker(fixture.userId, fixture.source.ID, unsent[0], "Participant One", false); err != nil {
			test.Fatal(err)
		}
	})
	for _, segment := range fixture.timeline() {
		if segment.MergedTemporarySpeakerLabel == "Speaker 7" && segment.EffectiveSpeakerName != "Participant One" {
			test.Fatal("naming the earlier label did not name the merged one", segment.EffectiveSpeakerName)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SeparateRecordingTemporarySpeaker(fixture.userId, fixture.source.ID, "Speaker 7"); err != nil {
			test.Fatal(err)
		}
	})
	for _, segment := range fixture.timeline() {
		if segment.TemporarySpeakerLabel == "Speaker 7" && segment.EffectiveSpeakerName != "Speaker 7" {
			test.Fatal("separating did not give the label back its own name", segment.EffectiveSpeakerName)
		}
	}
	// Separated, the label is its own again and is not asked about a second time.
	fixture.curate()
	if len(fixture.provider.mergeCheckRequests()) != 1 {
		test.Fatal("a separated label was asked about again")
	}
}

// Anything but one earlier label for the whole clip is no evidence: a letter,
// two labels, or an error leaves the new label as it is, and the label is
// still not asked about again.
func TestAnswersThatAreNotOneEarlierLabelDoNotMerge(test *testing.T) {
	for _, scenario := range []struct {
		description string
		answer      func(unsent []string, request fakeTranscriptionRequest) []fakeTranscriptionSegment
		status      int
	}{
		{description: "a letter", answer: func(unsent []string, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 100, EndMS: request.AudioDurationMS - 100, Text: "A voice."}}
		}},
		{description: "two labels", answer: func(unsent []string, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
			middleMS := request.AudioDurationMS / 2
			return []fakeTranscriptionSegment{{SpeakerLabel: unsent[0], StartMS: 100, EndMS: middleMS, Text: "A voice."}, {SpeakerLabel: unsent[1], StartMS: middleMS, EndMS: request.AudioDurationMS - 100, Text: "Another."}}
		}},
		{description: "one label and a letter", answer: func(unsent []string, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
			middleMS := request.AudioDurationMS / 2
			return []fakeTranscriptionSegment{{SpeakerLabel: unsent[0], StartMS: 100, EndMS: middleMS, Text: "A voice."}, {SpeakerLabel: "A", StartMS: middleMS, EndMS: request.AudioDurationMS - 100, Text: "Another."}}
		}},
		{description: "no passages", answer: func(unsent []string, request fakeTranscriptionRequest) []fakeTranscriptionSegment { return nil }},
		{description: "an error", status: 500},
	} {
		test.Run(scenario.description, func(test *testing.T) {
			fixture := newLiveRecordingFixture(test)
			unsent := fiveVoiceCapture(test, fixture)
			fixture.provider.checkStatus = scenario.status
			if scenario.answer != nil {
				fixture.provider.checkAnswer = func(request fakeTranscriptionRequest) []fakeTranscriptionSegment {
					return scenario.answer(unsent, request)
				}
			}
			fixture.curate()
			fixture.curate()
			if len(fixture.provider.mergeCheckRequests()) != 1 {
				test.Fatal("the label was not asked about exactly once", len(fixture.provider.mergeCheckRequests()))
			}
			for _, segment := range fixture.timeline() {
				if segment.MergedTemporarySpeakerLabel != "" {
					test.Fatal("the answer merged a label", segment)
				}
			}
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, fixture.source.ID)
				if err != nil || len(speakers) != 7 {
					test.Fatal("the label did not stay a speaker of its own", err, len(speakers))
				}
				for _, speaker := range speakers {
					if speaker.SpeakerLabel == "Speaker 7" && speaker.MergeCheckedAt == nil {
						test.Fatal("the check was not recorded")
					}
				}
			})
		})
	}
}

// With no earlier label left to choose from there is nothing to ask, and the
// label is marked so that it is not looked at again.
func TestALabelWithNoEarlierVoiceToChooseFromIsNotAsked(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.hasMergeChecks = true
	fixture.provider.answer = answerAsTheProviderDoes("Speaker 1", "Speaker 2")
	fixture.transcribe(fixture.addPiece(8000, 0))
	fixture.curate()
	if len(fixture.provider.mergeCheckRequests()) != 0 {
		test.Fatal("a recording's first labels were asked about")
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, fixture.source.ID)
		if err != nil || len(speakers) != 2 {
			test.Fatal(err, len(speakers))
		}
		for _, speaker := range speakers {
			if speaker.MergeCheckedAt == nil {
				test.Fatal("a label with nothing to choose from was not marked checked", speaker.SpeakerLabel)
			}
		}
	})
}

// A label still waiting to be asked about once its piece is older than the
// transcripts a curation looks at (one made before the check existed, or
// whose check could not be made when it was new) is still asked about at the
// next curation, from its own piece.
func TestALabelFromAnOlderPieceIsStillAskedAbout(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	unsent := fiveVoiceCapture(test, fixture)
	fixture.curate()
	if len(fixture.provider.mergeCheckRequests()) != 1 {
		test.Fatal("the new label was not asked about once", len(fixture.provider.mergeCheckRequests()))
	}
	// Later pieces, answered with the samples they are sent, make no new
	// label and push the label's piece out of the latest transcripts.
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		names := slices.Clone(request.KnownSpeakerNames)
		slices.Sort(names)
		return []fakeTranscriptionSegment{{SpeakerLabel: names[0], StartMS: 200, EndMS: 4200, Text: "The first voice."}, {SpeakerLabel: names[1], StartMS: 4300, EndMS: 7400, Text: "The second voice."}}
	}
	for range recordingTemporaryCurationRuns {
		fixture.transcribe(fixture.addPiece(8000, 0))
		fixture.curate()
	}
	// As if it had never been asked.
	dbtest.Exec(test, fixture.database, `UPDATE recording_temporary_speaker SET merge_checked_at = NULL WHERE speaker_label = 'Speaker 7'`)
	fixture.provider.checkAnswer = func(request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		return []fakeTranscriptionSegment{{SpeakerLabel: unsent[0], StartMS: 100, EndMS: request.AudioDurationMS - 100, Text: "A voice."}}
	}
	fixture.curate()
	if len(fixture.provider.mergeCheckRequests()) != 2 {
		test.Fatal("the label from an older piece was not asked about", len(fixture.provider.mergeCheckRequests()))
	}
	isMerged := false
	for _, segment := range fixture.timeline() {
		if segment.MergedTemporarySpeakerLabel == "Speaker 7" && segment.EffectiveSpeakerName == unsent[0] {
			isMerged = true
		}
	}
	if !isMerged {
		test.Fatal("the label from an older piece was not merged into the earlier one")
	}
}
