package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

func photoDigest(label string) string {
	digest := sha256.Sum256([]byte(label))
	return hex.EncodeToString(digest[:])
}

// newTestPhoto stores a photo with a file in the original role, for tests
// that need a photo to be there.
func newTestPhoto(test *testing.T, database db.Database, userId, clientPhotoId string, takenAt *time.Time) *models.Photo {
	test.Helper()
	var photo *models.Photo
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		photo, _, err = transaction.CreatePhoto(&models.Photo{UserID: userId, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine, TakenAt: takenAt})
		if err != nil {
			test.Fatal(err)
		}
		digest := photoDigest("blob of " + clientPhotoId)
		if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digest, BlobSizeBytes: 100}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.SetPhotoFile(userId, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: digest, FileSizeBytes: 100, ContentType: "image/jpeg"}, 0); err != nil {
			test.Fatal(err)
		}
	})
	return photo
}

func TestPhotoListingOrderPagingAndOwner(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-owner")
	otherId := dbtest.CreateUser(test, database, "photo-other")
	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	var photoIds []string
	for index := 0; index < 5; index++ {
		takenAt := base.Add(time.Duration(index) * time.Hour)
		photoIds = append(photoIds, newTestPhoto(test, database, ownerId, fmt.Sprintf("photo-%d", index), &takenAt).ID)
	}
	// One with no date sits at the time it arrived, which is later than all
	// of those.
	undatedId := newTestPhoto(test, database, ownerId, "photo-undated", nil).ID
	newTestPhoto(test, database, otherId, "photo-of-somebody-else", nil)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		first, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3})
		if err != nil {
			test.Fatal(err)
		}
		if len(first) != 3 || first[0].ID != undatedId || first[1].ID != photoIds[4] || first[2].ID != photoIds[3] {
			test.Fatalf("the first page is out of order: %v", first)
		}
		second, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, BeforePhotoID: first[2].ID})
		if err != nil {
			test.Fatal(err)
		}
		if len(second) != 3 || second[0].ID != photoIds[2] || second[2].ID != photoIds[0] {
			test.Fatalf("the second page does not continue the first: %v", second)
		}
		// A page reached by number is the same page as one reached by cursor.
		byNumber, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, Offset: 3})
		if err != nil || len(byNumber) != 3 || byNumber[0].ID != second[0].ID || byNumber[2].ID != second[2].ID {
			test.Fatalf("the page at offset 3 is %v, %v", byNumber, err)
		}
		if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, Offset: 3, BeforePhotoID: first[2].ID}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("an offset with a cursor was accepted: %v", err)
		}
		for _, offset := range []int{-1, 100001} {
			if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, Offset: offset}); !errors.Is(err, db.ErrInvalidArguments) {
				test.Fatalf("offset %d was accepted: %v", offset, err)
			}
		}
		past, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, Offset: 100000})
		if err != nil || len(past) != 0 {
			test.Fatalf("a page past the end: %v %v", past, err)
		}
		// The count ignores paging, and is the account's own.
		if count, err := transaction.CountPhotos(ownerId, &db.PhotoFilter{PageSize: 1, Offset: 4}); err != nil || count != 6 {
			test.Fatalf("counted %d: %v", count, err)
		}
		if count, err := transaction.CountPhotos(otherId, &db.PhotoFilter{}); err != nil || count != 1 {
			test.Fatalf("the other account counted %d: %v", count, err)
		}
		if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, BeforePhotoID: "no-such-photo"}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a cursor that is not a photo of the account was accepted: %v", err)
		}
		if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 0}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a page of no size was accepted: %v", err)
		}
		if _, err := transaction.GetPhoto(otherId, photoIds[0]); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's photo was read: %v", err)
		}
		files, err := transaction.ListPhotoFiles(otherId, photoIds)
		if err != nil || len(files) != 0 {
			test.Fatalf("somebody else's files were listed: %v %v", files, err)
		}
		everyone, err := transaction.ListPhotos(otherId, &db.PhotoFilter{PageSize: 100})
		if err != nil || len(everyone) != 1 || everyone[0].ClientPhotoID != "photo-of-somebody-else" {
			test.Fatalf("the other account saw %v, %v", everyone, err)
		}
	})
}

