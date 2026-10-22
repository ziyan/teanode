package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A circle has one address book, made by a member, edited by every member and
// managed by an admin; a person outside the circle reaches nothing, and a
// person who leaves stops reaching it at once.
func TestACircleAddressBookBelongsToItsMembers(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newContactAccountWorld(t, tx)
		family, err := tx.CreateCircleAddressBook(world.circle.ID, "")
		if err != nil {
			t.Fatalf("CreateCircleAddressBook: %s", err)
		}
		if family.CircleID != world.circle.ID || family.UserID != "" || family.Name != "Home" || family.AgentGranted {
			t.Fatalf("a circle book belongs to the circle and is called what the circle is: %+v", family)
		}
		if _, err := tx.CreateCircleAddressBook(world.circle.ID, "Again"); !errors.Is(err, db.ErrAlreadyExists) {
			t.Fatalf("a circle has one: %v", err)
		}
		if _, err := tx.CreateCircleAddressBook("nobody", ""); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("no such circle: %v", err)
		}
		found, err := tx.GetCircleAddressBook(world.circle.ID)
		if err != nil || found == nil || found.ID != family.ID {
			t.Fatalf("GetCircleAddressBook: %v %v", found, err)
		}

		accessOf := func(userId string) models.AddressBookAccess {
			access, err := tx.AddressBookAccess(userId, family)
			if err != nil {
				t.Fatalf("AddressBookAccess: %s", err)
			}
			return access
		}
		if access := accessOf(world.owner.ID); !access.CanEdit || !access.CanManage || access.IsOwner {
			t.Errorf("an admin edits and manages, and owns nothing: %+v", access)
		}
		if access := accessOf(world.sam.ID); !access.CanEdit || access.CanManage || access.IsOwner {
			t.Errorf("a member edits only: %+v", access)
		}
		if access := accessOf(world.stranger.ID); access.CanEdit || access.CanManage || access.IsOwner {
			t.Errorf("a stranger has nothing: %+v", access)
		}
		if access, _ := tx.AddressBookAccess("", family); access.CanEdit {
			t.Errorf("nobody has nothing: %+v", access)
		}
		if access, _ := tx.AddressBookAccess(world.sam.ID, nil); access.CanEdit {
			t.Errorf("no book has nothing: %+v", access)
		}
		if access, err := tx.AddressBookAccess(world.owner.ID, world.book); err != nil || !access.IsOwner || !access.CanManage || !access.CanEdit {
			t.Errorf("a person's own book is theirs: %+v %v", access, err)
		}
		if access, _ := tx.AddressBookAccess(world.sam.ID, world.book); access.CanEdit {
			t.Errorf("a person's own book is never shared with a circle mate: %+v", access)
		}

		// Cards added by anybody sit in the one book.
		first := putTestContact(t, tx, family.ID, "urn:uuid:first", "First")
		second := putTestContact(t, tx, family.ID, "urn:uuid:second", "Second")
		if first.ID == "" || second.ID == "" {
			t.Fatalf("the cards are kept")
		}

		// The reachable list has the person's own and the circle's, with the
		// circle's name, and the own list leaves the circle's out.
		reachable, err := tx.ListReachableAddressBooks(world.sam.ID)
		if err != nil || len(reachable) != 1 || reachable[0].Book.ID != family.ID || reachable[0].CircleName != "Home" ||
			!reachable[0].Access.CanEdit || reachable[0].Access.CanManage {
			t.Fatalf("sam reaches the circle's book: %+v %+v %v", reachable, reachable[0].Book, err)
		}
		reachable, err = tx.ListReachableAddressBooks(world.owner.ID)
		if err != nil || len(reachable) != 2 || reachable[0].Book.ID != world.book.ID || !reachable[0].Access.IsOwner ||
			reachable[1].Book.ID != family.ID || !reachable[1].Access.CanManage || reachable[1].Access.IsOwner {
			t.Fatalf("the owner reaches their own, then the circle's: %+v %v", reachable, err)
		}
		own, err := tx.ListAddressBooks(world.owner.ID)
		if err != nil || len(own) != 1 || own[0].ID != world.book.ID {
			t.Fatalf("a circle's book is not the owner's own: %+v %v", own, err)
		}
		if reachable, _ = tx.ListReachableAddressBooks(world.stranger.ID); len(reachable) != 0 {
			t.Errorf("a stranger reaches nothing: %+v", reachable)
		}

		// Sam leaves; he stops reaching it, and what he added stays.
		if _, err := tx.RemoveCircleMember(world.circle.ID, world.sam.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if access := accessOf(world.sam.ID); access.CanEdit {
			t.Errorf("a member who left: %+v", access)
		}
		if reachable, _ = tx.ListReachableAddressBooks(world.sam.ID); len(reachable) != 0 {
			t.Errorf("a member who left reaches nothing: %+v", reachable)
		}
		if kept, err := tx.GetContact(family.ID, first.ID); err != nil || kept == nil {
			t.Errorf("what he added stays: %v %v", kept, err)
		}
	})
}

// Deleting the circle deletes its book and every card in it.
func TestDeletingACircleDeletesItsAddressBook(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var bookId, contactId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newContactAccountWorld(t, tx)
		family, err := tx.CreateCircleAddressBook(world.circle.ID, "Family")
		if err != nil {
			t.Fatalf("CreateCircleAddressBook: %s", err)
		}
		bookId = family.ID
		contactId = putTestContact(t, tx, family.ID, "urn:uuid:first", "First").ID
		if err := tx.DeleteCircle(world.circle.ID); err != nil {
			t.Fatalf("DeleteCircle: %s", err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if book, err := tx.GetAddressBook(bookId); err != nil || book != nil {
			t.Errorf("the book went with the circle: %v %v", book, err)
		}
		if contact, err := tx.GetContact(bookId, contactId); err != nil || contact != nil {
			t.Errorf("the cards went with it: %v %v", contact, err)
		}
	})
}

// What is owner only never reads a circle's book: the known-sender check, the
// account link, the suggestions, and the agent's grant.
func TestOwnerOnlyReadsIgnoreACircleAddressBook(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newContactAccountWorld(t, tx)
		family, err := tx.CreateCircleAddressBook(world.circle.ID, "Family")
		if err != nil {
			t.Fatalf("CreateCircleAddressBook: %s", err)
		}
		inFamily := putTestContact(t, tx, family.ID, "urn:uuid:kim", "Kim", "kim@example.test")

		for _, member := range []*models.User{world.owner, world.sam} {
			if found, err := tx.FindContactByAddress(member.ID, "kim@example.test"); err != nil || found != nil {
				t.Errorf("%s does not know a sender from the circle's book: %+v %v", member.Username, found, err)
			}
		}
		if err := tx.SetContactAccount(family.ID, inFamily.ID, world.sam.ID); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a circle card is never linked to an account: %v", err)
		}
		if links, err := tx.ListContactAccountLinks(world.owner.ID); err != nil || len(links) != 0 {
			t.Errorf("no links: %+v %v", links, err)
		}
		if suggestions, err := tx.ListContactAccountSuggestions(world.owner.ID); err != nil || len(suggestions) != 0 {
			t.Errorf("no suggestions: %+v %v", suggestions, err)
		}

		// Renaming keeps it out of every agent, whatever the caller says.
		family.Name = "Everyone"
		family.AgentGranted = true
		renamed, err := tx.UpdateAddressBook(family)
		if err != nil || renamed.Name != "Everyone" || renamed.AgentGranted || renamed.CircleID != world.circle.ID || renamed.UserID != "" {
			t.Errorf("a circle book is renamed and never granted: %+v %v", renamed, err)
		}
	})
}
