package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// searchPhotoIds are the photos a search finds for a person, best first.
func searchPhotoIds(test *testing.T, database db.Database, userId, searchText string, meaning *db.PhotoSearchMeaning) []string {
	test.Helper()
	var photoIds []string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		photos, err := transaction.ListPhotos(userId, &db.PhotoFilter{PageSize: 50, SearchText: searchText, SearchMeaning: meaning})
		if err != nil {
			test.Fatal(err)
		}
		photoIds = photoIdsOf(photos)
	})
	return photoIds
}

func describePhoto(test *testing.T, database db.Database, userId, photoId, photoDescription, visibleText string) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoDescription(userId, photoId, &db.PhotoDescription{PhotoDescription: photoDescription, VisibleText: visibleText, ModelName: "test-model"}); err != nil {
			test.Fatal(err)
		}
	})
}

func equalIds(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestAPhotoIsFoundByAWordOfItsDescriptionOrTheWordsReadInIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-describer")
	otherId := dbtest.CreateUser(test, database, "photo-describer-other")
	taken := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	lighthouse := newTestPhoto(test, database, ownerId, "lighthouse", &taken)
	plain := newTestPhoto(test, database, ownerId, "plain", &taken)
	somebodyElses := newTestPhoto(test, database, otherId, "somebody-elses", &taken)

	if found := searchPhotoIds(test, database, ownerId, "lighthouse", nil); len(found) != 0 {
		test.Fatalf("a photo nobody described was found by what is in it: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.RequestPhotoDescription(ownerId, lighthouse.ID); err != nil {
			test.Fatal(err)
		}
		requested, err := transaction.GetPhoto(ownerId, lighthouse.ID)
		if err != nil || !requested.IsDescriptionRequested || requested.DescribedAt != nil {
			test.Fatalf("a request is pending until the description is written: %+v %v", requested, err)
		}
		if err := transaction.RequestPhotoDescription(ownerId, somebodyElses.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a photo of somebody else's was requested: %v", err)
		}
		undescribed, err := transaction.CountUndescribedPhotos(ownerId, &db.PhotoFilter{})
		if err != nil || undescribed != 2 {
			test.Fatalf("both photos are undescribed: %d %v", undescribed, err)
		}
	})

	describePhoto(test, database, ownerId, lighthouse.ID, "A red lighthouse on a rocky point under a clear sky.", "HARBOR ENTRANCE")
	describePhoto(test, database, otherId, somebodyElses.ID, "A red lighthouse in somebody else's library.", "")

	for _, searchText := range []string{"lighthouse", "Lighthouse", "red lighthouse", "harbor", "entrance"} {
		if found := searchPhotoIds(test, database, ownerId, searchText, nil); !equalIds(found, []string{lighthouse.ID}) {
			test.Fatalf("%q finds %v, want the described photo alone", searchText, found)
		}
	}
	if found := searchPhotoIds(test, database, ownerId, "lighthouse bicycle", nil); len(found) != 0 {
		test.Fatalf("every word must be there: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		described, err := transaction.GetPhoto(ownerId, lighthouse.ID)
		if err != nil || described.IsDescriptionRequested || described.DescribedAt == nil || described.PhotoDescription == "" || described.DescriptionVisibleText != "HARBOR ENTRANCE" {
			test.Fatalf("writing the description clears the request and keeps the words: %+v %v", described, err)
		}
		undescribed, err := transaction.CountUndescribedPhotos(ownerId, &db.PhotoFilter{SearchText: "lighthouse"})
		if err != nil || undescribed != 1 {
			test.Fatalf("one photo is left undescribed, whatever the search words: %d %v", undescribed, err)
		}
		count, err := transaction.CountPhotos(ownerId, &db.PhotoFilter{SearchText: "lighthouse"})
		if err != nil || count != 1 {
			test.Fatalf("the count of a search is the size of its ranking: %d %v", count, err)
		}
		if err := transaction.SetPhotoDescription(ownerId, plain.ID, &db.PhotoDescription{PhotoDescription: "x", ModelName: "test-model"}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SetPhotoDescription(otherId, plain.ID, &db.PhotoDescription{PhotoDescription: "x"}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a photo of somebody else's was described: %v", err)
		}
	})
}

