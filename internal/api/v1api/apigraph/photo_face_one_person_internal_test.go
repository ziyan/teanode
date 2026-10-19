package apigraph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// existingContactPicture is a one pixel PNG, as a phone would have put on a
// card: what is on a card already is never replaced.
const existingContactPicture = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// keepContactWithCard keeps a contact whose card is given, in a new address
// book of the person's.
func (self *photoWorld) keepContactWithCard(userId, name, card string) string {
	self.test.Helper()
	var contactId string
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId})
		if err != nil {
			self.test.Fatal(err)
		}
		parsed, err := contacts.Parse([]byte(card))
		if err != nil {
			self.test.Fatal(err)
		}
		kept, err := transaction.PutContact(&models.Contact{AddressBookID: book.ID, UID: parsed.UID, ETag: contacts.ETag(parsed.Card), Name: name, Card: string(parsed.Card)})
		if err != nil {
			self.test.Fatal(err)
		}
		contactId = kept.ID
	})
	return contactId
}

// cardOf is the card a contact has now.
func (self *photoWorld) cardOf(userId, contactId string) string {
	self.test.Helper()
	var card string
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		books, err := transaction.ListAddressBooks(userId)
		if err != nil {
			self.test.Fatal(err)
		}
		for _, book := range books {
			contact, err := transaction.GetContact(book.ID, contactId)
			if err != nil {
				self.test.Fatal(err)
			}
			if contact != nil {
				card = contact.Card
			}
		}
	})
	if card == "" {
		self.test.Fatalf("no contact %s", contactId)
	}
	return card
}

func (self *photoWorld) contactHasPicture(userId, contactId string) bool {
	self.test.Helper()
	return contacts.HasPhoto([]byte(self.cardOf(userId, contactId)))
}

// faceWorld is a person with photos that have a face in them, in groups.
func newFaceWorld(test *testing.T, username string) (*photoWorld, string, []models.Permission) {
	world := newPhotoWorld(test)
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Contacts.IsEnabled = new(true)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	permissions := []models.Permission{models.PermissionPhotosUse, models.PermissionContactsUse}
	return world, world.person(username, permissions...), permissions
}

// groupOfPhotos makes a group of faces, one in each of the photos it stores.
func (self *photoWorld) groupOfPhotos(userId, prefix string, previewContent []byte, photoCount int, angleDegrees float64) string {
	self.test.Helper()
	var photoFaceGroupId string
	for index := range photoCount {
		stored := self.storePhoto(userId, fmt.Sprintf("%s-%d", prefix, index), fmt.Sprintf("%s-%d.jpg", prefix, index), []byte(fmt.Sprintf("original %s %d", prefix, index)),
			map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: previewContent})
		_, photoFaceGroupId = self.storeFaceOf(userId, stored.ID, photoFaceGroupId, angleDegrees)
	}
	return photoFaceGroupId
}

