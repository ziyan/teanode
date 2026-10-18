package photo_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// holdFirstFaceReading holds the first photo the store works on where it is
// after its previews are kept, and returns the function that lets it go.
func holdFirstFaceReading(test *testing.T, fixture *fixture) (reader *gateFaceReader, letGo func()) {
	test.Helper()
	release := make(chan struct{})
	reader = newGateFaceReader()
	reader.hold = func(callNumber int) <-chan struct{} {
		if callNumber == 1 {
			return release
		}
		return nil
	}
	fixture.store.SetFaceReader(reader, nil)
	closed := false
	return reader, func() {
		if !closed {
			closed = true
			close(release)
		}
	}
}

// startProcessing runs a pass that takes one photo and no more, so that when
// the work on it ends the pass does too, and nothing else is claimed.
func (self *fixture) startProcessing(ctx context.Context) <-chan error {
	finished := make(chan error, 1)
	go func() { finished <- self.store.Process(ctx, 1) }()
	return finished
}

func waitForReading(test *testing.T, reader *gateFaceReader) {
	test.Helper()
	select {
	case <-reader.entered:
	case <-time.After(10 * time.Second):
		test.Fatal("the photo never reached the face reader")
	}
}

func waitForPass(test *testing.T, finished <-chan error) {
	test.Helper()
	select {
	case err := <-finished:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		test.Fatal("the pass did not end")
	}
}

// The file of a photo is replaced while the old one is being worked on. The
// photo goes back to waiting for the new one, and the old work's outcome is not
// recorded over it: the photo is not marked ready on the strength of previews of
// a file it no longer has, and the rows of those previews are taken back.
func TestWorkOnAFileThatWasReplacedDoesNotMarkThePhotoReadyAndItsPreviewsAreTakenBack(test *testing.T) {
	fixture := newFixture(test)
	reader, letGo := holdFirstFaceReading(test, fixture)
	defer letGo()
	fixture.uploadPicture("replaced", 20)
	finished := fixture.startProcessing(test.Context())
	waitForReading(test, reader)

	// The previews of the old file are made; now an edited version arrives.
	photoId := fixture.photoByClientId("replaced").ID
	if preview, _ := fixture.previewOf(photoId, models.PhotoPreviewSmall); preview == nil {
		test.Fatal("the old file's previews were not made yet")
	}
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("replaced", "X-Photo-File-Role", "edited"), smallJpeg(test, 200)), http.StatusCreated)
	letGo()
	waitForPass(test, finished)

	found := fixture.photoByClientId("replaced")
	if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingClaimedAt != nil || found.ProcessingAttemptCount != 0 {
		test.Fatalf("the old work marked the photo %s: %+v", found.PhotoProcessingState, found)
	}
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		if preview, _ := fixture.previewOf(photoId, photoPreviewSize); preview != nil {
			test.Fatalf("the %s preview of the old file was kept", photoPreviewSize)
		}
	}

	// The new file is worked on by the next pass.
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	if found := fixture.photoByClientId("replaced"); found.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("%+v", found)
	}
	if preview, _ := fixture.previewOf(photoId, models.PhotoPreviewSmall); preview == nil {
		test.Fatal("the new file has no previews")
	}
}

// The claim on a photo is lost to another worker on the same file, which makes
// the same previews: the work that was overtaken records nothing, and its
// previews, which are the other's too, stay.
func TestWorkWhoseClaimWasLostToAnotherOnTheSameFileRecordsNothingAndKeepsThePreviews(test *testing.T) {
	fixture := newFixture(test)
	reader, letGo := holdFirstFaceReading(test, fixture)
	defer letGo()
	fixture.uploadPicture("overtaken", 30)
	finished := fixture.startProcessing(test.Context())
	waitForReading(test, reader)

	photoId := fixture.photoByClientId("overtaken").ID
	// The claim ran out and the photo was claimed again by another instance.
	dbtest.Exec(test, fixture.database, "UPDATE photo SET processing_claimed_at = now() WHERE id = '"+photoId+"'")
	letGo()
	waitForPass(test, finished)

	found := fixture.photoByClientId("overtaken")
	if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingClaimedAt == nil {
		test.Fatalf("the overtaken work changed the photo: %+v", found)
	}
	if preview, _ := fixture.previewOf(photoId, models.PhotoPreviewSmall); preview == nil {
		test.Fatal("previews of the same file were deleted")
	}
}

