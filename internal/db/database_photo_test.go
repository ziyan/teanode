package db_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
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
	// An hour after, so the order of the two August photos is the dates' and
	// not that of identifiers made in the same millisecond.
	augustLater := august.Add(time.Hour)
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
		video, _, err := transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "video", PhotoKind: models.PhotoKindVideo, PhotoSource: models.PhotoSourceIPhone, TakenAt: &augustLater})
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
		deleted, err := transaction.MarkPhotosForDeletion(ownerId, []string{first.ID})
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
		deleted, err = transaction.MarkPhotosForDeletion(ownerId, []string{first.ID, second.ID})
		if err != nil || deleted != 1 {
			test.Fatalf("deleted %d: %v", deleted, err)
		}
		// Deleting forever is at once as far as anybody can tell: the photo
		// is neither readable, listed, counted, restorable nor findable by
		// the uploader's name, and does not count against the quota.
		if _, err := transaction.GetPhoto(ownerId, second.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a deleted photo is still there: %v", err)
		}
		if inTrash, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, IsTrashed: true}); err != nil || len(inTrash) != 0 {
			test.Fatalf("the trash shows %v %v", inTrash, err)
		}
		if counts, err := transaction.ReadPhotoLibraryCounts(ownerId); err != nil || counts.TrashedCount != 0 {
			test.Fatalf("counts %+v %v", counts, err)
		}
		if restored, err := transaction.RestorePhotos(ownerId, []string{second.ID}); err != nil || restored != 0 {
			test.Fatalf("a deleted photo was restored: %d %v", restored, err)
		}
		if found, err := transaction.FindPhotosByClientIDs(ownerId, []string{"second"}); err != nil || len(found) != 0 {
			test.Fatalf("a deleted photo is found by its name: %v %v", found, err)
		}
		if usedBytes, err := transaction.PhotoUsedBytes(ownerId); err != nil || usedBytes != 100 {
			test.Fatalf("the account holds %d bytes after one photo was deleted: %v", usedBytes, err)
		}
		// Cleanup deletes the rows later, and only those marked.
		claimed, err := transaction.ClaimPhotosForDeletion(time.Time{}, 10)
		if err != nil || len(claimed) != 1 || claimed[0].ID != second.ID {
			test.Fatalf("claimed %v %v", claimed, err)
		}
		if gone, err := transaction.DeleteMarkedPhotos([]string{first.ID, second.ID}); err != nil || gone != 1 {
			test.Fatalf("deleted %d rows: %v", gone, err)
		}
		files, err := transaction.ListPhotoFiles(ownerId, []string{second.ID})
		if err != nil || len(files) != 0 {
			test.Fatalf("the files of a deleted photo are still there: %v %v", files, err)
		}
	})
}

