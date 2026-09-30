package db_test

import (
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

func spendingCategoryIdsByName(t *testing.T, tx db.Transaction, agentId string) map[string]string {
	t.Helper()
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		t.Fatalf("ListSpendingCategories: %s", err)
	}
	byName := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		byName[spendingCategory.SpendingCategoryName] = spendingCategory.ID
	}
	return byName
}

// The defaults arrive once; a person who deleted one does not get it back.
func TestEnsureDefaultSpendingCategoriesOnce(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "spending-defaults")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		createdCount, err := tx.EnsureDefaultSpendingCategories(fixture.agentId)
		if err != nil || createdCount != len(finance.DefaultSpendingCategoryNames) {
			t.Fatalf("EnsureDefaultSpendingCategories: %v %d", err, createdCount)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		income, err := tx.GetSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryIncome])
		if err != nil || income == nil || !income.IsIncome {
			t.Errorf("income is an income category: %v %+v", err, income)
		}
		if err := tx.DeleteSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryFees]); err != nil {
			t.Fatalf("DeleteSpendingCategory: %s", err)
		}
		if again, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil || again != 0 {
			t.Errorf("a second call makes nothing: %v %d", err, again)
		}
		if _, isBack := spendingCategoryIdsByName(t, tx, fixture.agentId)[finance.SpendingCategoryFees]; isBack {
			t.Error("a deleted default must stay deleted")
		}
		if _, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "dining"}); !errors.Is(err, db.ErrAlreadyExists) {
			t.Errorf("a second spending category of one name must be refused: %v", err)
		}
		child, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "coffee",
			ParentSpendingCategoryID: byName[finance.SpendingCategoryDining]})
		if err != nil {
			t.Fatalf("a child spending category: %s", err)
		}
		if _, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "espresso",
			ParentSpendingCategoryID: child.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("one level of parents only: %v", err)
		}
		renamed, err := tx.UpdateSpendingCategory(fixture.agentId, child.ID, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.SpendingCategoryName = "cafes"
			return nil
		})
		if err != nil || renamed.SpendingCategoryName != "cafes" || renamed.ParentSpendingCategoryID == "" {
			t.Errorf("UpdateSpendingCategory: %v %+v", err, renamed)
		}
	})
}

// The first rule by priority wins; reapplying skips what the person chose;
// a rule's spending category goes when no rule matches any more; a
// transfer rule marks transfers.
func TestSpendingRulesApplyByPriority(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "spending-rules")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "grocer-two", ProviderAccountID: "account-card", PostedOn: "2026-09-11",
			Amount: "-120.00", CurrencyCode: "USD", Description: "CORNER GROCER 0412", MerchantName: "Corner Grocer"},
		finance.Transaction{ProviderTransactionID: "card-payment", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "-300", CurrencyCode: "USD", Description: "AUTOPAY TRAVEL CARD"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["transaction-grocer"].ID, byName[finance.SpendingCategoryDining],
			models.CategorizedByPerson, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}

		broad, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "grocer",
			SpendingCategoryID: byName[finance.SpendingCategoryGroceries], RulePriority: 20})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		large, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "GROCER",
			SpendingCategoryID: byName[finance.SpendingCategoryShopping], MaximumAmount: "-100", RulePriority: 10})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "autopay", IsTransfer: true, RulePriority: 30}); err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "nothing to assign"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a rule that assigns nothing must be refused: %v", err)
		}

		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		if grocer := found["transaction-grocer"]; grocer.CategorizedBy != models.CategorizedByPerson || grocer.SpendingCategoryID != byName[finance.SpendingCategoryDining] {
			t.Errorf("a rule never overwrites the person: %+v", grocer)
		}
		if grocerTwo := found["grocer-two"]; grocerTwo.CategorizedBy != models.CategorizedBySpendingRule || grocerTwo.SpendingCategoryID != byName[finance.SpendingCategoryShopping] {
			t.Errorf("the rule with the lower priority number wins: %+v", grocerTwo)
		}
		if payment := found["card-payment"]; !payment.IsTransfer || payment.SpendingCategoryID != "" {
			t.Errorf("a transfer rule marks a transfer: %+v", payment)
		}

		if err := tx.DeleteSpendingRule(fixture.agentId, large.ID); err != nil {
			t.Fatalf("DeleteSpendingRule: %s", err)
		}
		if grocerTwo := financeTransactionsByProviderId(t, tx, fixture.agentId)["grocer-two"]; grocerTwo.SpendingCategoryID != byName[finance.SpendingCategoryGroceries] {
			t.Errorf("with the first rule gone the next one applies: %+v", grocerTwo)
		}
		if _, err := tx.UpdateSpendingRule(fixture.agentId, broad.ID, func(spendingRule *models.SpendingRule) error {
			spendingRule.MatchText = "no such merchant"
			return nil
		}); err != nil {
			t.Fatalf("UpdateSpendingRule: %s", err)
		}
		if grocerTwo := financeTransactionsByProviderId(t, tx, fixture.agentId)["grocer-two"]; grocerTwo.SpendingCategoryID != "" || grocerTwo.CategorizedBy != "" {
			t.Errorf("a rule's spending category goes when no rule matches: %+v", grocerTwo)
		}
		if changedCount, err := tx.ApplySpendingRules(fixture.agentId); err != nil || changedCount != 0 {
			t.Errorf("applying again changes nothing: %v %d", err, changedCount)
		}
		rules, err := tx.ListSpendingRules(fixture.agentId)
		if err != nil || len(rules) != 2 || rules[0].RulePriority != 20 {
			t.Errorf("ListSpendingRules by priority: %v %+v", err, rules)
		}
	})
}

