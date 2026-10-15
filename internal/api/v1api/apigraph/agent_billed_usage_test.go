package apigraph

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Billed amounts are added to server usage rows grouped by nothing, day or
// model, summed over the period; a row with no bill stays without one, and a
// grouping a bill is not kept for is left alone.
func TestServerUsageCarriesWhatWasBilled(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	firstDay := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if err := tx.PutAgentBilledDays("provider-one", []models.AgentBilledDay{
			{BilledOn: firstDay, Model: "chat-model", BilledAmount: 1.5, CurrencyCode: "usd"},
			{BilledOn: firstDay.AddDate(0, 0, 1), Model: "chat-model", BilledAmount: 2, CurrencyCode: "usd"},
			{BilledOn: firstDay, Model: "embed-model", BilledAmount: 0.25, CurrencyCode: "usd"},
		}); err != nil {
			test.Fatal(err)
		}
	})
	billedOf := func(row models.AgentUsageRow) float64 {
		test.Helper()
		if row.BilledAmount == nil {
			test.Fatalf("row %q has no billed amount", row.Key)
		}
		return *row.BilledAmount
	}
	since := firstDay
	until := firstDay.AddDate(0, 0, 3)
	ask := func(by string, rows []models.AgentUsageRow) []models.AgentUsageRow {
		test.Helper()
		arguments := AgentUsageArguments{Since: &since, Until: &until, By: &by}
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			if err := addBilledAmounts(tx, rows, arguments); err != nil {
				test.Fatal(err)
			}
		})
		return rows
	}

	total := ask("", []models.AgentUsageRow{{Key: ""}})
	if billedOf(total[0]) != 3.75 || total[0].BilledCurrencyCode != "usd" {
		test.Errorf("total %+v", total[0])
	}

	byDay := ask("day", []models.AgentUsageRow{{Key: "2026-03-01"}, {Key: "2026-03-02"}, {Key: "2026-03-03"}})
	if billedOf(byDay[0]) != 1.75 || billedOf(byDay[1]) != 2 || byDay[2].BilledAmount != nil {
		test.Errorf("by day %+v", byDay)
	}

	byModel := ask("model", []models.AgentUsageRow{
		{Key: "provider-one:chat-model"}, {Key: "provider-one:embed-model@256"}, {Key: "provider-one:embed-model@512"}, {Key: "provider-two:other-model"},
	})
	if billedOf(byModel[0]) != 3.5 || billedOf(byModel[1]) != 0.25 || byModel[2].BilledAmount != nil || byModel[3].BilledAmount != nil {
		test.Errorf("by model %+v", byModel)
	}

	byKind := ask("kind", []models.AgentUsageRow{{Key: "chat"}})
	if byKind[0].BilledAmount != nil {
		test.Errorf("a grouping with no bill kept got one: %+v", byKind[0])
	}
}
