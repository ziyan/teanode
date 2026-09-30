package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// Invented values that must never come back out of the API.
const (
	inventedPlaidSecret       = "plaid-secret-invented-0001"
	inventedSimpleFinPassword = "bridge-password-invented-0002"
	inventedPlaidCredential   = "access-sandbox-invented-0003"
	inventedSimpleFinAddress  = "https://person:" + inventedSimpleFinPassword + "@bridge.example.net/simplefin"

	// Plaid credentials a person brings from elsewhere: one Plaid knows,
	// one it does not, and one asked about while Plaid cannot be reached.
	importedPlaidCredential    = "access-sandbox-invented-0005"
	unknownPlaidCredential     = "access-sandbox-invented-0006"
	unreachablePlaidCredential = "access-sandbox-invented-0007"
)

// fakeLinker is Plaid, answering to order and remembering what it ended.
type fakeLinker struct {
	mutex   sync.Mutex
	removed []string
}

func (self *fakeLinker) CreateLinkToken(_ context.Context, personReference string, credentialForRepair string) (string, error) {
	return "link-token-for-" + personReference, nil
}

func (self *fakeLinker) ExchangePublicToken(_ context.Context, publicToken string) (string, string, error) {
	return inventedPlaidCredential, "item-invented-" + publicToken, nil
}

func (self *fakeLinker) InstitutionName(_ context.Context, institutionId string) (string, error) {
	return "Invented Savings Bank", nil
}

// DescribeCredential knows the credentials it handed out and the one a
// person brings from elsewhere; any other is refused the way Plaid refuses
// one, with a message that repeats it, and one stands for Plaid being
// unreachable, with a message that repeats it too.
func (self *fakeLinker) DescribeCredential(_ context.Context, credential string) (*finance.CredentialDescription, error) {
	switch credential {
	case inventedPlaidCredential, importedPlaidCredential:
		return &finance.CredentialDescription{ProviderReference: "item-invented-imported", InstitutionID: "institution-invented"}, nil
	case unreachablePlaidCredential:
		return nil, errors.New("finance: cannot reach Plaid while describing " + credential)
	}
	return nil, &finance.PlaidError{StatusCode: 400, ErrorCode: "INVALID_ACCESS_TOKEN", ErrorMessage: "the access token " + credential + " is not valid"}
}

func (self *fakeLinker) Remove(_ context.Context, credential string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.removed = append(self.removed, credential)
	return nil
}

func (self *fakeLinker) removedCredentials() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string(nil), self.removed...)
}

// fakeClaimer is the SimpleFIN Bridge, handing over an invented credential.
type fakeClaimer struct{}

func (fakeClaimer) Claim(_ context.Context, setupToken string) (string, error) {
	if setupToken == "used-token" {
		return "", errors.New("the token was claimed already")
	}
	return inventedSimpleFinAddress, nil
}

// DescribeCredential knows the credential it hands out; the bridge refuses
// any other as revoked.
func (fakeClaimer) DescribeCredential(_ context.Context, credential string) (*finance.CredentialDescription, error) {
	if credential != inventedSimpleFinAddress {
		return nil, fmt.Errorf("finance: SimpleFIN answered 403: %w", finance.ErrCredentialRefused)
	}
	return &finance.CredentialDescription{InstitutionName: "Invented Credit Union"}, nil
}

func (fakeClaimer) Remove(context.Context, string) error { return nil }

// financeFixture is a server offering both providers, a worker that can
// seal credentials, and two people with agents. The fakes stand in for the
// providers and the central bank, so nothing reaches the network. Not
// parallel: it sets the package's provider and rate hooks, which only these
// tests read, before any parallel test resumes.
type financeFixture struct {
	database      db.Database
	resolver      *graph
	configuration *config.Configuration
	linker        *fakeLinker
	owner         *models.User
	stranger      *models.User
	ownerAgent    *models.Agent
}

