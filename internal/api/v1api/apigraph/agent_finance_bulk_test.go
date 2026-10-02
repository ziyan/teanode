package apigraph

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
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

// spendingCategoryNamed is one of the owner's spending categories by name.
func spendingCategoryNamed(test *testing.T, ctx context.Context, resolver *graph, spendingCategoryName string) *models.SpendingCategory {
	test.Helper()
	categories, err := resolver.SpendingCategories(ctx)
	if err != nil {
		test.Fatal(err)
	}
	for _, category := range categories {
		if category.SpendingCategoryName == spendingCategoryName {
			return category
		}
	}
	test.Fatalf("no spending category %q in %+v", spendingCategoryName, categories)
	return nil
}

// addFinanceTransactions syncs more invented transactions into the account
// seedFinanceSource made, and answers every one of the owner's by its
// provider id.
func (self *financeFixture) addFinanceTransactions(test *testing.T, sourceId string, added ...finance.Transaction) map[string]*models.FinanceTransaction {
	test.Helper()
	for index := range added {
		added[index].ProviderAccountID = "account-invented-1"
		added[index].PostedOn = "2026-09-15"
		added[index].CurrencyCode = "USD"
		if added[index].Amount == "" {
			added[index].Amount = "-15.0000"
		}
	}
	byProviderId := map[string]*models.FinanceTransaction{}
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		if _, err := tx.ApplyFinanceSync(self.ownerAgent.ID, sourceId, &finance.SyncResult{
			Accounts: []finance.Account{{
				ProviderAccountID: "account-invented-1", AccountName: "Everyday Checking", AccountMask: "0001", AccountKind: "depository",
				CurrencyCode: "USD", CurrentBalance: "1200.5000", BalanceAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
			}},
			Added: added,
		}, "2026-09-20"); err != nil {
			test.Fatalf("ApplyFinanceSync: %s", err)
		}
		page, err := tx.ListFinanceTransactions(self.ownerAgent.ID, &db.FinanceTransactionFilter{Limit: db.FinanceTransactionLimitMost})
		if err != nil {
			test.Fatal(err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			byProviderId[financeTransaction.ProviderTransactionID] = financeTransaction
		}
	})
	return byProviderId
}

// confirmAll is every spending rule proposed, as the person confirms them.
func confirmAll(proposals *SpendingRuleProposalsView) []ConfirmedSpendingRule {
	confirmed := make([]ConfirmedSpendingRule, 0, len(proposals.SpendingRuleProposals))
	for _, proposal := range proposals.SpendingRuleProposals {
		spendingRule := ConfirmedSpendingRule{MatchText: proposal.MatchText, SpendingCategoryID: proposal.SpendingCategoryID}
		if proposal.AheadOfSpendingRule != nil {
			spendingRule.AheadOfSpendingRuleID = proposal.AheadOfSpendingRule.ID
		}
		confirmed = append(confirmed, spendingRule)
	}
	return confirmed
}

// Saving rules in bulk saves exactly the confirmed ones, and they apply to
// other transactions like these, not to the ones the person chose. A rule
// that already files a match text there covers it; a rule to another
// spending category does not, and the new rule goes ahead of it.
func TestCategorizeTransactionsSavesTheConfirmedRules(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		first, second := twoSpendingCategories(test, ctx, resolver)
		// CAFE ABROAD is filed under first already; Corner Grocer is not.
		cafeRule, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "cafe", SpendingCategoryID: first.ID})
		if err != nil {
			test.Fatal(err)
		}
		ids := []string{transactions[0].ID, transactions[1].ID}
		proposals, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids, SpendingCategoryID: first.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 1 {
			test.Fatalf("proposed %+v %v", proposals, err)
		}
		if proposal := proposals.SpendingRuleProposals[0]; proposal.MatchText != "Corner Grocer" || proposal.FinanceTransactionCount != 1 ||
			proposal.SpendingCategoryID != first.ID || proposal.AheadOfSpendingRule != nil {
			test.Fatalf("proposed %+v", proposal)
		}
		if _, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a proposal with no spending category answered %v", err)
		}
		categorized, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: ids, SpendingCategoryID: first.ID, SpendingRules: confirmAll(proposals),
		})
		if err != nil {
			test.Fatal(err)
		}
		if len(categorized.SpendingRules) != 1 || categorized.SpendingRules[0].MatchText != "Corner Grocer" || categorized.SpendingRules[0].SpendingCategoryID != first.ID {
			test.Fatalf("saved %+v", categorized.SpendingRules)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 2 || rules[0].ID != cafeRule.ID || rules[1].RulePriority <= rules[0].RulePriority {
			test.Errorf("a rule nothing matched before goes after every rule: %+v %v", rules, err)
		}
		// Asked again, every match text is covered now.
		proposals, err = resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids, SpendingCategoryID: first.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 0 {
			test.Errorf("proposed again %+v %v", proposals, err)
		}
		// A rule to another spending category does not cover them, and the
		// rule proposed goes ahead of the one that wins now.
		proposals, err = resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: ids, SpendingCategoryID: second.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 2 {
			test.Fatalf("proposed for another spending category %+v %v", proposals, err)
		}
		for _, proposal := range proposals.SpendingRuleProposals {
			if proposal.MatchText == "CAFE ABROAD" && (proposal.AheadOfSpendingRule == nil || proposal.AheadOfSpendingRule.ID != cafeRule.ID) {
				test.Errorf("the CAFE ABROAD rule does not go ahead of the cafe rule: %+v", proposal)
			}
		}
	})
}

