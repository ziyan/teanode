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
		item, err := tx.AddItem(inbox.ID, mail.ID, models.MailboxItemFlags{})
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
