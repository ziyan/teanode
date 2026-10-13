package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// accurateFixture is a piece of 20 seconds with two speakers: the first
// speaker's two passages are one turn, the second speaker has one passage, and
// the first speaker's last passage is under a second.
func accurateFixture(test *testing.T) *liveRecordingFixture {
	fixture := newLiveRecordingFixture(test)
	fixture.configuration.Agent.Recording.IsAccurateWordsEnabled = nil
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		return []fakeTranscriptionSegment{
			{SpeakerLabel: "A", StartMS: 1000, EndMS: 4000, Text: "First palette words."},
			{SpeakerLabel: "A", StartMS: 4100, EndMS: 7000, Text: "More palette words here."},
			{SpeakerLabel: "B", StartMS: 8000, EndMS: 11000, Text: "Reply from the other."},
			{SpeakerLabel: "A", StartMS: 12000, EndMS: 12500, Text: "Yes."},
		}
	}
	fixture.provider.plainAnswer = func(plainIndex int, request fakeTranscriptionRequest) string {
		if plainIndex == 0 {
			return "First pallet words. More pallet words here."
		}
		return "Reply from the other person."
	}
	return fixture
}

func (self *liveRecordingFixture) plainRequests() []fakeTranscriptionRequest {
	var plain []fakeTranscriptionRequest
	for index := 0; index < self.provider.requestCount(); index++ {
		if request := self.provider.requestAt(index); !strings.Contains(request.Model, "diarize") {
			plain = append(plain, request)
		}
	}
	return plain
}

func (self *liveRecordingFixture) usage() models.AgentUsageTotals {
	var totals models.AgentUsageTotals
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		totals, err = transaction.SumAgentUsage(self.assistant.ID, time.Now().Add(-time.Hour))
		if err != nil {
			self.test.Fatal(err)
		}
	})
	return totals
}

func TestTheSecondPassReplacesTheWordsOfEachTurnAndKeepsSpeakersAndTimes(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.transcribe(fixture.addPiece(20000, 0))
	segments := fixture.timeline()
	expected := []struct {
		startMS, endMS int64
		text           string
		firstText      string
	}{
		{1000, 4000, "First pallet words.", "First palette words."},
		{4100, 7000, "More pallet words here.", "More palette words here."},
		{8000, 11000, "Reply from the other person.", "Reply from the other."},
		{12000, 12500, "Yes.", "Yes."},
	}
	if len(segments) != len(expected) {
		test.Fatalf("the timeline has %d passages", len(segments))
	}
	for index, want := range expected {
		if segments[index].SegmentStartMS != want.startMS || segments[index].SegmentEndMS != want.endMS {
			test.Fatalf("passage %d moved to %d-%d", index, segments[index].SegmentStartMS, segments[index].SegmentEndMS)
		}
		if segments[index].EffectiveTranscriptText != want.text {
			test.Fatalf("passage %d reads %q, want %q", index, segments[index].EffectiveTranscriptText, want.text)
		}
		if segments[index].TranscriptText != want.firstText {
			test.Fatalf("passage %d lost the first words: %q", index, segments[index].TranscriptText)
		}
	}
	if segments[0].LocalSpeakerID != segments[1].LocalSpeakerID || segments[0].LocalSpeakerID == segments[2].LocalSpeakerID || segments[3].LocalSpeakerID != segments[0].LocalSpeakerID {
		test.Fatal("the second pass changed who spoke")
	}
	plain := fixture.plainRequests()
	if len(plain) != 2 {
		test.Fatalf("%d turns were sent again, want 2 (the half second turn keeps its words)", len(plain))
	}
	if plain[0].Model != "gpt-4o-transcribe" || plain[0].AudioDurationMS != 6000 || plain[1].AudioDurationMS != 3000 {
		test.Fatalf("the turns were sent as %q of %d and %d ms", plain[0].Model, plain[0].AudioDurationMS, plain[1].AudioDurationMS)
	}
	if !strings.Contains(plain[1].Prompt, "More pallet words here.") {
		test.Fatalf("the second turn was not sent the end of the first: %q", plain[1].Prompt)
	}
	totals := fixture.usage()
	if totals.Calls != 3 || totals.PromptTokens != 200 || totals.CompletionTokens != 20 {
		test.Fatalf("usage is %+v, want the first pass and two turns", totals)
	}
}

