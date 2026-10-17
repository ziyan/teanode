package db_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// givePhotoASmallPreview records that the photo has a small preview, which is
// what lets an album be shown by it.
func givePhotoASmallPreview(test *testing.T, database db.Database, userId, photoId string) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.UpsertPhotoPreview(userId, &models.PhotoPreview{PhotoID: photoId, PhotoPreviewSize: models.PhotoPreviewSmall, ContentType: "image/jpeg", PreviewSizeBytes: 10}); err != nil {
			test.Fatal(err)
		}
	})
}

func photoIdsOf(photos []*models.Photo) []string {
	photoIds := make([]string, 0, len(photos))
	for _, candidate := range photos {
		photoIds = append(photoIds, candidate.ID)
	}
	return photoIds
}

func TestPhotoAlbumTitlesAreTrimmedAndBounded(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-title-owner")
	var albumId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "  Picnic by the lake \n", nil)
		if err != nil || album.AlbumTitle != "Picnic by the lake" || album.PhotoCount != 0 || album.FirstTakenAt != nil || album.LastTakenAt != nil || album.CoverPhotoID != nil {
			test.Fatalf("%+v %v", album, err)
		}
		albumId = album.ID
	})
	// 255 characters is the limit, counted in characters and not bytes.
	longest := strings.Repeat("é", 255)
	for _, scenario := range []struct {
		name  string
		title string
		isOK  bool
	}{{"empty", "", false}, {"only spaces", " \t ", false}, {"the longest", longest, true}, {"one too many", longest + "x", false}} {
		err := database.Transaction(func(transaction db.Transaction) error {
			if _, err := transaction.CreatePhotoAlbum(ownerId, scenario.title, nil); err != nil {
				return err
			}
			_, err := transaction.RenamePhotoAlbum(ownerId, albumId, scenario.title)
			return err
		})
		if scenario.isOK != (err == nil) {
			test.Fatalf("%s: %v", scenario.name, err)
		}
		if !scenario.isOK {
			var validation models.ValidationErrors
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), "255") {
				test.Fatalf("%s: the refusal does not name the limit: %v", scenario.name, err)
			}
		}
	}
}

func TestPhotoAlbumsAreTheOwnersAlone(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-owner")
	otherId := dbtest.CreateUser(test, database, "album-other")
	owned := newTestPhoto(test, database, ownerId, "owned", nil)
	foreign := newTestPhoto(test, database, otherId, "foreign", nil)
	var albumId, foreignAlbumId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "Mine", []string{owned.ID})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		foreignAlbum, err := transaction.CreatePhotoAlbum(otherId, "Theirs", []string{foreign.ID})
		if err != nil {
			test.Fatal(err)
		}
		foreignAlbumId = foreignAlbum.ID
	})

	failsAsNotFound := func(name string, change func(transaction db.Transaction) error) {
		test.Helper()
		if err := database.Transaction(change); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%s: %v", name, err)
		}
	}
	failsAsNotFound("reading another's album", func(transaction db.Transaction) error {
		_, err := transaction.GetPhotoAlbum(otherId, albumId)
		return err
	})
	failsAsNotFound("renaming another's album", func(transaction db.Transaction) error {
		_, err := transaction.RenamePhotoAlbum(otherId, albumId, "Stolen")
		return err
	})
	failsAsNotFound("deleting another's album", func(transaction db.Transaction) error {
		return transaction.DeletePhotoAlbum(otherId, albumId)
	})
	failsAsNotFound("adding to another's album", func(transaction db.Transaction) error {
		_, err := transaction.AddPhotosToAlbum(otherId, albumId, []string{foreign.ID})
		return err
	})
	failsAsNotFound("removing from another's album", func(transaction db.Transaction) error {
		_, err := transaction.RemovePhotosFromAlbum(otherId, albumId, []string{owned.ID})
		return err
	})
	failsAsNotFound("choosing the cover of another's album", func(transaction db.Transaction) error {
		_, err := transaction.SetPhotoAlbumCover(otherId, albumId, owned.ID)
		return err
	})
	failsAsNotFound("listing another's album", func(transaction db.Transaction) error {
		_, err := transaction.ListPhotos(otherId, &db.PhotoFilter{PageSize: 10, AlbumID: albumId})
		return err
	})
	failsAsNotFound("counting another's album", func(transaction db.Transaction) error {
		_, err := transaction.CountPhotos(otherId, &db.PhotoFilter{AlbumID: albumId})
		return err
	})
	failsAsNotFound("the albums of another's photo", func(transaction db.Transaction) error {
		_, err := transaction.ListPhotoAlbumsOfPhoto(otherId, owned.ID)
		return err
	})
	// Another's photo does not go in my album, and nothing is added of the
	// rest when one is not found.
	failsAsNotFound("adding another's photo", func(transaction db.Transaction) error {
		_, err := transaction.AddPhotosToAlbum(ownerId, albumId, []string{foreign.ID})
		return err
	})
	failsAsNotFound("making an album with another's photo", func(transaction db.Transaction) error {
		_, err := transaction.CreatePhotoAlbum(ownerId, "Borrowed", []string{owned.ID, foreign.ID})
		return err
	})
	failsAsNotFound("adding a photo that is not there", func(transaction db.Transaction) error {
		_, err := transaction.AddPhotosToAlbum(ownerId, albumId, []string{"no-such-photo"})
		return err
	})
	failsAsNotFound("an album that is not there", func(transaction db.Transaction) error {
		return transaction.DeletePhotoAlbum(ownerId, "no-such-album")
	})

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mine, err := transaction.ListPhotoAlbums(ownerId)
		if err != nil || len(mine) != 1 || mine[0].ID != albumId || mine[0].PhotoCount != 1 {
			test.Fatalf("the owner's albums: %+v %v", mine, err)
		}
		theirs, err := transaction.ListPhotoAlbums(otherId)
		if err != nil || len(theirs) != 1 || theirs[0].ID != foreignAlbumId || theirs[0].PhotoCount != 1 {
			test.Fatalf("the other's albums: %+v %v", theirs, err)
		}
		if titles, err := transaction.ListPhotoAlbumsOfPhoto(ownerId, owned.ID); err != nil || len(titles) != 1 || titles[0].AlbumTitle != "Mine" {
			test.Fatalf("%+v %v", titles, err)
		}
		// Nothing of the other account's got into the owner's album.
		if members, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, AlbumID: albumId}); err != nil || fmt.Sprint(photoIdsOf(members)) != fmt.Sprint([]string{owned.ID}) {
			test.Fatalf("%v %v", members, err)
		}
	})
}

