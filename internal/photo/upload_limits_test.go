package photo_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// uploadIdOf is the identifier at the end of an upload's address.
func uploadIdOf(location string) string {
	return location[strings.LastIndex(location, "/")+1:]
}

// fillAccountWith records a photo of the person's whose file is the given size,
// as if it had been uploaded.
func (self *fixture) fillAccountWith(clientPhotoId string, sizeBytes int64) {
	self.test.Helper()
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		created, _, err := transaction.CreatePhoto(&models.Photo{UserID: self.userId, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine})
		if err != nil {
			self.test.Fatal(err)
		}
		digest := digestOf([]byte("filler " + clientPhotoId))
		if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digest, BlobSizeBytes: sizeBytes}); err != nil {
			self.test.Fatal(err)
		}
		if _, err := transaction.SetPhotoFile(self.userId, created.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: digest, FileSizeBytes: sizeBytes}, 0); err != nil {
			self.test.Fatal(err)
		}
	})
}

// An upload that has not finished holds bytes that are not photos yet, and they
// count towards the account's limit: otherwise any number of uploads each pass
// the check at the door and then fill the store.
func TestTheBytesOfOpenUploadsCountTowardsTheAccountsLimit(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)

	// The first says its size and sends some of it: 600 of 1000 are held.
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first-open", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(600, 21))
	fixture.expect(first, http.StatusCreated)

	// A second of 1000 would be 1600 with it: refused at the door, before a
	// byte is sent, and no upload is made.
	before := fixture.count("photo_upload")
	refused := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("second-open", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(10, 22))
	fixture.expectTryAgain(refused)
	if !strings.Contains(refused.body, "1500") || fixture.count("photo_upload") != before {
		test.Fatalf("%s, %s uploads", refused.body, fixture.count("photo_upload"))
	}

	// One that does not say its size is judged as its parts arrive.
	unsaid := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("unsaid-open", "Upload-Complete", "?0"), randomBytes(1000, 23))
	fixture.expectTryAgain(unsaid)

	// Sending the same file again does not count the try it ends.
	again := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first-open", "Upload-Complete", "?0", "Upload-Length", "1000"), randomBytes(600, 21))
	fixture.expect(again, http.StatusCreated)

	// And the rest of the first can still be sent: it was counted when it began.
	rest := fixture.send(http.MethodPatch, again.header.Get("Location"), map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": "600", "Upload-Complete": "?1"}, randomBytes(1000, 21)[600:])
	fixture.expect(rest, http.StatusCreated)
}

func TestStartingAnUploadAgainEndsTheEarlierTryAndCleanupRemovesItsParts(test *testing.T) {
	fixture := newFixture(test)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("restarted", "Upload-Complete", "?0"), randomBytes(500, 24))
	fixture.expect(first, http.StatusCreated)
	firstPart := models.PhotoUploadPartFileID(uploadIdOf(first.header.Get("Location")), 0)
	if !fixture.files.has(firstPart) {
		test.Fatal("the first part was not stored")
	}
	second := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("restarted", "Upload-Complete", "?0"), randomBytes(500, 24))
	fixture.expect(second, http.StatusCreated)

	// The earlier try is over: it is not found, and takes no more bytes.
	fixture.expect(fixture.send(http.MethodHead, first.header.Get("Location"), nil, nil), http.StatusNotFound)
	fixture.expect(fixture.send(http.MethodPatch, first.header.Get("Location"), map[string]string{"Upload-Offset": "500", "Upload-Complete": "?1"}, nil), http.StatusNotFound)
	fixture.expect(fixture.send(http.MethodHead, second.header.Get("Location"), nil, nil), http.StatusNoContent)
	if open := dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM photo_upload WHERE photo_upload_state = 'receiving'"); open != "1" {
		test.Fatalf("%s uploads are open", open)
	}
	// Its parts are deleted by cleanup, with no week to wait.
	if err := fixture.store.Cleanup(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	if fixture.files.has(firstPart) {
		test.Fatal("the ended try's part is still in the store")
	}
	if !fixture.files.has(models.PhotoUploadPartFileID(uploadIdOf(second.header.Get("Location")), 0)) {
		test.Fatal("the new try's part was deleted")
	}

	// A hundred restarts leave one open, not a hundred.
	for range 30 {
		fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("restarted", "Upload-Complete", "?0"), randomBytes(100, 25)), http.StatusCreated)
	}
	if open := dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM photo_upload WHERE photo_upload_state = 'receiving'"); open != "1" {
		test.Fatalf("%s uploads are open after many restarts", open)
	}
}

