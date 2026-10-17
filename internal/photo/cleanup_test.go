package photo_test

import (
	"net/http"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// age moves rows back in time, which is how a test makes an hour pass.
func (self *fixture) age(statement string) {
	self.test.Helper()
	dbtest.Exec(self.test, self.database, statement)
}

func TestCleanupDeletesAnUnreferencedBlobAndKeepsAReferencedOne(test *testing.T) {
	fixture := newFixture(test)
	kept := smallJpeg(test, 20)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("kept"), kept)
	fixture.expect(got, http.StatusCreated)

	// A blob nothing refers to: bytes stored, row recorded, no file.
	orphan := randomBytes(300, 20)
	orphanDigest := digestOf(orphan)
	if err := fixture.files.PutFileStream(test.Context(), models.PhotoBlobFileID(orphanDigest), readSeeker(orphan), int64(len(orphan))); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: orphanDigest, BlobSizeBytes: int64(len(orphan))}); err != nil {
			test.Fatal(err)
		}
	})

	// Younger than an hour, it is left alone: its file may be about to be recorded.
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if !fixture.files.has(models.PhotoBlobFileID(orphanDigest)) || fixture.count("photo_blob") != "2" {
		test.Fatal("a blob younger than an hour was deleted")
	}

	fixture.age("UPDATE photo_blob SET created_at = now() - interval '2 hours'")
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if fixture.files.has(models.PhotoBlobFileID(orphanDigest)) || fixture.count("photo_blob") != "1" {
		test.Fatal("an unreferenced blob was kept")
	}
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(kept))) {
		test.Fatal("a referenced blob was deleted")
	}
}

func TestCleanupDeletesAMarkedBlobsBytesAgainWhenAFileArrivesBeforeItsRowIsGone(test *testing.T) {
	// A crash after the bytes were deleted and before the row was leaves a
	// marked row with no bytes. A file with the same digest storing the bytes
	// again must not trust the row.
	fixture := newFixture(test)
	picture := smallJpeg(test, 30)
	first := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("first"), picture)
	fixture.expect(first, http.StatusCreated)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		photoId := first.header.Get("X-Photo-ID")
		if _, err := transaction.TrashPhotos(fixture.userId, []string{photoId}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.DeletePhotos(fixture.userId, []string{photoId}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.age("UPDATE photo_blob SET created_at = now() - interval '2 hours', deleting_at = now()")
	_ = fixture.files.DeleteFileEverywhere(test.Context(), models.PhotoBlobFileID(digestOf(picture)))

	second := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("second"), picture)
	fixture.expect(second, http.StatusCreated)
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(picture))) {
		test.Fatal("the bytes of a marked blob were trusted")
	}
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(picture))) || fixture.count("photo_blob") != "1" {
		test.Fatal("cleanup deleted a blob a file refers to")
	}
}

func TestCleanupDeletesPhotosPastTheTrashAndTheirPreviews(test *testing.T) {
	fixture := newFixture(test)
	old := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("old"), smallJpeg(test, 50))
	recent := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("recent"), smallJpeg(test, 60))
	fixture.expect(old, http.StatusCreated)
	fixture.expect(recent, http.StatusCreated)
	oldId, recentId := old.header.Get("X-Photo-ID"), recent.header.Get("X-Photo-ID")
	for _, photoId := range []string{oldId, recentId} {
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			if _, err := transaction.UpsertPhotoPreview(fixture.userId, &models.PhotoPreview{PhotoID: photoId, PhotoPreviewSize: models.PhotoPreviewSmall, ContentType: "image/jpeg", PreviewSizeBytes: 5}); err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.TrashPhotos(fixture.userId, []string{photoId}); err != nil {
				test.Fatal(err)
			}
		})
		if err := fixture.files.PutFileStream(test.Context(), models.PhotoPreviewFileID(photoId, models.PhotoPreviewSmall), readSeeker([]byte("small")), 5); err != nil {
			test.Fatal(err)
		}
	}
	fixture.age("UPDATE photo SET trashed_at = now() - interval '31 days' WHERE id = '" + oldId + "'")
	fixture.age("UPDATE photo SET trashed_at = now() - interval '29 days' WHERE id = '" + recentId + "'")
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if fixture.photoByClientId("old") != nil || fixture.photoByClientId("recent") == nil {
		test.Fatal("the trash retention is not 30 days")
	}
	if fixture.files.has(models.PhotoPreviewFileID(oldId, models.PhotoPreviewSmall)) || !fixture.files.has(models.PhotoPreviewFileID(recentId, models.PhotoPreviewSmall)) {
		test.Fatal("a deleted photo's preview was kept, or a kept photo's was deleted")
	}
	// Its file is no longer referenced, and goes with the blobs after the hour.
	if !fixture.files.has(models.PhotoBlobFileID(digestOf(smallJpeg(test, 50)))) {
		test.Fatal("the file of a deleted photo was deleted before the blobs' hour")
	}
	fixture.age("UPDATE photo_blob SET created_at = now() - interval '2 hours'")
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if fixture.files.has(models.PhotoBlobFileID(digestOf(smallJpeg(test, 50)))) || !fixture.files.has(models.PhotoBlobFileID(digestOf(smallJpeg(test, 60)))) {
		test.Fatal("the unreferenced file was kept, or the referenced one deleted")
	}
}