func TestNamingAGroupWithAContactThatNamesAnotherGroupMergesThemAndSaysSo(test *testing.T) {
	world, ownerId, permissions := newFaceWorld(test, "one-person-graph")
	preview := facePictureJpeg(test)
	firstGroup := world.groupOfPhotos(ownerId, "first", preview, 3, 0)
	secondGroup := world.groupOfPhotos(ownerId, "second", preview, 2, 80)
	contactId := world.keepContact(ownerId, "Example Aunt")
	otherContactId := world.keepContact(ownerId, "Example Uncle")

	world.ask(ownerId, permissions, func(ctx context.Context) {
		first, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: firstGroup, ContactID: contactId})
		if err != nil || first.MergedPhotoFaceGroupID != "" || first.PhotoFaceGroup.ID != firstGroup {
			test.Fatalf("%+v %v", first, err)
		}
		// A different contact does not merge anything.
		other, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: secondGroup, ContactID: otherContactId})
		if err != nil || other.MergedPhotoFaceGroupID != "" || other.PhotoFaceGroup.ID != secondGroup {
			test.Fatalf("%+v %v", other, err)
		}
		// Naming the second group with the first's contact makes them one
		// person: the first group stays, with all five faces.
		merged, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: secondGroup, ContactID: contactId})
		if err != nil {
			test.Fatal(err)
		}
		if merged.PhotoFaceGroup.ID != firstGroup || merged.MergedPhotoFaceGroupID != secondGroup ||
			merged.PhotoFaceGroup.FaceCount != 5 || merged.PhotoFaceGroup.ContactID != contactId {
			test.Fatalf("%+v %+v", merged, merged.PhotoFaceGroup)
		}
		// The contact had its picture from the first naming, so this one gives
		// it nothing.
		if merged.IsContactPictureSet {
			test.Fatal("a contact with a picture was given another")
		}
		if _, err := world.graph.ReadPhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: secondGroup}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("the group that was put into the other is still there: %v", err)
		}
		listed, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{})
		if err != nil || len(listed) != 1 || listed[0].ID != firstGroup || listed[0].FaceCount != 5 {
			test.Fatalf("%+v %v", listed, err)
		}
		// Her photos are the photos of both.
		photos, err := world.graph.ListPhotos(ctx, ListPhotosArguments{ContactID: contactId})
		if err != nil || len(photos) != 5 {
			test.Fatalf("%v %v", photos, err)
		}
	})
}

func TestNamingAGroupGivesTheContactAPictureFromAFaceOnlyWhenItHasNone(test *testing.T) {
	world, ownerId, permissions := newFaceWorld(test, "contact-picture-graph")
	preview := facePictureJpeg(test)
	groupWithoutPicture := world.groupOfPhotos(ownerId, "without", preview, 3, 0)
	groupWithPicture := world.groupOfPhotos(ownerId, "with", preview, 3, 90)
	withoutPictureId := world.keepContact(ownerId, "Example Cousin")
	// A version 3 card, as a phone sends one.
	withPictureCard := "BEGIN:VCARD\nVERSION:3.0\nFN:Example Niece\nNOTE:Likes boats\nPHOTO;ENCODING=b;TYPE=PNG:" + existingContactPicture + "\nEND:VCARD\n"
	withPictureId := world.keepContactWithCard(ownerId, "Example Niece", withPictureCard)
	cardBefore := world.cardOf(ownerId, withPictureId)

	world.ask(ownerId, permissions, func(ctx context.Context) {
		named, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupWithoutPicture, ContactID: withoutPictureId})
		if err != nil || !named.IsContactPictureSet {
			test.Fatalf("%+v %v", named, err)
		}
		kept, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupWithPicture, ContactID: withPictureId})
		if err != nil || kept.IsContactPictureSet || kept.PhotoFaceGroup.ContactID != withPictureId {
			test.Fatalf("a contact with a picture was given another, or the naming failed: %+v %v", kept, err)
		}
	})
	if world.cardOf(ownerId, withPictureId) != cardBefore {
		test.Fatal("the card of a contact that had a picture changed")
	}

	// The picture is a JPEG on the card, as large as the face with its room,
	// and square.
	card := []byte(world.cardOf(ownerId, withoutPictureId))
	picture, mediaType, err := contacts.Photo(card)
	if err != nil || mediaType != "image/jpeg" {
		test.Fatalf("%s %v", mediaType, err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(picture))
	if err != nil {
		test.Fatal(err)
	}
	// The face in the test photo is 100 pixels wide and tall, and 40 percent
	// of room on each side makes 180.
	if bounds := decoded.Bounds(); bounds.Dx() != 180 || bounds.Dy() != 180 {
		test.Fatalf("%v", bounds)
	}
	if !strings.Contains(string(card), "FN:Example Cousin") {
		test.Fatalf("the rest of the card is as it was: %s", card[:min(len(card), 200)])
	}
}

