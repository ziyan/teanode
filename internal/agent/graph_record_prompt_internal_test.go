package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The card that is the person themselves goes into the prompt only from an
// address book they gave the agent.
func TestTheSelfCardIsInThePromptOnlyFromAGrantedBook(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var card *models.Contact
	var book *models.AddressBook
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if book, err = tx.CreateAddressBook(&models.AddressBook{UserID: run.Owner.ID, Name: "Mine"}); err != nil {
			t.Fatal(err)
		}
		if card, err = tx.PutContact(&models.Contact{AddressBookID: book.ID, UID: "me", ETag: "e", Card: "x", Name: "Fixture Owner", Emails: []string{"owner@example.com"}}); err != nil {
			t.Fatal(err)
		}
		if err = tx.SetUserContact(run.Owner.ID, card.ID); err != nil {
			t.Fatal(err)
		}
	})
	owner := *run.Owner
	owner.ContactID = card.ID

	joined := strings.Join(worker.selfLines(t.Context(), run.Agent, &owner), "\n")
	if strings.Contains(joined, "Fixture Owner") || strings.Contains(joined, "owner@example.com") {
		t.Errorf("a card in a book that was not given reached the prompt:\n%s", joined)
	}
	if !strings.Contains(joined, "not one you may read") {
		t.Errorf("the prompt says why their addresses are not known:\n%s", joined)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		book.AgentGranted = true
		if _, err := tx.UpdateAddressBook(book); err != nil {
			t.Fatal(err)
		}
	})
	joined = strings.Join(worker.selfLines(t.Context(), run.Agent, &owner), "\n")
	if !strings.Contains(joined, "Fixture Owner") || !strings.Contains(joined, "owner@example.com") {
		t.Errorf("a card in a granted book is in the prompt:\n%s", joined)
	}
}

// A recalled page names the records it is linked to by kind and title, only
// those the agent may read, with the titles inside the fence for data this
// server did not write, and nothing of a recording.
func TestARecalledPageNamesItsReadableRecordsInsideTheFence(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	_, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var page *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for name, isGranted := range map[string]bool{"Shared": true, "Private": false} {
			book, err := tx.CreateAddressBook(&models.AddressBook{UserID: run.Owner.ID, Name: name})
			if err != nil {
				t.Fatal(err)
			}
			if isGranted {
				book.AgentGranted = true
				if _, err := tx.UpdateAddressBook(book); err != nil {
					t.Fatal(err)
				}
			}
			contact, err := tx.PutContact(&models.Contact{AddressBookID: book.ID, UID: name, ETag: "e", Card: "x", Name: name + " Card"})
			if err != nil {
				t.Fatal(err)
			}
			if page == nil {
				if page, err = tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "people/fixture-friend", Kind: models.NodePerson, Name: "Fixture Friend"}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
				AgentID: run.Agent.ID, NodeID: page.ID, RecordKind: models.RecordContact, RecordID: contact.ID, RecordRelation: models.RecordRelationAbout,
			}); err != nil {
				t.Fatal(err)
			}
		}
		recording, err := tx.CreateRecording(&models.Recording{UserID: run.Owner.ID, ClientRecordingID: "fixture", RecordingTitle: "Spoken title", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
			AgentID: run.Agent.ID, NodeID: page.ID, RecordKind: models.RecordRecordingPassage, RecordID: recording.ID,
			RecordRelation: models.RecordRelationSpokeIn, PassageStartMS: 1, PassageEndMS: 2,
		}); err != nil {
			t.Fatal(err)
		}
	})
	var lines string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if lines, err = recordLinkPromptLines(tx, run.Agent, page); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(lines, "contact: Shared Card") {
		t.Errorf("a readable record is named by kind and title:\n%s", lines)
	}
	for _, leaked := range []string{"Private Card", "Spoken title"} {
		if strings.Contains(lines, leaked) {
			t.Errorf("%q reached the prompt:\n%s", leaked, lines)
		}
	}
	open, closing := strings.Index(lines, untrustedOpen), strings.Index(lines, untrustedClose)
	if open < 0 || closing < open || strings.Index(lines, "Shared Card") < open || strings.Index(lines, "Shared Card") > closing {
		t.Errorf("the titles are inside the fence:\n%s", lines)
	}
}