func newFinanceFixture(test *testing.T, hasServerSecret bool) *financeFixture {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)
	fixture := &financeFixture{database: database, linker: &fakeLinker{}}
	financeRatesFetcher = func(db.Database) *rates.Fetcher { return nil }
	newFinanceLinker = func(*config.Configuration) (financeLinker, error) { return fixture.linker, nil }
	newFinanceClaimer = func() financeClaimer { return fakeClaimer{} }

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if fixture.owner, err = tx.CreateUser(&models.User{Username: "finance-owner", Name: "Alice Example"}); err != nil {
			test.Fatal(err)
		}
		if fixture.ownerAgent, err = tx.CreateAgent(&models.Agent{UserID: fixture.owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			test.Fatal(err)
		}
		if fixture.stranger, err = tx.CreateUser(&models.User{Username: "finance-stranger", Name: "Carol Example"}); err != nil {
			test.Fatal(err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: fixture.stranger.ID, Enabled: true, Name: "Dora"}); err != nil {
			test.Fatal(err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	if hasServerSecret {
		configuration.Server.Secret = "an-invented-server-secret-for-sealing-0123456789"
	}
	configuration.Agent.Finance = config.AgentFinance{
		OfferedProviders: []string{config.AgentFinanceProviderPlaid, config.AgentFinanceProviderSimpleFIN},
		Plaid:            config.AgentPlaid{Environment: config.AgentPlaidEnvironmentSandbox, ClientID: "client-invented", Secret: inventedPlaidSecret},
	}
	fixture.configuration = configuration
	worker := agentpackage.New(&agentpackage.Settings{
		Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour,
	})
	fixture.resolver = &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{Agent: worker}}
	return fixture
}

// as runs one step as a person, in a transaction of its own.
func (self *financeFixture) as(test *testing.T, user *models.User, run func(ctx context.Context, tx db.Transaction)) {
	test.Helper()
	principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx), tx)
	})
}

// seedFinanceSource links a SimpleFIN finance source for the owner and
// syncs an invented account with two transactions into it.
func (self *financeFixture) seedFinanceSource(test *testing.T) (*FinanceSourceView, *models.FinanceAccount, []*models.FinanceTransaction) {
	test.Helper()
	var linked *FinanceSourceView
	self.as(test, self.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if linked, err = self.resolver.LinkSimpleFIN(ctx, LinkSimpleFINArguments{SetupToken: "aW52ZW50ZWQtc2V0dXAtdG9rZW4="}); err != nil {
			test.Fatalf("LinkSimpleFIN: %s", err)
		}
		if _, err := tx.ApplyFinanceSync(self.ownerAgent.ID, linked.ID, &finance.SyncResult{
			Accounts: []finance.Account{{
				ProviderAccountID: "account-invented-1", AccountName: "Everyday Checking", AccountMask: "0001", AccountKind: "depository",
				CurrencyCode: "USD", CurrentBalance: "1200.5000", BalanceAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
			}},
			Added: []finance.Transaction{
				{ProviderTransactionID: "transaction-invented-1", ProviderAccountID: "account-invented-1", PostedOn: "2026-09-12", Amount: "-42.1700", CurrencyCode: "USD", Description: "CORNER GROCER 0412", MerchantName: "Corner Grocer"},
				{ProviderTransactionID: "transaction-invented-2", ProviderAccountID: "account-invented-1", PostedOn: "2026-09-14", Amount: "-20.0000", CurrencyCode: "EUR", Description: "CAFE ABROAD"},
			},
		}, "2026-09-20"); err != nil {
			test.Fatalf("ApplyFinanceSync: %s", err)
		}
	})
	var account *models.FinanceAccount
	var transactions []*models.FinanceTransaction
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		accounts, err := tx.ListFinanceAccounts(self.ownerAgent.ID, linked.ID)
		if err != nil || len(accounts) != 1 {
			test.Fatalf("accounts %v %v", accounts, err)
		}
		account = accounts[0]
		page, err := tx.ListFinanceTransactions(self.ownerAgent.ID, nil)
		if err != nil || len(page.FinanceTransactions) != 2 {
			test.Fatalf("transactions %v %v", page, err)
		}
		transactions = page.FinanceTransactions
	})
	return linked, account, transactions
}

// assertNoSecret fails when anything the API answered carries a provider
// secret or a credential.
func assertNoSecret(test *testing.T, name string, answered any) {
	test.Helper()
	encoded, err := json.Marshal(answered)
	if err != nil {
		test.Fatalf("%s: %s", name, err)
	}
	for _, secret := range []string{
		inventedPlaidSecret, inventedSimpleFinPassword, inventedPlaidCredential, "bridge.example.net",
		importedPlaidCredential, unknownPlaidCredential, unreachablePlaidCredential,
	} {
		if strings.Contains(string(encoded), secret) {
			test.Errorf("%s answered a secret: %s", name, encoded)
		}
	}
}

