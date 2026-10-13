package agent

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// waitUntil polls a condition a background job is expected to make true.
func waitUntil(test *testing.T, description string, condition func() bool) {
	test.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			test.Fatal("timed out waiting for " + description)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitUntilTicking is waitUntil with the worker's tick standing in: now and
// then it claims what is due an hour from now, as a tick would after the wait
// of a piece that missed its wake.
func (self *liveRecordingFixture) waitUntilTicking(description string, condition func() bool) {
	self.test.Helper()
	lastTickAt := time.Now()
	waitUntil(self.test, description, func() bool {
		if time.Since(lastTickAt) > 200*time.Millisecond {
			lastTickAt = time.Now()
			if err := self.worker.claimJobs(self.test.Context(), time.Now().Add(time.Hour)); err != nil {
				self.test.Fatal(err)
			}
		}
		return condition()
	})
}

func (self *liveRecordingFixture) runState(transcriptRun *models.RecordingTranscriptRun) *models.RecordingTranscriptRun {
	self.test.Helper()
	var current *models.RecordingTranscriptRun
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		current, err = transaction.GetRecordingTranscriptRun(self.userId, transcriptRun.ID)
		if err != nil {
			self.test.Fatal(err)
		}
	})
	return current
}

func (self *liveRecordingFixture) job(transcriptRun *models.RecordingTranscriptRun) *models.AgentJob {
	self.test.Helper()
	var job *models.AgentJob
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		job, err = transaction.GetAgentJob(self.claimedJobs[transcriptRun.ID].ID)
		if err != nil || job == nil {
			self.test.Fatal("the piece's job is gone", err)
		}
	})
	return job
}

// speechAcrossEveryCut answers for three pieces of ten seconds whose speech
// runs across both cuts: each piece ends in the middle of a sentence that the
// next one finishes.
func speechAcrossEveryCut(fixture *liveRecordingFixture) func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
	return func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment {
		switch {
		case len(fixture.pieceWaves) > 0 && bytes.Equal(request.WaveBytes, fixture.pieceWaves[0]):
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 8000, EndMS: 10000, Text: "one of"}}
		case len(fixture.pieceWaves) > 1 && bytes.Equal(request.WaveBytes, fixture.pieceWaves[1]):
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 0, EndMS: 2000, Text: "two of"}, {SpeakerLabel: "A", StartMS: 8000, EndMS: 10000, Text: "and so"}}
		case len(fixture.pieceWaves) > 2 && bytes.Equal(request.WaveBytes, fixture.pieceWaves[2]):
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 0, EndMS: 2000, Text: "three"}}
		case len(fixture.pieceWaves) > 1 && bytes.Equal(request.WaveBytes[44:44+64000], fixture.pieceWaves[0][44+256000:]):
			return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 0, EndMS: 4000, Text: "one of two of"}}
		}
		return []fakeTranscriptionSegment{{SpeakerLabel: "A", StartMS: 0, EndMS: 4000, Text: "and so three"}}
	}
}

// Pieces of one recording are sent to the provider at the same time, the first
// one slowest, yet they are published in the order they were recorded and each
// is stitched to the one before it.
func TestPiecesAreTranscribedTogetherAndPublishedInOrderWithTheHandoff(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	release := make(chan struct{})
	// No request is answered until all three have arrived, which only
	// happens when the pieces are sent at the same time; the first is then
	// kept waiting after the others are answered.
	fixture.provider.hold = func(requestContext context.Context, request fakeTranscriptionRequest) {
		if request.AudioDurationMS != 10000 {
			return
		}
		for fixture.provider.requestCount() < 3 && requestContext.Err() == nil {
			time.Sleep(10 * time.Millisecond)
		}
		if bytes.Equal(request.WaveBytes, fixture.pieceWaves[0]) {
			select {
			case <-release:
			case <-requestContext.Done():
			}
		}
	}
	runs := []*models.RecordingTranscriptRun{fixture.queue(fixture.addPiece(10000, 0)), fixture.queue(fixture.addPiece(10000, 0)), fixture.queue(fixture.addPiece(10000, 0))}
	results := make([]chan error, len(runs))
	for index := len(runs) - 1; index >= 0; index-- {
		results[index] = make(chan error, 1)
		go func() { results[index] <- fixture.execute(runs[index]) }()
	}
	// The later pieces have their words but wait for the first to be published.
	for _, index := range []int{2, 1} {
		var deferral *Deferral
		if err := <-results[index]; !errors.As(err, &deferral) {
			test.Fatal("a later piece did not wait to be published", index, err)
		}
	}
	if fixture.provider.mostInFlight() < 3 {
		test.Fatal("the pieces were not sent at the same time")
	}
	if texts := timelineTexts(fixture.timeline()); len(texts) != 0 {
		test.Fatal("a piece was shown before the one before it was published", texts)
	}
	close(release)
	if err := <-results[0]; err != nil {
		test.Fatal(err)
	}
	for _, index := range []int{1, 2} {
		if err := fixture.execute(runs[index]); err != nil {
			test.Fatal(err)
		}
		if current := fixture.runState(runs[index]); current.RecordingProcessingState != models.RecordingProcessingComplete || current.HandoffStartMS == nil || *current.HandoffStartMS != int64(index)*10000-2000 {
			test.Fatal("a piece was not stitched to the one before it", index, current)
		}
	}
	segments := fixture.timeline()
	if texts := timelineTexts(segments); !slices.Equal(texts, []string{"one of two of", "and so three"}) {
		test.Fatal("the sentences across the cuts were not shown once and whole", texts)
	}
	if starts := timelineStarts(segments); !slices.Equal(starts, []int64{8000, 18000}) {
		test.Fatal("passages are not at the time they were spoken", starts)
	}
	// Three requests for the pieces, one more for each cut.
	if fixture.provider.diarizedRequestCount() != 5 {
		test.Fatal("unexpected number of requests", fixture.provider.diarizedRequestCount())
	}
}

