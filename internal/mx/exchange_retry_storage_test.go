package mx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type unavailableMessageStorage struct{ storage.Storage }

func (self *unavailableMessageStorage) Get(context.Context, string) ([]string, []byte, error) {
	return nil, nil, errors.New("storage unavailable")
}

func TestDeliveryRetryDoesNotDispatchWithoutStoredContent(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var deliveryId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mail, err := transaction.CreateMail(&models.Mail{Subject: "Retry fixture"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		retryAt := time.Now().Add(-time.Minute)
		// A mailbox delivery exercises dispatch without any network transport.
		// It would be marked delivered if the worker ignored the load failure.
		delivery, err := transaction.CreateDelivery(&models.Delivery{
			MailID: mail.ID, Kind: models.DeliveryKindMailbox,
			Status: models.DeliveryStatusQueued, RetryAt: &retryAt,
		}, nil)
		if err != nil {
			test.Fatal(err)
		}
		deliveryId = delivery.ID
	})
	exchange := &exchange{database: database, storage: &unavailableMessageStorage{}, ctx: context.Background()}
	if _, err := exchange.deliverBatch(context.Background()); err != nil {
		test.Fatal(err)
	}
	exchange.waitGroup.Wait()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		delivery, err := transaction.GetDelivery(deliveryId, nil)
		if err != nil {
			test.Fatal(err)
		}
		if delivery.Status != models.DeliveryStatusQueued || delivery.Attempts != 0 || delivery.DeliveredAt != nil {
			test.Fatalf("storage failure was dispatched: %+v", delivery)
		}
		if delivery.RetryAt == nil || !delivery.RetryAt.After(time.Now()) || delivery.RetryAt.After(time.Now().Add(2*time.Minute)) {
			test.Fatal("storage failure lost its retry")
		}
	})
}
