package photo_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
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
// a file it no longer has, and those previews are deleted.
func TestWorkOnAFileThatWasReplacedDoesNotMarkThePhotoReadyAndItsPreviewsAreDeleted(test *testing.T) {
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
		if preview, _ := fixture.previewOf(photoId, photoPreviewSize); preview != nil || fixture.files.has(models.PhotoPreviewFileID(photoId, photoPreviewSize)) {
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
