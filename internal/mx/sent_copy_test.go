package mx

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A message a person sends from a mail program reaches their Sent folder
// twice: the server files it when it accepts the submission, and the program
// uploads its own copy over IMAP. One sent message is one item, whichever
// arrives first.
func TestASentMessageIsFiledOnce(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	const messageId = "<A90729F5-F9FB-4033-A66A-FAA3772A2F4D@example.com>"
	exchange := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "sender"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		sent, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindSent)
		if err != nil || sent == nil {
			t.Fatalf("a new mailbox has no Sent folder: %v", err)
		}
		drafts, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil || drafts == nil {
			t.Fatalf("a new mailbox has no Drafts folder: %v", err)
		}

		// The program's copy, uploaded first.
		uploaded, err := tx.CreateMail(&models.Mail{Kind: models.MailKindOutgoing, Subject: "Test", MessageID: messageId}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		first, err := tx.AddItem(sent.ID, uploaded.ID, "", models.MailboxItemFlags{})
		if err != nil {
			t.Fatalf("AddItem: %s", err)
		}

		// Then the submission the server accepted.
		submitted, err := tx.CreateMail(&models.Mail{Kind: models.MailKindOutgoing, Subject: "Test", MessageID: messageId}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if err := exchange.fileInSent(tx, mailbox.ID, submitted); err != nil {
			t.Fatalf("fileInSent: %s", err)
		}
		items, err := tx.ListItems(sent.ID, nil)
		if err != nil {
			t.Fatalf("ListItems: %s", err)
		}
		if len(items) != 1 || items[0].ID != first.ID {
			t.Errorf("the Sent folder should hold the one item the program uploaded, got %+v", items)
		}

		// And the other order: the server filed it, the program then uploads
		// its copy and is told the item the server already has.
		found, err := FindSentCopy(tx, sent, messageId)
		if err != nil || found == nil || found.ID != first.ID {
			t.Errorf("FindSentCopy should find the existing item, got %+v, %v", found, err)
		}

		// A different message is not a copy, and Drafts never dedupe.
		other, err := FindSentCopy(tx, sent, "<other@example.com>")
		if err != nil || other != nil {
			t.Errorf("a different Message-ID should find nothing, got %+v, %v", other, err)
		}
		draft, err := FindSentCopy(tx, drafts, messageId)
		if err != nil || draft != nil {
			t.Errorf("Drafts should never report a copy, got %+v, %v", draft, err)
		}
		none, err := FindSentCopy(tx, sent, "")
		if err != nil || none != nil {
			t.Errorf("a message with no Message-ID has no copy, got %+v, %v", none, err)
		}
	})
}
