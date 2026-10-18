package db_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// groupWithFace gives a person a photo with one face in a group of its own,
// and returns the group and the face.
func groupWithFace(test *testing.T, database db.Database, userId, clientPhotoId string, angleDegrees float64) (string, *models.PhotoFace) {
	test.Helper()
	found := readyPhotoWithPreview(test, database, userId, clientPhotoId)
	stored := storeFaces(test, database, userId, found.ID, foundFaceAt(angleDegrees, 0.1))
	return groupOwnFace(test, database, userId, stored[0].ID), stored[0]
}

// nameGroupAnswer is nameGroup with the group that was merged away as well.
func nameGroupAnswer(test *testing.T, database db.Database, userId, photoFaceGroupId, addressBookId, contactId string) (*models.PhotoFaceGroup, string) {
	test.Helper()
	var named *models.PhotoFaceGroup
	var mergedPhotoFaceGroupId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if named, mergedPhotoFaceGroupId, err = transaction.NamePhotoFaceGroup(userId, photoFaceGroupId, addressBookId, contactId); err != nil {
			test.Fatal(err)
		}
	})
	return named, mergedPhotoFaceGroupId
}

func hideGroup(test *testing.T, database db.Database, userId, photoFaceGroupId string, isHidden bool) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.HidePhotoFaceGroup(userId, photoFaceGroupId, isHidden); err != nil {
			test.Fatal(err)
		}
	})
}

// A contact names one person. Naming a group with a contact that names
// another group puts the group named into the one already named, whichever of
// the two is older.
func TestNamingAGroupWithAContactThatNamesAnotherGroupMergesTheTwo(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-person-owner")
	bookId, contactId := keepContact(test, database, ownerId, "Example Cousin")

	// The group named first is older, then the one that is named second is.
	for _, scenario := range []struct {
		name               string
		isNamedFirstOlder  bool
		clientPhotoPrefix  string
		firstAngleDegrees  float64
		secondAngleDegrees float64
	}{
		{"the group already named is the older", true, "older", 0, 30},
		{"the group already named is the younger", false, "younger", 10, 40},
	} {
		var firstGroup, secondGroup string
		var firstFace, secondFace *models.PhotoFace
		if scenario.isNamedFirstOlder {
			firstGroup, firstFace = groupWithFace(test, database, ownerId, scenario.clientPhotoPrefix+"-first", scenario.firstAngleDegrees)
			secondGroup, secondFace = groupWithFace(test, database, ownerId, scenario.clientPhotoPrefix+"-second", scenario.secondAngleDegrees)
		} else {
			secondGroup, secondFace = groupWithFace(test, database, ownerId, scenario.clientPhotoPrefix+"-second", scenario.secondAngleDegrees)
			firstGroup, firstFace = groupWithFace(test, database, ownerId, scenario.clientPhotoPrefix+"-first", scenario.firstAngleDegrees)
		}
		// The first group is the one named already; the cover it has is kept.
		named, mergedId := nameGroupAnswer(test, database, ownerId, firstGroup, bookId, contactId)
		if mergedId != "" || named.ID != firstGroup || named.CoverPhotoFaceID != firstFace.ID {
			test.Fatalf("%s: naming a group nobody else has merged something: %+v %q", scenario.name, named, mergedId)
		}
		// Naming it again with the same contact is not a merge either.
		if named, mergedId = nameGroupAnswer(test, database, ownerId, firstGroup, bookId, contactId); mergedId != "" || named.ID != firstGroup {
			test.Fatalf("%s: %+v %q", scenario.name, named, mergedId)
		}
		merged, mergedId := nameGroupAnswer(test, database, ownerId, secondGroup, bookId, contactId)
		if merged.ID != firstGroup || mergedId != secondGroup {
			test.Fatalf("%s: the group already named stays and the other goes into it: %+v %q", scenario.name, merged, mergedId)
		}
		if merged.FaceCount != 2 || merged.PhotoCount != 2 || merged.ContactID != contactId || merged.CoverPhotoFaceID != firstFace.ID {
			test.Fatalf("%s: %+v", scenario.name, merged)
		}
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			if _, err := transaction.GetPhotoFaceGroup(ownerId, secondGroup); !errors.Is(err, db.ErrNotFound) {
				test.Fatalf("%s: the group that went into the other is still there: %v", scenario.name, err)
			}
			face, err := transaction.GetPhotoFace(ownerId, secondFace.ID)
			if err != nil || face.PhotoFaceGroupID != firstGroup || face.ContactID != contactId {
				test.Fatalf("%s: %+v %v", scenario.name, face, err)
			}
			// The vector of the group that stayed is the sum of both faces.
			vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
			if err != nil {
				test.Fatal(err)
			}
			for _, vector := range vectors {
				if vector.ID != firstGroup {
					continue
				}
				want := math.Cos(scenario.firstAngleDegrees*math.Pi/180) + math.Cos(scenario.secondAngleDegrees*math.Pi/180)
				if math.Abs(float64(vector.VectorSum[0])-want) > 1e-5 {
					test.Fatalf("%s: %v, want %v", scenario.name, vector.VectorSum[0], want)
				}
			}
		})
		// Taking the name off and giving it to a face again starts over.
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			if _, err := transaction.UnnamePhotoFaceGroup(ownerId, firstGroup); err != nil {
				test.Fatal(err)
			}
		})
	}
}