// What CategorizeTransactions saves is checked again: no rules without a
// spending category, none proposed for another, none too short or holding
// a number that changes, none twice, no more than the most, and none ahead
// of a rule that is gone. A refused call writes nothing.
func TestCategorizeTransactionsRefusesRulesNotAsProposed(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		first, second := twoSpendingCategories(test, ctx, resolver)
		ids := []string{transactions[0].ID, transactions[1].ID}
		tooMany := make([]ConfirmedSpendingRule, maximumSpendingRuleProposalCount+1)
		for index := range tooMany {
			tooMany[index] = ConfirmedSpendingRule{MatchText: fmt.Sprintf("Invented Shop %c%c", 'a'+index/26, 'a'+index%26), SpendingCategoryID: first.ID}
		}
		for _, refused := range []struct {
			name               string
			spendingCategoryId string
			spendingRules      []ConfirmedSpendingRule
			wanted             error
		}{
			{"no spending category", "", []ConfirmedSpendingRule{{MatchText: "Corner Grocer", SpendingCategoryID: first.ID}}, api.ErrInvalidArguments},
			{"another spending category", first.ID, []ConfirmedSpendingRule{{MatchText: "Corner Grocer", SpendingCategoryID: second.ID}}, api.ErrInvalidArguments},
			{"too short", first.ID, []ConfirmedSpendingRule{{MatchText: "XQ", SpendingCategoryID: first.ID}}, api.ErrInvalidArguments},
			{"mostly digits", first.ID, []ConfirmedSpendingRule{{MatchText: "AB 12345", SpendingCategoryID: first.ID}}, api.ErrInvalidArguments},
			{"a changing number", first.ID, []ConfirmedSpendingRule{{MatchText: "INVENTED SHOP REF 1234567", SpendingCategoryID: first.ID}}, api.ErrInvalidArguments},
			{"a date", first.ID, []ConfirmedSpendingRule{{MatchText: "INVENTED SHOP 2026-09-01", SpendingCategoryID: first.ID}}, api.ErrInvalidArguments},
			{"twice", first.ID, []ConfirmedSpendingRule{{MatchText: "Corner Grocer", SpendingCategoryID: first.ID}, {MatchText: "corner grocer", SpendingCategoryID: first.ID}}, api.ErrInvalidArguments},
			{"too many", first.ID, tooMany, api.ErrInvalidArguments},
			{"ahead of a rule that is gone", first.ID, []ConfirmedSpendingRule{{MatchText: "Corner Grocer", SpendingCategoryID: first.ID, AheadOfSpendingRuleID: "rule-invented-gone"}}, api.ErrConflict},
		} {
			if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
				FinanceTransactionIDs: ids, SpendingCategoryID: refused.spendingCategoryId, SpendingRules: refused.spendingRules,
			}); !errors.Is(err, refused.wanted) {
				test.Errorf("%s answered %v, not %v", refused.name, err, refused.wanted)
			}
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 0 {
			test.Errorf("a refused call saved rules: %+v %v", rules, err)
		}
		for _, financeTransaction := range transactions {
			after, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, financeTransaction.ID)
			if err != nil {
				test.Fatal(err)
			}
			if after.CategorizedBy == models.CategorizedByPerson {
				test.Errorf("a refused call categorized %+v", after)
			}
		}
	})
}

