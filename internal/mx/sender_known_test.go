package mx

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// "The sender is known" means the person keeps them in their address book.
//
// It used to mean the mailbox had seen the address before, counted off a
// ledger this server built of everybody who had ever written -- which made
// the condition true for the first stranger to write twice, and for every
// newsletter. Known is now what the word means everywhere else.
func TestSenderKnownReadsTheAddressBook(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	exchange := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		condition := models.MailboxRuleCondition{Field: "sender-known"}
		known := func(sender string) bool {
			t.Helper()
			mail := &models.Mail{From: sender, Sender: sender, ReceivedAt: time.Now(),
				Headers: []string{"From: " + sender}}
			holds, err := exchange.conditionHolds(tx, mailbox, condition, mail, nil)
			if err != nil {
				t.Fatalf("conditionHolds: %s", err)
			}
			return holds
		}

		if known("maria@example.net") {
			t.Fatal("nobody is known before anybody is kept")
		}

		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: user.ID, Name: "Contacts"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		if _, err := tx.PutContact(&models.Contact{
			AddressBookID: book.ID, UID: "urn:uuid:maria", ETag: "e1",
			Card: "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:maria\r\nFN:Maria\r\n" +
				"EMAIL:maria@example.net\r\nEMAIL:maria@work.example\r\nEND:VCARD\r\n",
			Name: "Maria", Emails: []string{"maria@example.net", "maria@work.example"},
		}); err != nil {
			t.Fatalf("PutContact: %s", err)
		}

		if !known("maria@example.net") {
			t.Fatal("somebody kept is known")
		}
		// Every address on the card, not only the first: a contact is one
		// person however many addresses they write from.
		if !known("maria@work.example") {
			t.Fatal("the second address on the card is the same person")
		}
		// And an address that merely begins the same way is somebody else.
		if known("maria@example.network") {
			t.Fatal("a longer address is a different address")
		}
		if known("aria@example.net") {
			t.Fatal("a shorter address is a different address")
		}
		// A sender does not get to widen the question by choosing their own
		// address. These are the wildcards of a SQL LIKE, which is what the
		// lookup used to be built from: "%" matched everybody this person
		// keeps, and an underscore -- an ordinary character in an address --
		// matched any character at all.
		for _, crafted := range []string{"%@example.net", "%", "maria@example.ne_", "m%@example.net", "_aria@example.net"} {
			if known(crafted) {
				t.Fatalf("%q is not somebody in the address book", crafted)
			}
		}
	})
}