// A second person can read, change or delete none of the first person's
// finance data by id, and nothing the API answers carries a secret.
func TestFinanceDataIsTheCallersOwn(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, account, transactions := fixture.seedFinanceSource(test)
	resolver := fixture.resolver

	var asset *models.Asset
	var valuation *models.AssetValuation
	var spendingCategory *models.SpendingCategory
	var spendingRule *models.SpendingRule
	var savingsTarget *SavingsTargetView
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if asset, err = resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the car", AssetKind: "vehicle", CurrencyCode: "usd"}); err != nil {
			test.Fatalf("CreateAsset: %s", err)
		}
		if valuation, err = resolver.RecordValuation(ctx, RecordValuationArguments{AssetID: asset.ID, Value: "18,000", ValuedOn: "2026-09-01"}); err != nil {
			test.Fatalf("RecordValuation: %s", err)
		}
		if valuation.ValuationSource != models.ValuationSourceManual || valuation.Value != "18000.0000" {
			test.Errorf("recorded %+v", valuation)
		}
		if spendingCategory, err = resolver.CreateSpendingCategory(ctx, CreateSpendingCategoryArguments{SpendingCategoryName: "invented hobbies"}); err != nil {
			test.Fatalf("CreateSpendingCategory: %s", err)
		}
		if spendingRule, err = resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "grocer", SpendingCategoryID: spendingCategory.ID}); err != nil {
			test.Fatalf("CreateSpendingRule: %s", err)
		}
		if _, err = resolver.SetBudget(ctx, SetBudgetArguments{SpendingCategoryID: spendingCategory.ID, MonthlyAmount: "400", CurrencyCode: "USD", EffectiveFrom: "2026-09"}); err != nil {
			test.Fatalf("SetBudget: %s", err)
		}
		if savingsTarget, err = resolver.CreateSavingsTarget(ctx, CreateSavingsTargetArguments{
			SavingsTargetName: "a rainy day", TargetAmount: "5000", CurrencyCode: "USD", TargetOn: "2027-06-30", StartedOn: "2026-09-01",
		}); err != nil {
			test.Fatalf("CreateSavingsTarget: %s", err)
		}

		// What the owner reads carries no secret either.
		sources, err := resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 1 || len(sources[0].FinanceAccounts) != 1 {
			test.Fatalf("FinanceSources %v %v", sources, err)
		}
		assertNoSecret(test, "FinanceSources", sources)
		accounts, err := resolver.FinanceAccounts(ctx, FinanceAccountsArguments{})
		if err != nil || len(accounts) != 1 || accounts[0].ConvertedCurrentBalance != "1200.5000" {
			test.Fatalf("FinanceAccounts %+v %v", accounts, err)
		}
		assertNoSecret(test, "FinanceAccounts", accounts)
		page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{})
		if err != nil || len(page.FinanceTransactions) != 2 {
			test.Fatalf("FinanceTransactions %v %v", page, err)
		}
		assertNoSecret(test, "FinanceTransactions", page)
		providers, err := resolver.FinanceProviders(ctx)
		if err != nil || len(providers) != 2 {
			test.Fatalf("FinanceProviders %v %v", providers, err)
		}
		assertNoSecret(test, "FinanceProviders", providers)
	})

	// The credential is kept sealed beside the source, never as given.
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		secrets, err := tx.ListAgentSourceSecrets(source.ID)
		if err != nil || len(secrets) != 1 || secrets[0].Key != models.FinanceCredentialSecretKey {
			test.Fatalf("secrets %v %v", secrets, err)
		}
		if secrets[0].Value == "" || strings.Contains(secrets[0].Value, inventedSimpleFinPassword) {
			test.Errorf("the credential is not sealed: %q", secrets[0].Value)
		}
		stored, err := tx.GetAgentSource(fixture.ownerAgent.ID, source.ID)
		if err != nil || stored.Cron != models.FinanceSourceCron || stored.NextRunAt == nil || stored.Kind != models.SourceFinance {
			test.Errorf("the finance source is %+v %v", stored, err)
		}
		categories, err := tx.ListSpendingCategories(fixture.ownerAgent.ID)
		if err != nil || len(categories) != len(finance.DefaultSpendingCategoryNames)+1 {
			test.Errorf("the default spending categories were not made: %d %v", len(categories), err)
		}
	})

	refusals := map[string]func(ctx context.Context) error{
		"CompleteFinanceRepair": func(ctx context.Context) error {
			_, err := resolver.CompleteFinanceRepair(ctx, FinanceSourceArguments{SourceID: source.ID})
			return err
		},
		"CreateFinanceLinkToken": func(ctx context.Context) error {
			_, err := resolver.CreateFinanceLinkToken(ctx, CreateFinanceLinkTokenArguments{SourceID: source.ID})
			return err
		},
		"DeleteAgentKnowledgeSource": func(ctx context.Context) error {
			_, err := resolver.DeleteAgentKnowledgeSource(ctx, DeleteAgentKnowledgeSourceArguments{SourceID: source.ID})
			return err
		},
		"AssetHistory": func(ctx context.Context) error {
			_, err := resolver.AssetHistory(ctx, AssetArguments{AssetID: asset.ID})
			return err
		},
		"UpdateAsset": func(ctx context.Context) error {
			name := "taken"
			_, err := resolver.UpdateAsset(ctx, UpdateAssetArguments{AssetID: asset.ID, AssetName: &name})
			return err
		},
		"CloseAsset": func(ctx context.Context) error {
			_, err := resolver.CloseAsset(ctx, CloseAssetArguments{AssetID: asset.ID})
			return err
		},
		"DeleteAsset": func(ctx context.Context) error {
			_, err := resolver.DeleteAsset(ctx, AssetArguments{AssetID: asset.ID})
			return err
		},
		"RecordValuation": func(ctx context.Context) error {
			_, err := resolver.RecordValuation(ctx, RecordValuationArguments{AssetID: asset.ID, Value: "1"})
			return err
		},
		"DeleteValuation": func(ctx context.Context) error {
			_, err := resolver.DeleteValuation(ctx, ValuationArguments{ValuationID: valuation.ID})
			return err
		},
		"UpdateSpendingCategory": func(ctx context.Context) error {
			name := "taken"
			_, err := resolver.UpdateSpendingCategory(ctx, UpdateSpendingCategoryArguments{SpendingCategoryID: spendingCategory.ID, SpendingCategoryName: &name})
			return err
		},
		"DeleteSpendingCategory": func(ctx context.Context) error {
			_, err := resolver.DeleteSpendingCategory(ctx, SpendingCategoryArguments{SpendingCategoryID: spendingCategory.ID})
			return err
		},
		"UpdateSpendingRule": func(ctx context.Context) error {
			matchText := "taken"
			_, err := resolver.UpdateSpendingRule(ctx, UpdateSpendingRuleArguments{SpendingRuleID: spendingRule.ID, MatchText: &matchText})
			return err
		},
		"DeleteSpendingRule": func(ctx context.Context) error {
			_, err := resolver.DeleteSpendingRule(ctx, SpendingRuleArguments{SpendingRuleID: spendingRule.ID})
			return err
		},
		"CreateSpendingRule naming theirs": func(ctx context.Context) error {
			_, err := resolver.CreateSpendingRule(ctx, CreateSpendingRuleArguments{MatchText: "grocer", SpendingCategoryID: spendingCategory.ID})
			return err
		},
		"CategorizeTransaction": func(ctx context.Context) error {
			_, err := resolver.CategorizeTransaction(ctx, CategorizeTransactionArguments{FinanceTransactionID: transactions[0].ID})
			return err
		},
		"MarkTransfer": func(ctx context.Context) error {
			_, err := resolver.MarkTransfer(ctx, MarkTransferArguments{FinanceTransactionID: transactions[0].ID, IsTransfer: true})
			return err
		},
		"SetBudget": func(ctx context.Context) error {
			_, err := resolver.SetBudget(ctx, SetBudgetArguments{SpendingCategoryID: spendingCategory.ID, MonthlyAmount: "1", CurrencyCode: "USD"})
			return err
		},
		"UpdateSavingsTarget": func(ctx context.Context) error {
			name := "taken"
			_, err := resolver.UpdateSavingsTarget(ctx, UpdateSavingsTargetArguments{SavingsTargetID: savingsTarget.SavingsTarget.ID, SavingsTargetName: &name})
			return err
		},
		"CloseSavingsTarget": func(ctx context.Context) error {
			_, err := resolver.CloseSavingsTarget(ctx, CloseSavingsTargetArguments{SavingsTargetID: savingsTarget.SavingsTarget.ID})
			return err
		},
		"CreateSavingsTarget naming their asset": func(ctx context.Context) error {
			_, err := resolver.CreateSavingsTarget(ctx, CreateSavingsTargetArguments{
				SavingsTargetName: "taken", TargetAmount: "1", CurrencyCode: "USD", TargetOn: "2027-01-01", TargetMeasure: "asset_value", AssetIDs: []string{asset.ID},
			})
			return err
		},
		"SaveAgentKnowledgeSource": func(ctx context.Context) error {
			isEnabled := false
			_, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Enabled: &isEnabled})
			return err
		},
	}
	for name, attempt := range refusals {
		fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
			if err := attempt(ctx); err == nil {
				test.Errorf("%s: the stranger reached the owner's row", name)
			}
		})
	}

	// And the stranger's lists hold none of it.
	fixture.as(test, fixture.stranger, func(ctx context.Context, tx db.Transaction) {
		sources, err := resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 0 {
			test.Errorf("FinanceSources %v %v", sources, err)
		}
		accounts, err := resolver.FinanceAccounts(ctx, FinanceAccountsArguments{})
		if err != nil || len(accounts) != 0 {
			test.Errorf("FinanceAccounts %v %v", accounts, err)
		}
		page, err := resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{FinanceAccountID: account.ID})
		if err != nil || len(page.FinanceTransactions) != 0 {
			test.Errorf("FinanceTransactions %v %v", page, err)
		}
		summary, err := resolver.FinanceSpendingSummary(ctx, FinanceSpendingSummaryArguments{FinanceAccountID: account.ID})
		if err != nil || len(summary.SpendingSummaryRows) != 0 {
			test.Errorf("FinanceSpendingSummary %v %v", summary, err)
		}
		assets, err := resolver.Assets(ctx)
		if err != nil || len(assets) != 0 {
			test.Errorf("Assets %v %v", assets, err)
		}
		spendingCategories, err := resolver.SpendingCategories(ctx)
		if err != nil || len(spendingCategories) != 0 {
			test.Errorf("SpendingCategories %v %v", spendingCategories, err)
		}
		spendingRules, err := resolver.SpendingRules(ctx)
		if err != nil || len(spendingRules) != 0 {
			test.Errorf("SpendingRules %v %v", spendingRules, err)
		}
		budgets, err := resolver.Budgets(ctx)
		if err != nil || len(budgets) != 0 {
			test.Errorf("Budgets %v %v", budgets, err)
		}
		savingsTargets, err := resolver.SavingsTargets(ctx)
		if err != nil || len(savingsTargets) != 0 {
			test.Errorf("SavingsTargets %v %v", savingsTargets, err)
		}
		netWorth, err := resolver.NetWorth(ctx, NetWorthArguments{From: "2026-09-01", To: "2026-09-30"})
		if err != nil || len(netWorth.NetWorthPoints) != 0 {
			test.Errorf("NetWorth %v %v", netWorth, err)
		}
	})

	// Everything the owner had is still there, unchanged.
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		history, err := resolver.AssetHistory(ctx, AssetArguments{AssetID: asset.ID})
		if err != nil || history.Asset.AssetName != "the car" || history.Asset.ClosedOn != "" || len(history.AssetValuations) != 1 {
			test.Errorf("the asset is %+v %v", history, err)
		}
		rules, err := resolver.SpendingRules(ctx)
		if err != nil || len(rules) != 1 || rules[0].MatchText != "grocer" {
			test.Errorf("the rules are %v %v", rules, err)
		}
		budgets, err := resolver.Budgets(ctx)
		if err != nil || len(budgets) != 1 || budgets[0].MonthlyAmount != "400.0000" {
			test.Errorf("the budgets are %+v %v", budgets, err)
		}
		targets, err := resolver.SavingsTargets(ctx)
		if err != nil || len(targets) != 1 || targets[0].SavingsTarget.SavingsTargetName != "a rainy day" || targets[0].SavingsTarget.ClosedOn != "" {
			test.Errorf("the savings targets are %v %v", targets, err)
		}
		transaction, err := tx.GetFinanceTransaction(fixture.ownerAgent.ID, transactions[0].ID)
		if err != nil || transaction.TransferMarkedBy == models.TransferMarkedByPerson || transaction.CategorizedBy == models.CategorizedByPerson {
			test.Errorf("the transaction is %+v %v", transaction, err)
		}
		sources, err := resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 1 || !sources[0].IsEnabled {
			test.Errorf("the finance source is %v %v", sources, err)
		}
	})
}

