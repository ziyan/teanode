package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func netWorthByDay(t *testing.T, tx db.Transaction, agentId, from, to string) map[string]map[string]string {
	t.Helper()
	points, err := tx.NetWorthSeries(agentId, from, to)
	if err != nil {
		t.Fatalf("NetWorthSeries: %s", err)
	}
	byDay := map[string]map[string]string{}
	for _, point := range points {
		if byDay[point.NetWorthOn] == nil {
			byDay[point.NetWorthOn] = map[string]string{}
		}
		byDay[point.NetWorthOn][point.CurrencyCode] = point.NetWorthAmount
	}
	return byDay
}

func recordValuation(t *testing.T, tx db.Transaction, valuation *models.AssetValuation) *models.AssetValuation {
	t.Helper()
	recorded, err := tx.RecordValuation(valuation)
	if err != nil {
		t.Fatalf("RecordValuation %+v: %s", valuation, err)
	}
	return recorded
}

// Values carry forward, a late correction changes the days after it until
// the next value, a closed asset stops counting after the day it closed,
// liabilities subtract, the person's value wins a day, and currencies are
// never added together.
func TestNetWorthSeriesSumsTheWinningValuations(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "net-worth")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		car, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "car", AssetKind: models.AssetKindVehicle, CurrencyCode: "USD"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		if car.ValuationSource != models.ValuationSourceManual || car.IsLiability {
			t.Errorf("a car is a manual asset that adds: %+v", car)
		}
		loan, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "car loan", AssetKind: models.AssetKindLoan,
			CurrencyCode: "USD", ValuationSource: models.ValuationSourceAgentReading})
		if err != nil || !loan.IsLiability {
			t.Fatalf("a loan subtracts: %v %+v", err, loan)
		}
		savings, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "savings abroad", AssetKind: models.AssetKindCash, CurrencyCode: "EUR"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		bike, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "bike", AssetKind: models.AssetKindVehicle, CurrencyCode: "USD"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}

		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: car.ID, ValuedOn: "2026-09-01", Value: "18000", ValuationSource: models.ValuationSourceManual})
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: car.ID, ValuedOn: "2026-09-05", Value: "16500", ValuationSource: models.ValuationSourceManual})
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: loan.ID, ValuedOn: "2026-09-02", Value: "4000", ValuationSource: models.ValuationSourceAgentReading})
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: savings.ID, ValuedOn: "2026-09-01", Value: "3000", ValuationSource: models.ValuationSourceManual})
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: bike.ID, ValuedOn: "2026-09-01", Value: "500", ValuationSource: models.ValuationSourceManual})
		if _, err := tx.CloseAsset(fixture.agentId, bike.ID, "2026-09-03"); err != nil {
			t.Fatalf("CloseAsset: %s", err)
		}

		byDay := netWorthByDay(t, tx, fixture.agentId, "2026-08-31", "2026-09-06")
		for day, expected := range map[string]map[string]string{
			"2026-09-01": {"USD": "18500.0000", "EUR": "3000.0000"},
			"2026-09-02": {"USD": "14500.0000", "EUR": "3000.0000"},
			"2026-09-03": {"USD": "14500.0000", "EUR": "3000.0000"},
			"2026-09-04": {"USD": "14000.0000", "EUR": "3000.0000"},
			"2026-09-06": {"USD": "12500.0000", "EUR": "3000.0000"},
		} {
			for currencyCode, amount := range expected {
				if byDay[day][currencyCode] != amount {
					t.Errorf("%s %s: got %q, want %q (all: %v)", day, currencyCode, byDay[day][currencyCode], amount, byDay[day])
				}
			}
			if len(byDay[day]) != len(expected) {
				t.Errorf("%s: currencies stay apart: %v", day, byDay[day])
			}
		}
		if len(byDay["2026-08-31"]) != 0 {
			t.Errorf("before any valuation there is nothing to sum: %v", byDay["2026-08-31"])
		}

		// A correction for a past day changes that day and those after it
		// until the next value.
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: car.ID, ValuedOn: "2026-09-03", Value: "17000", ValuationSource: models.ValuationSourceManual})
		// The same day from a lower source does not beat the person.
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: car.ID, ValuedOn: "2026-09-03", Value: "99999", ValuationSource: models.ValuationSourceAgentEstimate,
			EstimateLow: "90000", EstimateHigh: "110000", EvidenceURLs: []string{"https://example.com/listing"}})
		byDay = netWorthByDay(t, tx, fixture.agentId, "2026-09-02", "2026-09-05")
		for day, amount := range map[string]string{"2026-09-02": "14500.0000", "2026-09-03": "13500.0000", "2026-09-04": "13000.0000", "2026-09-05": "12500.0000"} {
			if byDay[day]["USD"] != amount {
				t.Errorf("after the correction, %s: got %q, want %q", day, byDay[day]["USD"], amount)
			}
		}

		valuations, err := tx.ListAssetValuations(fixture.agentId, car.ID)
		if err != nil || len(valuations) != 4 {
			t.Fatalf("ListAssetValuations: %v %d", err, len(valuations))
		}
		if valuations[0].ValuedOn != "2026-09-05" || valuations[1].ValuationSource != models.ValuationSourceManual || valuations[2].ValuationSource != models.ValuationSourceAgentEstimate {
			t.Errorf("newest first, the winner first within a day: %+v", valuations)
		}
		if len(valuations[2].EvidenceURLs) != 1 || valuations[2].EstimateHigh != "110000.0000" {
			t.Errorf("an estimate keeps its range and evidence: %+v", valuations[2])
		}

		assets, err := tx.ListAssets(fixture.agentId)
		if err != nil || len(assets) != 4 {
			t.Fatalf("ListAssets: %v %d", err, len(assets))
		}
		for _, asset := range assets {
			if asset.ID == car.ID && (asset.LatestValuation == nil || asset.LatestValuation.Value != "16500.0000") {
				t.Errorf("the car's latest valuation: %+v", asset.LatestValuation)
			}
		}
		if assets[len(assets)-1].ID != bike.ID {
			t.Errorf("closed assets come last: %+v", assets[len(assets)-1])
		}

		// Precedence between a finance sync and the person on one day.
		checking, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "checking", AssetKind: models.AssetKindCash,
			CurrencyCode: "JPY", ValuationSource: models.ValuationSourceFinanceSync})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: checking.ID, ValuedOn: "2026-09-06", Value: "50000", ValuationSource: models.ValuationSourceManual})
		recordValuation(t, tx, &models.AssetValuation{AgentID: fixture.agentId, AssetID: checking.ID, ValuedOn: "2026-09-06", Value: "70000", ValuationSource: models.ValuationSourceFinanceSync})
		if byDay := netWorthByDay(t, tx, fixture.agentId, "2026-09-06", "2026-09-06"); byDay["2026-09-06"]["JPY"] != "50000.0000" {
			t.Errorf("manual beats finance_sync on one day: %v", byDay)
		}

		if err := tx.DeleteValuation(fixture.agentId, valuations[0].ID); err != nil {
			t.Fatalf("DeleteValuation: %s", err)
		}
		if byDay := netWorthByDay(t, tx, fixture.agentId, "2026-09-05", "2026-09-05"); byDay["2026-09-05"]["USD"] != "13000.0000" {
			t.Errorf("deleting the latest valuation carries the one before forward: %v", byDay)
		}
		manualEvents, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourceAssetValuation)})
		if err != nil || len(manualEvents) == 0 {
			t.Errorf("manual valuations are audited: %v %d", err, len(manualEvents))
		}
		for _, event := range manualEvents {
			if event.ResourceID == "" {
				t.Errorf("an audited valuation names itself: %+v", event)
			}
		}
		if _, err := tx.NetWorthSeries(fixture.agentId, "2000-01-01", "2026-09-05"); !errors.Is(err, db.ErrTooMuchAsked) {
			t.Errorf("a range too long must be refused: %v", err)
		}
		if _, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "boat", AssetKind: "yacht", CurrencyCode: "USD"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown asset kind must be refused: %v", err)
		}
	})
}

