package db_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// faceVectorAt is a unit vector of 128 numbers pointing at an angle in the
// plane of the first two: the similarity of two of them is the cosine of the
// angle between, so a test can say how alike two faces are.
func faceVectorAt(angleDegrees float64) []float32 {
	vector := make([]float32, 128)
	radians := angleDegrees * math.Pi / 180
	vector[0], vector[1] = float32(math.Cos(radians)), float32(math.Sin(radians))
	return vector
}

// foundFaceAt is a face found at the given place in a picture, as the reader
// would give it, with a made-up vector.
func foundFaceAt(angleDegrees, boxLeftFraction float64) *models.PhotoFace {
	return &models.PhotoFace{
		BoxLeftFraction: boxLeftFraction, BoxTopFraction: 0.2, BoxWidthFraction: 0.2, BoxHeightFraction: 0.25,
		DetectionScore: 0.95, FaceVector: faceVectorAt(angleDegrees),
	}
}

// readyPhotoWithPreview is a photo that is ready and has the large preview
// that faces are read from.
func readyPhotoWithPreview(test *testing.T, database db.Database, userId, clientPhotoId string) *models.Photo {
	test.Helper()
	found := newTestPhoto(test, database, userId, clientPhotoId, nil)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.UpsertPhotoPreview(userId, &models.PhotoPreview{PhotoID: found.ID, PhotoPreviewSize: models.PhotoPreviewLarge, ContentType: "image/jpeg", PreviewSizeBytes: 10}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.MarkPhotoReady(found.ID); err != nil {
			test.Fatal(err)
		}
	})
	return found
}

// storedFaces stores faces for a photo, each put in a group of its own, and
// returns them as stored.
func storeFaces(test *testing.T, database db.Database, userId, photoId string, found ...*models.PhotoFace) []*models.PhotoFace {
	test.Helper()
	var stored []*models.PhotoFace
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if stored, err = transaction.StorePhotoFaces(userId, photoId, found); err != nil {
			test.Fatal(err)
		}
	})
	return stored
}

// groupOwnFace starts a group for a face.
func groupOwnFace(test *testing.T, database db.Database, userId, photoFaceId string) string {
	test.Helper()
	var photoFaceGroupId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if photoFaceGroupId, err = transaction.StartPhotoFaceGroup(userId, photoFaceId); err != nil {
			test.Fatal(err)
		}
	})
	return photoFaceGroupId
}

func joinFaceToGroup(test *testing.T, database db.Database, userId, photoFaceId, photoFaceGroupId string) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.JoinPhotoFaceGroup(userId, photoFaceId, photoFaceGroupId); err != nil {
			test.Fatal(err)
		}
	})
}

// keepContact puts a contact in a new address book of the person and returns
// both identifiers.
func keepContact(test *testing.T, database db.Database, userId, name string) (string, string) {
	test.Helper()
	var addressBookId, contactId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId})
		if err != nil {
			test.Fatal(err)
		}
		kept, err := transaction.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "urn:uuid:" + name, ETag: "e1", Name: name,
			Card: "BEGIN:VCARD\nVERSION:3.0\nFN:" + name + "\nEND:VCARD\n",
		})
		if err != nil {
			test.Fatal(err)
		}
		addressBookId, contactId = book.ID, kept.ID
	})
	return addressBookId, contactId
}

func nameGroup(test *testing.T, database db.Database, userId, photoFaceGroupId, addressBookId, contactId string) *models.PhotoFaceGroup {
	test.Helper()
	var named *models.PhotoFaceGroup
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if named, err = transaction.NamePhotoFaceGroup(userId, photoFaceGroupId, addressBookId, contactId); err != nil {
			test.Fatal(err)
		}
	})
	return named
}

func listedGroupIds(test *testing.T, database db.Database, userId string, isHiddenIncluded bool) []string {
	test.Helper()
	var photoFaceGroupIds []string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		groups, err := transaction.ListPhotoFaceGroups(userId, isHiddenIncluded)
		if err != nil {
			test.Fatal(err)
		}
		for _, group := range groups {
			photoFaceGroupIds = append(photoFaceGroupIds, group.ID)
		}
	})
	return photoFaceGroupIds
}