// A piece waiting for the one before it is made due when that one is
// published, and runs without waiting for a tick.
func TestPublishingAPieceWakesTheNextOne(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	firstRun := fixture.queue(fixture.addPiece(10000, 0))
	secondRun := fixture.queue(fixture.addPiece(10000, 0))
	err := fixture.execute(secondRun)
	var deferral *Deferral
	if !errors.As(err, &deferral) {
		test.Fatal("the second piece did not wait", err)
	}
	fixture.finishJob(secondRun, err)
	if job := fixture.job(secondRun); job.NotBefore == nil || time.Until(*job.NotBefore) < time.Second {
		test.Fatal("the waiting piece was not put back with a wait", job.NotBefore)
	}
	if err := fixture.execute(firstRun); err != nil {
		test.Fatal(err)
	}
	// Nothing ticked: the worker was woken by the first piece's publication.
	waitUntil(test, "the second piece to complete", func() bool {
		return fixture.runState(secondRun).RecordingProcessingState == models.RecordingProcessingComplete
	})
	fixture.worker.Wait()
	if texts := timelineTexts(fixture.timeline()); !slices.Equal(texts, []string{"one of two of", "and so"}) {
		test.Fatal("the woken piece was not stitched to the first", texts)
	}
}

// A call stopped by a restart ends the piece without a transcript, and the
// pieces after it go on without it instead of waiting for it.
func TestARestartInTheMiddleOfACallDoesNotBlockThePiecesAfterIt(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	isHeld := make(chan struct{})
	fixture.provider.hold = func(requestContext context.Context, request fakeTranscriptionRequest) {
		if bytes.Equal(request.WaveBytes, fixture.pieceWaves[0]) {
			close(isHeld)
			<-requestContext.Done()
		}
	}
	runs := []*models.RecordingTranscriptRun{fixture.queue(fixture.addPiece(10000, 0)), fixture.queue(fixture.addPiece(10000, 0)), fixture.queue(fixture.addPiece(10000, 0))}
	firstContext, stopFirst := context.WithCancel(test.Context())
	defer stopFirst()
	firstResult := make(chan error, 1)
	go func() { firstResult <- fixture.executeIn(firstContext, runs[0]) }()
	<-isHeld
	for _, index := range []int{1, 2} {
		err := fixture.execute(runs[index])
		var deferral *Deferral
		if !errors.As(err, &deferral) {
			test.Fatal("a later piece did not wait", index, err)
		}
		fixture.finishJob(runs[index], err)
	}
	// The server stops: the request is hung up on and the job is put back.
	stopFirst()
	<-firstResult
	if state := fixture.runState(runs[0]).RecordingProcessingState; state != models.RecordingProcessingUncertain {
		test.Fatal("the interrupted piece is not left for an explicit retry", state)
	}
	fixture.waitUntilTicking("the later pieces to complete", func() bool {
		return fixture.runState(runs[1]).RecordingProcessingState == models.RecordingProcessingComplete && fixture.runState(runs[2]).RecordingProcessingState == models.RecordingProcessingComplete
	})
	fixture.worker.Wait()
	if fixture.runState(runs[1]).HandoffStartMS != nil {
		test.Fatal("a piece was stitched to a piece with no transcript")
	}
	if fixture.runState(runs[2]).HandoffStartMS == nil {
		test.Fatal("the piece after a published one was not stitched to it")
	}
	if texts := timelineTexts(fixture.timeline()); !slices.Equal(texts, []string{"two of", "and so three"}) {
		test.Fatal("the pieces after the interrupted one were not shown", texts)
	}
}

// A job claimed by an instance that then died is put back when the instance
// starts again, and the piece after it, which waited, goes on once it is done.
func TestAClaimLostWithTheInstanceIsRecoveredOnStart(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	firstRun := fixture.queue(fixture.addPiece(10000, 0))
	secondRun := fixture.queue(fixture.addPiece(10000, 0))
	err := fixture.execute(secondRun)
	var deferral *Deferral
	if !errors.As(err, &deferral) {
		test.Fatal("the second piece did not wait", err)
	}
	fixture.finishJob(secondRun, err)
	// The first job is running, and its instance is gone.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if released, err := transaction.ReleaseAgentJobsClaimedBy("live-worker"); err != nil || released != 1 {
			test.Fatal("the lost claim was not put back", err, released)
		}
	})
	fixture.claimAgain(time.Now())
	if fixture.job(firstRun).Status != models.AgentJobRunning {
		test.Fatal("the first piece's job was not claimed again")
	}
	if err := fixture.execute(firstRun); err != nil {
		test.Fatal(err)
	}
	fixture.waitUntilTicking("the second piece to complete", func() bool {
		return fixture.runState(secondRun).RecordingProcessingState == models.RecordingProcessingComplete
	})
	fixture.worker.Wait()
	if texts := timelineTexts(fixture.timeline()); !slices.Equal(texts, []string{"one of two of", "and so"}) {
		test.Fatal("the second piece was not stitched after the recovery", texts)
	}
}