// An earlier rule, "eats", sends Zoomly Eats to Dining; the person picks Zoomly Eats
// rows and Transport. The new rule goes ahead of the Dining one, so the
// other Zoomly Eats transaction, the one the person did not pick, goes to
// Transport too; one the person filed themselves stays. The count said
// beforehand is the one transaction that changed.
func TestBulkRuleGoesAheadOfTheRuleThatWins(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	byProviderId := fixture.addFinanceTransactions(test, source.ID,
		finance.Transaction{ProviderTransactionID: "eats-picked-1", Description: "ZOOMLY EATS 01", MerchantName: "Zoomly Eats"},
		finance.Transaction{ProviderTransactionID: "eats-picked-2", Description: "ZOOMLY EATS 02", MerchantName: "Zoomly Eats"},
		finance.Transaction{ProviderTransactionID: "eats-other", Description: "ZOOMLY EATS 03", MerchantName: "Zoomly Eats"},
		finance.Transaction{ProviderTransactionID: "eats-filed", Description: "ZOOMLY EATS 04", MerchantName: "Zoomly Eats"},
	)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		dining := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryDining)
		transport := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryTransport)
		groceries := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryGroceries)
		if _, err := resolver.CategorizeTransaction(ctx, CategorizeTransactionArguments{
			FinanceTransactionID: byProviderId["eats-filed"].ID, SpendingCategoryID: groceries.ID,
		}); err != nil {
			test.Fatal(err)
		}
		diningRule, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "eats", SpendingCategoryID: dining.ID})
		if err != nil {
			test.Fatal(err)
		}
		picked := []string{byProviderId["eats-picked-1"].ID, byProviderId["eats-picked-2"].ID}
		proposals, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: picked, SpendingCategoryID: transport.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 1 {
			test.Fatalf("proposed %+v %v", proposals, err)
		}
		proposal := proposals.SpendingRuleProposals[0]
		if proposal.MatchText != "Zoomly Eats" || proposal.AheadOfSpendingRule == nil || proposal.AheadOfSpendingRule.ID != diningRule.ID ||
			proposal.FinanceTransactionCount != 2 || proposal.ChangedTransactionCount != 1 {
			test.Fatalf("proposed %+v ahead of %+v", proposal, proposal.AheadOfSpendingRule)
		}
		if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: picked, SpendingCategoryID: transport.ID, SpendingRules: confirmAll(proposals),
		}); err != nil {
			test.Fatal(err)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 2 || rules[0].MatchText != "Zoomly Eats" || rules[1].ID != diningRule.ID || rules[0].RulePriority >= rules[1].RulePriority {
			test.Errorf("the new rule is not tried first: %+v %v", rules, err)
		}
		other, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, byProviderId["eats-other"].ID)
		if err != nil || other.SpendingCategoryID != transport.ID || other.CategorizedBy != models.CategorizedBySpendingRule {
			test.Errorf("the other Zoomly Eats transaction still goes elsewhere: %+v %v", other, err)
		}
		filed, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, byProviderId["eats-filed"].ID)
		if err != nil || filed.SpendingCategoryID != groceries.ID || filed.CategorizedBy != models.CategorizedByPerson {
			test.Errorf("the rule took over the person's own choice: %+v %v", filed, err)
		}
	})
}

