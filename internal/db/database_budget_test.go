package db_test

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
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
		// Other is built in, made with the agent, and not counted.
		createdCount, err := tx.EnsureDefaultSpendingCategories(fixture.agentId)
		if err != nil || createdCount != len(finance.DefaultSpendingCategoryNames)-1 {
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

// Every agent has one transfer category from the start, built in: it
// cannot be deleted, made income, put under a parent, given children or a
// budget, and no second one can be made. It can be renamed and keeps
// being the transfer category, since it is found by its flag.
func TestTransferSpendingCategoryIsBuiltIn(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "transfer-category")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		spendingCategories, err := tx.ListSpendingCategories(fixture.agentId)
		if err != nil || len(spendingCategories) != 2 || !spendingCategories[1].IsTransfer ||
			spendingCategories[1].SpendingCategoryName != finance.SpendingCategoryTransfer || spendingCategories[1].IsIncome {
			t.Fatalf("a new agent has the transfer category and the other category and nothing else: %v %+v", err, spendingCategories)
		}
		transferId := spendingCategories[1].ID
		if createdCount, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil || createdCount != len(finance.DefaultSpendingCategoryNames)-1 {
			t.Errorf("the built-in categories do not stand in for the defaults: %v %d", err, createdCount)
		}
		if again, err := tx.EnsureTransferSpendingCategory(fixture.agentId); err != nil || again.ID != transferId {
			t.Errorf("one transfer category per agent: %v %+v", err, again)
		}
		if err := tx.DeleteSpendingCategory(fixture.agentId, transferId); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the transfer category cannot be deleted: %v", err)
		}
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, transferId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.IsIncome = true
			return nil
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the transfer category cannot be income: %v", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, transferId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.ParentSpendingCategoryID = byName[finance.SpendingCategoryShopping]
			return nil
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the transfer category cannot have a parent: %v", err)
		}
		if _, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "card payments",
			ParentSpendingCategoryID: transferId}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the transfer category cannot have children: %v", err)
		}
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryShopping], func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.IsTransfer = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateSpendingCategory: %s", err)
		}
		if shopping, err := tx.GetSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryShopping]); err != nil || shopping.IsTransfer {
			t.Errorf("no other spending category becomes the transfer category: %v %+v", err, shopping)
		}
		made, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "moving money", IsTransfer: true})
		if err != nil || made.IsTransfer {
			t.Errorf("a second transfer category cannot be made: %v %+v", err, made)
		}
		if _, err := tx.SetBudget(&models.Budget{AgentID: fixture.agentId, SpendingCategoryID: transferId, MonthlyAmount: "100",
			CurrencyCode: "USD", EffectiveFrom: "2026-09"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the transfer category takes no budget: %v", err)
		}
		renamed, err := tx.UpdateSpendingCategory(fixture.agentId, transferId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.SpendingCategoryName = "between my accounts"
			return nil
		})
		if err != nil || !renamed.IsTransfer || renamed.SpendingCategoryName != "between my accounts" {
			t.Errorf("renamed, it is still the transfer category: %v %+v", err, renamed)
		}
		if found, err := tx.EnsureTransferSpendingCategory(fixture.agentId); err != nil || found.ID != transferId {
			t.Errorf("found by its flag, not its name: %v %+v", err, found)
		}
	})
}

