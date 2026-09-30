package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A newer provider category mapping judges again what the older one put in
// other: a car payment moves to loans on the next sync. A transaction the
// person categorized stays where they put it, and the version is recorded
// so the next sync does not do it again.
func TestFinanceSyncRemapsWhatAnOlderMappingCategorized(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	today := time.Now().UTC().Format(time.DateOnly)
	carPayment := inventedTransaction("car-payment", today, "-450.00", "INVENTED AUTO FINANCE", "Invented Auto Finance", "LOAN_PAYMENTS_CAR_PAYMENT")
	carPayment.ProviderCategoryPrimary = "LOAN_PAYMENTS"
	schoolFee := inventedTransaction("school-fee", today, "-300.00", "INVENTED ACADEMY", "Invented Academy", "GENERAL_SERVICES_EDUCATION")
	schoolFee.ProviderCategoryPrimary = "GENERAL_SERVICES"
	fixture.provider.result = &finance.SyncResult{
		Accounts: []finance.Account{inventedAccount()}, Added: []finance.Transaction{carPayment, schoolFee}, NextCursor: "cursor-one",
	}
	fixture.sync(t)

	// As the older mapping left them: both in other, the school fee by the
	// person, and the version not yet recorded.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		categoryIdByName := map[string]string{}
		categories, err := tx.ListSpendingCategories(fixture.agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, category := range categories {
			categoryIdByName[category.SpendingCategoryName] = category.ID
		}
		page, err := tx.ListFinanceTransactions(fixture.agent.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, transaction := range page.FinanceTransactions {
			categorizedBy := models.CategorizedByProviderCategoryMapping
			if transaction.ProviderTransactionID == "school-fee" {
				categorizedBy = models.CategorizedByPerson
			}
			if _, err := tx.SetTransactionCategorization(fixture.agent.ID, transaction.ID, categoryIdByName[finance.SpendingCategoryOther], categorizedBy, nil); err != nil {
				t.Fatal(err)
			}
		}
	})
	source := fixture.reload(t)
	cursor := map[string]any{}
	for key, value := range source.Cursor {
		cursor[key] = value
	}
	delete(cursor, models.FinanceCursorProviderCategoryMappingVersion)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		due := time.Now()
		if err := tx.MarkAgentSourceRun(source.ID, cursor, db.SourceCounts{}, false, "", &due); err != nil {
			t.Fatal(err)
		}
	})

	fixture.provider.result.Added = nil
	source = fixture.sync(t)
	if version, _ := source.Cursor[models.FinanceCursorProviderCategoryMappingVersion].(float64); int(version) != finance.ProviderCategoryMappingVersion {
		t.Errorf("the mapping version is recorded: %+v", source.Cursor)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		categoryNameById := map[string]string{}
		categories, _ := tx.ListSpendingCategories(fixture.agent.ID)
		for _, category := range categories {
			categoryNameById[category.ID] = category.SpendingCategoryName
		}
		page, _ := tx.ListFinanceTransactions(fixture.agent.ID, nil)
		for _, transaction := range page.FinanceTransactions {
			want := finance.SpendingCategoryLoans
			if transaction.ProviderTransactionID == "school-fee" {
				want = finance.SpendingCategoryOther
			}
			if got := categoryNameById[transaction.SpendingCategoryID]; got != want {
				t.Errorf("%s is in %q, want %q", transaction.ProviderTransactionID, got, want)
			}
		}
	})
}
