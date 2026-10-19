package db_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const oneCard = "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:ada\r\nFN:Ada Lovelace\r\nEMAIL:ada@example.com\r\nEND:VCARD\r\n"

// An address book belongs to an account, holds contacts, and takes them with
// it when it goes.
func TestAnAddressBookHoldsContactsAndTakesThemWithIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var book *models.AddressBook
	var kept *models.Contact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if book, err = tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID}); err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		// An address book with no name is called something anyway, because
		// a nameless collection is no use on a phone.
		if book.Name != "Contacts" {
			t.Fatalf("a book is named: %q", book.Name)
		}
		books, err := tx.ListAddressBooks(owner.ID)
		if err != nil || len(books) != 1 {
			t.Fatalf("one book for the account: %d %v", len(books), err)
		}

		if kept, err = tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "urn:uuid:ada", ETag: "e1", Card: oneCard,
			Name: "Ada Lovelace", Emails: []string{"ada@example.com", "ada@home.example"},
		}); err != nil {
			t.Fatalf("PutContact: %s", err)
		}
		if kept.ID == "" || kept.CreatedAt.IsZero() {
			t.Fatalf("a kept contact has an id and a time: %+v", kept)
		}
		// The lists beside the card come back as lists, not as the one
		// string they are stored as.
		read, err := tx.GetContact(book.ID, kept.ID)
		if err != nil || read == nil {
			t.Fatalf("GetContact: %v %v", read, err)
		}
		if len(read.Emails) != 2 || read.Emails[1] != "ada@home.example" {
			t.Fatalf("addresses survive the round trip: %v", read.Emails)
		}
		if read.Card != oneCard {
			t.Fatalf("the card is kept exactly:\n%q", read.Card)
		}

		// The card's own identifier is how the same person is recognized.
		byUID, err := tx.GetContactByUID(book.ID, "urn:uuid:ada")
		if err != nil || byUID == nil || byUID.ID != kept.ID {
			t.Fatalf("found by uid: %v %v", byUID, err)
		}
		if missing, err := tx.GetContactByUID(book.ID, "urn:uuid:nobody"); err != nil || missing != nil {
			t.Fatalf("an unknown uid is nothing, not an error: %v %v", missing, err)
		}

		count, err := tx.CountContacts(book.ID)
		if err != nil || count != 1 {
			t.Fatalf("counted: %d %v", count, err)
		}
	})

	// Deleting the book takes the contacts with it, through the foreign key
	// rather than a second statement.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.DeleteAddressBook(book.ID); err != nil {
			t.Fatalf("DeleteAddressBook: %s", err)
		}
		gone, err := tx.GetContact(book.ID, kept.ID)
		if err != nil || gone != nil {
			t.Fatalf("the contacts went with the book: %v %v", gone, err)
		}
	})
}

// The same person cannot be kept twice under one identifier, which is what
// stops two devices each adding somebody from making two of them.
func TestOneIdentifierIsOnePerson(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var book *models.AddressBook
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ := tx.CreateUser(&models.User{Username: "alice"})
		var err error
		if book, err = tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID}); err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		if _, err := tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "urn:uuid:ada", ETag: "e1", Card: oneCard, Name: "Ada",
		}); err != nil {
			t.Fatalf("PutContact: %s", err)
		}
	})

	// A transaction of its own, because a refused write aborts the one it
	// happened in: PostgreSQL will not take anything more from a
	// transaction that has hit a constraint, so a test that expects a
	// refusal cannot go on using the same one.
	var refused error
	_ = database.Transaction(func(tx db.Transaction) error {
		_, refused = tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "urn:uuid:ada", ETag: "e2", Card: oneCard, Name: "Ada again",
		})
		return refused
	})
	if refused == nil {
		t.Fatal("a second contact under the same identifier must be refused")
	}

	// And the first is untouched.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.ListContacts(book.ID, "", 10)
		if err != nil || len(found) != 1 || found[0].Name != "Ada" {
			t.Fatalf("the one that was there is still there: %d %v", len(found), err)
		}
	})
}

