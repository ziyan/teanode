package photo_test

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
)

// gateFaceReader stands in for the program that finds faces, which processing
// calls last, after the previews are kept and before the photo is marked ready.
// It lets a test hold a photo there, count how many are there at once, and make
// the reading panic.
type gateFaceReader struct {
	mutex      sync.Mutex
	callCount  int
	concurrent int
	peak       int
	// entered gets the number of each call as it begins.
	entered chan int
	// hold is asked about each call by its number and may return a channel the
	// call waits to see closed.
	hold func(callNumber int) <-chan struct{}
	// pause is how long every call takes.
	pause time.Duration
	// panicOn makes the call of that number panic.
	panicOn int
}

func newGateFaceReader() *gateFaceReader {
	return &gateFaceReader{entered: make(chan int, 1000)}
}

func (self *gateFaceReader) ReadFaces(ctx context.Context, picture image.Image) ([]*faces.FoundFace, error) {
	self.mutex.Lock()
	self.callCount++
	callNumber := self.callCount
	self.concurrent++
	self.peak = max(self.peak, self.concurrent)
	self.mutex.Unlock()
	defer func() {
		self.mutex.Lock()
		self.concurrent--
		self.mutex.Unlock()
	}()
	self.entered <- callNumber
	if callNumber == self.panicOn {
		panic("the face reader fell over")
	}
	if self.hold != nil {
		if wait := self.hold(callNumber); wait != nil {
			select {
			case <-wait:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	time.Sleep(self.pause)
	return nil, nil
}

func (self *gateFaceReader) peakCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.peak
}

// waitUntil polls a condition for ten seconds.
func waitUntil(test *testing.T, what string, condition func() bool) {
	test.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	test.Fatalf("gave up waiting for %s", what)
}

func (self *fixture) uploadPicture(clientPhotoId string, shade uint8) {
	self.test.Helper()
	self.expect(self.send(http.MethodPost, self.collectionUrl(), jpegHeaders(clientPhotoId), smallJpeg(self.test, shade)), http.StatusCreated)
}

func (self *fixture) stateOf(clientPhotoId string) models.PhotoProcessingState {
	self.test.Helper()
	return self.photoByClientId(clientPhotoId).PhotoProcessingState
}

// A photo that takes long does not hold up the ones behind it, not even ones
// that arrive while it is being made: the other worker is free, and looks.
func TestAPhotoThatTakesLongDoesNotHoldUpThePhotosThatArriveMeanwhile(test *testing.T) {
	fixture := newFixture(test)
	release := make(chan struct{})
	reader := newGateFaceReader()
	reader.hold = func(callNumber int) <-chan struct{} {
		if callNumber == 1 {
			return release
		}
		return nil
	}
	fixture.store.SetFaceReader(reader, nil)
	fixture.uploadPicture("slow", 20)

	finished := make(chan error, 1)
	go func() { finished <- fixture.store.Process(test.Context(), 100) }()
	select {
	case <-reader.entered:
	case <-time.After(10 * time.Second):
		test.Fatal("the first photo never reached the face reader")
	}

	// The pass is under way, held by the first photo. These arrive now.
	fixture.uploadPicture("quick-one", 90)
	fixture.uploadPicture("quick-two", 160)
	waitUntil(test, "the photos that arrived to be ready while the slow one is held", func() bool {
		return fixture.stateOf("quick-one") == models.PhotoProcessingReady && fixture.stateOf("quick-two") == models.PhotoProcessingReady
	})
	select {
	case err := <-finished:
		test.Fatalf("the pass ended while the slow photo was held: %v", err)
	default:
	}
	if fixture.stateOf("slow") != models.PhotoProcessingWaiting {
		test.Fatal("the slow photo was finished while it was held")
	}
	close(release)
	select {
	case err := <-finished:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		test.Fatal("the pass did not end")
	}
	if fixture.stateOf("slow") != models.PhotoProcessingReady {
		test.Fatal("the slow photo was not finished")
	}
}

func TestNoMoreThanTwoPhotosAreWorkedOnAtOnce(test *testing.T) {
	fixture := newFixture(test)
	reader := newGateFaceReader()
	reader.pause = 150 * time.Millisecond
	fixture.store.SetFaceReader(reader, nil)
	for index := range 6 {
		fixture.uploadPicture(fmt.Sprintf("many-%d", index), uint8(20+index*30))
	}
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	for index := range 6 {
		if state := fixture.stateOf(fmt.Sprintf("many-%d", index)); state != models.PhotoProcessingReady {
			test.Fatalf("photo %d is %s", index, state)
		}
	}
	if peak := reader.peakCount(); peak != 2 {
		test.Fatalf("at most %d photos were worked on at once, want exactly 2", peak)
	}
}

// A pass takes no more photos than it is told to.
func TestAPassTakesNoMorePhotosThanItIsGiven(test *testing.T) {
	fixture := newFixture(test)
	for index := range 5 {
		fixture.uploadPicture(fmt.Sprintf("capped-%d", index), uint8(20+index*30))
	}
	if err := fixture.store.Process(test.Context(), 3); err != nil {
		test.Fatal(err)
	}
	if ready := fixture.count("photo WHERE photo_processing_state = 'ready'"); ready != "3" {
		test.Fatalf("%s photos are ready after a pass of three", ready)
	}
}

// A panic while a photo is processed is that photo's failure and not the
// server's death, and a photo that does it every time runs out of attempts.
func TestAPanicWhileProcessingFailsThePhotoAndTheServerGoesOn(test *testing.T) {
	fixture := newFixture(test)
	reader := newGateFaceReader()
	reader.panicOn = 1
	fixture.store.SetFaceReader(reader, nil)
	fixture.uploadPicture("panics", 20)
	fixture.uploadPicture("fine", 160)
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	panicked, fine := fixture.photoByClientId("panics"), fixture.photoByClientId("fine")
	if panicked.PhotoProcessingState != models.PhotoProcessingWaiting || panicked.ProcessingAttemptCount != 1 || panicked.ProcessingErrorMessage != "The file could not be processed." ||
		panicked.ProcessingNotBefore == nil || panicked.ProcessingClaimedAt != nil {
		test.Fatalf("the photo that panicked: %+v", panicked)
	}
	if fine.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("the photo after it: %+v", fine)
	}

	// Every attempt panics: it is failed for good after the five.
	fixture.store.SetFaceReader(&panicEveryTime{}, nil)
	for range 4 {
		fixture.age("UPDATE photo SET processing_not_before = NULL WHERE client_photo_id = 'panics'")
		if err := fixture.store.Process(test.Context(), 100); err != nil {
			test.Fatal(err)
		}
	}
	if found := fixture.photoByClientId("panics"); found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingAttemptCount != 5 {
		test.Fatalf("after five panics: %+v", found)
	}
}

type panicEveryTime struct{}

func (self *panicEveryTime) ReadFaces(context.Context, image.Image) ([]*faces.FoundFace, error) {
	panic("the face reader fell over again")
}

// The attempt is counted when the photo is claimed, so a file that kills the
// process without a word cannot be tried for ever: its claim runs out, it is
// claimed again, and when all its attempts are gone it is failed.
func TestAFileThatKillsTheProcessIsFailedAfterItsAttempts(test *testing.T) {
	fixture := newFixture(test)
	fixture.uploadPicture("killer", 20)
	for attempt := 1; attempt <= 5; attempt++ {
		// The instance that claimed it is gone: it never recorded how it went.
		fixture.age("UPDATE photo SET photo_processing_state = 'waiting', processing_claimed_at = now() - interval '11 minutes', processing_attempt_count = " + fmt.Sprint(attempt-1) + " WHERE client_photo_id = 'killer'")
		dbClaim := fixture.claimOne()
		if dbClaim == nil || dbClaim.ProcessingAttemptCount != attempt {
			test.Fatalf("attempt %d: %+v", attempt, dbClaim)
		}
	}
	fixture.age("UPDATE photo SET processing_claimed_at = now() - interval '11 minutes' WHERE client_photo_id = 'killer'")
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	if found := fixture.photoByClientId("killer"); found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingErrorMessage == "" {
		test.Fatalf("%+v", found)
	}
}

// Decoded pictures share a budget of pixels. A picture that does not fit in what
// is left waits for the others to be done with theirs, and its photo is not
// failed for waiting.
func TestADecodeWaitsForRoomInThePixelBudget(test *testing.T) {
	fixture := newFixture(test)
	fixture.uploadPicture("waits-for-pixels", 20)
	// The whole budget is held, as by pictures being decoded.
	release, err := fixture.store.HoldPixels(test.Context(), 128_000_000)
	if err != nil {
		test.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- fixture.store.Process(test.Context(), 100) }()
	time.Sleep(500 * time.Millisecond)
	select {
	case <-finished:
		test.Fatal("a picture was decoded with no room for it")
	default:
	}
	if state := fixture.stateOf("waits-for-pixels"); state != models.PhotoProcessingWaiting {
		test.Fatalf("the photo is %s while it waits for room", state)
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		test.Fatal("the pass did not end once there was room")
	}
	if found := fixture.photoByClientId("waits-for-pixels"); found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingAttemptCount != 0 {
		test.Fatalf("%+v", found)
	}

	// Stopping while it waits is not the photo's fault.
	fixture.uploadPicture("stopped-waiting", 90)
	release, err = fixture.store.HoldPixels(test.Context(), 128_000_000)
	if err != nil {
		test.Fatal(err)
	}
	defer release()
	stopContext, stop := context.WithCancel(test.Context())
	go func() { finished <- fixture.store.Process(stopContext, 100) }()
	time.Sleep(300 * time.Millisecond)
	stop()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		test.Fatal("the pass did not end when it was stopped")
	}
	if found := fixture.photoByClientId("stopped-waiting"); found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingErrorMessage != "" {
		test.Fatalf("a photo was blamed for a pass that was stopped: %+v", found)
	}
}