func TestStoredFacesComeBackWithTheirBoxesAndNoVector(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "face-owner")
	otherId := dbtest.CreateUser(test, database, "face-other")
	found := readyPhotoWithPreview(test, database, ownerId, "faces")
	stored := storeFaces(test, database, ownerId, found.ID, foundFaceAt(0, 0.1), foundFaceAt(90, 0.6))
	if len(stored) != 2 || stored[0].ID == "" || len(stored[0].FaceVector) != 128 || stored[0].PhotoFaceGroupID != "" || stored[0].IsGroupConfirmed {
		test.Fatalf("%+v", stored)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		faces, err := transaction.ListPhotoFaces(ownerId, []string{found.ID})
		if err != nil || len(faces) != 2 || faces[0].ID != stored[0].ID || faces[1].ID != stored[1].ID {
			test.Fatalf("%+v %v", faces, err)
		}
		if faces[0].BoxLeftFraction < 0.099 || faces[0].BoxLeftFraction > 0.101 || faces[0].BoxWidthFraction < 0.19 || faces[0].DetectionScore < 0.94 || faces[0].FaceVector != nil {
			test.Fatalf("the box and the score are read back and the vector is not: %+v", faces[0])
		}
		// Somebody else's photo has no faces to read, and a face of another
		// person is not found.
		if faces, err := transaction.ListPhotoFaces(otherId, []string{found.ID}); err != nil || len(faces) != 0 {
			test.Fatalf("%+v %v", faces, err)
		}
		if _, err := transaction.GetPhotoFace(otherId, stored[0].ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		if _, err := transaction.StorePhotoFaces(otherId, found.ID, nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		// The faces of a photo are found once, until its pixels change.
		if _, err := transaction.StorePhotoFaces(ownerId, found.ID, nil); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("%v", err)
		}
	})
	// Storing records that the photo was read, even when no face was found.
	empty := readyPhotoWithPreview(test, database, ownerId, "no faces")
	if waiting := dbtest.QueryString(test, database, "SELECT count(*) FROM photo WHERE faces_found_at IS NULL"); waiting != "1" {
		test.Fatalf("one photo has not been read: %s", waiting)
	}
	storeFaces(test, database, ownerId, empty.ID)
	if waiting := dbtest.QueryString(test, database, "SELECT count(*) FROM photo WHERE faces_found_at IS NULL"); waiting != "0" {
		test.Fatalf("%s photos have not been read", waiting)
	}
}

func TestJoiningAGroupAddsTheVectorAndABetterFaceBecomesTheCover(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "group-owner")
	found := readyPhotoWithPreview(test, database, ownerId, "group")
	small := foundFaceAt(0, 0.1)
	small.BoxWidthFraction, small.BoxHeightFraction = 0.05, 0.06
	large := foundFaceAt(40, 0.6)
	stored := storeFaces(test, database, ownerId, found.ID, small, large)
	photoFaceGroupId := groupOwnFace(test, database, ownerId, stored[0].ID)
	joinFaceToGroup(test, database, ownerId, stored[1].ID, photoFaceGroupId)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
		if err != nil || len(vectors) != 1 || vectors[0].ID != photoFaceGroupId {
			test.Fatalf("%+v %v", vectors, err)
		}
		// The sum of a vector at 0 degrees and one at 40.
		if got, want := vectors[0].VectorSum[0], float32(1+math.Cos(40*math.Pi/180)); math.Abs(float64(got-want)) > 1e-5 {
			test.Fatalf("%v, want %v", got, want)
		}
		if got, want := vectors[0].VectorSum[1], float32(math.Sin(40*math.Pi/180)); math.Abs(float64(got-want)) > 1e-5 {
			test.Fatalf("%v, want %v", got, want)
		}
		group, err := transaction.GetPhotoFaceGroup(ownerId, photoFaceGroupId)
		if err != nil || group.FaceCount != 2 || group.PhotoCount != 1 || group.CoverPhotoFaceID != stored[1].ID {
			test.Fatalf("the larger face stands for the group: %+v %v", group, err)
		}
		// A face that is in a group already cannot be put in another.
		if err := transaction.JoinPhotoFaceGroup(ownerId, stored[1].ID, photoFaceGroupId); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("%v", err)
		}
	})
}

