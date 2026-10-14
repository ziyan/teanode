package db_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A tax year's settings are put and read back, an amount out of range
// refused.
func TestTaxYearSettingIsPutAndRead(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if found, err := tx.GetTaxYearSetting(fixture.agentId, 2026); err != nil || found != nil {
			t.Fatalf("none yet: %+v %v", found, err)
		}
		saved, err := tx.PutTaxYearSetting(&models.TaxYearSetting{AgentID: fixture.agentId, TaxYear: 2026, ChildCount: 1, DependentCareExpenseAmount: "5000",
			OtherIncomeAmount: "4500.50", EstimatedLongTermGainAmount: "-1200", EstimatedGainNote: "from an example brokerage"})
		if err != nil || saved.ChildCount != 1 || saved.OtherIncomeAmount != "4500.5000" || saved.EstimatedLongTermGainAmount != "-1200.0000" ||
			saved.EstimatedShortTermGainAmount != "" || saved.CharitableContributionAmount != "0.0000" {
			t.Fatalf("PutTaxYearSetting: %+v %v", saved, err)
		}
		if _, err := tx.PutTaxYearSetting(&models.TaxYearSetting{AgentID: fixture.agentId, TaxYear: 2026, ChildCount: 21}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("21 children is refused: %v", err)
		}
		if _, err := tx.PutTaxYearSetting(&models.TaxYearSetting{AgentID: fixture.agentId, TaxYear: 2026, PropertyTaxAmount: "-5"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a negative property tax is refused: %v", err)
		}
	})
}

// Interest and dividends are added up by account and kind: what the
// provider calls them, or what a description says when it names no
// category; money out, other categories and other currencies are not.
func TestAccountInvestmentIncomeIsAddedUp(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	fixture := createFinanceFixture(t, database, "robin")
	sync := &finance.SyncResult{
		Accounts: []finance.Account{
			{ProviderAccountID: "account-savings", AccountName: "Example Savings", AccountKind: finance.AccountKindDepository, CurrencyCode: "USD",
				CurrentBalance: "100", ProviderMetadata: json.RawMessage(`{"subtype":"savings"}`)},
			{ProviderAccountID: "account-ira", AccountName: "Example IRA", AccountKind: finance.AccountKindInvestment, CurrencyCode: "USD",
				CurrentBalance: "100", ProviderMetadata: json.RawMessage(`{"subtype":"ira"}`)},
		},
		Added: []finance.Transaction{
			{ProviderTransactionID: "interest-1", ProviderAccountID: "account-savings", PostedOn: "2026-03-31", Amount: "4.10", CurrencyCode: "USD",
				Description: "Interest Paid", ProviderCategoryPrimary: "INCOME", ProviderCategoryDetailed: "INCOME_INTEREST_EARNED"},
			{ProviderTransactionID: "interest-2", ProviderAccountID: "account-savings", PostedOn: "2026-04-30", Amount: "4.20", CurrencyCode: "USD",
				Description: "INTEREST PAYMENT"},
			{ProviderTransactionID: "fee", ProviderAccountID: "account-savings", PostedOn: "2026-04-30", Amount: "-2.00", CurrencyCode: "USD",
				Description: "INTEREST CHARGE"},
			{ProviderTransactionID: "dividend", ProviderAccountID: "account-ira", PostedOn: "2026-03-15", Amount: "50.00", CurrencyCode: "USD",
				Description: "Dividend EXAMPLE FUND", ProviderCategoryPrimary: "INCOME", ProviderCategoryDetailed: "INCOME_DIVIDENDS"},
			{ProviderTransactionID: "last-year", ProviderAccountID: "account-savings", PostedOn: "2025-12-31", Amount: "3.90", CurrencyCode: "USD",
				Description: "Interest Paid", ProviderCategoryPrimary: "INCOME", ProviderCategoryDetailed: "INCOME_INTEREST_EARNED"},
		},
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.ApplyFinanceSync(fixture.agentId, fixture.sourceId, sync, "2026-05-01"); err != nil {
			t.Fatalf("ApplyFinanceSync: %s", err)
		}
		incomes, err := tx.ListAccountInvestmentIncome(fixture.agentId, "2026-01-01", "2026-12-31")
		if err != nil || len(incomes) != 2 {
			t.Fatalf("two accounts' income: %+v %v", incomes, err)
		}
		if ira := incomes[0]; ira.AccountName != "Example IRA" || ira.IncomeKind != "dividend" || ira.Amount != "50.0000" || ira.AccountSubtype != "ira" {
			t.Fatalf("the IRA's dividend: %+v", ira)
		}
		if savings := incomes[1]; savings.IncomeKind != "interest" || savings.Amount != "8.3000" || savings.TransactionCount != 2 {
			t.Fatalf("the savings' interest, by category and by description: %+v", savings)
		}
	})
}
