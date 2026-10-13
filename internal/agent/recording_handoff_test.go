package agent

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func timelineTexts(segments []*models.RecordingTranscriptSegment) []string {
	texts := make([]string, 0, len(segments))
	for _, segment := range segments {
		texts = append(texts, segment.TranscriptText)
	}
	return texts
}

func timelineStarts(segments []*models.RecordingTranscriptSegment) []int64 {
	starts := make([]int64, 0, len(segments))
	for _, segment := range segments {
		starts = append(starts, segment.SegmentStartMS)
	}
	return starts
}

// The first piece ends in the middle of a sentence. The next piece is sent
// with the end of the first one in front of it, so the sentence comes back
// whole, and the timeline shows it once, at the time it was spoken.
func TestASentenceAcrossACutIsReadOnceAndWhole(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		if requestIndex == 0 {
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 500, EndMS: 3000, Text: "First sentence."}, {SpeakerLabel: "A", StartMS: 8000, EndMS: 10000, Text: "I think we should"}}
		}
		// Times are from the start of the audio it was sent, which began 2 seconds before the cut.
		return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 0, EndMS: 5000, Text: "I think we should ship it on Friday."}, {SpeakerLabel: "B", StartMS: 6000, EndMS: 9000, Text: "Agreed."}}
	}
	first := fixture.addPiece(10000, 0)
	second := fixture.addPiece(10000, 0)
	fixture.transcribe(first)
	if texts := timelineTexts(fixture.timeline()); !slices.Equal(texts, []string{"First sentence.", "I think we should"}) {
		test.Fatal("the unfinished sentence was not shown while it was all there was", texts)
	}
	secondRun := fixture.transcribe(second)
	if secondRun.HandoffStartMS == nil || *secondRun.HandoffStartMS != 8000 {
		test.Fatal("the second piece did not record where it started reading", secondRun.HandoffStartMS)
	}
	if request := fixture.provider.requestAt(1); request.AudioDurationMS != 12000 {
		test.Fatal("the second piece was not sent with the end of the first", request.AudioDurationMS)
	}
	// The audio it was sent is the last two seconds of the first piece, then the second piece.
	sentWave := fixture.provider.requestAt(1).WaveBytes
	if !bytes.Equal(sentWave[44:44+64000], fixture.pieceWaves[0][44+256000:]) || !bytes.Equal(sentWave[44+64000:], fixture.pieceWaves[1][44:]) {
		test.Fatal("the second piece was not sent as the tail of the first followed by itself")
	}
	segments := fixture.timeline()
	if texts := timelineTexts(segments); !slices.Equal(texts, []string{"First sentence.", "I think we should ship it on Friday.", "Agreed."}) {
		test.Fatal("the sentence across the cut was not shown once and whole", texts)
	}
	if starts := timelineStarts(segments); !slices.Equal(starts, []int64{500, 8000, 14000}) {
		test.Fatal("passages are not at the time they were spoken", starts)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.UpdateRecordingCapture(fixture.userId, source.ID, source.SourceGeneration, models.RecordingCaptureStopped, 20000, nil); err != nil {
			test.Fatal(err)
		}
		if err := fixture.worker.FinishRecordingCapture(transaction, fixture.userId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	if dbtest.QueryString(test, fixture.database, "SELECT COUNT(*)::text FROM agent_document") != "3" {
		test.Fatal("memory kept the half sentence beside the whole one")
	}
}

// When the last passage ended two seconds before the cut, the speech did not
// run into it and the next piece is sent alone.
func TestNoHandoffWhenTheLastPassageEndedBeforeTheCut(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		if requestIndex == 0 {
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 500, EndMS: 3000, Text: "First sentence."}, {SpeakerLabel: "A", StartMS: 6000, EndMS: 8000, Text: "Finished before the cut."}}
		}
		return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 1000, EndMS: 4000, Text: "Next sentence."}}
	}
	fixture.transcribe(fixture.addPiece(10000, 0))
	secondRun := fixture.transcribe(fixture.addPiece(10000, 0))
	if secondRun.HandoffStartMS != nil {
		test.Fatal("a piece handed off from a passage that ended before the cut", *secondRun.HandoffStartMS)
	}
	if request := fixture.provider.requestAt(1); request.AudioDurationMS != 10000 {
		test.Fatal("the second piece was sent more than its own audio", request.AudioDurationMS)
	}
	if texts := timelineTexts(fixture.timeline()); !slices.Equal(texts, []string{"First sentence.", "Finished before the cut.", "Next sentence."}) {
		test.Fatal("a passage was hidden without a handoff", texts)
	}
}

// A pause between pieces is not a sentence running over; a passage that began
// more than 15 seconds before the cut is not re-read, because replacing only
// its end would show its beginning twice.
func TestNoHandoffAcrossAPauseOrFromALongPassage(test *testing.T) {
	for name, scenario := range map[string]struct {
		firstPieceMS int64
		gapMS        int64
		firstAnswer  fakeTranscriptionSegment
	}{
		"pause":        {firstPieceMS: 10000, gapMS: 5000, firstAnswer: fakeTranscriptionSegment{SpeakerLabel: "A", StartMS: 8000, EndMS: 10000, Text: "Cut off by a pause."}},
		"long passage": {firstPieceMS: 30000, gapMS: 0, firstAnswer: fakeTranscriptionSegment{SpeakerLabel: "A", StartMS: 2000, EndMS: 30000, Text: "A very long turn."}},
	} {
		test.Run(name, func(test *testing.T) {
			fixture := newLiveRecordingFixture(test)
			fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
				if requestIndex == 0 {
					return []fakeTranscriptionSegment{scenario.firstAnswer}
				}
				return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 1000, EndMS: 4000, Text: "Next sentence."}}
			}
			fixture.transcribe(fixture.addPiece(scenario.firstPieceMS, 0))
			secondRun := fixture.transcribe(fixture.addPiece(10000, scenario.gapMS))
			if secondRun.HandoffStartMS != nil || fixture.provider.requestAt(1).AudioDurationMS != 10000 {
				test.Fatal("a piece was handed off where it should not have been", secondRun.HandoffStartMS)
			}
			if len(fixture.timeline()) != 2 {
				test.Fatal("a passage was hidden without a handoff")
			}
		})
	}
}

// Two pieces picked up together must not skip the handoff because the second
// looked before the first had its transcript.
func TestAPieceWaitsForTheTranscriptOfThePieceBeforeIt(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		if requestIndex == 0 {
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 8000, EndMS: 10000, Text: "I think we should"}}
		}
		return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 0, EndMS: 5000, Text: "I think we should ship it."}}
	}
	firstRun := fixture.queue(fixture.addPiece(10000, 0))
	secondRun := fixture.queue(fixture.addPiece(10000, 0))
	var deferral *Deferral
	if err := fixture.execute(secondRun); !errors.As(err, &deferral) {
		test.Fatal("the second piece did not wait for the first", err)
	}
	if fixture.provider.requestCount() != 0 {
		test.Fatal("the second piece was sent before the first had a transcript")
	}
	if err := fixture.execute(firstRun); err != nil {
		test.Fatal(err)
	}
	if err := fixture.execute(secondRun); err != nil {
		test.Fatal(err)
	}
	if texts := timelineTexts(fixture.timeline()); !slices.Equal(texts, []string{"I think we should ship it."}) {
		test.Fatal("waiting for the first piece did not give the whole sentence", texts)
	}
}
