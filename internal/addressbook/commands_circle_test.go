package addressbook

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/contacts"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type circleBookFixture struct {
	database                 db.Database
	admin, member, outsider  *access.Principal
	withoutCircleUse         *access.Principal
	circleBook, adminOwnBook *models.AddressBook
}

func circlePrincipal(userId string) *access.Principal {
	return &access.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionContactsUse}})}
}

func newCircleBookFixture(test *testing.T) *circleBookFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	fixture := &circleBookFixture{database: database}
	adminId := dbtest.CreateUser(test, database, "book-admin")
	memberId := dbtest.CreateUser(test, database, "book-member")
	outsiderId := dbtest.CreateUser(test, database, "book-outsider")
	withoutCircleUseId := dbtest.CreateUser(test, database, "book-without-circle-use")
	fixture.admin, fixture.member, fixture.outsider = circlePrincipal(adminId), circlePrincipal(memberId), circlePrincipal(outsiderId)
	fixture.withoutCircleUse = circlePrincipal(withoutCircleUseId)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		circle, err := transaction.CreateCircle(&models.Circle{Name: "Family", CircleKind: models.CircleKindFamily}, adminId)
		if err != nil {
			test.Fatal(err)
		}
		for _, each := range []string{memberId, withoutCircleUseId} {
			if _, err := transaction.AddCircleMember(circle.ID, each, models.MemberRoleMember, adminId); err != nil {
				test.Fatal(err)
			}
		}
		// A circle's book is reached with circle:use as well as contacts:use.
		dbtest.GrantPermission(test, transaction, adminId, models.PermissionCircleUse)
		dbtest.GrantPermission(test, transaction, memberId, models.PermissionCircleUse)
		if fixture.circleBook, err = transaction.CreateCircleAddressBook(circle.ID, ""); err != nil {
			test.Fatal(err)
		}
		if fixture.adminOwnBook, err = transaction.CreateAddressBook(&models.AddressBook{UserID: adminId, Name: "Mine"}); err != nil {
			test.Fatal(err)
		}
	})
	return fixture
}

// Every member adds cards to the circle's book and edits and removes each
// other's; somebody outside the circle can do none of it, and a personal book
// is not reached through the circle.
func TestEveryMemberEditsACircleAddressBook(test *testing.T) {
	fixture := newCircleBookFixture(test)
	added, err := New(fixture.database).Save(test.Context(), fixture.member, SaveRequest{AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Name: new("Kim")}})
	if err != nil {
		test.Fatalf("a member adds a card: %s", err)
	}
	edited, err := New(fixture.database).Save(test.Context(), fixture.admin, SaveRequest{ID: added.ID, AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Note: new("Neighbor")}})
	if err != nil || edited.Name != "Kim" {
		test.Fatalf("another member edits it and the name stays: %+v %v", edited, err)
	}
	if _, err := New(fixture.database).Save(test.Context(), fixture.outsider, SaveRequest{AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Name: new("Sam")}}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("an outsider adds nothing: %v", err)
	}
	if _, err := New(fixture.database).Save(test.Context(), fixture.outsider, SaveRequest{ID: added.ID, Fields: contacts.Fields{Name: new("Sam")}}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("an outsider edits nothing: %v", err)
	}
	if _, err := New(fixture.database).Save(test.Context(), fixture.member, SaveRequest{AddressBookID: fixture.adminOwnBook.ID, Fields: contacts.Fields{Name: new("Sam")}}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("a circle mate's own book is not theirs to write: %v", err)
	}
	if err := New(fixture.database).Delete(test.Context(), fixture.outsider, added.ID); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("an outsider removes nothing: %v", err)
	}
	if err := New(fixture.database).Delete(test.Context(), fixture.member, added.ID); err != nil {
		test.Fatalf("a member removes a card: %s", err)
	}
	if count := dbtest.QueryString(test, fixture.database, `SELECT count(*)::text FROM contact`); count != "0" {
		test.Fatalf("the card is gone: %s", count)
	}
}

// Leaving the circle ends the right to edit its book at once.
func TestALeavingMemberCannotEditTheCircleAddressBook(test *testing.T) {
	fixture := newCircleBookFixture(test)
	added, err := New(fixture.database).Save(test.Context(), fixture.member, SaveRequest{AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Name: new("Kim")}})
	if err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.RemoveCircleMember(fixture.circleBook.CircleID, fixture.member.User.ID); err != nil {
			test.Fatal(err)
		}
	})
	if _, err := New(fixture.database).Save(test.Context(), fixture.member, SaveRequest{ID: added.ID, Fields: contacts.Fields{Name: new("Changed")}}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("edit after leaving: %v", err)
	}
	if err := New(fixture.database).Delete(test.Context(), fixture.member, added.ID); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("remove after leaving: %v", err)
	}
	if _, err := New(fixture.database).UpdateBook(test.Context(), fixture.member, UpdateBookRequest{ID: fixture.circleBook.ID, Name: "Mine now"}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("rename after leaving: %v", err)
	}
}

// An admin renames the circle's book; a member who is not one is refused in
// words, and an outsider is told there is no such book.
func TestOnlyAnAdminRenamesACircleAddressBook(test *testing.T) {
	fixture := newCircleBookFixture(test)
	if _, err := New(fixture.database).UpdateBook(test.Context(), fixture.member, UpdateBookRequest{ID: fixture.circleBook.ID, Name: "Ours"}); !errors.Is(err, ErrRefused) {
		test.Fatalf("a member renames nothing: %v", err)
	}
	if _, err := New(fixture.database).UpdateBook(test.Context(), fixture.outsider, UpdateBookRequest{ID: fixture.circleBook.ID, Name: "Ours"}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("an outsider renames nothing: %v", err)
	}
	outcome, err := New(fixture.database).UpdateBook(test.Context(), fixture.admin, UpdateBookRequest{ID: fixture.circleBook.ID, Name: "Ours"})
	if err != nil || outcome.Book.Name != "Ours" || outcome.Book.CircleID != fixture.circleBook.CircleID || outcome.Book.UserID != "" {
		test.Fatalf("an admin renames it: %+v %v", outcome, err)
	}
}

// A picture made from a face in the owner's photos goes on the owner's own
// cards only.
func TestAPictureIsNeverSetOnACircleCard(test *testing.T) {
	fixture := newCircleBookFixture(test)
	added, err := New(fixture.database).Save(test.Context(), fixture.member, SaveRequest{AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Name: new("Kim")}})
	if err != nil {
		test.Fatal(err)
	}
	if _, _, err := New(fixture.database).SetPicture(test.Context(), fixture.member, added.ID, []byte("not read")); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("no picture from a face on a circle's card: %v", err)
	}
}

// Without circle:use a member does not reach the circle's book, though the
// membership and contacts:use are both in order.
func TestACircleAddressBookNeedsCircleUse(test *testing.T) {
	fixture := newCircleBookFixture(test)
	if _, err := New(fixture.database).Save(test.Context(), fixture.withoutCircleUse, SaveRequest{AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Name: new("Kim")}}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("a member without circle:use adds nothing: %v", err)
	}
	if _, err := New(fixture.database).UpdateBook(test.Context(), fixture.withoutCircleUse, UpdateBookRequest{ID: fixture.circleBook.ID, Name: "Mine now"}); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("nor renames: %v", err)
	}
	if _, err := New(fixture.database).Save(test.Context(), fixture.member, SaveRequest{AddressBookID: fixture.circleBook.ID, Fields: contacts.Fields{Name: new("Kim")}}); err != nil {
		test.Fatalf("a member with circle:use adds a card: %s", err)
	}
}
