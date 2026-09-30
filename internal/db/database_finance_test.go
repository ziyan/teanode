package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// financeFixture is one person with an agent and one source their finance
// rows hang from.
type financeFixture struct {
	agentId  string
	sourceId string
}

func createFinanceFixture(t *testing.T, database db.Database, username string) financeFixture {
	t.Helper()
	var fixture financeFixture
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: username})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agent.ID, Kind: models.SourceWeb, Name: "institution",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.com/"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		fixture = financeFixture{agentId: agent.ID, sourceId: source.ID}
	})
	return fixture
}

// A checking account, a card whose balance the institution reports as a
// negative amount owed, and three transactions on them.
func sampleFinanceSync() *finance.SyncResult {
	return &finance.SyncResult{
		Accounts: []finance.Account{
			{
				ProviderAccountID: "account-checking", AccountName: "Everyday Checking", AccountMask: "0001",
				AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "1200.50",
				AvailableBalance: "1100.50", ProviderMetadata: json.RawMessage(`{"id":"account-checking","unmapped":"kept"}`),
			},
			{
				ProviderAccountID: "account-card", AccountName: "Travel Card", AccountKind: finance.AccountKindCredit,
				CurrencyCode: "USD", CurrentBalance: "-340.25",
			},
		},
		Added: []finance.Transaction{
			{
				ProviderTransactionID: "transaction-grocer", ProviderAccountID: "account-checking", PostedOn: "2026-09-10",
				Amount: "-42.17", CurrencyCode: "USD", Description: "CORNER GROCER 0412", MerchantName: "Corner Grocer",
				ProviderCategoryPrimary: "FOOD_AND_DRINK", ProviderMetadata: json.RawMessage(`{"payee":"Corner Grocer"}`),
			},
			{
				ProviderTransactionID: "transaction-salary", ProviderAccountID: "account-checking", PostedOn: "2026-09-01",
				Amount: "2500", CurrencyCode: "USD", Description: "PAYROLL EXAMPLE CO",
			},
			{
				ProviderTransactionID: "transaction-diner", ProviderAccountID: "account-card", PostedOn: "2026-09-12",
				Amount: "-18.40", CurrencyCode: "USD", Description: "LAKESIDE DINER", MerchantName: "Lakeside Diner",
			},
		},
	}
}

func applyFinanceSync(t *testing.T, database db.Database, fixture financeFixture, result *finance.SyncResult, syncedOn string) *db.FinanceSyncApplied {
	t.Helper()
	var applied *db.FinanceSyncApplied
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		applied, err = tx.ApplyFinanceSync(fixture.agentId, fixture.sourceId, result, syncedOn)
		if err != nil {
			t.Fatalf("ApplyFinanceSync: %s", err)
		}
	})
	return applied
}

func financeTransactionsByProviderId(t *testing.T, tx db.Transaction, agentId string) map[string]*models.FinanceTransaction {
	t.Helper()
	page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{Limit: db.FinanceTransactionLimitMost})
	if err != nil {
		t.Fatalf("ListFinanceTransactions: %s", err)
	}
	found := map[string]*models.FinanceTransaction{}
	for _, transaction := range page.FinanceTransactions {
		found[transaction.ProviderTransactionID] = transaction
	}
	return found
}

// Running the same sync twice writes nothing the second time; each finance
// account gets one asset, and a day gets one valuation per asset however
// often it syncs, the latest balance winning.
func TestApplyFinanceSyncIsIdempotent(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-idempotent")

	first := applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	if len(first.InsertedFinanceAccountIDs) != 2 || len(first.CreatedAssetIDs) != 2 || first.WrittenTransactionCount != 3 ||
		len(first.FinanceTransactionIDsToCategorize) != 3 || first.RecordedValuationCount != 2 {
		t.Fatalf("first sync: %+v", first)
	}
	second := applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	if len(second.InsertedFinanceAccountIDs) != 0 || len(second.CreatedAssetIDs) != 0 || second.WrittenTransactionCount != 0 ||
		len(second.FinanceTransactionIDsToCategorize) != 0 {
		t.Fatalf("the same sync again must write nothing new: %+v", second)
	}
	changed := sampleFinanceSync()
	changed.Accounts[0].CurrentBalance = "1150.00"
	applyFinanceSync(t, database, fixture, changed, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		accounts, err := tx.ListFinanceAccounts(fixture.agentId, fixture.sourceId)
		if err != nil || len(accounts) != 2 {
			t.Fatalf("ListFinanceAccounts: %v %d", err, len(accounts))
		}
		if accounts[0].CurrentBalance != "1150.0000" || accounts[0].AvailableBalance != "1100.5000" {
			t.Errorf("the balance is the latest sync's: %+v", accounts[0])
		}
		var metadata map[string]string
		if err := json.Unmarshal(accounts[0].ProviderMetadata, &metadata); err != nil || metadata["unmapped"] != "kept" {
			t.Errorf("the provider metadata must survive whole: %s", accounts[0].ProviderMetadata)
		}
		if found := financeTransactionsByProviderId(t, tx, fixture.agentId); len(found) != 3 || found["transaction-grocer"].Amount != "-42.1700" {
			t.Fatalf("three transactions, amounts as numeric(19,4): %+v", found)
		}

		assets, err := tx.ListAssets(fixture.agentId)
		if err != nil || len(assets) != 2 {
			t.Fatalf("ListAssets: %v %d", err, len(assets))
		}
		for _, asset := range assets {
			if asset.ValuationSource != models.ValuationSourceFinanceSync || asset.FinanceAccountID == "" {
				t.Errorf("a finance account's asset is valued by the sync: %+v", asset)
			}
			valuations, err := tx.ListAssetValuations(fixture.agentId, asset.ID)
			if err != nil || len(valuations) != 1 {
				t.Fatalf("one valuation per day, got %v %d", err, len(valuations))
			}
			switch asset.AssetKind {
			case models.AssetKindCash:
				if asset.IsLiability || valuations[0].Value != "1150.0000" || valuations[0].ValuedOn != "2026-09-12" {
					t.Errorf("checking: %+v %+v", asset, valuations[0])
				}
			case models.AssetKindCreditCard:
				if !asset.IsLiability || valuations[0].Value != "340.2500" {
					t.Errorf("a card owes the balance without its sign: %+v %+v", asset, valuations[0])
				}
			default:
				t.Errorf("unexpected asset kind %s", asset.AssetKind)
			}
		}
	})
}