// Totals are per currency and, converted at each day's stored rate, in the
// reporting currency; a currency with no rate is named and left out.
func TestFinanceTotalsConvertEachDay(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	fixture.seedFinanceSource(test)
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		if _, err := tx.UpsertExchangeRates([]models.ExchangeRate{
			{RateOn: "2026-09-11", CurrencyCode: "USD", EuroRate: "1.1000000000", RateSource: models.RateSourceECB},
			{RateOn: "2026-09-14", CurrencyCode: "USD", EuroRate: "1.2000000000", RateSource: models.RateSourceECB},
		}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		summary, err := fixture.resolver.FinanceSpendingSummary(ctx, FinanceSpendingSummaryArguments{GroupBy: "month", CurrencyCode: "USD"})
		if err != nil {
			test.Fatal(err)
		}
		if len(summary.CurrencyTotals) != 2 || summary.ReportingCurrencyCode != "USD" {
			test.Fatalf("totals %+v", summary)
		}
		// 42.17 USD, and 20 EUR at the 14th's 1.2: 24 USD.
		if summary.ConvertedMoneyOut != "66.1700" || len(summary.UnconvertedCurrencyCodes) != 0 {
			test.Errorf("converted %s, left out %v", summary.ConvertedMoneyOut, summary.UnconvertedCurrencyCodes)
		}
		if len(summary.ConvertedSpendingSummaryRows) != 1 || summary.ConvertedSpendingSummaryRows[0].GroupKey != "2026-09" {
			test.Errorf("rows %+v", summary.ConvertedSpendingSummaryRows)
		}

		// Into a currency nobody publishes a rate for, neither converts.
		summary, err = fixture.resolver.FinanceSpendingSummary(ctx, FinanceSpendingSummaryArguments{CurrencyCode: "ZZZ"})
		if err != nil {
			test.Fatal(err)
		}
		if summary.ConvertedMoneyOut != "0.0000" || strings.Join(summary.UnconvertedCurrencyCodes, ",") != "EUR,USD" {
			test.Errorf("converted %s, left out %v", summary.ConvertedMoneyOut, summary.UnconvertedCurrencyCodes)
		}

		conversion, err := fixture.resolver.ConvertCurrency(ctx, ConvertCurrencyArguments{Amount: "100", FromCurrencyCode: "EUR", ToCurrencyCode: "USD", RateOn: "2026-09-13"})
		if err != nil || conversion.ConvertedAmount != "110.0000" || conversion.RateOn != "2026-09-11" {
			test.Errorf("converted %+v %v", conversion, err)
		}
		if _, err := fixture.resolver.ExchangeRate(ctx, ExchangeRateArguments{FromCurrencyCode: "ZZZ", ToCurrencyCode: "USD"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a currency with no rate answered %v", err)
		}
	})
}