func TestAPhotoIsFoundByItsPlaceFileNameAndCamera(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-place-owner")
	taken := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	harbor := newTestPhoto(test, database, ownerId, "harbor", &taken)
	other := newTestPhoto(test, database, ownerId, "other", &taken)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.UpdatePhotoMetadata(harbor.ID, &db.PhotoMetadata{PlaceName: "Harborview, Exampleland", CameraMake: "Acme", CameraModel: "Pocket 9"}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.UpdatePhotoMetadata(other.ID, &db.PhotoMetadata{PlaceName: "Hillcrest, Exampleland"}); err != nil {
			test.Fatal(err)
		}
	})
	for searchText, want := range map[string][]string{
		"harborview":  {harbor.ID},
		"Harbor":      {harbor.ID}, // part of the place's name
		"pocket":      {harbor.ID},
		"exampleland": nil, // both, checked below
	} {
		found := searchPhotoIds(test, database, ownerId, searchText, nil)
		if want == nil {
			if len(found) != 2 {
				test.Fatalf("%q finds %v, want both", searchText, found)
			}
			continue
		}
		if !equalIds(found, want) {
			test.Fatalf("%q finds %v, want %v", searchText, found, want)
		}
	}
	// The file name was set when the photo was made: a piece of it still
	// finds it, as the search before descriptions did.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		created, _, err := transaction.CreatePhoto(&models.Photo{UserID: ownerId, ClientPhotoID: "named", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine, OriginalFilename: "IMG_4821.jpg"})
		if err != nil {
			test.Fatal(err)
		}
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "4821"})
		if err != nil || len(photos) != 1 || photos[0].ID != created.ID {
			test.Fatalf("a part of the file name finds the photo: %v %v", photos, err)
		}
	})
}

func TestAnAlbumsTitleIsPartOfWhatFindsItsPhotosUntilItIsRenamed(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-album-search-owner")
	taken := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	inAlbum := newTestPhoto(test, database, ownerId, "in-album", &taken)
	added := newTestPhoto(test, database, ownerId, "added-later", &taken)
	newTestPhoto(test, database, ownerId, "in-none", &taken)
	var album *models.PhotoAlbum
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if album, err = transaction.CreatePhotoAlbum(ownerId, "Lakeside Picnic", []string{inAlbum.ID}); err != nil {
			test.Fatal(err)
		}
	})
	if found := searchPhotoIds(test, database, ownerId, "picnic", nil); !equalIds(found, []string{inAlbum.ID}) {
		test.Fatalf("a photo is found by the title of an album made with it: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.AddPhotosToAlbum(ownerId, album.ID, []string{added.ID}); err != nil {
			test.Fatal(err)
		}
	})
	if found := searchPhotoIds(test, database, ownerId, "lakeside", nil); len(found) != 2 {
		test.Fatalf("a photo is found by the title of the album it was added to: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.RenamePhotoAlbum(ownerId, album.ID, "Mountain Hike"); err != nil {
			test.Fatal(err)
		}
	})
	if found := searchPhotoIds(test, database, ownerId, "picnic", nil); len(found) != 0 {
		test.Fatalf("the old title still finds photos after the rename: %v", found)
	}
	if found := searchPhotoIds(test, database, ownerId, "hike", nil); len(found) != 2 {
		test.Fatalf("the new title finds the album's photos: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.RemovePhotosFromAlbum(ownerId, album.ID, []string{added.ID}); err != nil {
			test.Fatal(err)
		}
	})
	if found := searchPhotoIds(test, database, ownerId, "hike", nil); !equalIds(found, []string{inAlbum.ID}) {
		test.Fatalf("a photo taken out of the album is no longer found by its title: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeletePhotoAlbum(ownerId, album.ID); err != nil {
			test.Fatal(err)
		}
	})
	if found := searchPhotoIds(test, database, ownerId, "hike", nil); len(found) != 0 {
		test.Fatalf("a deleted album's title still finds photos: %v", found)
	}
}

