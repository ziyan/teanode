package db_test

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// mirroredBrokerageSync is one connection with three brokerage accounts
// and a checking account. The account-level fee arrives once per brokerage
// account, each copy with its own id; a fee charged twice on one account
// that same day, and the copies of a pending charge, are there too.
// Mirrored copies are looked for in a Plaid source only, so the fixture
// syncing it is createPlaidFinanceFixture.
func mirroredBrokerageSync() *finance.SyncResult {
	brokerage := func(providerAccountId, name string) finance.Account {
		return finance.Account{ProviderAccountID: providerAccountId, AccountName: name, AccountKind: finance.AccountKindInvestment,
			CurrencyCode: "USD", CurrentBalance: "1000"}
	}
	fee := func(providerTransactionId, providerAccountId, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: "2026-09-15",
			Amount: "-25", CurrencyCode: "USD", Description: description}
	}
	return &finance.SyncResult{
		Accounts: []finance.Account{
			brokerage("account-individual", "Individual"), brokerage("account-joint", "Joint"), brokerage("account-retirement", "Retirement"),
			{ProviderAccountID: "account-checking", AccountName: "Checking", AccountKind: finance.AccountKindDepository, CurrencyCode: "USD", CurrentBalance: "500"},
		},
		Added: []finance.Transaction{
			// The descriptions differ in case and spaces only.
			fee("fee-individual", "account-individual", "ACCOUNT FEE"),
			fee("fee-joint", "account-joint", "Account Fee "),
			fee("fee-retirement", "account-retirement", " account fee"),
			// Two charges of the same on one account are two charges.
			{ProviderTransactionID: "coffee-first", ProviderAccountID: "account-checking", PostedOn: "2026-09-15",
				Amount: "-4.50", CurrencyCode: "USD", Description: "CORNER CAFE"},
			{ProviderTransactionID: "coffee-second", ProviderAccountID: "account-checking", PostedOn: "2026-09-15",
				Amount: "-4.50", CurrencyCode: "USD", Description: "CORNER CAFE"},
			// Pending on two accounts: grouped with each other only.
			{ProviderTransactionID: "pending-individual", ProviderAccountID: "account-individual", PostedOn: "2026-09-16",
				Amount: "-10", CurrencyCode: "USD", Description: "WIRE FEE", IsPending: true},
			{ProviderTransactionID: "pending-joint", ProviderAccountID: "account-joint", PostedOn: "2026-09-16",
				Amount: "-10", CurrencyCode: "USD", Description: "WIRE FEE", IsPending: true},
			// Different amount, different day: not copies.
			{ProviderTransactionID: "fee-other-amount", ProviderAccountID: "account-joint", PostedOn: "2026-09-15",
				Amount: "-30", CurrencyCode: "USD", Description: "ACCOUNT FEE"},
			{ProviderTransactionID: "fee-other-day", ProviderAccountID: "account-retirement", PostedOn: "2026-09-14",
				Amount: "-25", CurrencyCode: "USD", Description: "ACCOUNT FEE"},
		},
	}
}

// createPlaidFinanceFixture is a finance fixture whose source is a Plaid
// connection, the only provider whose copies are looked for.
func createPlaidFinanceFixture(t *testing.T, database db.Database, username string) financeFixture {
	t.Helper()
	fixture := createFinanceFixture(t, database, username)
	setFinanceSourceType(t, database, fixture.sourceId, string(finance.ProviderKindPlaid))
	return fixture
}

