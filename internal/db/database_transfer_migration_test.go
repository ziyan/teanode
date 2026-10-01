package db_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// transferMigrationId is the migration that turned the transfer mark into
// the transfer category.
const transferMigrationId = "0143_agent_spending_category_transfer"

func transferMigration(t *testing.T) migrations.Migration {
	t.Helper()
	for _, migration := range migrations.Migrations() {
		if migration.ID == transferMigrationId {
			return migration
		}
	}
	t.Fatalf("the migration %s is missing", transferMigrationId)
	return migrations.Migration{}
}

func rawQueryString(t *testing.T, database db.Database, query string) string {
	t.Helper()
	value, err := database.(interface {
		RawQueryString(string) (string, error)
	}).RawQueryString(query)
	if err != nil {
		t.Fatalf("query failed: %s", err)
	}
	return value
}

// The totals as the code before migration 0143 counted them, with the
// transfer mark: every day's cash flow, spending and income per spending
// category, merchants' monthly spending and the spending summary by
// merchant, each one line per row, sorted.
func transferMarkTotals(t *testing.T, database db.Database, agentId string) string {
	t.Helper()
	queries := map[string]string{
		"cash flow": `SELECT to_char("flowed"."posted_on", 'YYYY-MM-DD') || '/' || "flowed"."currency_code" || '/' ||
				SUM(CASE WHEN "spending_category"."id" IS NOT NULL AND "spending_category"."is_income" THEN "flowed"."amount"
				         WHEN "spending_category"."id" IS NULL AND "flowed"."amount" > 0 THEN "flowed"."amount" ELSE 0 END)::text || '/' ||
				SUM(CASE WHEN "spending_category"."id" IS NOT NULL AND NOT "spending_category"."is_income" THEN -"flowed"."amount"
				         WHEN "spending_category"."id" IS NULL AND "flowed"."amount" < 0 THEN -"flowed"."amount" ELSE 0 END)::text AS "line"
			FROM "agent_finance_transaction" AS "flowed"
			LEFT JOIN "agent_spending_category" AS "spending_category" ON "spending_category"."id" = "flowed"."spending_category_id"
			WHERE "flowed"."agent_id" = '%[1]s' AND NOT "flowed"."is_transfer"
			GROUP BY "flowed"."posted_on", "flowed"."currency_code"`,
		"spending": `SELECT COALESCE("spent"."spending_category_id", '') || '/' || to_char("spent"."posted_on", 'YYYY-MM-DD') || '/' ||
				"spent"."currency_code" || '/' || SUM(-"spent"."amount")::text AS "line"
			FROM "agent_finance_transaction" AS "spent"
			LEFT JOIN "agent_spending_category" AS "spending_category" ON "spending_category"."id" = "spent"."spending_category_id"
			WHERE "spent"."agent_id" = '%[1]s' AND NOT "spent"."is_transfer"
			  AND (("spending_category"."id" IS NULL AND "spent"."amount" < 0) OR ("spending_category"."id" IS NOT NULL AND NOT "spending_category"."is_income"))
			GROUP BY "spent"."spending_category_id", "spent"."posted_on", "spent"."currency_code"`,
		"income": `SELECT "received"."spending_category_id" || '/' || to_char("received"."posted_on", 'YYYY-MM-DD') || '/' ||
				"received"."currency_code" || '/' || SUM("received"."amount")::text AS "line"
			FROM "agent_finance_transaction" AS "received"
			JOIN "agent_spending_category" AS "spending_category" ON "spending_category"."id" = "received"."spending_category_id"
			WHERE "received"."agent_id" = '%[1]s' AND NOT "received"."is_transfer" AND "spending_category"."is_income"
			GROUP BY "received"."spending_category_id", "received"."posted_on", "received"."currency_code"`,
		"merchant months": `SELECT "spent"."spending_category_id" || '/' || COALESCE(NULLIF("spent"."merchant_name", ''), "spent"."description") || '/' ||
				to_char("spent"."posted_on", 'YYYY-MM') || '/' || "spent"."currency_code" || '/' || SUM(-"spent"."amount")::text || '/' || COUNT(*) AS "line"
			FROM "agent_finance_transaction" AS "spent"
			JOIN "agent_spending_category" AS "spending_category" ON "spending_category"."id" = "spent"."spending_category_id"
			WHERE "spent"."agent_id" = '%[1]s' AND NOT "spent"."is_transfer" AND "spent"."amount" < 0 AND NOT "spending_category"."is_income"
			  AND "spent"."posted_on" >= '2026-07-01' AND "spent"."posted_on" < '2026-10-01'
			GROUP BY "spent"."spending_category_id", COALESCE(NULLIF("spent"."merchant_name", ''), "spent"."description"),
				to_char("spent"."posted_on", 'YYYY-MM'), "spent"."currency_code"`,
		"summary": `SELECT COALESCE(NULLIF("summarized"."merchant_name", ''), "summarized"."description") || '/' || "summarized"."currency_code" || '/' ||
				COALESCE(SUM(-"summarized"."amount") FILTER (WHERE "summarized"."amount" < 0), 0::numeric(19,4))::text || '/' ||
				COALESCE(SUM("summarized"."amount") FILTER (WHERE "summarized"."amount" > 0), 0::numeric(19,4))::text || '/' || COUNT(*) AS "line"
			FROM "agent_finance_transaction" AS "summarized"
			WHERE "summarized"."agent_id" = '%[1]s' AND NOT "summarized"."is_transfer"
			GROUP BY COALESCE(NULLIF("summarized"."merchant_name", ''), "summarized"."description"), "summarized"."currency_code"`,
	}
	sections := []string{}
	for _, name := range []string{"cash flow", "spending", "income", "merchant months", "summary"} {
		lines := rawQueryString(t, database, `SELECT COALESCE(string_agg("line", E'\n' ORDER BY "line" COLLATE "C"), '') FROM (`+fmt.Sprintf(queries[name], agentId)+`) AS "rows"`)
		sections = append(sections, name+":\n"+lines)
	}
	return strings.Join(sections, "\n")
}