func TestThePreparedWorkDirectoryIsEmptiedOfWhatAnEarlierRunLeft(test *testing.T) {
	fixture := newFixture(test)
	leftover := filepath.Join(fixture.workDirectory, "photo-12345")
	if err := os.MkdirAll(filepath.Join(leftover, "inside"), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "source"), []byte("a video that was being made"), 0o600); err != nil {
		test.Fatal(err)
	}
	if err := fixture.store.PrepareWorkDirectory(); err != nil {
		test.Fatal(err)
	}
	if entries, err := os.ReadDir(fixture.workDirectory); err != nil || len(entries) != 0 {
		test.Fatalf("%v %v", entries, err)
	}
	// And it is made when it is not there.
	if err := os.RemoveAll(fixture.workDirectory); err != nil {
		test.Fatal(err)
	}
	if err := fixture.store.PrepareWorkDirectory(); err != nil {
		test.Fatal(err)
	}
	if info, err := os.Stat(fixture.workDirectory); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		test.Fatalf("%v %v", info, err)
	}
}

// claimOne claims the next waiting photo the way a worker does and returns it.
func (self *fixture) claimOne() *models.Photo {
	self.test.Helper()
	var claimed []*models.Photo
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		if claimed, err = transaction.ClaimPhotosForProcessing(1, time.Now().Add(-10*time.Minute), 5); err != nil {
			self.test.Fatal(err)
		}
	})
	if len(claimed) == 0 {
		return nil
	}
	return claimed[0]
}
