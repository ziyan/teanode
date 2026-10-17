package photo_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

func appendHeaders(offset, isComplete string) map[string]string {
	return map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": offset, "Upload-Complete": isComplete}
}

// All the bytes are in and the upload was not completed (the request that was
// completing it is gone): an append with no bytes, marked as the last, has the
// server complete it.
func TestAnEmptyFinalAppendCompletesAnUploadWhoseBytesAreAllThere(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 70)
	started := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("all-there", "Upload-Complete", "?0", "Upload-Length", itoa(len(picture))), picture)
	fixture.expect(started, http.StatusCreated)
	location := started.header.Get("Location")
	if started.header.Get("Upload-Offset") != itoa(len(picture)) || started.header.Get("Upload-Complete") != "?0" {
		test.Fatalf("%v", started.header)
	}
	if fixture.photoByClientId("all-there") != nil {
		test.Fatal("the upload was completed without being asked")
	}
	finished := fixture.send(http.MethodPatch, location, appendHeaders(itoa(len(picture)), "?1"), nil)
	fixture.expect(finished, http.StatusCreated)
	if finished.header.Get("Upload-Complete") != "?1" || finished.header.Get("X-Photo-ID") == "" || fixture.photoByClientId("all-there") == nil {
		test.Fatalf("%v", finished.header)
	}
}

func itoa(number int) string { return strconv.Itoa(number) }

// expectTryAgain checks an answer that is a refusal for the account's other
// uploads: a 503 that says when to ask again, between a minute and an hour.
func (self *fixture) expectTryAgain(got *answer) {
	self.test.Helper()
	self.expect(got, http.StatusServiceUnavailable)
	seconds, err := strconv.Atoi(got.header.Get("Retry-After"))
	if err != nil || seconds < 60 || seconds > 3600 {
		self.test.Fatalf("Retry-After is %q", got.header.Get("Retry-After"))
	}
}

// Bytes cleanup has just deleted are not trusted: a file that arrives with the
// bytes of a blob whose row is there and whose object is gone stores them
// again, and is not recorded against nothing.
func TestAFileRecordedAgainstBytesCleanupJustDeletedStoresThemAgain(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 71)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first"), picture)
	fixture.expect(first, http.StatusCreated)
	// Cleanup got to the object between the row being trusted and the file
	// being recorded; the row is still there and unmarked.
	_ = fixture.files.DeleteFileEverywhere(test.Context(), models.PhotoBlobFileID(digestOf(picture)))
	second := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("second", "X-Photo-File-Role", "edited"), picture)
	fixture.expect(second, http.StatusCreated)
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(picture))) {
		test.Fatal("a file was recorded against bytes that are not in the store")
	}

	// The same holds when the same bytes arrive again under a name the photo
	// already has: nothing new is recorded, and the bytes are still checked.
	_ = fixture.files.DeleteFileEverywhere(test.Context(), models.PhotoBlobFileID(digestOf(picture)))
	again := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first"), picture)
	fixture.expect(again, http.StatusCreated)
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(picture))) {
		test.Fatal("a file that was already recorded was left with no bytes")
	}
}

// observingFiles notes what the database says of a blob when its object is
// deleted, and can fail the deletion of one blob.
type observingFiles struct {
	*memoryFiles
	fixture  *fixture
	failFor  string
	mutex    sync.Mutex
	observed map[string]string
}

func (self *observingFiles) DeleteFileEverywhere(ctx context.Context, id string) error {
	if strings.HasPrefix(id, "photo-blob-") {
		digest := strings.TrimPrefix(id, "photo-blob-")
		// What another connection sees: the row, and its mark if it has one.
		state := dbtest.QueryString(self.fixture.test, self.fixture.database, "SELECT COALESCE((SELECT CASE WHEN deleting_at IS NOT NULL THEN 'marked' ELSE 'unmarked' END FROM photo_blob WHERE blob_sha256 = '"+digest+"'), 'no row')")
		self.mutex.Lock()
		if self.observed == nil {
			self.observed = map[string]string{}
		}
		self.observed[digest] = state
		self.mutex.Unlock()
		if digest == self.failFor {
			return errors.New("the object store is down for this one")
		}
	}
	return self.memoryFiles.DeleteFileEverywhere(ctx, id)
}