func TestPhotoAlbumCountsDatesAndTheOrderOfPhotos(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-counts-owner")
	base := time.Date(2025, 7, 12, 10, 0, 0, 0, time.UTC)
	var photoIds []string
	for index := 0; index < 4; index++ {
		takenAt := base.Add(time.Duration(index) * 24 * time.Hour)
		photoIds = append(photoIds, newTestPhoto(test, database, ownerId, fmt.Sprintf("counted-%d", index), &takenAt).ID)
	}
	outside := newTestPhoto(test, database, ownerId, "outside", nil)
	var albumId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		// A photo named twice is in the album once.
		album, err := transaction.CreatePhotoAlbum(ownerId, "Trip", []string{photoIds[2], photoIds[0], photoIds[1], photoIds[3], photoIds[0]})
		if err != nil || album.PhotoCount != 4 {
			test.Fatalf("%+v %v", album, err)
		}
		albumId = album.ID
		if album.FirstTakenAt == nil || !album.FirstTakenAt.Equal(base) || album.LastTakenAt == nil || !album.LastTakenAt.Equal(base.Add(72*time.Hour)) {
			test.Fatalf("the dates are %v to %v", album.FirstTakenAt, album.LastTakenAt)
		}
		// The album lists its photos newest first, and counts what it lists.
		listed, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, AlbumID: albumId})
		if err != nil || fmt.Sprint(photoIdsOf(listed)) != fmt.Sprint([]string{photoIds[3], photoIds[2], photoIds[1], photoIds[0]}) {
			test.Fatalf("%v %v", photoIdsOf(listed), err)
		}
		if count, err := transaction.CountPhotos(ownerId, &db.PhotoFilter{AlbumID: albumId}); err != nil || count != 4 {
			test.Fatalf("counted %d: %v", count, err)
		}
		// The rest of the library is unchanged by there being an album.
		if count, err := transaction.CountPhotos(ownerId, &db.PhotoFilter{}); err != nil || count != 5 {
			test.Fatalf("counted %d: %v", count, err)
		}
		// An album's listing takes the other filters too.
		afterSecond := base.Add(36 * time.Hour)
		if listed, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, AlbumID: albumId, TakenAfter: &afterSecond}); err != nil || len(listed) != 2 {
			test.Fatalf("%v %v", photoIdsOf(listed), err)
		}
		if _, err := transaction.AddPhotosToAlbum(ownerId, albumId, []string{outside.ID}); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.GetPhotoAlbum(ownerId, albumId)
		if err != nil || album.PhotoCount != 5 {
			test.Fatalf("%+v %v", album, err)
		}
		// A photo with no date sits at the time it arrived, which is later
		// than the others.
		if album.LastTakenAt == nil || !album.LastTakenAt.After(base.Add(72*time.Hour)) {
			test.Fatalf("the last date is %v", album.LastTakenAt)
		}
	})
}