// Deleting a finance source keeps its assets and their history, turned
// manual and closed on the day of the delete, so net worth stops counting
// them after it; an asset already closed keeps its day, and the finance
// account link goes with the source.
func TestNetWorthKeepsHistoryWhenAFinanceSourceIsDeleted(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "net-worth-detach")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets, err := tx.ListAssets(fixture.agentId)
		if err != nil || len(assets) != 2 {
			t.Fatalf("ListAssets: %v %d", err, len(assets))
		}
		closedByHand := assets[0]
		if _, err := tx.CloseAsset(fixture.agentId, closedByHand.ID, "2026-09-12"); err != nil {
			t.Fatalf("CloseAsset: %s", err)
		}
		detachedCount, err := tx.DetachAssetsOfSource(fixture.agentId, fixture.sourceId, "2026-09-14")
		if err != nil || detachedCount != 2 {
			t.Fatalf("DetachAssetsOfSource: %v %d", err, detachedCount)
		}
		if err := tx.DeleteAgentSource(fixture.agentId, fixture.sourceId); err != nil {
			t.Fatalf("DeleteAgentSource: %s", err)
		}
		assets, err = tx.ListAssets(fixture.agentId)
		if err != nil || len(assets) != 2 {
			t.Fatalf("the assets outlive the source: %v %d", err, len(assets))
		}
		for _, asset := range assets {
			if asset.ValuationSource != models.ValuationSourceManual || asset.FinanceAccountID != "" || asset.LatestValuation == nil {
				t.Errorf("a detached asset is manual, unlinked, with its history: %+v", asset)
			}
			expectedClosedOn := "2026-09-14"
			if asset.ID == closedByHand.ID {
				expectedClosedOn = "2026-09-12"
			}
			if asset.ClosedOn != expectedClosedOn {
				t.Errorf("%s closes on %s, got %q", asset.AssetName, expectedClosedOn, asset.ClosedOn)
			}
		}
		points, err := tx.NetWorthSeries(fixture.agentId, "2026-09-12", "2026-09-16")
		if err != nil {
			t.Fatalf("NetWorthSeries: %s", err)
		}
		countedDays := map[string]bool{}
		for _, point := range points {
			countedDays[point.NetWorthOn] = true
		}
		if !countedDays["2026-09-14"] || countedDays["2026-09-15"] || countedDays["2026-09-16"] {
			t.Errorf("net worth counts the assets through the day of the delete and not after: %v", countedDays)
		}
		if page, err := tx.ListFinanceTransactions(fixture.agentId, nil); err != nil || len(page.FinanceTransactions) != 0 {
			t.Errorf("the source's transactions go with it: %v", err)
		}
	})
}