func TestNamingAGroupNeedsOneOfThePersonsOwnContacts(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "naming-owner")
	otherId := dbtest.CreateUser(test, database, "naming-other")
	found := readyPhotoWithPreview(test, database, ownerId, "named")
	stored := storeFaces(test, database, ownerId, found.ID, foundFaceAt(0, 0.1))
	photoFaceGroupId := groupOwnFace(test, database, ownerId, stored[0].ID)
	ownBookId, ownContactId := keepContact(test, database, ownerId, "Example Grandma")
	foreignBookId, foreignContactId := keepContact(test, database, otherId, "Foreign Person")

	err := database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.NamePhotoFaceGroup(ownerId, photoFaceGroupId, foreignBookId, foreignContactId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("another person's contact was accepted: %v", err)
	}
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.NamePhotoFaceGroup(otherId, photoFaceGroupId, foreignBookId, foreignContactId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("another person named a group that is not theirs: %v", err)
	}
	// A contact's identifier means a contact only in its own book.
	err = database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.NamePhotoFaceGroup(ownerId, photoFaceGroupId, foreignBookId, ownContactId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("%v", err)
	}

	named := nameGroup(test, database, ownerId, photoFaceGroupId, ownBookId, ownContactId)
	if named.ContactID != ownContactId || named.ContactAddressBookID != ownBookId || named.ContactName != "Example Grandma" {
		test.Fatalf("%+v", named)
	}
	// A photo is found by the name of the person in it.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "grandma"})
		if err != nil || len(photos) != 1 || photos[0].ID != found.ID {
			test.Fatalf("%v %v", photoIdsOf(photos), err)
		}
		faces, err := transaction.ListPhotoFaces(ownerId, []string{found.ID})
		if err != nil || len(faces) != 1 || faces[0].ContactID != ownContactId || faces[0].ContactName != "Example Grandma" || faces[0].PhotoFaceGroupID != photoFaceGroupId {
			test.Fatalf("%+v %v", faces, err)
		}
	})

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		unnamed, err := transaction.UnnamePhotoFaceGroup(ownerId, photoFaceGroupId)
		if err != nil || unnamed.ContactID != "" || unnamed.ContactName != "" {
			test.Fatalf("%+v %v", unnamed, err)
		}
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "grandma"})
		if err != nil || len(photos) != 0 {
			test.Fatalf("a name that was taken off still finds the photo: %v %v", photoIdsOf(photos), err)
		}
	})

	// A contact that is deleted leaves the group unnamed, not gone.
	nameGroup(test, database, ownerId, photoFaceGroupId, ownBookId, ownContactId)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteContact(ownBookId, ownContactId); err != nil {
			test.Fatal(err)
		}
		group, err := transaction.GetPhotoFaceGroup(ownerId, photoFaceGroupId)
		if err != nil || group.ContactID != "" || group.ContactName != "" || group.FaceCount != 1 {
			test.Fatalf("%+v %v", group, err)
		}
	})
}

func TestGroupsAreListedNamedFirstThenByFaceCountAndSmallOnesAreLeftOut(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "listing-owner")
	otherId := dbtest.CreateUser(test, database, "listing-other")
	// Four photos with three faces each of the same two people and one
	// stranger, so that the groups have 4, 4 and 1 faces, plus a pair.
	var firstGroup, secondGroup, pairGroup string
	for index := 0; index < 4; index++ {
		found := readyPhotoWithPreview(test, database, ownerId, fmt.Sprintf("listing-%d", index))
		faces := []*models.PhotoFace{foundFaceAt(0, 0.1), foundFaceAt(90, 0.5)}
		if index == 0 {
			faces = append(faces, foundFaceAt(180, 0.8))
		}
		stored := storeFaces(test, database, ownerId, found.ID, faces...)
		if index == 0 {
			firstGroup = groupOwnFace(test, database, ownerId, stored[0].ID)
			secondGroup = groupOwnFace(test, database, ownerId, stored[1].ID)
			groupOwnFace(test, database, ownerId, stored[2].ID)
			continue
		}
		joinFaceToGroup(test, database, ownerId, stored[0].ID, firstGroup)
		joinFaceToGroup(test, database, ownerId, stored[1].ID, secondGroup)
	}
	pairPhoto := readyPhotoWithPreview(test, database, ownerId, "pair")
	pairStored := storeFaces(test, database, ownerId, pairPhoto.ID, foundFaceAt(45, 0.1))
	pairGroup = groupOwnFace(test, database, ownerId, pairStored[0].ID)
	joinFaceToGroup(test, database, ownerId, storeFaces(test, database, ownerId, readyPhotoWithPreview(test, database, ownerId, "pair second").ID, foundFaceAt(46, 0.1))[0].ID, pairGroup)

	// Two groups of four faces and a pair that has two: the stranger and the
	// pair are not listed, being fewer than three and nobody's.
	listed := listedGroupIds(test, database, ownerId, false)
	if len(listed) != 2 || (listed[0] != firstGroup && listed[0] != secondGroup) {
		test.Fatalf("%v", listed)
	}

	// Naming one lists it first, named groups ordered by the contact's name
	// whatever their size; naming the pair lists the pair, at two faces.
	bookId, zoeContactId := keepContact(test, database, ownerId, "Zoe Example")
	var adaContactId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		kept, err := transaction.PutContact(&models.Contact{AddressBookID: bookId, UID: "urn:uuid:ada", ETag: "e2", Name: "ada Example", Card: "BEGIN:VCARD\nVERSION:3.0\nFN:ada Example\nEND:VCARD\n"})
		if err != nil {
			test.Fatal(err)
		}
		adaContactId = kept.ID
	})
	nameGroup(test, database, ownerId, firstGroup, bookId, zoeContactId)
	nameGroup(test, database, ownerId, pairGroup, bookId, adaContactId)
	listed = listedGroupIds(test, database, ownerId, false)
	if len(listed) != 3 || listed[0] != pairGroup || listed[1] != firstGroup || listed[2] != secondGroup {
		test.Fatalf("named first, by name without regard to case, then by size: %v (pair %s, first %s, second %s)", listed, pairGroup, firstGroup, secondGroup)
	}

	// A hidden group is left out unless asked for.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		hidden, err := transaction.HidePhotoFaceGroup(ownerId, secondGroup, true)
		if err != nil || !hidden.IsHidden {
			test.Fatalf("%+v %v", hidden, err)
		}
		if _, err := transaction.HidePhotoFaceGroup(otherId, secondGroup, false); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%v", err)
		}
	})
	if listed = listedGroupIds(test, database, ownerId, false); len(listed) != 2 {
		test.Fatalf("%v", listed)
	}
	if listed = listedGroupIds(test, database, ownerId, true); len(listed) != 3 {
		test.Fatalf("%v", listed)
	}
	if listed = listedGroupIds(test, database, otherId, true); len(listed) != 0 {
		test.Fatalf("%v", listed)
	}
}