func TestAddingPhotosToAnAlbumIgnoresThoseAlreadyInIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-add-owner")
	first := newTestPhoto(test, database, ownerId, "first", nil)
	second := newTestPhoto(test, database, ownerId, "second", nil)
	third := newTestPhoto(test, database, ownerId, "third", nil)
	var albumId string
	var updatedAt time.Time
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "Adds", []string{first.ID})
		if err != nil {
			test.Fatal(err)
		}
		albumId, updatedAt = album.ID, album.UpdatedAt
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		added, err := transaction.AddPhotosToAlbum(ownerId, albumId, []string{first.ID, second.ID, second.ID})
		if err != nil || added != 1 {
			test.Fatalf("added %d: %v", added, err)
		}
		added, err = transaction.AddPhotosToAlbum(ownerId, albumId, []string{first.ID, second.ID})
		if err != nil || added != 0 {
			test.Fatalf("adding what is there added %d: %v", added, err)
		}
		if added, err = transaction.AddPhotosToAlbum(ownerId, albumId, nil); err != nil || added != 0 {
			test.Fatalf("adding nothing added %d: %v", added, err)
		}
		album, err := transaction.GetPhotoAlbum(ownerId, albumId)
		if err != nil || album.PhotoCount != 2 || !album.UpdatedAt.After(updatedAt) {
			test.Fatalf("%+v %v", album, err)
		}
		// A photo can be in several albums.
		other, err := transaction.CreatePhotoAlbum(ownerId, "Also", []string{first.ID, third.ID})
		if err != nil {
			test.Fatal(err)
		}
		albums, err := transaction.ListPhotoAlbumsOfPhoto(ownerId, first.ID)
		if err != nil || len(albums) != 2 || albums[0].AlbumTitle != "Adds" || albums[1].ID != other.ID {
			test.Fatalf("%+v %v", albums, err)
		}
		// More than a call may name is refused, and an empty name too.
		tooMany := make([]string, 501)
		for index := range tooMany {
			tooMany[index] = fmt.Sprintf("photo-%d", index)
		}
		if _, err := transaction.AddPhotosToAlbum(ownerId, albumId, tooMany); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("501 photos: %v", err)
		}
		if _, err := transaction.AddPhotosToAlbum(ownerId, albumId, []string{""}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("an empty identifier: %v", err)
		}
	})
}

func TestRemovingPhotosFromAnAlbumLeavesThemInTheLibrary(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-remove-owner")
	first := newTestPhoto(test, database, ownerId, "first", nil)
	second := newTestPhoto(test, database, ownerId, "second", nil)
	outside := newTestPhoto(test, database, ownerId, "outside", nil)
	givePhotoASmallPreview(test, database, ownerId, first.ID)
	givePhotoASmallPreview(test, database, ownerId, second.ID)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "Removals", []string{first.ID, second.ID})
		if err != nil {
			test.Fatal(err)
		}
		if album, err = transaction.SetPhotoAlbumCover(ownerId, album.ID, first.ID); err != nil || album.CoverPhotoID == nil || *album.CoverPhotoID != first.ID {
			test.Fatalf("%+v %v", album, err)
		}
		// Those not in the album are not counted; the cover going takes the
		// choice with it.
		removed, err := transaction.RemovePhotosFromAlbum(ownerId, album.ID, []string{first.ID, outside.ID, first.ID})
		if err != nil || removed != 1 {
			test.Fatalf("removed %d: %v", removed, err)
		}
		album, err = transaction.GetPhotoAlbum(ownerId, album.ID)
		if err != nil || album.PhotoCount != 1 || album.CoverPhotoID != nil || album.CoverPreviewPhotoID != second.ID {
			test.Fatalf("%+v %v", album, err)
		}
		if _, err := transaction.GetPhoto(ownerId, first.ID); err != nil {
			test.Fatalf("the photo went with the album: %v", err)
		}
		if albums, err := transaction.ListPhotoAlbumsOfPhoto(ownerId, first.ID); err != nil || len(albums) != 0 {
			test.Fatalf("%+v %v", albums, err)
		}
	})
}