func TestPhotoTrashIsListedNewestTrashedFirstAcrossPages(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-trash-order-owner")
	// The dates taken run the opposite way to the days moved to the Trash, and
	// two photos were moved at the same moment, so that the order has to break
	// that tie by id.
	takenLongAgo := time.Date(2010, 1, 1, 12, 0, 0, 0, time.UTC)
	takenRecently := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	oldest := newTestPhoto(test, database, ownerId, "trashed-first", &takenRecently)
	middle := newTestPhoto(test, database, ownerId, "trashed-second", &takenLongAgo)
	sameMomentOne := newTestPhoto(test, database, ownerId, "trashed-third-a", &takenLongAgo)
	sameMomentTwo := newTestPhoto(test, database, ownerId, "trashed-third-b", &takenRecently)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if trashed, err := transaction.TrashPhotos(ownerId, []string{oldest.ID, middle.ID, sameMomentOne.ID, sameMomentTwo.ID}); err != nil || trashed != 4 {
			test.Fatalf("trashed %d: %v", trashed, err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo SET trashed_at = now() - interval '3 days' WHERE id = '"+oldest.ID+"'")
	dbtest.Exec(test, database, "UPDATE photo SET trashed_at = now() - interval '2 days' WHERE id = '"+middle.ID+"'")
	dbtest.Exec(test, database, "UPDATE photo SET trashed_at = now() - interval '1 day' WHERE id IN ('"+sameMomentOne.ID+"', '"+sameMomentTwo.ID+"')")
	expectedIds := []string{sameMomentOne.ID, sameMomentTwo.ID}
	sort.Sort(sort.Reverse(sort.StringSlice(expectedIds)))
	expectedIds = append(expectedIds, middle.ID, oldest.ID)

	idsOf := func(photos []*models.Photo) []string {
		ids := []string{}
		for _, photo := range photos {
			ids = append(ids, photo.ID)
		}
		return ids
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		whole, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, IsTrashed: true})
		if err != nil || !slices.Equal(idsOf(whole), expectedIds) {
			test.Fatalf("the trash is listed %v, want %v: %v", idsOf(whole), expectedIds, err)
		}
		// Paged by number and by the last photo of the previous page, the pages
		// join up in the same order.
		var byOffset, byCursor []string
		for offset := 0; offset < 4; offset += 3 {
			page, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, Offset: offset, IsTrashed: true})
			if err != nil {
				test.Fatal(err)
			}
			byOffset = append(byOffset, idsOf(page)...)
		}
		cursor := ""
		for pageIndex := 0; pageIndex < 2; pageIndex++ {
			page, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, BeforePhotoID: cursor, IsTrashed: true})
			if err != nil {
				test.Fatal(err)
			}
			byCursor = append(byCursor, idsOf(page)...)
			if len(page) > 0 {
				cursor = page[len(page)-1].ID
			}
		}
		if !slices.Equal(byOffset, expectedIds) || !slices.Equal(byCursor, expectedIds) {
			test.Fatalf("pages by number %v and by cursor %v, want %v", byOffset, byCursor, expectedIds)
		}
		// A photo that is not in the Trash is not a place to continue from.
		if _, err := transaction.RestorePhotos(ownerId, []string{middle.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 3, BeforePhotoID: middle.ID, IsTrashed: true}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("continuing after a restored photo gave %v", err)
		}
	})
}

