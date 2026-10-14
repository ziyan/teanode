package agent

import (
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

func (self *fakeTranscriptionRequest) hasSpeakerName(name string) bool {
	return slices.Contains(self.KnownSpeakerNames, name)
}

// answerAsTheProviderDoes labels the two voices of the fixture by the names of
// the samples it was sent, and by a fresh letter when it was sent none.
func answerAsTheProviderDoes(firstName, secondName string) func(int, fakeTranscriptionRequest) []fakeTranscriptionSegment {
	return func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		first, second := "A", "B"
		if request.hasSpeakerName(firstName) {
			first = firstName
		}
		if request.hasSpeakerName(secondName) {
			second = secondName
		}
		return []fakeTranscriptionSegment{{SpeakerLabel: first, StartMS: 200, EndMS: 4200, Text: "The first voice."}, {SpeakerLabel: second, StartMS: 4300, EndMS: 7400, Text: "The second voice."}}
	}
}

func speakerNamesOf(segments []*models.RecordingTranscriptSegment) []string {
	names := make([]string, 0, len(segments))
	for _, segment := range segments {
		names = append(names, segment.EffectiveSpeakerName)
	}
	return names
}

// Each piece of a capture is transcribed on its own, so the provider calls the
// same person by a different letter every time. The first piece's speakers get
// a label and a sample; later pieces are sent the samples, answer with the
// labels, and the person is called the same thing in all of them. Naming a
// speaker names it everywhere, and the next piece is sent the sample under the
// name.
func TestSpeakersKeepTheirLabelAcrossPiecesAndNamingAppliesToAll(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = answerAsTheProviderDoes("Speaker 1", "Speaker 2")
	curate := func() {
		test.Helper()
		if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
			test.Fatal(err)
		}
	}
	for pieceIndex := 0; pieceIndex < 3; pieceIndex++ {
		fixture.transcribe(fixture.addPiece(8000, 0))
		curate()
	}
	if request := fixture.provider.requestAt(0); len(request.KnownSpeakerNames) != 0 {
		test.Fatal("the first piece was sent samples that do not exist yet", request.KnownSpeakerNames)
	}
	for requestIndex := 1; requestIndex < 3; requestIndex++ {
		names := slices.Clone(fixture.provider.requestAt(requestIndex).KnownSpeakerNames)
		slices.Sort(names)
		if !slices.Equal(names, []string{"Speaker 1", "Speaker 2"}) {
			test.Fatal("a later piece was not sent the samples of the speakers heard so far", requestIndex, names)
		}
	}
	segments := fixture.timeline()
	if names := speakerNamesOf(segments); !slices.Equal(names, []string{"Speaker 1", "Speaker 2", "Speaker 1", "Speaker 2", "Speaker 1", "Speaker 2"}) {
		test.Fatal("the same voices did not keep one label across the pieces", names)
	}
	for _, segment := range segments {
		if segment.TemporarySpeakerLabel != segment.EffectiveSpeakerName {
			test.Fatal("a passage does not say which temporary speaker it belongs to", segment.TemporarySpeakerLabel)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, fixture.source.ID)
		if err != nil || len(speakers) != 2 {
			test.Fatal("later pieces made new speakers for voices already heard", err, len(speakers))
		}
		if err := transaction.NameRecordingTemporarySpeaker(fixture.userId, fixture.source.ID, "Speaker 1", "Participant One", false); err != nil {
			test.Fatal(err)
		}
	})
	if names := speakerNamesOf(fixture.timeline()); !slices.Equal(names, []string{"Participant One", "Speaker 2", "Participant One", "Speaker 2", "Participant One", "Speaker 2"}) {
		test.Fatal("naming a speaker did not name it in every piece", names)
	}
	// The next piece is sent the sample under the name, and the provider's answer under that name is recognized.
	fixture.provider.answer = answerAsTheProviderDoes("Participant One", "Speaker 2")
	fixture.transcribe(fixture.addPiece(8000, 0))
	names := slices.Clone(fixture.provider.requestAt(3).KnownSpeakerNames)
	slices.Sort(names)
	if !slices.Equal(names, []string{"Participant One", "Speaker 2"}) {
		test.Fatal("the fourth piece was not sent the named sample under its name", names)
	}
	segments = fixture.timeline()
	if names := speakerNamesOf(segments); len(names) != 8 || names[6] != "Participant One" || names[7] != "Speaker 2" {
		test.Fatal("the fourth piece did not come back with the name", names)
	}
	if !segments[6].IsRecognizedSpeaker || segments[7].TemporarySpeakerLabel != "Speaker 2" {
		test.Fatal("the fourth piece's speakers were not recognized", segments[6], segments[7])
	}
	curate()
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, fixture.source.ID)
		if err != nil || len(speakers) != 2 {
			test.Fatal("a recognized speaker was given a new temporary label", err, len(speakers))
		}
	})
}

// The samples of one recording's speakers are for that recording only.
func TestTemporarySpeakerSamplesAreNotSentForAnotherRecording(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = answerAsTheProviderDoes("Speaker 1", "Speaker 2")
	fixture.transcribe(fixture.addPiece(8000, 0))
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	capabilities := llm.TranscriptionCapabilities{HasSpeakerReferences: true, MaximumSpeakerReferenceCount: 4}
	sent, _, err := fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, fixture.source.ID, capabilities, fixture.files)
	if err != nil || len(sent) != 2 {
		test.Fatal("the recording's own speakers were not offered", err, len(sent))
	}
	var otherSource *models.Recording
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		otherSource, err = transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "another-capture", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
	})
	sent, _, err = fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, otherSource.ID, capabilities, fixture.files)
	if err != nil || len(sent) != 0 {
		test.Fatal("a sample cut from one recording was offered for another", err, len(sent))
	}
}

// A meeting of more people than samples a request takes sends the samples of
// those who spoke in the latest pieces, not of those who spoke first.
func TestTheLatestSpeakersAreSentFirstWhenThereAreMoreThanFour(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	// Each piece is two people not heard before.
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		first, second := string(rune('A'+2*requestIndex)), string(rune('B'+2*requestIndex))
		return []fakeTranscriptionSegment{{SpeakerLabel: first, StartMS: 200, EndMS: 4200, Text: "The first voice."}, {SpeakerLabel: second, StartMS: 4300, EndMS: 7400, Text: "The second voice."}}
	}
	for pieceIndex := 0; pieceIndex < 3; pieceIndex++ {
		fixture.transcribe(fixture.addPiece(8000, 0))
		if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
			test.Fatal(err)
		}
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		speakers, err := transaction.ListRecordingTemporarySpeakers(fixture.userId, fixture.source.ID)
		if err != nil || len(speakers) != 6 {
			test.Fatal("the three pieces did not make six speakers", err, len(speakers))
		}
	})
	capabilities := llm.TranscriptionCapabilities{HasSpeakerReferences: true, MaximumSpeakerReferenceCount: 4}
	sent, _, err := fixture.worker.recordingSpeakerReferences(test.Context(), fixture.userId, fixture.source.ID, capabilities, fixture.files)
	if err != nil {
		test.Fatal(err)
	}
	names := make([]string, 0, len(sent))
	for _, reference := range sent {
		names = append(names, reference.SpeakerName)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Speaker 3", "Speaker 4", "Speaker 5", "Speaker 6"}) {
		test.Fatal("the samples sent are not of the latest two pieces' speakers", names)
	}
}