// The file of a photo is replaced, and the work on the new file finishes before
// the work on the old one does. The old work, overtaken, takes back only what it
// made: the previews of the new file, which have the same names, stay, rows and
// objects both.
func TestOvertakenWorkDoesNotTakeBackThePreviewsOfTheWorkThatOvertookIt(test *testing.T) {
	fixture := newFixture(test)
	reader, letGo := holdFirstFaceReading(test, fixture)
	defer letGo()
	fixture.uploadPicture("replaced-twice", 22)
	finished := fixture.startProcessing(test.Context())
	waitForReading(test, reader)
	photoId := fixture.photoByClientId("replaced-twice").ID
	oldPreview, _ := fixture.previewOf(photoId, models.PhotoPreviewSmall)
	if oldPreview == nil {
		test.Fatal("the old file's previews were not made yet")
	}

	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("replaced-twice", "X-Photo-File-Role", "edited"), smallJpeg(test, 210)), http.StatusCreated)
	// The new file is worked on to the end while the old work waits.
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	newPreview, _ := fixture.previewOf(photoId, models.PhotoPreviewSmall)
	if found := fixture.photoByClientId("replaced-twice"); found.PhotoProcessingState != models.PhotoProcessingReady || newPreview == nil || !newPreview.CreatedAt.After(oldPreview.CreatedAt) {
		test.Fatalf("the new file was not worked on: %+v %+v", found, newPreview)
	}

	letGo()
	waitForPass(test, finished)
	for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
		preview, _ := fixture.previewOf(photoId, photoPreviewSize)
		if preview == nil || !fixture.files.has(models.PhotoPreviewFileID(photoId, photoPreviewSize)) {
			test.Fatalf("the %s preview of the new file was taken back by the work that was overtaken", photoPreviewSize)
		}
	}
	if found := fixture.photoByClientId("replaced-twice"); found.PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatalf("%+v", found)
	}
}

// Stopping the server while a photo is being worked on costs the photo nothing:
// the attempt is given back and the claim cleared, so the next run takes it at
// once and not when the claim has run out.
func TestStoppingWhileAPhotoIsBeingWorkedOnGivesBackTheAttemptAndTheClaim(test *testing.T) {
	fixture := newFixture(test)
	reader, letGo := holdFirstFaceReading(test, fixture)
	defer letGo()
	fixture.uploadPicture("interrupted", 40)
	stopContext, stop := context.WithCancel(test.Context())
	finished := fixture.startProcessing(stopContext)
	waitForReading(test, reader)
	if claimed := fixture.photoByClientId("interrupted"); claimed.ProcessingClaimedAt == nil || claimed.ProcessingAttemptCount != 1 {
		test.Fatalf("%+v", claimed)
	}
	stop()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		test.Fatal("the pass did not end when it was stopped")
	}
	found := fixture.photoByClientId("interrupted")
	if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingClaimedAt != nil || found.ProcessingAttemptCount != 0 || found.ProcessingNotBefore != nil {
		test.Fatalf("the interrupted photo: %+v", found)
	}
	// And the next run takes it at once.
	if claimed := fixture.claimOne(); claimed == nil || claimed.ID != found.ID {
		test.Fatalf("the photo was not claimable at once: %+v", claimed)
	}
}