func TestPhotosForDeletionAreThoseMarkedAndThoseTrashedBeforeATime(test *testing.T) {
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
		none, err := transaction.ClaimPhotosForDeletion(time.Time{}, 10)
		if err != nil || len(none) != 0 {
			test.Fatalf("without a time, only the marked are taken: %v %v", none, err)
		}
		expired, err := transaction.ClaimPhotosForDeletion(time.Now().Add(-30*24*time.Hour), 10)
		if err != nil || len(expired) != 1 || expired[0].ID != old.ID {
			test.Fatalf("expired %v %v", expired, err)
		}
		// Taking an expired photo marks it, so that a crash before its rows
		// are deleted is found again without the clock.
		again, err := transaction.ClaimPhotosForDeletion(time.Time{}, 10)
		if err != nil || len(again) != 1 || again[0].ID != old.ID {
			test.Fatalf("the claimed photo is not marked: %v %v", again, err)
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
	dbtest.MarkPhotoReady(test, database, photo.ID)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		// The photo is ready, and a trashed photo that gets a new file stays
		// in the trash.
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
	dbtest.MarkPhotoReady(test, database, photo.ID)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
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
		claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore(), 5, false)
		if err != nil || len(claimed) != 1 || claimed[0].ID != waiting.ID {
			test.Fatalf("claimed %v %v", claimed, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore(), 5, false)
		if err != nil || len(claimed) != 0 {
			test.Fatalf("a claimed photo was claimed again: %v %v", claimed, err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo SET processing_claimed_at = now() - interval '11 minutes' WHERE id = '"+waiting.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore(), 5, false)
		if err != nil || len(claimed) != 1 {
			test.Fatalf("a claim that old was not taken again: %v %v", claimed, err)
		}
		// Claiming is what counts an attempt: this is the second.
		if claimed[0].ProcessingAttemptCount != 2 {
			test.Fatalf("attempts %d, want 2", claimed[0].ProcessingAttemptCount)
		}
	})
}

// A failure that is the file's keeps the attempt that claiming counted, makes
// the photo wait a longer time after each, and fails it after the last. A
// failure that is not the file's gives the attempt back.
func TestAFailedPhotoWaitsOutABackoffAndRunsOutOfAttempts(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-backoff-owner")
	waiting := newTestPhoto(test, database, ownerId, "backoff", nil)
	staleBefore := func() time.Time { return time.Now().Add(-10 * time.Minute) }
	claim := func() []*models.Photo {
		var claimed []*models.Photo
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			var err error
			if claimed, err = transaction.ClaimPhotosForProcessing(10, staleBefore(), 5, false); err != nil {
				test.Fatal(err)
			}
		})
		return claimed
	}
	read := func() *models.Photo {
		var found *models.Photo
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			var err error
			if found, err = transaction.GetPhoto(ownerId, waiting.ID); err != nil {
				test.Fatal(err)
			}
		})
		return found
	}
	fail := func(isFileAtFault bool) {
		dbtest.MarkPhotoProcessingFailed(test, database, waiting.ID, "The picture could not be read.", isFileAtFault, 5)
	}
	// Waits are one, five, fifteen and then sixty minutes.
	for attempt, wantWait := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 0} {
		if claimed := claim(); len(claimed) != 1 || claimed[0].ProcessingAttemptCount != attempt+1 {
			test.Fatalf("attempt %d: claimed %+v", attempt+1, claimed)
		}
		fail(true)
		found := read()
		if attempt == 4 {
			if found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingAttemptCount != 5 || found.ProcessingNotBefore != nil || found.ProcessingClaimedAt != nil {
				test.Fatalf("after the last attempt: %+v", found)
			}
			break
		}
		if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingAttemptCount != attempt+1 || found.ProcessingErrorMessage != "The picture could not be read." || found.ProcessingNotBefore == nil {
			test.Fatalf("after attempt %d: %+v", attempt+1, found)
		}
		if wait := time.Until(*found.ProcessingNotBefore); wait < wantWait-10*time.Second || wait > wantWait+10*time.Second {
			test.Fatalf("after attempt %d the photo waits %s, want about %s", attempt+1, wait, wantWait)
		}
		// Not due yet, so not claimed; due, it is.
		if claimed := claim(); len(claimed) != 0 {
			test.Fatalf("a photo that waits out a backoff was claimed: %+v", claimed)
		}
		dbtest.Exec(test, database, "UPDATE photo SET processing_not_before = now() - interval '1 second' WHERE id = '"+waiting.ID+"'")
	}
	if claimed := claim(); len(claimed) != 0 {
		test.Fatalf("a failed photo was claimed: %+v", claimed)
	}

	// A failure of the store or the database says nothing of the file: the
	// attempt is given back, and the photo waits a minute.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if retried, err := transaction.RetryPhotoProcessing(ownerId, []string{waiting.ID}); err != nil || retried != 1 {
			test.Fatalf("retried %d: %v", retried, err)
		}
	})
	if claimed := claim(); len(claimed) != 1 || claimed[0].ProcessingAttemptCount != 1 {
		test.Fatalf("claimed %+v", claimed)
	}
	fail(false)
	found := read()
	if found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingAttemptCount != 0 || found.ProcessingNotBefore == nil || time.Until(*found.ProcessingNotBefore) > time.Minute+10*time.Second {
		test.Fatalf("after a failure that is not the file's: %+v", found)
	}
	// Ready clears the count and the wait.
	dbtest.MarkPhotoReady(test, database, waiting.ID)
	if found := read(); found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingAttemptCount != 0 || found.ProcessingNotBefore != nil || found.ProcessingErrorMessage != "" {
		test.Fatalf("after ready: %+v", found)
	}
}

