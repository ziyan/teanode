package mx

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A list exists as soon as it writes.
//
// The row used to be made only when somebody did something about a list, so
// until then a list had no identity: nothing to link to, and nowhere to keep
// what is known about it. Delivery makes it, once, and the item names it — so
// "what has this list sent me" is a lookup rather than a grouping over a text
// key on the mail.
func TestDeliveryMakesTheSubscription(t *testing.T) {
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
		alias := &models.Alias{}

		deliver := func(subject, listKey string) *models.MailboxItem {
			t.Helper()
			headers := []string{"From: Example Weekly <news@example.com>"}
			if listKey != "" {
				headers = append(headers, "List-Id: Example Weekly <"+listKey+">")
			}
			stored, err := tx.CreateMail(&models.Mail{
				From:       "news@example.com",
				Subject:    subject,
				ReceivedAt: time.Now(),
				Headers:    headers,
			}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if _, err := exchange.deliverToMailbox(tx, mailbox, alias, "reader@example.com", stored); err != nil {
				t.Fatalf("deliverToMailbox: %s", err)
			}
			items, err := tx.ListItemsByMail(stored.ID)
			if err != nil || len(items) != 1 {
				t.Fatalf("the message was not filed once: %v %d", err, len(items))
			}
			return items[0]
		}

		const list = "weekly.news.example.com"
		first := deliver("first issue", list)
		if first.SubscriptionID == "" {
			t.Fatal("a message from a list was filed without naming the list it came from")
		}

		subscription, err := tx.GetSubscription(mailbox.ID, list)
		if err != nil || subscription == nil {
			t.Fatalf("the list has no subscription after its first message: %v", err)
		}
		if subscription.ID != first.SubscriptionID {
			t.Errorf("the item names subscription %q, the list is %q", first.SubscriptionID, subscription.ID)
		}

		// The second issue joins the same list rather than making another.
		second := deliver("second issue", list)
		if second.SubscriptionID != first.SubscriptionID {
			t.Errorf("a second message from one list made a second subscription: %q then %q",
				first.SubscriptionID, second.SubscriptionID)
		}

		// And the identity is what a link can be made from.
		byIdentity, err := tx.GetSubscriptionByID(mailbox.ID, subscription.ID)
		if err != nil || byIdentity == nil {
			t.Fatalf("the subscription cannot be found by its own identity: %v", err)
		}
		if byIdentity.Key != list {
			t.Errorf("identity %q found the list %q, want %q", subscription.ID, byIdentity.Key, list)
		}

		// A message from nobody's list is nobody's list.
		ordinary := deliver("a person writing", "")
		if ordinary.SubscriptionID != "" {
			t.Errorf("an ordinary message was filed as part of list %q", ordinary.SubscriptionID)
		}
	})
}