// A person hidden from the list is shown again by naming them with a contact
// that is listed, and a hidden person stays hidden only with a hidden one.
func TestMergingOnNamingHidesThePersonOnlyWhenBothWereHidden(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-person-hidden")
	bookId, contactId := keepContact(test, database, ownerId, "Example Neighbor")

	for _, scenario := range []struct {
		name                 string
		isNamedGroupHidden   bool
		isNewlyNamedHidden   bool
		isMergedGroupHidden  bool
		wantIsHiddenAfterAll bool
	}{
		{"a hidden group is named with a contact that names a listed one", false, true, false, false},
		{"a listed group is named with a contact that names a hidden one", true, false, true, false},
		{"both are hidden", true, true, true, true},
		{"neither is hidden", false, false, false, false},
	} {
		namedGroup, _ := groupWithFace(test, database, ownerId, scenario.name+" named", 0)
		newGroup, _ := groupWithFace(test, database, ownerId, scenario.name+" new", 20)
		nameGroup(test, database, ownerId, namedGroup, bookId, contactId)
		hideGroup(test, database, ownerId, namedGroup, scenario.isNamedGroupHidden)
		hideGroup(test, database, ownerId, newGroup, scenario.isNewlyNamedHidden)
		merged, mergedId := nameGroupAnswer(test, database, ownerId, newGroup, bookId, contactId)
		if merged.ID != namedGroup || mergedId != newGroup || merged.IsHidden != scenario.wantIsHiddenAfterAll {
			test.Fatalf("%s: %+v %q", scenario.name, merged, mergedId)
		}
		listed := listedGroupIds(test, database, ownerId, false)
		isListed := false
		for _, listedId := range listed {
			isListed = isListed || listedId == namedGroup
		}
		if isListed == scenario.wantIsHiddenAfterAll {
			test.Fatalf("%s: listed %v, hidden %v", scenario.name, isListed, scenario.wantIsHiddenAfterAll)
		}
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			if _, err := transaction.UnnamePhotoFaceGroup(ownerId, namedGroup); err != nil {
				test.Fatal(err)
			}
		})
	}
}

// A contact of another person never merges anything: it is refused first.
func TestMergingOnNamingLeavesAnotherPersonsGroupsAlone(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-person-first")
	otherId := dbtest.CreateUser(test, database, "one-person-second")
	ownerGroup, _ := groupWithFace(test, database, ownerId, "owner", 0)
	otherGroup, _ := groupWithFace(test, database, otherId, "other", 0)
	otherBookId, otherContactId := keepContact(test, database, otherId, "Example Friend")
	nameGroup(test, database, otherId, otherGroup, otherBookId, otherContactId)
	err := database.Transaction(func(transaction db.Transaction) error {
		_, _, err := transaction.NamePhotoFaceGroup(ownerId, ownerGroup, otherBookId, otherContactId)
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("%v", err)
	}
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group"); count != "2" {
		test.Fatalf("%s groups", count)
	}
}