func TestAFailedSecondPassKeepsTheFirstWordsAndTheRunCompletes(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.provider.plainStatus = 500
	fixture.transcribe(fixture.addPiece(20000, 0))
	segments := fixture.timeline()
	if len(segments) != 4 || segments[0].EffectiveTranscriptText != "First palette words." || segments[2].EffectiveTranscriptText != "Reply from the other." {
		test.Fatal("a failed second pass changed the words")
	}
	if totals := fixture.usage(); totals.Calls != 1 {
		test.Fatalf("a refused request was counted as usage: %+v", totals)
	}
}

func TestAnEmptySecondPassKeepsTheFirstWords(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.provider.plainAnswer = func(int, fakeTranscriptionRequest) string { return "  " }
	fixture.transcribe(fixture.addPiece(20000, 0))
	if segments := fixture.timeline(); segments[0].EffectiveTranscriptText != "First palette words." {
		test.Fatal("an empty answer replaced the words")
	}
}

func TestTurnsAreSkippedWhenTheSettingIsOff(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.configuration.Agent.Recording.IsAccurateWordsEnabled = new(false)
	fixture.transcribe(fixture.addPiece(20000, 0))
	if len(fixture.plainRequests()) != 0 || fixture.timeline()[0].EffectiveTranscriptText != "First palette words." {
		test.Fatal("the second pass ran with the setting off")
	}
}

func TestTheSecondPassStopsWhenTheBudgetIsSpent(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.configuration.Agent.Limits.DailyTokensPerAgent = 50
	fixture.transcribe(fixture.addPiece(20000, 0))
	if len(fixture.plainRequests()) != 1 {
		test.Fatalf("%d turns were sent, the first turn's 110 tokens spent the 50 of the day", len(fixture.plainRequests()))
	}
	segments := fixture.timeline()
	if segments[0].EffectiveTranscriptText != "First pallet words." || segments[2].EffectiveTranscriptText != "Reply from the other." {
		test.Fatal("the turn before the budget ran out was not kept, or the one after was changed")
	}
}

func TestAPersonsCorrectionWinsOverTheAccurateWords(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.transcribe(fixture.addPiece(20000, 0))
	segments := fixture.timeline()
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingTranscriptText(fixture.userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: source.ID, RecordingTranscriptSegmentID: segments[0].ID, ClientCorrectionID: "person-fix", CorrectedTranscriptText: "First pallet, as written.", ExpectedSourceGeneration: source.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
	})
	segments = fixture.timeline()
	if segments[0].EffectiveTranscriptText != "First pallet, as written." || segments[0].AppliedTextCorrectionOrigin != models.TextCorrectionOriginPerson {
		test.Fatalf("the person's words lost to the second pass: %q", segments[0].EffectiveTranscriptText)
	}
	if segments[1].EffectiveTranscriptText != "More pallet words here." {
		test.Fatal("the correction reached the next passage")
	}
}

// The glossary pass looks for a phrase in what a passage reads as; the phrase
// "pallet" is only in the second pass's words.
func TestTheGlossaryPassCorrectsTheAccurateWords(test *testing.T) {
	fixture := accurateFixture(test)
	fixture.transcribe(fixture.addPiece(20000, 0))
	segments := fixture.timeline()
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		applied, err := transaction.ApplyRecordingGlossaryCorrections(fixture.userId, fixture.source.ID, []db.RecordingGlossaryCorrectionInput{{RecordingTranscriptSegmentID: segments[0].ID, OriginalPhrase: "pallet", CorrectedPhrase: "Pallet", GlossaryTerm: "Pallet"}})
		if err != nil || len(applied) != 1 {
			test.Fatal("the glossary pass did not find the phrase in the accurate words", err)
		}
	})
	segments = fixture.timeline()
	if segments[0].EffectiveTranscriptText != "First Pallet words." || segments[0].AppliedTextCorrectionOrigin != models.TextCorrectionOriginGlossary {
		test.Fatalf("the glossary fix is %q", segments[0].EffectiveTranscriptText)
	}
}