// The API records a person's valuation as manual; the agent's readings and
// estimates only where they are allowed; finance_sync never.
func TestRecordValuationKeepsToItsSources(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		car, err := resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the car", AssetKind: "vehicle", CurrencyCode: "USD"})
		if err != nil {
			test.Fatal(err)
		}
		isAllowed := true
		house, err := resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the house", AssetKind: "property", CurrencyCode: "USD", EstimateDescription: "a house on an invented street", IsEstimateAllowed: &isAllowed})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "synced", AssetKind: "cash", CurrencyCode: "USD", ValuationSource: "finance_sync"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a finance_sync asset was made by hand: %v", err)
		}
		assets, err := resolver.Assets(ctx)
		if err != nil {
			test.Fatal(err)
		}
		var synced *models.Asset
		for _, asset := range assets {
			if asset.ValuationSource == models.ValuationSourceFinanceSync {
				synced = asset
			}
		}
		if synced == nil {
			test.Fatalf("the finance account has no asset: %+v", assets)
		}

		for _, refused := range []RecordValuationArguments{
			{AssetID: car.ID, Value: "1", ValuationSource: "finance_sync"},
			{AssetID: car.ID, Value: "1", ValuationSource: "agent_estimate"},
			{AssetID: car.ID, Value: "1", ValuationSource: "invented"},
			{AssetID: synced.ID, Value: "1", ValuationSource: "agent_reading"},
			{AssetID: house.ID, Value: "1", ValuationSource: "agent_estimate", EvidenceURLs: []string{"javascript:alert(1)"}},
			{AssetID: car.ID, Value: "a lot"},
		} {
			if _, err := resolver.RecordValuation(ctx, refused); !errors.Is(err, api.ErrInvalidArguments) {
				test.Errorf("%+v was recorded: %v", refused, err)
			}
		}
		for _, taken := range []RecordValuationArguments{
			{AssetID: car.ID, Value: "16500"},
			{AssetID: car.ID, Value: "16400", ValuationSource: "agent_reading"},
			{AssetID: synced.ID, Value: "1100"},
			{
				AssetID: house.ID, Value: "410000", ValuationSource: "agent_estimate", EstimateLow: "390000", EstimateHigh: "430000",
				ValuationNote: "two comparable sales", EvidenceURLs: []string{"https://listings.example.com/one", "https://listings.example.com/two"},
			},
		} {
			if _, err := resolver.RecordValuation(ctx, taken); err != nil {
				test.Errorf("%+v was refused: %s", taken, err)
			}
		}
	})
}

