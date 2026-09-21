package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A folder's list is one row per conversation, not one per message.
//
// Three messages of one conversation and one of another are four items and
// two rows. The row carries the newest message of its conversation, how many
// there are, how many are unread, and who wrote them, oldest first — which is
// everything the list shows without going back for each conversation.
func TestListThreadsGroupsAFolderByConversation(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

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

		start := time.Now().Add(-time.Hour)
		// The message that begins a conversation carries its own id, which is
		// what the exchange does when nothing is answered.
		first, err := tx.CreateMail(&models.Mail{
			Subject: "the question", From: "ada@example.com", FromName: "Ada",
			ReceivedAt: start, Kind: models.MailKindIncoming,
		}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.ModifyMail(first.ID, func(mail *models.Mail) error {
			mail.ThreadID = mail.ID
			return nil
		}, nil); err != nil {
			t.Fatalf("ModifyMail: %s", err)
		}

		answers := []struct {
			subject string
			from    string
			name    string
			at      time.Time
		}{
			{"Re: the question", "grace@example.com", "Grace", start.Add(10 * time.Minute)},
			{"Re: the question", "ada@example.com", "Ada", start.Add(20 * time.Minute)},
		}
		var replies []*models.Mail
		for _, answer := range answers {
			reply, err := tx.CreateMail(&models.Mail{
				ThreadID: first.ID, Subject: answer.subject, From: answer.from, FromName: answer.name,
				ReceivedAt: answer.at, Kind: models.MailKindIncoming,
			}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			replies = append(replies, reply)
		}

		other, err := tx.CreateMail(&models.Mail{
			Subject: "unrelated", From: "linus@example.com", FromName: "Linus",
			ReceivedAt: start.Add(5 * time.Minute), Kind: models.MailKindIncoming,
		}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.ModifyMail(other.ID, func(mail *models.Mail) error {
			mail.ThreadID = mail.ID
			return nil
		}, nil); err != nil {
			t.Fatalf("ModifyMail: %s", err)
		}

		// Every message of both conversations is filed in the Inbox in the
		// order they arrived, so that UID order and time order agree.
		seen := true
		filing := []struct {
			mail  *models.Mail
			flags models.MailboxItemFlags
		}{
			// The first message has been read; the two answers have not, so
			// the row should say three messages and two unread.
			{first, models.MailboxItemFlags{Seen: &seen}},
			{other, models.MailboxItemFlags{Seen: &seen}},
			{replies[0], models.MailboxItemFlags{}},
			{replies[1], models.MailboxItemFlags{}},
		}
		for _, entry := range filing {
			if _, err := tx.AddItem(inbox.ID, entry.mail.ID, "", entry.flags); err != nil {
				t.Fatalf("AddItem: %s", err)
			}
		}

		threads, err := tx.ListThreads(inbox.ID, &db.ItemOptions{Limit: 50})
		if err != nil {
			t.Fatalf("ListThreads: %s", err)
		}
		if len(threads) != 2 {
			t.Fatalf("four messages in two conversations gave %d rows, want 2", len(threads))
		}

		// Newest conversation first: the answered one, whose newest message
		// arrived twenty minutes in.
		newest := threads[0]
		if newest.ThreadID != first.ID {
			t.Errorf("the first row is conversation %q, want the answered one %q", newest.ThreadID, first.ID)
		}
		if newest.Count != 3 {
			t.Errorf("the conversation says %d messages, want 3", newest.Count)
		}
		if newest.Unread != 2 {
			t.Errorf("the conversation says %d unread, want 2", newest.Unread)
		}
		if newest.Item == nil {
			t.Fatalf("the row carries no message")
		}
		if newest.Item.MailID != replies[1].ID {
			t.Errorf("the row shows message %q, want the newest of the conversation %q", newest.Item.MailID, replies[1].ID)
		}
		want := []string{"Ada", "Grace"}
		if len(newest.Participants) != len(want) {
			t.Fatalf("participants are %q, want %q", newest.Participants, want)
		}
		for index, name := range want {
			if newest.Participants[index] != name {
				t.Errorf("participant %d is %q, want %q", index, newest.Participants[index], name)
			}
		}

		if len(newest.ItemIDs) != 3 {
			t.Errorf("the row carries %d item ids, want 3 — the conversation's messages in this folder", len(newest.ItemIDs))
		}

		if threads[1].ThreadID != other.ID {
			t.Errorf("the second row is conversation %q, want the unrelated one %q", threads[1].ThreadID, other.ID)
		}
		if threads[1].Count != 1 {
			t.Errorf("the unrelated conversation says %d messages, want 1", threads[1].Count)
		}

		count, err := tx.CountThreads(inbox.ID, &db.ItemOptions{})
		if err != nil {
			t.Fatalf("CountThreads: %s", err)
		}
		if count != 2 {
			t.Errorf("CountThreads says %d, want 2", count)
		}
	})
}

// A conversation is read across the whole mailbox, not one folder: your own
// answers are in Sent, and the conversation is being read from the Inbox.
func TestThreadItemsSpanFolders(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

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
		sent, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindSent)
		if err != nil || sent == nil {
			t.Fatalf("a new mailbox has no Sent folder: %v", err)
		}

		start := time.Now().Add(-time.Hour)
		question, err := tx.CreateMail(&models.Mail{
			Subject: "the question", From: "ada@example.com", ReceivedAt: start, Kind: models.MailKindIncoming,
		}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.ModifyMail(question.ID, func(mail *models.Mail) error {
			mail.ThreadID = mail.ID
			return nil
		}, nil); err != nil {
			t.Fatalf("ModifyMail: %s", err)
		}
		answer, err := tx.CreateMail(&models.Mail{
			ThreadID: question.ID, Subject: "Re: the question", From: "me@example.com",
			ReceivedAt: start.Add(time.Minute), Kind: models.MailKindOutgoing,
		}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.AddItem(inbox.ID, question.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		if _, err := tx.AddItem(sent.ID, answer.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}

		items, err := tx.ListItems("", &db.ItemOptions{MailboxID: mailbox.ID, ThreadID: question.ID, Limit: 50})
		if err != nil {
			t.Fatalf("ListItems: %s", err)
		}
		if len(items) != 2 {
			t.Fatalf("the conversation has %d messages across the mailbox, want 2", len(items))
		}
		folders := map[string]bool{}
		for _, item := range items {
			folders[item.FolderID] = true
		}
		if !folders[inbox.ID] || !folders[sent.ID] {
			t.Errorf("the conversation misses a folder: got %v, want the Inbox and Sent", folders)
		}
	})
}