func TestFacesInThePhotosOfTheTrashAreNotCounted(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "trash-owner")
	var photoFaceGroupId string
	var photoIds []string
	for index := 0; index < 3; index++ {
		found := readyPhotoWithPreview(test, database, ownerId, fmt.Sprintf("trashed-%d", index))
		stored := storeFaces(test, database, ownerId, found.ID, foundFaceAt(0, 0.1))
		if index == 0 {
			photoFaceGroupId = groupOwnFace(test, database, ownerId, stored[0].ID)
		} else {
			joinFaceToGroup(test, database, ownerId, stored[0].ID, photoFaceGroupId)
		}
		photoIds = append(photoIds, found.ID)
	}
	if listed := listedGroupIds(test, database, ownerId, false); len(listed) != 1 {
		test.Fatalf("%v", listed)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, photoIds[:2]); err != nil {
			test.Fatal(err)
		}
		group, err := transaction.GetPhotoFaceGroup(ownerId, photoFaceGroupId)
		if err != nil || group.FaceCount != 1 || group.PhotoCount != 1 {
			test.Fatalf("%+v %v", group, err)
		}
	})
	if listed := listedGroupIds(test, database, ownerId, false); len(listed) != 0 {
		test.Fatalf("a group of one visible face is not worth listing: %v", listed)
	}
}