// A finance source is made only by linking: the generic source creation
// refuses the kind, and an existing finance source changes only its name,
// schedule and switch.
func TestGenericSourceCreationRefusesFinance(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	resolver := fixture.resolver
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{Kind: "finance", Name: "made by hand"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a finance source was made without a link: %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Path: "~/elsewhere"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a finance source's path was changed: %v", err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		isEnabled := false
		saved, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{SourceID: source.ID, Name: "renamed bank", Enabled: &isEnabled, Cron: "0 7 * * *"})
		if err != nil || saved.Name != "renamed bank" || saved.Enabled || saved.Cron != "0 7 * * *" || saved.Kind != models.SourceFinance {
			test.Errorf("saved %+v %v", saved, err)
		}
	})
}

// A Plaid link whose finance source cannot be made is ended at Plaid at
// once; one that can is kept, sealed, and never answered back.
func TestCompleteFinanceLinkEndsALinkItCannotKeep(test *testing.T) {
	// No server secret: sealing the credential fails after the exchange.
	fixture := newFinanceFixture(test, false)
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.CompleteFinanceLink(ctx, CompleteFinanceLinkArguments{PublicToken: "public-invented", InstitutionID: "institution-invented"}); err == nil {
			test.Fatal("a link that could not be sealed was kept")
		}
	})
	if removed := fixture.linker.removedCredentials(); len(removed) != 1 || removed[0] != inventedPlaidCredential {
		test.Errorf("the link was not ended at Plaid: %v", removed)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		sources, err := tx.ListAgentSources(fixture.ownerAgent.ID)
		if err != nil || len(sources) != 0 {
			test.Errorf("a source was left behind: %v %v", sources, err)
		}
	})

	kept := newFinanceFixture(test, true)
	kept.as(test, kept.owner, func(ctx context.Context, tx db.Transaction) {
		token, err := kept.resolver.CreateFinanceLinkToken(ctx, CreateFinanceLinkTokenArguments{})
		if err != nil || token.LinkToken != "link-token-for-"+kept.ownerAgent.ID {
			test.Fatalf("CreateFinanceLinkToken %+v %v", token, err)
		}
		linked, err := kept.resolver.CompleteFinanceLink(ctx, CompleteFinanceLinkArguments{PublicToken: "public-invented", InstitutionID: "institution-invented"})
		if err != nil {
			test.Fatalf("CompleteFinanceLink: %s", err)
		}
		if linked.InstitutionName != "Invented Savings Bank" || linked.ProviderKind != config.AgentFinanceProviderPlaid || !linked.IsEnabled {
			test.Errorf("linked %+v", linked)
		}
		assertNoSecret(test, "CompleteFinanceLink", linked)
		repaired, err := kept.resolver.CreateFinanceLinkToken(ctx, CreateFinanceLinkTokenArguments{SourceID: linked.ID})
		if err != nil || repaired.SourceID != linked.ID {
			test.Errorf("a repair token for the finance source: %+v %v", repaired, err)
		}
	})
	if removed := kept.linker.removedCredentials(); len(removed) != 0 {
		test.Errorf("a kept link was ended at Plaid: %v", removed)
	}
}