// The budget in force in a month is the latest set from that month or
// before, and a zero ends it.
func TestBudgetsForMonthTakeTheLatestInForce(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "budget-month")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		dining, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "dining"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		travel, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "travel"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		for _, budget := range []models.Budget{
			{SpendingCategoryID: dining.ID, MonthlyAmount: "400", EffectiveFrom: "2026-01"},
			{SpendingCategoryID: dining.ID, MonthlyAmount: "450", EffectiveFrom: "2026-04"},
			{SpendingCategoryID: dining.ID, MonthlyAmount: "500", EffectiveFrom: "2026-04-15"},
			{SpendingCategoryID: dining.ID, MonthlyAmount: "0", EffectiveFrom: "2026-07"},
			{SpendingCategoryID: travel.ID, MonthlyAmount: "250.5", EffectiveFrom: "2026-03"},
		} {
			budget.AgentID, budget.CurrencyCode = fixture.agentId, "USD"
			if _, err := tx.SetBudget(&budget); err != nil {
				t.Fatalf("SetBudget %+v: %s", budget, err)
			}
		}
		for month, expected := range map[string]map[string]string{
			"2025-12": {},
			"2026-02": {dining.ID: "400.0000"},
			"2026-03": {dining.ID: "400.0000", travel.ID: "250.5000"},
			"2026-05": {dining.ID: "500.0000", travel.ID: "250.5000"},
			"2026-08": {travel.ID: "250.5000"},
		} {
			budgets, err := tx.BudgetsForMonth(fixture.agentId, month)
			if err != nil {
				t.Fatalf("BudgetsForMonth %s: %s", month, err)
			}
			if len(budgets) != len(expected) {
				t.Errorf("%s: got %d budgets, want %d: %+v", month, len(budgets), len(expected), budgets)
			}
			for _, budget := range budgets {
				if expected[budget.SpendingCategoryID] != budget.MonthlyAmount {
					t.Errorf("%s: %s is %s, want %s", month, budget.SpendingCategoryID, budget.MonthlyAmount, expected[budget.SpendingCategoryID])
				}
			}
		}
		all, err := tx.ListBudgets(fixture.agentId)
		if err != nil || len(all) != 4 {
			t.Errorf("setting the same month twice replaces it: %v %d", err, len(all))
		}
		if _, err := tx.SetBudget(&models.Budget{AgentID: fixture.agentId, SpendingCategoryID: dining.ID, MonthlyAmount: "-5",
			CurrencyCode: "USD", EffectiveFrom: "2026-09"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a negative budget must be refused: %v", err)
		}
	})
}