// A rule the same as the one being saved, further down and never applying,
// is reused rather than copied, and it moves to where the new rule was
// placed: ahead of the rule that would otherwise win, so it applies.
func TestBulkRuleReusingTheSameRuleMovesItAhead(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	byProviderId := fixture.addFinanceTransactions(test, source.ID,
		finance.Transaction{ProviderTransactionID: "eats-picked", Description: "ZOOMLY EATS 01", MerchantName: "Zoomly Eats"},
		finance.Transaction{ProviderTransactionID: "eats-other", Description: "ZOOMLY EATS 02", MerchantName: "Zoomly Eats"},
	)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		dining := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryDining)
		shopping := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryShopping)
		groceries := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryGroceries)
		zero, five := 0, 5
		diningRule, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "eats", SpendingCategoryID: dining.ID, RulePriority: &zero})
		if err != nil {
			test.Fatal(err)
		}
		staleRule, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "Zoomly Eats", SpendingCategoryID: shopping.ID, RulePriority: &five})
		if err != nil {
			test.Fatal(err)
		}
		picked := []string{byProviderId["eats-picked"].ID}
		proposals, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: picked, SpendingCategoryID: groceries.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 1 {
			test.Fatalf("proposed %+v %v", proposals, err)
		}
		if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: picked, SpendingCategoryID: groceries.ID, SpendingRules: confirmAll(proposals),
		}); err != nil {
			test.Fatal(err)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 2 || rules[0].ID != staleRule.ID || rules[1].ID != diningRule.ID ||
			rules[0].SpendingCategoryID != groceries.ID || rules[0].RulePriority >= rules[1].RulePriority {
			test.Fatalf("the reused rule is not ahead with the new category: %+v %v", rules, err)
		}
		other, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, byProviderId["eats-other"].ID)
		if err != nil || other.SpendingCategoryID != groceries.ID {
			test.Errorf("the other Zoomly Eats transaction still goes elsewhere: %+v %v", other, err)
		}
	})
}

// "eats" to Dining is tried before "zoomly" to Transport. Zoomly Eats
// rows picked for Transport are not covered by the Transport rule, since
// the Dining one wins for them: a rule is proposed, ahead of the Dining
// one. An Zoomly Trip row is covered by the Transport rule, the first that
// matches it, and gets none.
func TestBulkRuleCoverageFollowsRuleOrder(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	byProviderId := fixture.addFinanceTransactions(test, source.ID,
		finance.Transaction{ProviderTransactionID: "eats-picked", Description: "ZOOMLY EATS 01", MerchantName: "Zoomly Eats"},
		finance.Transaction{ProviderTransactionID: "eats-other", Description: "ZOOMLY EATS 02", MerchantName: "Zoomly Eats"},
		finance.Transaction{ProviderTransactionID: "trip-picked", Description: "ZOOMLY TRIP 01", MerchantName: "Zoomly Trip"},
	)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		dining := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryDining)
		transport := spendingCategoryNamed(test, ctx, resolver, finance.SpendingCategoryTransport)
		zero, one := 0, 1
		diningRule, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "eats", SpendingCategoryID: dining.ID, RulePriority: &zero})
		if err != nil {
			test.Fatal(err)
		}
		transportRule, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "zoomly", SpendingCategoryID: transport.ID, RulePriority: &one})
		if err != nil {
			test.Fatal(err)
		}
		picked := []string{byProviderId["eats-picked"].ID, byProviderId["trip-picked"].ID}
		proposals, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: picked, SpendingCategoryID: transport.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 1 {
			test.Fatalf("proposed %+v %v", proposals, err)
		}
		proposal := proposals.SpendingRuleProposals[0]
		if proposal.MatchText != "Zoomly Eats" || proposal.AheadOfSpendingRule == nil || proposal.AheadOfSpendingRule.ID != diningRule.ID || proposal.ChangedTransactionCount != 1 {
			test.Fatalf("proposed %+v", proposal)
		}
		if _, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: picked, SpendingCategoryID: transport.ID, SpendingRules: confirmAll(proposals),
		}); err != nil {
			test.Fatal(err)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 3 || rules[0].MatchText != "Zoomly Eats" || rules[1].ID != diningRule.ID || rules[2].ID != transportRule.ID {
			test.Fatalf("rules in order %+v %v", rules, err)
		}
		if rules[0].RulePriority >= rules[1].RulePriority || rules[1].RulePriority >= rules[2].RulePriority {
			test.Errorf("the priorities do not keep that order: %+v", rules)
		}
		other, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, byProviderId["eats-other"].ID)
		if err != nil || other.SpendingCategoryID != transport.ID {
			test.Errorf("the other Zoomly Eats transaction still goes to Dining: %+v %v", other, err)
		}
	})
}