// The migration that merges the groups named with one contact before naming
// did, once.
func TestTheMigrationMergesGroupsAlreadyNamedWithOneContact(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-person-migration")
	bookId, aloneContactId := keepContact(test, database, ownerId, "Example Aunt")
	var pairedContactId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		kept, err := transaction.PutContact(&models.Contact{AddressBookID: bookId, UID: "urn:uuid:paired", ETag: "e3", Name: "Example Uncle", Card: "BEGIN:VCARD\nVERSION:3.0\nFN:Example Uncle\nEND:VCARD\n"})
		if err != nil {
			test.Fatal(err)
		}
		pairedContactId = kept.ID
	})
	// Three groups the uncle names, one the aunt names alone, one nobody names.
	oldest, oldestFace := groupWithFace(test, database, ownerId, "migration-oldest", 0)
	middle, middleFace := groupWithFace(test, database, ownerId, "migration-middle", 40)
	youngest, youngestFace := groupWithFace(test, database, ownerId, "migration-youngest", 80)
	aloneGroup, _ := groupWithFace(test, database, ownerId, "migration-alone", 120)
	unnamedGroup, _ := groupWithFace(test, database, ownerId, "migration-unnamed", 160)
	// Written as a database from before would hold them: through the SQL,
	// because naming no longer lets two groups have one contact.
	for _, photoFaceGroupId := range []string{oldest, middle, youngest} {
		dbtest.Exec(test, database, "UPDATE photo_face_group SET addressbook_id = '"+bookId+"', contact_id = '"+pairedContactId+"' WHERE id = '"+photoFaceGroupId+"'")
	}
	dbtest.Exec(test, database, "UPDATE photo_face_group SET addressbook_id = '"+bookId+"', contact_id = '"+aloneContactId+"' WHERE id = '"+aloneGroup+"'")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET created_at = now() - interval '3 hours' WHERE id = '"+oldest+"'")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET created_at = now() - interval '2 hours' WHERE id = '"+middle+"'")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET created_at = now() - interval '1 hours' WHERE id = '"+youngest+"'")
	// The oldest was hidden and the youngest is listed, so the person is.
	hideGroup(test, database, ownerId, oldest, true)
	hideGroup(test, database, ownerId, middle, true)

	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0242_photo_face_group_one_per_contact" {
			migration = candidate
		}
	}
	if migration.ID == "" {
		test.Fatal("the migration is missing")
	}
	dbtest.Exec(test, database, migration.SQL)

	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group"); count != "3" {
		test.Fatalf("three groups are left: the uncle, the aunt and nobody: %s", count)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		kept, err := transaction.GetPhotoFaceGroup(ownerId, oldest)
		if err != nil || kept.ContactID != pairedContactId || kept.FaceCount != 3 || kept.IsHidden {
			test.Fatalf("the oldest group stays with the faces of all, shown because one was: %+v %v", kept, err)
		}
		for _, goneId := range []string{middle, youngest} {
			if _, err := transaction.GetPhotoFaceGroup(ownerId, goneId); !errors.Is(err, db.ErrNotFound) {
				test.Fatalf("%s: %v", goneId, err)
			}
		}
		for _, face := range []*models.PhotoFace{oldestFace, middleFace, youngestFace} {
			moved, err := transaction.GetPhotoFace(ownerId, face.ID)
			if err != nil || moved.PhotoFaceGroupID != oldest {
				test.Fatalf("%+v %v", moved, err)
			}
		}
		alone, err := transaction.GetPhotoFaceGroup(ownerId, aloneGroup)
		if err != nil || alone.ContactID != aloneContactId || alone.FaceCount != 1 {
			test.Fatalf("%+v %v", alone, err)
		}
		if _, err := transaction.GetPhotoFaceGroup(ownerId, unnamedGroup); err != nil {
			test.Fatal(err)
		}
		vectors, err := transaction.ListPhotoFaceGroupVectors(ownerId)
		if err != nil {
			test.Fatal(err)
		}
		for _, vector := range vectors {
			if vector.ID != oldest {
				continue
			}
			want := 1 + math.Cos(40*math.Pi/180) + math.Cos(80*math.Pi/180)
			if math.Abs(float64(vector.VectorSum[0])-want) > 1e-4 {
				test.Fatalf("the vector of the person is the sum of the three faces: %v, want %v", vector.VectorSum[0], want)
			}
		}
	})
	// Run again over what it left it changes nothing.
	dbtest.Exec(test, database, migration.SQL)
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group"); count != "3" {
		test.Fatalf("%s", count)
	}
	if !strings.Contains(migration.ReverseSQL, "SELECT 1") {
		test.Fatalf("the reverse is a no-op the runner can run: %q", migration.ReverseSQL)
	}
}