// A modify from the provider keeps what the person chose, and drops a
// spending category anything else gave when what it was judged from
// changed.
func TestApplyFinanceSyncKeepsWhatThePersonChose(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-person")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		dining, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "dining"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		groceries, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "groceries"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if isSet, err := tx.SetTransactionCategorization(fixture.agentId, found["transaction-grocer"].ID, dining.ID, models.CategorizedByPerson, nil); err != nil || !isSet {
			t.Fatalf("the person categorizes: %v %v", isSet, err)
		}
		if isSet, err := tx.SetTransactionCategorization(fixture.agentId, found["transaction-grocer"].ID, groceries.ID, models.CategorizedByCategorizeModel, nil); err != nil || isSet {
			t.Fatalf("the model must not overwrite the person: %v %v", isSet, err)
		}
		confidence := "0.8125"
		if isSet, err := tx.SetTransactionCategorization(fixture.agentId, found["transaction-diner"].ID, dining.ID, models.CategorizedByCategorizeModel, &confidence); err != nil || !isSet {
			t.Fatalf("the model categorizes: %v %v", isSet, err)
		}
		if isMarked, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["transaction-salary"].ID, true, models.TransferMarkedByPerson); err != nil || !isMarked {
			t.Fatalf("the person marks a transfer: %v %v", isMarked, err)
		}
		if isMarked, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["transaction-salary"].ID, false, models.TransferMarkedByDetection); err != nil || isMarked {
			t.Fatalf("anything else must not unmark the person's transfer: %v %v", isMarked, err)
		}
	})

	modified := sampleFinanceSync()
	modified.Added[0].Description = "CORNER GROCER 0412 POSTED"
	modified.Added[1].Amount = "2600"
	modified.Added[2].MerchantName = "Lakeside Diner and Bar"
	applied := applyFinanceSync(t, database, fixture, modified, "2026-09-13")
	if applied.WrittenTransactionCount != 3 || len(applied.FinanceTransactionIDsToCategorize) != 1 {
		t.Fatalf("three changed, one to categorize again: %+v", applied)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		grocer, salary, diner := found["transaction-grocer"], found["transaction-salary"], found["transaction-diner"]
		if grocer.CategorizedBy != models.CategorizedByPerson || grocer.SpendingCategoryID == "" || grocer.Description != "CORNER GROCER 0412 POSTED" {
			t.Errorf("the person's spending category survives a modify: %+v", grocer)
		}
		if !salary.IsTransfer || salary.TransferMarkedBy != models.TransferMarkedByPerson || salary.Amount != "2600.0000" {
			t.Errorf("the person's transfer survives a modify: %+v", salary)
		}
		if diner.SpendingCategoryID != "" || diner.CategorizedBy != "" || diner.CategorizationConfidence != "" {
			t.Errorf("the model's spending category is dropped when the merchant changed: %+v", diner)
		}
		if applied.FinanceTransactionIDsToCategorize[0] != diner.ID {
			t.Errorf("the diner is the one to categorize again: %v", applied.FinanceTransactionIDsToCategorize)
		}
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(fixture.agentId, 10)
		if err != nil || len(uncategorized) != 1 || uncategorized[0].ID != diner.ID {
			t.Errorf("uncategorized leaves out the person's and transfers: %v %+v", err, uncategorized)
		}
	})
}

