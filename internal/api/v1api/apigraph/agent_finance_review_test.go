package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A finance account names its institution: a Plaid source's, kept when it
// was linked, or for SimpleFIN, which keeps none on the source, the one
// the account's own provider metadata names.
func TestFinanceAccountsNameTheirInstitution(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	resolver := fixture.resolver
	var simpleFin, plaid *FinanceSourceView
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if simpleFin, err = resolver.LinkSimpleFIN(ctx, LinkSimpleFINArguments{SetupToken: "aW52ZW50ZWQtc2V0dXAtdG9rZW4="}); err != nil {
			test.Fatal(err)
		}
		if plaid, err = resolver.CompleteFinanceLink(ctx, CompleteFinanceLinkArguments{PublicToken: "public-invented", InstitutionID: "institution-invented"}); err != nil {
			test.Fatal(err)
		}
		for sourceId, account := range map[string]finance.Account{
			simpleFin.ID: {
				ProviderAccountID: "account-invented-bridge", AccountName: "Everyday Checking", AccountKind: "depository", CurrencyCode: "USD",
				CurrentBalance: "100.0000", BalanceAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
				ProviderMetadata: json.RawMessage(`{"id":"account-invented-bridge","org":{"name":"Invented Credit Union","domain":"example.org"}}`),
			},
			plaid.ID: {
				ProviderAccountID: "account-invented-plaid", AccountName: "Savings", AccountKind: "depository", CurrencyCode: "USD",
				CurrentBalance: "200.0000", BalanceAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
			},
		} {
			if _, err := tx.ApplyFinanceSync(fixture.ownerAgent.ID, sourceId, &finance.SyncResult{Accounts: []finance.Account{account}}, "2026-09-20"); err != nil {
				test.Fatal(err)
			}
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		accounts, err := resolver.FinanceAccounts(ctx, FinanceAccountsArguments{})
		if err != nil || len(accounts) != 2 {
			test.Fatalf("accounts %v %v", accounts, err)
		}
		for _, account := range accounts {
			wanted := map[string]string{simpleFin.ID: "Invented Credit Union", plaid.ID: "Invented Savings Bank"}[account.SourceID]
			if account.InstitutionName != wanted {
				test.Errorf("the account of %s names %q, not %q", account.SourceID, account.InstitutionName, wanted)
			}
		}
		sources, err := resolver.FinanceSources(ctx)
		if err != nil {
			test.Fatal(err)
		}
		for _, source := range sources {
			if source.ID == simpleFin.ID && source.InstitutionName != "Invented Credit Union" {
				test.Errorf("the SimpleFIN source names %q", source.InstitutionName)
			}
		}
	})
}

// An asset is made with its first value in one call; a value that is not
// an amount makes nothing at all.
func TestCreateAssetWithItsFirstValue(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		car, err := resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the car", AssetKind: "vehicle", CurrencyCode: "USD", Value: "18000", ValuedOn: "2026-08-30"})
		if err != nil {
			test.Fatal(err)
		}
		if car.LatestValuation == nil || car.LatestValuation.Value != "18000.0000" || car.LatestValuation.ValuedOn != "2026-08-30" ||
			car.LatestValuation.ValuationSource != models.ValuationSourceManual {
			test.Errorf("the first value is %+v", car.LatestValuation)
		}
		history, err := resolver.AssetHistory(ctx, AssetArguments{AssetID: car.ID})
		if err != nil || len(history.AssetValuations) != 1 {
			test.Errorf("the history is %+v %v", history, err)
		}
		for _, refused := range []CreateAssetArguments{
			{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD", Value: "a lot"},
			{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD", Value: "9000", ValuedOn: "last spring"},
			{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD", ValuedOn: "2026-08-30"},
			{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD", ValuationSource: "agent_estimate", Value: "9000"},
		} {
			if _, err := resolver.CreateAsset(ctx, refused); !errors.Is(err, api.ErrInvalidArguments) {
				test.Errorf("%+v answered %v", refused, err)
			}
		}
		assets, err := resolver.Assets(ctx, AssetsArguments{})
		if err != nil || len(assets) != 1 {
			test.Errorf("a refused call left an asset behind: %+v %v", assets, err)
		}
	})
}

// The reporting currency says whether the person chose it or it falls
// back, and to what.
func TestReportingCurrencyFallsBackAndSaysSo(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		reporting, err := resolver.ReportingCurrency(ctx)
		if err != nil || reporting.ReportingCurrencyCode != "" || reporting.IsChosen {
			test.Errorf("with nothing: %+v %v", reporting, err)
		}
		if _, err := resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the flat", AssetKind: "property", CurrencyCode: "EUR"}); err != nil {
			test.Fatal(err)
		}
		reporting, err = resolver.ReportingCurrency(ctx)
		if err != nil || reporting.ReportingCurrencyCode != "EUR" || reporting.IsChosen {
			test.Errorf("from the first asset: %+v %v", reporting, err)
		}
		if _, err := resolver.SetReportingCurrency(ctx, SetReportingCurrencyArguments{CurrencyCode: "JPY"}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		reporting, err := resolver.ReportingCurrency(ctx)
		if err != nil || reporting.ReportingCurrencyCode != "JPY" || !reporting.IsChosen {
			test.Errorf("chosen: %+v %v", reporting, err)
		}
	})
}