func TestCleanupDeletesTheStoredPartsOfFailedAndIdleUploads(test *testing.T) {
	fixture := newFixture(test)
	failed := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("failed", "Upload-Complete", "?0"), randomBytes(100, 30))
	idle := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("idle", "Upload-Complete", "?0"), randomBytes(100, 31))
	moving := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("moving", "Upload-Complete", "?0"), randomBytes(100, 32))
	fixture.expect(fixture.send(http.MethodDelete, failed.header.Get("Location"), nil, nil), http.StatusNoContent)
	idleId := idle.header.Get("Location")[len(idle.header.Get("Location"))-26:]
	fixture.age("UPDATE photo_upload SET updated_at = now() - interval '8 days' WHERE id = '" + idleId + "'")
	if len(fixture.files.names()) != 3 {
		test.Fatalf("each upload should hold a part: %v", fixture.files.names())
	}
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if names := fixture.files.names(); len(names) != 1 || fixture.count("photo_upload") != "1" || fixture.count("photo_upload_part") != "1" {
		test.Fatalf("what is left: %v, %s uploads", names, fixture.count("photo_upload"))
	}
	fixture.expect(fixture.send(http.MethodHead, moving.header.Get("Location"), nil, nil), http.StatusNoContent)
}

func TestProcessMarksWaitingPhotosReadyOnceAndTakesBackAStaleClaim(test *testing.T) {
	fixture := newFixture(test)
	got := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("waiting"), smallJpeg(test, 70))
	fixture.expect(got, http.StatusCreated)
	if fixture.photoByClientId("waiting").PhotoProcessingState != models.PhotoProcessingWaiting {
		test.Fatal("a new photo is not waiting")
	}
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	found := fixture.photoByClientId("waiting")
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingClaimedAt != nil {
		test.Fatalf("%+v", found)
	}

	// A claim made a moment ago is another instance's work in progress, and
	// one made eleven minutes ago is the work of an instance that has gone.
	second := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("claimed"), smallJpeg(test, 80))
	fixture.expect(second, http.StatusCreated)
	fixture.age("UPDATE photo SET processing_claimed_at = now() WHERE client_photo_id = 'claimed'")
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if fixture.photoByClientId("claimed").PhotoProcessingState != models.PhotoProcessingWaiting {
		test.Fatal("a photo another instance is working on was taken")
	}
	fixture.age("UPDATE photo SET processing_claimed_at = now() - interval '11 minutes' WHERE client_photo_id = 'claimed'")
	if err := fixture.store.Process(test.Context(), 4); err != nil {
		test.Fatal(err)
	}
	if fixture.photoByClientId("claimed").PhotoProcessingState != models.PhotoProcessingReady {
		test.Fatal("a stale claim was not taken again")
	}
}

