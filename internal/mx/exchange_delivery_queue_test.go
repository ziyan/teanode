package mx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type blockedQueueStorage struct {
	storage.Storage
	entered   chan string
	release   chan struct{}
	loadCount atomic.Int32
}

func (self *blockedQueueStorage) Get(ctx context.Context, mailId string) ([]string, []byte, error) {
	self.loadCount.Add(1)
	self.entered <- mailId
	select {
	case <-self.release:
		return []string{"Subject: Queued fixture"}, []byte("Fixture body"), nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func TestDeliveryQueueDrainsBoundedBatchesAndStops(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	var deliveryIds []string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for range 9 {
			mail, err := transaction.CreateMail(&models.Mail{Subject: "Queued fixture"}, nil)
			if err != nil {
				test.Fatal(err)
			}
			delivery, err := transaction.CreateDelivery(&models.Delivery{MailID: mail.ID, Kind: models.DeliveryKindMailbox, Status: models.DeliveryStatusQueued, RetryAt: new(time.Now().Add(-time.Minute))}, nil)
			if err != nil {
				test.Fatal(err)
			}
			deliveryIds = append(deliveryIds, delivery.ID)
		}
	})
	spool := &blockedQueueStorage{entered: make(chan string, 9), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &exchange{database: database, storage: spool, ctx: ctx, deliveryWake: make(chan struct{}, 1)}
	worker.waitGroup.Add(1)
	go worker.runDeliveryQueue()
	test.Cleanup(func() { cancel(); worker.waitGroup.Wait() })
	for range 8 {
		select {
		case <-spool.entered:
		case <-time.After(3 * time.Second):
			test.Fatal("startup did not dispatch the pending batch")
		}
	}
	select {
	case <-spool.entered:
		test.Fatal("worker claimed a second batch before finishing the first")
	case <-time.After(100 * time.Millisecond):
	}
	close(spool.release)
	select {
	case <-spool.entered:
	case <-time.After(3 * time.Second):
		test.Fatal("backlog waited for the idle poll")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		deliveredCount := 0
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			for _, deliveryId := range deliveryIds {
				delivery, err := transaction.GetDelivery(deliveryId, nil)
				if err != nil {
					test.Fatal(err)
				}
				if delivery.Status == models.DeliveryStatusDelivered {
					deliveredCount++
				}
			}
		})
		if deliveredCount == len(deliveryIds) {
			break
		}
		if time.Now().After(deadline) {
			test.Fatalf("only %d deliveries completed", deliveredCount)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if spool.loadCount.Load() != 9 {
		test.Fatal("queue loaded a message more than once")
	}
	// An idle queue must observe cancellation without waiting for its poll.
	cancel()
	stopped := make(chan struct{})
	go func() { worker.waitGroup.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		test.Fatal("idle delivery worker did not stop")
	}
}

type observedQueueDatabase struct {
	db.Database
	emptyScan chan struct{}
}

type observedQueueTransaction struct {
	db.Transaction
	emptyScan chan struct{}
}

func (self *observedQueueDatabase) TransactionContext(ctx context.Context, callback func(db.Transaction) error) error {
	return self.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return callback(&observedQueueTransaction{Transaction: transaction, emptyScan: self.emptyScan})
	})
}

func (self *observedQueueTransaction) ListDeliveriesToRetry(options *db.Options) ([]*models.Delivery, error) {
	deliveries, err := self.Transaction.ListDeliveriesToRetry(options)
	if err == nil && len(deliveries) == 0 {
		select {
		case self.emptyScan <- struct{}{}:
		default:
		}
	}
	return deliveries, err
}

func TestDeliveryQueueWakeDoesNotWaitForIdlePoll(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	observed := &observedQueueDatabase{Database: database, emptyScan: make(chan struct{}, 1)}
	spool := &blockedQueueStorage{entered: make(chan string, 1), release: make(chan struct{})}
	close(spool.release)
	ctx, cancel := context.WithCancel(context.Background())
	worker := &exchange{database: observed, storage: spool, ctx: ctx, deliveryWake: make(chan struct{}, 1)}
	worker.waitGroup.Add(1)
	go worker.runDeliveryQueue()
	test.Cleanup(func() { cancel(); worker.waitGroup.Wait() })
	select {
	case <-observed.emptyScan:
	case <-time.After(3 * time.Second):
		test.Fatal("worker never became idle")
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Wake fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateDelivery(&models.Delivery{MailID: mail.ID, Kind: models.DeliveryKindMailbox, Status: models.DeliveryStatusQueued, RetryAt: new(time.Now().Add(-time.Minute))}, nil); err != nil {
			test.Fatal(err)
		}
		transaction.AfterCommit(worker.wakeDeliveryQueue)
	})
	select {
	case <-spool.entered:
	case <-time.After(3 * time.Second):
		test.Fatal("committed work waited for the five-second idle poll")
	}
}