func TestMergingMovesTheFacesIntoTheFirstGroupAndKeepsANameFromTheOthers(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "merge-owner")
	otherId := dbtest.CreateUser(test, database, "merge-other")
	first := readyPhotoWithPreview(test, database, ownerId, "merge-first")
	second := readyPhotoWithPreview(test, database, ownerId, "merge-second")
	foreign := readyPhotoWithPreview(test, database, otherId, "merge-foreign")
	firstStored := storeFaces(test, database, ownerId, first.ID, foundFaceAt(0, 0.1))
	secondStored := storeFaces(test, database, ownerId, second.ID, foundFaceAt(30, 0.1))
	foreignStored := storeFaces(test, database, otherId, foreign.ID, foundFaceAt(0, 0.1))
	firstGroup := groupOwnFace(test, database, ownerId, firstStored[0].ID)
	secondGroup := groupOwnFace(test, database, ownerId, secondStored[0].ID)
	foreignGroup := groupOwnFace(test, database, otherId, foreignStored[0].ID)
	bookId, contactId := keepContact(test, database, ownerId, "Example Uncle")
	nameGroup(test, database, ownerId, secondGroup, bookId, contactId)

	for _, scenario := range []struct {
		name     string
		groupIds []string
		want     error
	}{
		{"one group", []string{firstGroup}, db.ErrInvalidArguments},
		{"the same group twice", []string{firstGroup, firstGroup}, db.ErrInvalidArguments},
		{"somebody else's group", []string{firstGroup, foreignGroup}, db.ErrNotFound},
		{"a group that is not there", []string{firstGroup, "nowhere"}, db.ErrNotFound},
	} {
		err := database.Transaction(func(transaction db.Transaction) error {
			_, err := transaction.MergePhotoFaceGroups(ownerId, scenario.groupIds)
			return err
		})
		if !errors.Is(err, scenario.want) {
			test.Fatalf("%s: %v", scenario.name, err)
		}
	}
	// Nothing moved for the refusals.
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group"); count != "3" {
		test.Fatalf("%s groups", count)
	}

	var merged *models.PhotoFaceGroup
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if merged, err = transaction.MergePhotoFaceGroups(ownerId, []string{firstGroup, secondGroup}); err != nil {
			test.Fatal(err)
		}
		if merged.ID != firstGroup || merged.FaceCount != 2 || merged.PhotoCount != 2 || merged.ContactID != contactId || merged.ContactName != "Example Uncle" {
			test.Fatalf("%+v", merged)
		}
		if _, err := transaction.GetPhotoFaceGroup(ownerId, secondGroup); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("the merged group is still there: %v", err)
		}
		vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
		if err != nil || len(vectors) != 1 || vectors[0].ID != firstGroup || math.Abs(float64(vectors[0].VectorSum[0])-(1+math.Cos(30*math.Pi/180))) > 1e-5 {
			test.Fatalf("%+v %v", vectors, err)
		}
		// The photos of the name that came with the merge find it.
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, ContactID: contactId})
		if err != nil || len(photos) != 2 {
			test.Fatalf("%v %v", photoIdsOf(photos), err)
		}
		found, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "uncle"})
		if err != nil || len(found) != 2 {
			test.Fatalf("%v %v", photoIdsOf(found), err)
		}
	})
	// The other person's group is where it was.
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE user_id = '"+otherId+"'"); count != "1" {
		test.Fatalf("%s", count)
	}
}

