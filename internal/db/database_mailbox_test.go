package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A message in three mailboxes is one row and three items; moving an item is
// a new UID in the new folder and the old one logged as expunged; and the
// message's retention clock starts only when the last item lets go.
func TestMailboxItemsReferenceOneMessage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var mailId string
	var inboxes []*models.MailboxFolder
	var mailboxes []*models.Mailbox
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mail, err := tx.CreateMail(&models.Mail{Subject: "hello", Kind: models.MailKindIncoming}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		mailId = mail.ID
		for _, name := range []string{"one", "two", "three"} {
			user, err := tx.CreateUser(&models.User{Username: name})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
			if err != nil {
				t.Fatalf("CreateMailbox: %s", err)
			}
			mailboxes = append(mailboxes, mailbox)
			inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
			if err != nil || inbox == nil {
				t.Fatalf("a new mailbox has no Inbox: %v", err)
			}
			inboxes = append(inboxes, inbox)
			if _, err := tx.AddItem(inbox.ID, mailId, models.MailboxItemFlags{}); err != nil {
				t.Fatalf("AddItem: %s", err)
			}
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		items, err := tx.ListItemsByMail(mailId)
		if err != nil || len(items) != 3 {
			t.Fatalf("the message is held by %d items, want 3: %v", len(items), err)
		}
		mail, err := tx.GetMail(mailId, nil)
		if err != nil || mail == nil || mail.UnreferencedAt != nil {
			t.Errorf("a held message has a retention clock running: %+v, %v", mail, err)
		}
		folders, err := tx.ListFolders(mailboxes[0].ID)
		if err != nil {
			t.Fatalf("ListFolders: %s", err)
		}
		for _, folder := range folders {
			if folder.Kind == models.MailboxFolderKindInbox && (folder.Unread != 1 || folder.Total != 1) {
				t.Errorf("the Inbox counts %d unread of %d, want 1 of 1", folder.Unread, folder.Total)
			}
		}

		// Move the first mailbox's item to its Archive: a new UID there, the
		// old one expunged, both folders' modseq moved on.
		archive, err := tx.GetFolderByKind(mailboxes[0].ID, models.MailboxFolderKindArchive)
		if err != nil || archive == nil {
			t.Fatalf("no Archive: %v", err)
		}
		before, _ := tx.GetFolder(inboxes[0].ID)
		inboxItems, err := tx.ListItems(inboxes[0].ID, nil)
		if err != nil || len(inboxItems) != 1 {
			t.Fatalf("ListItems: %d, %v", len(inboxItems), err)
		}
		moved, err := tx.MoveItems([]string{inboxItems[0].ID}, archive.ID)
		if err != nil || len(moved) != 1 {
			t.Fatalf("MoveItems: %v, %v", moved, err)
		}
		if moved[0].FolderID != archive.ID || moved[0].UID != 1 || moved[0].ID == inboxItems[0].ID {
			t.Errorf("the moved item is %+v", moved[0])
		}
		after, _ := tx.GetFolder(inboxes[0].ID)
		if after.ModSeq <= before.ModSeq {
			t.Error("moving out did not move the source folder's modseq")
		}
		expunged, err := tx.ListExpunged(inboxes[0].ID, before.ModSeq)
		if err != nil || len(expunged) != 1 || expunged[0].UID != 1 {
			t.Errorf("the expunge log did not record the move: %+v, %v", expunged, err)
		}
		// The next item in the Inbox gets UID 2, never 1 again.
		next, err := tx.AddItem(inboxes[0].ID, mailId, models.MailboxItemFlags{})
		if err != nil || next.UID != 2 {
			t.Errorf("the next UID is %d, want 2", next.UID)
		}
	})

	// Deleting every item starts the clock; deleting a mailbox does too.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		items, err := tx.ListItemsByMail(mailId)
		if err != nil {
			t.Fatalf("ListItemsByMail: %s", err)
		}
		itemIds := make([]string, 0, len(items))
		for _, item := range items {
			itemIds = append(itemIds, item.ID)
		}
		if _, err := tx.DeleteItems(itemIds[:len(itemIds)-1]); err != nil {
			t.Fatalf("DeleteItems: %s", err)
		}
		mail, _ := tx.GetMail(mailId, nil)
		if mail.UnreferencedAt != nil {
			t.Error("the clock started while an item still held the message")
		}
		if _, err := tx.DeleteItems(itemIds[len(itemIds)-1:]); err != nil {
			t.Fatalf("DeleteItems: %s", err)
		}
		mail, _ = tx.GetMail(mailId, nil)
		if mail.UnreferencedAt == nil {
			t.Error("the clock did not start when the last item let go")
		}
		if _, err := tx.AddItem(inboxes[1].ID, mailId, models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		mail, _ = tx.GetMail(mailId, nil)
		if mail.UnreferencedAt != nil {
			t.Error("holding the message again did not stop the clock")
		}
		if err := tx.DeleteMailbox(mailboxes[1].ID); err != nil {
			t.Fatalf("DeleteMailbox: %s", err)
		}
		mail, _ = tx.GetMail(mailId, nil)
		if mail.UnreferencedAt == nil {
			t.Error("deleting the mailbox that held the message did not start the clock")
		}
	})
}

// Flags stamp the item with the folder's next modseq, so a client asking
// "what changed since" gets exactly the changed ones.
func TestFlagsMoveTheModSeq(t *testing.T) {
	dbtest.RunTransaction(t, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "flagger"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		inbox, _ := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		var items []*models.MailboxItem
		for index := 0; index < 3; index++ {
			mail, err := tx.CreateMail(&models.Mail{Subject: "x", Kind: models.MailKindIncoming}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			item, err := tx.AddItem(inbox.ID, mail.ID, models.MailboxItemFlags{})
			if err != nil {
				t.Fatalf("AddItem: %s", err)
			}
			items = append(items, item)
		}
		folder, _ := tx.GetFolder(inbox.ID)
		since := folder.ModSeq
		yes := true
		if _, err := tx.SetItemFlags([]string{items[1].ID}, models.MailboxItemFlags{Seen: &yes}); err != nil {
			t.Fatalf("SetItemFlags: %s", err)
		}
		changed, err := tx.ListItems(inbox.ID, &db.ItemOptions{SinceModSeq: since})
		if err != nil || len(changed) != 1 || changed[0].ID != items[1].ID || !changed[0].Seen {
			t.Errorf("changed since %d: %+v, %v", since, changed, err)
		}
		unread, err := tx.CountItems(inbox.ID, &db.ItemOptions{Unseen: &yes})
		if err != nil || unread != 2 {
			t.Errorf("unread = %d, want 2", unread)
		}
	})
}
