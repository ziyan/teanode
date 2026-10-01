package apigraph

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// twoSpendingCategories is two of the owner's spending categories that are
// neither income nor the transfer category.
func twoSpendingCategories(test *testing.T, ctx context.Context, resolver *graph) (*models.SpendingCategory, *models.SpendingCategory) {
	test.Helper()
	categories, err := resolver.SpendingCategories(ctx)
	if err != nil {
		test.Fatal(err)
	}
	var found []*models.SpendingCategory
	for _, category := range categories {
		if !category.IsTransfer && !category.IsIncome {
			found = append(found, category)
		}
	}
	if len(found) < 2 {
		test.Fatalf("two spending categories: %+v", categories)
	}
	return found[0], found[1]
}

// Categorizing several is all or none: every one becomes the person's
// choice, in the order named, and a list with an id that is not the
// caller's, or naming too many, is refused with nothing written. Another
// person cannot categorize the owner's at all.
func TestCategorizeTransactionsAllOrNone(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	var first, second *models.SpendingCategory
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		first, second = twoSpendingCategories(test, ctx, resolver)
		categorized, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: []string{transactions[1].ID, transactions[0].ID, transactions[1].ID},
			SpendingCategoryID:    first.ID,
		})
		if err != nil {
			test.Fatal(err)
		}
		if len(categorized.FinanceTransactions) != 2 || categorized.FinanceTransactions[0].ID != transactions[1].ID ||
			categorized.FinanceTransactions[1].ID != transactions[0].ID || len(categorized.SpendingRules) != 0 {
			test.Fatalf("categorized %+v", categorized)
		}
		for _, financeTransaction := range categorized.FinanceTransactions {
			if financeTransaction.SpendingCategoryID != first.ID || financeTransaction.CategorizedBy != models.CategorizedByPerson {
				test.Errorf("categorized %+v", financeTransaction)
			}
		}

		tooMany := make([]string, maximumCategorizedTransactionCount+1)
		for index := range tooMany {
			tooMany[index] = fmt.Sprintf("transaction-invented-%d", index)
		}
		for _, refused := range []struct {
			arguments CategorizeTransactionsArguments
			wanted    error
		}{
			{CategorizeTransactionsArguments{FinanceTransactionIDs: []string{transactions[0].ID, "transaction-invented-elsewhere"}, SpendingCategoryID: second.ID}, api.ErrNotFound},
			{CategorizeTransactionsArguments{FinanceTransactionIDs: tooMany, SpendingCategoryID: second.ID}, api.ErrInvalidArguments},
			{CategorizeTransactionsArguments{FinanceTransactionIDs: []string{" "}, SpendingCategoryID: second.ID}, api.ErrInvalidArguments},
			{CategorizeTransactionsArguments{FinanceTransactionIDs: []string{transactions[0].ID}, SpendingCategoryID: "spending-category-invented"}, api.ErrInvalidArguments},
		} {
			if _, err := resolver.CategorizeTransactions(ctx, refused.arguments); !errors.Is(err, refused.wanted) {
				test.Errorf("%d ids answered %v, not %v", len(refused.arguments.FinanceTransactionIDs), err, refused.wanted)
			}
		}
	})
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: []string{transactions[0].ID, transactions[1].ID},
		}); err == nil {
			test.Error("the stranger categorized the owner's transactions")
		}
		if _, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{
			FinanceTransactionIDs: []string{transactions[0].ID}, SpendingCategoryID: second.ID,
		}); err == nil {
			test.Error("the stranger was proposed rules for the owner's transactions")
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		for _, financeTransaction := range transactions {
			after, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, financeTransaction.ID)
			if err != nil {
				test.Fatal(err)
			}
			if after.SpendingCategoryID != first.ID || after.CategorizedBy != models.CategorizedByPerson {
				test.Errorf("a refused call changed %+v", after)
			}
		}
	})
}

// The person's choice in bulk beats a spending rule made afterwards, as
// one made in a transaction's own row does.
func TestCategorizeTransactionsBeatsLaterRules(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		first, second := twoSpendingCategories(test, ctx, resolver)
		if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: []string{transactions[0].ID, transactions[1].ID}, SpendingCategoryID: first.ID,
		}); err != nil {
			test.Fatal(err)
		}
		if _, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "grocer", SpendingCategoryID: second.ID}); err != nil {
			test.Fatal(err)
		}
		for _, financeTransaction := range transactions {
			after, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, financeTransaction.ID)
			if err != nil {
				test.Fatal(err)
			}
			if after.SpendingCategoryID != first.ID || after.CategorizedBy != models.CategorizedByPerson {
				test.Errorf("a later rule took over the person's choice: %+v", after)
			}
		}
	})
}