func TestTakingAFaceOutOfAGroupStartsAConfirmedGroupThatSurvivesReading(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "remove-owner")
	otherId := dbtest.CreateUser(test, database, "remove-other")
	first := readyPhotoWithPreview(test, database, ownerId, "remove-first")
	second := readyPhotoWithPreview(test, database, ownerId, "remove-second")
	firstStored := storeFaces(test, database, ownerId, first.ID, foundFaceAt(0, 0.1))
	secondStored := storeFaces(test, database, ownerId, second.ID, foundFaceAt(10, 0.1))
	photoFaceGroupId := groupOwnFace(test, database, ownerId, firstStored[0].ID)
	joinFaceToGroup(test, database, ownerId, secondStored[0].ID, photoFaceGroupId)

	if err := database.Transaction(func(transaction db.Transaction) error {
		_, err := transaction.RemovePhotoFaceFromGroup(otherId, secondStored[0].ID)
		return err
	}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("%v", err)
	}
	var alone *models.PhotoFaceGroup
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if alone, err = transaction.RemovePhotoFaceFromGroup(ownerId, secondStored[0].ID); err != nil {
			test.Fatal(err)
		}
		if alone.ID == photoFaceGroupId || alone.FaceCount != 1 || alone.CoverPhotoFaceID != secondStored[0].ID {
			test.Fatalf("%+v", alone)
		}
		old, err := transaction.GetPhotoFaceGroup(ownerId, photoFaceGroupId)
		if err != nil || old.FaceCount != 1 {
			test.Fatalf("%+v %v", old, err)
		}
		vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
		if err != nil || len(vectors) != 2 {
			test.Fatalf("%+v %v", vectors, err)
		}
		for _, vector := range vectors {
			switch vector.ID {
			case photoFaceGroupId:
				// What is left of the old group is its first face alone.
				if math.Abs(float64(vector.VectorSum[0])-1) > 1e-5 || math.Abs(float64(vector.VectorSum[1])) > 1e-5 {
					test.Fatalf("the old group's vector is not its remaining face: %v", vector.VectorSum[:2])
				}
			case alone.ID:
				if math.Abs(float64(vector.VectorSum[0])-math.Cos(10*math.Pi/180)) > 1e-5 {
					test.Fatalf("%v", vector.VectorSum[:2])
				}
			}
		}
		face, err := transaction.GetPhotoFace(ownerId, secondStored[0].ID)
		if err != nil || !face.IsGroupConfirmed || face.PhotoFaceGroupID != alone.ID {
			test.Fatalf("%+v %v", face, err)
		}
	})

	// Reading the photo again finds a face in about the same place, which is
	// the face the person confirmed and keeps its group.
	dbtest.Exec(test, database, "UPDATE photo SET faces_found_at = NULL")
	again := storeFaces(test, database, ownerId, second.ID, foundFaceAt(12, 0.11))
	if len(again) != 1 || again[0].ID == secondStored[0].ID || again[0].PhotoFaceGroupID != alone.ID || !again[0].IsGroupConfirmed {
		test.Fatalf("%+v", again)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		group, err := transaction.GetPhotoFaceGroup(ownerId, alone.ID)
		if err != nil || group.FaceCount != 1 {
			test.Fatalf("%+v %v", group, err)
		}
	})
	// And a face the person never confirmed is simply replaced, leaving its
	// group with one fewer.
	dbtest.Exec(test, database, "UPDATE photo SET faces_found_at = NULL")
	moved := storeFaces(test, database, ownerId, first.ID, foundFaceAt(0, 0.7))
	if len(moved) != 1 || moved[0].PhotoFaceGroupID != "" || moved[0].IsGroupConfirmed {
		test.Fatalf("%+v", moved)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		old, err := transaction.GetPhotoFaceGroup(ownerId, photoFaceGroupId)
		if err != nil || old.FaceCount != 0 {
			test.Fatalf("%+v %v", old, err)
		}
		// A group nothing is in is not offered to a face, and is deleted by
		// cleanup once it has stood empty for a while.
		vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
		if err != nil || len(vectors) != 1 || vectors[0].ID != alone.ID {
			test.Fatalf("%+v %v", vectors, err)
		}
		if deletedCount, err := transaction.DeleteEmptyPhotoFaceGroups(); err != nil || deletedCount != 0 {
			test.Fatalf("a group emptied a moment ago was deleted: %d %v", deletedCount, err)
		}
	})
	dbtest.Exec(test, database, "UPDATE photo_face_group SET updated_at = now() - interval '2 hours'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if deletedCount, err := transaction.DeleteEmptyPhotoFaceGroups(); err != nil || deletedCount != 1 {
			test.Fatalf("%d %v", deletedCount, err)
		}
	})
}

func TestListingPhotosByAFaceGroupOrByAContactIsTheOwnersAlone(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "people-owner")
	otherId := dbtest.CreateUser(test, database, "people-other")
	var firstGroup, secondGroup string
	bookId, contactId := keepContact(test, database, ownerId, "Example Friend")
	for index := 0; index < 4; index++ {
		found := readyPhotoWithPreview(test, database, ownerId, fmt.Sprintf("people-%d", index))
		stored := storeFaces(test, database, ownerId, found.ID, foundFaceAt(float64(index*10), 0.1))
		switch index {
		case 0:
			firstGroup = groupOwnFace(test, database, ownerId, stored[0].ID)
		case 1:
			joinFaceToGroup(test, database, ownerId, stored[0].ID, firstGroup)
		case 2:
			// The same person in a second group, which the same contact names.
			secondGroup = groupOwnFace(test, database, ownerId, stored[0].ID)
		}
	}
	nameGroup(test, database, ownerId, firstGroup, bookId, contactId)
	nameGroup(test, database, ownerId, secondGroup, bookId, contactId)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		byContact, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, ContactID: contactId})
		if err != nil || len(byContact) != 3 {
			test.Fatalf("a contact finds the photos of every group it names: %v %v", photoIdsOf(byContact), err)
		}
		count, err := transaction.CountPhotos(ownerId, &db.PhotoFilter{ContactID: contactId})
		if err != nil || count != 3 {
			test.Fatalf("%d %v", count, err)
		}
		byGroup, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, FaceGroupID: firstGroup})
		if err != nil || len(byGroup) != 2 {
			test.Fatalf("%v %v", photoIdsOf(byGroup), err)
		}
		both, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, FaceGroupID: firstGroup, ContactID: contactId})
		if err != nil || len(both) != 2 {
			test.Fatalf("%v %v", photoIdsOf(both), err)
		}
		none, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, ContactID: "nobody"})
		if err != nil || len(none) != 0 {
			test.Fatalf("%v %v", photoIdsOf(none), err)
		}
		// Somebody else asking for the group or the contact gets nothing of
		// the owner's: the group is not found, and the contact matches none
		// of their own.
		if _, err := transaction.ListPhotos(otherId, &db.PhotoFilter{PageSize: 10, FaceGroupID: firstGroup}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		foreign, err := transaction.ListPhotos(otherId, &db.PhotoFilter{PageSize: 10, ContactID: contactId})
		if err != nil || len(foreign) != 0 {
			test.Fatalf("%v %v", photoIdsOf(foreign), err)
		}
	})
}