// The face a contact's picture is cut from is the surest of those that are
// large enough, in the person's own photos, out of the trash.
func TestTheFaceForAContactsPictureIsTheSurestOfTheLargeOnes(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "picture-owner")
	otherId := dbtest.CreateUser(test, database, "picture-other")

	findFace := func(userId, photoFaceGroupId string) (*models.PhotoFace, error) {
		var face *models.PhotoFace
		err := database.Transaction(func(transaction db.Transaction) error {
			var err error
			face, err = transaction.FindPhotoFaceForContactPicture(userId, photoFaceGroupId)
			return err
		})
		return face, err
	}

	large := func(score float64, left float64) *models.PhotoFace {
		face := foundFaceAt(0, left)
		face.BoxWidthFraction, face.BoxHeightFraction, face.DetectionScore = 0.3, 0.4, score
		return face
	}
	tiny := foundFaceAt(0, 0.2)
	tiny.BoxWidthFraction, tiny.BoxHeightFraction, tiny.DetectionScore = 0.05, 0.06, 0.999

	// Only small faces: none qualifies.
	smallPhoto := readyPhotoWithPreview(test, database, ownerId, "picture-small")
	smallStored := storeFaces(test, database, ownerId, smallPhoto.ID, tiny)
	photoFaceGroupId := groupOwnFace(test, database, ownerId, smallStored[0].ID)
	if _, err := findFace(ownerId, photoFaceGroupId); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("a face a twentieth of the picture wide was chosen: %v", err)
	}

	// Among the large ones the surest wins, however small the others' lead.
	firstPhoto := readyPhotoWithPreview(test, database, ownerId, "picture-first")
	secondPhoto := readyPhotoWithPreview(test, database, ownerId, "picture-second")
	firstStored := storeFaces(test, database, ownerId, firstPhoto.ID, large(0.90, 0.1))
	secondStored := storeFaces(test, database, ownerId, secondPhoto.ID, large(0.97, 0.4))
	joinFaceToGroup(test, database, ownerId, firstStored[0].ID, photoFaceGroupId)
	joinFaceToGroup(test, database, ownerId, secondStored[0].ID, photoFaceGroupId)
	face, err := findFace(ownerId, photoFaceGroupId)
	if err != nil || face.ID != secondStored[0].ID || face.PhotoID != secondPhoto.ID {
		test.Fatalf("%+v %v", face, err)
	}

	// The surest face of a photo in the trash is passed over.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, []string{secondPhoto.ID}); err != nil {
			test.Fatal(err)
		}
	})
	if face, err = findFace(ownerId, photoFaceGroupId); err != nil || face.ID != firstStored[0].ID {
		test.Fatalf("%+v %v", face, err)
	}

	// Nobody else can have a picture cut from the person's faces.
	if _, err := findFace(otherId, photoFaceGroupId); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("%v", err)
	}
	if _, err := findFace(ownerId, ""); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("%v", err)
	}

	// A photo with no large preview is passed over as well.
	noPreview := newTestPhoto(test, database, ownerId, "picture-no-preview", nil)
	dbtest.MarkPhotoReady(test, database, noPreview.ID)
	noPreviewStored := storeFaces(test, database, ownerId, noPreview.ID, large(0.99, 0.2))
	joinFaceToGroup(test, database, ownerId, noPreviewStored[0].ID, photoFaceGroupId)
	if face, err = findFace(ownerId, photoFaceGroupId); err != nil || face.ID != firstStored[0].ID {
		test.Fatalf("a face of a photo with no large preview was chosen: %+v %v", face, err)
	}
}