// Searching a book finds a person by any of the things somebody might type.
func TestContactsAreFoundByWhatSomebodyWouldType(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ := tx.CreateUser(&models.User{Username: "alice"})
		book, _ := tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID})
		for _, contact := range []*models.Contact{
			{AddressBookID: book.ID, UID: "u1", ETag: "e1", Card: oneCard, Name: "Ada Lovelace",
				Organization: "Analytical Engines", Emails: []string{"ada@example.com"}},
			{AddressBookID: book.ID, UID: "u2", ETag: "e2", Card: oneCard, Name: "Grace Hopper",
				Organization: "Navy", Phones: []string{"+1-555-0199"}},
		} {
			if _, err := tx.PutContact(contact); err != nil {
				t.Fatalf("PutContact: %s", err)
			}
		}
		for query, want := range map[string]string{
			"lovelace":   "Ada Lovelace",
			"ANALYTICAL": "Ada Lovelace",
			"ada@":       "Ada Lovelace",
			"0199":       "Grace Hopper",
			"navy":       "Grace Hopper",
		} {
			found, err := tx.ListContacts(book.ID, query, 10)
			if err != nil || len(found) != 1 || found[0].Name != want {
				t.Errorf("searching %q found %d, wanted %q (%v)", query, len(found), want, err)
			}
		}
		// No query is everybody, by name.
		all, err := tx.ListContacts(book.ID, "", 10)
		if err != nil || len(all) != 2 || all[0].Name != "Ada Lovelace" {
			t.Fatalf("all of them, by name: %d %v", len(all), err)
		}
		if found, err := tx.ListContacts(book.ID, "nobody at all", 10); err != nil || len(found) != 0 {
			t.Fatalf("a query matching nothing finds nothing: %d %v", len(found), err)
		}
	})
}

// A contact needs the things that make it one.
func TestAContactNeedsACardAndAnIdentifier(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, _ := tx.CreateUser(&models.User{Username: "alice"})
		book, _ := tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID})
		for _, refused := range []*models.Contact{
			{AddressBookID: book.ID, UID: "u", ETag: "e"},
			{AddressBookID: book.ID, Card: oneCard, ETag: "e"},
			{UID: "u", Card: oneCard, ETag: "e"},
		} {
			if _, err := tx.PutContact(refused); err == nil {
				t.Errorf("should be refused: %+v", refused)
			} else if !strings.Contains(err.Error(), "db: a contact needs") {
				t.Errorf("refused for the wrong reason: %s", err)
			}
		}
	})
}

// A book is bounded by what it holds, not only by how many things are in it.
//
// A card may be a megabyte -- a photograph makes one -- so ten thousand of
// them is ten gigabytes, and the listing a client reads is the whole book
// built in memory and then serialised whole, because the protocol library has
// no streaming. Two thousand individually legal cards were a way for one
// account to take the server down for everybody on it.
func TestAnAddressBookIsBoundedInBytes(t *testing.T) {
	dbtest.RunTransaction(t, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "collector"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: user.ID, Name: "Contacts"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		card := func(size int) string {
			return "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Somebody\r\nNOTE:" +
				strings.Repeat("x", size) + "\r\nEND:VCARD\r\n"
		}
		// Seven cards of eight megabytes fit; the eighth does not.
		for index := 0; index < 7; index++ {
			if _, err := tx.PutContact(&models.Contact{
				AddressBookID: book.ID, UID: fmt.Sprintf("uid-%d", index),
				ETag: "e", Card: card(8 << 20), Name: "Somebody",
			}); err != nil {
				t.Fatalf("card %d should fit: %s", index, err)
			}
		}
		_, err = tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "uid-over", ETag: "e", Card: card(8 << 20), Name: "Somebody",
		})
		if !errors.Is(err, db.ErrBookFull) {
			t.Fatalf("the book is full and says so: %v", err)
		}
		// Replacing a card already there is measured against the book
		// without it, so editing one is not refused because of its own
		// size.
		held, err := tx.GetContactByUID(book.ID, "uid-6")
		if err != nil || held == nil {
			t.Fatalf("the card is there: %v %v", held, err)
		}
		held.Card = card(8 << 20)
		held.ETag = "e2"
		if _, err := tx.PutContact(held); err != nil {
			t.Fatalf("rewriting a card of the same size is not refused: %s", err)
		}
	})
}