// Every agent has one other category from the start, built in like the
// transfer category: it cannot be deleted, made income, put under a parent
// or given children, and no second one can be made. Unlike transfer it is
// spending: it takes a budget, the person may file a transaction under it,
// and it can be renamed and hidden and stays the other category.
func TestOtherSpendingCategoryIsBuiltIn(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "other-category")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		otherId := byName[finance.SpendingCategoryOther]
		other, err := tx.GetSpendingCategory(fixture.agentId, otherId)
		if err != nil || other == nil || !other.IsOther || other.IsTransfer || other.IsIncome {
			t.Fatalf("the default other is the built-in one: %v %+v", err, other)
		}
		if again, err := tx.EnsureOtherSpendingCategory(fixture.agentId); err != nil || again.ID != otherId {
			t.Errorf("one other category per agent: %v %+v", err, again)
		}
		if err := tx.DeleteSpendingCategory(fixture.agentId, otherId); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the other category cannot be deleted: %v", err)
		}
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, otherId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.IsIncome = true
			return nil
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the other category cannot be income: %v", err)
		}
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, otherId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.ParentSpendingCategoryID = byName[finance.SpendingCategoryShopping]
			return nil
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the other category cannot have a parent: %v", err)
		}
		if _, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "odds and ends",
			ParentSpendingCategoryID: otherId}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the other category cannot have children: %v", err)
		}
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryShopping], func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.ParentSpendingCategoryID = otherId
			return nil
		}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("nothing can be moved under the other category: %v", err)
		}
		if _, err := tx.UpdateSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryShopping], func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.IsOther = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateSpendingCategory: %s", err)
		}
		if shopping, err := tx.GetSpendingCategory(fixture.agentId, byName[finance.SpendingCategoryShopping]); err != nil || shopping.IsOther {
			t.Errorf("no other spending category becomes the other category: %v %+v", err, shopping)
		}
		made, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "miscellany", IsOther: true})
		if err != nil || made.IsOther {
			t.Errorf("a second other category cannot be made: %v %+v", err, made)
		}
		if _, err := tx.SetBudget(&models.Budget{AgentID: fixture.agentId, SpendingCategoryID: otherId, MonthlyAmount: "100",
			CurrencyCode: "USD", EffectiveFrom: "2026-09"}); err != nil {
			t.Errorf("the other category is spending and takes a budget: %v", err)
		}
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["transaction-grocer"].ID, otherId, models.CategorizedByPerson, nil); err != nil {
			t.Errorf("the person files a transaction under the other category: %v", err)
		}
		renamed, err := tx.UpdateSpendingCategory(fixture.agentId, otherId, func(spendingCategory *models.SpendingCategory) error {
			spendingCategory.SpendingCategoryName = "everything else"
			spendingCategory.IsHidden = true
			return nil
		})
		if err != nil || !renamed.IsOther || !renamed.IsHidden || renamed.SpendingCategoryName != "everything else" {
			t.Errorf("renamed and hidden, it is still the other category: %v %+v", err, renamed)
		}
		if found, err := tx.EnsureOtherSpendingCategory(fixture.agentId); err != nil || found.ID != otherId {
			t.Errorf("found by its flag, not its name: %v %+v", err, found)
		}
	})
}

// An agent that already had spending categories of its own called other
// and anything else, and somehow lost its other category, gets one named
// around both, and theirs stay ordinary spending categories.
func TestOtherSpendingCategoryLeavesAPersonsOtherAlone(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "other-category-taken")
	dbtest.Exec(t, database, fmt.Sprintf(`DELETE FROM "agent_spending_category" WHERE "agent_id" = '%s' AND "is_other"`, fixture.agentId))

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		theirs, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "Other"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		otherCategory, err := tx.EnsureOtherSpendingCategory(fixture.agentId)
		if err != nil || otherCategory.ID == theirs.ID || otherCategory.SpendingCategoryName != finance.SpendingCategoryOtherFallback {
			t.Fatalf("EnsureOtherSpendingCategory: %v %+v", err, otherCategory)
		}
		if kept, err := tx.GetSpendingCategory(fixture.agentId, theirs.ID); err != nil || kept.IsOther {
			t.Errorf("theirs stays theirs: %v %+v", err, kept)
		}
	})

	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_spending_category" SET "is_other" = false WHERE "agent_id" = '%s'`, fixture.agentId))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		otherCategory, err := tx.EnsureOtherSpendingCategory(fixture.agentId)
		if err != nil || !strings.HasPrefix(otherCategory.SpendingCategoryName, finance.SpendingCategoryOtherFallback+" ") {
			t.Errorf("with both names taken, a few characters after the second: %v %+v", err, otherCategory)
		}
	})
}

// The person never takes a spending category away: no spending category
// means not decided yet, and the person's "fits nothing" is the other
// category. What a sync or the model does is not affected.
func TestPersonCannotChooseNoSpendingCategory(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "no-spending-category")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		financeTransactionId := found["transaction-grocer"].ID
		if _, err := tx.SetTransactionCategorization(fixture.agentId, financeTransactionId, "", models.CategorizedByPerson, nil); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the person choosing none is refused: %v", err)
		}
		if _, err := tx.CategorizeTransactionsByPerson(fixture.agentId, []string{financeTransactionId}, ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("the person choosing none for several is refused: %v", err)
		}
		if kept, err := tx.GetFinanceTransaction(fixture.agentId, financeTransactionId); err != nil || kept.CategorizedBy == models.CategorizedByPerson {
			t.Errorf("nothing was written: %v %+v", err, kept)
		}
	})
}