// The same totals as the code reads them now, from the transfer category.
func transferCategoryTotals(t *testing.T, tx db.Transaction, agentId string) string {
	t.Helper()
	sorted := func(lines []string) string {
		sort.Strings(lines)
		return strings.Join(lines, "\n")
	}
	cashFlowDays, err := tx.ListCashFlowDays(agentId, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatalf("ListCashFlowDays: %s", err)
	}
	cashFlow := []string{}
	for _, day := range cashFlowDays {
		cashFlow = append(cashFlow, day.CashFlowOn+"/"+day.CurrencyCode+"/"+day.IncomeAmount+"/"+day.SpendingAmount)
	}
	spending, income := []string{}, []string{}
	for _, month := range []string{"2026-08", "2026-09"} {
		spendingDays, err := tx.ListSpendingCategoryDays(agentId, month)
		if err != nil {
			t.Fatalf("ListSpendingCategoryDays: %s", err)
		}
		for _, day := range spendingDays {
			spending = append(spending, day.SpendingCategoryID+"/"+day.SpentOn+"/"+day.CurrencyCode+"/"+day.SpendingAmount)
		}
		incomeDays, err := tx.ListIncomeCategoryDays(agentId, month)
		if err != nil {
			t.Fatalf("ListIncomeCategoryDays: %s", err)
		}
		for _, day := range incomeDays {
			income = append(income, day.SpendingCategoryID+"/"+day.ReceivedOn+"/"+day.CurrencyCode+"/"+day.IncomeAmount)
		}
	}
	merchantMonths, err := tx.ListMerchantMonthSpending(agentId, "2026-10")
	if err != nil {
		t.Fatalf("ListMerchantMonthSpending: %s", err)
	}
	merchants := []string{}
	for _, row := range merchantMonths {
		merchants = append(merchants, fmt.Sprintf("%s/%s/%s/%s/%s/%d", row.SpendingCategoryID, row.MerchantName, row.SpendingMonth, row.CurrencyCode,
			row.SpendingAmount, row.FinanceTransactionCount))
	}
	summaryRows, err := tx.FinanceSpendingSummary(agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByMerchant})
	if err != nil {
		t.Fatalf("FinanceSpendingSummary: %s", err)
	}
	summary := []string{}
	for _, row := range summaryRows {
		summary = append(summary, fmt.Sprintf("%s/%s/%s/%s/%d", row.GroupKey, row.CurrencyCode, row.MoneyOut, row.MoneyIn, row.FinanceTransactionCount))
	}
	return "cash flow:\n" + sorted(cashFlow) + "\nspending:\n" + sorted(spending) + "\nincome:\n" + sorted(income) +
		"\nmerchant months:\n" + sorted(merchants) + "\nsummary:\n" + sorted(summary)
}