// contactAccountWorld is an owner with an address book, two accounts in a
// circle with them and one that is not, each of the two with a mailbox.
type contactAccountWorld struct {
	owner, sam, riley, stranger *models.User
	book                        *models.AddressBook
	circle                      *models.Circle
}

func newContactAccountWorld(t *testing.T, tx db.Transaction) *contactAccountWorld {
	t.Helper()
	world := &contactAccountWorld{
		owner:    newCircleTestUser(t, tx, "link-owner"),
		sam:      newCircleTestUser(t, tx, "link-sam"),
		riley:    newCircleTestUser(t, tx, "link-riley"),
		stranger: newCircleTestUser(t, tx, "link-stranger"),
	}
	var err error
	if world.book, err = tx.CreateAddressBook(&models.AddressBook{UserID: world.owner.ID}); err != nil {
		t.Fatalf("CreateAddressBook: %s", err)
	}
	if world.circle, err = tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, world.owner.ID); err != nil {
		t.Fatalf("CreateCircle: %s", err)
	}
	for _, member := range []*models.User{world.sam, world.riley} {
		if _, err := tx.AddCircleMember(world.circle.ID, member.ID, models.MemberRoleMember, world.owner.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
	}
	return world
}

func putTestContact(t *testing.T, tx db.Transaction, bookId, uid, name string, emails ...string) *models.Contact {
	t.Helper()
	kept, err := tx.PutContact(&models.Contact{AddressBookID: bookId, UID: uid, ETag: "e", Card: oneCard, Name: name, Emails: emails})
	if err != nil {
		t.Fatalf("PutContact: %s", err)
	}
	return kept
}

// A link is read back, survives saving the card again (a sync or an edit
// builds the contact afresh), and is cleared by an empty account.
func TestAContactLinkSurvivesSavingTheCard(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newContactAccountWorld(t, tx)
		kept := putTestContact(t, tx, world.book.ID, "urn:uuid:sam", "Sam")
		if kept.AccountUserID != "" {
			t.Fatalf("a new contact is not linked: %q", kept.AccountUserID)
		}
		if err := tx.SetContactAccount(world.book.ID, kept.ID, world.sam.ID); err != nil {
			t.Fatalf("SetContactAccount: %s", err)
		}
		read, err := tx.GetContact(world.book.ID, kept.ID)
		if err != nil || read.AccountUserID != world.sam.ID {
			t.Fatalf("the link is read back: %+v %v", read, err)
		}

		// The same contact saved again, built afresh without the link.
		saved, err := tx.PutContact(&models.Contact{
			ID: kept.ID, AddressBookID: world.book.ID, CreatedAt: kept.CreatedAt, UID: "urn:uuid:sam", ETag: "e2", Card: oneCard, Name: "Samuel",
		})
		if err != nil || saved.AccountUserID != world.sam.ID || saved.Name != "Samuel" {
			t.Fatalf("saving the card keeps the link: %+v %v", saved, err)
		}
		listed, err := tx.ListContacts(world.book.ID, "", 10)
		if err != nil || len(listed) != 1 || listed[0].AccountUserID != world.sam.ID {
			t.Fatalf("a listing shows the link: %+v %v", listed, err)
		}
		byUID, err := tx.GetContactByUID(world.book.ID, "urn:uuid:sam")
		if err != nil || byUID.AccountUserID != world.sam.ID {
			t.Fatalf("a lookup by uid shows the link: %+v %v", byUID, err)
		}
		found, err := tx.FindContactByAccount(world.owner.ID, world.sam.ID)
		if err != nil || found == nil || found.ID != kept.ID {
			t.Fatalf("found by account: %+v %v", found, err)
		}
		if other, err := tx.FindContactByAccount(world.owner.ID, world.riley.ID); err != nil || other != nil {
			t.Fatalf("an unlinked account is nobody: %+v %v", other, err)
		}
		if err := tx.SetContactAccount(world.book.ID, kept.ID, ""); err != nil {
			t.Fatalf("clearing: %s", err)
		}
		if read, _ = tx.GetContact(world.book.ID, kept.ID); read.AccountUserID != "" {
			t.Fatalf("an empty account clears the link: %q", read.AccountUserID)
		}
	})
}