func TestAMeaningFindsAPhotoTheWordsDoNotAndRanksTheNearestFirst(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-meaning-owner")
	otherId := dbtest.CreateUser(test, database, "photo-meaning-other")
	taken := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	nearest := newTestPhoto(test, database, ownerId, "nearest", &taken)
	near := newTestPhoto(test, database, ownerId, "near", &taken)
	far := newTestPhoto(test, database, ownerId, "far", &taken)
	undescribed := newTestPhoto(test, database, ownerId, "undescribed", &taken)
	somebodyElses := newTestPhoto(test, database, otherId, "somebody-elses", &taken)

	const model = "test-embedding@4"
	vectors := map[string][]float32{
		nearest.ID: {1, 0.05, 0, 0}, near.ID: {1, 0.5, 0, 0}, far.ID: {0, 0, 1, 0},
	}
	for photoId, vector := range vectors {
		describePhoto(test, database, ownerId, photoId, "words that match nothing asked", "")
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			if err := transaction.PutPhotoVector(ownerId, photoId, model, vector); err != nil {
				test.Fatal(err)
			}
		})
	}
	describePhoto(test, database, otherId, somebodyElses.ID, "words that match nothing asked", "")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.PutPhotoVector(otherId, somebodyElses.ID, model, []float32{1, 0, 0, 0}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.PutPhotoVector(ownerId, somebodyElses.ID, model, []float32{1, 0, 0, 0}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a vector was kept for a photo of somebody else's: %v", err)
		}
		if err := transaction.PutPhotoVector(ownerId, undescribed.ID, "", []float32{1}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a vector with no model was kept: %v", err)
		}
	})

	// Without a meaning, nothing in these descriptions matches the words.
	if found := searchPhotoIds(test, database, ownerId, "boat", nil); len(found) != 0 {
		test.Fatalf("words found %v", found)
	}
	meaning := &db.PhotoSearchMeaning{ModelName: model, Vector: []float32{1, 0, 0, 0}}
	found := searchPhotoIds(test, database, ownerId, "boat", meaning)
	if !equalIds(found, []string{nearest.ID, near.ID}) {
		test.Fatalf("the meaning ranks the nearest descriptions first, and leaves out what is unrelated, undescribed or somebody else's: %v", found)
	}
	// The dates narrow a meaning as they narrow words.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		after := taken.Add(time.Hour)
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "boat", SearchMeaning: meaning, TakenAfter: &after})
		if err != nil || len(photos) != 0 {
			test.Fatalf("a near photo outside the dates was found: %v %v", photos, err)
		}
	})
	// A model that is not the one that wrote them ranks nothing.
	if found := searchPhotoIds(test, database, ownerId, "boat", &db.PhotoSearchMeaning{ModelName: "another-model@4", Vector: []float32{1, 0, 0, 0}}); len(found) != 0 {
		test.Fatalf("another model's reading found %v", found)
	}

	// A match by words and by meaning outranks a match by meaning alone, and
	// the second page continues the first in the same order.
	describePhoto(test, database, ownerId, far.ID, "a boat at a quay", "")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.PutPhotoVector(ownerId, far.ID, model, []float32{0.8, 0.8, 0, 0}); err != nil {
			test.Fatal(err)
		}
		first, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 2, SearchText: "boat", SearchMeaning: meaning})
		if err != nil || len(first) != 2 || first[0].ID != nearest.ID && first[0].ID != far.ID {
			test.Fatalf("the first page: %v %v", photoIdsOf(first), err)
		}
		second, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 2, SearchText: "boat", SearchMeaning: meaning, BeforePhotoID: first[1].ID})
		if err != nil || len(second) != 1 {
			test.Fatalf("the second page: %v %v", photoIdsOf(second), err)
		}
		byNumber, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 2, SearchText: "boat", SearchMeaning: meaning, Offset: 2})
		if err != nil || len(byNumber) != 1 || byNumber[0].ID != second[0].ID {
			test.Fatalf("the page at offset 2: %v %v", photoIdsOf(byNumber), err)
		}
		if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 2, SearchText: "boat", BeforePhotoID: undescribed.ID}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a page after a photo the search did not rank: %v", err)
		}
	})
	describedAgain := searchPhotoIds(test, database, ownerId, "boat", meaning)
	if len(describedAgain) != 3 || describedAgain[0] != far.ID {
		test.Fatalf("the photo found by words and by meaning is first: %v (far is %s)", describedAgain, far.ID)
	}

	// Writing a new description deletes the vector made from the old one.
	describePhoto(test, database, ownerId, nearest.ID, "something else entirely", "")
	if found := searchPhotoIds(test, database, ownerId, "boat", meaning); len(found) != 2 {
		test.Fatalf("the vector of a replaced description is gone: %v", found)
	}
}