// A SimpleFIN-style sync that says pending transactions from a day on are
// replaced deletes the ones in that window it did not send again, and
// keeps the ones before it; a removal deletes its row.
func TestApplyFinanceSyncReplacesPending(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-pending")
	first := sampleFinanceSync()
	first.Added = append(first.Added,
		finance.Transaction{ProviderTransactionID: "pending-old", ProviderAccountID: "account-checking", PostedOn: "2026-09-02",
			Amount: "-5.00", CurrencyCode: "USD", Description: "PARKING", IsPending: true},
		finance.Transaction{ProviderTransactionID: "pending-new", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "-9.99", CurrencyCode: "USD", Description: "STREAMING", IsPending: true},
	)
	applyFinanceSync(t, database, fixture, first, "2026-09-12")

	replacedFrom := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	second := &finance.SyncResult{
		Accounts: first.Accounts,
		Added: []finance.Transaction{{ProviderTransactionID: "posted-streaming", ProviderAccountID: "account-checking",
			PostedOn: "2026-09-12", Amount: "-9.99", CurrencyCode: "USD", Description: "STREAMING"}},
		RemovedProviderTransactionIDs: []string{"transaction-diner"},
		PendingReplacedFrom:           &replacedFrom,
	}
	applied := applyFinanceSync(t, database, fixture, second, "2026-09-13")
	if applied.ReplacedPendingTransactionCount != 1 || applied.RemovedTransactionCount != 1 {
		t.Fatalf("one pending replaced, one removed: %+v", applied)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if found["pending-new"] != nil || found["transaction-diner"] != nil {
			t.Errorf("the replaced pending and the removed transaction must be gone: %+v", found)
		}
		if found["pending-old"] == nil || found["posted-streaming"] == nil || found["transaction-grocer"] == nil {
			t.Errorf("a pending row before the window and posted rows survive: %+v", found)
		}
	})
}

// A card payment out of checking and its credit on the card two days
// later are both transfers; the same amount ten days apart is not, a
// transaction the person decided about pairs with nothing, and a provider
// transfer category is a transfer on its own.
func TestDetectFinanceTransfersPairsAccounts(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-transfer")
	result := sampleFinanceSync()
	result.Added = []finance.Transaction{
		{ProviderTransactionID: "payment-out", ProviderAccountID: "account-checking", PostedOn: "2026-09-10", Amount: "-500", CurrencyCode: "USD", Description: "CARD PAYMENT"},
		{ProviderTransactionID: "payment-in", ProviderAccountID: "account-card", PostedOn: "2026-09-12", Amount: "500", CurrencyCode: "USD", Description: "PAYMENT THANK YOU"},
		{ProviderTransactionID: "far-out", ProviderAccountID: "account-checking", PostedOn: "2026-09-01", Amount: "-75", CurrencyCode: "USD", Description: "HARDWARE STORE"},
		{ProviderTransactionID: "far-in", ProviderAccountID: "account-card", PostedOn: "2026-09-11", Amount: "75", CurrencyCode: "USD", Description: "REFUND"},
		{ProviderTransactionID: "person-out", ProviderAccountID: "account-checking", PostedOn: "2026-09-05", Amount: "-60", CurrencyCode: "USD", Description: "GIFT"},
		{ProviderTransactionID: "person-in", ProviderAccountID: "account-card", PostedOn: "2026-09-06", Amount: "60", CurrencyCode: "USD", Description: "RETURN"},
		{ProviderTransactionID: "other-currency", ProviderAccountID: "account-card", PostedOn: "2026-09-10", Amount: "500", CurrencyCode: "EUR", Description: "FOREIGN CREDIT"},
		{ProviderTransactionID: "provider-transfer", ProviderAccountID: "account-checking", PostedOn: "2026-09-09", Amount: "-20", CurrencyCode: "USD",
			Description: "TO SAVINGS", ProviderCategoryPrimary: "TRANSFER_OUT"},
	}
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["person-out"].ID, false, models.TransferMarkedByPerson); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
		markedCount, err := tx.DetectFinanceTransfers(fixture.agentId, fixture.sourceId, "2026-09-01")
		if err != nil {
			t.Fatalf("DetectFinanceTransfers: %s", err)
		}
		if markedCount != 3 {
			t.Errorf("the payment pair and the provider transfer, got %d", markedCount)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		for providerTransactionId, isTransfer := range map[string]bool{
			"payment-out": true, "payment-in": true, "far-out": false, "far-in": false,
			"person-out": false, "person-in": false, "other-currency": false, "provider-transfer": true,
		} {
			if found[providerTransactionId].IsTransfer != isTransfer {
				t.Errorf("%s: is transfer %v, want %v", providerTransactionId, found[providerTransactionId].IsTransfer, isTransfer)
			}
		}
		if again, err := tx.DetectFinanceTransfers(fixture.agentId, "", "2026-09-01"); err != nil || again != 0 {
			t.Errorf("running it again marks nothing more: %v %d", err, again)
		}
	})
}

