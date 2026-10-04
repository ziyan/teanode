package db_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// otherMigrationId is the migration that made other a built-in spending
// category.
const otherMigrationId = "0147_agent_spending_category_other"

func otherMigration(t *testing.T) migrations.Migration {
	t.Helper()
	for _, migration := range migrations.Migrations() {
		if migration.ID == otherMigrationId {
			return migration
		}
	}
	t.Fatalf("the migration %s is missing", otherMigrationId)
	return migrations.Migration{}
}

// otherMigrationTotals is what the totals read for an agent, keyed so two
// readings can be compared amount by amount: each day's cash flow income
// and spending ("cash flow income/<day>"), the spending per spending
// category and day ("spending/<category>/<day>", "" for what has none),
// each merchant's spending per spending category in the three months
// before October ("merchant/<category>/<merchant>") and the spending
// summary by merchant ("summary/<merchant>/out" and "/in").
func otherMigrationTotals(t *testing.T, tx db.Transaction, agentId string) map[string]*big.Rat {
	t.Helper()
	totals := map[string]*big.Rat{}
	add := func(key, amount string) {
		value, isParsed := new(big.Rat).SetString(amount)
		if !isParsed {
			t.Fatalf("%s: %q is not an amount", key, amount)
		}
		if totals[key] == nil {
			totals[key] = new(big.Rat)
		}
		totals[key].Add(totals[key], value)
	}
	cashFlowDays, err := tx.ListCashFlowDays(agentId, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("ListCashFlowDays: %s", err)
	}
	for _, day := range cashFlowDays {
		add("cash flow income/"+day.CashFlowOn, day.IncomeAmount)
		add("cash flow spending/"+day.CashFlowOn, day.SpendingAmount)
	}
	for _, month := range []string{"2026-08", "2026-09"} {
		spendingDays, err := tx.ListSpendingCategoryDays(agentId, month)
		if err != nil {
			t.Fatalf("ListSpendingCategoryDays: %s", err)
		}
		for _, day := range spendingDays {
			add("spending/"+day.SpendingCategoryID+"/"+day.SpentOn, day.SpendingAmount)
		}
		incomeDays, err := tx.ListIncomeCategoryDays(agentId, month)
		if err != nil {
			t.Fatalf("ListIncomeCategoryDays: %s", err)
		}
		for _, day := range incomeDays {
			add("income/"+day.SpendingCategoryID+"/"+day.ReceivedOn, day.IncomeAmount)
		}
	}
	merchantMonths, err := tx.ListMerchantMonthSpending(agentId, "2026-10")
	if err != nil {
		t.Fatalf("ListMerchantMonthSpending: %s", err)
	}
	for _, row := range merchantMonths {
		add("merchant/"+row.SpendingCategoryID+"/"+row.MerchantName, row.SpendingAmount)
	}
	summaryRows, err := tx.FinanceSpendingSummary(agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByMerchant})
	if err != nil {
		t.Fatalf("FinanceSpendingSummary: %s", err)
	}
	for _, row := range summaryRows {
		add("summary/"+row.GroupKey+"/out", row.MoneyOut)
		add("summary/"+row.GroupKey+"/in", row.MoneyIn)
	}
	return totals
}