// A photo that used all its attempts and is claimed again was never recorded
// as failing: the process died each time. It is failed, not returned.
func TestAPhotoThatKeepsKillingTheProcessRunsOutOfAttempts(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-killer-owner")
	killer := newTestPhoto(test, database, ownerId, "killer", nil)
	staleBefore := func() time.Time { return time.Now().Add(-10 * time.Minute) }
	for attempt := 1; attempt <= 5; attempt++ {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			claimed, err := transaction.ClaimPhotosForProcessing(10, staleBefore(), 5, false)
			if err != nil || len(claimed) != 1 || claimed[0].ProcessingAttemptCount != attempt {
				test.Fatalf("attempt %d: claimed %+v %v", attempt, claimed, err)
			}
		})
		// The process died, and the claim ran out.
		dbtest.Exec(test, database, "UPDATE photo SET processing_claimed_at = now() - interval '11 minutes' WHERE id = '"+killer.ID+"'")
	}
	// A photo that waits behind it is claimed in the same call, the one that
	// failed not counting towards the limit.
	bystander := newTestPhoto(test, database, ownerId, "bystander", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		claimed, err := transaction.ClaimPhotosForProcessing(1, staleBefore(), 5, false)
		if err != nil || len(claimed) != 1 || claimed[0].ID != bystander.ID {
			test.Fatalf("a photo with no attempts left was claimed, or the one behind it was missed: %+v %v", claimed, err)
		}
		found, err := transaction.GetPhoto(ownerId, killer.ID)
		if err != nil || found.PhotoProcessingState != models.PhotoProcessingFailed || found.ProcessingErrorMessage == "" || found.ProcessingClaimedAt != nil {
			test.Fatalf("%+v %v", found, err)
		}
	})
}