func TestListFinanceTransactionsFiltersAndPages(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-paging")
	result := sampleFinanceSync()
	for index, day := range []string{"2026-09-03", "2026-09-03", "2026-09-05"} {
		result.Added = append(result.Added, finance.Transaction{
			ProviderTransactionID: fmt.Sprintf("coffee-%d", index), ProviderAccountID: "account-card",
			PostedOn: day, Amount: "-4.50", CurrencyCode: "USD", Description: "HARBOR COFFEE", ProviderCategoryDetailed: "mcc:5814",
		})
	}
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		seen := map[string]bool{}
		previousPostedOn := "9999-12-31"
		cursor := ""
		pageCount := 0
		for {
			page, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{Limit: 2, After: cursor})
			if err != nil {
				t.Fatalf("ListFinanceTransactions: %s", err)
			}
			pageCount++
			for _, transaction := range page.FinanceTransactions {
				if seen[transaction.ID] || transaction.PostedOn > previousPostedOn {
					t.Fatalf("pages must go newest first with no repeats: %+v", transaction)
				}
				seen[transaction.ID] = true
				previousPostedOn = transaction.PostedOn
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if len(seen) != 6 || pageCount != 3 {
			t.Errorf("six transactions over three pages, got %d over %d", len(seen), pageCount)
		}

		for description, testCase := range map[string]struct {
			filter        db.FinanceTransactionFilter
			expectedCount int
		}{
			"text in the merchant":    {filter: db.FinanceTransactionFilter{Text: "grocer"}, expectedCount: 1},
			"text in the description": {filter: db.FinanceTransactionFilter{Text: "harbor"}, expectedCount: 3},
			"money in":                {filter: db.FinanceTransactionFilter{MinimumAmount: "0"}, expectedCount: 1},
			"between":                 {filter: db.FinanceTransactionFilter{MinimumAmount: "-20", MaximumAmount: "-10"}, expectedCount: 1},
			"range of days":           {filter: db.FinanceTransactionFilter{From: "2026-09-03", To: "2026-09-05"}, expectedCount: 3},
			"provider category":       {filter: db.FinanceTransactionFilter{ProviderCategory: "mcc:5814"}, expectedCount: 3},
			"uncategorized":           {filter: db.FinanceTransactionFilter{IsUncategorized: true}, expectedCount: 6},
			"one finance account":     {filter: db.FinanceTransactionFilter{FinanceAccountID: "not-an-account"}, expectedCount: 0},
			"a pattern is literal":    {filter: db.FinanceTransactionFilter{Text: "%"}, expectedCount: 0},
			"a limit beyond 200":      {filter: db.FinanceTransactionFilter{Limit: 5000}, expectedCount: 6},
			"a search with nothing":   {filter: db.FinanceTransactionFilter{Text: "no such merchant"}, expectedCount: 0},
		} {
			filter := testCase.filter
			page, err := tx.ListFinanceTransactions(fixture.agentId, &filter)
			if err != nil {
				t.Fatalf("%s: %s", description, err)
			}
			if len(page.FinanceTransactions) != testCase.expectedCount {
				t.Errorf("%s: got %d, want %d", description, len(page.FinanceTransactions), testCase.expectedCount)
			}
		}
		if _, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{After: "not a cursor"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a cursor that is not one must be refused, got %v", err)
		}
	})
}

// Transfers are neither spending nor income, and the summary groups by
// what it is asked to.
func TestFinanceSpendingSummaryLeavesOutTransfers(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-summary")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "grocer-refund", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "7.00", CurrencyCode: "USD", Description: "CORNER GROCER REFUND", MerchantName: "Corner Grocer"},
		finance.Transaction{ProviderTransactionID: "to-card", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "-300", CurrencyCode: "USD", Description: "CARD PAYMENT"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["to-card"].ID, true, models.TransferMarkedByPerson); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
		summary, err := tx.FinanceSpendingSummary(fixture.agentId, &db.FinanceSpendingSummaryFilter{
			From: "2026-09-01", To: "2026-09-30", GroupBy: models.FinanceSpendingSummaryGroupByMerchant,
		})
		if err != nil {
			t.Fatalf("FinanceSpendingSummary: %s", err)
		}
		byMerchant := map[string]*models.FinanceSpendingSummaryRow{}
		for _, row := range summary {
			byMerchant[row.GroupKey] = row
		}
		if byMerchant["CARD PAYMENT"] != nil {
			t.Errorf("a transfer is left out: %+v", byMerchant["CARD PAYMENT"])
		}
		grocer := byMerchant["Corner Grocer"]
		if grocer == nil || grocer.MoneyOut != "42.1700" || grocer.MoneyIn != "7.0000" || grocer.FinanceTransactionCount != 2 {
			t.Errorf("the grocer's money out and refund: %+v", grocer)
		}
		if salary := byMerchant["PAYROLL EXAMPLE CO"]; salary == nil || salary.MoneyOut != "0.0000" || salary.MoneyIn != "2500.0000" {
			t.Errorf("income is money in: %+v", salary)
		}

		byMonth, err := tx.FinanceSpendingSummary(fixture.agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByMonth})
		if err != nil || len(byMonth) != 1 || byMonth[0].GroupKey != "2026-09" || byMonth[0].MoneyOut != "60.5700" {
			t.Errorf("one month, transfers left out: %v %+v", err, byMonth)
		}
		byAccount, err := tx.FinanceSpendingSummary(fixture.agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByFinanceAccount})
		if err != nil || len(byAccount) != 2 || byAccount[0].GroupLabel != "Everyday Checking" {
			t.Errorf("by finance account, named: %v %+v", err, byAccount)
		}
		if _, err := tx.FinanceSpendingSummary(fixture.agentId, &db.FinanceSpendingSummaryFilter{GroupBy: "weekday"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown grouping must be refused, got %v", err)
		}
	})
}

