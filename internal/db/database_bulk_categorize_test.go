package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// Categorizing several at once is the person's choice for every one, in
// one statement, all or none: an id that is none of the agent's, or a
// spending category that is not theirs, writes nothing. An empty spending
// category takes it away, and a rule made afterwards leaves them alone.
func TestCategorizeTransactionsByPerson(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "bulk-categorize")
	stranger := createFinanceFixture(t, database, "bulk-categorize-stranger")
	applyFinanceSync(t, database, fixture, sampleFinanceSync(), "2026-09-12")
	applyFinanceSync(t, database, stranger, sampleFinanceSync(), "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		if _, err := tx.EnsureDefaultSpendingCategories(stranger.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		strangerByName := spendingCategoryIdsByName(t, tx, stranger.agentId)
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		strangerFound := financeTransactionsByProviderId(t, tx, stranger.agentId)
		ids := []string{found["transaction-grocer"].ID, found["transaction-diner"].ID}

		if _, err := tx.CategorizeTransactionsByPerson(fixture.agentId, append(ids, strangerFound["transaction-grocer"].ID), byName[finance.SpendingCategoryDining]); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("somebody else's transaction among them answered %v", err)
		}
		if _, err := tx.CategorizeTransactionsByPerson(fixture.agentId, ids, strangerByName[finance.SpendingCategoryDining]); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("somebody else's spending category answered %v", err)
		}
		for _, financeTransaction := range financeTransactionsByProviderId(t, tx, fixture.agentId) {
			if financeTransaction.CategorizedBy == models.CategorizedByPerson {
				t.Errorf("a refused call wrote %+v", financeTransaction)
			}
		}

		writtenCount, err := tx.CategorizeTransactionsByPerson(fixture.agentId, append(ids, ids[0]), byName[finance.SpendingCategoryDining])
		if err != nil || writtenCount != 2 {
			t.Fatalf("CategorizeTransactionsByPerson: %d %v", writtenCount, err)
		}
		if _, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "grocer", SpendingCategoryID: byName[finance.SpendingCategoryGroceries]}); err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		for _, providerId := range []string{"transaction-grocer", "transaction-diner"} {
			if chosen := found[providerId]; chosen.SpendingCategoryID != byName[finance.SpendingCategoryDining] || chosen.CategorizedBy != models.CategorizedByPerson {
				t.Errorf("%s is not the person's choice: %+v", providerId, chosen)
			}
		}
		if salary := found["transaction-salary"]; salary.CategorizedBy == models.CategorizedByPerson {
			t.Errorf("a transaction not named was written: %+v", salary)
		}

		// No spending category is not the person's to choose: what fits
		// nothing is the other category.
		if _, err := tx.CategorizeTransactionsByPerson(fixture.agentId, ids, ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("CategorizeTransactionsByPerson with none is refused: %v", err)
		}
		found = financeTransactionsByProviderId(t, tx, fixture.agentId)
		if grocer := found["transaction-grocer"]; grocer.SpendingCategoryID != byName[finance.SpendingCategoryDining] {
			t.Errorf("a refused none writes nothing: %+v", grocer)
		}
		otherCategory, err := tx.EnsureOtherSpendingCategory(fixture.agentId)
		if err != nil {
			t.Fatalf("EnsureOtherSpendingCategory: %s", err)
		}
		if writtenCount, err := tx.CategorizeTransactionsByPerson(fixture.agentId, ids, otherCategory.ID); err != nil || writtenCount != 2 {
			t.Fatalf("CategorizeTransactionsByPerson with other: %d %v", writtenCount, err)
		}
	})
}

