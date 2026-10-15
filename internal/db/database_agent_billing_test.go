package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A provider has one check, the last one written; a check with nothing in it
// reads back with empty lists rather than nulls, and a provider never checked
// reads back as none.
func TestAgentBillingCheckIsReplacedByTheNext(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	checkedAt := time.Now().Round(time.Second)
	put := func(check *models.AgentBillingCheck) {
		test.Helper()
		if err := database.Transaction(func(tx db.Transaction) error { return tx.PutAgentBillingCheck(check) }); err != nil {
			test.Fatalf("PutAgentBillingCheck: %s", err)
		}
	}
	put(&models.AgentBillingCheck{
		ProviderName: "first",
		CheckedAt:    checkedAt.Add(-time.Hour),
		Compared:     []models.AgentBilledPrice{{Model: "chat-model", TokenKind: "output", ConfiguredPerMillion: 1, BilledPerMillion: 2, DayCount: 3, IsPattern: true}},
		Changed:      []models.AgentPriceChange{{Model: "chat-model", TokenKind: "output", BeforePerMillion: 1, AfterPerMillion: 2}},
	})
	put(&models.AgentBillingCheck{ProviderName: "first", CheckedAt: checkedAt, ErrorMessage: "the billing key was refused"})

	var found, missing *models.AgentBillingCheck
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		if found, err = tx.GetAgentBillingCheck("first"); err != nil {
			return err
		}
		missing, err = tx.GetAgentBillingCheck("second")
		return err
	}); err != nil {
		test.Fatalf("GetAgentBillingCheck: %s", err)
	}
	if missing != nil {
		test.Errorf("a provider never checked read back as %+v", missing)
	}
	if found == nil || !found.CheckedAt.Equal(checkedAt) || found.ErrorMessage != "the billing key was refused" || found.Compared == nil || len(found.Compared) != 0 || found.Changed == nil || len(found.Changed) != 0 {
		test.Errorf("the second check did not replace the first: %+v", found)
	}

	put(&models.AgentBillingCheck{
		ProviderName: "first", CheckedAt: checkedAt,
		Compared: []models.AgentBilledPrice{{Model: "chat-model", TokenKind: "cacheWrite", BilledPerMillion: 0.25, DayCount: 3}},
	})
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		found, err = tx.GetAgentBillingCheck("first")
		return err
	}); err != nil {
		test.Fatalf("GetAgentBillingCheck: %s", err)
	}
	if len(found.Compared) != 1 || found.Compared[0].BilledPerMillion != 0.25 || found.Compared[0].DayCount != 3 {
		test.Errorf("compared prices did not survive the round trip: %+v", found.Compared)
	}
}