// twoGroupsWithAFaceEachOfOnePhoto makes two groups of one person that each
// have a different face of one photo, and one more face of their own. The
// photo's faces are at 0 and 60 degrees; the first group's other face is at 5
// and the second's at 20, so that the faces of the photo are 5 and 40 degrees
// from the direction of the groups together.
func twoGroupsWithAFaceEachOfOnePhoto(test *testing.T, database db.Database, userId, prefix string) (firstGroup, secondGroup string, firstFace, secondFace *models.PhotoFace) {
	test.Helper()
	shared := readyPhotoWithPreview(test, database, userId, prefix+"-shared")
	both := storeFaces(test, database, userId, shared.ID, foundFaceAt(0, 0.1), foundFaceAt(60, 0.6))
	firstGroup = groupOwnFace(test, database, userId, both[0].ID)
	secondGroup = groupOwnFace(test, database, userId, both[1].ID)
	firstOther := storeFaces(test, database, userId, readyPhotoWithPreview(test, database, userId, prefix+"-first").ID, foundFaceAt(5, 0.1))
	secondOther := storeFaces(test, database, userId, readyPhotoWithPreview(test, database, userId, prefix+"-second").ID, foundFaceAt(20, 0.1))
	joinFaceToGroup(test, database, userId, firstOther[0].ID, firstGroup)
	joinFaceToGroup(test, database, userId, secondOther[0].ID, secondGroup)
	return firstGroup, secondGroup, both[0], both[1]
}

func faceNow(test *testing.T, database db.Database, userId, photoFaceId string) *models.PhotoFace {
	test.Helper()
	var face *models.PhotoFace
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if face, err = transaction.GetPhotoFace(userId, photoFaceId); err != nil {
			test.Fatal(err)
		}
	})
	return face
}

// assertGroupHoldsOnlyTheFacesLeft checks that a group's count and vector are
// those of the faces that are in it.
func assertGroupHoldsOnlyTheFacesLeft(test *testing.T, database db.Database, userId, photoFaceGroupId string, wantFaceCount int, wantAnglesDegrees ...float64) {
	test.Helper()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		group, err := transaction.GetPhotoFaceGroup(userId, photoFaceGroupId)
		if err != nil || group.FaceCount != wantFaceCount {
			test.Fatalf("%+v %v", group, err)
		}
		vectors, err := transaction.ListPhotoFaceGroupVectors(userId)
		if err != nil {
			test.Fatal(err)
		}
		var wantX, wantY float64
		for _, angle := range wantAnglesDegrees {
			wantX += math.Cos(angle * math.Pi / 180)
			wantY += math.Sin(angle * math.Pi / 180)
		}
		for _, vector := range vectors {
			if vector.ID == photoFaceGroupId && (math.Abs(float64(vector.VectorSum[0])-wantX) > 1e-4 || math.Abs(float64(vector.VectorSum[1])-wantY) > 1e-4) {
				test.Fatalf("the group's vector is not the sum of the faces in it: %v, want %v %v", vector.VectorSum[:2], wantX, wantY)
			}
		}
	})
	if count := dbtest.QueryString(test, database, "SELECT face_group_vector_count FROM photo_face_group WHERE id = '"+photoFaceGroupId+"'"); count != fmt.Sprint(wantFaceCount) {
		test.Fatalf("the group counts %s faces", count)
	}
}