func TestPhotoListingFilters(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-filter-owner")
	june := time.Date(2024, 6, 10, 9, 0, 0, 0, time.UTC)
	august := time.Date(2024, 8, 10, 9, 0, 0, 0, time.UTC)
	nearLatitude, nearLongitude := 10.0, 20.0
	farLatitude, farLongitude := 40.0, 20.0
	var nearId, farId, videoId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		near, _, err := transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "near", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone, TakenAt: &june, Latitude: &nearLatitude, Longitude: &nearLongitude, OriginalFilename: "IMG_0001.HEIC", CameraModel: "Example Phone"})
		if err != nil {
			test.Fatal(err)
		}
		far, _, err := transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "far", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone, TakenAt: &august, Latitude: &farLatitude, Longitude: &farLongitude, PlaceName: "Exampleville, Nowhere"})
		if err != nil {
			test.Fatal(err)
		}
		video, _, err := transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "video", PhotoKind: models.PhotoKindVideo, PhotoSource: models.PhotoSourceIPhone, TakenAt: &august})
		if err != nil {
			test.Fatal(err)
		}
		nearId, farId, videoId = near.ID, far.ID, video.ID
		if _, err := transaction.UpdatePhoto(ownerId, farId, &db.PhotoUpdate{IsFavorite: new(true)}); err != nil {
			test.Fatal(err)
		}
	})
	list := func(filter *db.PhotoFilter) []string {
		test.Helper()
		filter.PageSize = 100
		var ids []string
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			photos, err := transaction.ListPhotos(ownerId, filter)
			if err != nil {
				test.Fatal(err)
			}
			// The same filter counts what it lists.
			if count, err := transaction.CountPhotos(ownerId, filter); err != nil || count != len(photos) {
				test.Fatalf("the filter lists %d and counts %d: %v", len(photos), count, err)
			}
			for _, photo := range photos {
				ids = append(ids, photo.ID)
			}
		})
		return ids
	}
	same := func(name string, got []string, want ...string) {
		test.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			test.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
	same("favorites", list(&db.PhotoFilter{IsFavorite: new(true)}), farId)
	same("not favorites", list(&db.PhotoFilter{IsFavorite: new(false)}), videoId, nearId)
	same("videos", list(&db.PhotoFilter{PhotoKind: models.PhotoKindVideo}), videoId)
	afterJuly := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	same("after", list(&db.PhotoFilter{TakenAfter: &afterJuly}), videoId, farId)
	same("before", list(&db.PhotoFilter{TakenBefore: &afterJuly}), nearId)
	same("a word of the place", list(&db.PhotoFilter{SearchText: "exampleville"}), farId)
	same("words of the file and the camera", list(&db.PhotoFilter{SearchText: "img_0001 phone"}), nearId)
	same("a wildcard is not one", list(&db.PhotoFilter{SearchText: "%"}))
	distance := 50.0
	same("near", list(&db.PhotoFilter{NearLatitude: &nearLatitude, NearLongitude: &nearLongitude, NearDistanceKilometers: &distance}), nearId)
	wide := 4000.0
	same("far enough to reach both", list(&db.PhotoFilter{NearLatitude: &nearLatitude, NearLongitude: &nearLongitude, NearDistanceKilometers: &wide}), farId, nearId)
	err := database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, NearLatitude: &nearLatitude})
		return err
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("a point without a distance was accepted: %v", err)
	}
}

