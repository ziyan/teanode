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

// A day is written once per provider and model: writing it again replaces the
// amount, days are listed from the day since falls on up to but not including
// the day until falls on, and older days can be deleted.
func TestAgentBilledDaysAreReplacedListedAndDeleted(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	firstDay := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	days := []models.AgentBilledDay{
		{BilledOn: firstDay, Model: "chat-model", BilledAmount: 1.25, CurrencyCode: "usd"},
		{BilledOn: firstDay.AddDate(0, 0, 1), Model: "chat-model", BilledAmount: 2.5, CurrencyCode: "usd"},
		{BilledOn: firstDay.AddDate(0, 0, 2), Model: "chat-model", BilledAmount: 0.125, CurrencyCode: "usd"},
	}
	var listed []*models.AgentBilledDay
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		if err = tx.PutAgentBilledDays("first", days); err != nil {
			return err
		}
		if err = tx.PutAgentBilledDays("second", days[:1]); err != nil {
			return err
		}
		days[0].BilledAmount = 1.5
		if err = tx.PutAgentBilledDays("first", days[:1]); err != nil {
			return err
		}
		listed, err = tx.ListAgentBilledDays(firstDay.Add(5*time.Hour), firstDay.AddDate(0, 0, 2).Add(time.Hour))
		return err
	}); err != nil {
		test.Fatal(err)
	}
	// 2026-03-01 and 2026-03-02 for the first provider, 2026-03-01 for the second.
	if len(listed) != 3 || listed[0].ProviderName != "first" || listed[0].BilledAmount != 1.5 ||
		listed[1].ProviderName != "second" || listed[1].BilledAmount != 1.25 ||
		listed[2].ProviderName != "first" || !listed[2].BilledOn.Equal(firstDay.AddDate(0, 0, 1)) || listed[2].CurrencyCode != "usd" {
		test.Errorf("billed days listed wrongly: %+v", listed)
	}

	if err := database.Transaction(func(tx db.Transaction) (err error) {
		if err = tx.DeleteAgentBilledDaysBefore(firstDay.AddDate(0, 0, 2).Add(3 * time.Hour)); err != nil {
			return err
		}
		listed, err = tx.ListAgentBilledDays(firstDay, time.Time{})
		return err
	}); err != nil {
		test.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].BilledOn.Equal(firstDay.AddDate(0, 0, 2)) {
		test.Errorf("older days were not deleted: %+v", listed)
	}
}