// Spending per day counts refunds against their spending category and
// leaves out transfers and income; the merchant months are the three full
// months before.
func TestSpendingCategoryDaysAndMerchantMonths(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "spending-days")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "grocer-refund", ProviderAccountID: "account-checking", PostedOn: "2026-09-10",
			Amount: "2.17", CurrencyCode: "USD", Description: "CORNER GROCER", MerchantName: "Corner Grocer"},
		finance.Transaction{ProviderTransactionID: "rent-june", ProviderAccountID: "account-checking", PostedOn: "2026-06-01",
			Amount: "-1500", CurrencyCode: "USD", Description: "RENT", MerchantName: "Maple Lettings"},
		finance.Transaction{ProviderTransactionID: "rent-july", ProviderAccountID: "account-checking", PostedOn: "2026-07-01",
			Amount: "-1500", CurrencyCode: "USD", Description: "RENT", MerchantName: "Maple Lettings"},
		finance.Transaction{ProviderTransactionID: "rent-august", ProviderAccountID: "account-checking", PostedOn: "2026-08-01",
			Amount: "-1500", CurrencyCode: "USD", Description: "RENT", MerchantName: "Maple Lettings"},
		finance.Transaction{ProviderTransactionID: "rent-may", ProviderAccountID: "account-checking", PostedOn: "2026-05-01",
			Amount: "-1500", CurrencyCode: "USD", Description: "RENT", MerchantName: "Maple Lettings"},
		finance.Transaction{ProviderTransactionID: "to-savings", ProviderAccountID: "account-checking", PostedOn: "2026-09-10",
			Amount: "-100", CurrencyCode: "USD", Description: "AUTOPAY SAVINGS"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		for _, spendingRule := range []models.SpendingRule{
			{MatchText: "corner grocer", SpendingCategoryID: byName[finance.SpendingCategoryGroceries]},
			{MatchText: "maple lettings", SpendingCategoryID: byName[finance.SpendingCategoryHousing]},
			{MatchText: "payroll", SpendingCategoryID: byName[finance.SpendingCategoryIncome]},
			{MatchText: "autopay", IsTransfer: true},
		} {
			spendingRule.AgentID = fixture.agentId
			if _, err := tx.CreateSpendingRule(&spendingRule); err != nil {
				t.Fatalf("CreateSpendingRule: %s", err)
			}
		}

		days, err := tx.ListSpendingCategoryDays(fixture.agentId, "2026-09")
		if err != nil {
			t.Fatalf("ListSpendingCategoryDays: %s", err)
		}
		bySpendingCategoryAndDay := map[string]string{}
		for _, day := range days {
			bySpendingCategoryAndDay[day.SpendingCategoryID+"/"+day.SpentOn] = day.SpendingAmount
		}
		expected := map[string]string{
			byName[finance.SpendingCategoryGroceries] + "/2026-09-10": "40.0000",
			"/2026-09-12": "18.4000",
		}
		if len(bySpendingCategoryAndDay) != len(expected) {
			t.Errorf("income and transfers are left out: %v", bySpendingCategoryAndDay)
		}
		for key, amount := range expected {
			if bySpendingCategoryAndDay[key] != amount {
				t.Errorf("%s: got %q, want %q (all: %v)", key, bySpendingCategoryAndDay[key], amount, bySpendingCategoryAndDay)
			}
		}

		// Cash flow counts the month the same way: the refund lowers the
		// groceries spending rather than counting as income, the payroll
		// is income, and the transfer is neither.
		cashFlowDays, err := tx.ListCashFlowDays(fixture.agentId, "2026-09-01", "2026-09-30")
		if err != nil {
			t.Fatalf("ListCashFlowDays: %s", err)
		}
		incomeTotal, spendingTotal := new(big.Rat), new(big.Rat)
		for _, day := range cashFlowDays {
			income, _ := finance.ParseAmount(day.IncomeAmount)
			spending, _ := finance.ParseAmount(day.SpendingAmount)
			incomeTotal.Add(incomeTotal, income)
			spendingTotal.Add(spendingTotal, spending)
		}
		if finance.FormatAmount(incomeTotal) != "2500.0000" || finance.FormatAmount(spendingTotal) != "58.4000" {
			t.Errorf("September's cash flow is income %s and spending %s, want 2500.0000 and 58.4000 (days %+v)",
				finance.FormatAmount(incomeTotal), finance.FormatAmount(spendingTotal), cashFlowDays)
		}

		months, err := tx.ListMerchantMonthSpending(fixture.agentId, "2026-09")
		if err != nil {
			t.Fatalf("ListMerchantMonthSpending: %s", err)
		}
		if len(months) != 3 {
			t.Fatalf("rent in each of the three full months before, May left out: %+v", months)
		}
		for index, month := range []string{"2026-06", "2026-07", "2026-08"} {
			if months[index].SpendingMonth != month || months[index].MerchantName != "Maple Lettings" || months[index].SpendingAmount != "1500.0000" ||
				months[index].SpendingCategoryID != byName[finance.SpendingCategoryHousing] {
				t.Errorf("month %d: %+v", index, months[index])
			}
		}
	})
}