// The database refuses the links it must, whatever asks.
func TestAContactCannotBeLinkedToTheOwnerOrToAStrangerOrTwice(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var world *contactAccountWorld
	var first, second *models.Contact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world = newContactAccountWorld(t, tx)
		first = putTestContact(t, tx, world.book.ID, "urn:uuid:first", "First")
		second = putTestContact(t, tx, world.book.ID, "urn:uuid:second", "Second")
		if err := tx.SetContactAccount(world.book.ID, first.ID, world.sam.ID); err != nil {
			t.Fatalf("SetContactAccount: %s", err)
		}
	})
	attempt := func(contactId, accountUserId string) error {
		var refused error
		_ = database.Transaction(func(tx db.Transaction) error {
			refused = tx.SetContactAccount(world.book.ID, contactId, accountUserId)
			return refused
		})
		return refused
	}
	if err := attempt(second.ID, world.owner.ID); !errors.Is(err, db.ErrInvalidArguments) {
		t.Errorf("the owner's own account: %v", err)
	}
	if err := attempt(second.ID, world.stranger.ID); !errors.Is(err, db.ErrInvalidArguments) {
		t.Errorf("an account outside the owner's circles: %v", err)
	}
	if err := attempt(second.ID, world.sam.ID); !errors.Is(err, db.ErrAlreadyExists) {
		t.Errorf("an account another contact in the book already is: %v", err)
	}
	if err := attempt("missing", world.riley.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("a contact that is not there: %v", err)
	}
	// Linking the same contact again is not a conflict with itself.
	if err := attempt(first.ID, world.sam.ID); err != nil {
		t.Errorf("the same link again: %v", err)
	}
	// Another book of the same owner may hold its own contact for the account.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		other, err := tx.CreateAddressBook(&models.AddressBook{UserID: world.owner.ID, Name: "Work"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		inOther := putTestContact(t, tx, other.ID, "urn:uuid:other", "Sam at work")
		if err := tx.SetContactAccount(other.ID, inOther.ID, world.sam.ID); err != nil {
			t.Fatalf("a second book may link the same account: %s", err)
		}
		// The first book by creation time answers.
		found, err := tx.FindContactByAccount(world.owner.ID, world.sam.ID)
		if err != nil || found == nil || found.ID != first.ID {
			t.Fatalf("the first book wins: %+v %v", found, err)
		}
		links, err := tx.ListContactAccountLinks(world.owner.ID)
		if err != nil || len(links) != 2 || links[0].ID != first.ID {
			t.Fatalf("both links, the first book first: %+v %v", links, err)
		}
	})
}

