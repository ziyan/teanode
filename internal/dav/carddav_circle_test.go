package dav_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// circleBook makes a circle with the given first admin and members and gives
// it an address book, returning the circle and the book.
func (self *world) circleBook(t *testing.T, adminUserID string, memberUserIDs ...string) (*models.Circle, *models.AddressBook) {
	t.Helper()
	var circle *models.Circle
	var book *models.AddressBook
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if circle, err = tx.CreateCircle(&models.Circle{Name: "Family", CircleKind: models.CircleKindFamily}, adminUserID); err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		for _, memberUserID := range memberUserIDs {
			if _, err := tx.AddCircleMember(circle.ID, memberUserID, models.MemberRoleMember, adminUserID); err != nil {
				t.Fatalf("AddCircleMember: %s", err)
			}
		}
		if book, err = tx.CreateCircleAddressBook(circle.ID, ""); err != nil {
			t.Fatalf("CreateCircleAddressBook: %s", err)
		}
	})
	return circle, book
}

// grantCircleUse gives the signed-in person circle:use, which a circle's book
// needs on top of contacts:use.
func (self *world) grantCircleUse(t *testing.T) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, self.userID, models.PermissionCircleUse)
	})
}

// A circle's address book is another book in the home set, read and written
// like the person's own, and cards put in by another member are listed.
func TestACircleAddressBookIsAnotherBookOnThePhone(t *testing.T) {
	here, done := newWorld(t)
	defer done()
	here.grantCircleUse(t)
	_, book := here.circleBook(t, here.other, here.userID)

	listed := here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/", dav.Prefix, here.userID), propfindETags, "Depth", "1")
	body := text(t, listed)
	if listed.StatusCode != http.StatusMultiStatus || !strings.Contains(body, here.bookID) || !strings.Contains(body, book.ID) {
		t.Fatalf("the home set lists both books: %d\n%s", listed.StatusCode, body)
	}

	where := fmt.Sprintf("%s/%s/contacts/%s/ada.vcf", dav.Prefix, here.userID, book.ID)
	put := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard")
	_ = text(t, put)
	if put.StatusCode >= 400 {
		t.Fatalf("a member puts a card in the circle's book: %d", put.StatusCode)
	}
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		if _, err := tx.PutContact(&models.Contact{AddressBookID: book.ID, ID: "kim", UID: "urn:uuid:kim", ETag: "e", Card: strings.ReplaceAll(strings.ReplaceAll(aCard, "ada", "kim"), "Ada Lovelace", "Kim Example"), Name: "Kim Example"}); err != nil {
			t.Fatalf("PutContact: %s", err)
		}
	})
	listed = here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, book.ID), propfindETags, "Depth", "1")
	body = text(t, listed)
	if listed.StatusCode != http.StatusMultiStatus || !strings.Contains(body, "ada.vcf") || !strings.Contains(body, "kim.vcf") {
		t.Fatalf("the book lists a card each member put in: %d\n%s", listed.StatusCode, body)
	}
	got := here.ask(t, http.MethodGet, fmt.Sprintf("%s/%s/contacts/%s/kim.vcf", dav.Prefix, here.userID, book.ID), "")
	if card := text(t, got); got.StatusCode != http.StatusOK || !strings.Contains(card, "FN:Kim Example") {
		t.Fatalf("reading another member's card: %d\n%s", got.StatusCode, card)
	}
	removed := here.ask(t, http.MethodDelete, fmt.Sprintf("%s/%s/contacts/%s/kim.vcf", dav.Prefix, here.userID, book.ID), "")
	_ = text(t, removed)
	if removed.StatusCode >= 400 {
		t.Fatalf("removing another member's card: %d", removed.StatusCode)
	}

	// The book itself is made and removed in the dashboard, not by a phone.
	deleted := here.ask(t, http.MethodDelete, fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, book.ID), "")
	_ = text(t, deleted)
	if deleted.StatusCode != http.StatusForbidden {
		t.Fatalf("a phone cannot delete a circle's book: %d", deleted.StatusCode)
	}
}