// With ffmpeg missing, no video is claimed, so none has its file copied to the
// work directory, again and again. Looking again after a few minutes finds the
// programs once they are installed.
func TestVideosWaitWithoutBeingClaimedOrCopiedWhileFfmpegIsMissing(test *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		test.Skip("ffprobe is not installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		test.Skip("ffmpeg is not installed")
	}
	fixture := newFixture(test)
	installedPath := os.Getenv("PATH")
	test.Setenv("PATH", test.TempDir())
	started := time.Now()
	fixture.store.SetNow(func() time.Time { return started })

	clip := randomBytes(3000, 41)
	// A video, and a video that was uploaded as a picture, which is claimed
	// before anything knows better.
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "video/mp4", "X-Photo-Client-ID": "clip"}, clip), http.StatusCreated)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "video/mp4", "X-Photo-Client-ID": "mislabeled", "X-Photo-Kind": "picture"}, clip[:2000]), http.StatusCreated)
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	clipPhoto := fixture.photoByClientId("clip")
	if clipPhoto.PhotoProcessingState != models.PhotoProcessingWaiting || clipPhoto.ProcessingAttemptCount != 0 || clipPhoto.ProcessingNotBefore != nil || clipPhoto.ProcessingClaimedAt != nil {
		test.Fatalf("a video was claimed while its program is missing: %+v", clipPhoto)
	}
	// The one that was claimed found the program missing before it copied the
	// file, and waits longer each time it fails so.
	mislabeled := fixture.photoByClientId("mislabeled")
	if mislabeled.PhotoProcessingState != models.PhotoProcessingWaiting || mislabeled.ProcessingAttemptCount != 0 || mislabeled.ProcessingNotBefore == nil || mislabeled.ProcessingOutageCount != 1 {
		test.Fatalf("%+v", mislabeled)
	}
	if _, err := os.Stat(fixture.workDirectory); err == nil {
		test.Fatal("a file was copied to the work directory with no program to read it")
	}

	// Installed, and noticed once the few minutes have passed.
	test.Setenv("PATH", installedPath)
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	if again := fixture.photoByClientId("clip"); again.ProcessingAttemptCount != 0 {
		test.Fatalf("the program was looked for again too soon: %+v", again)
	}
	fixture.store.SetNow(func() time.Time { return started.Add(6 * time.Minute) })
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	// The bytes are not a video, so the attempt it was claimed for fails; that
	// it was claimed at all is what is shown.
	if again := fixture.photoByClientId("clip"); again.ProcessingAttemptCount != 1 || again.ProcessingErrorMessage != "The video could not be read." {
		test.Fatalf("the video was not worked on once ffmpeg was back: %+v", again)
	}
}

// A file is copied to the work directory only if the disk has room for twice
// its size and a reserve after the files being copied already; otherwise the
// photo waits, longer each time, and nothing is copied.
func TestAFileIsNotCopiedWithoutRoomForItAndWhatIsMadeFromIt(test *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		test.Skip("ffprobe is not installed")
	}
	fixture := newFixture(test)
	clip := randomBytes(3000, 42)
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{"Content-Type": "video/mp4", "X-Photo-Client-ID": "tight"}, clip), http.StatusCreated)
	// Room for the file itself and not for a reserve.
	fixture.store.SetFreeBytesOf(func(string) (int64, bool) { return 100 << 20, true })
	if err := fixture.store.Process(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	found := fixture.photoByClientId("tight")
	if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingAttemptCount != 0 || found.ProcessingOutageCount != 1 || found.ProcessingNotBefore == nil {
		test.Fatalf("%+v", found)
	}
	if promised := fixture.store.WorkSpacePromisedBytes(); promised != 0 {
		test.Fatalf("%d bytes are still promised", promised)
	}
}

// lostAnswerDatabase commits every renewal of a processing claim and then
// reports an error, as a connection that drops after the commit does.
type lostAnswerDatabase struct {
	db.Database
	mutex     sync.Mutex
	lostCount int
}

func (self *lostAnswerDatabase) TransactionContext(ctx context.Context, run func(db.Transaction) error) error {
	isRenewal := false
	err := self.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return run(&renewalWatcher{Transaction: transaction, isRenewal: &isRenewal})
	})
	if err == nil && isRenewal {
		self.mutex.Lock()
		self.lostCount++
		self.mutex.Unlock()
		return errors.New("the connection dropped after the commit")
	}
	return err
}

func (self *lostAnswerDatabase) lost() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.lostCount
}

type renewalWatcher struct {
	db.Transaction
	isRenewal *bool
}

func (self *renewalWatcher) RenewPhotoProcessingClaim(photoId string, claimedAt, renewedAt time.Time) (bool, error) {
	*self.isRenewal = true
	return self.Transaction.RenewPhotoProcessingClaim(photoId, claimedAt, renewedAt)
}

