package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestDeliveryWorkersClaimDisjointBatchesWithoutWaiting(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Queued fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		retryAt := time.Now().Add(-time.Minute)
		for deliveryIndex := 0; deliveryIndex < 16; deliveryIndex++ {
			if _, err := transaction.CreateDelivery(&models.Delivery{MailID: mail.ID, Recipient: "recipient@example.com", Kind: models.DeliveryKindExternal, RetryAt: &retryAt}, nil); err != nil {
				test.Fatal(err)
			}
		}
	})
	firstBatch := make(chan []*models.Delivery, 1)
	firstFinished := make(chan error, 1)
	releaseFirst := make(chan struct{})
	test.Cleanup(func() {
		close(releaseFirst)
		if err := <-firstFinished; err != nil {
			test.Errorf("first worker: %v", err)
		}
	})
	go func() {
		defer close(firstBatch)
		firstFinished <- database.Transaction(func(transaction db.Transaction) error {
			deliveries, err := transaction.ListDeliveriesToRetry(nil)
			firstBatch <- deliveries
			if err != nil {
				return err
			}
			<-releaseFirst
			return nil
		})
	}()
	claimedFirst := <-firstBatch
	if len(claimedFirst) != 8 {
		test.Fatalf("first batch = %d, want 8", len(claimedFirst))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var claimedSecond []*models.Delivery
	if err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		claimedSecond, err = transaction.ListDeliveriesToRetry(nil)
		return err
	}); err != nil {
		test.Fatalf("second worker waited for the first: %v", err)
	}
	if len(claimedSecond) != 8 {
		test.Fatalf("second batch = %d, want 8", len(claimedSecond))
	}
	identifiers := make(map[string]bool)
	for _, delivery := range append(claimedFirst, claimedSecond...) {
		if identifiers[delivery.ID] {
			test.Fatalf("delivery %s claimed by both workers", delivery.ID)
		}
		identifiers[delivery.ID] = true
		if delivery.RetryAt == nil || !delivery.RetryAt.After(time.Now()) {
			test.Fatal("claim did not postpone retry")
		}
	}
}

func TestStorageRetryCannotOverwriteAnotherDeliveryLease(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var delivery *models.Delivery
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Lease fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		delivery, err = transaction.CreateDelivery(&models.Delivery{MailID: mail.ID, Kind: models.DeliveryKindExternal, RetryAt: new(time.Now().Add(-time.Minute))}, nil)
		if err != nil {
			test.Fatal(err)
		}
	})
	claim := func() *models.Delivery {
		test.Helper()
		var claimed *models.Delivery
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			deliveries, err := transaction.ListDeliveriesToRetry(nil)
			if err != nil || len(deliveries) != 1 {
				test.Fatalf("claim: %d, %v", len(deliveries), err)
			}
			claimed = deliveries[0]
		})
		return claimed
	}
	first := claim()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		wasDeferred, err := transaction.DeferDeliveryRetry(first.ID, *first.RetryAt, time.Now().Add(-time.Second))
		if err != nil || !wasDeferred {
			test.Fatalf("owned retry: %t, %v", wasDeferred, err)
		}
	})
	second := claim()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		wasDeferred, err := transaction.DeferDeliveryRetry(first.ID, *first.RetryAt, time.Now().Add(time.Minute))
		if err != nil || wasDeferred {
			test.Fatalf("stale retry replaced a newer claim: %t, %v", wasDeferred, err)
		}
		stored, err := transaction.GetDelivery(delivery.ID, nil)
		if err != nil || stored == nil || stored.RetryAt == nil || !stored.RetryAt.Equal(*second.RetryAt) || stored.Attempts != 0 {
			test.Fatalf("lease changed: %+v, %v", stored, err)
		}
		if _, err := transaction.ModifyDelivery(delivery.ID, func(stored *models.Delivery) error {
			stored.RetryAt = nil
			stored.Status = models.DeliveryStatusDelivered
			return nil
		}, nil); err != nil {
			test.Fatal(err)
		}
		wasDeferred, err = transaction.DeferDeliveryRetry(second.ID, *second.RetryAt, time.Now().Add(time.Minute))
		if err != nil || wasDeferred {
			test.Fatalf("retry requeued completed mail: %t, %v", wasDeferred, err)
		}
	})
}