func TestAnUploadInTooManyPartsIsEndedAndTheNextIndexIsKeptOnTheUpload(test *testing.T) {
	fixture := newFixture(test)
	// With a file limit of 1 MiB, twice as many parts as it needs when each
	// is full is none, and the cap is the 16 that are always allowed.
	fixture.configuration.Photos.MaxFileBytes = config.ByteSize(1 << 20)
	created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("crumbs", "Upload-Complete", "?0"), []byte("x"))
	fixture.expect(created, http.StatusCreated)
	location := created.header.Get("Location")
	for index := 1; index < 16; index++ {
		got := fixture.send(http.MethodPatch, location, map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": strconv.Itoa(index), "Upload-Complete": "?0"}, []byte("x"))
		fixture.expect(got, http.StatusNoContent)
	}
	if parts := dbtest.QueryString(test, fixture.database, "SELECT part_count FROM photo_upload WHERE id = '"+uploadIdOf(location)+"'"); parts != "16" {
		test.Fatalf("the upload says %s parts", parts)
	}
	over := fixture.send(http.MethodPatch, location, map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": "16", "Upload-Complete": "?0"}, []byte("x"))
	fixture.expect(over, http.StatusBadRequest)
	if !strings.Contains(over.body, "too many parts") {
		test.Fatal(over.body)
	}
	fixture.expect(fixture.send(http.MethodHead, location, nil, nil), http.StatusNotFound)
	if got := fmt.Sprint(fixture.count("photo_upload_part")); got != "16" {
		test.Fatalf("%s parts were stored", got)
	}
}

// The type a request names is judged before any of the file is read: a type
// that is not kept makes no upload and takes no bytes.
func TestADeclaredTypeThatIsNotKeptIsRefusedBeforeAnyByteIsAccepted(test *testing.T) {
	fixture := newFixture(test)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{
		"Content-Type": "image/svg+xml", "X-Photo-Client-ID": "svg", "Upload-Complete": "?0", "Upload-Length": "5000",
	}, []byte("<svg/>"))
	fixture.expect(got, http.StatusUnsupportedMediaType)
	if !strings.Contains(got.body, "image/svg+xml") {
		test.Fatal(got.body)
	}
	if uploads := fixture.count("photo_upload"); uploads != "0" {
		test.Fatalf("a refused type made %s uploads", uploads)
	}
	if len(fixture.files.names()) != 0 {
		test.Fatalf("a refused type was stored: %v", fixture.files.names())
	}
	// A type that names none, or the plain one a program sends, is judged when
	// the file has arrived, by its name and its bytes.
	for _, contentType := range []string{"", "application/octet-stream"} {
		got := fixture.send(http.MethodPost, fixture.collectionUrl(), map[string]string{
			"Content-Type": contentType, "X-Photo-Client-ID": "named-" + contentType, "X-Photo-Filename": "IMG_0001.JPG",
		}, smallJpeg(test, 33))
		fixture.expect(got, http.StatusCreated)
	}
}