// An agent that already had a spending category of its own called
// transfer, and somehow lost its transfer category, gets one under the
// other built-in name, and theirs stays an ordinary spending category.
func TestTransferSpendingCategoryLeavesAPersonsTransferAlone(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "transfer-category-taken")
	dbtest.Exec(t, database, fmt.Sprintf(`DELETE FROM "agent_spending_category" WHERE "agent_id" = '%s'`, fixture.agentId))

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		theirs, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "Transfer"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		transferCategory, err := tx.EnsureTransferSpendingCategory(fixture.agentId)
		if err != nil || transferCategory.ID == theirs.ID || transferCategory.SpendingCategoryName != finance.SpendingCategoryTransferFallback {
			t.Fatalf("EnsureTransferSpendingCategory: %v %+v", err, transferCategory)
		}
		if kept, err := tx.GetSpendingCategory(fixture.agentId, theirs.ID); err != nil || kept.IsTransfer {
			t.Errorf("theirs stays theirs: %v %+v", err, kept)
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
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "autopay",
			SpendingCategoryID: byName[finance.SpendingCategoryTransfer], RulePriority: 30}); err != nil {
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
		if payment := found["card-payment"]; payment.SpendingCategoryID != byName[finance.SpendingCategoryTransfer] || payment.CategorizedBy != models.CategorizedBySpendingRule {
			t.Errorf("a rule to the transfer category marks a transfer: %+v", payment)
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

// A rule matching exactly what one already matches is that rule: saved
// again it is not copied, saved with another category the one there takes
// it, and a rule that differs in account or amount limits is its own.
func TestCreatingTheSameSpendingRuleKeepsOne(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "same-rule")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		create := func(rule models.SpendingRule) *models.SpendingRule {
			t.Helper()
			rule.AgentID = fixture.agentId
			created, err := tx.CreateSpendingRule(&rule)
			if err != nil {
				t.Fatalf("CreateSpendingRule %+v: %s", rule, err)
			}
			return created
		}
		first := create(models.SpendingRule{MatchText: "INVENTED STORE 0042", SpendingCategoryID: byName[finance.SpendingCategoryShopping]})
		again := create(models.SpendingRule{MatchText: "invented store 0042 ", SpendingCategoryID: byName[finance.SpendingCategoryShopping]})
		recategorized := create(models.SpendingRule{MatchText: "Invented Store 0042", SpendingCategoryID: byName[finance.SpendingCategoryGroceries]})
		if again.ID != first.ID || recategorized.ID != first.ID {
			t.Errorf("the same rule was copied: %s, %s, %s", first.ID, again.ID, recategorized.ID)
		}
		if recategorized.SpendingCategoryID != byName[finance.SpendingCategoryGroceries] {
			t.Errorf("the rule there did not take the new category: %+v", recategorized)
		}
		limited := create(models.SpendingRule{MatchText: "INVENTED STORE 0042", MaximumAmount: "-100", SpendingCategoryID: byName[finance.SpendingCategoryShopping]})
		if limited.ID == first.ID {
			t.Errorf("a rule with an amount limit was taken for the one without")
		}
		rules, err := tx.ListSpendingRules(fixture.agentId)
		if err != nil {
			t.Fatalf("ListSpendingRules: %s", err)
		}
		if len(rules) != 2 {
			t.Errorf("rules %d, want 2: %+v", len(rules), rules)
		}
	})
}

// Setting a budget from a month drops the later rows that only repeated
// what it replaced or repeat it now, so moving a budget's start earlier and
// then changing its amount from that start changes all of it; a later
// change to another amount stays.
func TestSetBudgetDropsRowsThatOnlyRepeat(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "budget-repeat")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		dining, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: "dining"})
		if err != nil {
			t.Fatalf("CreateSpendingCategory: %s", err)
		}
		set := func(monthlyAmount, effectiveFrom string) {
			t.Helper()
			budget := models.Budget{AgentID: fixture.agentId, SpendingCategoryID: dining.ID, MonthlyAmount: monthlyAmount, CurrencyCode: "USD", EffectiveFrom: effectiveFrom}
			if _, err := tx.SetBudget(&budget); err != nil {
				t.Fatalf("SetBudget %+v: %s", budget, err)
			}
		}
		rows := func() string {
			t.Helper()
			budgets, err := tx.ListBudgets(fixture.agentId)
			if err != nil {
				t.Fatalf("ListBudgets: %s", err)
			}
			said := []string{}
			for _, budget := range budgets {
				said = append(said, budget.EffectiveFrom[:7]+"="+budget.MonthlyAmount)
			}
			return strings.Join(said, " ")
		}

		// Set from September, then the same amount again from January: the
		// September row only repeated it and goes.
		set("400", "2026-09")
		set("400", "2026-01")
		if have := rows(); have != "2026-01=400.0000" {
			t.Errorf("after moving the start earlier: %s", have)
		}

		// A real change from June stays; changing the amount at January then
		// keeps June's change, which is a different amount.
		set("450", "2026-06")
		set("420", "2026-01")
		if have := rows(); have != "2026-01=420.0000 2026-06=450.0000" {
			t.Errorf("after a change at the start with a later change: %s", have)
		}

		// A stale copy of the old amount after the start, as the dashboard
		// used to make, goes when the amount changes from the start.
		set("300", "2027-01")
		set("300", "2027-03")
		set("320", "2027-01")
		if have := rows(); have != "2026-01=420.0000 2026-06=450.0000 2027-01=320.0000" {
			t.Errorf("after changing the amount at a start with a stale copy: %s", have)
		}
	})
}