// Another person's agent id never reads or changes a row, whatever id it
// is given.
func TestFinanceRowsAreTheirOwnersOnly(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	owner := createFinanceFixture(t, database, "finance-owner")
	stranger := createFinanceFixture(t, database, "finance-stranger")
	applyFinanceSync(t, database, owner, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		accounts, err := tx.ListFinanceAccounts(owner.agentId, "")
		if err != nil || len(accounts) != 2 {
			t.Fatalf("ListFinanceAccounts: %v %d", err, len(accounts))
		}
		found := financeTransactionsByProviderId(t, tx, owner.agentId)
		transactionId := found["transaction-grocer"].ID
		assets, err := tx.ListAssets(owner.agentId)
		if err != nil || len(assets) != 2 {
			t.Fatalf("ListAssets: %v %d", err, len(assets))
		}
		category, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: owner.agentId, SpendingCategoryName: "dining"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		strangerCategory, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: stranger.agentId, SpendingCategoryName: "dining"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory for the stranger: %s", err)
		}

		if account, err := tx.GetFinanceAccount(stranger.agentId, accounts[0].ID); err != nil || account != nil {
			t.Errorf("GetFinanceAccount across agents: %v %+v", err, account)
		}
		if transaction, err := tx.GetFinanceTransaction(stranger.agentId, transactionId); err != nil || transaction != nil {
			t.Errorf("GetFinanceTransaction across agents: %v %+v", err, transaction)
		}
		if listed, err := tx.ListFinanceAccounts(stranger.agentId, owner.sourceId); err != nil || len(listed) != 0 {
			t.Errorf("ListFinanceAccounts across agents: %v %d", err, len(listed))
		}
		if page, err := tx.ListFinanceTransactions(stranger.agentId, &db.FinanceTransactionFilter{FinanceAccountID: accounts[0].ID}); err != nil || len(page.FinanceTransactions) != 0 {
			t.Errorf("ListFinanceTransactions across agents: %v", err)
		}
		if _, err := tx.SetTransactionCategorization(stranger.agentId, transactionId, strangerCategory.ID, models.CategorizedByPerson, nil); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("SetTransactionCategorization across agents: %v", err)
		}
		if _, err := tx.SetTransactionCategorization(owner.agentId, transactionId, strangerCategory.ID, models.CategorizedByPerson, nil); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("another agent's spending category must not be assigned: %v", err)
		}
		if _, err := tx.MarkFinanceTransactionTransfer(stranger.agentId, transactionId, true, models.TransferMarkedByPerson); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("MarkFinanceTransactionTransfer across agents: %v", err)
		}
		if _, err := tx.ApplyFinanceSync(stranger.agentId, owner.sourceId, sampleFinanceSync(), "2026-09-12"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("ApplyFinanceSync into another agent's source: %v", err)
		}
		if listed, err := tx.ListAssets(stranger.agentId); err != nil || len(listed) != 0 {
			t.Errorf("ListAssets across agents: %v %d", err, len(listed))
		}
		if _, err := tx.UpdateAsset(stranger.agentId, assets[0].ID, func(asset *models.Asset) error { asset.AssetName = "taken"; return nil }); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("UpdateAsset across agents: %v", err)
		}
		if err := tx.DeleteAsset(stranger.agentId, assets[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("DeleteAsset across agents: %v", err)
		}
		if _, err := tx.RecordValuation(&models.AssetValuation{AgentID: stranger.agentId, AssetID: assets[0].ID, ValuedOn: "2026-09-12",
			Value: "1", ValuationSource: models.ValuationSourceManual}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("RecordValuation across agents: %v", err)
		}
		if valuations, err := tx.ListAssetValuations(stranger.agentId, assets[0].ID); err != nil || len(valuations) != 0 {
			t.Errorf("ListAssetValuations across agents: %v %d", err, len(valuations))
		}
		if detached, err := tx.DetachAssetsOfSource(stranger.agentId, owner.sourceId); err != nil || detached != 0 {
			t.Errorf("DetachAssetsOfSource across agents: %v %d", err, detached)
		}
		if err := tx.DeleteSpendingCategory(stranger.agentId, category.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("DeleteSpendingCategory across agents: %v", err)
		}
		if _, err := tx.SetBudget(&models.Budget{AgentID: stranger.agentId, SpendingCategoryID: category.ID, MonthlyAmount: "100",
			CurrencyCode: "USD", EffectiveFrom: "2026-09"}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("SetBudget on another agent's spending category: %v", err)
		}
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: stranger.agentId, MatchText: "grocer", SpendingCategoryID: category.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("CreateSpendingRule with another agent's spending category: %v", err)
		}
		if _, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: stranger.agentId, SavingsTargetName: "car", TargetAmount: "1000",
			CurrencyCode: "USD", TargetOn: "2027-01-01", TargetMeasure: models.TargetMeasureAssetValue, StartedOn: "2026-09-01",
			AssetIDs: []string{assets[0].ID}}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("CreateSavingsTarget with another agent's asset: %v", err)
		}
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: stranger.agentId, MatchText: "grocer", IsTransfer: true}); err != nil {
			t.Fatalf("the stranger's own rule: %s", err)
		}
		if found := financeTransactionsByProviderId(t, tx, owner.agentId); found["transaction-grocer"].IsTransfer || found["transaction-grocer"].SpendingCategoryID != "" {
			t.Errorf("another agent's rule must not touch the owner's rows: %+v", found["transaction-grocer"])
		}
	})
}