// A renewal of the claim that the database made, and whose answer the worker
// never got, moved the photo's claim to a time the worker does not know. The
// worker finds the claim it has by reading it, and records what it made against
// that, instead of throwing finished work away because it asked with the time
// before.
func TestWorkWhoseRenewalAnswerWasLostStillRecordsItsOutcome(test *testing.T) {
	fixture := newFixture(test)
	lossy := &lostAnswerDatabase{Database: fixture.database}
	store := photo.New(lossy, fixture.files, func() *config.Configuration { return fixture.configuration })
	store.SetWorkDirectory(fixture.workDirectory)
	store.SetClaimPollInterval(10 * time.Millisecond)
	store.SetClaimRenewalInterval(20 * time.Millisecond)
	release := make(chan struct{})
	reader := newGateFaceReader()
	reader.hold = func(callNumber int) <-chan struct{} {
		if callNumber == 1 {
			return release
		}
		return nil
	}
	store.SetFaceReader(reader, nil)

	fixture.uploadPicture("renewed", 23)
	finished := make(chan error, 1)
	go func() { finished <- store.Process(test.Context(), 1) }()
	waitForReading(test, reader)
	// Several renewals are made and not answered, one after the other.
	deadline := time.Now().Add(10 * time.Second)
	for lossy.lost() < 3 {
		if time.Now().After(deadline) {
			test.Fatal("the claim was not renewed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	waitForPass(test, finished)

	found := fixture.photoByClientId("renewed")
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingClaimedAt != nil {
		test.Fatalf("the finished work was thrown away: %+v", found)
	}
	if preview, _ := fixture.previewOf(found.ID, models.PhotoPreviewSmall); preview == nil {
		test.Fatal("the photo has no previews")
	}
}

// outageCallCount is how many calls the outage of outageDatabase refuses, and
// renewalsAfterOutageCount how many renewals must go through after it before
// the test goes on.
const (
	outageCallCount          = 3
	renewalsAfterOutageCount = 2
)

// outageDatabase loses the answer to the first renewal of a processing claim
// once it is armed, as lostAnswerDatabase does, and then refuses the next few
// calls, as a database that went away does: the worker can neither renew nor
// read the claim to find out what became of the renewal.
//
// It is armed by the test, once the photo is held in the face reader, because
// until then the worker is still reading and keeping the photo through the same
// database, and an outage that began there would fail that work instead. The
// outage lasts a number of calls and not a time: the only caller while the
// photo is held is the renewal, once every tick, and a machine that stalls the
// test for longer than any time chosen would let it pass with no call refused.
type outageDatabase struct {
	db.Database
	mutex                    sync.Mutex
	isArmed                  bool
	hasStarted               bool
	remainingRefusedCount    int
	renewalsAfterOutageCount int
}

func (self *outageDatabase) arm() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.isArmed = true
}

func (self *outageDatabase) TransactionContext(ctx context.Context, run func(db.Transaction) error) error {
	self.mutex.Lock()
	if self.remainingRefusedCount > 0 {
		self.remainingRefusedCount--
		self.mutex.Unlock()
		return errors.New("the database is unreachable")
	}
	self.mutex.Unlock()
	isRenewal := false
	err := self.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return run(&renewalWatcher{Transaction: transaction, isRenewal: &isRenewal})
	})
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if err != nil || !isRenewal || !self.isArmed {
		return err
	}
	if self.hasStarted {
		self.renewalsAfterOutageCount++
		return nil
	}
	self.hasStarted = true
	self.remainingRefusedCount = outageCallCount
	return errors.New("the connection dropped after the commit")
}

// isSettled says the outage happened, was over, and renewals went through after
// it.
func (self *outageDatabase) isSettled() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.hasStarted && self.remainingRefusedCount == 0 && self.renewalsAfterOutageCount >= renewalsAfterOutageCount
}

// A renewal whose answer was lost, followed by an outage long enough that the
// claim cannot be read either, is settled when the database is back: no other
// renewal is sent over it meanwhile, so the photo's claim is still one the
// worker knows, and the finished work is recorded.
func TestWorkWhoseRenewalCannotBeSettledDuringAnOutageStillRecordsItsOutcome(test *testing.T) {
	fixture := newFixture(test)
	unreliable := &outageDatabase{Database: fixture.database}
	store := photo.New(unreliable, fixture.files, func() *config.Configuration { return fixture.configuration })
	store.SetWorkDirectory(fixture.workDirectory)
	store.SetClaimPollInterval(10 * time.Millisecond)
	store.SetClaimRenewalInterval(20 * time.Millisecond)
	release := make(chan struct{})
	reader := newGateFaceReader()
	reader.hold = func(callNumber int) <-chan struct{} {
		if callNumber == 1 {
			return release
		}
		return nil
	}
	store.SetFaceReader(reader, nil)

	fixture.uploadPicture("outage", 24)
	finished := make(chan error, 1)
	go func() { finished <- store.Process(test.Context(), 1) }()
	waitForReading(test, reader)
	unreliable.arm()
	// The outage, and a few renewals after the database is back.
	deadline := time.Now().Add(10 * time.Second)
	for !unreliable.isSettled() {
		if time.Now().After(deadline) {
			test.Fatal("the outage did not happen and end")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	waitForPass(test, finished)

	found := fixture.photoByClientId("outage")
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingClaimedAt != nil {
		test.Fatalf("the finished work was thrown away: %+v", found)
	}
}