// Merging two groups that each have a face of one photo keeps the one face of
// that photo that is most like the group, takes the other out of every group,
// and makes the group's vector again from the faces that stay.
func TestMergingKeepsOneFaceOfAPhotoAndUngroupsTheOther(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-face-merge")

	// By hand, with MergePhotoFaceGroups.
	firstGroup, secondGroup, firstFace, secondFace := twoGroupsWithAFaceEachOfOnePhoto(test, database, ownerId, "by-hand")
	var merged *models.PhotoFaceGroup
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if merged, err = transaction.MergePhotoFaceGroups(ownerId, []string{firstGroup, secondGroup}); err != nil {
			test.Fatal(err)
		}
	})
	if merged.ID != firstGroup || merged.FaceCount != 3 || merged.PhotoCount != 3 {
		test.Fatalf("the merged group has the shared photo once and the two others: %+v", merged)
	}
	if kept := faceNow(test, database, ownerId, firstFace.ID); kept.PhotoFaceGroupID != firstGroup {
		test.Fatalf("the face most like the group was not kept: %+v", kept)
	}
	if left := faceNow(test, database, ownerId, secondFace.ID); left.PhotoFaceGroupID != "" || left.IsGroupConfirmed {
		test.Fatalf("the other face of the photo is still in a group, or confirmed: %+v", left)
	}
	assertGroupHoldsOnlyTheFacesLeft(test, database, ownerId, firstGroup, 3, 0, 5, 20)

	// The face the person confirmed is kept over one that is more like the group.
	firstGroup, secondGroup, firstFace, secondFace = twoGroupsWithAFaceEachOfOnePhoto(test, database, ownerId, "confirmed")
	dbtest.Exec(test, database, "UPDATE photo_face SET is_group_confirmed = true WHERE id = '"+secondFace.ID+"'")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.MergePhotoFaceGroups(ownerId, []string{firstGroup, secondGroup}); err != nil {
			test.Fatal(err)
		}
	})
	if kept := faceNow(test, database, ownerId, secondFace.ID); kept.PhotoFaceGroupID != firstGroup || !kept.IsGroupConfirmed {
		test.Fatalf("the confirmed face was not kept: %+v", kept)
	}
	if left := faceNow(test, database, ownerId, firstFace.ID); left.PhotoFaceGroupID != "" {
		test.Fatalf("%+v", left)
	}
	assertGroupHoldsOnlyTheFacesLeft(test, database, ownerId, firstGroup, 3, 60, 5, 20)

	// Two confirmed faces: the more like the group stays and the other is
	// unconfirmed and out of the group.
	firstGroup, secondGroup, firstFace, secondFace = twoGroupsWithAFaceEachOfOnePhoto(test, database, ownerId, "both-confirmed")
	dbtest.Exec(test, database, "UPDATE photo_face SET is_group_confirmed = true WHERE id IN ('"+firstFace.ID+"', '"+secondFace.ID+"')")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.MergePhotoFaceGroups(ownerId, []string{firstGroup, secondGroup}); err != nil {
			test.Fatal(err)
		}
	})
	if kept := faceNow(test, database, ownerId, firstFace.ID); kept.PhotoFaceGroupID != firstGroup || !kept.IsGroupConfirmed {
		test.Fatalf("%+v", kept)
	}
	if left := faceNow(test, database, ownerId, secondFace.ID); left.PhotoFaceGroupID != "" || left.IsGroupConfirmed {
		test.Fatalf("%+v", left)
	}
}

// Naming a group with the contact of another group merges them the same way.
func TestNamingAGroupAlsoKeepsOneFaceOfAPhotoAndAKeptCoverStaysACover(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-face-naming")
	bookId, contactId := keepContact(test, database, ownerId, "Example Uncle")
	firstGroup, secondGroup, firstFace, secondFace := twoGroupsWithAFaceEachOfOnePhoto(test, database, ownerId, "naming")
	// The face that will leave is the cover of the group it is in.
	dbtest.Exec(test, database, "UPDATE photo_face_group SET cover_photo_face_id = '"+secondFace.ID+"' WHERE id = '"+secondGroup+"'")
	nameGroup(test, database, ownerId, firstGroup, bookId, contactId)
	merged, mergedId := nameGroupAnswer(test, database, ownerId, secondGroup, bookId, contactId)
	if merged.ID != firstGroup || mergedId != secondGroup || merged.FaceCount != 3 {
		test.Fatalf("%+v %q", merged, mergedId)
	}
	if kept := faceNow(test, database, ownerId, firstFace.ID); kept.PhotoFaceGroupID != firstGroup {
		test.Fatalf("%+v", kept)
	}
	if left := faceNow(test, database, ownerId, secondFace.ID); left.PhotoFaceGroupID != "" {
		test.Fatalf("%+v", left)
	}
	if merged.CoverPhotoFaceID == "" || merged.CoverPhotoFaceID == secondFace.ID {
		test.Fatalf("the group is shown by a face that is not in it: %+v", merged)
	}
	assertGroupHoldsOnlyTheFacesLeft(test, database, ownerId, firstGroup, 3, 0, 5, 20)
}

