package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Whether a message's remote pictures may be shown without asking again.
//
// Two ways to have said yes, and they are not the same promise: this message,
// and every message from this list. Both are the reader's own — the question
// is about a mailbox, not about the message, since two people who received the
// same newsletter answer it separately.
func TestImagesAllowedFor(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader3"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		other, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Second"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("no Inbox: %v", err)
		}

		const list = "weekly.news.example.com"
		mail, err := tx.CreateMail(&models.Mail{
			From:    "news@example.com",
			Subject: "an issue",
			Headers: []string{"From: news@example.com", "List-Id: Example Weekly <" + list + ">"},
		}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		item, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{})
		if err != nil {
			t.Fatalf("AddItem: %s", err)
		}

		mine := []string{mailbox.ID}
		allowed := func(mailboxIds []string, key string) bool {
			t.Helper()
			answer, err := tx.ImagesAllowedFor(mailboxIds, mail.ID, key)
			if err != nil {
				t.Fatalf("ImagesAllowedFor: %s", err)
			}
			return answer
		}

		if allowed(mine, list) {
			t.Error("nothing has been said yet, so the question should still be asked")
		}

		// Said for this message.
		if err := tx.SetItemImages([]string{item.ID}, true); err != nil {
			t.Fatalf("SetItemImages: %s", err)
		}
		if !allowed(mine, list) {
			t.Error("the reader loaded this message's pictures; it should not be asked again")
		}

		// Another mailbox holding no item of this message has said nothing,
		// whoever owns it.
		if allowed([]string{other.ID}, list) {
			t.Error("a mailbox that has not answered should still be asked")
		}

		// Taken back.
		if err := tx.SetItemImages([]string{item.ID}, false); err != nil {
			t.Fatalf("SetItemImages: %s", err)
		}
		if allowed(mine, list) {
			t.Error("the choice was taken back, so the question comes back with it")
		}

		// Said for the whole list instead, which covers a message nothing was
		// said about.
		if err := tx.SetSubscriptionImages(mailbox.ID, list, true); err != nil {
			t.Fatalf("SetSubscriptionImages: %s", err)
		}
		if !allowed(mine, list) {
			t.Error("the reader said to load this list's pictures always")
		}
		if allowed(mine, "") {
			t.Error("a message belonging to no list is not covered by a list's answer")
		}
		if allowed(mine, "other.example.com") {
			t.Error("one list's answer does not speak for another's")
		}
	})
}

// Reading a subscription shows the mail the listing counted, and no other.
//
// The listing leaves out Trash and Junk on purpose — what you threw away is
// not a subscription you have, and what a filter caught is not one you agreed
// to. The reader used the ordinary item query, which leaves out neither, so a
// list said it had three messages and then showed five.
func TestASubscriptionsMailLeavesOutTrashAndJunk(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader4"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		folder := func(kind models.MailboxFolderKind) *models.MailboxFolder {
			t.Helper()
			found, err := tx.GetFolderByKind(mailbox.ID, kind)
			if err != nil || found == nil {
				t.Fatalf("no %s folder: %v", kind, err)
			}
			return found
		}

		const list = "weekly.news.example.com"
		file := func(kind models.MailboxFolderKind, subject string, deleted bool) {
			t.Helper()
			mail, err := tx.CreateMail(&models.Mail{
				From:    "news@example.com",
				Subject: subject,
				Headers: []string{"From: news@example.com", "List-Id: Example Weekly <" + list + ">"},
			}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			item, err := tx.AddItem(folder(kind).ID, mail.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatalf("AddItem: %s", err)
			}
			if deleted {
				if _, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Deleted: &deleted}); err != nil {
					t.Fatalf("SetItemFlags: %s", err)
				}
			}
		}

		file(models.MailboxFolderKindInbox, "in the inbox", false)
		file(models.MailboxFolderKindArchive, "archived", false)
		file(models.MailboxFolderKindTrash, "thrown away", false)
		file(models.MailboxFolderKindJunk, "caught by the filter", false)
		file(models.MailboxFolderKindInbox, "marked deleted", true)

		notDeleted := false
		items, err := tx.ListItems("", &db.ItemOptions{
			MailboxID:    mailbox.ID,
			ListKey:      list,
			ByReceived:   true,
			Deleted:      &notDeleted,
			ExcludeKinds: []models.MailboxFolderKind{models.MailboxFolderKindTrash, models.MailboxFolderKindJunk},
		})
		if err != nil {
			t.Fatalf("ListItems: %s", err)
		}

		subjects := map[string]bool{}
		for _, item := range items {
			mail, err := tx.GetMail(item.MailID, nil)
			if err != nil || mail == nil {
				t.Fatalf("GetMail: %v", err)
			}
			subjects[mail.Subject] = true
		}
		for _, want := range []string{"in the inbox", "archived"} {
			if !subjects[want] {
				t.Errorf("%q should be part of the subscription", want)
			}
		}
		for _, unwanted := range []string{"thrown away", "caught by the filter", "marked deleted"} {
			if subjects[unwanted] {
				t.Errorf("%q should not be shown as part of the subscription", unwanted)
			}
		}

		// And the count beside the list's name agrees, which is the point.
		subscription, err := tx.GetSubscription(mailbox.ID, list)
		if err != nil || subscription == nil {
			t.Fatalf("GetSubscription: %v", err)
		}
		if subscription.Count != len(items) {
			t.Errorf("the list says it has %d messages and shows %d", subscription.Count, len(items))
		}
	})
}