func TestRetryingPhotoProcessingMovesFailedAndReadyPhotosBackToWaiting(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-retry-owner")
	otherId := dbtest.CreateUser(test, database, "photo-retry-other")
	failed := newTestPhoto(test, database, ownerId, "failed", nil)
	ready := newTestPhoto(test, database, ownerId, "ready", nil)
	waiting := newTestPhoto(test, database, ownerId, "waiting", nil)
	trashed := newTestPhoto(test, database, ownerId, "trashed", nil)
	foreign := newTestPhoto(test, database, otherId, "foreign", nil)
	dbtest.Exec(test, database, "UPDATE photo SET photo_processing_state = 'failed', processing_attempt_count = 5, processing_error_message = 'The picture could not be read.', processing_not_before = now() + interval '1 hour' WHERE id = '"+failed.ID+"'")
	dbtest.Exec(test, database, "UPDATE photo SET photo_processing_state = 'ready' WHERE id IN ('"+ready.ID+"', '"+trashed.ID+"', '"+foreign.ID+"')")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if trashedCount, err := transaction.TrashPhotos(ownerId, []string{trashed.ID}); err != nil || trashedCount != 1 {
			test.Fatalf("trashed %d: %v", trashedCount, err)
		}
		everyone := []string{failed.ID, ready.ID, waiting.ID, trashed.ID, foreign.ID}
		retried, err := transaction.RetryPhotoProcessing(ownerId, everyone)
		if err != nil || retried != 2 {
			test.Fatalf("retried %d, want the failed one and the ready one: %v", retried, err)
		}
		for _, photoId := range []string{failed.ID, ready.ID} {
			found, err := transaction.GetPhoto(ownerId, photoId)
			if err != nil || found.PhotoProcessingState != models.PhotoProcessingWaiting || found.ProcessingAttemptCount != 0 || found.ProcessingErrorMessage != "" || found.ProcessingNotBefore != nil {
				test.Fatalf("%s: %+v %v", photoId, found, err)
			}
		}
		if again, err := transaction.RetryPhotoProcessing(ownerId, everyone); err != nil || again != 0 {
			test.Fatalf("photos that were waiting were retried: %d %v", again, err)
		}
		if _, err := transaction.RetryPhotoProcessing(ownerId, make([]string, 501)); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("501 photos: %v", err)
		}
		if retried, err := transaction.RetryPhotoProcessing(ownerId, nil); err != nil || retried != 0 {
			test.Fatalf("nothing named: %d %v", retried, err)
		}
	})
	// The other person's photo is untouched.
	if state := dbtest.QueryString(test, database, "SELECT photo_processing_state FROM photo WHERE id = '"+foreign.ID+"'"); state != "ready" {
		test.Fatalf("somebody else's photo is %s", state)
	}
	if state := dbtest.QueryString(test, database, "SELECT photo_processing_state FROM photo WHERE id = '"+trashed.ID+"'"); state != "ready" {
		test.Fatalf("a photo in the trash is %s", state)
	}
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
		if _, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour), nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a blob younger than the cutoff was picked: %v", err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_blob SET created_at = now() - interval '2 hours', unreferenced_since = CASE WHEN unreferenced_since IS NULL THEN NULL ELSE now() - interval '2 hours' END")
	var marked *models.PhotoBlob
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		marked, err = transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour), nil)
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
		// Unmarking gives it the age of a blob stored just now, so that cleanup
		// does not mark it again before the file naming it is recorded.
		if time.Since(blob.CreatedAt) > time.Minute {
			test.Fatalf("an unmarked blob kept its age: %v", blob.CreatedAt)
		}
		if _, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour), nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a blob just stored was picked again: %v", err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_blob SET created_at = now() - interval '2 hours', unreferenced_since = CASE WHEN unreferenced_since IS NULL THEN NULL ELSE now() - interval '2 hours' END")
	// A blob marked, then referenced before the second transaction locks it,
	// is spared, and unmarked.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour), nil); err != nil {
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
		if deleted, err := transaction.MarkPhotosForDeletion(ownerId, []string{photo.ID}); err != nil || deleted != 1 {
			test.Fatalf("%d %v", deleted, err)
		}
		if deleted, err := transaction.DeleteMarkedPhotos([]string{photo.ID}); err != nil || deleted != 1 {
			test.Fatalf("%d %v", deleted, err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_blob SET created_at = now() - interval '2 hours', unreferenced_since = CASE WHEN unreferenced_since IS NULL THEN NULL ELSE now() - interval '2 hours' END")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		marked, err := transaction.MarkUnreferencedPhotoBlobDeleting(time.Now().Add(-time.Hour), nil)
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
	var migration, albumMigration, descriptionMigration, faceMigration, shareMigration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		switch candidate.ID {
		case "0220_photo":
			migration = candidate
		case "0221_photo_album":
			albumMigration = candidate
		case "0222_photo_description":
			descriptionMigration = candidate
		case "0223_photo_face":
			faceMigration = candidate
		case "0224_photo_album_share":
			shareMigration = candidate
		}
	}
	if migration.ID == "" || albumMigration.ID == "" || descriptionMigration.ID == "" || faceMigration.ID == "" || shareMigration.ID == "" {
		test.Fatal("the photo migrations are missing")
	}
	// The built-in roles exist once a server has started; a made-up one stands
	// beside them to show the grant is theirs alone.
	for _, name := range []string{"Administrator", "Operator", "Member", "Guest"} {
		dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO role (id, created_at, modified_at, name) VALUES ('role-%s', now(), now(), '%s')`, strings.ToLower(name), name))
	}
	// The faces, the descriptions and the albums refer to the photos, so they
	// go first and come back last; the shares refer to the albums.
	dbtest.Exec(test, database, shareMigration.ReverseSQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name = 'photo_album_share'"); tables != "0" {
		test.Fatal("the album share table survives the reverse")
	}
	dbtest.Exec(test, database, faceMigration.ReverseSQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo_face%'"); tables != "0" {
		test.Fatalf("%s face tables survive the reverse", tables)
	}
	dbtest.Exec(test, database, descriptionMigration.ReverseSQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name = 'photo_vector'"); tables != "0" {
		test.Fatal("the photo vector table survives the reverse")
	}
	if columns := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.columns WHERE table_name = 'photo' AND column_name IN ('photo_description', 'description_visible_text', 'description_model', 'described_at', 'description_requested_at', 'search_document')"); columns != "0" {
		test.Fatalf("%s description columns survive the reverse", columns)
	}
	dbtest.Exec(test, database, albumMigration.ReverseSQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo_album%'"); tables != "0" {
		test.Fatalf("%s album tables survive the reverse", tables)
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
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo%'"); tables != "7" {
		test.Fatalf("%s photo tables after running it again", tables)
	}
	dbtest.Exec(test, database, albumMigration.SQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo_album%'"); tables != "2" {
		test.Fatalf("%s album tables after running it again", tables)
	}
	if granted := dbtest.QueryString(test, database, `SELECT string_agg(role_id, ',' ORDER BY role_id) FROM role_permission WHERE permission_key = 'photos:use'`); granted != "role-administrator,role-member,role-operator" {
		test.Fatalf("photos:use is granted to %q", granted)
	}
	dbtest.Exec(test, database, descriptionMigration.SQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name = 'photo_vector'"); tables != "1" {
		test.Fatal("the photo vector table is missing after running it again")
	}
	dbtest.Exec(test, database, faceMigration.SQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name LIKE 'photo_face%'"); tables != "2" {
		test.Fatalf("%s face tables after running it again", tables)
	}
	dbtest.Exec(test, database, shareMigration.SQL)
	if tables := dbtest.QueryString(test, database, "SELECT count(*) FROM information_schema.tables WHERE table_name = 'photo_album_share'"); tables != "1" {
		test.Fatal("the album share table is missing after running it again")
	}
}

func TestPhotoMetadataKeepsWhatThePhotoAlreadyHas(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-metadata-owner")
	given := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	givenOffsetMinutes := 120
	givenLatitude, givenLongitude := 1.5, 2.5
	var photo *models.Photo
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		photo, _, err = transaction.CreatePhoto(&models.Photo{
			UserID: ownerId, ClientPhotoID: "given", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone,
			TakenAt: &given, TakenAtOffsetMinutes: &givenOffsetMinutes, Latitude: &givenLatitude, Longitude: &givenLongitude,
		})
		if err != nil {
			test.Fatal(err)
		}
	})
	fileTakenAt := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	fileOffsetMinutes := -300
	fileLatitude, fileLongitude := 10.0, 20.0
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		err := transaction.UpdatePhotoMetadata(photo.ID, &db.PhotoMetadata{
			PhotoKind: models.PhotoKindVideo, WidthPixels: 30, HeightPixels: 40, DurationMS: 2000,
			TakenAt: &fileTakenAt, TakenAtOffsetMinutes: &fileOffsetMinutes, Latitude: &fileLatitude, Longitude: &fileLongitude,
			PlaceName: "Somewhere", CameraMake: "Make", CameraModel: "Model",
		})
		if err != nil {
			test.Fatal(err)
		}
		found, err := transaction.GetPhoto(ownerId, photo.ID)
		if err != nil {
			test.Fatal(err)
		}
		if !found.TakenAt.Equal(given) || *found.TakenAtOffsetMinutes != 120 || *found.Latitude != 1.5 || *found.Longitude != 2.5 {
			test.Fatalf("what the uploader said was replaced: %+v", found)
		}
		if found.PhotoKind != models.PhotoKindVideo || found.WidthPixels != 30 || found.HeightPixels != 40 || found.DurationMS != 2000 ||
			found.PlaceName != "Somewhere" || found.CameraMake != "Make" || found.CameraModel != "Model" {
			test.Fatalf("what the file said was not kept: %+v", found)
		}
		// Saying nothing about the size leaves the size.
		if err := transaction.UpdatePhotoMetadata(photo.ID, &db.PhotoMetadata{PlaceName: "Elsewhere"}); err != nil {
			test.Fatal(err)
		}
		found, _ = transaction.GetPhoto(ownerId, photo.ID)
		if found.WidthPixels != 30 || found.PlaceName != "Somewhere" {
			test.Fatalf("an empty reading replaced what was known: %+v", found)
		}
		if err := transaction.UpdatePhotoMetadata("no-such-photo", &db.PhotoMetadata{}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a photo that is not there took metadata: %v", err)
		}
	})
	// A photo with nothing takes the file's date with its offset and place.
	empty := newTestPhoto(test, database, ownerId, "empty", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.UpdatePhotoMetadata(empty.ID, &db.PhotoMetadata{TakenAt: &fileTakenAt, TakenAtOffsetMinutes: &fileOffsetMinutes, Latitude: &fileLatitude, Longitude: &fileLongitude}); err != nil {
			test.Fatal(err)
		}
		found, _ := transaction.GetPhoto(ownerId, empty.ID)
		if found.TakenAt == nil || !found.TakenAt.Equal(fileTakenAt) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != -300 || found.Latitude == nil || *found.Latitude != 10.0 {
			test.Fatalf("the file's reading was not taken: %+v", found)
		}
	})
}

func TestReadyPhotosWithoutASmallPreviewAreQueuedAgain(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-requeue-owner")
	without := newTestPhoto(test, database, ownerId, "without", nil)
	with := newTestPhoto(test, database, ownerId, "with", nil)
	waiting := newTestPhoto(test, database, ownerId, "waiting", nil)
	for _, photoId := range []string{without.ID, with.ID} {
		dbtest.MarkPhotoReady(test, database, photoId)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.UpsertPhotoPreview(ownerId, &models.PhotoPreview{PhotoID: with.ID, PhotoPreviewSize: models.PhotoPreviewSmall, ContentType: "image/jpeg", PreviewSizeBytes: 3}); err != nil {
			test.Fatal(err)
		}
		requeued, err := transaction.RequeuePhotosWithoutPreviews(10)
		if err != nil || requeued != 1 {
			test.Fatalf("requeued %d: %v", requeued, err)
		}
		found, _ := transaction.GetPhoto(ownerId, without.ID)
		if found.PhotoProcessingState != models.PhotoProcessingWaiting {
			test.Fatalf("the photo is %s", found.PhotoProcessingState)
		}
		if found, _ = transaction.GetPhoto(ownerId, with.ID); found.PhotoProcessingState != models.PhotoProcessingReady {
			test.Fatal("a photo with its preview was queued again")
		}
		if found, _ = transaction.GetPhoto(ownerId, waiting.ID); found.PhotoProcessingState != models.PhotoProcessingWaiting {
			test.Fatal("a waiting photo changed")
		}
		if again, err := transaction.RequeuePhotosWithoutPreviews(10); err != nil || again != 0 {
			test.Fatalf("queued again %d: %v", again, err)
		}
	})
}

func TestPhotoMetadataCompletesAnInstantOnlyDateWithTheFilesOffsetOrTheOwnersZone(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-instant-owner")
	instant := time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC)
	fileOffsetMinutes := 120
	// The owner's zone is five hours east of UTC all year.
	ownerLocation := time.FixedZone("owner", 5*3600)
	// A photo the uploader dated with an instant and no offset takes the
	// offset of a file whose own date is the same moment.
	agreeing := newTestPhoto(test, database, ownerId, "agreeing", &instant)
	// The file's date is a different moment, as when the person changed the
	// date after the file was written: the file's offset is not theirs, and the
	// photo is given the offset of the owner's zone.
	moved := newTestPhoto(test, database, ownerId, "moved", &instant)
	// A date the file does not give is the same: the owner's zone.
	silent := newTestPhoto(test, database, ownerId, "silent", &instant)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		fileAgreeing := instant.Add(30 * time.Second)
		fileMoved := instant.Add(3 * time.Hour)
		for _, change := range []struct {
			photo    *models.Photo
			metadata db.PhotoMetadata
		}{
			{agreeing, db.PhotoMetadata{OwnerLocation: ownerLocation, TakenAt: &fileAgreeing, TakenAtOffsetMinutes: &fileOffsetMinutes}},
			{moved, db.PhotoMetadata{OwnerLocation: ownerLocation, TakenAt: &fileMoved, TakenAtOffsetMinutes: &fileOffsetMinutes}},
			{silent, db.PhotoMetadata{OwnerLocation: ownerLocation, PlaceName: "Somewhere"}},
		} {
			if err := transaction.UpdatePhotoMetadata(change.photo.ID, &change.metadata); err != nil {
				test.Fatal(err)
			}
		}
		for _, check := range []struct {
			photo      *models.Photo
			wantOffset int
		}{{agreeing, 120}, {moved, 300}, {silent, 300}} {
			found, err := transaction.GetPhoto(ownerId, check.photo.ID)
			if err != nil {
				test.Fatal(err)
			}
			if found.TakenAt == nil || !found.TakenAt.Equal(instant) {
				test.Fatalf("%s: the uploader's instant was replaced: %v", check.photo.ClientPhotoID, found.TakenAt)
			}
			if found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != check.wantOffset {
				test.Fatalf("%s: offset %v, want %d", check.photo.ClientPhotoID, found.TakenAtOffsetMinutes, check.wantOffset)
			}
		}
	})
}

// A date the file wrote as a clock reading has no moment in it, and is read on
// the clock of the owner's zone; a video's creation time is an instant. Either
// way the photo gets an instant and an offset.
func TestPhotoMetadataReadsAWallClockInTheOwnersZone(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-wall-clock-owner")
	ownerLocation := time.FixedZone("owner", -(3*3600 + 30*60))
	// 07:08:09 on the owner's clock, which is 3 hours and 30 minutes behind UTC.
	wallClock := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	wantInstant := time.Date(2024, 5, 6, 10, 38, 9, 0, time.UTC)
	picture := newTestPhoto(test, database, ownerId, "wall-clock", nil)
	video := newTestPhoto(test, database, ownerId, "instant", nil)
	plain := newTestPhoto(test, database, ownerId, "plain", nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, change := range []struct {
			photo    *models.Photo
			metadata db.PhotoMetadata
		}{
			{picture, db.PhotoMetadata{OwnerLocation: ownerLocation, TakenAt: &wallClock, IsTakenAtWallClock: true}},
			{video, db.PhotoMetadata{OwnerLocation: ownerLocation, TakenAt: &wallClock}},
			{plain, db.PhotoMetadata{OwnerLocation: ownerLocation, PlaceName: "Somewhere"}},
		} {
			if err := transaction.UpdatePhotoMetadata(change.photo.ID, &change.metadata); err != nil {
				test.Fatal(err)
			}
		}
		found, err := transaction.GetPhoto(ownerId, picture.ID)
		if err != nil || found.TakenAt == nil || !found.TakenAt.Equal(wantInstant) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != -210 {
			test.Fatalf("a wall clock: %+v %v", found, err)
		}
		if found, err = transaction.GetPhoto(ownerId, video.ID); err != nil || found.TakenAt == nil || !found.TakenAt.Equal(wallClock) || found.TakenAtOffsetMinutes == nil || *found.TakenAtOffsetMinutes != -210 {
			test.Fatalf("an instant with no offset: %+v %v", found, err)
		}
		if found, err = transaction.GetPhoto(ownerId, plain.ID); err != nil || found.TakenAt != nil || found.TakenAtOffsetMinutes != nil {
			test.Fatalf("a photo with no date was given one: %+v %v", found, err)
		}
	})
}

func TestCorrectingAPhotosDateReplacesItsOffsetWithTheOneThatCameWithIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-correct-owner")
	instant := time.Date(2024, 5, 6, 5, 8, 9, 0, time.UTC)
	offsetMinutes := 120
	var photo *models.Photo
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		photo, _, err = transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "corrected", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone, TakenAt: &instant, TakenAtOffsetMinutes: &offsetMinutes})
		if err != nil {
			test.Fatal(err)
		}
		corrected := time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
		westMinutes := -480
		updated, err := transaction.UpdatePhoto(ownerId, photo.ID, &db.PhotoUpdate{TakenAt: &corrected, TakenAtOffsetMinutes: &westMinutes})
		if err != nil || updated.TakenAt == nil || !updated.TakenAt.Equal(corrected) || updated.TakenAtOffsetMinutes == nil || *updated.TakenAtOffsetMinutes != -480 {
			test.Fatalf("the date came with an offset: %+v %v", updated, err)
		}
		// Changing something else leaves both.
		isFavorite := true
		if updated, err = transaction.UpdatePhoto(ownerId, photo.ID, &db.PhotoUpdate{IsFavorite: &isFavorite}); err != nil || !updated.TakenAt.Equal(corrected) || *updated.TakenAtOffsetMinutes != -480 {
			test.Fatalf("a favorite changed the date: %+v %v", updated, err)
		}
		// A date that came with no offset does not keep the old one.
		if updated, err = transaction.UpdatePhoto(ownerId, photo.ID, &db.PhotoUpdate{TakenAt: &instant}); err != nil || updated.TakenAtOffsetMinutes != nil {
			test.Fatalf("the old offset stayed with a new date: %+v %v", updated, err)
		}
		if _, err := transaction.UpdatePhoto(ownerId, photo.ID, &db.PhotoUpdate{TakenAtOffsetMinutes: &offsetMinutes}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("an offset without a date: %v", err)
		}
	})
}