// When two people stop sharing their last circle the link goes, whichever of
// them owned the contact, and a link to somebody still in a circle stays.
func TestContactLinksEndWhenTwoPeopleStopSharingACircle(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var world *contactAccountWorld
	var ownersContact, samsContact, rileysContact *models.Contact
	var samsBook *models.AddressBook
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world = newContactAccountWorld(t, tx)
		var err error
		if samsBook, err = tx.CreateAddressBook(&models.AddressBook{UserID: world.sam.ID}); err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		ownersContact = putTestContact(t, tx, world.book.ID, "urn:uuid:a", "Sam")
		rileysContact = putTestContact(t, tx, world.book.ID, "urn:uuid:b", "Riley")
		samsContact = putTestContact(t, tx, samsBook.ID, "urn:uuid:c", "Owner")
		for _, link := range []struct {
			bookId, contactId, accountUserId string
		}{
			{world.book.ID, ownersContact.ID, world.sam.ID},
			{world.book.ID, rileysContact.ID, world.riley.ID},
			{samsBook.ID, samsContact.ID, world.owner.ID},
		} {
			if err := tx.SetContactAccount(link.bookId, link.contactId, link.accountUserId); err != nil {
				t.Fatalf("SetContactAccount: %s", err)
			}
		}
		// Sam leaves the circle: the owner shares it with Riley still.
		if _, err := tx.RemoveCircleMember(world.circle.ID, world.sam.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, expected := range []struct {
			name, bookId, contactId, accountUserId string
		}{
			{"the owner's link to Sam", world.book.ID, ownersContact.ID, ""},
			{"Sam's link to the owner", samsBook.ID, samsContact.ID, ""},
			{"the owner's link to Riley", world.book.ID, rileysContact.ID, world.riley.ID},
		} {
			read, err := tx.GetContact(expected.bookId, expected.contactId)
			if err != nil || read.AccountUserID != expected.accountUserId {
				t.Errorf("%s: got %q, wanted %q (%v)", expected.name, read.AccountUserID, expected.accountUserId, err)
			}
		}
		// Gone from the table too, not only hidden by the read.
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "contact" WHERE "account_user_id" IS NOT NULL`); got != "1" {
			t.Errorf("%s links are still stored, wanted 1", got)
		}
		// And they do not come back when Sam joins again.
		if _, err := tx.AddCircleMember(world.circle.ID, world.sam.ID, models.MemberRoleMember, world.owner.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if read, _ := tx.GetContact(world.book.ID, ownersContact.ID); read.AccountUserID != "" {
			t.Errorf("a link does not come back unannounced: %q", read.AccountUserID)
		}
	})
}

// A link the cleanup did not clear is still never shown once the circle is
// gone, because a read checks it too.
func TestAContactLinkIsHiddenWithoutASharedCircle(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var world *contactAccountWorld
	var kept *models.Contact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world = newContactAccountWorld(t, tx)
		kept = putTestContact(t, tx, world.book.ID, "urn:uuid:sam2", "Sam")
		if err := tx.SetContactAccount(world.book.ID, kept.ID, world.sam.ID); err != nil {
			t.Fatalf("SetContactAccount: %s", err)
		}
	})
	dbtest.Exec(t, database, `DELETE FROM "circle_member" WHERE "user_id" = '`+world.sam.ID+`'`)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if read, _ := tx.GetContact(world.book.ID, kept.ID); read.AccountUserID != "" {
			t.Errorf("a read hides a link without a shared circle: %q", read.AccountUserID)
		}
		if found, _ := tx.FindContactByAccount(world.owner.ID, world.sam.ID); found != nil {
			t.Errorf("a lookup finds nothing without a shared circle")
		}
		if links, _ := tx.ListContactAccountLinks(world.owner.ID); len(links) != 0 {
			t.Errorf("the links list is empty without a shared circle: %d", len(links))
		}
	})
}

// Suggestions come from mailbox addresses only, not from the address an
// account is notified at, stop once linked, and carry no address.
func TestContactAccountSuggestionsMatchMailboxAddressesOnly(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newContactAccountWorld(t, tx)
		domain, err := tx.CreateDomain(&models.Domain{Domain: "family.example"})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		for _, member := range []*models.User{world.sam, world.stranger} {
			mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: member.ID, Name: "Personal"})
			if err != nil {
				t.Fatalf("CreateMailbox: %s", err)
			}
			if _, err := tx.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: member.Username, Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
				t.Fatalf("CreateAlias: %s", err)
			}
		}
		dbtest.Exec(t, database, `UPDATE "user" SET "email" = 'riley.private@elsewhere.example' WHERE "id" = '`+world.riley.ID+`'`)

		bySam := putTestContact(t, tx, world.book.ID, "urn:uuid:sam", "Sam", "other@place.example", "Link-Sam@Family.Example")
		putTestContact(t, tx, world.book.ID, "urn:uuid:riley", "Riley", "riley.private@elsewhere.example")
		putTestContact(t, tx, world.book.ID, "urn:uuid:stranger", "Stranger", "link-stranger@family.example")
		putTestContact(t, tx, world.book.ID, "urn:uuid:nobody", "Nobody")

		suggestions, err := tx.ListContactAccountSuggestions(world.owner.ID)
		if err != nil || len(suggestions) != 1 {
			t.Fatalf("one suggestion, from a mailbox address in the circles: %+v %v", suggestions, err)
		}
		if suggestions[0].Contact.ID != bySam.ID || suggestions[0].AccountUserID != world.sam.ID {
			t.Fatalf("the suggestion is Sam's: %+v", suggestions[0])
		}
		if err := tx.SetContactAccount(world.book.ID, bySam.ID, world.sam.ID); err != nil {
			t.Fatalf("SetContactAccount: %s", err)
		}
		if suggestions, err = tx.ListContactAccountSuggestions(world.owner.ID); err != nil || len(suggestions) != 0 {
			t.Fatalf("none once linked: %+v %v", suggestions, err)
		}
	})
}

// The accounts whose linked contact names a group of faces are found in one
// query, a photo listing can be narrowed to the contact in one address book,
// and both stop when the two no longer share a circle.
func TestPhotoPeopleAreFoundThroughLinkedContacts(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ownerId := dbtest.CreateUser(test, database, "photo-link-owner")
	samId := dbtest.CreateUser(test, database, "photo-link-sam")
	var circleId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		circle, err := transaction.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, ownerId)
		if err != nil {
			test.Fatal(err)
		}
		circleId = circle.ID
		if _, err := transaction.AddCircleMember(circle.ID, samId, models.MemberRoleMember, ownerId); err != nil {
			test.Fatal(err)
		}
	})
	bookId, contactId := keepContact(test, database, ownerId, "Example Sam")
	found := readyPhotoWithPreview(test, database, ownerId, "photo-link-one")
	stored := storeFaces(test, database, ownerId, found.ID, foundFaceAt(0, 0.1))
	groupId := groupOwnFace(test, database, ownerId, stored[0].ID)

	accountsWithPhotos := func() []string {
		var accountUserIds []string
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			var err error
			if accountUserIds, err = transaction.ListPhotoPeopleAccountUserIDs(ownerId); err != nil {
				test.Fatal(err)
			}
		})
		return accountUserIds
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.SetContactAccount(bookId, contactId, samId); err != nil {
			test.Fatal(err)
		}
	})
	// Linked, but no group is named by the contact yet.
	if got := accountsWithPhotos(); len(got) != 0 {
		test.Fatalf("a contact that names no group has no photos: %v", got)
	}
	nameGroup(test, database, ownerId, groupId, bookId, contactId)
	if got := accountsWithPhotos(); len(got) != 1 || got[0] != samId {
		test.Fatalf("the linked account has photos: %v", got)
	}

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		matched, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, ContactID: contactId, ContactAddressBookID: bookId})
		if err != nil || len(matched) != 1 {
			test.Fatalf("the contact of its own book: %v %v", photoIdsOf(matched), err)
		}
		other, err := transaction.ListPhotos(ownerId, &db.PhotoFilter{PageSize: 10, ContactID: contactId, ContactAddressBookID: "another-book"})
		if err != nil || len(other) != 0 {
			test.Fatalf("a contact of the same id in another book is not this one: %v %v", photoIdsOf(other), err)
		}
		// A stranger asking about the same account finds nobody.
		if contact, err := transaction.FindContactByAccount(samId, ownerId); err != nil || contact != nil {
			test.Fatalf("the link is the owner's alone: %v %v", contact, err)
		}
	})

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.RemoveCircleMember(circleId, samId); err != nil {
			test.Fatal(err)
		}
	})
	if got := accountsWithPhotos(); len(got) != 0 {
		test.Fatalf("no photos of somebody who left the circle: %v", got)
	}
}