// A blob is marked in a transaction that has committed before its object is
// deleted, and the row goes after; one that cannot be deleted is passed over,
// and the blobs behind it are deleted.
func TestCleanupDeletesTheObjectOfAMarkedBlobAndPassesOverOneThatCannotBeDeleted(test *testing.T) {
	fixture := newFixture(test)
	var digests []string
	for index := range 3 {
		content := randomBytes(200, int64(60+index))
		digest := digestOf(content)
		digests = append(digests, digest)
		if err := fixture.files.PutFileStream(test.Context(), models.PhotoBlobFileID(digest), readSeeker(content), int64(len(content))); err != nil {
			test.Fatal(err)
		}
		dbtest.Exec(test, fixture.database, "INSERT INTO photo_blob (blob_sha256, blob_size_bytes, created_at, unreferenced_since) VALUES ('"+digest+"', 200, now() - interval '2 hours', now() - interval '"+itoa(3+index)+" hours')")
	}
	// The oldest is the one whose object cannot be deleted.
	observing := &observingFiles{memoryFiles: fixture.files, fixture: fixture, failFor: digests[2]}
	store := photo.New(fixture.database, observing, func() *config.Configuration { return fixture.configuration })
	if err := store.Cleanup(test.Context(), 10); err != nil {
		test.Fatalf("one blob that cannot be deleted failed the pass: %s", err)
	}
	for _, digest := range digests {
		if observing.observed[digest] != "marked" {
			test.Fatalf("blob %s was %s when its object was deleted, want marked and still there", digest[:8], observing.observed[digest])
		}
	}
	for _, digest := range digests[:2] {
		if fixture.files.has(models.PhotoBlobFileID(digest)) {
			test.Fatalf("blob %s was not deleted after the one before it failed", digest[:8])
		}
		if rows := dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM photo_blob WHERE blob_sha256 = '"+digest+"'"); rows != "0" {
			test.Fatalf("blob %s has a row left", digest[:8])
		}
	}
	// The one that failed is left marked, with its row, for the next pass.
	if !fixture.files.has(models.PhotoBlobFileID(digests[2])) || dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM photo_blob WHERE blob_sha256 = '"+digests[2]+"' AND deleting_at IS NOT NULL") != "1" {
		test.Fatal("the blob that could not be deleted is not left marked")
	}
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if fixture.files.has(models.PhotoBlobFileID(digests[2])) || fixture.count("photo_blob") != "0" {
		test.Fatal("the next pass did not delete it")
	}
}

// unreferencedPicture uploads a picture and deletes its photo for good, so
// that its blob is unreferenced and, once an hour has passed, cleanup's.
func (self *fixture) unreferencedPicture(clientPhotoId string, shade uint8) []byte {
	self.test.Helper()
	picture := smallJpeg(self.test, shade)
	first := self.send(http.MethodPost, self.collectionUrl(), jpegHeaders(clientPhotoId), picture)
	self.expect(first, http.StatusCreated)
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		photoId := first.header.Get("X-Photo-ID")
		if _, err := transaction.TrashPhotos(self.userId, []string{photoId}); err != nil {
			self.test.Fatal(err)
		}
		if _, err := transaction.MarkPhotosForDeletion(self.userId, []string{photoId}); err != nil {
			self.test.Fatal(err)
		}
		if _, err := transaction.DeleteMarkedPhotos([]string{photoId}); err != nil {
			self.test.Fatal(err)
		}
	})
	self.age("UPDATE photo_blob SET created_at = now() - interval '2 hours', unreferenced_since = now() - interval '2 hours'")
	return picture
}

// A file for the same bytes that completes after cleanup marked the blob and
// before it deletes the object has claimed them: it cleared the mark, recorded
// a file, and found the object still there. Cleanup reads the blob again under
// the blob's lock and leaves it, bytes included.
func TestCleanupLeavesABlobWhoseMarkWasClearedByAFileArriving(test *testing.T) {
	fixture := newFixture(test)
	picture := fixture.unreferencedPicture("gone", 72)
	arrivals := 0
	fixture.store.SetAfterBlobMarked(func(blobSha256 string) {
		arrivals++
		fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("arriving"), picture), http.StatusCreated)
	})
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if arrivals != 1 {
		test.Fatalf("the blob was marked %d times", arrivals)
	}
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(picture))) {
		test.Fatal("cleanup deleted the bytes of a blob a file had claimed")
	}
	if fixture.count("photo_blob") != "1" || fixture.count("photo_file") != "1" {
		test.Fatalf("blobs %s, files %s", fixture.count("photo_blob"), fixture.count("photo_file"))
	}
	// The photo is whole: its file is there to be read.
	arrived := fixture.photoByClientId("arriving")
	if arrived == nil || len(fixture.photoFiles(arrived.ID)) != 1 {
		test.Fatal("the arriving file was not recorded")
	}
}