func TestNamingAGroupWithNoFaceLargeEnoughOrNoReadablePreviewLeavesTheContactAsItWas(test *testing.T) {
	world, ownerId, permissions := newFaceWorld(test, "contact-picture-none")
	smallFaceGroup := ""
	for index := range 3 {
		stored := world.storePhoto(ownerId, fmt.Sprintf("small-%d", index), fmt.Sprintf("small-%d.jpg", index), []byte(fmt.Sprintf("small original %d", index)),
			map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: facePictureJpeg(test)})
		var stage []*models.PhotoFace
		dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
			var err error
			stage, err = transaction.StorePhotoFaces(ownerId, stored.ID, []*models.PhotoFace{{
				BoxLeftFraction: 0.4, BoxTopFraction: 0.4, BoxWidthFraction: 0.05, BoxHeightFraction: 0.05,
				DetectionScore: 0.99, FaceVector: faceVectorTowards(0),
			}})
			if err != nil {
				test.Fatal(err)
			}
			if smallFaceGroup == "" {
				smallFaceGroup, err = transaction.StartPhotoFaceGroup(ownerId, stage[0].ID)
			} else {
				err = transaction.JoinPhotoFaceGroup(ownerId, stage[0].ID, smallFaceGroup)
			}
			if err != nil {
				test.Fatal(err)
			}
		})
	}
	// Faces large enough, in photos whose large preview is not a picture.
	brokenGroup := world.groupOfPhotos(ownerId, "broken", []byte("this is not a picture"), 3, 90)
	smallContactId := world.keepContact(ownerId, "Example Friend")
	brokenContactId := world.keepContact(ownerId, "Example Colleague")

	world.ask(ownerId, permissions, func(ctx context.Context) {
		for _, scenario := range []struct{ groupId, contactId string }{{smallFaceGroup, smallContactId}, {brokenGroup, brokenContactId}} {
			named, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: scenario.groupId, ContactID: scenario.contactId})
			if err != nil || named.IsContactPictureSet || named.PhotoFaceGroup.ContactID != scenario.contactId {
				test.Fatalf("the naming goes through without a picture: %+v %v", named, err)
			}
		}
	})
	if world.contactHasPicture(ownerId, smallContactId) || world.contactHasPicture(ownerId, brokenContactId) {
		test.Fatal("a picture was made from nothing")
	}
}

func TestRemovingAContactsPictureLeavesTheRestOfTheCard(test *testing.T) {
	world, ownerId, permissions := newFaceWorld(test, "remove-picture-owner")
	otherId := world.person("remove-picture-other", permissions...)
	card := "BEGIN:VCARD\nVERSION:3.0\nFN:Example Niece\nNOTE:Likes boats\nEMAIL:niece@example.com\nPHOTO;ENCODING=b;TYPE=PNG:" + existingContactPicture + "\nEND:VCARD\n"
	withPictureId := world.keepContactWithCard(ownerId, "Example Niece", card)
	withoutPictureId := world.keepContact(ownerId, "Example Cousin")
	foreignId := world.keepContact(otherId, "Foreign Person")
	cardBefore := world.cardOf(ownerId, withoutPictureId)

	world.ask(ownerId, permissions, func(ctx context.Context) {
		view, err := world.graph.RemoveContactPicture(ctx, ContactArguments{ID: withPictureId})
		if err != nil || view.HasPhoto || view.Note != "Likes boats" || len(view.Emails) != 1 {
			test.Fatalf("%+v %v", view, err)
		}
		// Again, and for a contact that had none, it changes nothing.
		if again, err := world.graph.RemoveContactPicture(ctx, ContactArguments{ID: withPictureId}); err != nil || again.HasPhoto || again.ETag != view.ETag {
			test.Fatalf("%+v %v", again, err)
		}
		if view, err := world.graph.RemoveContactPicture(ctx, ContactArguments{ID: withoutPictureId}); err != nil || view.HasPhoto {
			test.Fatalf("%+v %v", view, err)
		}
		if _, err := world.graph.RemoveContactPicture(ctx, ContactArguments{ID: foreignId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's contact: %v", err)
		}
	})
	if world.contactHasPicture(ownerId, withPictureId) || !strings.Contains(world.cardOf(ownerId, withPictureId), "NOTE:Likes boats") {
		test.Fatal("the picture is still on the card, or the rest of it is gone")
	}
	if world.cardOf(ownerId, withoutPictureId) != cardBefore {
		test.Fatal("a card with no picture was rewritten")
	}
	// Without the right to use Contacts there is no way in.
	world.ask(ownerId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		if _, err := world.graph.RemoveContactPicture(ctx, ContactArguments{ID: withPictureId}); err == nil {
			test.Fatal("a person who may not use Contacts removed a picture")
		}
	})
}