// The reporting currency is kept on the agent row, read back and
// validated with the rest of its settings.
func TestFinanceReportingCurrencyIsKeptOnTheAgent(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-currency")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		updated, err := tx.UpdateAgent(fixture.agentId, func(agent *models.Agent) error {
			agent.ReportingCurrencyCode = "EUR"
			return nil
		})
		if err != nil || updated.ReportingCurrencyCode != "EUR" {
			t.Fatalf("UpdateAgent: %v %+v", err, updated)
		}
		if found, err := tx.GetAgent(fixture.agentId); err != nil || found.ReportingCurrencyCode != "EUR" {
			t.Errorf("GetAgent reads it back: %v %+v", err, found)
		}
		if _, err := tx.UpdateAgent(fixture.agentId, func(agent *models.Agent) error {
			agent.ReportingCurrencyCode = "euro"
			return nil
		}); err == nil {
			t.Error("a reporting currency that is not a code must be refused")
		}
	})
}

// What the person decided about a pending transaction follows it when it
// posts: under Plaid, to the posted transaction that names it; under
// SimpleFIN, which names nothing, to the posted transaction of the same
// account and amount a few days later, equal charges taken in order; and
// under either, to the same row when the provider keeps its id.
func TestPersonDecisionsFollowAPendingTransactionThatPosts(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-pending-decisions")
	accounts := sampleFinanceSync().Accounts
	pending := func(providerTransactionId, postedOn, amount, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: "account-checking", PostedOn: postedOn,
			Amount: amount, CurrencyCode: "USD", Description: description, IsPending: true}
	}
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		pending("plaid-pending-book", "2026-09-10", "-30.00", "BOOKSHOP"),
		pending("plaid-pending-move", "2026-09-10", "-250.00", "TO BROKERAGE"),
		pending("simple-pending-first", "2026-09-10", "-20.00", "BAKERY"),
		pending("simple-pending-second", "2026-09-11", "-20.00", "BAKERY"),
		pending("same-id", "2026-09-11", "-9.00", "NEWSSTAND"),
	}}, "2026-09-11")

	var spendingCategoryIdByName map[string]string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		spendingCategoryIdByName = spendingCategoryIdsByName(t, tx, fixture.agentId)
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for providerTransactionId, spendingCategoryName := range map[string]string{
			"plaid-pending-book":    finance.SpendingCategoryShopping,
			"simple-pending-first":  finance.SpendingCategoryGroceries,
			"simple-pending-second": finance.SpendingCategoryDining,
			"same-id":               finance.SpendingCategoryEntertainment,
		} {
			if _, err := tx.SetTransactionCategorization(fixture.agentId, found[providerTransactionId].ID, spendingCategoryIdByName[spendingCategoryName],
				models.CategorizedByPerson, nil); err != nil {
				t.Fatalf("SetTransactionCategorization: %s", err)
			}
		}
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["plaid-pending-move"].ID, true, models.TransferMarkedByPerson); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
	})

	// Plaid: the posted ones name the pending ones they replace, which are
	// removed in the same sync.
	plaidPosted := &finance.SyncResult{Accounts: accounts, Added: []finance.Transaction{
		{ProviderTransactionID: "plaid-posted-book", ProviderAccountID: "account-checking", PostedOn: "2026-09-12", Amount: "-30.00",
			CurrencyCode: "USD", Description: "BOOKSHOP", PendingProviderTransactionID: "plaid-pending-book"},
		{ProviderTransactionID: "plaid-posted-move", ProviderAccountID: "account-checking", PostedOn: "2026-09-12", Amount: "-250.00",
			CurrencyCode: "USD", Description: "TO BROKERAGE", PendingProviderTransactionID: "plaid-pending-move"},
	}, RemovedProviderTransactionIDs: []string{"plaid-pending-book", "plaid-pending-move"}}
	applied := applyFinanceSync(t, database, fixture, plaidPosted, "2026-09-12")
	if len(applied.FinanceTransactionIDsToCategorize) != 0 {
		t.Errorf("a posted transaction that took the person's decision is not categorized again: %v", applied.FinanceTransactionIDsToCategorize)
	}

	// SimpleFIN: the pending ones are not sent again; the posted ones
	// arrive with new ids, and the one kept its id.
	replacedFrom := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	simplePosted := &finance.SyncResult{Accounts: accounts, PendingReplacedFrom: &replacedFrom, Added: []finance.Transaction{
		{ProviderTransactionID: "simple-posted-first", ProviderAccountID: "account-checking", PostedOn: "2026-09-12", Amount: "-20.00",
			CurrencyCode: "USD", Description: "BAKERY"},
		{ProviderTransactionID: "simple-posted-second", ProviderAccountID: "account-checking", PostedOn: "2026-09-13", Amount: "-20.00",
			CurrencyCode: "USD", Description: "BAKERY"},
		{ProviderTransactionID: "simple-posted-unrelated", ProviderAccountID: "account-checking", PostedOn: "2026-09-13", Amount: "-30.00",
			CurrencyCode: "USD", Description: "HARDWARE"},
		{ProviderTransactionID: "plaid-posted-book", ProviderAccountID: "account-checking", PostedOn: "2026-09-12", Amount: "-30.00",
			CurrencyCode: "USD", Description: "BOOKSHOP", PendingProviderTransactionID: "plaid-pending-book"},
		{ProviderTransactionID: "plaid-posted-move", ProviderAccountID: "account-checking", PostedOn: "2026-09-12", Amount: "-250.00",
			CurrencyCode: "USD", Description: "TO BROKERAGE", PendingProviderTransactionID: "plaid-pending-move"},
		{ProviderTransactionID: "same-id", ProviderAccountID: "account-checking", PostedOn: "2026-09-12", Amount: "-9.00",
			CurrencyCode: "USD", Description: "NEWSSTAND"},
	}}
	applied = applyFinanceSync(t, database, fixture, simplePosted, "2026-09-13")
	if applied.ReplacedPendingTransactionCount != 2 {
		t.Fatalf("the two pending bakery charges are replaced: %+v", applied)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for _, providerTransactionId := range []string{"plaid-pending-book", "plaid-pending-move", "simple-pending-first", "simple-pending-second"} {
			if found[providerTransactionId] != nil {
				t.Errorf("%s is gone once posted", providerTransactionId)
			}
		}
		for providerTransactionId, spendingCategoryName := range map[string]string{
			"plaid-posted-book":    finance.SpendingCategoryShopping,
			"simple-posted-first":  finance.SpendingCategoryGroceries,
			"simple-posted-second": finance.SpendingCategoryDining,
			"same-id":              finance.SpendingCategoryEntertainment,
		} {
			posted := found[providerTransactionId]
			if posted == nil || posted.CategorizedBy != models.CategorizedByPerson || posted.SpendingCategoryID != spendingCategoryIdByName[spendingCategoryName] {
				t.Errorf("%s keeps the person's %s: %+v", providerTransactionId, spendingCategoryName, posted)
			}
		}
		if move := found["plaid-posted-move"]; !move.IsTransfer || move.TransferMarkedBy != models.TransferMarkedByPerson {
			t.Errorf("the person's transfer follows the posted transaction: %+v", move)
		}
		if unrelated := found["simple-posted-unrelated"]; unrelated.CategorizedBy != "" {
			t.Errorf("a posted transaction no pending one matches takes nothing: %+v", unrelated)
		}
	})
}

