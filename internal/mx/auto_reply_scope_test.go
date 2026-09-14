package mx

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An away message written for colleagues can be kept to colleagues.
//
// Left off, an out-of-office reply tells whoever writes in that the person is
// gone and when they are back, which is more than a stranger needs and used
// to be softened by answering each sender only once a week. That ledger is
// gone; this switch is the honest version of the same thought.
func TestTheAwayMessageCanBeKeptToOwnDomains(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	exchange := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		domain, err := tx.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal",
			AutoReply: &models.MailboxAutoReply{Enabled: true, Text: "Away until Monday."}})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if _, err := tx.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "alice",
			Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
		if mailbox, err = tx.GetMailbox(mailbox.ID); err != nil {
			t.Fatalf("GetMailbox: %s", err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("a new mailbox has no Inbox: %v", err)
		}

		// A message that every other protection allows: addressed to the
		// mailbox, vouched for by SPF, not automatic, not a list.
		refusalFor := func(sender string) string {
			t.Helper()
			mail := &models.Mail{
				From: sender, Sender: sender, Subject: "hello", ReceivedAt: time.Now(),
				Headers: []string{"From: " + sender, "To: alice@example.com", "Subject: hello"},
			}
			mail.AuthenticationResults.SPF = &models.SPFResult{Result: "pass"}
			stored, err := tx.CreateMail(mail, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			item, err := tx.AddItem(inbox.ID, stored.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatalf("AddItem: %s", err)
			}
			reason, err := exchange.autoReplyRefusal(tx, mailbox, "alice@example.com", item, stored, time.Now())
			if err != nil {
				t.Fatalf("autoReplyRefusal: %s", err)
			}
			return reason
		}

		// Off: anybody is answered.
		if reason := refusalFor("stranger@elsewhere.example"); reason != "" {
			t.Fatalf("with the switch off a stranger is answered, got %q", reason)
		}

		mailbox.AutoReply.SameDomainOnly = true
		if reason := refusalFor("stranger@elsewhere.example"); reason != "the sender is not at one of this mailbox's own domains" {
			t.Fatalf("a stranger should be refused, got %q", reason)
		}
		if reason := refusalFor("bob@example.com"); reason != "" {
			t.Fatalf("a colleague is still answered, got %q", reason)
		}
	})
}

// Two away messages talking to each other are stopped by the hourly count,
// which is what is left now that the per-sender ledger is gone.
func TestTheHourlyLimitIsClaimedOncePerReply(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		now := time.Now()
		for attempt := 1; attempt <= 3; attempt++ {
			claimed, err := tx.ClaimAutoReply(mailbox.ID, now, 3)
			if err != nil || !claimed {
				t.Fatalf("reply %d should be under the limit: %v %s", attempt, claimed, err)
			}
		}
		if claimed, err := tx.ClaimAutoReply(mailbox.ID, now, 3); err != nil || claimed {
			t.Fatalf("the fourth is over it: %v %s", claimed, err)
		}
		// The next hour starts again: the limit is about the hour in hand.
		if claimed, err := tx.ClaimAutoReply(mailbox.ID, now.Add(time.Hour), 3); err != nil || !claimed {
			t.Fatalf("a new hour starts again: %v %s", claimed, err)
		}
	})
}