func TestPhotoCreateIsIdempotentPerUploaderName(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	firstId := dbtest.CreateUser(test, database, "photo-first")
	secondId := dbtest.CreateUser(test, database, "photo-second")
	var photoId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		created, isCreated, err := transaction.CreatePhoto(&models.Photo{UserID: firstId, ClientPhotoID: "stable", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceDashboard})
		if err != nil || !isCreated {
			test.Fatalf("not created: %v %v", isCreated, err)
		}
		photoId = created.ID
		if created.PhotoProcessingState != models.PhotoProcessingWaiting {
			test.Fatalf("a new photo is %s", created.PhotoProcessingState)
		}
		again, isCreated, err := transaction.CreatePhoto(&models.Photo{UserID: firstId, ClientPhotoID: "stable", PhotoKind: models.PhotoKindVideo, PhotoSource: models.PhotoSourceIPhone})
		if err != nil || isCreated || again.ID != photoId || again.PhotoKind != models.PhotoKindPicture {
			test.Fatalf("the same name made another photo: %v %v %v", again, isCreated, err)
		}
		other, isCreated, err := transaction.CreatePhoto(&models.Photo{UserID: secondId, ClientPhotoID: "stable", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceDashboard})
		if err != nil || !isCreated || other.ID == photoId {
			test.Fatalf("another account shares a name with this one: %v %v %v", other, isCreated, err)
		}
		found, err := transaction.FindPhotosByClientIDs(firstId, []string{"stable", "unknown"})
		if err != nil || len(found) != 1 || found[0].ID != photoId {
			test.Fatalf("found %v %v", found, err)
		}
	})
	err := database.Transaction(func(transaction db.Transaction) error {
		_, _, err := transaction.CreatePhoto(&models.Photo{UserID: firstId, ClientPhotoID: "bad", PhotoKind: "gif", PhotoSource: models.PhotoSourceDashboard})
		return err
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("an unknown kind was accepted: %v", err)
	}
}

func TestPhotoTrashRestoreAndDeleteOnlyTrashed(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-trash-owner")
	otherId := dbtest.CreateUser(test, database, "photo-trash-other")
	first := newTestPhoto(test, database, ownerId, "first", nil)
	second := newTestPhoto(test, database, ownerId, "second", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		deleted, err := transaction.DeletePhotos(ownerId, []string{first.ID})
		if err != nil || deleted != 0 {
			test.Fatalf("a photo that is not in the trash was deleted: %d %v", deleted, err)
		}
		trashed, err := transaction.TrashPhotos(otherId, []string{first.ID, second.ID})
		if err != nil || trashed != 0 {
			test.Fatalf("somebody else trashed photos: %d %v", trashed, err)
		}
		trashed, err = transaction.TrashPhotos(ownerId, []string{first.ID, second.ID, "no-such-photo"})
		if err != nil || trashed != 2 {
			test.Fatalf("trashed %d: %v", trashed, err)
		}
		trashed, err = transaction.TrashPhotos(ownerId, []string{first.ID})
		if err != nil || trashed != 0 {
			test.Fatalf("a photo was trashed twice: %d %v", trashed, err)
		}
		library, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10})
		if err != nil || len(library) != 0 {
			test.Fatalf("the library still shows %v", library)
		}
		inTrash, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, IsTrashed: true})
		if err != nil || len(inTrash) != 2 {
			test.Fatalf("the trash shows %v", inTrash)
		}
		counts, err := transaction.ReadPhotoLibraryCounts(ownerId)
		if err != nil || counts.TrashedCount != 2 || counts.PhotoCount != 0 {
			test.Fatalf("counts %+v %v", counts, err)
		}
		restored, err := transaction.RestorePhotos(ownerId, []string{first.ID})
		if err != nil || restored != 1 {
			test.Fatalf("restored %d: %v", restored, err)
		}
		deleted, err = transaction.DeletePhotos(ownerId, []string{first.ID, second.ID})
		if err != nil || deleted != 1 {
			test.Fatalf("deleted %d: %v", deleted, err)
		}
		if _, err := transaction.GetPhoto(ownerId, second.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a deleted photo is still there: %v", err)
		}
		files, err := transaction.ListPhotoFiles(ownerId, []string{second.ID})
		if err != nil || len(files) != 0 {
			test.Fatalf("the files of a deleted photo are still there: %v %v", files, err)
		}
		if usedBytes, err := transaction.PhotoUsedBytes(ownerId); err != nil || usedBytes != 100 {
			test.Fatalf("the account holds %d bytes after one photo was deleted: %v", usedBytes, err)
		}
	})
}

func TestPhotosTrashedBeforeATime(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-retention-owner")
	old := newTestPhoto(test, database, ownerId, "old", nil)
	recent := newTestPhoto(test, database, ownerId, "recent", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, []string{old.ID, recent.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo SET trashed_at = now() - interval '40 days' WHERE id = '"+old.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		expired, err := transaction.ListPhotosTrashedBefore(time.Now().Add(-30*24*time.Hour), 10)
		if err != nil || len(expired) != 1 || expired[0].ID != old.ID {
			test.Fatalf("expired %v %v", expired, err)
		}
	})
}