// setFinanceSourceType makes a source one of this provider's.
func setFinanceSourceType(t *testing.T, database db.Database, sourceId, sourceType string) {
	t.Helper()
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_source" SET "specification" = jsonb_set("specification"::jsonb, '{type}', to_jsonb('%s'::text))
		WHERE "id" = '%s'`, sourceType, sourceId))
}

// duplicateOfByProviderId is what each finance transaction is a duplicate
// of, by provider ids, leaving out the counted ones.
func duplicateOfByProviderId(t *testing.T, tx db.Transaction, agentId string) map[string]string {
	t.Helper()
	found := financeTransactionsByProviderId(t, tx, agentId)
	providerIdById := map[string]string{}
	for providerTransactionId, financeTransaction := range found {
		providerIdById[financeTransaction.ID] = providerTransactionId
	}
	duplicates := map[string]string{}
	for providerTransactionId, financeTransaction := range found {
		if financeTransaction.DuplicateOfTransactionID == "" {
			continue
		}
		if financeTransaction.DuplicateDecidedBy != models.DuplicateDecidedByMirrorDetection {
			t.Errorf("%s: a duplicate decided by %q", providerTransactionId, financeTransaction.DuplicateDecidedBy)
		}
		duplicates[providerTransactionId] = providerIdById[financeTransaction.DuplicateOfTransactionID]
	}
	return duplicates
}

// One fee on three brokerage accounts of one Plaid connection is one
// counted copy and two duplicates of it, and a pending pair is one and its
// duplicate; two charges on one account, and a different amount or day,
// are none.
func TestMirroredFinanceTransactionsWithinOneSource(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-one-source")
	result := mirroredBrokerageSync()
	applyFinanceSync(t, database, fixture, result, "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) != 3 {
			t.Fatalf("two duplicates of one counted copy, and one of the pending pair: %v", duplicates)
		}
		if (duplicates["pending-individual"] == "pending-joint") == (duplicates["pending-joint"] == "pending-individual") {
			t.Errorf("one of the pending pair is a duplicate of the other: %v", duplicates)
		}
		counted := duplicates["fee-joint"]
		if counted == "" || duplicates["fee-retirement"] != counted || duplicates[counted] != "" {
			t.Errorf("the two other copies point at the counted one: %v", duplicates)
		}
		// A sync stores all three at once, so the oldest account wins
		// only by its id; what matters is that the choice holds.
		again, err := tx.DetectMirroredFinanceTransactions(fixture.agentId, fixture.sourceId)
		if err != nil || again != 0 {
			t.Errorf("detecting again changes nothing: %d %v", again, err)
		}
		page, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{
			DuplicateOfTransactionID: financeTransactionsByProviderId(t, tx, fixture.agentId)[counted].ID,
		})
		if err != nil || len(page.FinanceTransactions) != 2 {
			t.Errorf("the counted copy's duplicates are listed by it: %v %+v", err, page)
		}
	})

	// The pending pair, posted, is grouped again; the pending rows the
	// provider replaced are gone.
	posted := &finance.SyncResult{
		Accounts: result.Accounts,
		Added: []finance.Transaction{
			{ProviderTransactionID: "posted-individual", ProviderAccountID: "account-individual", PostedOn: "2026-09-16",
				Amount: "-10", CurrencyCode: "USD", Description: "WIRE FEE", PendingProviderTransactionID: "pending-individual"},
			{ProviderTransactionID: "posted-joint", ProviderAccountID: "account-joint", PostedOn: "2026-09-16",
				Amount: "-10", CurrencyCode: "USD", Description: "WIRE FEE", PendingProviderTransactionID: "pending-joint"},
		},
		RemovedProviderTransactionIDs: []string{"pending-individual", "pending-joint"},
	}
	applyFinanceSync(t, database, fixture, posted, "2026-09-17")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) != 3 || (duplicates["posted-individual"] == "") == (duplicates["posted-joint"] == "") {
			t.Errorf("one of the posted pair is a duplicate of the other: %v", duplicates)
		}
	})
}

// Copies are looked for within one connection only: the same fee from
// two finance sources is two charges.
func TestMirroredFinanceTransactionsNeverAcrossSources(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-two-sources")
	var other financeFixture
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: fixture.agentId, Kind: models.SourceWeb, Name: "another institution",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.net/"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		other = financeFixture{agentId: fixture.agentId, sourceId: source.ID}
	})
	setFinanceSourceType(t, database, other.sourceId, string(finance.ProviderKindPlaid))
	single := func(providerAccountId, providerTransactionId string) *finance.SyncResult {
		return &finance.SyncResult{
			Accounts: []finance.Account{{ProviderAccountID: providerAccountId, AccountName: "Brokerage", AccountKind: finance.AccountKindInvestment, CurrencyCode: "USD"}},
			Added: []finance.Transaction{{ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: "2026-09-15",
				Amount: "-25", CurrencyCode: "USD", Description: "ACCOUNT FEE"}},
		}
	}
	applyFinanceSync(t, database, fixture, single("account-here", "fee-here"), "2026-09-16")
	applyFinanceSync(t, database, other, single("account-there", "fee-there"), "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.DetectMirroredFinanceTransactions(fixture.agentId, ""); err != nil {
			t.Fatalf("DetectMirroredFinanceTransactions: %s", err)
		}
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); len(duplicates) != 0 {
			t.Errorf("no copies across two sources: %v", duplicates)
		}
	})
}

// A set is decided again when a member goes or changes: the copies of a
// counted copy that is removed count one of them, a copy whose amount
// changed counts again, and a copy arriving later never takes over.
func TestMirroredFinanceTransactionsAreDecidedAgain(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-again")
	first := mirroredBrokerageSync()
	// The first sync brings the fee on one account only.
	first.Added = first.Added[:1]
	applyFinanceSync(t, database, fixture, first, "2026-09-15")
	full := mirroredBrokerageSync()
	applyFinanceSync(t, database, fixture, full, "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if duplicates["fee-joint"] != "fee-individual" || duplicates["fee-retirement"] != "fee-individual" {
			t.Fatalf("the copy stored first stays counted: %v", duplicates)
		}
	})

	// The counted copy is removed: one of the other two is counted now.
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: full.Accounts, RemovedProviderTransactionIDs: []string{"fee-individual"}}, "2026-09-17")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		isJointCounted := duplicates["fee-retirement"] == "fee-joint" && duplicates["fee-joint"] == ""
		isRetirementCounted := duplicates["fee-joint"] == "fee-retirement" && duplicates["fee-retirement"] == ""
		if !isJointCounted && !isRetirementCounted {
			t.Errorf("one of the two left is counted, the other its duplicate: %v", duplicates)
		}
	})

	// One of the two left changes its amount: neither is a copy.
	changed := full.Added[2]
	changed.Amount = "-26"
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: full.Accounts, Added: []finance.Transaction{changed}}, "2026-09-18")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); duplicates["fee-joint"] != "" || duplicates["fee-retirement"] != "" {
			t.Errorf("a set that no longer holds counts each again: %v", duplicates)
		}
	})
}

// The person counting a copy sticks across syncs that send it again, and
// taking that back hands it to detection, which marks it again at once.
// Counting one that is not a duplicate is refused.
func TestMirroredFinanceTransactionCountedByThePerson(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-person")
	first := mirroredBrokerageSync()
	first.Added = first.Added[:1]
	applyFinanceSync(t, database, fixture, first, "2026-09-15")
	full := mirroredBrokerageSync()
	applyFinanceSync(t, database, fixture, full, "2026-09-16")
	var jointId, individualId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		jointId, individualId = found["fee-joint"].ID, found["fee-individual"].ID
		if err := tx.SetFinanceTransactionCountedByPerson(fixture.agentId, individualId, true); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("counting the counted copy is refused: %v", err)
		}
		if err := tx.SetFinanceTransactionCountedByPerson(fixture.agentId, "no-such-transaction", true); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("an unknown finance transaction is not found: %v", err)
		}
		if err := tx.SetFinanceTransactionCountedByPerson(fixture.agentId, jointId, true); err != nil {
			t.Fatalf("SetFinanceTransactionCountedByPerson: %s", err)
		}
	})

	// Sent again, and changed in its description: still counted.
	again := mirroredBrokerageSync()
	again.Added[1].Description = "ACCOUNT FEE"
	applyFinanceSync(t, database, fixture, again, "2026-09-17")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		joint, err := tx.GetFinanceTransaction(fixture.agentId, jointId)
		if err != nil || joint.DuplicateOfTransactionID != "" || joint.DuplicateDecidedBy != models.DuplicateDecidedByPerson {
			t.Fatalf("the person's count survives the sync: %v %+v", err, joint)
		}
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); duplicates["fee-retirement"] != "fee-individual" || duplicates["fee-joint"] != "" {
			t.Errorf("the other copy stays a duplicate of the counted one: %v", duplicates)
		}
		if err := tx.SetFinanceTransactionCountedByPerson(fixture.agentId, jointId, false); err != nil {
			t.Fatalf("SetFinanceTransactionCountedByPerson: %s", err)
		}
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); duplicates["fee-joint"] != "fee-individual" {
			t.Errorf("taken back, it is a duplicate again at once: %v", duplicates)
		}
	})
}

// The totals with mirrored copies are the totals with the extra copies
// deleted: spending, income, cash flow, merchant months and the spending
// summary each count the charge once.
func TestMirroredFinanceTransactionsCountOnce(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-totals")
	result := mirroredBrokerageSync()
	// Mirrored money in too, and a fee in an earlier month for the
	// merchant months.
	for _, providerAccountId := range []string{"account-individual", "account-joint", "account-retirement"} {
		result.Added = append(result.Added,
			finance.Transaction{ProviderTransactionID: "interest-" + providerAccountId, ProviderAccountID: providerAccountId, PostedOn: "2026-09-30",
				Amount: "3.10", CurrencyCode: "USD", Description: "SWEEP INTEREST"},
			finance.Transaction{ProviderTransactionID: "august-" + providerAccountId, ProviderAccountID: providerAccountId, PostedOn: "2026-08-15",
				Amount: "-25", CurrencyCode: "USD", Description: "ACCOUNT FEE", MerchantName: "Example Brokerage"},
		)
	}
	applyFinanceSync(t, database, fixture, result, "2026-09-30")
	var withCopies string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		for providerTransactionId, financeTransaction := range financeTransactionsByProviderId(t, tx, fixture.agentId) {
			spendingCategoryName := finance.SpendingCategoryFees
			if financeTransaction.Amount[0] != '-' {
				spendingCategoryName = finance.SpendingCategoryIncome
			}
			if _, err := tx.SetTransactionCategorization(fixture.agentId, financeTransaction.ID, byName[spendingCategoryName], models.CategorizedBySpendingRule, nil); err != nil {
				t.Fatalf("%s: SetTransactionCategorization: %s", providerTransactionId, err)
			}
		}
		withCopies = transferCategoryTotals(t, tx, fixture.agentId)

		// A listing that leaves the copies out counts them apart, under
		// the same filters as its total.
		every, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{Text: "fee", ShouldCountTotal: true})
		if err != nil || every.LeftOutDuplicateCount != 0 {
			t.Fatalf("with the copies, none is left out: %v %+v", err, every)
		}
		counted, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{Text: "fee", IsDuplicateExcluded: true, ShouldCountTotal: true})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		if counted.LeftOutDuplicateCount == 0 || counted.TotalCount+counted.LeftOutDuplicateCount != every.TotalCount || len(counted.FinanceTransactions) != counted.TotalCount {
			t.Errorf("%d listed and %d left out of %d", counted.TotalCount, counted.LeftOutDuplicateCount, every.TotalCount)
		}
		for _, financeTransaction := range counted.FinanceTransactions {
			if financeTransaction.DuplicateOfTransactionID != "" {
				t.Errorf("a copy was listed: %+v", financeTransaction)
			}
		}

		// The ids alone, a row at a time by the cursor as Select all reads
		// them, are the same ids in the same order, and nothing else of
		// the rows is read.
		idsByCursor := []string{}
		cursor := ""
		for {
			idsOnly, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{Text: "fee", IsDuplicateExcluded: true,
				Limit: 1, After: cursor, ShouldReadIDsOnly: true})
			if err != nil {
				t.Fatalf("ListFinanceTransactions with ids only: %s", err)
			}
			for _, financeTransaction := range idsOnly.FinanceTransactions {
				if financeTransaction.Description != "" || financeTransaction.Amount != "" {
					t.Errorf("more than the id was read: %+v", financeTransaction)
				}
				idsByCursor = append(idsByCursor, financeTransaction.ID)
			}
			if idsOnly.NextCursor == "" {
				break
			}
			cursor = idsOnly.NextCursor
		}
		countedIds := []string{}
		for _, financeTransaction := range counted.FinanceTransactions {
			countedIds = append(countedIds, financeTransaction.ID)
		}
		if fmt.Sprint(idsByCursor) != fmt.Sprint(countedIds) {
			t.Errorf("ids only %v, the full rows %v", idsByCursor, countedIds)
		}
	})
	// Every duplicate deleted outright, which is what counting once means.
	dbtest.Exec(t, database, fmt.Sprintf(`DELETE FROM "agent_finance_transaction" WHERE "agent_id" = '%s' AND "duplicate_of_transaction_id" IS NOT NULL`, fixture.agentId))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if withoutCopies := transferCategoryTotals(t, tx, fixture.agentId); withoutCopies != withCopies {
			t.Errorf("the totals count a mirrored copy:\nwith the copies\n%s\nwithout them\n%s", withCopies, withoutCopies)
		}
	})
	expected := "Example Brokerage/USD/25.0000/0.0000/1"
	if !containsLine(withCopies, expected) {
		t.Errorf("the August fee is counted once in the summary (%s):\n%s", expected, withCopies)
	}
}

// containsLine says one line of the text is exactly this.
func containsLine(text, line string) bool {
	for start := 0; start <= len(text); {
		end := start
		for end < len(text) && text[end] != '\n' {
			end++
		}
		if text[start:end] == line {
			return true
		}
		start = end + 1
	}
	return false
}

// Copies are looked for in a Plaid source only: a SimpleFIN credential
// can reach accounts at several institutions, and the statement source
// holds accounts from different ones, whose same-day fees are separate
// charges.
func TestMirroredFinanceTransactionsOnlyInPlaidSources(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	for _, sourceType := range []finance.ProviderKind{finance.ProviderKindSimpleFIN, finance.ProviderKindStatement} {
		fixture := createFinanceFixture(t, database, "mirrored-"+string(sourceType))
		setFinanceSourceType(t, database, fixture.sourceId, string(sourceType))
		applyFinanceSync(t, database, fixture, mirroredBrokerageSync(), "2026-09-16")
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); len(duplicates) != 0 {
				t.Errorf("%s: nothing is a copy: %v", sourceType, duplicates)
			}
		})
	}
}

// Within one Plaid connection, a checking and a savings account can each
// be charged the same monthly fee for real, so only a set whose every copy
// is on an investment account is mirrored: two deposit accounts are not,
// and neither are two brokerage accounts with a deposit account beside
// them.
func TestMirroredFinanceTransactionsOnlyOnInvestmentAccounts(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-deposit")
	account := func(providerAccountId, accountKind string) finance.Account {
		return finance.Account{ProviderAccountID: providerAccountId, AccountName: providerAccountId, AccountKind: accountKind, CurrencyCode: "USD"}
	}
	charge := func(providerTransactionId, providerAccountId, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: "2026-09-15",
			Amount: "-12", CurrencyCode: "USD", Description: description}
	}
	applyFinanceSync(t, database, fixture, &finance.SyncResult{
		Accounts: []finance.Account{
			account("account-checking", finance.AccountKindDepository), account("account-savings", finance.AccountKindDepository),
			account("account-brokerage", finance.AccountKindInvestment), account("account-retirement", finance.AccountKindInvestment),
		},
		Added: []finance.Transaction{
			charge("monthly-checking", "account-checking", "MONTHLY SERVICE FEE"),
			charge("monthly-savings", "account-savings", "MONTHLY SERVICE FEE"),
			charge("advisory-brokerage", "account-brokerage", "ADVISORY FEE"),
			charge("advisory-retirement", "account-retirement", "ADVISORY FEE"),
			charge("advisory-checking", "account-checking", "ADVISORY FEE"),
		},
	}, "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); len(duplicates) != 0 {
			t.Errorf("nothing beside a deposit account is a copy: %v", duplicates)
		}
	})
}

// A pending fee mirrored on three brokerage accounts counts once, with a
// copy that has posted already counted before the pending ones; a pending
// and a posted row on one account are two charges, never copies of each
// other. The posted rows that replace the pending ones stay duplicates of
// the copy that posted first.
func TestMirroredPendingCopiesCountOnce(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-pending")
	accounts := mirroredBrokerageSync().Accounts
	applyFinanceSync(t, database, fixture, &finance.SyncResult{
		Accounts: accounts,
		Added: []finance.Transaction{
			mirroredWireFee("pending-individual", "account-individual", true), mirroredWireFee("pending-joint", "account-joint", true),
			mirroredWireFee("pending-retirement", "account-retirement", true),
			// Posted already on one account, while its pending row is still
			// there: two rows of one account are never copies of each other.
			mirroredWireFee("posted-individual", "account-individual", false),
		},
	}, "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) != 2 || duplicates["pending-joint"] != "posted-individual" || duplicates["pending-retirement"] != "posted-individual" {
			t.Fatalf("the posted copy is counted and the other accounts' pending copies are its duplicates: %v", duplicates)
		}
		if _, isDuplicate := duplicates["pending-individual"]; isDuplicate {
			t.Errorf("a pending row is not a copy of a posted one on its own account: %v", duplicates)
		}
		page, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{IsDuplicateExcluded: true})
		if err != nil || len(page.FinanceTransactions) != 2 {
			t.Errorf("the set and the other row of the individual account count once each: %v %+v", err, page)
		}
	})

	posted := &finance.SyncResult{Accounts: accounts, RemovedProviderTransactionIDs: []string{"pending-individual", "pending-joint", "pending-retirement"}}
	for _, providerAccountId := range []string{"account-joint", "account-retirement"} {
		replacement := mirroredWireFee("posted-"+providerAccountId, providerAccountId, false)
		replacement.PendingProviderTransactionID = "pending-" + providerAccountId[len("account-"):]
		posted.Added = append(posted.Added, replacement)
	}
	applyFinanceSync(t, database, fixture, posted, "2026-09-17")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) != 2 || duplicates["posted-account-joint"] != "posted-individual" || duplicates["posted-account-retirement"] != "posted-individual" {
			t.Errorf("the posted fee stored first is counted and the replacements are its duplicates: %v", duplicates)
		}
	})
}

// mirroredWireFee is one copy of a wire fee mirrored on the brokerage
// accounts of mirroredBrokerageSync, pending or posted.
func mirroredWireFee(providerTransactionId, providerAccountId string, isPending bool) finance.Transaction {
	return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: "2026-09-16",
		Amount: "-10", CurrencyCode: "USD", Description: "WIRE FEE", IsPending: isPending}
}

// mirroredWireFeePosts is the sync in which the wire fee's pending copy on
// one brokerage account ("joint" for account-joint) is replaced by its
// posted one.
func mirroredWireFeePosts(accountName string) *finance.SyncResult {
	posting := mirroredWireFee("posted-"+accountName, "account-"+accountName, false)
	posting.PendingProviderTransactionID = "pending-" + accountName
	return &finance.SyncResult{Accounts: mirroredBrokerageSync().Accounts, Added: []finance.Transaction{posting},
		RemovedProviderTransactionIDs: []string{"pending-" + accountName}}
}

// countedWireFeeIds is the provider ids of the wire fee's copies that
// count: those that are not a duplicate of another.
func countedWireFeeIds(t *testing.T, tx db.Transaction, agentId string) []string {
	t.Helper()
	page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{Text: "WIRE FEE", IsDuplicateExcluded: true, Limit: db.FinanceTransactionLimitMost})
	if err != nil {
		t.Fatalf("ListFinanceTransactions: %s", err)
	}
	countedIds := []string{}
	for _, financeTransaction := range page.FinanceTransactions {
		countedIds = append(countedIds, financeTransaction.ProviderTransactionID)
	}
	return countedIds
}

// The copies of one charge post on different syncs. At every step, with
// some copies pending and some posted, the charge counts exactly once:
// never twice (a pending copy alone beside a posted one) and never zero
// times. The copy that posts first counts from then on, whether or not its
// pending copy was the counted one.
func TestMirroredCopiesPostingOnDifferentSyncsCountOnce(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	for _, order := range [][]string{{"joint", "individual", "retirement"}, {"individual", "retirement", "joint"}} {
		fixture := createPlaidFinanceFixture(t, database, "mirrored-posting-"+order[0])
		applyFinanceSync(t, database, fixture, &finance.SyncResult{
			Accounts: mirroredBrokerageSync().Accounts,
			Added: []finance.Transaction{
				mirroredWireFee("pending-individual", "account-individual", true), mirroredWireFee("pending-joint", "account-joint", true),
				mirroredWireFee("pending-retirement", "account-retirement", true),
			},
		}, "2026-09-16")
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if counted := countedWireFeeIds(t, tx, fixture.agentId); len(counted) != 1 {
				t.Fatalf("%v: all pending, the charge counts once: %v", order, counted)
			}
		})
		for index, accountName := range order {
			applyFinanceSync(t, database, fixture, mirroredWireFeePosts(accountName), fmt.Sprintf("2026-09-%d", 17+index))
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				counted := countedWireFeeIds(t, tx, fixture.agentId)
				if len(counted) != 1 || counted[0] != "posted-"+order[0] {
					t.Errorf("%v: after the %s copy posted, only posted-%s counts: %v (duplicates %v)", order, accountName, order[0], counted,
						duplicateOfByProviderId(t, tx, fixture.agentId))
				}
			})
		}
	}
}

// The person counting a pending copy is kept when it posts: the posted row
// that replaces it counts by the person's word, and detection does not
// mark it a duplicate of the other account's copy again.
func TestMirroredPersonCountSurvivesPosting(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-person-posting")
	applyFinanceSync(t, database, fixture, &finance.SyncResult{
		Accounts: mirroredBrokerageSync().Accounts,
		Added:    []finance.Transaction{mirroredWireFee("pending-individual", "account-individual", true), mirroredWireFee("pending-joint", "account-joint", true)},
	}, "2026-09-16")
	var duplicateAccountName string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) != 1 {
			t.Fatalf("one pending copy is a duplicate of the other: %v", duplicates)
		}
		for providerTransactionId := range duplicates {
			duplicateAccountName = providerTransactionId[len("pending-"):]
		}
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if err := tx.SetFinanceTransactionCountedByPerson(fixture.agentId, found["pending-"+duplicateAccountName].ID, true); err != nil {
			t.Fatalf("SetFinanceTransactionCountedByPerson: %s", err)
		}
	})

	applyFinanceSync(t, database, fixture, mirroredWireFeePosts(duplicateAccountName), "2026-09-17")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		posted := financeTransactionsByProviderId(t, tx, fixture.agentId)["posted-"+duplicateAccountName]
		if posted == nil || posted.DuplicateOfTransactionID != "" || posted.DuplicateDecidedBy != models.DuplicateDecidedByPerson {
			t.Fatalf("the posted copy keeps the person's count: %+v", posted)
		}
		if counted := countedWireFeeIds(t, tx, fixture.agentId); len(counted) != 2 {
			t.Errorf("both copies count, one by the person's word: %v", counted)
		}
	})
}

// A counted copy deleted outside a sync, here with its finance account,
// leaves its copies pointing at nothing; the next detection, which every
// sync of the source and every count runs, decides them again so exactly
// one of them counts.
func TestMirroredCountedCopyDeletedOutsideASync(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-account-deleted")
	mirroredJointCopy(t, database, fixture)
	dbtest.Exec(t, database, fmt.Sprintf(`DELETE FROM "agent_finance_account" WHERE "agent_id" = '%s' AND "provider_account_id" = 'account-individual'`,
		fixture.agentId))
	applyFinanceSync(t, database, fixture, &finance.SyncResult{Accounts: mirroredBrokerageSync().Accounts[1:]}, "2026-09-17")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		isJointCounted := duplicates["fee-retirement"] == "fee-joint" && duplicates["fee-joint"] == ""
		isRetirementCounted := duplicates["fee-joint"] == "fee-retirement" && duplicates["fee-retirement"] == ""
		if !isJointCounted && !isRetirementCounted {
			t.Errorf("one of the two copies left is counted, the other its duplicate: %v", duplicates)
		}
	})
}

// A duplicate's category counts for nothing, so it is neither listed as
// uncategorized nor handed to the categorize model.
func TestMirroredCopiesAreNotUncategorized(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-uncategorized")
	applyFinanceSync(t, database, fixture, mirroredBrokerageSync(), "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) == 0 {
			t.Fatal("the data set has no duplicates")
		}
		uncategorized, err := tx.ListUncategorizedFinanceTransactions(fixture.agentId, 0)
		if err != nil {
			t.Fatalf("ListUncategorizedFinanceTransactions: %s", err)
		}
		page, err := tx.ListFinanceTransactions(fixture.agentId, &db.FinanceTransactionFilter{IsUncategorized: true, Limit: db.FinanceTransactionLimitMost})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for name, listed := range map[string][]*models.FinanceTransaction{"to categorize": uncategorized, "uncategorized": page.FinanceTransactions} {
			if len(listed) == 0 {
				t.Errorf("%s: nothing is listed", name)
			}
			for _, financeTransaction := range listed {
				if financeTransaction.DuplicateOfTransactionID != "" {
					t.Errorf("%s: the duplicate %s is listed", name, financeTransaction.ProviderTransactionID)
				}
			}
		}
	})
}

// waitForLockWaiter waits until a statement on the test's database waits
// for a lock another transaction holds.
func waitForLockWaiter(t *testing.T, database db.Database) bool {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		waiterCount := rawQueryString(t, database, `SELECT COUNT(*)::text FROM "pg_stat_activity"
			WHERE "datname" = current_database() AND "wait_event_type" = 'Lock'`)
		if waiterCount != "0" {
			return true
		}
	}
	return false
}

// mirroredJointCopy syncs the brokerage fee so that the joint account's
// copy is a duplicate of the individual account's, and answers its id.
func mirroredJointCopy(t *testing.T, database db.Database, fixture financeFixture) string {
	t.Helper()
	first := mirroredBrokerageSync()
	first.Added = first.Added[:1]
	applyFinanceSync(t, database, fixture, first, "2026-09-15")
	applyFinanceSync(t, database, fixture, mirroredBrokerageSync(), "2026-09-16")
	var jointId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); duplicates["fee-joint"] != "fee-individual" {
			t.Fatalf("the joint copy is a duplicate of the individual one: %v", duplicates)
		}
		jointId = financeTransactionsByProviderId(t, tx, fixture.agentId)["fee-joint"].ID
	})
	return jointId
}

// Detection waits for the person counting a copy of the same source to
// finish, so it reads their word rather than what was there before.
func TestMirroredDetectionWaitsForThePersonCounting(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-count-lock")
	jointId := mirroredJointCopy(t, database, fixture)

	isCounted, release, countDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		countDone <- database.Transaction(func(tx db.Transaction) error {
			if err := tx.SetFinanceTransactionCountedByPerson(fixture.agentId, jointId, true); err != nil {
				return err
			}
			close(isCounted)
			<-release
			return nil
		})
	}()
	select {
	case <-isCounted:
	case err := <-countDone:
		t.Fatalf("SetFinanceTransactionCountedByPerson: %v", err)
	}
	detectDone := make(chan error, 1)
	go func() {
		detectDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.DetectMirroredFinanceTransactions(fixture.agentId, fixture.sourceId)
			return err
		})
	}()
	isWaiting := waitForLockWaiter(t, database)
	close(release)
	if err := <-countDone; err != nil {
		t.Fatalf("the count: %s", err)
	}
	if err := <-detectDone; err != nil {
		t.Fatalf("the detection: %s", err)
	}
	if !isWaiting {
		t.Error("detection did not wait for the person counting a copy of its source")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		joint, err := tx.GetFinanceTransaction(fixture.agentId, jointId)
		if err != nil || joint.DuplicateDecidedBy != models.DuplicateDecidedByPerson || joint.DuplicateOfTransactionID != "" {
			t.Errorf("the person's count stands: %v %+v", err, joint)
		}
	})
}

// A row the person counted after detection's statement read it, but before
// detection wrote it, is left alone: detection checks again that the
// person has not decided it, on the row as it is by then.
func TestMirroredDetectionNeverWritesOverThePerson(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-count-race")
	jointId := mirroredJointCopy(t, database, fixture)
	// As a sync leaves a copy it has just stored: not decided yet, so the
	// detection below has a row to write.
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_finance_transaction" SET "duplicate_of_transaction_id" = NULL, "duplicate_decided_by" = ''
		WHERE "id" = '%s'`, jointId))

	// A writer that does not take the source's lock holds the row, saying
	// the person counted it, while detection reads and then waits for it.
	databaseName := rawQueryString(t, database, `SELECT current_database()`)
	writer, err := gorm.Open(postgres.Open(fmt.Sprintf("host=%s port=5432 user=teanode password=teanode dbname=%s sslmode=disable",
		os.Getenv("TEANODE_TEST_DATABASE_HOST"), databaseName)), &gorm.Config{})
	if err != nil {
		t.Fatalf("a second connection: %s", err)
	}
	writerConnection, err := writer.DB()
	if err != nil {
		t.Fatalf("a second connection: %s", err)
	}
	defer func() {
		if err := writerConnection.Close(); err != nil {
			t.Errorf("closing the second connection: %s", err)
		}
	}()
	writing := writer.Begin()
	if err := writing.Exec(`UPDATE "agent_finance_transaction" SET "duplicate_decided_by" = 'person' WHERE "id" = ?`, jointId).Error; err != nil {
		t.Fatalf("the person's count: %s", err)
	}
	detectDone := make(chan error, 1)
	go func() {
		detectDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.DetectMirroredFinanceTransactions(fixture.agentId, fixture.sourceId)
			return err
		})
	}()
	isWaiting := waitForLockWaiter(t, database)
	if err := writing.Commit().Error; err != nil {
		t.Fatalf("committing the person's count: %s", err)
	}
	if err := <-detectDone; err != nil {
		t.Fatalf("the detection: %s", err)
	}
	if !isWaiting {
		t.Fatal("detection did not wait for the row, so this test proved nothing")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		joint, err := tx.GetFinanceTransaction(fixture.agentId, jointId)
		if err != nil || joint.DuplicateDecidedBy != models.DuplicateDecidedByPerson || joint.DuplicateOfTransactionID != "" {
			t.Errorf("the person's count stands: %v %+v", err, joint)
		}
	})
}