// A savings target keeps the assets it measures, and replaces them when
// changed.
func TestSavingsTargetsKeepTheirAssets(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "savings-target")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		first, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "savings", AssetKind: models.AssetKindCash, CurrencyCode: "USD"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		second, err := tx.CreateAsset(&models.Asset{AgentID: fixture.agentId, AssetName: "brokerage", AssetKind: models.AssetKindInvestment, CurrencyCode: "USD"})
		if err != nil {
			t.Fatalf("CreateAsset: %s", err)
		}
		created, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: fixture.agentId, SavingsTargetName: "house deposit",
			TargetAmount: "40000", CurrencyCode: "USD", TargetOn: "2028-06-01", TargetMeasure: models.TargetMeasureAssetValue,
			StartingAmount: "12000", StartedOn: "2026-09-01", AssetIDs: []string{first.ID, first.ID}})
		if err != nil {
			t.Fatalf("CreateSavingsTarget: %s", err)
		}
		if len(created.AssetIDs) != 1 || created.TargetAmount != "40000.0000" || created.StartingAmount != "12000.0000" {
			t.Errorf("CreateSavingsTarget: %+v", created)
		}
		updated, err := tx.UpdateSavingsTarget(fixture.agentId, created.ID, func(savingsTarget *models.SavingsTarget) error {
			savingsTarget.AssetIDs = []string{second.ID}
			return nil
		})
		if err != nil || len(updated.AssetIDs) != 1 || updated.AssetIDs[0] != second.ID {
			t.Errorf("UpdateSavingsTarget replaces the assets: %v %+v", err, updated)
		}
		closed, err := tx.CloseSavingsTarget(fixture.agentId, created.ID, "2027-01-01")
		if err != nil || closed.ClosedOn != "2027-01-01" {
			t.Errorf("CloseSavingsTarget: %v %+v", err, closed)
		}
		cashFlow, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: fixture.agentId, SavingsTargetName: "holiday",
			TargetAmount: "3000", CurrencyCode: "USD", TargetOn: "2027-06-01", TargetMeasure: models.TargetMeasureCashFlow, StartedOn: "2026-09-01"})
		if err != nil {
			t.Fatalf("CreateSavingsTarget: %s", err)
		}
		listed, err := tx.ListSavingsTargets(fixture.agentId)
		if err != nil || len(listed) != 2 || listed[0].ID != cashFlow.ID {
			t.Errorf("open targets first: %v %+v", err, listed)
		}
		if err := tx.DeleteSavingsTarget(fixture.agentId, created.ID); err != nil {
			t.Fatalf("DeleteSavingsTarget: %s", err)
		}
		if _, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: fixture.agentId, SavingsTargetName: "bad", TargetAmount: "1",
			CurrencyCode: "USD", TargetOn: "2027-06-01", TargetMeasure: "vibes", StartedOn: "2026-09-01"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown measure must be refused: %v", err)
		}
	})
}

// A budget crossing is written once: its key is found among candidates
// whatever became of them, and among alerts as their subject key.
func TestBudgetAlertKeyIsWrittenOnce(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "budget-alert")
	stranger := createFinanceFixture(t, database, "budget-alert-stranger")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		budgetKey := "spending-category:example:2026-09:at_risk"
		if isFound, err := tx.HasAgentBudgetAlert(fixture.agentId, budgetKey); err != nil || isFound {
			t.Fatalf("nothing yet: %v %v", err, isFound)
		}
		if _, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{AgentID: fixture.agentId, CandidateKind: models.AlertCandidateBudget}); err == nil {
			t.Error("a budget candidate without a key must be refused")
		}
		candidate, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{AgentID: fixture.agentId, CandidateKind: models.AlertCandidateBudget,
			AlertSignal: models.AlertSignalSoon, CandidateReason: "dining at 85 percent", BudgetKey: budgetKey})
		if err != nil || candidate.BudgetKey != budgetKey {
			t.Fatalf("CreateAgentAlertCandidate: %v %+v", err, candidate)
		}
		if isFound, err := tx.HasAgentBudgetAlert(fixture.agentId, budgetKey); err != nil || !isFound {
			t.Errorf("the candidate's key is found: %v %v", err, isFound)
		}
		if isFound, err := tx.HasAgentBudgetAlert(stranger.agentId, budgetKey); err != nil || isFound {
			t.Errorf("another agent's key is not: %v %v", err, isFound)
		}
		sentKey := "savings-target:example:2026-09:behind"
		if _, err := tx.CreateAgentAlert(&models.AgentAlert{AgentID: fixture.agentId, SubjectKey: sentKey, AlertText: "behind"}); err != nil {
			t.Fatalf("CreateAgentAlert: %s", err)
		}
		if isFound, err := tx.HasAgentBudgetAlert(fixture.agentId, sentKey); err != nil || !isFound {
			t.Errorf("a sent alert's subject key is found: %v %v", err, isFound)
		}
	})
}