func TestPhotoFileQuotaReplacementAndProcessing(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-file-owner")
	photo := newTestPhoto(test, database, ownerId, "photo", nil)
	secondDigest := photoDigest("second file")
	liveDigest := photoDigest("live video")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		// The photo is ready, and a trashed photo that gets a new file stays
		// in the trash.
		if err := transaction.MarkPhotoReady(photo.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.TrashPhotos(ownerId, []string{photo.ID}); err != nil {
			test.Fatal(err)
		}
		for _, digest := range []string{secondDigest, liveDigest} {
			if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digest, BlobSizeBytes: 150}); err != nil {
				test.Fatal(err)
			}
		}
		// 100 bytes are held; replacing them with 150 fits in 200, and the
		// 100 being replaced are not counted twice.
		isChanged, err := transaction.SetPhotoFile(ownerId, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: secondDigest, FileSizeBytes: 150}, 200)
		if err != nil || !isChanged {
			test.Fatalf("a replacement that fits was refused: %v %v", isChanged, err)
		}
		again, err := transaction.SetPhotoFile(ownerId, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: secondDigest, FileSizeBytes: 150}, 200)
		if err != nil || again {
			test.Fatalf("the same file twice was a change: %v %v", again, err)
		}
		reloaded, err := transaction.GetPhoto(ownerId, photo.ID)
		if err != nil {
			test.Fatal(err)
		}
		if reloaded.TrashedAt == nil {
			test.Fatal("a file arriving took the photo out of the trash")
		}
		if reloaded.PhotoProcessingState != models.PhotoProcessingWaiting {
			test.Fatalf("a new original left the photo %s", reloaded.PhotoProcessingState)
		}
		_, err = transaction.SetPhotoFile(ownerId, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleLiveVideo, BlobSHA256: liveDigest, FileSizeBytes: 150}, 200)
		if !errors.Is(err, db.ErrTooMuchAsked) {
			test.Fatalf("a file over the account's limit was accepted: %v", err)
		}
	})
	// A refused file leaves nothing behind, and one that fits makes the photo
	// a Live Photo without queuing it again.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.MarkPhotoReady(photo.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.SetPhotoFile(ownerId, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleLiveVideo, BlobSHA256: liveDigest, FileSizeBytes: 150}, 0); err != nil {
			test.Fatal(err)
		}
		reloaded, err := transaction.GetPhoto(ownerId, photo.ID)
		if err != nil {
			test.Fatal(err)
		}
		if reloaded.PhotoKind != models.PhotoKindLivePhoto || reloaded.PhotoProcessingState != models.PhotoProcessingReady {
			test.Fatalf("after a live video: %s %s", reloaded.PhotoKind, reloaded.PhotoProcessingState)
		}
		if usedBytes, err := transaction.PhotoUsedBytes(ownerId); err != nil || usedBytes != 300 {
			test.Fatalf("used %d: %v", usedBytes, err)
		}
	})
	// A file whose blob is not recorded cannot be recorded.
	err := database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.SetPhotoFile(ownerId, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleEdited, BlobSHA256: photoDigest("never stored"), FileSizeBytes: 1}, 0)
		return err
	})
	if !errors.Is(err, db.ErrPhotoBlobGone) {
		test.Fatalf("a file with no blob was recorded: %v", err)
	}
}