func TestAnAlbumsCoverIsTheChosenPhotoElseTheNewestWithAPreview(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-cover-owner")
	base := time.Date(2025, 3, 1, 8, 0, 0, 0, time.UTC)
	var photos []*models.Photo
	for index := 0; index < 3; index++ {
		takenAt := base.Add(time.Duration(index) * time.Hour)
		photos = append(photos, newTestPhoto(test, database, ownerId, fmt.Sprintf("cover-%d", index), &takenAt))
	}
	outside := newTestPhoto(test, database, ownerId, "outside", nil)
	var albumId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "Covers", photoIdsOf(photos))
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		if album.CoverPreviewPhotoID != "" || album.CoverPreviewPhotoUpdatedAt != nil {
			test.Fatalf("a cover with no preview to make it from: %+v", album)
		}
	})
	cover := func() *models.PhotoAlbum {
		test.Helper()
		var album *models.PhotoAlbum
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			var err error
			if album, err = transaction.GetPhotoAlbum(ownerId, albumId); err != nil {
				test.Fatal(err)
			}
			// The listing says the same as the single reading.
			albums, err := transaction.ListPhotoAlbums(ownerId)
			if err != nil || len(albums) != 1 || albums[0].CoverPreviewPhotoID != album.CoverPreviewPhotoID {
				test.Fatalf("%+v %v", albums, err)
			}
		})
		return album
	}

	// The newest photo has no small preview yet, so the one before it is shown.
	givePhotoASmallPreview(test, database, ownerId, photos[1].ID)
	givePhotoASmallPreview(test, database, ownerId, photos[0].ID)
	if got := cover(); got.CoverPreviewPhotoID != photos[1].ID || got.CoverPreviewPhotoUpdatedAt == nil {
		test.Fatalf("the cover is %q", got.CoverPreviewPhotoID)
	}
	givePhotoASmallPreview(test, database, ownerId, photos[2].ID)
	if got := cover(); got.CoverPreviewPhotoID != photos[2].ID {
		test.Fatalf("the newest photo is not the cover: %q", got.CoverPreviewPhotoID)
	}

	// A chosen cover wins over the newest.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.SetPhotoAlbumCover(ownerId, albumId, photos[0].ID)
		if err != nil || album.CoverPhotoID == nil || *album.CoverPhotoID != photos[0].ID || album.CoverPreviewPhotoID != photos[0].ID {
			test.Fatalf("%+v %v", album, err)
		}
		// Only a photo of the album can be chosen.
		if _, err := transaction.SetPhotoAlbumCover(ownerId, albumId, outside.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a photo outside the album was chosen: %v", err)
		}
		if _, err := transaction.SetPhotoAlbumCover(ownerId, albumId, ""); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("no photo was chosen: %v", err)
		}
	})

	// While the chosen photo is in the trash the album falls back to the
	// newest, and the choice returns with the photo.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if trashed, err := transaction.TrashPhotos(ownerId, []string{photos[0].ID}); err != nil || trashed != 1 {
			test.Fatalf("%d %v", trashed, err)
		}
		if _, err := transaction.SetPhotoAlbumCover(ownerId, albumId, photos[0].ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a photo in the trash was chosen: %v", err)
		}
	})
	if got := cover(); got.CoverPreviewPhotoID != photos[2].ID || got.PhotoCount != 2 {
		test.Fatalf("with the chosen cover in the trash: %q, %d photos", got.CoverPreviewPhotoID, got.PhotoCount)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if restored, err := transaction.RestorePhotos(ownerId, []string{photos[0].ID}); err != nil || restored != 1 {
			test.Fatalf("%d %v", restored, err)
		}
	})
	if got := cover(); got.CoverPreviewPhotoID != photos[0].ID || got.PhotoCount != 3 {
		test.Fatalf("with the chosen cover restored: %q, %d photos", got.CoverPreviewPhotoID, got.PhotoCount)
	}
}