// Pairing is one to one and closest first: a transfer to savings on
// Monday pairs with its arrival on Monday, and a rent check of the same
// amount on Tuesday stays spending. A finance transaction the person
// categorized, or that is already a transfer, is not paired.
func TestDetectFinanceTransfersPairsOneToOne(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-transfer-one-to-one")
	result := sampleFinanceSync()
	result.Accounts = append(result.Accounts, finance.Account{ProviderAccountID: "account-savings", AccountName: "Rainy Day Savings",
		AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "5000"})
	result.Added = []finance.Transaction{
		{ProviderTransactionID: "to-savings", ProviderAccountID: "account-checking", PostedOn: "2026-09-07", Amount: "-500", CurrencyCode: "USD", Description: "ONLINE TRANSFER"},
		{ProviderTransactionID: "rent-check", ProviderAccountID: "account-checking", PostedOn: "2026-09-08", Amount: "-500", CurrencyCode: "USD", Description: "CHECK 1041"},
		{ProviderTransactionID: "into-savings", ProviderAccountID: "account-savings", PostedOn: "2026-09-07", Amount: "500", CurrencyCode: "USD", Description: "ONLINE TRANSFER"},
		{ProviderTransactionID: "person-spending", ProviderAccountID: "account-checking", PostedOn: "2026-09-09", Amount: "-80", CurrencyCode: "USD", Description: "CONCERT TICKETS"},
		{ProviderTransactionID: "refund-in", ProviderAccountID: "account-card", PostedOn: "2026-09-09", Amount: "80", CurrencyCode: "USD", Description: "TICKET REFUND"},
		{ProviderTransactionID: "already-out", ProviderAccountID: "account-checking", PostedOn: "2026-09-10", Amount: "-45", CurrencyCode: "USD", Description: "TO CARD"},
		{ProviderTransactionID: "already-in", ProviderAccountID: "account-card", PostedOn: "2026-09-10", Amount: "45", CurrencyCode: "USD", Description: "FROM CHECKING"},
		{ProviderTransactionID: "stray-in", ProviderAccountID: "account-savings", PostedOn: "2026-09-10", Amount: "45", CurrencyCode: "USD", Description: "INTEREST ADJUSTMENT"},
	}
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["person-spending"].ID, byName[finance.SpendingCategoryEntertainment],
			models.CategorizedByPerson, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["already-out"].ID, true, models.TransferMarkedBySpendingRule); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["already-in"].ID, true, models.TransferMarkedBySpendingRule); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
		markedCount, err := tx.DetectFinanceTransfers(fixture.agentId, fixture.sourceId, "2026-09-01")
		if err != nil {
			t.Fatalf("DetectFinanceTransfers: %s", err)
		}
		if markedCount != 2 {
			t.Errorf("only the savings transfer pairs, got %d marked", markedCount)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		for providerTransactionId, expectedMarkedBy := range map[string]models.TransferMarkedBy{
			"to-savings": models.TransferMarkedByDetection, "into-savings": models.TransferMarkedByDetection,
			"rent-check": "", "person-spending": "", "refund-in": "", "stray-in": "",
			"already-out": models.TransferMarkedBySpendingRule, "already-in": models.TransferMarkedBySpendingRule,
		} {
			financeTransaction := found[providerTransactionId]
			if financeTransaction.TransferMarkedBy != expectedMarkedBy || financeTransaction.IsTransfer != (expectedMarkedBy != "") {
				t.Errorf("%s: transfer %v marked by %q, want %q", providerTransactionId, financeTransaction.IsTransfer, financeTransaction.TransferMarkedBy, expectedMarkedBy)
			}
		}
	})
}