// Saving rules in bulk saves one per match text the proposal lists,
// leaving out one an existing rule already files there, and they apply to
// other transactions like these, not to the ones the person chose.
func TestCategorizeTransactionsSavesTheProposedRules(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	isRuleWanted := true
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		first, second := twoSpendingCategories(test, ctx, resolver)
		// CAFE ABROAD is filed under first already; Corner Grocer is not.
		if _, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "cafe", SpendingCategoryID: first.ID}); err != nil {
			test.Fatal(err)
		}
		ids := []string{transactions[0].ID, transactions[1].ID}
		proposals, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids, SpendingCategoryID: first.ID})
		if err != nil || len(proposals) != 1 || proposals[0].MatchText != "Corner Grocer" || proposals[0].FinanceTransactionCount != 1 {
			test.Fatalf("proposed %+v %v", proposals, err)
		}
		if _, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a proposal with no spending category answered %v", err)
		}
		if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{FinanceTransactionIDs: ids, ShouldCreateSpendingRules: &isRuleWanted}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("rules with no spending category answered %v", err)
		}
		categorized, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: ids, SpendingCategoryID: first.ID, ShouldCreateSpendingRules: &isRuleWanted,
		})
		if err != nil {
			test.Fatal(err)
		}
		if len(categorized.SpendingRules) != 1 || categorized.SpendingRules[0].MatchText != "Corner Grocer" || categorized.SpendingRules[0].SpendingCategoryID != first.ID {
			test.Fatalf("saved %+v", categorized.SpendingRules)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 2 || rules[1].RulePriority <= rules[0].RulePriority {
			test.Errorf("rules %+v %v", rules, err)
		}
		// Asked again, every match text is covered now.
		proposals, err = resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids, SpendingCategoryID: first.ID})
		if err != nil || len(proposals) != 0 {
			test.Errorf("proposed again %+v %v", proposals, err)
		}
		// A rule to another spending category does not cover them.
		proposals, err = resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids, SpendingCategoryID: second.ID})
		if err != nil || len(proposals) != 2 {
			test.Errorf("proposed for another spending category %+v %v", proposals, err)
		}
	})
}

// The proposal is one rule per distinct match text, in any case, the
// merchant or else the description; leaves out one an unbounded rule to
// the same spending category covers, and one a shorter proposal covers;
// and lists the most transactions first.
func TestProposeSpendingRules(test *testing.T) {
	test.Parallel()
	transaction := func(merchantName, description string) *models.FinanceTransaction {
		return &models.FinanceTransaction{MerchantName: merchantName, Description: description}
	}
	financeTransactions := []*models.FinanceTransaction{
		transaction("Invented Bistro", "INVENTED BISTRO 0101"),
		transaction("invented bistro", "INVENTED BISTRO 0102"),
		transaction("", "  NOODLE PLACE 77 "),
		transaction("Noodle Place Downtown", ""),
		transaction("", "ONLINE PAYMENT 2026-09-01"),
		transaction("Kiosk Example", ""),
		transaction("Bounded Example", ""),
		transaction("Elsewhere Example", ""),
		transaction("", ""),
	}
	spendingRules := []*models.SpendingRule{
		{MatchText: "online payment", SpendingCategoryID: "category-dining"},
		{MatchText: "kiosk", SpendingCategoryID: "category-dining", MaximumAmount: "-10.0000"},
		{MatchText: "bounded", SpendingCategoryID: "category-dining", FinanceAccountID: "account-one"},
		{MatchText: "elsewhere", SpendingCategoryID: "category-groceries"},
	}
	proposals := proposeSpendingRules(financeTransactions, spendingRules, "category-dining")
	var have []string
	for _, proposal := range proposals {
		have = append(have, fmt.Sprintf("%s:%d", proposal.MatchText, proposal.FinanceTransactionCount))
	}
	wanted := []string{"Invented Bistro:2", "Bounded Example:1", "Elsewhere Example:1", "Kiosk Example:1", "NOODLE PLACE 77:1", "Noodle Place Downtown:1"}
	if fmt.Sprint(have) != fmt.Sprint(wanted) {
		test.Errorf("proposed %v, not %v", have, wanted)
	}

	// A shorter match text proposed covers a longer one it is within.
	proposals = proposeSpendingRules([]*models.FinanceTransaction{
		transaction("Invented Roasters Downtown", ""), transaction("Invented Roasters", ""),
	}, nil, "category-dining")
	if len(proposals) != 1 || proposals[0].MatchText != "Invented Roasters" || proposals[0].FinanceTransactionCount != 2 {
		test.Errorf("proposed %+v", proposals)
	}
}
