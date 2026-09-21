package mx

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestAliasUsageCountsOnlyCommittedDeliveries(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	exchange := &exchange{}
	domain := &models.Domain{ID: "fixture-domain", Domain: "example.com", Aliases: []*models.Alias{{
		ID: "fixture-alias", Kind: models.AliasKindEmail, Email: "destination@example.net",
	}}}
	injectedErr := errors.New("delivery transaction failed")
	for _, shouldFail := range []bool{true, false} {
		err := database.Transaction(func(transaction db.Transaction) error {
			mail, err := transaction.CreateMail(&models.Mail{Size: 64, ReceivedAt: time.Now()}, nil)
			if err != nil {
				return err
			}
			deliveries, err := exchange.matchAliases(transaction, domain, "recipient", mail)
			if err != nil {
				return err
			}
			if _, err := transaction.CreateDeliveries(deliveries, nil); err != nil {
				return err
			}
			if len(exchange.aliasUsagesMap) != 0 {
				return errors.New("usage changed before delivery commit")
			}
			if shouldFail {
				return injectedErr
			}
			return nil
		})
		if shouldFail {
			if !errors.Is(err, injectedErr) || len(exchange.aliasUsagesMap) != 0 {
				test.Fatalf("failed delivery changed usage: %+v, %v", exchange.aliasUsagesMap, err)
			}
		} else if err != nil {
			test.Fatal(err)
		}
	}
	usages := exchange.aliasUsagesMap["fixture-alias"]
	if len(usages) == 0 {
		test.Fatal("committed delivery has no usage")
	}
	for _, usage := range usages {
		if usage.Values[0] != 64 || usage.Values[2] != 1 {
			test.Fatalf("usage included the rolled-back delivery: %v", usage.Values)
		}
	}
}