// Leaving the circle takes its book off the phone's listing and refuses every
// request into it at once; a circle one is not in is not there at all.
func TestLeavingTheCircleEndsAccessToItsAddressBook(t *testing.T) {
	here, done := newWorld(t)
	defer done()
	here.grantCircleUse(t)
	circle, book := here.circleBook(t, here.other, here.userID)
	_, strangers := here.circleBook(t, here.other)

	where := fmt.Sprintf("%s/%s/contacts/%s/ada.vcf", dav.Prefix, here.userID, book.ID)
	put := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard")
	_ = text(t, put)
	if put.StatusCode >= 400 {
		t.Fatalf("a member puts a card in: %d", put.StatusCode)
	}

	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(circle.ID, here.userID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
	})
	for name, attempt := range map[string]*http.Response{
		"list":   here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, book.ID), propfindETags, "Depth", "1"),
		"read":   here.ask(t, http.MethodGet, where, ""),
		"write":  here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard"),
		"remove": here.ask(t, http.MethodDelete, where, ""),
		"never":  here.ask(t, http.MethodPut, fmt.Sprintf("%s/%s/contacts/%s/ada.vcf", dav.Prefix, here.userID, strangers.ID), aCard, "Content-Type", "text/vcard"),
	} {
		_ = text(t, attempt)
		if attempt.StatusCode != http.StatusNotFound {
			t.Errorf("%s in a book one is not in: %d, wanted 404", name, attempt.StatusCode)
		}
	}
	listed := here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/", dav.Prefix, here.userID), propfindETags, "Depth", "1")
	body := text(t, listed)
	if strings.Contains(body, book.ID) || strings.Contains(body, strangers.ID) || !strings.Contains(body, here.bookID) {
		t.Fatalf("the home set has only the person's own book now:\n%s", body)
	}
}

// A member whose roles lack circle:use does not see the circle's book on the
// phone, and every request into it is a 404, until the permission is granted.
func TestACircleAddressBookNeedsCircleUseOnThePhone(t *testing.T) {
	here, done := newWorld(t)
	defer done()
	_, book := here.circleBook(t, here.other, here.userID)

	where := fmt.Sprintf("%s/%s/contacts/%s/ada.vcf", dav.Prefix, here.userID, book.ID)
	for name, attempt := range map[string]*http.Response{
		"list":  here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, book.ID), propfindETags, "Depth", "1"),
		"write": here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard"),
	} {
		_ = text(t, attempt)
		if attempt.StatusCode != http.StatusNotFound {
			t.Errorf("%s without circle:use: %d, wanted 404", name, attempt.StatusCode)
		}
	}
	listed := here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/", dav.Prefix, here.userID), propfindETags, "Depth", "1")
	if body := text(t, listed); strings.Contains(body, book.ID) || !strings.Contains(body, here.bookID) {
		t.Fatalf("the home set has only the person's own book without circle:use:\n%s", body)
	}

	here.grantCircleUse(t)
	put := here.ask(t, http.MethodPut, where, aCard, "Content-Type", "text/vcard")
	_ = text(t, put)
	if put.StatusCode >= 400 {
		t.Fatalf("a member with circle:use puts a card in: %d", put.StatusCode)
	}
}

// A person's own book is not reachable through a circle they share with its
// owner.
func TestAPersonalAddressBookIsNeverSharedThroughACircle(t *testing.T) {
	here, done := newWorld(t)
	defer done()
	here.circleBook(t, here.userID, here.other)
	var otherBook *models.AddressBook
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		var err error
		if otherBook, err = tx.CreateAddressBook(&models.AddressBook{UserID: here.other, Name: "Private"}); err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
	})
	answer := here.ask(t, "PROPFIND", fmt.Sprintf("%s/%s/contacts/%s/", dav.Prefix, here.userID, otherBook.ID), propfindETags, "Depth", "1")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusNotFound {
		t.Fatalf("a circle mate's own book: %d, wanted 404", answer.StatusCode)
	}
}