// The changes a person set on purpose stay: a return to an earlier amount
// after a different one, a restart after a budget was ended, and a later
// row with the old amount when a new change is put between. Each case
// starts from its own spending category.
func TestSetBudgetKeepsDeliberateChanges(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "budget-deliberate")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		category := func(name string) string {
			t.Helper()
			created, err := tx.CreateSpendingCategory(&models.SpendingCategory{AgentID: fixture.agentId, SpendingCategoryName: name})
			if err != nil {
				t.Fatalf("CreateSpendingCategory: %s", err)
			}
			return created.ID
		}
		set := func(spendingCategoryId, monthlyAmount, currencyCode, effectiveFrom string) {
			t.Helper()
			budget := models.Budget{AgentID: fixture.agentId, SpendingCategoryID: spendingCategoryId, MonthlyAmount: monthlyAmount, CurrencyCode: currencyCode, EffectiveFrom: effectiveFrom}
			if _, err := tx.SetBudget(&budget); err != nil {
				t.Fatalf("SetBudget %+v: %s", budget, err)
			}
		}
		rows := func(spendingCategoryId string) string {
			t.Helper()
			budgets, err := tx.ListBudgets(fixture.agentId)
			if err != nil {
				t.Fatalf("ListBudgets: %s", err)
			}
			said := []string{}
			for _, budget := range budgets {
				if budget.SpendingCategoryID == spendingCategoryId {
					said = append(said, budget.EffectiveFrom[:7]+"="+budget.MonthlyAmount+budget.CurrencyCode)
				}
			}
			return strings.Join(said, " ")
		}

		// A summer increase moved earlier keeps its return to 400.
		summer := category("summer")
		set(summer, "400", "USD", "2026-01")
		set(summer, "450", "USD", "2026-06")
		set(summer, "400", "USD", "2026-09")
		set(summer, "450", "USD", "2026-03")
		if have := rows(summer); have != "2026-01=400.0000USD 2026-03=450.0000USD 2026-09=400.0000USD" {
			t.Errorf("moving a summer increase earlier: %s", have)
		}

		// Ending a budget earlier keeps its later restart.
		restarted := category("restarted")
		set(restarted, "400", "USD", "2026-01")
		set(restarted, "0", "USD", "2026-03")
		set(restarted, "400", "USD", "2026-06")
		set(restarted, "0", "USD", "2026-02")
		if have := rows(restarted); have != "2026-01=400.0000USD 2026-02=0.0000USD 2026-06=400.0000USD" {
			t.Errorf("ending a budget earlier: %s", have)
		}

		// Editing a row in place drops the copy after it but keeps the
		// change after that, which differs from what came just before it.
		edited := category("edited")
		set(edited, "400", "USD", "2026-01")
		set(edited, "450", "USD", "2026-06")
		set(edited, "400", "USD", "2026-09")
		set(edited, "450", "USD", "2026-12")
		set(edited, "400", "USD", "2026-06")
		if have := rows(edited); have != "2026-01=400.0000USD 2026-06=400.0000USD 2026-12=450.0000USD" {
			t.Errorf("editing a row in place: %s", have)
		}

		// A new change put between keeps a later deliberate return to the
		// old amount, whichever was set first.
		between := category("between")
		set(between, "400", "USD", "2026-01")
		set(between, "400", "USD", "2026-09")
		set(between, "450", "USD", "2026-06")
		if have := rows(between); have != "2026-01=400.0000USD 2026-06=450.0000USD 2026-09=400.0000USD" {
			t.Errorf("a change put between: %s", have)
		}

		// The same amount in another currency is a change, not a repeat.
		currency := category("currency")
		set(currency, "400", "USD", "2026-01")
		set(currency, "400", "EUR", "2026-06")
		set(currency, "400", "USD", "2026-01")
		if have := rows(currency); have != "2026-01=400.0000USD 2026-06=400.0000EUR" {
			t.Errorf("a change of currency: %s", have)
		}

		// A dropped row leaves a delete in the audit log.
		audited := category("audited")
		set(audited, "400", "USD", "2026-09")
		budgets, err := tx.ListBudgets(fixture.agentId)
		if err != nil {
			t.Fatalf("ListBudgets: %s", err)
		}
		droppedId := ""
		for _, budget := range budgets {
			if budget.SpendingCategoryID == audited {
				droppedId = budget.ID
			}
		}
		set(audited, "400", "USD", "2026-01")
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourceBudget), ResourceID: droppedId})
		if err != nil {
			t.Fatalf("ListAuditEvents: %s", err)
		}
		isDeleteRecorded := false
		for _, event := range events {
			isDeleteRecorded = isDeleteRecorded || event.Action == models.AuditActionDelete
		}
		if droppedId == "" || !isDeleteRecorded {
			t.Errorf("no delete recorded for the dropped row %q: %+v", droppedId, events)
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
			{MatchText: "autopay", SpendingCategoryID: byName[finance.SpendingCategoryTransfer]},
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

// The other category is counted the way money with no spending category
// was: money out in it is its spending, and money in it is income in cash
// flow, not a refund that lowers that spending, and no income category's.
func TestOtherCategoryCountsMoneyOutAsSpendingAndMoneyInAsIncome(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "other-counting")
	result := sampleFinanceSync()
	result.Added = []finance.Transaction{
		{ProviderTransactionID: "odd-purchase", ProviderAccountID: "account-checking", PostedOn: "2026-08-03",
			Amount: "-50", CurrencyCode: "USD", Description: "EXAMPLE ODD PURCHASE"},
		{ProviderTransactionID: "odd-credit", ProviderAccountID: "account-checking", PostedOn: "2026-08-04",
			Amount: "20", CurrencyCode: "USD", Description: "EXAMPLE ODD CREDIT"},
	}
	applyFinanceSync(t, database, fixture, result, "2026-08-05")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		other, err := tx.EnsureOtherSpendingCategory(fixture.agentId)
		if err != nil {
			t.Fatalf("EnsureOtherSpendingCategory: %s", err)
		}
		ids := []string{}
		for _, financeTransaction := range financeTransactionsByProviderId(t, tx, fixture.agentId) {
			ids = append(ids, financeTransaction.ID)
		}
		if _, err := tx.CategorizeTransactionsByPerson(fixture.agentId, ids, other.ID); err != nil {
			t.Fatalf("CategorizeTransactionsByPerson: %s", err)
		}

		days, err := tx.ListSpendingCategoryDays(fixture.agentId, "2026-08")
		if err != nil {
			t.Fatalf("ListSpendingCategoryDays: %s", err)
		}
		if len(days) != 1 || days[0].SpendingCategoryID != other.ID || days[0].SpentOn != "2026-08-03" || days[0].SpendingAmount != "50.0000" {
			t.Errorf("other's spending is its money out alone: %+v", days)
		}
		incomeDays, err := tx.ListIncomeCategoryDays(fixture.agentId, "2026-08")
		if err != nil {
			t.Fatalf("ListIncomeCategoryDays: %s", err)
		}
		if len(incomeDays) != 0 {
			t.Errorf("other's money in is no income category's: %+v", incomeDays)
		}
		cashFlowDays, err := tx.ListCashFlowDays(fixture.agentId, "2026-08-01", "2026-08-31")
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
		if finance.FormatAmount(incomeTotal) != "20.0000" || finance.FormatAmount(spendingTotal) != "50.0000" {
			t.Errorf("August's cash flow is income %s and spending %s, want 20.0000 and 50.0000 (days %+v)",
				finance.FormatAmount(incomeTotal), finance.FormatAmount(spendingTotal), cashFlowDays)
		}
	})
}