func TestTheSecondPassIsDoneBeforeTheRunCompletes(test *testing.T) {
	fixture := accurateFixture(test)
	transcriptRun := fixture.queue(fixture.addPiece(20000, 0))
	if err := fixture.execute(transcriptRun); err != nil {
		test.Fatal(err)
	}
	// Only a completed run is on the timeline, and it already has the words.
	if segments := fixture.timeline(); len(segments) == 0 || segments[0].AccurateTranscriptText != "First pallet words." {
		test.Fatal("the run completed before the second pass")
	}
}

func TestAJobThatStartsOverDoesNotSendATurnTwice(test *testing.T) {
	fixture := accurateFixture(test)
	transcriptRun := fixture.transcribe(fixture.addPiece(20000, 0))
	run := &Run{Job: fixture.claimedJobs[transcriptRun.ID], Agent: fixture.assistant, Owner: fixture.owner, settings: fixture.worker.settings}
	var chunks []*models.RecordingAudioChunk
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		chunks, err = transaction.ListRecordingAudioChunks(fixture.userId, transcriptRun.ID)
		if err != nil {
			test.Fatal(err)
		}
	})
	before := len(fixture.plainRequests())
	if err := fixture.worker.transcribeRecordingAccurateWords(test.Context(), run, transcriptRun, fixture.files, chunks, ""); err != nil {
		test.Fatal(err)
	}
	if len(fixture.plainRequests()) != before {
		test.Fatal("turns that had their words were sent again")
	}
}

func TestTurnsAreCutAtASpeakerChangeAndAtThirtySeconds(test *testing.T) {
	chunk := &models.RecordingAudioChunk{TimelineStartMS: 0, AudioDurationMS: 120000}
	passage := func(speaker string, startMS, endMS int64) *models.RecordingTranscriptSegment {
		return &models.RecordingTranscriptSegment{LocalSpeakerID: speaker, SegmentStartMS: startMS, SegmentEndMS: endMS}
	}
	turns := recordingTurnsOf([]*models.RecordingTranscriptSegment{
		passage("A", 0, 10000), passage("A", 10000, 20000), passage("A", 20000, 29000), passage("A", 29000, 35000),
		passage("B", 36000, 40000), passage("A", 41000, 50000),
	}, []*models.RecordingAudioChunk{chunk})
	if len(turns) != 4 || len(turns[0].Segments) != 3 || len(turns[1].Segments) != 1 || turns[2].Segments[0].LocalSpeakerID != "B" {
		test.Fatalf("the turns are %d", len(turns))
	}
}

func TestATurnsWordsAreSharedOutByTime(test *testing.T) {
	passage := func(identifier string, startMS, endMS int64) *models.RecordingTranscriptSegment {
		return &models.RecordingTranscriptSegment{ID: identifier, SegmentStartMS: startMS, SegmentEndMS: endMS}
	}
	one := splitRecordingTurnText("One whole answer.", []*models.RecordingTranscriptSegment{passage("a", 0, 5000)})
	if one["a"] != "One whole answer." {
		test.Fatal("a turn of one passage was split")
	}
	cut := splitRecordingTurnText("The first sentence is here. A second one follows it.", []*models.RecordingTranscriptSegment{passage("a", 0, 5000), passage("b", 5000, 10000)})
	if cut["a"] != "The first sentence is here." || cut["b"] != "A second one follows it." {
		test.Fatalf("the cut missed the sentence end: %q", cut)
	}
	spaceless := splitRecordingTurnText("今天我们开会讨论托盘的问题", []*models.RecordingTranscriptSegment{passage("a", 0, 3000), passage("b", 3000, 9000)})
	if len(spaceless) != 2 || spaceless["a"] == "" || spaceless["b"] == "" || spaceless["a"]+spaceless["b"] != "今天我们开会讨论托盘的问题" {
		test.Fatalf("text without spaces was split into %q", spaceless)
	}
	if splitRecordingTurnText("Hi", []*models.RecordingTranscriptSegment{passage("a", 0, 1000), passage("b", 1000, 2000)}) != nil {
		test.Fatal("fewer words than passages were shared out")
	}
}

func TestTheEndOfTheTurnBeforeStartsAtAWord(test *testing.T) {
	text := strings.Repeat("alpha beta ", 40)
	tail := recordingTurnContext(text, recordingTurnContextCharacters)
	if len([]rune(tail)) > recordingTurnContextCharacters || !strings.HasPrefix(tail, "alpha") && !strings.HasPrefix(tail, "beta") {
		test.Fatalf("the context is %q", tail)
	}
}
