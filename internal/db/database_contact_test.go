package db_test

import (
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
