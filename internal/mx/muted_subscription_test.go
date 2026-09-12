package mx

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A muted list keeps arriving and stops being in the way.
//
// The whole feature is this one choice at delivery: the Archive instead of the
// Inbox. Everything else about muting is a button and a row, and both are
// worth nothing if this does not hold — a reader who mutes a list and then
// finds it in the Inbox anyway has been told something untrue.
//
// It arrives unread. Muting says where a list's mail waits, not that it has
// been read, and the unread count is how somebody finds what is still waiting
// when they have time for it.
func TestAMutedListSkipsTheInbox(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	exchange := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("a new mailbox has no Inbox: %v", err)
		}
		archive, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindArchive)
		if err != nil || archive == nil {
			t.Fatalf("a new mailbox has no Archive: %v", err)
		}
		alias := &models.Alias{}

		const list = "weekly.news.example.com"
		// The key is read from the headers when the message is stored, not
		// taken from the caller, so these say what a newsletter says.
		deliver := func(name, listKey string, suspicious bool) *models.MailboxItem {
			t.Helper()
			headers := []string{"From: Example Weekly <news@example.com>"}
			if listKey != "" {
				headers = append(headers, "List-Id: Example Weekly <"+listKey+">")
			}
			mail := &models.Mail{
				From:       "news@example.com",
				Subject:    name,
				ReceivedAt: time.Now(),
				Headers:    headers,
			}
			if suspicious {
				// What the filter said, which is what isSuspicious reads.
				mail.AuthenticationResults.SpamFilter = &models.SpamFilterResult{Result: "fail"}
			}
			stored, err := tx.CreateMail(mail, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if _, err := exchange.deliverToMailbox(tx, mailbox, alias, "reader@example.com", stored); err != nil {
				t.Fatalf("deliverToMailbox: %s", err)
			}
			if stored.ListKey != listKey {
				t.Fatalf("the stored message has list key %q, want %q", stored.ListKey, listKey)
			}
			items, err := tx.ListItemsByMail(stored.ID)
			if err != nil || len(items) != 1 {
				t.Fatalf("the message was not filed once: %v %d", err, len(items))
			}
			return items[0]
		}

		// Before muting: the Inbox, unread, as any other message.
		item := deliver("before", list, false)
		if item.FolderID != inbox.ID {
			t.Errorf("an unmuted list was filed in %q, want the Inbox", item.FolderID)
		}

		if err := tx.SetSubscriptionMuted(mailbox.ID, list, true); err != nil {
			t.Fatalf("SetSubscriptionMuted: %s", err)
		}

		item = deliver("after", list, false)
		if item.FolderID != archive.ID {
			t.Errorf("a muted list was filed in %q, want the Archive", item.FolderID)
		}
		if item.Seen {
			t.Error("a muted list's mail should arrive unread; it is waiting to be read, not dealt with")
		}

		// Spam from a muted list is still spam. Muting is a reader saying
		// where they want mail they asked for; it is not a way for a sender
		// to get past the filter into a folder nobody is suspicious of.
		if junk, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk); err == nil && junk != nil {
			if item := deliver("spam", list, true); item.FolderID != junk.ID {
				t.Errorf("suspicious mail from a muted list was filed in %q, want Junk", item.FolderID)
			}
		}

		// Another list is unaffected, and unmuting puts this one back.
		if item := deliver("other", "other.example.com", false); item.FolderID != inbox.ID {
			t.Errorf("an unmuted list was filed in %q, want the Inbox", item.FolderID)
		}
		if err := tx.SetSubscriptionMuted(mailbox.ID, list, false); err != nil {
			t.Fatalf("SetSubscriptionMuted: %s", err)
		}
		if item := deliver("unmuted", list, false); item.FolderID != inbox.ID {
			t.Errorf("an unmuted list was filed in %q, want the Inbox", item.FolderID)
		}
	})
}

// Neither a mailing list nor an address that refuses answers becomes a
// contact. The address book is for people you might write to, and it is also
// what the "sender is known" rule reads.
func TestAListDoesNotBecomeAContact(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	exchange := &exchange{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "reader2"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		alias := &models.Alias{}

		deliver := func(from, listKey string) {
			t.Helper()
			headers := []string{"From: " + from}
			if listKey != "" {
				headers = append(headers, "List-Id: Example Weekly <"+listKey+">")
			}
			mail, err := tx.CreateMail(&models.Mail{
				From:       from,
				Subject:    "hello",
				ReceivedAt: time.Now(),
				Headers:    headers,
			}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if _, err := exchange.deliverToMailbox(tx, mailbox, alias, "reader2@example.com", mail); err != nil {
				t.Fatalf("deliverToMailbox: %s", err)
			}
		}

		deliver("ann@example.com", "")
		deliver("news@example.com", "weekly.news.example.com")
		deliver("no-reply@example.com", "")

		contacts, err := tx.ListLearnedContacts(mailbox.ID, "", 50)
		if err != nil {
			t.Fatalf("ListContacts: %s", err)
		}
		addresses := make([]string, 0, len(contacts))
		for _, contact := range contacts {
			addresses = append(addresses, contact.Address)
		}
		if len(addresses) != 1 || addresses[0] != "ann@example.com" {
			t.Errorf("contacts = %q, want only the person who can be written back to", addresses)
		}
	})
}