func TestAPhotoInTheTrashStaysInItsAlbumsButIsNotCountedOrListed(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-trash-owner")
	base := time.Date(2025, 5, 1, 8, 0, 0, 0, time.UTC)
	var photoIds []string
	for index := 0; index < 3; index++ {
		takenAt := base.Add(time.Duration(index) * time.Hour)
		photoIds = append(photoIds, newTestPhoto(test, database, ownerId, fmt.Sprintf("trashed-%d", index), &takenAt).ID)
	}
	var albumId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "Trash", photoIds)
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		if trashed, err := transaction.TrashPhotos(ownerId, []string{photoIds[2]}); err != nil || trashed != 1 {
			test.Fatalf("%d %v", trashed, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.GetPhotoAlbum(ownerId, albumId)
		if err != nil || album.PhotoCount != 2 || album.LastTakenAt == nil || !album.LastTakenAt.Equal(base.Add(time.Hour)) {
			test.Fatalf("%+v %v", album, err)
		}
		listed, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, AlbumID: albumId})
		if err != nil || fmt.Sprint(photoIdsOf(listed)) != fmt.Sprint([]string{photoIds[1], photoIds[0]}) {
			test.Fatalf("%v %v", photoIdsOf(listed), err)
		}
		// The trash view of the album is the photo that is in it.
		trashed, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, AlbumID: albumId, IsTrashed: true})
		if err != nil || fmt.Sprint(photoIdsOf(trashed)) != fmt.Sprint([]string{photoIds[2]}) {
			test.Fatalf("%v %v", photoIdsOf(trashed), err)
		}
		// It is still in the album when asked which albums it is in.
		if albums, err := transaction.ListPhotoAlbumsOfPhoto(ownerId, photoIds[2]); err != nil || len(albums) != 1 {
			test.Fatalf("%+v %v", albums, err)
		}
		if restored, err := transaction.RestorePhotos(ownerId, []string{photoIds[2]}); err != nil || restored != 1 {
			test.Fatalf("%d %v", restored, err)
		}
		album, err = transaction.GetPhotoAlbum(ownerId, albumId)
		if err != nil || album.PhotoCount != 3 {
			test.Fatalf("%+v %v", album, err)
		}
		// Deleted for good, a photo leaves its albums with the rest of it.
		if _, err := transaction.TrashPhotos(ownerId, []string{photoIds[0]}); err != nil {
			test.Fatal(err)
		}
		if marked, err := transaction.MarkPhotosForDeletion(ownerId, []string{photoIds[0]}); err != nil || marked != 1 {
			test.Fatalf("%d %v", marked, err)
		}
		if album, err = transaction.GetPhotoAlbum(ownerId, albumId); err != nil || album.PhotoCount != 2 {
			test.Fatalf("a photo being deleted is counted: %+v %v", album, err)
		}
		if deleted, err := transaction.DeleteMarkedPhotos([]string{photoIds[0]}); err != nil || deleted != 1 {
			test.Fatalf("%d %v", deleted, err)
		}
	})
	if count := dbtest.QueryString(test, database, fmt.Sprintf("SELECT count(*) FROM photo_album_item WHERE photo_album_id = '%s'", albumId)); count != "2" {
		test.Fatalf("%s photos are in the album after one was deleted", count)
	}
}

