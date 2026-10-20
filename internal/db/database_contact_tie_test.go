package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Deleting a contact unties the memory page that named it, the link that said
// the page is that contact, and the account that named it as its own card. The
// page stays. A contact of the same ID in another of the person's books is
// still theirs, so deleting one copy unties nothing, and deleting a whole
// address book unties what its contacts were named by.
func TestDeletingAContactUntiesWhatNamedIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		first, err := tx.CreateAddressBook(&models.AddressBook{UserID: agent.UserID, Name: "First"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		second, err := tx.CreateAddressBook(&models.AddressBook{UserID: agent.UserID, Name: "Second"})
		if err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		for _, book := range []*models.AddressBook{first, second} {
			if _, err := tx.PutContact(&models.Contact{ID: "contact-shared", AddressBookID: book.ID, UID: "urn:uuid:ada", ETag: "e1", Card: oneCard, Name: "Ada Lovelace"}); err != nil {
				t.Fatalf("PutContact: %s", err)
			}
		}
		if _, err := tx.PutContact(&models.Contact{ID: "contact-only", AddressBookID: second.ID, UID: "urn:uuid:only", ETag: "e1", Card: oneCard, Name: "Only Example"}); err != nil {
			t.Fatalf("PutContact: %s", err)
		}
		shared := aPersonPage(t, tx, agent, "people/fixture-shared")
		only := aPersonPage(t, tx, agent, "people/fixture-only")
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
			AgentID: agent.ID, NodeID: shared.ID, RecordKind: models.RecordContact, RecordID: "contact-shared",
			RecordRelation: models.RecordRelationIsContact, LinkStatus: models.EdgeStated,
		}); err != nil {
			t.Fatalf("PutAgentRecordLink: %s", err)
		}
		if err := tx.SetAgentNodeContact(agent.ID, only.ID, "contact-only"); err != nil {
			t.Fatalf("SetAgentNodeContact: %s", err)
		}
		if err := tx.SetUserContact(agent.UserID, "contact-shared"); err != nil {
			t.Fatalf("SetUserContact: %s", err)
		}

		tiedTo := func(page *models.AgentNode) string {
			t.Helper()
			read, err := tx.GetAgentNodeByID(agent.ID, page.ID)
			if err != nil || read == nil {
				t.Fatalf("the page is still there after its contact went: %v %v", read, err)
			}
			return read.ContactID
		}
		accountTiedTo := func() string {
			t.Helper()
			owner, err := tx.GetUser(agent.UserID)
			if err != nil || owner == nil {
				t.Fatalf("GetUser: %v %v", owner, err)
			}
			return owner.ContactID
		}

		if err := tx.DeleteContact(first.ID, "contact-shared"); err != nil {
			t.Fatalf("DeleteContact: %s", err)
		}
		if got := tiedTo(shared); got != "contact-shared" {
			t.Errorf("deleting one copy of a contact untied the page from the other copy: %q", got)
		}
		if got := accountTiedTo(); got != "contact-shared" {
			t.Errorf("deleting one copy of a contact untied the account from the other copy: %q", got)
		}

		if err := tx.DeleteContact(second.ID, "contact-shared"); err != nil {
			t.Fatalf("DeleteContact: %s", err)
		}
		if got := tiedTo(shared); got != "" {
			t.Errorf("the page still names a contact that is gone: %q", got)
		}
		if got := accountTiedTo(); got != "" {
			t.Errorf("the account still names a contact that is gone: %q", got)
		}
		links, err := tx.ListAgentRecordLinks(agent.ID, shared.ID)
		if err != nil || len(links) != 0 {
			t.Errorf("the link to a contact that is gone stays: %d %v", len(links), err)
		}
		if got := tiedTo(only); got != "contact-only" {
			t.Errorf("deleting one contact untied a page about another: %q", got)
		}

		if err := tx.DeleteAddressBook(second.ID); err != nil {
			t.Fatalf("DeleteAddressBook: %s", err)
		}
		if got := tiedTo(only); got != "" {
			t.Errorf("deleting the address book left the page naming its contact: %q", got)
		}
	})
}
