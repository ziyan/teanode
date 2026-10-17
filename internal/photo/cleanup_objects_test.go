package photo_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// slowFiles is a store whose deletes take as long as the test says: each waits
// to be let go, after saying it has begun.
type slowFiles struct {
	*memoryFiles
	begun   chan string
	proceed chan struct{}
	once    sync.Once
}

func (self *slowFiles) DeleteFileEverywhere(ctx context.Context, id string) error {
	self.once.Do(func() { self.begun <- id })
	select {
	case <-self.proceed:
	case <-ctx.Done():
		return ctx.Err()
	}
	return self.memoryFiles.DeleteFileEverywhere(ctx, id)
}

// Deleting an object takes as long as the store takes, and no row lock is held
// meanwhile: the upload's row can be written to while its parts are being
// deleted.
func TestCleanupHoldsNoRowLockWhileItDeletesAnUploadsParts(test *testing.T) {
	fixture := newFixture(test)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("abandoned", "Upload-Complete", "?0"), randomBytes(700, 61))
	fixture.expect(created, http.StatusCreated)
	uploadId := uploadIdOf(created.header.Get("Location"))
	fixture.expect(fixture.send(http.MethodDelete, created.header.Get("Location"), nil, nil), http.StatusNoContent)

	slow := &slowFiles{memoryFiles: fixture.files, begun: make(chan string, 1), proceed: make(chan struct{})}
	store := photo.New(fixture.database, slow, func() *config.Configuration { return fixture.configuration })
	finished := make(chan error, 1)
	go func() { finished <- store.Cleanup(test.Context(), 10) }()
	select {
	case <-slow.begun:
	case <-time.After(10 * time.Second):
		test.Fatal("cleanup never began to delete a part")
	}
	// A write to the row is not made to wait for the store.
	fixture.age("DO $$ BEGIN SET LOCAL lock_timeout = '2s'; UPDATE photo_upload SET updated_at = updated_at WHERE id = '" + uploadId + "'; END $$")
	close(slow.proceed)
	select {
	case err := <-finished:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		test.Fatal("cleanup did not finish")
	}
	if fixture.count("photo_upload") != "0" || fixture.files.has(models.PhotoUploadPartFileID(uploadId, 0)) {
		test.Fatal("the upload or its part is still there")
	}
}

// An upload that went idle is ended before its objects are deleted, so that a
// part cannot arrive for it while they go.
func TestCleanupEndsAnIdleUploadBeforeItDeletesItsParts(test *testing.T) {
	fixture := newFixture(test)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("idle", "Upload-Complete", "?0"), randomBytes(700, 62))
	fixture.expect(created, http.StatusCreated)
	uploadId := uploadIdOf(created.header.Get("Location"))
	fixture.age("UPDATE photo_upload SET updated_at = now() - interval '8 days'")

	slow := &slowFiles{memoryFiles: fixture.files, begun: make(chan string, 1), proceed: make(chan struct{})}
	store := photo.New(fixture.database, slow, func() *config.Configuration { return fixture.configuration })
	finished := make(chan error, 1)
	go func() { finished <- store.Cleanup(test.Context(), 10) }()
	select {
	case <-slow.begun:
	case <-time.After(10 * time.Second):
		test.Fatal("cleanup never began to delete a part")
	}
	// While the parts go, nothing more is taken for the upload.
	fixture.expect(fixture.send(http.MethodPatch, created.header.Get("Location"), map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": "700", "Upload-Complete": "?0"}, []byte("more")), http.StatusNotFound)
	close(slow.proceed)
	if err := <-finished; err != nil {
		test.Fatal(err)
	}
	if fixture.count("photo_upload") != "0" || fixture.files.has(models.PhotoUploadPartFileID(uploadId, 0)) {
		test.Fatal("the upload or its part is still there")
	}
}

// Deleting an account takes the objects that its rows named with it: the
// previews of every photo, those in the trash and those being deleted too, and
// the parts of every upload. Another person's objects, and the blobs of files
// (which the cleanup of unreferenced blobs deletes), are left.
func TestDeletingAnAccountsObjectsLeavesOthersAndBlobs(test *testing.T) {
	fixture := newFixture(test)
	otherId := dbtest.CreateUser(test, fixture.database, "photo-person-other")

	mine := fixture.uploadAndProcess("mine", jpegHeaders("mine"), smallJpeg(test, 90))
	inTheTrash := fixture.uploadAndProcess("trashed", jpegHeaders("trashed"), smallJpeg(test, 100))
	beingDeleted := fixture.uploadAndProcess("deleting", jpegHeaders("deleting"), smallJpeg(test, 110))
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(fixture.userId, []string{inTheTrash.ID, beingDeleted.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.MarkPhotosForDeletion(fixture.userId, []string{beingDeleted.ID}); err != nil {
			test.Fatal(err)
		}
	})
	open := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("open", "Upload-Complete", "?0"), randomBytes(600, 63))
	fixture.expect(open, http.StatusCreated)
	failed := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("failed", "Upload-Complete", "?0"), randomBytes(600, 64))
	fixture.expect(failed, http.StatusCreated)
	fixture.expect(fixture.send(http.MethodDelete, failed.header.Get("Location"), nil, nil), http.StatusNoContent)

	// The other person's photo, with a preview in the store.
	var others *models.Photo
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		if others, _, err = transaction.CreatePhoto(&models.Photo{UserID: otherId, ClientPhotoID: "others", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine}); err != nil {
			test.Fatal(err)
		}
	})
	otherPreview := models.PhotoPreviewFileID(others.ID, models.PhotoPreviewSmall)
	if err := fixture.files.PutFileStream(test.Context(), otherPreview, readSeeker([]byte("theirs")), 6); err != nil {
		test.Fatal(err)
	}

	var objectNames []string
	for _, found := range []*models.Photo{mine, inTheTrash, beingDeleted} {
		for _, photoPreviewSize := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
			name := models.PhotoPreviewFileID(found.ID, photoPreviewSize)
			if !fixture.files.has(name) {
				test.Fatalf("%s was never made", name)
			}
			objectNames = append(objectNames, name)
		}
	}
	for _, location := range []string{open.header.Get("Location"), failed.header.Get("Location")} {
		name := models.PhotoUploadPartFileID(uploadIdOf(location), 0)
		if !fixture.files.has(name) {
			test.Fatalf("%s was never stored", name)
		}
		objectNames = append(objectNames, name)
	}

	// Listing deletes nothing: the account's deletion may yet fail.
	listed, err := fixture.store.AccountObjects(test.Context(), fixture.userId)
	if err != nil {
		test.Fatal(err)
	}
	for _, name := range objectNames {
		if fixture.files.has(name) != true || !slices.Contains(listed, name) {
			test.Errorf("%s is deleted or not listed after listing", name)
		}
	}
	if slices.Contains(listed, otherPreview) {
		test.Error("another person's preview was listed")
	}
	fixture.store.DeleteObjects(test.Context(), listed)

	for _, name := range objectNames {
		if fixture.files.has(name) {
			test.Errorf("%s is still in the store", name)
		}
	}
	if !fixture.files.has(otherPreview) {
		test.Error("another person's preview was deleted")
	}
	// The files are blobs, left for the cleanup of blobs once the rows are gone.
	for _, found := range []*models.Photo{mine, inTheTrash, beingDeleted} {
		for _, file := range fixture.photoFiles(found.ID) {
			if !fixture.files.has(models.PhotoBlobFileID(file.BlobSHA256)) {
				test.Errorf("the blob of %s was deleted with the account's objects", found.ID)
			}
		}
	}
}