// The migration does the same for groups already named with one contact.
func TestTheMigrationKeepsOneFaceOfAPhotoInTheGroupsItFolds(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "one-face-migration")
	bookId, contactId := keepContact(test, database, ownerId, "Example Aunt")
	firstGroup, secondGroup, firstFace, secondFace := twoGroupsWithAFaceEachOfOnePhoto(test, database, ownerId, "migration")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET addressbook_id = '"+bookId+"', contact_id = '"+contactId+"' WHERE id IN ('"+firstGroup+"', '"+secondGroup+"')")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET created_at = now() - interval '2 hours' WHERE id = '"+firstGroup+"'")
	dbtest.Exec(test, database, "UPDATE photo_face SET is_group_confirmed = true WHERE id = '"+secondFace.ID+"'")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET cover_photo_face_id = '"+firstFace.ID+"' WHERE id = '"+firstGroup+"'")
	// A group of another contact that nothing is folded into, and that
	// (artificially, grouping never does this) holds two faces of one photo:
	// the migration has no reason to touch it.
	otherBookId, otherContactId := keepContact(test, database, ownerId, "Example Uncle")
	untouchedPhoto := readyPhotoWithPreview(test, database, ownerId, "migration-untouched")
	untouchedFaces := storeFaces(test, database, ownerId, untouchedPhoto.ID, foundFaceAt(100, 0.1), foundFaceAt(110, 0.6))
	untouchedGroup := groupOwnFace(test, database, ownerId, untouchedFaces[0].ID)
	dbtest.Exec(test, database, "UPDATE photo_face SET photo_face_group_id = '"+untouchedGroup+"' WHERE id = '"+untouchedFaces[1].ID+"'")
	dbtest.Exec(test, database, "UPDATE photo_face_group SET addressbook_id = '"+otherBookId+"', contact_id = '"+otherContactId+"' WHERE id = '"+untouchedGroup+"'")
	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0242_photo_face_group_one_per_contact" {
			migration = candidate
		}
	}
	dbtest.Exec(test, database, migration.SQL)
	// The person confirmed the second face, so it stays and the first leaves,
	// and the first was the cover, so the group is shown by another face.
	if kept := faceNow(test, database, ownerId, secondFace.ID); kept.PhotoFaceGroupID != firstGroup || !kept.IsGroupConfirmed {
		test.Fatalf("%+v", kept)
	}
	if left := faceNow(test, database, ownerId, firstFace.ID); left.PhotoFaceGroupID != "" || left.IsGroupConfirmed {
		test.Fatalf("%+v", left)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		group, err := transaction.GetPhotoFaceGroup(ownerId, firstGroup)
		if err != nil || group.FaceCount != 3 || group.CoverPhotoFaceID == firstFace.ID || group.CoverPhotoFaceID == "" {
			test.Fatalf("%+v %v", group, err)
		}
	})
	assertGroupHoldsOnlyTheFacesLeft(test, database, ownerId, firstGroup, 3, 60, 5, 20)
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE id = '"+firstGroup+"'"); count != "1" {
		test.Fatalf("%s groups of the person", count)
	}
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE id = '"+secondGroup+"'"); count != "0" {
		test.Fatalf("the group that was folded is still there")
	}
	for _, face := range untouchedFaces {
		if kept := faceNow(test, database, ownerId, face.ID); kept.PhotoFaceGroupID != untouchedGroup {
			test.Fatalf("a group nothing was folded into lost a face: %+v", kept)
		}
	}
}