// A transfer a spending rule marked is cleared when no rule matches it any
// more. A transfer something else marked first is not the rule's to
// clear, and a person's decision is never touched.
func TestSpendingRuleClearsOnlyTheTransfersItMarked(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "spending-rule-transfer")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "autopay-rule", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "-300", CurrencyCode: "USD", Description: "AUTOPAY TRAVEL CARD"},
		finance.Transaction{ProviderTransactionID: "autopay-detected", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "-120", CurrencyCode: "USD", Description: "AUTOPAY STORE CARD"},
		finance.Transaction{ProviderTransactionID: "autopay-person", ProviderAccountID: "account-checking", PostedOn: "2026-09-11",
			Amount: "-60", CurrencyCode: "USD", Description: "AUTOPAY GYM"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["autopay-detected"].ID, true, models.TransferMarkedByDetection); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
		if _, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["autopay-person"].ID, false, models.TransferMarkedByPerson); err != nil {
			t.Fatalf("MarkFinanceTransactionTransfer: %s", err)
		}
		rule, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "autopay", IsTransfer: true, RulePriority: 10})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		if marked := found["autopay-rule"]; !marked.IsTransfer || marked.TransferMarkedBy != models.TransferMarkedBySpendingRule {
			t.Errorf("the rule marks a transfer and says so: %+v", marked)
		}
		if detected := found["autopay-detected"]; !detected.IsTransfer || detected.TransferMarkedBy != models.TransferMarkedByDetection {
			t.Errorf("the rule does not take over a transfer detection marked: %+v", detected)
		}
		if decided := found["autopay-person"]; decided.IsTransfer || decided.TransferMarkedBy != models.TransferMarkedByPerson {
			t.Errorf("the rule does not override the person: %+v", decided)
		}

		if err := tx.DeleteSpendingRule(fixture.agentId, rule.ID); err != nil {
			t.Fatalf("DeleteSpendingRule: %s", err)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		if cleared := found["autopay-rule"]; cleared.IsTransfer || cleared.TransferMarkedBy != "" {
			t.Errorf("with the rule gone its transfer is cleared: %+v", cleared)
		}
		if detected := found["autopay-detected"]; !detected.IsTransfer || detected.TransferMarkedBy != models.TransferMarkedByDetection {
			t.Errorf("a transfer detection marked outlives the rule: %+v", detected)
		}
		if decided := found["autopay-person"]; decided.IsTransfer || decided.TransferMarkedBy != models.TransferMarkedByPerson {
			t.Errorf("the person's decision outlives the rule: %+v", decided)
		}
		if isCleared, err := tx.MarkFinanceTransactionTransfer(fixture.agentId, found["autopay-detected"].ID, false, models.TransferMarkedBySpendingRule); err != nil || isCleared {
			t.Errorf("nothing but what marked a transfer clears it: %v %v", err, isCleared)
		}
	})
}

// Two syncs of one person that find the same budget crossing at the same
// time write one candidate between them.
func TestBudgetAlertKeyIsWrittenOnceUnderConcurrentSyncs(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "budget-alert-concurrent")
	budgetKey := "spending-category:example:2026-09:over"

	firstWritten := make(chan struct{})
	secondDone := make(chan error, 1)
	var createdCount atomic.Int32
	create := func(tx db.Transaction) error {
		candidate, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{AgentID: fixture.agentId, CandidateKind: models.AlertCandidateBudget,
			AlertSignal: models.AlertSignalSoon, CandidateReason: "dining is over its budget", BudgetKey: budgetKey})
		if err == nil && candidate != nil {
			createdCount.Add(1)
		}
		return err
	}
	err := database.Transaction(func(tx db.Transaction) error {
		isFound, err := tx.HasAgentBudgetAlert(fixture.agentId, budgetKey)
		if err != nil || isFound {
			return fmt.Errorf("nothing yet: %v %v", err, isFound)
		}
		if err := create(tx); err != nil {
			return err
		}
		close(firstWritten)
		// The second sync looked before this one commits, found nothing,
		// and writes while this transaction is still open.
		go func() {
			secondDone <- database.Transaction(func(tx db.Transaction) error { return create(tx) })
		}()
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("the first sync: %s", err)
	}
	<-firstWritten
	if err := <-secondDone; err != nil {
		t.Fatalf("the second sync must not fail on the crossing the first wrote: %s", err)
	}
	if createdCount.Load() != 1 {
		t.Errorf("one candidate written between them, got %d", createdCount.Load())
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		candidates, err := tx.ListWaitingAgentAlertCandidates(fixture.agentId, 10)
		if err != nil || len(candidates) != 1 {
			t.Errorf("one waiting candidate: %v %d", err, len(candidates))
		}
	})
}