// A file for the same bytes that starts while cleanup is deleting them waits
// for the blob's lock, and then stores them again: it is never recorded
// against the object cleanup is deleting.
func TestAFileArrivingWhileCleanupDeletesTheBytesStoresThemAgain(test *testing.T) {
	fixture := newFixture(test)
	picture := fixture.unreferencedPicture("gone", 73)
	arrival := make(chan *answer, 1)
	deleting := &arrivingFiles{memoryFiles: fixture.files, arrive: func() {
		go func() {
			arrival <- fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("arriving"), picture)
		}()
		// Long enough for the upload to get as far as it can without the lock.
		time.Sleep(500 * time.Millisecond)
	}}
	store := photo.New(fixture.database, deleting, func() *config.Configuration { return fixture.configuration })
	if err := store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	fixture.expect(<-arrival, http.StatusCreated)
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(picture))) {
		test.Fatal("a file was recorded against bytes cleanup deleted")
	}
	if fixture.count("photo_blob") != "1" || fixture.count("photo_file") != "1" {
		test.Fatalf("blobs %s, files %s", fixture.count("photo_blob"), fixture.count("photo_file"))
	}
}

// arrivingFiles stands for a file arriving at the moment a blob's object is
// deleted: arrive runs just before the deletion.
type arrivingFiles struct {
	*memoryFiles
	arrive func()
}

func (self *arrivingFiles) DeleteFileEverywhere(ctx context.Context, id string) error {
	if strings.HasPrefix(id, "photo-blob-") {
		self.arrive()
	}
	return self.memoryFiles.DeleteFileEverywhere(ctx, id)
}

// An account cannot keep more than its limit in unfinished uploads, however old:
// the quota check leaves out those idle for a day so that they do not hold room,
// and they are still bytes in the store until cleanup deletes them a week on.
// The upload that would take the unfinished bytes past the limit is answered
// like a refusal for the other uploads: 503 and a Retry-After, and the wait
// ends when the oldest of them is due to be deleted.
func TestUnfinishedUploadsOfAnyAgeAreBoundedByTheAccountsLimit(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	for index, name := range []string{"idle-one", "idle-two"} {
		started := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders(name, "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(1000, int64(100+index)))
		fixture.expect(started, http.StatusCreated)
		// Each sits idle for two days, so it does not hold room for the quota
		// check, and the next fits it; together they hold 2000 bytes of an
		// account that may keep 1500.
		fixture.age("UPDATE photo_upload SET updated_at = now() - interval '2 days'")
	}
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("one-too-many", "Upload-Complete", "?0", "Upload-Length", "100"), randomBytes(100, 102))
	fixture.expectTryAgain(refused)
	// The oldest is due to be deleted in five days, which is more than the hour
	// the wait is bounded to.
	if refused.header.Get("Retry-After") != "3600" {
		test.Fatalf("Retry-After is %q", refused.header.Get("Retry-After"))
	}
	// One that arrives in parts is judged as its parts arrive.
	unsaid := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("unsaid-too-many", "Upload-Complete", "?0"), randomBytes(100, 103))
	fixture.expectTryAgain(unsaid)

	// Once one of them is gone the rest fit the limit, and there is room.
	dbtest.Exec(test, fixture.database, "DELETE FROM photo_upload WHERE upload_headers->>'clientPhotoId' = 'idle-two'")
	accepted := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("room-again", "Upload-Complete", "?0", "Upload-Length", "100"), randomBytes(100, 104))
	fixture.expect(accepted, http.StatusCreated)
}