// A rule saved in bulk can assign the transfer category, like any other,
// and marks the transfers it matches that the person did not pick.
func TestBulkRuleToTheTransferCategory(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	byProviderId := fixture.addFinanceTransactions(test, source.ID,
		finance.Transaction{ProviderTransactionID: "payment-picked", Description: "INVENTED CARD AUTOPAY", Amount: "-300.0000"},
		finance.Transaction{ProviderTransactionID: "payment-other", Description: "INVENTED CARD AUTOPAY", Amount: "-250.0000"},
	)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var transfer *models.SpendingCategory
		categories, err := resolver.SpendingCategories(ctx)
		if err != nil {
			test.Fatal(err)
		}
		for _, category := range categories {
			if category.IsTransfer {
				transfer = category
			}
		}
		if transfer == nil {
			test.Fatalf("no transfer category in %+v", categories)
		}
		picked := []string{byProviderId["payment-picked"].ID}
		proposals, err := resolver.ProposeSpendingRules(ctx, ProposeSpendingRulesArguments{FinanceTransactionIDs: picked, SpendingCategoryID: transfer.ID})
		if err != nil || len(proposals.SpendingRuleProposals) != 1 || proposals.SpendingRuleProposals[0].ChangedTransactionCount != 1 {
			test.Fatalf("proposed %+v %v", proposals, err)
		}
		categorized, err := resolver.CategorizeTransactions(ctx, CategorizeTransactionsArguments{
			FinanceTransactionIDs: picked, SpendingCategoryID: transfer.ID, SpendingRules: confirmAll(proposals),
		})
		if err != nil || len(categorized.SpendingRules) != 1 || categorized.SpendingRules[0].SpendingCategoryID != transfer.ID {
			test.Fatalf("saved %+v %v", categorized, err)
		}
		other, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, byProviderId["payment-other"].ID)
		if err != nil || other.SpendingCategoryID != transfer.ID || other.CategorizedBy != models.CategorizedBySpendingRule {
			test.Errorf("the other payment is not marked a transfer by the rule: %+v %v", other, err)
		}
	})
}

// Each distinct match text is its own proposal: a short one never absorbs
// a longer one. A mirrored copy proposes nothing. One too short or mostly
// digits, or holding a number that changes each time, is left out and
// counted. Coverage is by the rule that applies first, and a rule goes
// ahead of the earliest rule that now wins for its transactions. The most
// matched first, at most the maximum, the rest counted.
func TestProposeSpendingRules(test *testing.T) {
	test.Parallel()
	transaction := func(id, merchantName, description string) *models.FinanceTransaction {
		return &models.FinanceTransaction{ID: id, MerchantName: merchantName, Description: description}
	}
	mirrored := transaction("transaction-mirrored", "Mirrored Fee Example", "")
	mirrored.DuplicateOfTransactionID = "transaction-counted"
	financeTransactions := []*models.FinanceTransaction{
		transaction("transaction-bistro-1", "Invented Bistro", "INVENTED BISTRO 0101"),
		transaction("transaction-bistro-2", "invented bistro", "INVENTED BISTRO 0102"),
		transaction("transaction-square", "XQ *INVENTED CAFE", ""),
		transaction("transaction-short", "XQ", ""),
		transaction("transaction-digits", "", "AB 1234 5678"),
		transaction("transaction-reference", "", "INVENTED SHOP REF 12345678"),
		transaction("transaction-dated", "", "INVENTED PAYMENT 2026-09-01"),
		transaction("transaction-slashed", "", "INVENTED GYM 9/14"),
		transaction("transaction-hours", "Invented 24/7 Market", ""),
		transaction("transaction-eats", "Zoomly Eats", ""),
		transaction("transaction-trip", "Zoomly Trip", ""),
		transaction("transaction-blank", "", ""),
		mirrored,
	}
	spendingRules := []*models.SpendingRule{
		{ID: "rule-eats", MatchText: "zoomly eats", SpendingCategoryID: "category-dining", RulePriority: 0},
		{ID: "rule-zoomly", MatchText: "zoomly", SpendingCategoryID: "category-transport", RulePriority: 1},
		{ID: "rule-bistro", MatchText: "bistro", SpendingCategoryID: "category-groceries", RulePriority: 2},
	}
	firstMatchingRuleIds := map[string]string{
		"transaction-eats": "rule-eats", "transaction-trip": "rule-zoomly",
		"transaction-bistro-1": "rule-bistro", "transaction-bistro-2": "rule-bistro",
	}
	proposals := proposeSpendingRules(financeTransactions, spendingRules, firstMatchingRuleIds, "category-transport")
	var have []string
	for _, proposal := range proposals.SpendingRuleProposals {
		aheadOf := ""
		if proposal.AheadOfSpendingRule != nil {
			aheadOf = " ahead of " + proposal.AheadOfSpendingRule.ID
		}
		have = append(have, fmt.Sprintf("%s:%d%s", proposal.MatchText, proposal.FinanceTransactionCount, aheadOf))
	}
	wanted := []string{"Invented Bistro:2 ahead of rule-bistro", "Invented 24/7 Market:1", "XQ *INVENTED CAFE:1", "Zoomly Eats:1 ahead of rule-eats"}
	if fmt.Sprint(have) != fmt.Sprint(wanted) {
		test.Errorf("proposed %v, not %v", have, wanted)
	}
	if proposals.TooGenericMatchTextCount != 2 || proposals.ChangingNumberMatchTextCount != 3 || proposals.OverLimitMatchTextCount != 0 {
		test.Errorf("left out %+v", proposals)
	}

	// At most the maximum, the rest counted.
	var many []*models.FinanceTransaction
	for index := 0; index < maximumSpendingRuleProposalCount+7; index++ {
		many = append(many, transaction(fmt.Sprintf("transaction-%d", index), fmt.Sprintf("Invented Shop %c%c", 'a'+index/26, 'a'+index%26), ""))
	}
	proposals = proposeSpendingRules(many, nil, nil, "category-transport")
	if len(proposals.SpendingRuleProposals) != maximumSpendingRuleProposalCount || proposals.OverLimitMatchTextCount != 7 {
		test.Errorf("proposed %d, left %d over", len(proposals.SpendingRuleProposals), proposals.OverLimitMatchTextCount)
	}
}