func TestPhotoProcessingClaims(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-claim-owner")
	waiting := newTestPhoto(test, database, ownerId, "waiting", nil)
	var withoutPicture *models.Photo
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		withoutPicture, _, err = transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "live-video-first", PhotoKind: models.PhotoKindLivePhoto, PhotoSource: models.PhotoSourceIPhone})
		if err != nil {
			test.Fatal(err)
		}
		digest := photoDigest("only a video")
		if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digest, BlobSizeBytes: 5}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.SetPhotoFile(ownerId, withoutPicture.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleLiveVideo, BlobSHA256: digest, FileSizeBytes: 5}, 0); err != nil {
			test.Fatal(err)
		}
	})
	staleBefore := func() time.Time { return time.Now().Add(-10 * time.Minute) }
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore())
		if err != nil || len(claimed) != 1 || claimed[0].ID != waiting.ID {
			test.Fatalf("claimed %v %v", claimed, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore())
		if err != nil || len(claimed) != 0 {
			test.Fatalf("a claimed photo was claimed again: %v %v", claimed, err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo SET processing_claimed_at = now() - interval '11 minutes' WHERE id = '"+waiting.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore())
		if err != nil || len(claimed) != 1 {
			test.Fatalf("a claim that old was not taken again: %v %v", claimed, err)
		}
		for attempt := 1; attempt <= 5; attempt++ {
			if err := transaction.MarkPhotoProcessingFailed(waiting.ID, "cannot read it", 5); err != nil {
				test.Fatal(err)
			}
			reloaded, err := transaction.GetPhoto(ownerId, waiting.ID)
			if err != nil {
				test.Fatal(err)
			}
			wantState := models.PhotoProcessingWaiting
			if attempt == 5 {
				wantState = models.PhotoProcessingFailed
			}
			if reloaded.PhotoProcessingState != wantState || reloaded.ProcessingAttemptCount != attempt || reloaded.ProcessingErrorMessage != "cannot read it" {
				test.Fatalf("after failure %d: %+v", attempt, reloaded)
			}
		}
	})
}

func TestPhotoBlobLifecycle(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-blob-owner")
	referenced := newTestPhoto(test, database, ownerId, "keeper", nil)
	_ = referenced
	orphanDigest := photoDigest("orphan")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		isInserted, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: orphanDigest, BlobSizeBytes: 7})
		if err != nil || !isInserted {
			test.Fatalf("not inserted: %v %v", isInserted, err)
		}
		isInserted, err = transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: orphanDigest, BlobSizeBytes: 7})
		if err != nil || isInserted {
			test.Fatalf("a blob that was there was inserted: %v %v", isInserted, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour)); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a blob younger than the cutoff was picked: %v", err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_blob SET created_at = now() - interval '2 hours'")
	var marked *models.PhotoBlob
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		marked, err = transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour))
		if err != nil {
			test.Fatal(err)
		}
		if marked.BlobSHA256 != orphanDigest || marked.DeletingAt == nil {
			test.Fatalf("picked %+v, and the referenced blob must never be picked", marked)
		}
	})
	// A file arriving with the same digest while the mark is there clears it,
	// and says the row was there.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		isInserted, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: orphanDigest, BlobSizeBytes: 7})
		if err != nil || isInserted {
			test.Fatalf("%v %v", isInserted, err)
		}
		blob, err := transaction.GetPhotoBlob(orphanDigest)
		if err != nil || blob.DeletingAt != nil {
			test.Fatalf("the mark stayed: %+v %v", blob, err)
		}
	})
	// A blob marked, then referenced before the second transaction locks it,
	// is spared, and unmarked.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour)); err != nil {
			test.Fatal(err)
		}
	})
	late := newTestPhoto(test, database, ownerId, "late", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.SetPhotoFile(ownerId, late.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleEdited, BlobSHA256: orphanDigest, FileSizeBytes: 7}, 0); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		locked, err := transaction.LockPhotoBlobForDeletion(orphanDigest)
		if err != nil || locked != nil {
			test.Fatalf("a referenced blob was locked for deletion: %v %v", locked, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		blob, err := transaction.GetPhotoBlob(orphanDigest)
		if err != nil || blob.DeletingAt != nil {
			test.Fatalf("a spared blob is still marked: %+v %v", blob, err)
		}
	})
}

func TestPhotoBlobDeletionAfterTheLastFileGoes(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-blob-gone-owner")
	photo := newTestPhoto(test, database, ownerId, "photo", nil)
	digest := photoDigest("blob of photo")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, []string{photo.ID}); err != nil {
			test.Fatal(err)
		}
		if deleted, err := transaction.DeletePhotos(ownerId, []string{photo.ID}); err != nil || deleted != 1 {
			test.Fatalf("%d %v", deleted, err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_blob SET created_at = now() - interval '2 hours'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		marked, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour))
		if err != nil || marked.BlobSHA256 != digest {
			test.Fatalf("%+v %v", marked, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		blob, err := transaction.LockPhotoBlobForDeletion(digest)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeletePhotoBlob(blob.BlobSHA256); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.GetPhotoBlob(digest); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("the blob is still there: %v", err)
		}
	})
}

