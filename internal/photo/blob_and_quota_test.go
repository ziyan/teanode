package photo_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/config"
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

// A file arriving with the bytes of a marked blob while its object is being
// deleted clears the mark, and the row is not dropped from under it.
func TestCleanupLeavesABlobWhoseMarkWasClearedByAFileArriving(test *testing.T) {
	fixture := newFixture(test)
	content := randomBytes(200, 70)
	digest := digestOf(content)
	if err := fixture.files.PutFileStream(test.Context(), models.PhotoBlobFileID(digest), readSeeker(content), int64(len(content))); err != nil {
		test.Fatal(err)
	}
	dbtest.Exec(test, fixture.database, "INSERT INTO photo_blob (blob_sha256, blob_size_bytes, created_at, unreferenced_since) VALUES ('"+digest+"', 200, now() - interval '2 hours', now() - interval '2 hours')")
	clearing := &clearingFiles{memoryFiles: fixture.files, fixture: fixture, digest: digest}
	store := photo.New(fixture.database, clearing, func() *config.Configuration { return fixture.configuration })
	if err := store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if fixture.count("photo_blob") != "1" {
		test.Fatal("the row of a blob a file had claimed was dropped")
	}
}

// clearingFiles stands for a file arriving with the same bytes at the moment
// their object is deleted: the blob's row is written again, which clears its mark.
type clearingFiles struct {
	*memoryFiles
	fixture *fixture
	digest  string
}

func (self *clearingFiles) DeleteFileEverywhere(ctx context.Context, id string) error {
	if id == models.PhotoBlobFileID(self.digest) {
		dbtest.Exec(self.fixture.test, self.fixture.database, "UPDATE photo_blob SET deleting_at = NULL, created_at = now(), unreferenced_since = now() WHERE blob_sha256 = '"+self.digest+"'")
	}
	return self.memoryFiles.DeleteFileEverywhere(ctx, id)
}

// Uploads that sat idle for a day no longer hold the account's room.
func TestAnUploadIdleForADayNoLongerHoldsTheAccountsRoom(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	idle := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("idle", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(1000, 80))
	fixture.expect(idle, http.StatusCreated)
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting-for-room", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 81))
	fixture.expect(refused, http.StatusRequestEntityTooLarge)

	fixture.age("UPDATE photo_upload SET updated_at = now() - interval '25 hours'")
	accepted := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting-for-room", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 81))
	fixture.expect(accepted, http.StatusCreated)
}

// A refusal that the other uploads' bytes cause is not the upload's fault: it is
// answered with 413 and the upload is left for the client to resume, where a
// refusal its own bytes cause ends it.
func TestARefusalCausedByOtherUploadsLeavesTheUploadToBeResumed(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	other := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("other-open", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(600, 82))
	fixture.expect(other, http.StatusCreated)

	// 1000 bytes of a file that does not say its size: with the 600 held by the
	// other upload that is 1600, over the limit, though it would fit alone.
	content := randomBytes(1000, 83)
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("resumable", "Upload-Complete", "?0"), content)
	fixture.expect(refused, http.StatusRequestEntityTooLarge)
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