// Migration 0143 moves every transfer mark into the transfer category and
// every spending rule that marked transfers onto it, and the totals come
// out the same before and after: the transfers were left out by their
// mark and are left out by their category. Running the spending rules and
// transfer detection again afterwards changes nothing either, and the
// reverse brings the marks back.
func TestTransferMigrationKeepsTheTotals(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "transfer-migration")
	result := sampleFinanceSync()
	transaction := func(providerTransactionId, providerAccountId, postedOn, amount, description string) finance.Transaction {
		return finance.Transaction{ProviderTransactionID: providerTransactionId, ProviderAccountID: providerAccountId, PostedOn: postedOn,
			Amount: amount, CurrencyCode: "USD", Description: description}
	}
	result.Added = append(result.Added,
		// Paired by detection, the card's side once guessed as groceries.
		transaction("paired-out", "account-checking", "2026-09-03", "-500", "CARD PAYMENT"),
		transaction("paired-in", "account-card", "2026-09-04", "500", "PAYMENT THANK YOU"),
		// Marked by a rule, by the provider category mapping, by the person.
		transaction("rule-marked", "account-checking", "2026-09-06", "-200", "AUTOPAY SAVINGS"),
		transaction("both-rule", "account-checking", "2026-08-06", "-80", "BOTH WAYS"),
		transaction("mapping-marked", "account-checking", "2026-08-20", "-150", "TO BROKERAGE"),
		transaction("person-marked", "account-card", "2026-09-08", "-60", "SPLIT WITH ROOMMATE"),
		// The person said this one is not a transfer.
		transaction("person-unmarked", "account-checking", "2026-09-09", "-75", "VENMO"),
		transaction("person-unmarked-bare", "account-checking", "2026-09-09", "40", "VENMO"),
		// Ordinary spending, a refund and income in two months.
		transaction("grocer-august", "account-card", "2026-08-12", "-64.10", "CORNER GROCER"),
		transaction("grocer-refund", "account-card", "2026-09-13", "5.50", "CORNER GROCER"),
		transaction("payroll-august", "account-checking", "2026-08-01", "2400", "PAYROLL EXAMPLE CO"),
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-14")
	var byName map[string]string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName = spendingCategoryIdsByName(t, tx, fixture.agentId)
	})

	// Back to 0142, and the rows as that schema kept them.
	migration := transferMigration(t)
	dbtest.Exec(t, database, migration.ReverseSQL)
	categorize := func(providerTransactionId, spendingCategoryName, categorizedBy string) {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_finance_transaction" SET "spending_category_id" = '%s', "categorized_by" = '%s'
			WHERE "agent_id" = '%s' AND "provider_transaction_id" = '%s'`, byName[spendingCategoryName], categorizedBy, fixture.agentId, providerTransactionId))
	}
	mark := func(providerTransactionId string, isTransfer bool, transferMarkedBy string) {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_finance_transaction" SET "is_transfer" = %t, "transfer_marked_by" = '%s'
			WHERE "agent_id" = '%s' AND "provider_transaction_id" = '%s'`, isTransfer, transferMarkedBy, fixture.agentId, providerTransactionId))
	}
	categorize("transaction-grocer", finance.SpendingCategoryGroceries, "spending_rule")
	categorize("transaction-salary", finance.SpendingCategoryIncome, "provider_category_mapping")
	categorize("transaction-diner", finance.SpendingCategoryDining, "categorize_model")
	categorize("grocer-august", finance.SpendingCategoryGroceries, "spending_rule")
	categorize("grocer-refund", finance.SpendingCategoryGroceries, "spending_rule")
	categorize("payroll-august", finance.SpendingCategoryIncome, "provider_category_mapping")
	categorize("paired-in", finance.SpendingCategoryGroceries, "categorize_model")
	categorize("both-rule", finance.SpendingCategoryShopping, "spending_rule")
	categorize("person-marked", finance.SpendingCategoryDining, "person")
	categorize("person-unmarked", finance.SpendingCategoryGiftsAndDonations, "categorize_model")
	mark("paired-out", true, "detection")
	mark("paired-in", true, "detection")
	mark("rule-marked", true, "spending_rule")
	mark("both-rule", true, "spending_rule")
	mark("mapping-marked", true, "provider_category_mapping")
	mark("person-marked", true, "person")
	mark("person-unmarked", false, "person")
	mark("person-unmarked-bare", false, "person")
	dbtest.Exec(t, database, fmt.Sprintf(`INSERT INTO "agent_spending_rule" ("id", "agent_id", "match_text", "spending_category_id", "is_transfer", "rule_priority", "created_at", "modified_at")
		VALUES ('ruleautopay', '%[1]s', 'autopay', NULL, true, 0, now(), now()),
		       ('ruleboth', '%[1]s', 'both ways', '%[2]s', true, 1, now(), now()),
		       ('rulegrocer', '%[1]s', 'corner grocer', '%[3]s', false, 2, now(), now())`,
		fixture.agentId, byName[finance.SpendingCategoryShopping], byName[finance.SpendingCategoryGroceries]))

	before := transferMarkTotals(t, database, fixture.agentId)
	if !strings.Contains(before, "VENMO/USD/75.0000/40.0000/2") || strings.Contains(before, "CARD PAYMENT") {
		t.Fatalf("the data set does not say what it was meant to:\n%s", before)
	}

	dbtest.Exec(t, database, migration.SQL)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		transferCategory, err := tx.EnsureTransferSpendingCategory(fixture.agentId)
		if err != nil || transferCategory.SpendingCategoryName != finance.SpendingCategoryTransfer {
			t.Fatalf("the migration made the transfer category: %v %+v", err, transferCategory)
		}
		if after := transferCategoryTotals(t, tx, fixture.agentId); after != before {
			t.Errorf("the totals changed across the migration:\nbefore\n%s\nafter\n%s", before, after)
		}

		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for providerTransactionId, expected := range map[string]struct {
			spendingCategoryId string
			categorizedBy      models.CategorizedBy
		}{
			"paired-out":           {transferCategory.ID, models.CategorizedByTransferDetection},
			"paired-in":            {transferCategory.ID, models.CategorizedByTransferDetection},
			"rule-marked":          {transferCategory.ID, models.CategorizedBySpendingRule},
			"both-rule":            {transferCategory.ID, models.CategorizedBySpendingRule},
			"mapping-marked":       {transferCategory.ID, models.CategorizedByProviderCategoryMapping},
			"person-marked":        {transferCategory.ID, models.CategorizedByPerson},
			"person-unmarked":      {byName[finance.SpendingCategoryGiftsAndDonations], models.CategorizedByPerson},
			"person-unmarked-bare": {"", models.CategorizedByPerson},
			"transaction-diner":    {byName[finance.SpendingCategoryDining], models.CategorizedByCategorizeModel},
		} {
			financeTransaction := found[providerTransactionId]
			if financeTransaction.SpendingCategoryID != expected.spendingCategoryId || financeTransaction.CategorizedBy != expected.categorizedBy {
				t.Errorf("%s: %q by %q, want %q by %q", providerTransactionId, financeTransaction.SpendingCategoryID, financeTransaction.CategorizedBy,
					expected.spendingCategoryId, expected.categorizedBy)
			}
		}
		rules, err := tx.ListSpendingRules(fixture.agentId)
		if err != nil || len(rules) != 3 || rules[0].SpendingCategoryID != transferCategory.ID || rules[1].SpendingCategoryID != transferCategory.ID ||
			rules[2].SpendingCategoryID != byName[finance.SpendingCategoryGroceries] {
			t.Errorf("the rules that marked transfers assign the transfer category: %v %+v", err, rules)
		}

		// What runs after every sync finds nothing to undo.
		if _, err := tx.ApplySpendingRules(fixture.agentId); err != nil {
			t.Fatalf("ApplySpendingRules: %s", err)
		}
		if _, err := tx.DetectFinanceTransfers(fixture.agentId, "", "2026-01-01"); err != nil {
			t.Fatalf("DetectFinanceTransfers: %s", err)
		}
		if again := transferCategoryTotals(t, tx, fixture.agentId); again != before {
			t.Errorf("the totals changed once the rules and detection ran again:\nbefore\n%s\nafter\n%s", before, again)
		}
	})

	// The reverse brings the marks back, and the totals with them.
	dbtest.Exec(t, database, migration.ReverseSQL)
	if reversed := transferMarkTotals(t, database, fixture.agentId); reversed != before {
		t.Errorf("the totals changed across the reverse:\nbefore\n%s\nafter\n%s", before, reversed)
	}
	marks := rawQueryString(t, database, fmt.Sprintf(`SELECT string_agg("provider_transaction_id" || '=' || "transfer_marked_by", ',' ORDER BY "provider_transaction_id")
		FROM "agent_finance_transaction" WHERE "agent_id" = '%s' AND "is_transfer"`, fixture.agentId))
	if marks != "both-rule=spending_rule,mapping-marked=provider_category_mapping,paired-in=detection,paired-out=detection,person-marked=person,rule-marked=spending_rule" {
		t.Errorf("the marks after the reverse: %s", marks)
	}
	dbtest.Exec(t, database, migration.SQL)
}