// A categorization asking for a spending rule it cannot have is refused
// before anything is written, so the transaction keeps what it had.
func TestCategorizeTransactionRefusedChangesNothing(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	isRuleWanted := true
	var before *models.FinanceTransaction
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if before, err = tx.GetFinanceTransaction(fixture.ownerAgent.ID, transactions[0].ID); err != nil {
			test.Fatal(err)
		}
		categories, err := resolver.SpendingCategories(ctx)
		if err != nil || len(categories) == 0 {
			test.Fatalf("categories %v %v", categories, err)
		}
		for _, refused := range []CategorizeTransactionArguments{
			{FinanceTransactionID: transactions[0].ID, ShouldCreateSpendingRule: &isRuleWanted},
			{FinanceTransactionID: transactions[0].ID, SpendingCategoryID: "spending-category-invented", ShouldCreateSpendingRule: &isRuleWanted},
			{FinanceTransactionID: transactions[0].ID, SpendingCategoryID: "spending-category-invented"},
		} {
			if _, err := resolver.CategorizeTransaction(ctx, refused); !errors.Is(err, api.ErrInvalidArguments) {
				test.Errorf("%+v answered %v", refused, err)
			}
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		after, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, transactions[0].ID)
		if err != nil {
			test.Fatal(err)
		}
		if after.SpendingCategoryID != before.SpendingCategoryID || after.CategorizedBy != before.CategorizedBy {
			test.Errorf("a refused call changed the transaction: %+v, was %+v", after, before)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 0 {
			test.Errorf("a refused call made spending rules: %+v %v", rules, err)
		}
	})
}

// Finishing a repair clears the sign-in flag and leaves the last error for
// the next sync to clear, so a repair that did not take still says so.
func TestCompleteFinanceRepairKeepsTheLastError(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	if err := fixture.database.Transaction(func(tx db.Transaction) error {
		stored, err := tx.LockAgentSource(fixture.ownerAgent.ID, source.ID)
		if err != nil {
			return err
		}
		if stored.Cursor == nil {
			stored.Cursor = map[string]any{}
		}
		stored.Cursor[models.FinanceCursorIsSignInRequired] = true
		stored.LastError = "the institution needs you to sign in again"
		_, err = tx.PutAgentSource(stored)
		return err
	}); err != nil {
		test.Fatal(err)
	}
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		repaired, err := fixture.resolver.CompleteFinanceRepair(ctx, FinanceSourceArguments{SourceID: source.ID})
		if err != nil {
			test.Fatal(err)
		}
		if repaired.IsSignInRequired || repaired.LastError == "" {
			test.Errorf("repaired %+v", repaired)
		}
	})
}

// Transfer is a spending category: categorizing a transaction as it marks
// a transfer, with a spending rule for its merchant when asked, and any
// other spending category takes the mark away. The transfer category is
// built in, so deleting it, making it income or budgeting it is refused.
func TestCategorizeTransactionAsTransfer(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	_, _, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	isRuleWanted, isIncome := true, true
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		categories, err := resolver.SpendingCategories(ctx)
		if err != nil {
			test.Fatal(err)
		}
		var transferCategory, otherCategory *models.SpendingCategory
		for _, category := range categories {
			if category.IsTransfer {
				transferCategory = category
			} else if otherCategory == nil && !category.IsIncome {
				otherCategory = category
			}
		}
		if transferCategory == nil || otherCategory == nil {
			test.Fatalf("the transfer category and another: %+v", categories)
		}
		categorized, err := resolver.CategorizeTransaction(ctx, CategorizeTransactionArguments{
			FinanceTransactionID: transactions[0].ID, SpendingCategoryID: transferCategory.ID, ShouldCreateSpendingRule: &isRuleWanted,
		})
		if err != nil || categorized.FinanceTransaction.SpendingCategoryID != transferCategory.ID ||
			categorized.FinanceTransaction.CategorizedBy != models.CategorizedByPerson ||
			categorized.SpendingRule == nil || categorized.SpendingRule.SpendingCategoryID != transferCategory.ID {
			test.Fatalf("marked a transfer by the person, with a rule to the transfer category: %+v %v", categorized, err)
		}
		recategorized, err := resolver.CategorizeTransaction(ctx, CategorizeTransactionArguments{
			FinanceTransactionID: transactions[0].ID, SpendingCategoryID: otherCategory.ID,
		})
		if err != nil || recategorized.FinanceTransaction.SpendingCategoryID != otherCategory.ID {
			test.Errorf("another spending category takes the mark away: %+v %v", recategorized, err)
		}
		if _, err := resolver.DeleteSpendingCategory(ctx, SpendingCategoryArguments{SpendingCategoryID: transferCategory.ID}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("deleting the transfer category answered %v", err)
		}
		if _, err := resolver.UpdateSpendingCategory(ctx, UpdateSpendingCategoryArguments{SpendingCategoryID: transferCategory.ID, IsIncome: &isIncome}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("making the transfer category income answered %v", err)
		}
		if _, err := resolver.SetBudget(ctx, SetBudgetArguments{SpendingCategoryID: transferCategory.ID, MonthlyAmount: "100", CurrencyCode: "USD"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("budgeting the transfer category answered %v", err)
		}
		if _, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "online payment"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a spending rule with no spending category answered %v", err)
		}
	})
}
