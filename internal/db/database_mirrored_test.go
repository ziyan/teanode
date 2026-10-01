package db_test

import (
	"errors"
	"fmt"
	"testing"

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
			// Pending on two accounts: not grouped until posted.
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

// One fee on three accounts of one connection is one counted copy and two
// duplicates of it; two charges on one account, a pending pair, and a
// different amount or day are none.
func TestMirroredFinanceTransactionsWithinOneSource(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "mirrored-one-source")
	result := mirroredBrokerageSync()
	applyFinanceSync(t, database, fixture, result, "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		duplicates := duplicateOfByProviderId(t, tx, fixture.agentId)
		if len(duplicates) != 2 {
			t.Fatalf("two duplicates, of one counted copy: %v", duplicates)
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

	// The pending pair, posted, is grouped; the pending rows the provider
	// replaced are gone.
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
	fixture := createFinanceFixture(t, database, "mirrored-two-sources")
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
	fixture := createFinanceFixture(t, database, "mirrored-again")
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
	fixture := createFinanceFixture(t, database, "mirrored-person")
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
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); duplicates["fee-retirement"] != "fee-individual" || len(duplicates) != 1 {
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
	fixture := createFinanceFixture(t, database, "mirrored-totals")
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

// The statement source holds accounts from different institutions, whose
// same-day fees are separate charges, so it is never looked at.
func TestMirroredFinanceTransactionsLeaveTheStatementSourceAlone(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "mirrored-statement")
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_source" SET "specification" = jsonb_set("specification"::jsonb, '{type}', '"statement"')
		WHERE "id" = '%s'`, fixture.sourceId))
	applyFinanceSync(t, database, fixture, mirroredBrokerageSync(), "2026-09-16")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if duplicates := duplicateOfByProviderId(t, tx, fixture.agentId); len(duplicates) != 0 {
			t.Errorf("nothing in the statement source is a copy: %v", duplicates)
		}
	})
}

// Migration 0144 marks the copies already stored the way detection marks
// them after a sync, and its reverse takes the columns away again.
func TestMirroredMigrationMarksStoredCopies(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "mirrored-migration")
	first := mirroredBrokerageSync()
	first.Added = first.Added[:1]
	applyFinanceSync(t, database, fixture, first, "2026-09-15")
	applyFinanceSync(t, database, fixture, mirroredBrokerageSync(), "2026-09-16")
	var detected map[string]string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		detected = duplicateOfByProviderId(t, tx, fixture.agentId)
	})
	if len(detected) != 2 {
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