// compareTotals says where two readings differ from what was expected:
// before plus the changes given, key by key, a key that ends at zero being
// the same as one that is not there.
func compareTotals(t *testing.T, label string, before, after map[string]*big.Rat, changes map[string]string) {
	t.Helper()
	expected := map[string]*big.Rat{}
	for key, value := range before {
		expected[key] = new(big.Rat).Set(value)
	}
	for key, change := range changes {
		value, isParsed := new(big.Rat).SetString(change)
		if !isParsed {
			t.Fatalf("%s: %q is not an amount", key, change)
		}
		if expected[key] == nil {
			expected[key] = new(big.Rat)
		}
		expected[key].Add(expected[key], value)
	}
	keys := map[string]bool{}
	for key := range expected {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	zero := new(big.Rat)
	for key := range keys {
		want, got := expected[key], after[key]
		if want == nil {
			want = zero
		}
		if got == nil {
			got = zero
		}
		if want.Cmp(got) != 0 {
			t.Errorf("%s: %s is %s, want %s", label, key, got.FloatString(4), want.FloatString(4))
		}
	}
}

// Migration 0147 makes each agent's default other the built-in one, makes
// one where there is none (named around a person's own other that cannot
// become it), and moves the person's choice of no spending category into
// it, leaving what is not decided yet alone. The totals change only by
// what that move has to change: money out the person filed under nothing
// was spending and stays spending, now under other; money in the person
// filed under nothing was income and stays income, since other counts
// money in the way no spending category did. The reverse keeps the
// category and the moved transactions.
func TestOtherMigrationMarksMovesAndKeepsTheTotals(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	transaction := func(providerTransactionId, postedOn, amount, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: "account-checking", PostedOn: postedOn,
			Amount: amount, CurrencyCode: "USD", Description: description}
	}

	// The default other, with the person's "fits nothing" both ways, one
	// still waiting, and one they filed under other themselves.
	kept := createFinanceFixture(t, database, "other-migration-default")
	keptSync := sampleFinanceSync()
	keptSync.Added = append(keptSync.Added,
		transaction("fits-nothing-out", "2026-09-20", "-12.50", "STALL WITH NO SIGN"),
		transaction("fits-nothing-in", "2026-09-21", "7", "MYSTERY CREDIT"),
		transaction("waiting", "2026-09-22", "-40", "NOT DECIDED YET"),
		transaction("already-other", "2026-09-23", "-9", "ODD PURCHASE"),
	)
	applyFinanceSync(t, database, kept, keptSync, "2026-09-24")
	// The default other renamed, and a "fits nothing".
	renamed := createFinanceFixture(t, database, "other-migration-renamed")
	renamedSync := sampleFinanceSync()
	renamedSync.Added = append(renamedSync.Added, transaction("fits-nothing-out", "2026-09-20", "-30", "STALL WITH NO SIGN"))
	applyFinanceSync(t, database, renamed, renamedSync, "2026-09-24")
	// An other with a child of its own, which cannot be the built-in one.
	parent := createFinanceFixture(t, database, "other-migration-parent")
	for _, fixture := range []financeFixture{kept, renamed, parent} {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
				t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
			}
		})
	}
	var keptByName, renamedByName, parentByName map[string]string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		keptByName = spendingCategoryIdsByName(t, tx, kept.agentId)
		renamedByName = spendingCategoryIdsByName(t, tx, renamed.agentId)
		parentByName = spendingCategoryIdsByName(t, tx, parent.agentId)
	})

	// Back to 0145, and the rows as that schema kept them.
	migration := otherMigration(t)
	execMigrationSQL(t, database, migration.ReverseSQL)
	categorize := func(fixture financeFixture, providerTransactionId, spendingCategoryId, categorizedBy string) {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_finance_transaction" SET "spending_category_id" = NULLIF('%s', ''), "categorized_by" = '%s'
			WHERE "agent_id" = '%s' AND "provider_transaction_id" = '%s'`, spendingCategoryId, categorizedBy, fixture.agentId, providerTransactionId))
	}
	categorize(kept, "transaction-grocer", keptByName[finance.SpendingCategoryGroceries], "spending_rule")
	categorize(kept, "transaction-salary", keptByName[finance.SpendingCategoryIncome], "provider_category_mapping")
	categorize(kept, "fits-nothing-out", "", "person")
	categorize(kept, "fits-nothing-in", "", "person")
	categorize(kept, "already-other", keptByName[finance.SpendingCategoryOther], "person")
	categorize(renamed, "fits-nothing-out", "", "person")
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_spending_category" SET "spending_category_name" = 'misc' WHERE "id" = '%s'`,
		renamedByName[finance.SpendingCategoryOther]))
	dbtest.Exec(t, database, fmt.Sprintf(`INSERT INTO "agent_spending_category" ("id", "agent_id", "spending_category_name", "parent_spending_category_id", "created_at", "modified_at")
		VALUES ('otherchild', '%s', 'odds and ends', '%s', now(), now())`, parent.agentId, parentByName[finance.SpendingCategoryOther]))

	// The totals read the flag, which that schema has not got; a flag no
	// category carries reads the old schema's rows the way it did.
	var keptBefore, renamedBefore map[string]*big.Rat
	dbtest.Exec(t, database, `ALTER TABLE "agent_spending_category" ADD COLUMN "is_other" boolean NOT NULL DEFAULT false`)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		keptBefore = otherMigrationTotals(t, tx, kept.agentId)
		renamedBefore = otherMigrationTotals(t, tx, renamed.agentId)
	})
	dbtest.Exec(t, database, `ALTER TABLE "agent_spending_category" DROP COLUMN "is_other"`)
	if keptBefore["cash flow income/2026-09-21"] == nil || keptBefore["spending//2026-09-20"] == nil {
		t.Fatalf("the data set does not say what it was meant to: %v", keptBefore)
	}

	execMigrationSQL(t, database, migration.SQL)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		otherOf := func(fixture financeFixture) *models.SpendingCategory {
			spendingCategories, err := tx.ListSpendingCategories(fixture.agentId)
			if err != nil {
				t.Fatalf("ListSpendingCategories: %s", err)
			}
			var found *models.SpendingCategory
			for _, spendingCategory := range spendingCategories {
				if spendingCategory.IsOther {
					if found != nil {
						t.Errorf("two other categories: %+v %+v", found, spendingCategory)
					}
					found = spendingCategory
				}
			}
			if found == nil {
				t.Fatalf("no other category for %s", fixture.agentId)
			}
			return found
		}
		keptOther, renamedOther, parentOther := otherOf(kept), otherOf(renamed), otherOf(parent)
		if keptOther.ID != keptByName[finance.SpendingCategoryOther] {
			t.Errorf("the default other is the built-in one: %+v", keptOther)
		}
		if renamedOther.ID == renamedByName[finance.SpendingCategoryOther] || renamedOther.SpendingCategoryName != finance.SpendingCategoryOther {
			t.Errorf("a renamed other stays the person's, beside a new built-in other: %+v", renamedOther)
		}
		if parentOther.ID == parentByName[finance.SpendingCategoryOther] || parentOther.SpendingCategoryName != finance.SpendingCategoryOtherFallback {
			t.Errorf("an other with children stays the person's, beside a built-in one named around it: %+v", parentOther)
		}

		keptFound := financeTransactionsByProviderId(t, tx, kept.agentId)
		for providerTransactionId, expected := range map[string]struct {
			spendingCategoryId string
			categorizedBy      models.CategorizedBy
		}{
			"fits-nothing-out":   {keptOther.ID, models.CategorizedByPerson},
			"fits-nothing-in":    {keptOther.ID, models.CategorizedByPerson},
			"already-other":      {keptOther.ID, models.CategorizedByPerson},
			"waiting":            {"", ""},
			"transaction-grocer": {keptByName[finance.SpendingCategoryGroceries], models.CategorizedBySpendingRule},
		} {
			financeTransaction := keptFound[providerTransactionId]
			if financeTransaction.SpendingCategoryID != expected.spendingCategoryId || financeTransaction.CategorizedBy != expected.categorizedBy {
				t.Errorf("%s: %q by %q, want %q by %q", providerTransactionId, financeTransaction.SpendingCategoryID, financeTransaction.CategorizedBy,
					expected.spendingCategoryId, expected.categorizedBy)
			}
		}
		if moved := financeTransactionsByProviderId(t, tx, renamed.agentId)["fits-nothing-out"]; moved.SpendingCategoryID != renamedOther.ID {
			t.Errorf("the renamed agent's fits-nothing goes to its new other: %+v", moved)
		}

		compareTotals(t, "the default other", keptBefore, otherMigrationTotals(t, tx, kept.agentId), map[string]string{
			// Money in the person filed under nothing was income and stays
			// income, so cash flow does not move. Money out stays spending,
			// under other instead of none.
			"spending//2026-09-20":                     "-12.5",
			"spending/" + keptOther.ID + "/2026-09-20": "12.5",
			// And a repeat charge can now be found in other.
			"merchant/" + keptOther.ID + "/STALL WITH NO SIGN": "12.5",
		})
		compareTotals(t, "the renamed other", renamedBefore, otherMigrationTotals(t, tx, renamed.agentId), map[string]string{
			"spending//2026-09-20":                                "-30",
			"spending/" + renamedOther.ID + "/2026-09-20":         "30",
			"merchant/" + renamedOther.ID + "/STALL WITH NO SIGN": "30",
		})
	})

	// The reverse keeps the categories, as ordinary ones, and leaves the
	// moved transactions where they are.
	execMigrationSQL(t, database, migration.ReverseSQL)
	if column := rawQueryString(t, database, `SELECT COUNT(*)::text FROM information_schema.columns
		WHERE table_name = 'agent_spending_category' AND column_name = 'is_other'`); column != "0" {
		t.Errorf("the reverse drops the flag: %s", column)
	}
	if moved := rawQueryString(t, database, fmt.Sprintf(`SELECT "spending_category_id" FROM "agent_finance_transaction"
		WHERE "agent_id" = '%s' AND "provider_transaction_id" = 'fits-nothing-out'`, kept.agentId)); moved != keptByName[finance.SpendingCategoryOther] {
		t.Errorf("the reverse leaves the moved transactions in other: %s", moved)
	}
	if count := rawQueryString(t, database, fmt.Sprintf(`SELECT COUNT(*)::text FROM "agent_spending_category" WHERE "agent_id" = '%s'
		AND "spending_category_name" IN ('other', 'misc')`, renamed.agentId)); count != "2" {
		t.Errorf("the reverse keeps the other the migration made: %s", count)
	}

	// And it runs again on what the reverse left, the built-in one being
	// the one it made the first time.
	execMigrationSQL(t, database, migration.SQL)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, fixture := range []financeFixture{kept, renamed, parent} {
			if _, err := tx.EnsureOtherSpendingCategory(fixture.agentId); err != nil {
				t.Errorf("EnsureOtherSpendingCategory after running again: %s", err)
			}
		}
		spendingCategories, err := tx.ListSpendingCategories(parent.agentId)
		if err != nil {
			t.Fatalf("ListSpendingCategories: %s", err)
		}
		otherCount := 0
		for _, spendingCategory := range spendingCategories {
			if spendingCategory.IsOther {
				otherCount++
				if spendingCategory.SpendingCategoryName != finance.SpendingCategoryOtherFallback {
					t.Errorf("the one made the first time is the built-in one again: %+v", spendingCategory)
				}
			}
		}
		if otherCount != 1 || len(spendingCategories) != len(finance.DefaultSpendingCategoryNames)+3 {
			t.Errorf("running again makes no second one: %d of %d", otherCount, len(spendingCategories))
		}
	})
}