// A budget on an income spending category is kept like any other, and
// income per day counts what income categories took in, a reversal
// against it, never transfers, spending or money in nothing categorized.
func TestIncomeBudgetsAndIncomeCategoryDays(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "income-days")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "payroll-correction", ProviderAccountID: "account-checking", PostedOn: "2026-09-01",
			Amount: "-100", CurrencyCode: "USD", Description: "PAYROLL EXAMPLE CO CORRECTION"},
		finance.Transaction{ProviderTransactionID: "payroll-second", ProviderAccountID: "account-checking", PostedOn: "2026-09-15",
			Amount: "1200", CurrencyCode: "USD", Description: "PAYROLL EXAMPLE CO"},
		finance.Transaction{ProviderTransactionID: "payroll-august", ProviderAccountID: "account-checking", PostedOn: "2026-08-15",
			Amount: "2400", CurrencyCode: "USD", Description: "PAYROLL EXAMPLE CO"},
		finance.Transaction{ProviderTransactionID: "from-savings", ProviderAccountID: "account-checking", PostedOn: "2026-09-16",
			Amount: "300", CurrencyCode: "USD", Description: "PAYROLL EXAMPLE CO TRANSFER"},
		finance.Transaction{ProviderTransactionID: "gift", ProviderAccountID: "account-checking", PostedOn: "2026-09-17",
			Amount: "50", CurrencyCode: "USD", Description: "A GIFT"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-18")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		incomeId := byName[finance.SpendingCategoryIncome]
		for _, spendingRule := range []models.SpendingRule{
			{MatchText: "transfer", SpendingCategoryID: byName[finance.SpendingCategoryTransfer], RulePriority: 0},
			{MatchText: "payroll", SpendingCategoryID: incomeId, RulePriority: 1},
			{MatchText: "corner grocer", SpendingCategoryID: byName[finance.SpendingCategoryGroceries], RulePriority: 2},
		} {
			spendingRule.AgentID = fixture.agentId
			if _, err := tx.CreateSpendingRule(&spendingRule); err != nil {
				t.Fatalf("CreateSpendingRule: %s", err)
			}
		}

		budget, err := tx.SetBudget(&models.Budget{AgentID: fixture.agentId, SpendingCategoryID: incomeId, MonthlyAmount: "3500", CurrencyCode: "USD", EffectiveFrom: "2026-09"})
		if err != nil {
			t.Fatalf("an income spending category takes a budget: %s", err)
		}
		budgets, err := tx.BudgetsForMonth(fixture.agentId, "2026-09")
		if err != nil || len(budgets) != 1 || budgets[0].ID != budget.ID || budgets[0].MonthlyAmount != "3500.0000" {
			t.Fatalf("the income budget is in force in September: %v %+v", err, budgets)
		}

		days, err := tx.ListIncomeCategoryDays(fixture.agentId, "2026-09")
		if err != nil {
			t.Fatalf("ListIncomeCategoryDays: %s", err)
		}
		byDay := map[string]string{}
		for _, day := range days {
			if day.SpendingCategoryID != incomeId || day.CurrencyCode != "USD" {
				t.Errorf("only the income spending category is listed: %+v", day)
			}
			byDay[day.ReceivedOn] = day.IncomeAmount
		}
		expected := map[string]string{"2026-09-01": "2400.0000", "2026-09-15": "1200.0000"}
		if len(byDay) != len(expected) {
			t.Errorf("the transfer, the gift with no category and the groceries are left out, and August is another month: %v", byDay)
		}
		for receivedOn, amount := range expected {
			if byDay[receivedOn] != amount {
				t.Errorf("%s: got %q, want %q (all: %v)", receivedOn, byDay[receivedOn], amount, byDay)
			}
		}
		august, err := tx.ListIncomeCategoryDays(fixture.agentId, "2026-08")
		if err != nil || len(august) != 1 || august[0].IncomeAmount != "2400.0000" {
			t.Errorf("August has its one payroll: %v %+v", err, august)
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

// A transfer a spending rule gave is cleared when no rule matches it any
// more. A transfer something else gave first is not the rule's to take
// over or clear, whatever the rule assigns, and a person's decision is
// never touched.
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
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		transferId := byName[finance.SpendingCategoryTransfer]
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["autopay-detected"].ID, transferId, models.CategorizedByTransferDetection, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["autopay-person"].ID, byName[finance.SpendingCategoryHealth], models.CategorizedByPerson, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}
		rule, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "autopay", SpendingCategoryID: transferId, RulePriority: 10})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		storeCard, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "store card",
			SpendingCategoryID: byName[finance.SpendingCategoryShopping], RulePriority: 5})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		if marked := found["autopay-rule"]; marked.SpendingCategoryID != transferId || marked.CategorizedBy != models.CategorizedBySpendingRule {
			t.Errorf("the rule marks a transfer and says so: %+v", marked)
		}
		if detected := found["autopay-detected"]; detected.SpendingCategoryID != transferId || detected.CategorizedBy != models.CategorizedByTransferDetection {
			t.Errorf("no rule takes over a transfer detection marked: %+v", detected)
		}
		if decided := found["autopay-person"]; decided.SpendingCategoryID != byName[finance.SpendingCategoryHealth] || decided.CategorizedBy != models.CategorizedByPerson {
			t.Errorf("the rule does not override the person: %+v", decided)
		}

		for _, spendingRuleId := range []string{rule.ID, storeCard.ID} {
			if err := tx.DeleteSpendingRule(fixture.agentId, spendingRuleId); err != nil {
				t.Fatalf("DeleteSpendingRule: %s", err)
			}
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		if cleared := found["autopay-rule"]; cleared.SpendingCategoryID != "" || cleared.CategorizedBy != "" {
			t.Errorf("with the rule gone its transfer is cleared: %+v", cleared)
		}
		if detected := found["autopay-detected"]; detected.SpendingCategoryID != transferId || detected.CategorizedBy != models.CategorizedByTransferDetection {
			t.Errorf("a transfer detection marked outlives the rule: %+v", detected)
		}
		if decided := found["autopay-person"]; decided.SpendingCategoryID != byName[finance.SpendingCategoryHealth] || decided.CategorizedBy != models.CategorizedByPerson {
			t.Errorf("the person's decision outlives the rule: %+v", decided)
		}
	})
}