// A confirmed rule goes ahead of the rule it names, or after every rule.
// Existing rules move back only as far as needed, keep their order, and
// two that shared a priority still share one; a named rule that is gone
// refuses them all.
func TestPlaceSpendingRules(test *testing.T) {
	test.Parallel()
	existing := []*models.SpendingRule{
		{ID: "rule-a", RulePriority: 0}, {ID: "rule-b", RulePriority: 1}, {ID: "rule-c", RulePriority: 1}, {ID: "rule-d", RulePriority: 5},
	}
	placed, moved, err := placeSpendingRules("agent-invented", existing, []ConfirmedSpendingRule{
		{MatchText: "Invented Ahead", SpendingCategoryID: "category-one", AheadOfSpendingRuleID: "rule-b"},
		{MatchText: "Invented After", SpendingCategoryID: "category-one"},
	})
	if err != nil {
		test.Fatal(err)
	}
	if len(placed) != 2 || placed[0].RulePriority != 1 || placed[1].RulePriority != 6 || placed[0].AgentID != "agent-invented" {
		test.Errorf("placed %+v %+v", placed[0], placed[1])
	}
	if fmt.Sprint(moved) != fmt.Sprint(map[string]int{"rule-b": 2, "rule-c": 2}) {
		test.Errorf("moved %v", moved)
	}

	placed, moved, err = placeSpendingRules("agent-invented", existing, []ConfirmedSpendingRule{
		{MatchText: "Invented First", SpendingCategoryID: "category-one", AheadOfSpendingRuleID: "rule-a"},
	})
	if err != nil || len(placed) != 1 || placed[0].RulePriority != 0 || fmt.Sprint(moved) != fmt.Sprint(map[string]int{"rule-a": 1, "rule-b": 2, "rule-c": 2}) {
		test.Errorf("placed first %+v, moved %v, %v", placed, moved, err)
	}

	if _, _, err := placeSpendingRules("agent-invented", existing, []ConfirmedSpendingRule{
		{MatchText: "Invented Late", SpendingCategoryID: "category-one", AheadOfSpendingRuleID: "rule-gone"},
	}); !errors.Is(err, api.ErrConflict) {
		test.Errorf("a rule ahead of one that is gone answered %v", err)
	}
}
