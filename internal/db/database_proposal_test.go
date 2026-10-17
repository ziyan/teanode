package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func proposalInsightFixture(test *testing.T) (db.Database, *models.MailInsight) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	insight := &models.MailInsight{AgentID: "fixture-agent", Summary: "Original summary", Proposals: []models.MailProposal{{Kind: models.MailProposalEvent, Summary: "Original offer"}}}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err := transaction.CreateUser(&models.User{Username: "fixture-owner"})
		if err != nil {
			test.Fatal(err)
		}
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Fixture mailbox"})
		if err != nil {
			test.Fatal(err)
		}
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Fixture message", Kind: models.MailKindIncoming}, nil)
		if err != nil {
			test.Fatal(err)
		}
		insight.MailID, insight.MailboxID = mail.ID, mailbox.ID
		if err := transaction.PutMailInsight(insight); err != nil {
			test.Fatal(err)
		}
	})
	return database, insight
}

func TestSortingPreservesAcceptedProposals(test *testing.T) {
	database, stale := proposalInsightFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.LockMailInsight(stale.MailboxID, stale.MailID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SetMailProposalStatus(stale.MailboxID, stale.MailID, 0, models.MailProposalAccepted); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SetMailInsightNotes(stale.MailID, stale.MailboxID, "Research notes", "research-run"); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		stale.Summary = "New sorting summary"
		if err := transaction.PutMailInsight(stale); err != nil {
			test.Fatal(err)
		}
		found, err := transaction.GetMailInsights(stale.MailboxID, []string{stale.MailID})
		if err != nil {
			test.Fatal(err)
		}
		insight := found[stale.MailID]
		if insight.Summary != stale.Summary || insight.Notes != "Research notes" || insight.Proposals[0].Status != models.MailProposalAccepted {
			test.Fatalf("sorting replaced unrelated content: %+v", insight)
		}
	})
}

func TestExtractionWaitsForProposalAcceptance(test *testing.T) {
	database, original := proposalInsightFixture(test)
	ctx, cancel := context.WithTimeout(test.Context(), 10*time.Second)
	defer cancel()
	extractionStarted := make(chan struct{})
	extractionErrors := make(chan error, 1)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.LockMailInsight(original.MailboxID, original.MailID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.SetMailProposalStatus(original.MailboxID, original.MailID, 0, models.MailProposalAccepted); err != nil {
			test.Fatal(err)
		}
		go func() {
			close(extractionStarted)
			extractionErrors <- database.TransactionContext(ctx, func(transaction db.Transaction) error {
				return transaction.ReplaceMailProposals(original.MailboxID, original.MailID, []models.MailProposal{{Kind: models.MailProposalContact, Name: "New contact"}})
			})
		}()
		<-extractionStarted
		select {
		case err := <-extractionErrors:
			test.Fatalf("extraction passed uncommitted acceptance: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	})
	if err := <-extractionErrors; err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		found, err := transaction.GetMailInsights(original.MailboxID, []string{original.MailID})
		if err != nil {
			test.Fatal(err)
		}
		insight := found[original.MailID]
		if insight.Summary != original.Summary || len(insight.Proposals) != 2 || insight.Proposals[0].Status != models.MailProposalAccepted || insight.Proposals[1].Name != "New contact" {
			test.Fatalf("extraction lost acceptance or sorting: %+v", insight)
		}
	})
}

// A second reading of a message (the look back over past mail reads ones
// sorting flagged long ago) replaces the offers still waiting and does not
// offer again what the person already accepted or dismissed, but does add
// what is new.
func TestRereadingDoesNotOfferADecidedProposalAgain(test *testing.T) {
	database, original := proposalInsightFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.ReplaceMailProposals(original.MailboxID, original.MailID, []models.MailProposal{
			{Kind: models.MailProposalEvent, Summary: "Parents evening", Starts: "2026-11-05T18:00:00Z"},
			{Kind: models.MailProposalEvent, Summary: "Bake sale", Starts: "2026-11-07T10:00:00Z"},
			{Kind: models.MailProposalContact, Name: "Pat Example", Emails: []string{"pat@example.org"}},
		}); err != nil {
			test.Fatal(err)
		}
		// The original offer is replaced; accept the first of the new ones
		// and dismiss the contact.
		for index, proposalStatus := range map[int]string{0: models.MailProposalAccepted, 2: models.MailProposalDismissed} {
			if err := transaction.SetMailProposalStatus(original.MailboxID, original.MailID, index, proposalStatus); err != nil {
				test.Fatal(err)
			}
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.ReplaceMailProposals(original.MailboxID, original.MailID, []models.MailProposal{
			{Kind: models.MailProposalEvent, Summary: "parents evening", Starts: "2026-11-05T18:00:00Z"},
			{Kind: models.MailProposalEvent, Summary: "Bake sale", Starts: "2026-11-07T10:00:00Z"},
			{Kind: models.MailProposalContact, Name: "P. Example", Emails: []string{"PAT@example.org"}},
			{Kind: models.MailProposalEvent, Summary: "Book fair", Starts: "2026-11-09T10:00:00Z"},
		}); err != nil {
			test.Fatal(err)
		}
		found, err := transaction.GetMailInsights(original.MailboxID, []string{original.MailID})
		if err != nil {
			test.Fatal(err)
		}
		summaries := []string{}
		for _, proposal := range found[original.MailID].Proposals {
			summaries = append(summaries, proposal.Summary+proposal.Name+"/"+proposal.Status)
		}
		want := []string{"Parents evening/accepted", "Pat Example/dismissed", "Bake sale/", "Book fair/"}
		if len(summaries) != len(want) {
			test.Fatalf("the decided ones kept once, the offered replaced and the new one added: %q", summaries)
		}
		for index := range want {
			if summaries[index] != want[index] {
				test.Fatalf("proposal %d: %q, want %q (all %q)", index, summaries[index], want[index], summaries)
			}
		}
	})
}