// A card payment out of checking is a transfer by its provider category,
// and the card's credit for it, with no category, is found by pairing with
// it: otherwise the credit counts as a refund on the card. The checking
// side, once paired, is not taken by a second credit of the same amount.
func TestDetectFinanceTransfersPairsWithAProviderMarkedSide(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-transfer-provider-marked")
	result := sampleFinanceSync()
	result.Added = []finance.Transaction{
		{ProviderTransactionID: "card-payment-out", ProviderAccountID: "account-checking", PostedOn: "2026-09-14", Amount: "-640", CurrencyCode: "USD",
			Description: "CARD PAYMENT", ProviderCategoryPrimary: "LOAN_PAYMENTS", ProviderCategoryDetailed: "LOAN_PAYMENTS_CREDIT_CARD_PAYMENT"},
		{ProviderTransactionID: "card-payment-in", ProviderAccountID: "account-card", PostedOn: "2026-09-16", Amount: "640", CurrencyCode: "USD", Description: "PAYMENT THANK YOU"},
		{ProviderTransactionID: "second-credit", ProviderAccountID: "account-card", PostedOn: "2026-09-16", Amount: "640", CurrencyCode: "USD", Description: "MERCHANT CREDIT"},
	}
	applyFinanceSync(t, database, fixture, result, "2026-09-20")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.DetectFinanceTransfers(fixture.agentId, fixture.sourceId, "2026-09-01"); err != nil {
			t.Fatalf("DetectFinanceTransfers: %s", err)
		}
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		pairedCredits := 0
		for _, providerTransactionId := range []string{"card-payment-in", "second-credit"} {
			if found[providerTransactionId].IsTransfer {
				pairedCredits++
			}
		}
		if !found["card-payment-out"].IsTransfer || found["card-payment-out"].TransferMarkedBy != models.TransferMarkedByDetection {
			t.Errorf("the payment out: transfer %v marked by %q, want paired", found["card-payment-out"].IsTransfer, found["card-payment-out"].TransferMarkedBy)
		}
		if pairedCredits != 1 {
			t.Errorf("%d card credits paired with the one payment, want exactly 1", pairedCredits)
		}
	})
}

// What the categorize model was asked about and could not place is not
// listed for it again, until what it is judged from changes; a change of
// amount alone is not that.
func TestCategorizeAttemptedIsListedAgainOnlyWhenChanged(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "finance-categorize-attempted")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		markedCount, err := tx.MarkCategorizeAttempted(fixture.agentId, []string{found["transaction-grocer"].ID, found["transaction-diner"].ID})
		if err != nil || markedCount != 2 {
			t.Fatalf("MarkCategorizeAttempted: %v %d", err, markedCount)
		}
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(fixture.agentId, 10)
		if err != nil || len(uncategorized) != 1 || uncategorized[0].ID != found["transaction-salary"].ID {
			t.Fatalf("only the one never asked about is listed: %v %+v", err, uncategorized)
		}
	})

	changed := sampleFinanceSync()
	changed.Added[0].MerchantName = "Corner Grocer Market"
	changed.Added[2].Amount = "-19.40"
	applied := applyFinanceSync(t, database, fixture, changed, "2026-09-13")
	if applied.WrittenTransactionCount != 2 {
		t.Fatalf("two changed: %+v", applied)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(fixture.agentId, 10)
		if err != nil {
			t.Fatalf("ListUncategorizedFinanceTransactions: %s", err)
		}
		listed := map[string]bool{}
		for _, financeTransaction := range uncategorized {
			listed[financeTransaction.ID] = true
		}
		if !listed[found["transaction-grocer"].ID] || listed[found["transaction-diner"].ID] || len(listed) != 2 {
			t.Errorf("the grocer's merchant changed and it is listed again; the diner's amount alone did not: %+v", uncategorized)
		}
	})
}