func TestSwitchingFacesOffDeletesEveryFaceAndGroupAndOnReadsThePhotosAgain(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "switch-owner")
	otherId := dbtest.CreateUser(test, database, "switch-other")
	bookId, contactId := keepContact(test, database, ownerId, "Example Cousin")
	own := readyPhotoWithPreview(test, database, ownerId, "switch-own")
	foreign := readyPhotoWithPreview(test, database, otherId, "switch-foreign")
	ownStored := storeFaces(test, database, ownerId, own.ID, foundFaceAt(0, 0.1))
	foreignStored := storeFaces(test, database, otherId, foreign.ID, foundFaceAt(0, 0.1))
	nameGroup(test, database, ownerId, groupOwnFace(test, database, ownerId, ownStored[0].ID), bookId, contactId)
	groupOwnFace(test, database, otherId, foreignStored[0].ID)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		isEnabled, err := transaction.IsPhotoFaceRecognitionEnabled(ownerId)
		if err != nil || !isEnabled {
			test.Fatalf("faces are on until they are turned off: %v %v", isEnabled, err)
		}
		if err := transaction.SetPhotoFaceRecognition(ownerId, false); err != nil {
			test.Fatal(err)
		}
		isEnabled, err = transaction.IsPhotoFaceRecognitionEnabled(ownerId)
		if err != nil || isEnabled {
			test.Fatalf("%v %v", isEnabled, err)
		}
		if err := transaction.SetPhotoFaceRecognition("nobody", false); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("%v", err)
		}
	})
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face WHERE user_id = '"+ownerId+"'"); count != "0" {
		test.Fatalf("%s faces", count)
	}
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE user_id = '"+ownerId+"'"); count != "0" {
		test.Fatalf("%s groups", count)
	}
	// The other person's faces are untouched.
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face WHERE user_id = '"+otherId+"'"); count != "1" {
		test.Fatalf("%s faces", count)
	}
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE user_id = '"+otherId+"'"); count != "1" {
		test.Fatalf("%s groups", count)
	}
	// Nothing waits for the faces of a person who switched them off.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		waiting, err := transaction.ListPhotosWaitingForFaces(10, nil)
		if err != nil || len(waiting) != 0 {
			test.Fatalf("%d %v", len(waiting), err)
		}
		photos, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, SearchText: "cousin"})
		if err != nil || len(photos) != 0 {
			test.Fatalf("the name stays in the search document: %v %v", photoIdsOf(photos), err)
		}
		if err := transaction.SetPhotoFaceRecognition(ownerId, true); err != nil {
			test.Fatal(err)
		}
		waiting, err = transaction.ListPhotosWaitingForFaces(10, nil)
		if err != nil || len(waiting) != 1 || waiting[0].ID != own.ID {
			test.Fatalf("%d %v", len(waiting), err)
		}
		// A photo that failed a moment ago is left out of the next question.
		if waiting, err = transaction.ListPhotosWaitingForFaces(10, []string{own.ID}); err != nil || len(waiting) != 0 {
			test.Fatalf("%d %v", len(waiting), err)
		}
	})
}

func TestPhotosWaitForFacesOnlyWhenReadyWithALargePreview(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "waiting-owner")
	ready := readyPhotoWithPreview(test, database, ownerId, "ready")
	// A photo whose previews are not made is not ready, and one marked ready
	// with no large preview has nothing to read.
	newTestPhoto(test, database, ownerId, "not processed", nil)
	noPreview := newTestPhoto(test, database, ownerId, "no preview", nil)
	trashed := readyPhotoWithPreview(test, database, ownerId, "trashed")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.MarkPhotoReady(noPreview.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.TrashPhotos(ownerId, []string{trashed.ID}); err != nil {
			test.Fatal(err)
		}
		waiting, err := transaction.ListPhotosWaitingForFaces(10, nil)
		if err != nil || len(waiting) != 1 || waiting[0].ID != ready.ID {
			test.Fatalf("%v %v", photoIdsOf(waiting), err)
		}
		if _, err := transaction.ListPhotosWaitingForFaces(0, nil); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("%v", err)
		}
	})
}