// The owner's case: a rule matching a description classifies what it
// matches as transfers, the ones already there and the ones a later sync
// brings, so spending leaves them out. A person's choice on one of them
// beats the rule, and a rule's transfer is judged again when the amount
// changes, as a pair is.
func TestSpendingRuleMarksTransfersPastAndFuture(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "spending-rule-transfer-future")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "payment-august", ProviderAccountID: "account-card", PostedOn: "2026-09-02",
			Amount: "250", CurrencyCode: "USD", Description: "ONLINE PAYMENT THANK YOU"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	var transferId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		transferId = transferCategoryId(t, tx, fixture.agentId)
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "online payment", SpendingCategoryID: transferId}); err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		if past := financeTransactionsByProviderId(t, tx, fixture.agentId)["payment-august"]; past.SpendingCategoryID != transferId {
			t.Errorf("the rule classifies the transaction already there: %+v", past)
		}
	})

	later := sampleFinanceSync()
	later.Added = append(later.Added,
		finance.Transaction{ProviderTransactionID: "payment-august", ProviderAccountID: "account-card", PostedOn: "2026-09-02",
			Amount: "250", CurrencyCode: "USD", Description: "ONLINE PAYMENT THANK YOU"},
		finance.Transaction{ProviderTransactionID: "payment-september", ProviderAccountID: "account-card", PostedOn: "2026-09-20",
			Amount: "410", CurrencyCode: "USD", Description: "ONLINE PAYMENT THANK YOU"},
		finance.Transaction{ProviderTransactionID: "payment-refund", ProviderAccountID: "account-card", PostedOn: "2026-09-21",
			Amount: "35", CurrencyCode: "USD", Description: "ONLINE PAYMENT REVERSAL"},
	)
	applyFinanceSync(t, database, fixture, later, "2026-09-22")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.ApplySpendingRules(fixture.agentId); err != nil {
			t.Fatalf("ApplySpendingRules: %s", err)
		}
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		for _, providerTransactionId := range []string{"payment-august", "payment-september", "payment-refund"} {
			if found[providerTransactionId].SpendingCategoryID != transferId || found[providerTransactionId].CategorizedBy != models.CategorizedBySpendingRule {
				t.Errorf("%s is a transfer by the rule: %+v", providerTransactionId, found[providerTransactionId])
			}
		}
		otherCategory, err := tx.EnsureOtherSpendingCategory(fixture.agentId)
		if err != nil {
			t.Fatalf("EnsureOtherSpendingCategory: %s", err)
		}
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["payment-refund"].ID, otherCategory.ID, models.CategorizedByPerson, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}
		if _, err := tx.ApplySpendingRules(fixture.agentId); err != nil {
			t.Fatalf("ApplySpendingRules: %s", err)
		}
		if refund := financeTransactionsByProviderId(t, tx, fixture.agentId)["payment-refund"]; refund.SpendingCategoryID != otherCategory.ID || refund.CategorizedBy != models.CategorizedByPerson {
			t.Errorf("the person's choice beats the rule: %+v", refund)
		}
		summary, err := tx.FinanceSpendingSummary(fixture.agentId, &db.FinanceSpendingSummaryFilter{GroupBy: models.FinanceSpendingSummaryGroupByMerchant})
		if err != nil {
			t.Fatalf("FinanceSpendingSummary: %s", err)
		}
		for _, row := range summary {
			if row.GroupKey == "ONLINE PAYMENT THANK YOU" {
				t.Errorf("the rule's transfers are left out of spending: %+v", row)
			}
		}
	})

	changed := sampleFinanceSync()
	changed.Added = append(changed.Added,
		finance.Transaction{ProviderTransactionID: "payment-september", ProviderAccountID: "account-card", PostedOn: "2026-09-20",
			Amount: "415", CurrencyCode: "USD", Description: "ONLINE PAYMENT THANK YOU"},
	)
	applyFinanceSync(t, database, fixture, changed, "2026-09-23")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if changedAmount := financeTransactionsByProviderId(t, tx, fixture.agentId)["payment-september"]; changedAmount.SpendingCategoryID != "" {
			t.Errorf("a rule's transfer is dropped when the amount changes, to be judged again: %+v", changedAmount)
		}
		if _, err := tx.ApplySpendingRules(fixture.agentId); err != nil {
			t.Fatalf("ApplySpendingRules: %s", err)
		}
		if again := financeTransactionsByProviderId(t, tx, fixture.agentId)["payment-september"]; again.SpendingCategoryID != transferId {
			t.Errorf("and the rule gives it again: %+v", again)
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

// A savings target keeps the finance accounts it measures, once each and
// only the agent's own; a target measured otherwise keeps none, and a
// deleted finance account leaves the targets that chose it.
func TestSavingsTargetsKeepTheirFinanceAccounts(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "savings-target-accounts")
	stranger := createFinanceFixture(t, database, "savings-target-stranger")
	applyFinanceSync(t, database, fixture, brokerageSync("2150.25", fundHolding("12", "1824.58")), "2026-09-12")
	applyFinanceSync(t, database, stranger, brokerageSync("10", nil), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		accounts, err := tx.ListFinanceAccounts(fixture.agentId, "")
		if err != nil || len(accounts) != 1 {
			t.Fatalf("ListFinanceAccounts: %v %v", accounts, err)
		}
		strangerAccounts, err := tx.ListFinanceAccounts(stranger.agentId, "")
		if err != nil || len(strangerAccounts) != 1 {
			t.Fatalf("ListFinanceAccounts: %v %v", strangerAccounts, err)
		}
		created, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: fixture.agentId, SavingsTargetName: "invested",
			TargetAmount: "10000", CurrencyCode: "USD", TargetOn: "2028-06-01", TargetMeasure: models.TargetMeasureAssetValue,
			StartedOn: "2026-09-12", FinanceAccountIDs: []string{accounts[0].ID, accounts[0].ID}})
		if err != nil {
			t.Fatalf("CreateSavingsTarget: %s", err)
		}
		if len(created.FinanceAccountIDs) != 1 || created.FinanceAccountIDs[0] != accounts[0].ID || len(created.AssetIDs) != 0 {
			t.Errorf("CreateSavingsTarget keeps the account once: %+v", created)
		}
		if _, err := tx.CreateSavingsTarget(&models.SavingsTarget{AgentID: fixture.agentId, SavingsTargetName: "taken",
			TargetAmount: "1", CurrencyCode: "USD", TargetOn: "2028-06-01", TargetMeasure: models.TargetMeasureAssetValue,
			StartedOn: "2026-09-12", FinanceAccountIDs: []string{strangerAccounts[0].ID}}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("another agent's finance account must be refused: %v", err)
		}
		netWorth, err := tx.UpdateSavingsTarget(fixture.agentId, created.ID, func(savingsTarget *models.SavingsTarget) error {
			savingsTarget.TargetMeasure = models.TargetMeasureNetWorth
			return nil
		})
		if err != nil || netWorth.TargetMeasure != models.TargetMeasureNetWorth || len(netWorth.FinanceAccountIDs) != 0 {
			t.Errorf("a net worth target chooses nothing: %v %+v", err, netWorth)
		}
		if _, err := tx.UpdateSavingsTarget(fixture.agentId, created.ID, func(savingsTarget *models.SavingsTarget) error {
			savingsTarget.TargetMeasure = models.TargetMeasureAssetValue
			savingsTarget.FinanceAccountIDs = []string{accounts[0].ID}
			return nil
		}); err != nil {
			t.Fatalf("UpdateSavingsTarget: %s", err)
		}
		if err := tx.DeleteAgentSource(fixture.agentId, fixture.sourceId); err != nil {
			t.Fatalf("DeleteAgentSource: %s", err)
		}
		after, err := tx.GetSavingsTarget(fixture.agentId, created.ID)
		if err != nil || after == nil || len(after.FinanceAccountIDs) != 0 {
			t.Errorf("the deleted finance account leaves the target: %v %+v", err, after)
		}
	})
}