// How long a refused client is told to wait depends on the oldest upload that
// holds the room: when it is soon due to stop counting, the wait is as short,
// and never under a minute.
func TestTheWaitAfterARefusalForOtherUploadsFollowsTheOldestOne(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	other := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("holder", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(1000, 105))
	fixture.expect(other, http.StatusCreated)
	// The holder stops counting in 20 minutes.
	fixture.age("UPDATE photo_upload SET updated_at = now() - interval '23 hours 40 minutes'")
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 106))
	fixture.expectTryAgain(refused)
	seconds, _ := strconv.Atoi(refused.header.Get("Retry-After"))
	if seconds < 19*60 || seconds > 20*60+5 {
		test.Fatalf("Retry-After is %d seconds, want about 20 minutes", seconds)
	}
	// Due in less than a minute: a minute.
	fixture.age("UPDATE photo_upload SET updated_at = now() - interval '23 hours 59 minutes 50 seconds'")
	soon := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 106))
	fixture.expectTryAgain(soon)
	if soon.header.Get("Retry-After") != "60" {
		test.Fatalf("Retry-After is %q, want 60", soon.header.Get("Retry-After"))
	}
}

// Uploads that sat idle for a day no longer hold the account's room.
func TestAnUploadIdleForADayNoLongerHoldsTheAccountsRoom(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	idle := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("idle", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(1000, 80))
	fixture.expect(idle, http.StatusCreated)
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting-for-room", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 81))
	fixture.expectTryAgain(refused)

	fixture.age("UPDATE photo_upload SET updated_at = now() - interval '25 hours'")
	accepted := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting-for-room", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 81))
	fixture.expect(accepted, http.StatusCreated)
}

// A refusal that the other uploads' bytes cause is not the upload's fault: it is
// answered with 503 and a Retry-After, which every client takes as "try again",
// and the upload is left for the client to resume, where a refusal its own bytes
// cause is a 413 and ends it.
func TestARefusalCausedByOtherUploadsLeavesTheUploadToBeResumed(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	other := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("other-open", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(600, 82))
	fixture.expect(other, http.StatusCreated)

	// 1000 bytes of a file that does not say its size: with the 600 held by the
	// other upload that is 1600, over the limit, though it would fit alone.
	content := randomBytes(1000, 83)
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("resumable", "Upload-Complete", "?0"), content)
	fixture.expectTryAgain(refused)
	location := refused.interimLocation
	if location == "" {
		test.Fatal("the upload was not announced")
	}
	if status := fixture.send(http.MethodHead, location, nil, nil); status.status != http.StatusNoContent || status.header.Get("Upload-Offset") != "0" {
		test.Fatalf("the upload is gone: %d", status.status)
	}

	// The other upload is cancelled, and the same bytes go on from where it was.
	fixture.expect(fixture.send(http.MethodDelete, other.header.Get("Location"), nil, nil), http.StatusNoContent)
	finished := fixture.send(http.MethodPatch, location, appendHeaders("0", "?1"), content)
	fixture.expect(finished, http.StatusCreated)
	if fixture.photoByClientId("resumable") == nil {
		test.Fatal("the resumed upload made no photo")
	}

	// A file the account could not hold alone is ended.
	tooMuch := randomBytes(1600, 84)
	own := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("too-much", "Upload-Complete", "?0"), tooMuch)
	fixture.expect(own, http.StatusRequestEntityTooLarge)
	if status := fixture.send(http.MethodHead, own.interimLocation, nil, nil); status.status != http.StatusNotFound {
		test.Fatalf("an upload its own bytes refused is still there: %d", status.status)
	}
}

// What a file called itself is stored and shown: a name from a conversation
// that carries control characters is cleaned, not stored as it came.
func TestAnAttachmentsNameIsCheckedLikeAnUploadersName(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 91)
	attachment := &models.AgentAttachment{ID: "attachment-named", Name: "chart\x00\x1b[31m\n.jpg", ContentType: "image/jpeg", Size: int64(len(picture))}
	if err := fixture.files.PutFileStream(test.Context(), attachment.ID, readSeeker(picture), int64(len(picture))); err != nil {
		test.Fatal(err)
	}
	saved, err := fixture.store.SaveAttachment(test.Context(), fixture.userId, attachment)
	if err != nil {
		test.Fatal(err)
	}
	if saved.OriginalFilename != "chart[31m.jpg" {
		test.Fatalf("the name is %q", saved.OriginalFilename)
	}
}
