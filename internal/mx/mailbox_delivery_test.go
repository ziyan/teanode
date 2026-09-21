package mx

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A delivery into a mailbox is made when it is created, so it is not queued
// again. It used to be: every mailbox delivery went through deliver(), which
// has no step for the kind, so it was left "attempted" with no error and
// retried on the backoff ladder until it was dropped — while the message sat
// in the Inbox the whole time.
func TestAMailboxDeliveryIsNotQueuedAgain(t *testing.T) {
	t.Parallel()

	delivered := &models.Delivery{Kind: models.DeliveryKindMailbox, Status: models.DeliveryStatusDelivered}
	queued := &models.Delivery{Kind: models.DeliveryKindForward, Status: models.DeliveryStatusQueued}
	pending := pendingDeliveries([]*models.Delivery{delivered, queued})
	if len(pending) != 1 || pending[0] != queued {
		t.Errorf("only the forward should be pending, got %+v", pending)
	}
}

// The retry loop selects by retry time alone, so a mailbox delivery a
// previous release left on the ladder is still handed to deliver(). It is
// settled there: delivered, no retry, and the time it was actually made is
// kept rather than replaced by the time it was noticed.
func TestARetriedMailboxDeliveryIsSettled(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var delivery *models.Delivery
	made := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	retry := time.Now().Add(-time.Minute)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mail, err := tx.CreateMail(&models.Mail{DomainID: "domain-one", Sender: "sender@example.com"}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		delivery, err = tx.CreateDelivery(&models.Delivery{
			MailID:      mail.ID,
			Recipient:   "hello@example.com",
			Kind:        models.DeliveryKindMailbox,
			Status:      models.DeliveryStatusAttempted,
			Attempts:    4,
			DeliveredAt: &made,
			RetryAt:     &retry,
		}, nil)
		if err != nil {
			t.Fatalf("CreateDelivery: %s", err)
		}
	})

	exchange := &exchange{database: database}
	if err := exchange.deliver(context.Background(), delivery); err != nil {
		t.Fatalf("deliver: %s", err)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		settled, err := tx.GetDelivery(delivery.ID, nil)
		if err != nil {
			t.Fatalf("GetDelivery: %s", err)
		}
		if settled.Status != models.DeliveryStatusDelivered {
			t.Errorf("status = %q, want delivered", settled.Status)
		}
		if settled.RetryAt != nil {
			t.Errorf("retry at = %s, want none", settled.RetryAt)
		}
		if settled.Error != "" {
			t.Errorf("error = %q, want none", settled.Error)
		}
		if settled.DeliveredAt == nil || !settled.DeliveredAt.Equal(made) {
			t.Errorf("delivered at = %v, want the time it was made, %s", settled.DeliveredAt, made)
		}
		// Nothing to pick up any more.
		again, err := tx.ListDeliveriesToRetry(nil)
		if err != nil {
			t.Fatalf("ListDeliveriesToRetry: %s", err)
		}
		for _, candidate := range again {
			if candidate.ID == delivery.ID {
				t.Errorf("the settled delivery is still on the retry ladder")
			}
		}
	})
}