// A group named by a contact the owner linked to somebody in their circles
// says who, and the photos of that account are the photos of the group; the
// person it names asks for the same account and finds nothing of the owner's.
func TestAGroupNamedByALinkedContactSaysWhoAndListsTheirPhotos(test *testing.T) {
	world, ownerId, permissions := newFaceWorld(test, "linked-person-owner")
	samId := world.person("linked-person-sam", models.PermissionPhotosUse, models.PermissionContactsUse)
	world.circleOf("Home", ownerId, samId)
	preview := facePictureJpeg(test)
	groupId := world.groupOfPhotos(ownerId, "linked", preview, 3, 0)
	otherGroupId := world.groupOfPhotos(ownerId, "unlinked", preview, 2, 90)
	contactId := world.keepContact(ownerId, "Example Sam")
	otherContactId := world.keepContact(ownerId, "Example Stranger")
	var bookId string
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		books, err := transaction.ListAddressBooks(ownerId)
		if err != nil {
			test.Fatal(err)
		}
		for _, book := range books {
			if contact, _ := transaction.GetContact(book.ID, contactId); contact != nil {
				bookId = book.ID
			}
		}
	})

	world.ask(ownerId, permissions, func(ctx context.Context) {
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: contactId}); err != nil {
			test.Fatal(err)
		}
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: otherGroupId, ContactID: otherContactId}); err != nil {
			test.Fatal(err)
		}
		// Before the link nothing is said, and the account lists nothing.
		before, err := world.graph.ReadPhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: groupId})
		if err != nil || before.AccountUserID != nil || before.Account != nil {
			test.Fatalf("%+v %v", before, err)
		}
		if photos, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AccountUserID: samId}); err != nil || len(photos) != 0 {
			test.Fatalf("%v %v", photos, err)
		}

		if _, err := world.graph.SetContactAccount(ctx, SetContactAccountArguments{AddressBookID: bookId, ContactID: contactId, AccountUserID: samId}); err != nil {
			test.Fatal(err)
		}
		groups, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{})
		if err != nil {
			test.Fatal(err)
		}
		sawLinked, sawUnlinked := false, false
		for _, group := range groups {
			switch group.ID {
			case groupId:
				sawLinked = group.AccountUserID != nil && *group.AccountUserID == samId && group.Account != nil && group.Account.Username == "linked-person-sam"
			case otherGroupId:
				sawUnlinked = group.AccountUserID == nil && group.Account == nil
			}
		}
		if !sawLinked || !sawUnlinked {
			test.Fatalf("the linked group says who and the other does not: %+v", groups)
		}
		photos, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AccountUserID: samId})
		if err != nil || len(photos) != 3 {
			test.Fatalf("the photos of the linked account are the group's: %d %v", len(photos), err)
		}
		if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{AccountUserID: samId}); err != nil || count != 3 {
			test.Fatalf("%d %v", count, err)
		}
	})

	// Sam asking for the owner's account finds none of the owner's photos.
	world.ask(samId, []models.Permission{models.PermissionPhotosUse, models.PermissionContactsUse}, func(ctx context.Context) {
		photos, err := world.graph.ListPhotos(ctx, ListPhotosArguments{AccountUserID: ownerId})
		if err != nil || len(photos) != 0 {
			test.Fatalf("the person named finds the owner's photos: %v %v", photos, err)
		}
	})
}