func TestPhotoPreviewsBelongToTheOwner(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-preview-owner")
	otherId := dbtest.CreateUser(test, database, "photo-preview-other")
	photo := newTestPhoto(test, database, ownerId, "photo", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, size := range []models.PhotoPreviewSize{models.PhotoPreviewSmall, models.PhotoPreviewLarge} {
			if _, err := transaction.UpsertPhotoPreview(ownerId, &models.PhotoPreview{PhotoID: photo.ID, PhotoPreviewSize: size, ContentType: "image/jpeg", PreviewSizeBytes: 10, WidthPixels: 40, HeightPixels: 30}); err != nil {
				test.Fatal(err)
			}
		}
		if _, err := transaction.UpsertPhotoPreview(ownerId, &models.PhotoPreview{PhotoID: photo.ID, PhotoPreviewSize: models.PhotoPreviewSmall, ContentType: "image/jpeg", PreviewSizeBytes: 12, WidthPixels: 40, HeightPixels: 30}); err != nil {
			test.Fatal(err)
		}
		previews, err := transaction.ListPhotoPreviews(ownerId, []string{photo.ID})
		if err != nil || len(previews) != 2 {
			test.Fatalf("%v %v", previews, err)
		}
		small, err := transaction.GetPhotoPreview(ownerId, photo.ID, models.PhotoPreviewSmall)
		if err != nil || small.PreviewSizeBytes != 12 {
			test.Fatalf("an upsert did not replace: %+v %v", small, err)
		}
		if _, err := transaction.GetPhotoPreview(otherId, photo.ID, models.PhotoPreviewSmall); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's preview was read: %v", err)
		}
		if _, err := transaction.UpsertPhotoPreview(otherId, &models.PhotoPreview{PhotoID: photo.ID, PhotoPreviewSize: models.PhotoPreviewVideo, ContentType: "video/mp4"}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a preview was made for somebody else's photo: %v", err)
		}
		if err := transaction.DeletePhotoPreview(ownerId, photo.ID, models.PhotoPreviewLarge); err != nil {
			test.Fatal(err)
		}
		previews, err = transaction.ListPhotoPreviews(ownerId, []string{photo.ID})
		if err != nil || len(previews) != 1 {
			test.Fatalf("%v %v", previews, err)
		}
	})
}

// The reverse of the photo migration undoes all of it, and the forward runs
// again over what it left: the downgrade the migration runner relies on.
func TestPhotoMigrationReverses(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0220_photo" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("the photo migration is missing")
	}
	// The built-in roles exist once a server has started; a made-up one stands
	// beside them to show the grant is theirs alone.
	for _, name := range []string{"Administrator", "Operator", "Member", "Guest"} {
		dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO role (id, created_at, modified_at, name) VALUES ('role-%s', now(), now(), '%s')`, strings.ToLower(name), name))
	}
	dbtest.Exec(test, database, migration.ReverseSQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo%'"); tables != "0" {
		test.Fatalf("%s photo tables survive the reverse", tables)
	}
	if columns := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'sync_credential' AND column_name = 'has_photos_access'"); columns != "0" {
		test.Fatal("the device password column survives the reverse")
	}
	if grants := dbtest.QueryString(test, database, "SELECT count(*) FROM role_permission WHERE permission_key = 'photos:use'"); grants != "0" {
		test.Fatal("the photos:use grants survive the reverse")
	}
	dbtest.Exec(test, database, migration.SQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo%'"); tables != "6" {
		test.Fatalf("%s photo tables after running it again", tables)
	}
	if granted := dbtest.QueryString(test, database, `SELECT string_agg(role_id, ',' ORDER BY role_id) FROM role_permission WHERE permission_key = 'photos:use'`); granted != "role-administrator,role-member,role-operator" {
		test.Fatalf("photos:use is granted to %q", granted)
	}
}
