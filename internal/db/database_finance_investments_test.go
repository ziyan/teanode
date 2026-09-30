package db_test

import (
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A brokerage account worth 2150.25, of which 1824.58 is a fund and the
// rest cash, with a buy of the fund; holdings passed in so a test can
// change them.
func brokerageSync(balance string, holdings []finance.Holding) *finance.SyncResult {
	return &finance.SyncResult{
		Accounts: []finance.Account{{
			ProviderAccountID: "account-brokerage", AccountName: "Individual", AccountKind: finance.AccountKindInvestment,
			CurrencyCode: "USD", CurrentBalance: balance, ProviderMetadata: json.RawMessage(`{"subtype":"brokerage"}`),
		}},
		Securities: []finance.Security{{
			ProviderSecurityID: "security-fund", TickerSymbol: "EXIF", SecurityName: "Example Index Fund",
			SecurityKind: finance.SecurityKindMutualFund, CurrencyCode: "USD", ClosePrice: "150.5", ClosePriceOn: "2026-09-29",
		}},
		Holdings:               holdings,
		HoldingsReadAccountIDs: []string{"account-brokerage"},
		Trades: []finance.Trade{{
			ProviderTradeID: "trade-buy", ProviderAccountID: "account-brokerage", ProviderSecurityID: "security-fund",
			TradedOn: "2026-09-02", TradeKind: finance.TradeKindBuy, TradeSubkind: "buy", TradedQuantity: "2", UnitPrice: "150.5",
			TradeAmount: "-301", FeeAmount: "0", CurrencyCode: "USD", Description: "BUY EXIF",
		}},
	}
}

func fundHolding(heldQuantity, holdingValue string) []finance.Holding {
	return []finance.Holding{{
		ProviderAccountID: "account-brokerage", ProviderSecurityID: "security-fund", HeldQuantity: heldQuantity,
		UnitPrice: "150.5", HoldingValue: holdingValue, CostBasis: "1500", CurrencyCode: "USD",
	}}
}

// assetsByName is the agent's assets by name.
func assetsByName(t *testing.T, tx db.Transaction, agentId string) map[string]*models.Asset {
	t.Helper()
	assets, err := tx.ListAssets(agentId)
	if err != nil {
		t.Fatalf("ListAssets: %s", err)
	}
	found := map[string]*models.Asset{}
	for _, asset := range assets {
		found[asset.AssetName] = asset
	}
	return found
}

// netWorthOn is the net worth in dollars on one day.
func netWorthOn(t *testing.T, tx db.Transaction, agentId, day string) string {
	t.Helper()
	points, err := tx.NetWorthSeries(agentId, day, day)
	if err != nil {
		t.Fatalf("NetWorthSeries: %s", err)
	}
	for _, point := range points {
		if point.CurrencyCode == "USD" {
			return point.NetWorthAmount
		}
	}
	return ""
}

// A holding is an asset valued with its quantity, price and cost basis,
// the account's own asset holds only the cash, and net worth is the
// account's balance, counted once.
func TestFinanceHoldingsAreAssetsAndTheAccountHoldsTheCash(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-holdings")
	applied := applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12.123456789", "1824.58")), "2026-09-12")
	if len(applied.CreatedAssetIDs) != 2 || applied.WrittenTradeCount != 1 {
		t.Fatalf("applied %+v, want the account's asset, the fund's, and the buy", applied)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets := assetsByName(t, tx, fixture.agentId)
		account, holding := assets["Individual"], assets["EXIF (Individual)"]
		if account == nil || holding == nil {
			t.Fatalf("assets %v, want the account and the fund", assets)
		}
		if account.LatestValuation == nil || account.LatestValuation.Value != "325.6700" {
			t.Errorf("the account's own asset is its cash: %+v", account.LatestValuation)
		}
		if holding.AssetKind != models.AssetKindInvestment || holding.FinanceSecurity == nil || holding.FinanceSecurity.TickerSymbol != "EXIF" ||
			holding.FinanceSecurity.SecurityKind != models.SecurityKindMutualFund {
			t.Errorf("holding %+v", holding)
		}
		valuation := holding.LatestValuation
		if valuation == nil || valuation.Value != "1824.5800" || valuation.HeldQuantity != "12.12345679" || valuation.UnitPrice != "150.50000000" ||
			valuation.CostBasis != "1500.0000" {
			t.Errorf("holding valuation %+v", valuation)
		}
		if worth := netWorthOn(t, tx, fixture.agentId, "2026-09-12"); worth != "2150.2500" {
			t.Errorf("net worth %s, want the account's balance once", worth)
		}
	})
}

// A trade is stored once however often it is synced, is read with its
// security, and never appears in spending.
func TestFinanceTradesAreNotSpending(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-trades")
	applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12", "1806")), "2026-09-12")
	again := applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12", "1806")), "2026-09-12")
	if again.WrittenTradeCount != 0 {
		t.Errorf("a trade sent again unchanged was written again: %+v", again)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTrades(fixture.agentId, nil)
		if err != nil || len(page.FinanceTrades) != 1 {
			t.Fatalf("ListFinanceTrades: %v %+v", err, page)
		}
		trade := page.FinanceTrades[0]
		if trade.TradeKind != models.TradeKindBuy || trade.TradeAmount != "-301.0000" || trade.TradedQuantity != "2.00000000" ||
			trade.FinanceSecurity == nil || trade.FinanceSecurity.TickerSymbol != "EXIF" {
			t.Errorf("trade %+v", trade)
		}
		byAccount, err := tx.ListFinanceTrades(fixture.agentId, &db.FinanceTradeFilter{FinanceSecurityID: trade.FinanceSecurityID, From: "2026-09-03"})
		if err != nil || len(byAccount.FinanceTrades) != 0 {
			t.Errorf("a trade before the day asked for came back: %v %+v", err, byAccount)
		}
		rows, err := tx.FinanceSpendingSummary(fixture.agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByMonth})
		if err != nil || len(rows) != 0 {
			t.Errorf("spending from a trade: %v %+v", err, rows)
		}
	})
}