// A used setup token says so, and nothing is made.
func TestLinkSimpleFINRefusesAUsedToken(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.LinkSimpleFIN(ctx, LinkSimpleFINArguments{SetupToken: "used-token"}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Errorf("a used token answered %v", err)
		}
		sources, err := fixture.resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 0 {
			test.Errorf("sources %v %v", sources, err)
		}
	})
}

// On a server that offers no provider, writes answer that finance is not
// offered, and what is stored can still be read and deleted.
func TestFinanceNotOfferedStillReadsWhatIsStored(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	source, _, _ := fixture.seedFinanceSource(test)
	var asset *models.Asset
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		var err error
		if asset, err = fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the bike", AssetKind: "vehicle", CurrencyCode: "USD"}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.configuration.Agent.Finance.OfferedProviders = nil
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "another", AssetKind: "vehicle", CurrencyCode: "USD"}); !errors.Is(err, errFinanceNotOffered) {
			test.Errorf("CreateAsset answered %v", err)
		}
		if _, err := fixture.resolver.LinkSimpleFIN(ctx, LinkSimpleFINArguments{SetupToken: "aW52ZW50ZWQ="}); !errors.Is(err, errFinanceNotOffered) {
			test.Errorf("LinkSimpleFIN answered %v", err)
		}
		if _, err := fixture.resolver.ConvertCurrency(ctx, ConvertCurrencyArguments{Amount: "1", FromCurrencyCode: "EUR", ToCurrencyCode: "USD"}); !errors.Is(err, errFinanceNotOffered) {
			test.Errorf("ConvertCurrency answered %v", err)
		}
		providers, err := fixture.resolver.FinanceProviders(ctx)
		if err != nil || len(providers) != 0 {
			test.Errorf("FinanceProviders %v %v", providers, err)
		}
		sources, err := fixture.resolver.FinanceSources(ctx)
		if err != nil || len(sources) != 1 {
			test.Errorf("FinanceSources %v %v", sources, err)
		}
		page, err := fixture.resolver.FinanceTransactions(ctx, FinanceTransactionsArguments{})
		if err != nil || len(page.FinanceTransactions) != 2 {
			test.Errorf("FinanceTransactions %v %v", page, err)
		}
		if _, err := fixture.resolver.DeleteAsset(ctx, AssetArguments{AssetID: asset.ID}); err != nil {
			test.Errorf("DeleteAsset answered %v", err)
		}
	})

	// Deleting the finance source keeps its account's asset, as a manual one
	// closed on the day before, so it no longer counts from today.
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.DeleteAgentKnowledgeSource(ctx, DeleteAgentKnowledgeSourceArguments{SourceID: source.ID}); err != nil {
			test.Fatalf("DeleteAgentKnowledgeSource: %s", err)
		}
		assets, err := fixture.resolver.Assets(ctx)
		if err != nil || len(assets) != 1 || assets[0].ValuationSource != models.ValuationSourceManual || assets[0].FinanceAccountID != "" {
			test.Errorf("the finance account's asset is %+v %v", assets, err)
		}
		yesterday := time.Now().In(agentpackage.Location(fixture.owner)).AddDate(0, 0, -1).Format(time.DateOnly)
		if len(assets) == 1 && assets[0].ClosedOn != yesterday {
			test.Errorf("the finance account's asset closed on %q, want the day before the delete, %s", assets[0].ClosedOn, yesterday)
		}
		accounts, err := fixture.resolver.FinanceAccounts(ctx, FinanceAccountsArguments{})
		if err != nil || len(accounts) != 0 {
			test.Errorf("the finance accounts are %v %v", accounts, err)
		}
	})
}