// A part that has a receipt and has changed since, or is gone, ends the upload
// at completion: the client is told to start again, and is not left retrying an
// answer that will never change.
func TestAPartThatNoLongerMatchesItsReceiptEndsTheUpload(test *testing.T) {
	for name, damage := range map[string]func(fixture *fixture, partName string){
		"overwritten": func(fixture *fixture, partName string) {
			if err := fixture.files.PutFileStream(test.Context(), partName, readSeeker(randomBytes(500, 99)), 500); err != nil {
				test.Fatal(err)
			}
		},
		"lost": func(fixture *fixture, partName string) {
			_ = fixture.files.DeleteFileEverywhere(test.Context(), partName)
		},
	} {
		test.Run(name, func(test *testing.T) {
			fixture := newFixture(test)
			created := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("damaged", "Upload-Complete", "?0"), randomBytes(500, 26))
			fixture.expect(created, http.StatusCreated)
			location := created.header.Get("Location")
			damage(fixture, models.PhotoUploadPartFileID(uploadIdOf(location), 0))
			finished := fixture.send(http.MethodPatch, location, map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": "500", "Upload-Complete": "?1"}, nil)
			fixture.expect(finished, http.StatusInternalServerError)
			if !strings.Contains(finished.body, "start the upload again") {
				test.Fatal(finished.body)
			}
			// It is over: asking where it is says it is gone, so the client
			// starts again, and nothing makes a photo.
			fixture.expect(fixture.send(http.MethodHead, location, nil, nil), http.StatusNotFound)
			if fixture.photoByClientId("damaged") != nil {
				test.Fatal("a damaged upload made a photo")
			}
		})
	}
}

// Bytes are stored before the transaction that records the file, and that
// transaction can fail. The blob's row is written at once, so the bytes are an
// unreferenced blob that cleanup finds, and not bytes nothing names.
func TestBytesStoredForAFileThatCouldNotBeRecordedAreFoundAndDeletedByCleanup(test *testing.T) {
	fixture := newFixture(test)
	fixture.configuration.Photos.MaxAccountBytes = config.ByteSize(1500)
	content := randomBytes(1000, 27)
	// It fits when it arrives, being the only thing there.
	started := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("too-late", "Upload-Complete", "?0"), content)
	fixture.expect(started, http.StatusCreated)
	// And then the account fills, so recording the file is refused.
	fixture.fillAccountWith("filler", 900)
	finished := fixture.send(http.MethodPatch, started.header.Get("Location"), map[string]string{"Content-Type": "application/partial-upload", "Upload-Offset": "1000", "Upload-Complete": "?1"}, nil)
	fixture.expect(finished, http.StatusRequestEntityTooLarge)
	if fixture.photoByClientId("too-late") != nil {
		test.Fatal("a refused file made a photo")
	}

	blobName := models.PhotoBlobFileID(digestOf(content))
	if !fixture.files.has(blobName) {
		test.Fatal("the bytes were not stored")
	}
	if rows := dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM photo_blob WHERE blob_sha256 = '"+digestOf(content)+"'"); rows != "1" {
		test.Fatalf("the stored bytes have %s rows to find them by", rows)
	}
	// Not before it is an hour old: a file may be about to name it.
	if err := fixture.store.Cleanup(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	if !fixture.files.has(blobName) {
		test.Fatal("a blob only just stored was deleted")
	}
	fixture.age("UPDATE photo_blob SET created_at = now() - interval '2 hours', unreferenced_since = CASE WHEN unreferenced_since IS NULL THEN NULL ELSE now() - interval '2 hours' END WHERE blob_sha256 = '" + digestOf(content) + "'")
	if err := fixture.store.Cleanup(test.Context(), 100); err != nil {
		test.Fatal(err)
	}
	if fixture.files.has(blobName) {
		test.Fatal("the bytes of a file that was never recorded are still in the store")
	}
	if rows := dbtest.QueryString(test, fixture.database, "SELECT count(*) FROM photo_blob WHERE blob_sha256 = '"+digestOf(content)+"'"); rows != "0" {
		test.Fatalf("%s blob rows are left", rows)
	}
}