// A piece whose job has given up is not waited for.
func TestAPieceIsNotPublishedAfterAPieceThatHasNoJobLeft(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	firstRun := fixture.queue(fixture.addPiece(10000, 0))
	secondRun := fixture.queue(fixture.addPiece(10000, 0))
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		job := fixture.claimedJobs[firstRun.ID]
		if _, err := transaction.FinishAgentJob(job.ID, job.ClaimID, &db.AgentJobOutcome{JobStatus: models.AgentJobDead, FinishedAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
	})
	if err := fixture.execute(secondRun); err != nil {
		test.Fatal("the second piece waited for a piece nothing would publish", err)
	}
	if current := fixture.runState(secondRun); current.RecordingProcessingState != models.RecordingProcessingComplete || current.HandoffStartMS != nil {
		test.Fatal("the second piece was not published alone", current)
	}
}

// Only a few pieces of one recording are between upload and publication, so a
// backlog cannot take every slot, and the ones beyond are queued waiting
// instead of being claimed again and again.
func TestOnlyAFewPiecesOfARecordingAreInFlightAtOnce(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	var runs []*models.RecordingTranscriptRun
	for range 5 {
		runs = append(runs, fixture.queueWithoutClaim(fixture.addPiece(10000, 0)))
	}
	fixture.claimAgain(time.Now())
	for index, transcriptRun := range runs {
		if isClaimed := fixture.claimedJobs[transcriptRun.ID] != nil; isClaimed != (index < recordingPiecesAtOnce) {
			test.Fatal("the wrong pieces were due", index, isClaimed)
		}
	}
	// Claimed anyway, the fourth piece is told to wait before anything is sent.
	fixture.claimAgain(time.Now().Add(time.Hour))
	var deferral *Deferral
	if err := fixture.execute(runs[3]); !errors.As(err, &deferral) {
		test.Fatal("the fourth piece started with three before it unpublished", err)
	}
	if fixture.provider.requestCount() != 0 {
		test.Fatal("a piece outside the window was sent", fixture.provider.requestCount())
	}
	if err := fixture.execute(runs[0]); err != nil {
		test.Fatal(err)
	}
	// With the first published, the fourth is inside the window: it is sent,
	// then waits for the third like the others.
	if err := fixture.execute(runs[3]); !errors.As(err, &deferral) {
		test.Fatal("the fourth piece did not wait to be published", err)
	}
	if fixture.provider.requestCount() != 2 {
		test.Fatal("the fourth piece was not sent once it was inside the window", fixture.provider.requestCount())
	}
}

// The second pass over the words of a piece comes after its publication, so the
// piece after it is stitched while that pass is still running.
func TestTheNextPieceDoesNotWaitForTheSecondPassOverThePieceBeforeIt(test *testing.T) {
	fixture := newLiveRecordingFixture(test)
	fixture.configuration.Agent.Recording.IsAccurateWordsEnabled = nil
	fixture.provider.answer = speechAcrossEveryCut(fixture)
	fixture.provider.plainAnswer = func(plainIndex int, request fakeTranscriptionRequest) string { return "words" }
	release := make(chan struct{})
	fixture.provider.hold = func(requestContext context.Context, request fakeTranscriptionRequest) {
		if !bytes.Contains([]byte(request.Model), []byte("diarize")) {
			select {
			case <-release:
			case <-requestContext.Done():
			}
		}
	}
	firstRun := fixture.queue(fixture.addPiece(10000, 0))
	secondRun := fixture.queue(fixture.addPiece(10000, 0))
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)
	go func() { firstResult <- fixture.execute(firstRun) }()
	waitUntil(test, "the second pass over the first piece to begin", func() bool { return fixture.provider.requestCount() > 1 })
	if current := fixture.runState(firstRun); current.RecordingProcessingState != models.RecordingProcessingTranscribing || current.PublishedAt == nil {
		test.Fatal("the first piece was not published before its second pass", current)
	}
	go func() { secondResult <- fixture.execute(secondRun) }()
	// The second piece's own request and the second look at the cut.
	waitUntil(test, "the second piece to be stitched", func() bool { return fixture.provider.diarizedRequestCount() == 3 })
	waitUntil(test, "the second piece to be published", func() bool { return fixture.runState(secondRun).PublishedAt != nil })
	close(release)
	if err := <-firstResult; err != nil {
		test.Fatal(err)
	}
	if err := <-secondResult; err != nil {
		test.Fatal(err)
	}
}
