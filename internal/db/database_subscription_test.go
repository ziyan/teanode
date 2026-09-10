package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The mailing lists a mailbox receives: one row per list however many messages
// it has sent, counts that say how much of it is unread, and the newest
// message of each — which is where the way to leave comes from, since a list
// can change its unsubscribe address between issues.
func TestSubscriptionsGroupTheMailOfEachList(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var mailbox *models.Mailbox
	var inbox, junk, trash *models.MailboxFolder
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		for _, pair := range []struct {
			kind   models.MailboxFolderKind
			folder **models.MailboxFolder
		}{
			{models.MailboxFolderKindInbox, &inbox},
			{models.MailboxFolderKindJunk, &junk},
			{models.MailboxFolderKindTrash, &trash},
		} {
			folder, err := tx.GetFolderByKind(mailbox.ID, pair.kind)
			if err != nil || folder == nil {
				t.Fatalf("a new mailbox has no %s: %v", pair.kind, err)
			}
			*pair.folder = folder
		}
	})

	// Two issues of one newsletter, one of another, one piece of ordinary
	// mail, one newsletter in Junk and one in Trash.
	base := time.Now().Add(-time.Hour)
	add := func(tx db.Transaction, folder *models.MailboxFolder, subject, listKey, listName, unsubscribe string,
		oneClick bool, at time.Time, seen bool) {
		mail, err := tx.CreateMail(&models.Mail{
			Subject:         subject,
			From:            "news@example.com",
			Kind:            models.MailKindIncoming,
			ReceivedAt:      at,
			ListKey:         listKey,
			ListName:        listName,
			ListUnsubscribe: unsubscribe,
			ListOneClick:    oneClick,
			ListChecked:     true,
		}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{Seen: &seen}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		add(tx, inbox, "Issue 40", "weekly.example.com", "Example Weekly",
			"https://example.com/old", false, base, true)
		add(tx, inbox, "Issue 41", "weekly.example.com", "Example Weekly",
			"https://example.com/new, mailto:leave@example.com", true, base.Add(30*time.Minute), false)
		add(tx, inbox, "Half price", "offers@shop.example.com", "Example Shop",
			"mailto:stop@shop.example.com", false, base.Add(10*time.Minute), false)
		add(tx, inbox, "lunch?", "", "", "", false, base.Add(20*time.Minute), false)
		add(tx, junk, "You have won", "prize@spam.example.com", "Prize",
			"https://spam.example.com/u", false, base.Add(15*time.Minute), false)
		add(tx, trash, "Old news", "gone.example.com", "Gone",
			"https://example.com/gone", false, base.Add(5*time.Minute), false)
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		subscriptions, err := tx.ListSubscriptions(mailbox.ID, 0, 0, true)
		if err != nil {
			t.Fatalf("ListSubscriptions: %s", err)
		}
		if len(subscriptions) != 2 {
			names := make([]string, 0, len(subscriptions))
			for _, subscription := range subscriptions {
				names = append(names, subscription.Key)
			}
			t.Fatalf("ListSubscriptions returned %d rows (%q), want 2: ordinary mail, Junk and Trash are not subscriptions",
				len(subscriptions), names)
		}

		// Newest first, so the newsletter that wrote most recently leads.
		if subscriptions[0].Key != "weekly.example.com" {
			t.Errorf("first subscription is %q, want the one that wrote most recently", subscriptions[0].Key)
		}
		weekly := subscriptions[0]
		if weekly.Count != 2 {
			t.Errorf("Count = %d, want 2", weekly.Count)
		}
		if weekly.Unread != 1 {
			t.Errorf("Unread = %d, want 1", weekly.Unread)
		}
		if weekly.Name != "Example Weekly" {
			t.Errorf("Name = %q", weekly.Name)
		}
		// From the newest message, because a list can change how it is left
		// between issues and the old address may be dead.
		if !weekly.OneClick {
			t.Errorf("OneClick = false, want the newest message's answer")
		}
		if len(weekly.Unsubscribe) != 2 || weekly.Unsubscribe[0] != "https://example.com/new" {
			t.Errorf("Unsubscribe = %q, want the newest message's addresses", weekly.Unsubscribe)
		}
		if weekly.RequestedAt != nil {
			t.Errorf("RequestedAt = %v on a list nobody has left", weekly.RequestedAt)
		}

		shop := subscriptions[1]
		if shop.Key != "offers@shop.example.com" || shop.Count != 1 || shop.OneClick {
			t.Errorf("the second subscription reads %+v", shop)
		}

		count, err := tx.CountSubscriptions(mailbox.ID, true)
		if err != nil {
			t.Fatalf("CountSubscriptions: %s", err)
		}
		if count != 2 {
			t.Errorf("CountSubscriptions = %d, want 2", count)
		}

		one, err := tx.GetSubscription(mailbox.ID, "weekly.example.com")
		if err != nil || one == nil {
			t.Fatalf("GetSubscription: %v %v", one, err)
		}
		if one.LastItemID == "" {
			t.Errorf("GetSubscription gave no item to open")
		}
	})

	// Asking to leave is recorded against the mailbox and the list, and
	// asking again writes the same row rather than a second one.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.RecordUnsubscribe(mailbox.ID, "weekly.example.com", models.UnsubscribeOneClick, true, "500"); err != nil {
			t.Fatalf("RecordUnsubscribe: %s", err)
		}
		if err := tx.RecordUnsubscribe(mailbox.ID, "weekly.example.com", models.UnsubscribeOneClick, false, ""); err != nil {
			t.Fatalf("RecordUnsubscribe again: %s", err)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		subscription, err := tx.GetSubscription(mailbox.ID, "weekly.example.com")
		if err != nil || subscription == nil {
			t.Fatalf("GetSubscription: %v %v", subscription, err)
		}
		if subscription.RequestedAt == nil {
			t.Fatalf("the request to leave was not recorded")
		}
		if subscription.Failed || subscription.Error != "" {
			t.Errorf("the second attempt did not clear the first one's failure: %+v", subscription)
		}
		if subscription.Method != models.UnsubscribeOneClick {
			t.Errorf("Method = %q", subscription.Method)
		}
		count, err := tx.CountSubscriptions(mailbox.ID, true)
		if err != nil {
			t.Fatalf("CountSubscriptions: %s", err)
		}
		if count != 2 {
			t.Errorf("leaving a list changed the count to %d; it is still a list this mailbox has mail from", count)
		}
	})
}