// Migration 0144 marks the copies already stored the way detection marks
// them after a sync, and its reverse takes the columns away again.
func TestMirroredMigrationMarksStoredCopies(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createPlaidFinanceFixture(t, database, "mirrored-migration")
	first := mirroredBrokerageSync()
	first.Added = first.Added[:1]
	applyFinanceSync(t, database, fixture, first, "2026-09-15")
	full := mirroredBrokerageSync()
	// Two deposit accounts charged the same fee, which is not mirrored.
	full.Accounts = append(full.Accounts, finance.Account{ProviderAccountID: "account-savings", AccountName: "Savings",
		AccountKind: finance.AccountKindDepository, CurrencyCode: "USD"})
	for _, providerAccountId := range []string{"account-checking", "account-savings"} {
		full.Added = append(full.Added, finance.Transaction{ProviderTransactionID: "monthly-" + providerAccountId, ProviderAccountID: providerAccountId,
			PostedOn: "2026-09-15", Amount: "-12", CurrencyCode: "USD", Description: "MONTHLY SERVICE FEE"})
	}
	// The pending wire fee posted on a third account: counted before the
	// two pending copies.
	full.Added = append(full.Added, mirroredWireFee("posted-retirement", "account-retirement", false))
	applyFinanceSync(t, database, fixture, full, "2026-09-16")
	// A SimpleFIN source of the same person with the brokerage fee, which
	// is not looked at either.
	var simpleFIN financeFixture
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: fixture.agentId, Kind: models.SourceWeb, Name: "another credential",
			Specification: models.AgentKnowledgeSpecification{Start: "https://example.net/"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		simpleFIN = financeFixture{agentId: fixture.agentId, sourceId: source.ID}
	})
	setFinanceSourceType(t, database, simpleFIN.sourceId, string(finance.ProviderKindSimpleFIN))
	simpleFINSync := mirroredBrokerageSync()
	for index := range simpleFINSync.Added {
		simpleFINSync.Added[index].ProviderTransactionID = "simplefin-" + simpleFINSync.Added[index].ProviderTransactionID
	}
	applyFinanceSync(t, database, simpleFIN, simpleFINSync, "2026-09-16")
	var detected map[string]string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		detected = duplicateOfByProviderId(t, tx, fixture.agentId)
	})
	if len(detected) != 4 || detected["pending-individual"] != "posted-retirement" {
		t.Fatalf("the data set does not say what it was meant to: %v", detected)
	}

	var migration migrations.Migration
	for _, candidate := range migrations.Migrations() {
		if candidate.ID == "0144_agent_finance_transaction_duplicate" {
			migration = candidate
		}
	}
	if migration.SQL == "" || migration.ReverseSQL == "" {
		t.Fatal("the migration 0144_agent_finance_transaction_duplicate is missing")
	}
	dbtest.Exec(t, database, migration.ReverseSQL)
	columns := rawQueryString(t, database, `SELECT COUNT(*)::text FROM "information_schema"."columns"
		WHERE "table_name" = 'agent_finance_transaction' AND "column_name" LIKE 'duplicate%'`)
	if columns != "0" {
		t.Fatalf("the reverse leaves %s duplicate columns", columns)
	}
	dbtest.Exec(t, database, migration.SQL)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		migrated := duplicateOfByProviderId(t, tx, fixture.agentId)
		if fmt.Sprint(migrated) != fmt.Sprint(detected) {
			t.Errorf("the migration marks what detection marked:\nmigration %v\ndetection %v", migrated, detected)
		}
		if changed, err := tx.DetectMirroredFinanceTransactions(fixture.agentId, ""); err != nil || changed != 0 {
			t.Errorf("detection after the migration changes nothing: %d %v", changed, err)
		}
	})
}