// The rule that applies first to a transaction is judged as
// ApplySpendingRules judges it, amounts included; a proposed rule's count
// is the transactions it would give another spending category once placed,
// leaving out the person's choices and the ones named; and priorities move
// without the rules being applied in between.
func TestSpendingRuleProposalQueries(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "bulk-proposal-queries")
	result := sampleFinanceSync()
	result.Added = append(result.Added,
		finance.Transaction{ProviderTransactionID: "grocer-large", ProviderAccountID: "account-card", PostedOn: "2026-09-11",
			Amount: "-120.00", CurrencyCode: "USD", Description: "CORNER GROCER 0413", MerchantName: "Corner Grocer"},
		finance.Transaction{ProviderTransactionID: "grocer-chosen", ProviderAccountID: "account-card", PostedOn: "2026-09-11",
			Amount: "-20.00", CurrencyCode: "USD", Description: "CORNER GROCER 0414", MerchantName: "Corner Grocer"},
	)
	applyFinanceSync(t, database, fixture, result, "2026-09-12")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.EnsureDefaultSpendingCategories(fixture.agentId); err != nil {
			t.Fatalf("EnsureDefaultSpendingCategories: %s", err)
		}
		byName := spendingCategoryIdsByName(t, tx, fixture.agentId)
		found := financeTransactionsByProviderId(t, tx, fixture.agentId)
		if _, err := tx.SetTransactionCategorization(fixture.agentId, found["grocer-chosen"].ID, byName[finance.SpendingCategoryHealth], models.CategorizedByPerson, nil); err != nil {
			t.Fatalf("SetTransactionCategorization: %s", err)
		}
		large, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "grocer",
			SpendingCategoryID: byName[finance.SpendingCategoryShopping], MaximumAmount: "-100", RulePriority: 0})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}
		broad, err := tx.CreateSpendingRule(&models.SpendingRule{AgentID: fixture.agentId, MatchText: "corner",
			SpendingCategoryID: byName[finance.SpendingCategoryGroceries], RulePriority: 1})
		if err != nil {
			t.Fatalf("CreateSpendingRule: %s", err)
		}

		firstMatching, err := tx.FirstMatchingSpendingRules(fixture.agentId, []string{found["transaction-grocer"].ID, found["grocer-large"].ID, found["transaction-diner"].ID})
		if err != nil {
			t.Fatalf("FirstMatchingSpendingRules: %s", err)
		}
		if len(firstMatching) != 2 || firstMatching[found["transaction-grocer"].ID] != broad.ID || firstMatching[found["grocer-large"].ID] != large.ID {
			t.Errorf("first matching %v", firstMatching)
		}

		// Ahead of the broad rule, a Dining rule for Corner Grocer takes
		// the small one only (the large one stays with the bounded rule
		// tried first); ahead of the bounded rule it takes both; after
		// every rule it takes none. The person's choice is never counted,
		// nor a transaction named.
		changedCounts, err := tx.CountSpendingRuleChanges(fixture.agentId, []*db.ProposedSpendingRule{
			{MatchText: "Corner Grocer", SpendingCategoryID: byName[finance.SpendingCategoryDining], AheadOfSpendingRuleID: broad.ID},
			{MatchText: "corner grocer", SpendingCategoryID: byName[finance.SpendingCategoryDining], AheadOfSpendingRuleID: large.ID},
			{MatchText: "Corner Grocer", SpendingCategoryID: byName[finance.SpendingCategoryDining]},
			{MatchText: "Lakeside Diner", SpendingCategoryID: byName[finance.SpendingCategoryDining]},
		}, []string{found["transaction-diner"].ID})
		if err != nil {
			t.Fatalf("CountSpendingRuleChanges: %s", err)
		}
		if len(changedCounts) != 4 || changedCounts[0] != 1 || changedCounts[1] != 2 || changedCounts[2] != 0 || changedCounts[3] != 0 {
			t.Errorf("changed counts %v", changedCounts)
		}
		// Proposed for the spending category they have already, nothing
		// changes.
		changedCounts, err = tx.CountSpendingRuleChanges(fixture.agentId, []*db.ProposedSpendingRule{
			{MatchText: "Corner Grocer", SpendingCategoryID: byName[finance.SpendingCategoryGroceries], AheadOfSpendingRuleID: broad.ID},
		}, nil)
		if err != nil || len(changedCounts) != 1 || changedCounts[0] != 0 {
			t.Errorf("changed counts to the same spending category %v %v", changedCounts, err)
		}

		if err := tx.SetSpendingRulePriorities(fixture.agentId, map[string]int{large.ID: 5, broad.ID: 3}); err != nil {
			t.Fatalf("SetSpendingRulePriorities: %s", err)
		}
		if grocer := financeTransactionsByProviderId(t, tx, fixture.agentId)["grocer-large"]; grocer.SpendingCategoryID != byName[finance.SpendingCategoryShopping] {
			t.Errorf("moving priorities applied the rules: %+v", grocer)
		}
		rules, err := tx.ListSpendingRules(fixture.agentId)
		if err != nil || len(rules) != 2 || rules[0].ID != broad.ID || rules[0].RulePriority != 3 || rules[1].RulePriority != 5 {
			t.Errorf("rules after moving %+v %v", rules, err)
		}
		if err := tx.SetSpendingRulePriorities(fixture.agentId, map[string]int{"rule-invented-elsewhere": 1}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("moving a rule that is none of the agent's answered %v", err)
		}
	})
}