// Linking an institution again after its source was deleted takes back the
// assets the old link made, rather than counting each account twice; an
// asset the person made by hand under the same name is not taken.
func TestRelinkingTakesBackTheDetachedAssets(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "net-worth-relink")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")

	var relinked financeFixture
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.DetachAssetsOfSource(fixture.agentId, fixture.sourceId); err != nil {
			t.Fatalf("DetachAssetsOfSource: %s", err)
		}
		if err := tx.DeleteAgentSource(fixture.agentId, fixture.sourceId); err != nil {
			t.Fatalf("DeleteAgentSource: %s", err)
		}
		if _, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "Everyday Checking", AssetKind: models.AssetKindCash,
			CurrencyCode: "USD", ValuationSource: models.ValuationSourceManual}); err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: fixture.agentId, Kind: models.SourceWeb, Name: "institution again",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.com/"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		relinked = financeFixture{agentId: fixture.agentId, sourceId: source.ID}
	})
	applyFinanceSync(t, database, relinked, sampleFinanceSync(), "2026-09-13")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets, err := tx.ListAssets(fixture.agentId)
		if err != nil {
			t.Fatal(err)
		}
		syncedCount, manualCount := 0, 0
		for _, asset := range assets {
			switch asset.ValuationSource {
			case models.ValuationSourceFinanceSync:
				syncedCount++
				if asset.FinanceAccountID == "" {
					t.Errorf("a synced asset with no account: %+v", asset)
				}
			case models.ValuationSourceManual:
				manualCount++
			}
		}
		if len(assets) != 3 || syncedCount != 2 || manualCount != 1 {
			t.Errorf("%d assets, %d synced and %d by hand; want the two taken back and the one made by hand", len(assets), syncedCount, manualCount)
		}
	})
}