func TestAnAttachmentIsSavedAsAPhotoNamedByItsDigest(test *testing.T) {
	fixture := newFixture(test)
	picture := smallJpeg(test, 90)
	attachment := &models.AgentAttachment{ID: "attachment-one", Name: "chart.jpg", ContentType: "image/jpeg", Size: int64(len(picture))}
	if err := fixture.files.PutFileStream(test.Context(), attachment.ID, readSeeker(picture), int64(len(picture))); err != nil {
		test.Fatal(err)
	}
	saved, err := fixture.store.SaveAttachment(test.Context(), fixture.userId, attachment)
	if err != nil {
		test.Fatal(err)
	}
	if saved.ClientPhotoID != digestOf(picture) || saved.PhotoSource != models.PhotoSourceConversation || saved.PhotoKind != models.PhotoKindPicture || saved.OriginalFilename != "chart.jpg" {
		test.Fatalf("%+v", saved)
	}
	again, err := fixture.store.SaveAttachment(test.Context(), fixture.userId, attachment)
	if err != nil || again.ID != saved.ID || fixture.count("photo") != "1" || fixture.count("photo_blob") != "1" {
		test.Fatalf("saving the same attachment twice: %v %v", again, err)
	}
	if !fixture.files.has(attachment.ID) {
		test.Fatal("the attachment itself was taken away")
	}

	document := &models.AgentAttachment{ID: "attachment-two", Name: "notes.pdf", ContentType: "application/pdf", Size: 20}
	_ = fixture.files.PutFileStream(test.Context(), document.ID, readSeeker([]byte("%PDF-1.4 12345678901")), 20)
	if _, err := fixture.store.SaveAttachment(test.Context(), fixture.userId, document); err == nil {
		test.Fatal("a document was saved as a photo")
	}
}

func TestDeletingPhotosRemovesOnlyThoseInTheTrashWithTheirPreviews(test *testing.T) {
	fixture := newFixture(test)
	trashed := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("trashed"), smallJpeg(test, 100))
	library := fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("library"), smallJpeg(test, 110))
	trashedId, libraryId := trashed.header.Get("X-Photo-ID"), library.header.Get("X-Photo-ID")
	if err := fixture.files.PutFileStream(test.Context(), models.PhotoPreviewFileID(trashedId, models.PhotoPreviewLarge), readSeeker([]byte("large")), 5); err != nil {
		test.Fatal(err)
	}
	err := fixture.database.Transaction(func(transaction db.Transaction) error {
		if _, err := transaction.UpsertPhotoPreview(fixture.userId, &models.PhotoPreview{PhotoID: trashedId, PhotoPreviewSize: models.PhotoPreviewLarge, ContentType: "image/jpeg", PreviewSizeBytes: 5}); err != nil {
			return err
		}
		if _, err := transaction.TrashPhotos(fixture.userId, []string{trashedId}); err != nil {
			return err
		}
		deletedCount, err := fixture.store.DeletePhotos(test.Context(), transaction, fixture.userId, []string{trashedId, libraryId})
		if err != nil || deletedCount != 1 {
			test.Fatalf("deleted %d: %v", deletedCount, err)
		}
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	if fixture.photoByClientId("trashed") != nil || fixture.photoByClientId("library") == nil || fixture.files.has(models.PhotoPreviewFileID(trashedId, models.PhotoPreviewLarge)) {
		test.Fatal("deleting forever did not keep to the trash")
	}

	// Emptying the trash takes everything in it and nothing else, however many.
	err = fixture.database.Transaction(func(transaction db.Transaction) error {
		for index := range 5 {
			created, _, err := transaction.CreatePhoto(&models.Photo{UserID: fixture.userId, ClientPhotoID: "bulk-" + string(rune('a'+index)), PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceDashboard})
			if err != nil {
				return err
			}
			if _, err := transaction.TrashPhotos(fixture.userId, []string{created.ID}); err != nil {
				return err
			}
		}
		emptiedCount, err := fixture.store.EmptyTrash(test.Context(), transaction, fixture.userId)
		if err != nil || emptiedCount != 5 {
			test.Fatalf("emptied %d: %v", emptiedCount, err)
		}
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	if fixture.count("photo") != "1" {
		test.Fatalf("%s photos are left", fixture.count("photo"))
	}
}