func TestSearchWordsAreCheckedAndTheTrashIsNotSearched(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-search-trash-owner")
	taken := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	trashed := newTestPhoto(test, database, ownerId, "trashed", &taken)
	describePhoto(test, database, ownerId, trashed.ID, "a bridge over a river", "")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, []string{trashed.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "a b c d e f g h i"}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("nine words were accepted: %v", err)
		}
	})
	if found := searchPhotoIds(test, database, ownerId, "bridge", nil); len(found) != 0 {
		test.Fatalf("the library search found a photo in the trash: %v", found)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "bridge", IsTrashed: true})
		if err != nil || len(photos) != 1 {
			test.Fatalf("the trash search finds it: %v %v", photos, err)
		}
	})
}

// A part of a word finds a photo too, after the photos whose document has the
// word, and the parts are looked for only when the words alone leave room: a
// page the words fill is the words' alone, and a page they do not fill goes on
// with the parts.
func TestAPartOfAWordFindsAPhotoAfterThoseThatHaveTheWord(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-parts-owner")
	for index, day := range []int{4, 3, 2} {
		taken := time.Date(2024, 5, day, 12, 0, 0, 0, time.UTC)
		found := newTestPhoto(test, database, ownerId, "with-word-"+string(rune('a'+index)), &taken)
		describePhoto(test, database, ownerId, found.ID, "A sailboat in the harbor at dusk.", "")
	}
	taken := time.Date(2024, 5, 5, 12, 0, 0, 0, time.UTC)
	partial := newTestPhoto(test, database, ownerId, "harborside-trip", &taken)
	dbtest.Exec(test, database, "UPDATE photo SET original_filename = 'harborside-trip.jpg' WHERE id = '"+partial.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoDescription(ownerId, partial.ID, &db.PhotoDescription{PhotoDescription: "A quiet street.", ModelName: "test-model"}); err != nil {
			test.Fatal(err)
		}
	})

	page := func(offset, pageSize int) []string {
		var photoIds []string
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{SearchText: "harbor", Offset: offset, PageSize: pageSize})
			if err != nil {
				test.Fatal(err)
			}
			photoIds = photoIdsOf(photos)
		})
		return photoIds
	}
	all := page(0, 50)
	if len(all) != 4 || all[3] != partial.ID {
		test.Fatalf("the whole ranking %v, want the three with the word and then the partial match", all)
	}
	// A page the words fill is theirs alone.
	if first := page(0, 2); !equalIds(first, all[:2]) {
		test.Fatalf("first page %v, want %v", first, all[:2])
	}
	// One they do not fill goes on with the parts.
	if second := page(2, 2); !equalIds(second, all[2:]) {
		test.Fatalf("second page %v, want %v", second, all[2:])
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if count, err := transaction.CountPhotos(ownerId, &db.PhotoFilter{SearchText: "harbor"}); err != nil || count != 4 {
			test.Fatalf("the count is the whole ranking: %d %v", count, err)
		}
	})
}