func TestTwoGroupingsOfOnePersonTakeTurns(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "lock-owner")
	otherId := dbtest.CreateUser(test, database, "lock-other")

	// The first transaction holds the person's lock; a second for the same
	// person does not get it until the first ends, and one for another person
	// does not wait.
	holding := make(chan struct{})
	release := make(chan struct{})
	var group sync.WaitGroup
	var order []string
	var orderMutex sync.Mutex
	note := func(what string) {
		orderMutex.Lock()
		order = append(order, what)
		orderMutex.Unlock()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		if err := database.Transaction(func(transaction db.Transaction) error {
			if err := transaction.LockPhotoFaceGrouping(ownerId); err != nil {
				return err
			}
			close(holding)
			<-release
			note("first done")
			return nil
		}); err != nil {
			test.Error(err)
		}
	}()
	<-holding
	if err := database.Transaction(func(transaction db.Transaction) error { return transaction.LockPhotoFaceGrouping(otherId) }); err != nil {
		test.Fatal(err)
	}
	secondDone := make(chan struct{})
	group.Add(1)
	go func() {
		defer group.Done()
		defer close(secondDone)
		if err := database.Transaction(func(transaction db.Transaction) error {
			if err := transaction.LockPhotoFaceGrouping(ownerId); err != nil {
				return err
			}
			note("second got the lock")
			return nil
		}); err != nil {
			test.Error(err)
		}
	}()
	select {
	case <-secondDone:
		test.Fatal("the second grouping did not wait for the first")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	group.Wait()
	if strings.Join(order, ",") != "first done,second got the lock" {
		test.Fatalf("%v", order)
	}
}

// A group keeps the sum of the vectors of its faces, so deleting a photo for
// good takes its faces out of the sum, and a group left with no face goes.
func TestDeletingPhotosForGoodTakesTheirFacesOutOfTheirGroups(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "delete-faces-owner")
	kept := readyPhotoWithPreview(test, database, ownerId, "kept")
	deleted := readyPhotoWithPreview(test, database, ownerId, "deleted")
	alone := readyPhotoWithPreview(test, database, ownerId, "alone")
	shared := groupOwnFace(test, database, ownerId, storeFaces(test, database, ownerId, kept.ID, foundFaceAt(0, 0.1))[0].ID)
	joinFaceToGroup(test, database, ownerId, storeFaces(test, database, ownerId, deleted.ID, foundFaceAt(40, 0.1))[0].ID, shared)
	loner := groupOwnFace(test, database, ownerId, storeFaces(test, database, ownerId, alone.ID, foundFaceAt(90, 0.1))[0].ID)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		photoIds := []string{deleted.ID, alone.ID}
		if trashed, err := transaction.TrashPhotos(ownerId, photoIds); err != nil || trashed != 2 {
			test.Fatalf("trashed %d: %v", trashed, err)
		}
		if marked, err := transaction.MarkPhotosForDeletion(ownerId, photoIds); err != nil || marked != 2 {
			test.Fatalf("marked %d: %v", marked, err)
		}
		if removed, err := transaction.DeleteMarkedPhotos(photoIds); err != nil || removed != 2 {
			test.Fatalf("deleted %d: %v", removed, err)
		}
		vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
		if err != nil || len(vectors) != 1 || vectors[0].ID != shared {
			test.Fatalf("the groups with faces: %+v %v", vectors, err)
		}
		// Only the kept photo's face, at 0 degrees, is in the sum now.
		if math.Abs(float64(vectors[0].VectorSum[0])-1) > 1e-5 || math.Abs(float64(vectors[0].VectorSum[1])) > 1e-5 {
			test.Fatalf("the sum still has the deleted face in it: %v", vectors[0].VectorSum[:2])
		}
		group, err := transaction.GetPhotoFaceGroup(ownerId, shared)
		if err != nil || group.FaceCount != 1 {
			test.Fatalf("%+v %v", group, err)
		}
		if _, err := transaction.GetPhotoFaceGroup(ownerId, loner); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a group with no face left stayed: %v", err)
		}
	})
	if groups := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE user_id = '"+ownerId+"'"); groups != "1" {
		test.Fatalf("%s groups are left", groups)
	}
}