// A holding no longer reported is worth nothing from that day and closed,
// so net worth is the cash its sale left; bought again, it opens again.
func TestFinanceHoldingSoldAndBoughtAgain(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-holding-sold")
	applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12", "1824.58")), "2026-09-12")
	applyFinanceSync(t, database, fixture, brokerageSync("2160.00", nil), "2026-09-13")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		holding := assetsByName(t, tx, fixture.agentId)["EXIF (Individual)"]
		if holding == nil || holding.ClosedOn != "2026-09-13" || holding.LatestValuation == nil || holding.LatestValuation.Value != "0.0000" {
			t.Fatalf("a sold holding is worth nothing and closed: %+v", holding)
		}
		if worth := netWorthOn(t, tx, fixture.agentId, "2026-09-13"); worth != "2160.0000" {
			t.Errorf("net worth %s the day of the sale, want the cash alone", worth)
		}
	})

	applyFinanceSync(t, database, fixture, brokerageSync("2160.00", fundHolding("1", "150.50")), "2026-09-20")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		holding := assetsByName(t, tx, fixture.agentId)["EXIF (Individual)"]
		if holding == nil || holding.ClosedOn != "" || holding.LatestValuation == nil || holding.LatestValuation.Value != "150.5000" {
			t.Fatalf("a holding bought again is open again: %+v", holding)
		}
		if worth := netWorthOn(t, tx, fixture.agentId, "2026-09-16"); worth != "2160.0000" {
			t.Errorf("net worth %s between the sale and the purchase, want the cash alone", worth)
		}
		if worth := netWorthOn(t, tx, fixture.agentId, "2026-09-20"); worth != "2160.0000" {
			t.Errorf("net worth %s after buying again, want the balance once", worth)
		}
	})
}

// Holdings not read on a sync leave that day unvalued for the account,
// rather than counting the carried-forward holdings twice.
func TestFinanceHoldingsNotReadRecordNothingForTheAccount(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-holdings-unread")
	applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12", "1824.58")), "2026-09-12")
	unread := brokerageSync("2200.00", nil)
	unread.HoldingsReadAccountIDs = nil
	applyFinanceSync(t, database, fixture, unread, "2026-09-13")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets := assetsByName(t, tx, fixture.agentId)
		if account := assets["Individual"]; account.LatestValuation == nil || account.LatestValuation.ValuedOn != "2026-09-12" {
			t.Errorf("the account was valued on a day its holdings were not read: %+v", account.LatestValuation)
		}
		if holding := assets["EXIF (Individual)"]; holding.ClosedOn != "" {
			t.Errorf("a holding not read was closed: %+v", holding)
		}
		if worth := netWorthOn(t, tx, fixture.agentId, "2026-09-13"); worth != "2150.2500" {
			t.Errorf("net worth %s, want yesterday's carried forward once", worth)
		}
	})
}

// Deleting the finance source closes its holdings with their history, and
// linking the account again takes each back by its security.
func TestFinanceHoldingTakenBackAfterRelinking(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-holding-relink")
	applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12", "1824.58")), "2026-09-12")

	var relinked financeFixture
	var accountAssetId, holdingId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets := assetsByName(t, tx, fixture.agentId)
		accountAssetId, holdingId = assets["Individual"].ID, assets["EXIF (Individual)"].ID
		if _, err := tx.DetachAssetsOfSource(fixture.agentId, fixture.sourceId, "2026-09-12"); err != nil {
			t.Fatalf("DetachAssetsOfSource: %s", err)
		}
		if err := tx.DeleteAgentSource(fixture.agentId, fixture.sourceId); err != nil {
			t.Fatalf("DeleteAgentSource: %s", err)
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
	applyFinanceSync(t, database, relinked, brokerageSync("2150.25", fundHolding("12", "1830.00")), "2026-09-14")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets := assetsByName(t, tx, fixture.agentId)
		if len(assets) != 2 || assets["Individual"].ID != accountAssetId {
			t.Errorf("assets %+v, want the account's own asset and the holding, both taken back", assets)
		}
		holding := assets["EXIF (Individual)"]
		if holding == nil || holding.ID != holdingId || holding.ClosedOn != "" || holding.ValuationSource != models.ValuationSourceFinanceSync ||
			holding.LatestValuation.Value != "1830.0000" {
			t.Errorf("the holding taken back: %+v", holding)
		}
	})
}

// An account Plaid calls a retirement account, and its holdings, are
// retirement assets.
func TestFinanceRetirementAccountHoldingsAreRetirement(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-retirement")
	sync := brokerageSync("2150.25", fundHolding("12", "1824.58"))
	sync.Accounts[0].AccountName = "Workplace Plan"
	sync.Accounts[0].ProviderMetadata = json.RawMessage(`{"subtype":"401k"}`)
	applyFinanceSync(t, database, fixture, sync, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		assets := assetsByName(t, tx, fixture.agentId)
		if assets["Workplace Plan"].AssetKind != models.AssetKindRetirement || assets["EXIF (Workplace Plan)"].AssetKind != models.AssetKindRetirement {
			t.Errorf("assets %+v, want both retirement", assets)
		}
	})
}