func TestDeletingAnAlbumLeavesItsPhotosAndAnAlbumsCoverGoesWithItsPhoto(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-delete-owner")
	kept := newTestPhoto(test, database, ownerId, "kept", nil)
	gone := newTestPhoto(test, database, ownerId, "gone", nil)
	var albumId, otherAlbumId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		album, err := transaction.CreatePhotoAlbum(ownerId, "Doomed", []string{kept.ID, gone.ID})
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		other, err := transaction.CreatePhotoAlbum(ownerId, "Survivor", []string{gone.ID})
		if err != nil {
			test.Fatal(err)
		}
		otherAlbumId = other.ID
		if _, err := transaction.SetPhotoAlbumCover(ownerId, other.ID, gone.ID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeletePhotoAlbum(ownerId, albumId); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.GetPhotoAlbum(ownerId, albumId); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("the album is still there: %v", err)
		}
		for _, photoId := range []string{kept.ID, gone.ID} {
			if _, err := transaction.GetPhoto(ownerId, photoId); err != nil {
				test.Fatalf("a photo went with the album: %v", err)
			}
		}
		if albums, err := transaction.ListPhotoAlbumsOfPhoto(ownerId, gone.ID); err != nil || len(albums) != 1 || albums[0].ID != otherAlbumId {
			test.Fatalf("%+v %v", albums, err)
		}
		// Deleting a photo for good leaves the album it chose as the cover
		// with no cover and not with a dangling one.
		if _, err := transaction.TrashPhotos(ownerId, []string{gone.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.MarkPhotosForDeletion(ownerId, []string{gone.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.DeleteMarkedPhotos([]string{gone.ID}); err != nil {
			test.Fatal(err)
		}
		album, err = transaction.GetPhotoAlbum(ownerId, otherAlbumId)
		if err != nil || album.CoverPhotoID != nil || album.PhotoCount != 0 {
			test.Fatalf("%+v %v", album, err)
		}
	})
}

func TestAlbumsAreListedMostRecentlyChangedFirst(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-order-owner")
	photo := newTestPhoto(test, database, ownerId, "ordered", nil)
	create := func(title string) string {
		var albumId string
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			album, err := transaction.CreatePhotoAlbum(ownerId, title, nil)
			if err != nil {
				test.Fatal(err)
			}
			albumId = album.ID
		})
		return albumId
	}
	firstId, secondId := create("First"), create("Second")
	create("Third")
	titles := func() string {
		var listed []string
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			albums, err := transaction.ListPhotoAlbums(ownerId)
			if err != nil {
				test.Fatal(err)
			}
			for _, album := range albums {
				listed = append(listed, album.AlbumTitle)
			}
		})
		return strings.Join(listed, ",")
	}
	if got := titles(); got != "Third,Second,First" {
		test.Fatalf("%s", got)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.AddPhotosToAlbum(ownerId, firstId, []string{photo.ID}); err != nil {
			test.Fatal(err)
		}
	})
	if got := titles(); got != "First,Third,Second" {
		test.Fatalf("adding a photo did not bring the album up: %s", got)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.RenamePhotoAlbum(ownerId, secondId, "Second, renamed"); err != nil {
			test.Fatal(err)
		}
	})
	if got := titles(); got != "Second, renamed,First,Third" {
		test.Fatalf("renaming did not bring the album up: %s", got)
	}
}

func TestPhotosInNoAlbumAreThoseDatedNotInTheTrashAndInNoAlbum(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "album-suggest-owner")
	otherId := dbtest.CreateUser(test, database, "album-suggest-other")
	base := time.Date(2025, 9, 1, 8, 0, 0, 0, time.UTC)
	at := func(hours int) *time.Time {
		moment := base.Add(time.Duration(hours) * time.Hour)
		return &moment
	}
	free := newTestPhoto(test, database, ownerId, "free", at(2))
	earlier := newTestPhoto(test, database, ownerId, "earlier", at(0))
	later := newTestPhoto(test, database, ownerId, "later", at(40))
	inAlbum := newTestPhoto(test, database, ownerId, "in-album", at(1))
	trashed := newTestPhoto(test, database, ownerId, "trashed", at(3))
	undated := newTestPhoto(test, database, ownerId, "undated", nil)
	newTestPhoto(test, database, otherId, "somebody-elses", at(2))
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.CreatePhotoAlbum(ownerId, "Taken", []string{inAlbum.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.TrashPhotos(ownerId, []string{trashed.ID}); err != nil {
			test.Fatal(err)
		}
		found, err := transaction.ListPhotosInNoAlbum(ownerId, nil, nil)
		if err != nil || fmt.Sprint(photoIdsOf(found)) != fmt.Sprint([]string{earlier.ID, free.ID, later.ID}) {
			test.Fatalf("%v %v (the undated photo is %s)", photoIdsOf(found), err, undated.ID)
		}
		if found[0].TakenAt == nil || !found[0].TakenAt.Equal(base) || found[0].UserID != ownerId {
			test.Fatalf("%+v", found[0])
		}
		after, before := *at(1), *at(40)
		if found, err = transaction.ListPhotosInNoAlbum(ownerId, &after, &before); err != nil || fmt.Sprint(photoIdsOf(found)) != fmt.Sprint([]string{free.ID}) {
			test.Fatalf("%v %v", photoIdsOf(found), err)
		}
		// Taking a photo out of its album frees it for a suggestion.
		if _, err := transaction.RemovePhotosFromAlbum(ownerId, mustFindAlbum(test, transaction, ownerId), []string{inAlbum.ID}); err != nil {
			test.Fatal(err)
		}
		if found, err = transaction.ListPhotosInNoAlbum(ownerId, nil, nil); err != nil || len(found) != 4 {
			test.Fatalf("%v %v", photoIdsOf(found), err)
		}
	})
}

func mustFindAlbum(test *testing.T, transaction db.Transaction, userId string) string {
	test.Helper()
	albums, err := transaction.ListPhotoAlbums(userId)
	if err != nil || len(albums) != 1 {
		test.Fatalf("%+v %v", albums, err)
	}
	return albums[0].ID
}
