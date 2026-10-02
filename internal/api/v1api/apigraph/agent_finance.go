package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/graphapi"
)

// Finance: the person's finance sources, finance accounts and finance
// transactions, exchange rates, net worth, spending categories and rules,
// budgets and savings targets. The dashboard's Finance page, the command
// line's teanode finance and the agent's finance tool all call these, so
// the three say the same numbers; none of them has logic of its own. Every
// resolver starts with requireAgentPerson and reads and writes the caller's
// own agent's rows only, each loaded by agent and id (SEC-13).
//
// Syncing, switching a finance source off and on, and deleting one are the
// operations every source has (SyncAgentKnowledgeSource,
// SaveAgentKnowledgeSource, DeleteAgentKnowledgeSource).

// FinanceQuery reads the person's finance data. Needs agent:use.
type FinanceQuery interface {
	// The providers people may link an institution through on this server:
	// empty when the operator offers none.
	FinanceProviders(ctx context.Context) ([]*FinanceProviderView, error)

	// The caller's finance sources, each with its institution, its state
	// and its finance accounts.
	FinanceSources(ctx context.Context) ([]*FinanceSourceView, error)

	// The caller's finance accounts with their balances, each also in the
	// reporting currency where there is an exchange rate.
	FinanceAccounts(ctx context.Context, arguments FinanceAccountsArguments) ([]*FinanceAccountView, error)

	// What is owed on the caller's credit cards against their credit
	// limits, overall in the reporting currency and per card: the limit
	// the provider gave, else what is owed plus the credit available.
	CreditUsage(ctx context.Context, arguments CreditUsageArguments) (*CreditUsageView, error)

	// A page of the caller's finance transactions, newest first.
	FinanceTransactions(ctx context.Context, arguments FinanceTransactionsArguments) (*FinanceTransactionPageView, error)

	// A page of the caller's trades, newest first, each with its security:
	// buys, sells, cancelled trades and securities moved in or out of an
	// investment account. Never spending or income.
	FinanceTrades(ctx context.Context, arguments FinanceTradesArguments) (*FinanceTradePageView, error)

	// Money out and money in per group, per currency and in the reporting
	// currency, transfers and mirrored copies left out.
	FinanceSpendingSummary(ctx context.Context, arguments FinanceSpendingSummaryArguments) (*FinanceSpendingSummaryView, error)

	// What one unit of one currency bought in another on a day, from the
	// European Central Bank's reference rates: the day's, or the latest
	// earlier published one.
	ExchangeRate(ctx context.Context, arguments ExchangeRateArguments) (*models.CurrencyPairRate, error)

	// An amount in another currency at a day's exchange rate.
	ConvertCurrency(ctx context.Context, arguments ConvertCurrencyArguments) (*CurrencyConversionView, error)

	// Net worth per day over a range, per currency and in the reporting
	// currency.
	NetWorth(ctx context.Context, arguments NetWorthArguments) (*NetWorthView, error)

	// The caller's assets, open ones first, each with the valuation that
	// counts today.
	Assets(ctx context.Context, arguments AssetsArguments) ([]*models.Asset, error)

	// One asset and its valuations, newest day first.
	AssetHistory(ctx context.Context, arguments AssetArguments) (*AssetHistoryView, error)

	// The caller's spending categories, by name.
	SpendingCategories(ctx context.Context) ([]*models.SpendingCategory, error)

	// The caller's spending rules, in the order they are tried.
	SpendingRules(ctx context.Context) ([]*models.SpendingRule, error)

	// The spending rules to offer when giving these finance transactions
	// (at most 5000, mirrored copies left out) this spending category: one
	// per distinct merchant, or description where there is no merchant,
	// unless the spending rule that applies first already sends it there.
	// Each says the existing rule it would go ahead of, so it takes effect,
	// and how many other finance transactions it would recategorize. A
	// match text too short or too generic, or holding a number that changes
	// each time, is left out and counted; at most 50 are proposed, the most
	// transactions first, and the rest counted. Nothing is saved.
	ProposeSpendingRules(ctx context.Context, arguments ProposeSpendingRulesArguments) (*SpendingRuleProposalsView, error)

	// Every budget row of the caller, by spending category and month.
	Budgets(ctx context.Context) ([]*models.Budget, error)

	// Each spending category with a budget in a month, against it: the
	// spending budgets, and apart from them the income budgets against
	// what came in.
	BudgetStatus(ctx context.Context, arguments BudgetStatusArguments) (*models.BudgetStatus, error)

	// A month's saving in the reporting currency: income budgets less
	// spending budgets, against income less spending so far, and where
	// the month is heading.
	SavingSummary(ctx context.Context, arguments SavingSummaryArguments) (*models.SavingSummary, error)

	// Cumulative spending per day of a month and of another to compare it
	// with, in the reporting currency.
	SpendingByDay(ctx context.Context, arguments SpendingByDayArguments) (*SpendingByDayView, error)

	// Income, spending and their difference per month, per currency and
	// in the reporting currency.
	CashFlow(ctx context.Context, arguments CashFlowArguments) (*CashFlowView, error)

	// The caller's savings targets, open ones first, each with its
	// progress.
	SavingsTargets(ctx context.Context) ([]*SavingsTargetView, error)

	// The currency totals are shown in, and whether the person chose it or
	// it falls back to the currency of their first finance account, else
	// of their first asset.
	ReportingCurrency(ctx context.Context) (*ReportingCurrencyView, error)

	// The caller's statement import: the address to mail OFX statements
	// to, whether importing is on, and what the last import did. The
	// statement finance source and its address are made the first time
	// this is asked.
	StatementImport(ctx context.Context) (*StatementImportView, error)
}

// FinanceMutation links institutions and changes the person's finance
// data. Needs agent:use.
type FinanceMutation interface {
	// A Plaid link token, for the linking page to open Plaid Link with;
	// with a source id, one that repairs that finance source's sign-in.
	CreateFinanceLinkToken(ctx context.Context, arguments CreateFinanceLinkTokenArguments) (*FinanceLinkTokenView, error)

	// Finish a Plaid link: exchange Plaid Link's public token for the
	// credential and make the finance source, due to sync now.
	CompleteFinanceLink(ctx context.Context, arguments CompleteFinanceLinkArguments) (*FinanceSourceView, error)

	// Finish a repair: the person signed in again, so the finance source
	// syncs now.
	CompleteFinanceRepair(ctx context.Context, arguments FinanceSourceArguments) (*FinanceSourceView, error)

	// Link through SimpleFIN with a setup token from the bridge, which can
	// be claimed once.
	LinkSimpleFIN(ctx context.Context, arguments LinkSimpleFINArguments) (*FinanceSourceView, error)

	// Bring a provider connection made elsewhere in as a finance source,
	// due to sync now, instead of linking the institution again: a Plaid
	// credential made with this server's Plaid keys, or a SimpleFIN
	// credential already claimed from a setup token. The provider is asked
	// first, so a credential it does not accept makes nothing.
	ImportFinanceCredential(ctx context.Context, arguments ImportFinanceCredentialArguments) (*FinanceSourceView, error)

	// Set the one currency totals are shown in; empty goes back to the
	// currency of the first finance account.
	SetReportingCurrency(ctx context.Context, arguments SetReportingCurrencyArguments) (string, error)

	// Import an OFX statement (.ofx, .qfx or .qbo): a file uploaded to the
	// agent's attachments, or every OFX attachment of a message in the
	// caller's mailbox. A transaction already imported, by its account and
	// FITID, is updated rather than added again.
	ImportStatement(ctx context.Context, arguments ImportStatementArguments) (*models.FinanceStatementImport, error)

	// Give the statement import address a new token. The old address stops
	// taking mail at once.
	RegenerateStatementImportAddress(ctx context.Context) (*StatementImportView, error)

	// Add an asset: something owned or owed that counts toward net worth.
	CreateAsset(ctx context.Context, arguments CreateAssetArguments) (*models.Asset, error)

	// Change an asset.
	UpdateAsset(ctx context.Context, arguments UpdateAssetArguments) (*models.Asset, error)

	// Record the day an asset was sold or paid off, or open it again.
	CloseAsset(ctx context.Context, arguments CloseAssetArguments) (*models.Asset, error)

	// Delete an asset and its whole history.
	DeleteAsset(ctx context.Context, arguments AssetArguments) (bool, error)

	// Record one value of an asset for a day.
	RecordValuation(ctx context.Context, arguments RecordValuationArguments) (*models.AssetValuation, error)

	// Delete one valuation.
	DeleteValuation(ctx context.Context, arguments ValuationArguments) (bool, error)

	// Add a spending category.
	CreateSpendingCategory(ctx context.Context, arguments CreateSpendingCategoryArguments) (*models.SpendingCategory, error)

	// Change a spending category.
	UpdateSpendingCategory(ctx context.Context, arguments UpdateSpendingCategoryArguments) (*models.SpendingCategory, error)

	// Delete a spending category, with its budgets and the spending rules
	// that assign it; its finance transactions become uncategorized.
	DeleteSpendingCategory(ctx context.Context, arguments SpendingCategoryArguments) (bool, error)

	// Add a spending rule, which applies at once to past finance
	// transactions except where the person chose.
	CreateSpendingRule(ctx context.Context, arguments CreateSpendingRuleArguments) (*models.SpendingRule, error)

	// Change a spending rule.
	UpdateSpendingRule(ctx context.Context, arguments UpdateSpendingRuleArguments) (*models.SpendingRule, error)

	// Delete a spending rule.
	DeleteSpendingRule(ctx context.Context, arguments SpendingRuleArguments) (bool, error)

	// Give a finance transaction a spending category, as the person's own
	// choice, which nothing overwrites; optionally make a spending rule
	// for its merchant too. The transfer category (isTransfer in
	// SpendingCategories) makes it a transfer between the person's own
	// accounts, neither spending nor income; any other takes that away.
	CategorizeTransaction(ctx context.Context, arguments CategorizeTransactionArguments) (*CategorizeTransactionView, error)

	// Give several finance transactions one spending category, at most
	// 500 at a time, as the person's own choice, all or none: an id that
	// is not the caller's refuses the whole call. Optionally save the
	// spending rules the person confirmed from ProposeSpendingRules,
	// exactly those, each ahead of the rule it names, so later
	// transactions like these get the same spending category; they are
	// checked again, refused when proposed for another spending category,
	// and applied to past finance transactions once, after the last.
	CategorizeTransactions(ctx context.Context, arguments CategorizeTransactionsArguments) (*CategorizeTransactionsView, error)

	// Count a mirrored copy, a duplicate of another finance transaction
	// (duplicateOfTransactionId), as the person's own decision that it is
	// a real charge of its own; mirror detection then leaves it alone.
	CountTransaction(ctx context.Context, arguments CountTransactionArguments) (*models.FinanceTransaction, error)

	// Take back CountTransaction: mirror detection decides again at once
	// whether the finance transaction is a duplicate.
	UndoCountTransaction(ctx context.Context, arguments CountTransactionArguments) (*models.FinanceTransaction, error)

	// Set a spending category's monthly budget from a month on.
	SetBudget(ctx context.Context, arguments SetBudgetArguments) (*models.Budget, error)

	// Add a savings target.
	CreateSavingsTarget(ctx context.Context, arguments CreateSavingsTargetArguments) (*SavingsTargetView, error)

	// Change a savings target.
	UpdateSavingsTarget(ctx context.Context, arguments UpdateSavingsTargetArguments) (*SavingsTargetView, error)

	// Record the day a savings target was closed, or open it again.
	CloseSavingsTarget(ctx context.Context, arguments CloseSavingsTargetArguments) (*SavingsTargetView, error)
}

// --- views --------------------------------------------------------------

// FinanceProviderView is one provider people may link through.
type FinanceProviderView struct {
	// ProviderKind is "plaid" or "simplefin".
	ProviderKind string `json:"providerKind"`

	// IsBrowserRequired says linking needs the dashboard's linking page,
	// /finance-link, because the provider's own window signs the person
	// in. A SimpleFIN setup token is pasted instead.
	IsBrowserRequired bool `json:"isBrowserRequired"`
}

// FinanceLinkTokenView is what the linking page opens Plaid Link with.
type FinanceLinkTokenView struct {
	LinkToken string `json:"linkToken"`

	// SourceID is the finance source a repair token repairs; empty for a
	// new link.
	SourceID string `json:"sourceId,omitempty" graphapi:"nullable"`
}

// FinanceSourceView is one finance source: one login at one institution
// through one provider. Its credential never comes back out.
type FinanceSourceView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ProviderKind    string `json:"providerKind"`
	InstitutionID   string `json:"institutionId,omitempty" graphapi:"nullable"`
	InstitutionName string `json:"institutionName,omitempty" graphapi:"nullable"`
	IsEnabled       bool   `json:"isEnabled"`

	// Cron is when it syncs by itself; empty only when asked.
	Cron      string     `json:"cron,omitempty" graphapi:"nullable"`
	LastRunAt *time.Time `json:"lastRunAt,omitempty" graphapi:"nullable"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty" graphapi:"nullable"`
	LastError string     `json:"lastError,omitempty" graphapi:"nullable"`

	// IsSignInRequired says the institution waits for the person to sign
	// in again (CreateFinanceLinkToken with this source, then
	// CompleteFinanceRepair); it does not sync until then.
	IsSignInRequired bool `json:"isSignInRequired"`

	FinanceAccounts []*FinanceAccountView `json:"financeAccounts"`
	CreatedAt       time.Time             `json:"createdAt"`
}

// FinanceAccountView is one finance account with where it comes from and
// its balance in the reporting currency.
type FinanceAccountView struct {
	ID               string                    `json:"id"`
	SourceID         string                    `json:"sourceId"`
	InstitutionName  string                    `json:"institutionName,omitempty" graphapi:"nullable"`
	ProviderKind     string                    `json:"providerKind"`
	AccountName      string                    `json:"accountName"`
	AccountMask      string                    `json:"accountMask,omitempty" graphapi:"nullable"`
	AccountKind      models.FinanceAccountKind `json:"accountKind"`
	CurrencyCode     string                    `json:"currencyCode"`
	CurrentBalance   string                    `json:"currentBalance,omitempty" graphapi:"nullable"`
	AvailableBalance string                    `json:"availableBalance,omitempty" graphapi:"nullable"`
	BalanceAt        *time.Time                `json:"balanceAt,omitempty" graphapi:"nullable"`
	IsSignInRequired bool                      `json:"isSignInRequired"`

	// CreditLimitAmount is a card's credit limit as the provider gave it;
	// CreditUsage says what usage is measured against when it gave none.
	CreditLimitAmount string `json:"creditLimitAmount,omitempty" graphapi:"nullable"`

	// ReportingCurrencyCode is the currency totals are shown in, and
	// ConvertedCurrentBalance the current balance in it, at the rate of
	// the balance's day; empty when there is no rate or no balance.
	ReportingCurrencyCode   string `json:"reportingCurrencyCode,omitempty" graphapi:"nullable"`
	ConvertedCurrentBalance string `json:"convertedCurrentBalance,omitempty" graphapi:"nullable"`

	// ProviderMetadata is the provider's whole object for the account, as
	// it arrived.
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty" graphapi:"nullable"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// FinanceTransactionPageView is one page of finance transactions, the
// cursor for the next, empty on the last, how many match the filters on
// every page, and how many mirrored copies would match too but were left
// out because duplicates were not asked for.
type FinanceTransactionPageView struct {
	FinanceTransactions   []*models.FinanceTransaction `json:"financeTransactions"`
	NextCursor            string                       `json:"nextCursor,omitempty" graphapi:"nullable"`
	TotalCount            int                          `json:"totalCount"`
	LeftOutDuplicateCount int                          `json:"leftOutDuplicateCount"`
}

// FinanceTradePageView is one page of trades, the cursor for the next,
// empty on the last, and how many match the filters on every page.
type FinanceTradePageView struct {
	FinanceTrades []*models.FinanceTrade `json:"financeTrades"`
	NextCursor    string                 `json:"nextCursor,omitempty" graphapi:"nullable"`
	TotalCount    int                    `json:"totalCount"`
}

// FinanceSpendingSummaryView is a spending summary: per group and
// currency, per currency, and converted into the reporting currency.
type FinanceSpendingSummaryView struct {
	GroupBy             models.FinanceSpendingSummaryGroupBy `json:"groupBy"`
	SpendingSummaryRows []*models.FinanceSpendingSummaryRow  `json:"spendingSummaryRows"`
	CurrencyTotals      []*FinanceCurrencyTotal              `json:"currencyTotals"`

	// ReportingCurrencyCode is what the converted figures are in: each
	// finance transaction converted at the rate of the day it posted. A
	// currency with no rate is left out of them and named in
	// UnconvertedCurrencyCodes.
	ReportingCurrencyCode        string                        `json:"reportingCurrencyCode,omitempty" graphapi:"nullable"`
	ConvertedSpendingSummaryRows []*FinanceConvertedSummaryRow `json:"convertedSpendingSummaryRows"`
	ConvertedMoneyOut            string                        `json:"convertedMoneyOut,omitempty" graphapi:"nullable"`
	ConvertedMoneyIn             string                        `json:"convertedMoneyIn,omitempty" graphapi:"nullable"`
	UnconvertedCurrencyCodes     []string                      `json:"unconvertedCurrencyCodes"`
}

// FinanceCurrencyTotal is money out and money in, both positive, in one
// currency.
type FinanceCurrencyTotal struct {
	CurrencyCode            string `json:"currencyCode"`
	MoneyOut                string `json:"moneyOut"`
	MoneyIn                 string `json:"moneyIn"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}

// FinanceConvertedSummaryRow is one group's money out and money in, in the
// reporting currency.
type FinanceConvertedSummaryRow struct {
	GroupKey   string `json:"groupKey"`
	GroupLabel string `json:"groupLabel"`
	MoneyOut   string `json:"moneyOut"`
	MoneyIn    string `json:"moneyIn"`
}

// ReportingCurrencyView is the currency totals are shown in.
type ReportingCurrencyView struct {
	// ReportingCurrencyCode is empty only when the person chose none and
	// has no finance account and no asset to fall back to; nothing is
	// converted then.
	ReportingCurrencyCode string `json:"reportingCurrencyCode"`

	// IsChosen says the person chose it (SetReportingCurrency), rather
	// than it being the fallback.
	IsChosen bool `json:"isChosen"`
}

// CurrencyConversionView is an amount converted, with the rate and the
// published day it is from.
type CurrencyConversionView struct {
	Amount           string            `json:"amount"`
	FromCurrencyCode string            `json:"fromCurrencyCode"`
	ConvertedAmount  string            `json:"convertedAmount"`
	ToCurrencyCode   string            `json:"toCurrencyCode"`
	Rate             string            `json:"rate"`
	RateOn           string            `json:"rateOn"`
	RateSource       models.RateSource `json:"rateSource"`
}

// --- arguments ----------------------------------------------------------

// FinanceAccountsArguments may name the currency to convert balances into
// instead of the reporting currency.
type FinanceAccountsArguments struct {
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

// FinanceTransactionsArguments narrow the listing; every one is optional.
type FinanceTransactionsArguments struct {
	// From and To bound the posted day, both included, "2006-01-02".
	From string `json:"from" graphapi:"nullable"`
	To   string `json:"to" graphapi:"nullable"`

	FinanceAccountID string `json:"financeAccountId" graphapi:"nullable"`

	// Text is matched within the description or the merchant.
	Text string `json:"text" graphapi:"nullable"`

	// MinimumAmount and MaximumAmount bound the signed amount; money out
	// is negative.
	MinimumAmount string `json:"minimumAmount" graphapi:"nullable"`
	MaximumAmount string `json:"maximumAmount" graphapi:"nullable"`

	// ProviderCategory matches the provider's primary or detailed category.
	ProviderCategory   string `json:"providerCategory" graphapi:"nullable"`
	SpendingCategoryID string `json:"spendingCategoryId" graphapi:"nullable"`

	// IsUncategorized keeps only finance transactions with no spending
	// category; a transfer has the transfer category.
	IsUncategorized *bool `json:"isUncategorized" graphapi:"nullable"`

	// DuplicateOfTransactionID keeps only the mirrored copies of this
	// finance transaction, its duplicates.
	DuplicateOfTransactionID string `json:"duplicateOfTransactionId" graphapi:"nullable"`

	// IsDuplicateIncluded lists the mirrored copies too. They are left
	// out otherwise, as every total leaves them out, except when asking
	// for a counted copy's duplicates or for finance transactions by id.
	IsDuplicateIncluded *bool `json:"isDuplicateIncluded" graphapi:"nullable"`

	// FinanceTransactionIDs keeps only these finance transactions, to read
	// one by its id.
	FinanceTransactionIDs []string `json:"financeTransactionIds" graphapi:"nullable"`

	// Limit is at most 200; zero is 50. After is the nextCursor of the
	// page before; Offset is how many to pass over, for a page by its
	// number, counted from After when both are given.
	Limit  *int   `json:"limit" graphapi:"nullable"`
	After  string `json:"after" graphapi:"nullable"`
	Offset *int   `json:"offset" graphapi:"nullable"`
}

// FinanceTradesArguments narrow a page of trades. Every field is
// optional.
type FinanceTradesArguments struct {
	// From and To bound the traded day, both included, "2006-01-02".
	From string `json:"from" graphapi:"nullable"`
	To   string `json:"to" graphapi:"nullable"`

	FinanceAccountID  string `json:"financeAccountId" graphapi:"nullable"`
	FinanceSecurityID string `json:"financeSecurityId" graphapi:"nullable"`

	// Limit is at most 200; zero is 50. After is the nextCursor of the
	// page before; Offset is how many to pass over, for a page by its
	// number, counted from After when both are given.
	Limit  *int   `json:"limit" graphapi:"nullable"`
	After  string `json:"after" graphapi:"nullable"`
	Offset *int   `json:"offset" graphapi:"nullable"`
}

// FinanceSpendingSummaryArguments say what a spending summary covers.
type FinanceSpendingSummaryArguments struct {
	// From and To bound the posted day, both included, "2006-01-02";
	// either may be left out.
	From string `json:"from" graphapi:"nullable"`
	To   string `json:"to" graphapi:"nullable"`

	// GroupBy is spendingCategory (the default), providerCategory,
	// merchant, month or financeAccount.
	GroupBy          string `json:"groupBy" graphapi:"nullable"`
	FinanceAccountID string `json:"financeAccountId" graphapi:"nullable"`

	// CurrencyCode converts into this currency instead of the reporting
	// currency.
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`
}

// ExchangeRateArguments name two currencies and a day ("2006-01-02",
// today when left out).
type ExchangeRateArguments struct {
	FromCurrencyCode string `json:"fromCurrencyCode"`
	ToCurrencyCode   string `json:"toCurrencyCode"`
	RateOn           string `json:"rateOn" graphapi:"nullable"`
}

// ConvertCurrencyArguments are an amount, its currency, the currency to
// convert it into, and the day ("2006-01-02", today when left out).
type ConvertCurrencyArguments struct {
	Amount           string `json:"amount"`
	FromCurrencyCode string `json:"fromCurrencyCode"`
	ToCurrencyCode   string `json:"toCurrencyCode"`
	RateOn           string `json:"rateOn" graphapi:"nullable"`
}

// SetReportingCurrencyArguments name the currency, an ISO 4217 code such
// as USD; empty clears it.
type SetReportingCurrencyArguments struct {
	CurrencyCode string `json:"currencyCode"`
}

// CreateFinanceLinkTokenArguments may name the finance source to repair.
type CreateFinanceLinkTokenArguments struct {
	SourceID string `json:"sourceId" graphapi:"nullable"`
}

// CompleteFinanceLinkArguments are what Plaid Link handed the page.
type CompleteFinanceLinkArguments struct {
	PublicToken     string `json:"publicToken"`
	InstitutionID   string `json:"institutionId" graphapi:"nullable"`
	InstitutionName string `json:"institutionName" graphapi:"nullable"`
}

// FinanceSourceArguments name one of the caller's finance sources.
type FinanceSourceArguments struct {
	SourceID string `json:"sourceId"`
}

// LinkSimpleFINArguments carry the setup token the person made on the
// SimpleFIN Bridge.
type LinkSimpleFINArguments struct {
	SetupToken string `json:"setupToken"`
}

// ImportFinanceCredentialArguments carry a credential the person brings
// from elsewhere: for Plaid its access token, for SimpleFIN its access
// URL. An institution name given here names the finance source; without
// one the provider is asked.
type ImportFinanceCredentialArguments struct {
	ProviderKind    string `json:"providerKind"`
	Credential      string `json:"credential"`
	InstitutionName string `json:"institutionName" graphapi:"nullable"`
}

// --- the switch, errors, and small readers ------------------------------

// errFinanceNotOffered is what the finance area answers on a server whose
// operator offers no provider, except reads of what is already stored.
var errFinanceNotOffered = fmt.Errorf("%w: finance is not offered on this server", api.ErrInvalidArguments)

// isFinanceOffered says the operator offers at least one provider people
// can link through.
func isFinanceOffered(configuration *config.Configuration) bool {
	for _, providerKind := range config.AgentFinanceProviders {
		if configuration.Agent.Finance.Offers(providerKind) {
			return true
		}
	}
	return false
}

// requireFinanceOffered refuses a write, or a lookup that may reach the
// European Central Bank, on a server that offers no provider.
func (self *graph) requireFinanceOffered() error {
	if !isFinanceOffered(self.config.Current()) {
		return errFinanceNotOffered
	}
	return nil
}

// financeProviderNames is what each provider is called in what people
// read.
var financeProviderNames = map[string]string{
	config.AgentFinanceProviderPlaid:     "Plaid",
	config.AgentFinanceProviderSimpleFIN: "SimpleFIN",
}

// requireFinanceProviderOffered refuses linking through a provider the
// operator does not offer, saying whether it is finance as a whole that
// is not offered.
func (self *graph) requireFinanceProviderOffered(providerKind string) error {
	configuration := self.config.Current()
	if configuration.Agent.Finance.Offers(providerKind) {
		return nil
	}
	if !isFinanceOffered(configuration) {
		return errFinanceNotOffered
	}
	return fmt.Errorf("%w: this server does not offer %s", api.ErrInvalidArguments, financeProviderNames[providerKind])
}

// financeRatesFetcher is the fetcher totals convert with. A variable so a
// test converts with the rates it stored and never reaches the network.
var financeRatesFetcher = func(database db.Database) *rates.Fetcher {
	return rates.Shared(database)
}

// exchangeRateFetcher is the fetcher for this request: none on a server
// that offers no provider, where totals of stored data convert with the
// rates already stored and nothing is fetched.
func (self *graph) exchangeRateFetcher() *rates.Fetcher {
	if !isFinanceOffered(self.config.Current()) {
		return nil
	}
	return financeRatesFetcher(self.database)
}

// financeError is a database error in the words the caller can act on: a
// refusal keeps its reason, and a currency with no rate says which.
func financeError(err error) error {
	if err == nil {
		return nil
	}
	var noRate *finance.ErrNoExchangeRate
	if errors.As(err, &noRate) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, noRate.Error())
	}
	if errors.Is(err, db.ErrInvalidArguments) {
		reason := strings.TrimPrefix(err.Error(), db.ErrInvalidArguments.Error())
		reason = strings.TrimPrefix(reason, ": ")
		if reason == "" {
			return api.ErrInvalidArguments
		}
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, reason)
	}
	return translateError(err)
}

// personToday is the caller's local day, "2006-01-02".
func personToday(principal *api.Principal) string {
	return time.Now().In(agent.Location(principal.User)).Format(time.DateOnly)
}

// personYesterday is the day before personToday.
func personYesterday(principal *api.Principal) string {
	return time.Now().In(agent.Location(principal.User)).AddDate(0, 0, -1).Format(time.DateOnly)
}

// dayArgument is a day written 2006-01-02, or the fallback when empty.
func dayArgument(name, value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return "", fmt.Errorf("%w: %s %q is not a day written 2006-01-02", api.ErrInvalidArguments, name, value)
	}
	return value, nil
}

// monthArgument is a month written 2006-01 (a day is taken for its month),
// or the fallback when empty.
func monthArgument(name, value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	month, err := time.Parse("2006-01", value)
	if err != nil {
		day, dayErr := time.Parse(time.DateOnly, value)
		if dayErr != nil {
			return "", fmt.Errorf("%w: %s %q is not a month written 2006-01", api.ErrInvalidArguments, name, value)
		}
		month = day
	}
	// Year 0 parses but is no date PostgreSQL holds, so a month that early
	// would fail as an internal error rather than as a bad argument.
	if month.Year() < agent.FirstBudgetYear {
		return "", fmt.Errorf("%w: %s %q is before %d", api.ErrInvalidArguments, name, value, agent.FirstBudgetYear)
	}
	return month.Format("2006-01"), nil
}

// yearArgument is a calendar year, "2006", from 1900 to ten years past
// today's ("2006-01-02"), or empty when none was given. A month given
// beside it is refused rather than one of them ignored.
func yearArgument(name, value, month, today string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(month) != "" {
		return "", fmt.Errorf("%w: give month or %s, not both", api.ErrInvalidArguments, name)
	}
	year, err := time.Parse("2006", value)
	if err != nil {
		return "", fmt.Errorf("%w: %s %q is not a year written 2006", api.ErrInvalidArguments, name, value)
	}
	if !agent.IsBudgetYear(year.Format("2006"), today) {
		return "", fmt.Errorf("%w: %s %q is not a year from %d to ten years from now", api.ErrInvalidArguments, name, value, agent.FirstBudgetYear)
	}
	return year.Format("2006"), nil
}

// amountArgument is a decimal amount in its canonical form.
func amountArgument(name, value string) (string, error) {
	canonical, err := finance.CanonicalAmount(strings.ReplaceAll(strings.TrimSpace(value), ",", ""))
	if err != nil {
		return "", fmt.Errorf("%w: %s %q is not an amount like 12.34", api.ErrInvalidArguments, name, value)
	}
	return canonical, nil
}

// optionalAmountArgument is amountArgument, keeping empty as empty.
func optionalAmountArgument(name, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return amountArgument(name, value)
}

// currencyArgument is an ISO 4217 code in capitals, or the fallback when
// empty.
func currencyArgument(name, value, fallback string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback, nil
	}
	if !models.IsCurrencyCode(value) {
		return "", fmt.Errorf("%w: %s %q is not a currency code like USD", api.ErrInvalidArguments, name, value)
	}
	return value, nil
}

// reportingCurrency is the currency totals are converted into: the one
// asked for, else the person's reporting currency, else the currency of
// their first finance account, else of their first asset; empty when they
// have none of these, and nothing is converted.
func reportingCurrency(tx db.Transaction, found *models.Agent, requested string) (string, error) {
	requested, err := currencyArgument("currencyCode", requested, "")
	if err != nil || requested != "" {
		return requested, err
	}
	if found.ReportingCurrencyCode != "" {
		return found.ReportingCurrencyCode, nil
	}
	accounts, err := tx.ListFinanceAccounts(found.ID, "")
	if err != nil {
		return "", err
	}
	if len(accounts) > 0 {
		return accounts[0].CurrencyCode, nil
	}
	assets, err := tx.ListAssets(found.ID)
	if err != nil {
		return "", err
	}
	if len(assets) > 0 {
		return assets[0].CurrencyCode, nil
	}
	return "", nil
}

// convertOrSkip converts an amount, answering false rather than an error
// when there is no exchange rate for it.
func convertOrSkip(converter *rates.Converter, amount, fromCurrencyCode, toCurrencyCode, on string) (*big.Rat, bool, error) {
	converted, _, err := converter.Convert(amount, fromCurrencyCode, toCurrencyCode, on)
	var noRate *finance.ErrNoExchangeRate
	if errors.As(err, &noRate) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	value, err := finance.ParseAmount(converted)
	return value, err == nil, err
}

// sortedCurrencyCodes is a set of currency codes in order.
func sortedCurrencyCodes(currencyCodes map[string]bool) []string {
	sorted := make([]string, 0, len(currencyCodes))
	for currencyCode := range currencyCodes {
		sorted = append(sorted, currencyCode)
	}
	sort.Strings(sorted)
	return sorted
}

// addAmount adds a decimal to a running sum, which it makes when nil.
func addAmount(sum *big.Rat, amount string) (*big.Rat, error) {
	value, err := finance.ParseAmount(amount)
	if err != nil {
		return sum, err
	}
	if sum == nil {
		sum = new(big.Rat)
	}
	return sum.Add(sum, value), nil
}

// formatOptionalAmount is an amount, or zero when there is none.
func formatOptionalAmount(value *big.Rat) string {
	if value == nil {
		return finance.FormatAmount(new(big.Rat))
	}
	return finance.FormatAmount(value)
}

// --- providers and linking ----------------------------------------------

// financeLinker is what linking through Plaid needs of its client. A
// variable builds it, so a test hands in one that answers to order.
type financeLinker interface {
	CreateLinkToken(ctx context.Context, personReference string, credentialForRepair string) (string, error)
	ExchangePublicToken(ctx context.Context, publicToken string) (credential, providerReference string, err error)
	InstitutionName(ctx context.Context, institutionId string) (string, error)
	DescribeCredential(ctx context.Context, credential string) (*finance.CredentialDescription, error)
	Remove(ctx context.Context, credential string) error
}

var newFinanceLinker = func(configuration *config.Configuration) (financeLinker, error) {
	plaid := &configuration.Agent.Finance.Plaid
	linker, err := finance.NewPlaid(plaid.Environment, plaid.ClientID, plaid.Secret, plaid.ResolvedCountryCodes(), plaid.ResolvedProducts())
	if err != nil {
		return nil, err
	}
	return linker, nil
}

// financeClaimer is what linking through SimpleFIN needs of its client.
type financeClaimer interface {
	Claim(ctx context.Context, setupToken string) (string, error)
	DescribeCredential(ctx context.Context, credential string) (*finance.CredentialDescription, error)
	Remove(ctx context.Context, credential string) error
}

var newFinanceClaimer = func() financeClaimer {
	return finance.NewSimpleFIN()
}

// financeRemoveTimeout bounds the provider call that undoes a link whose
// finance source could not be made.
const financeRemoveTimeout = 20 * time.Second

func (self *graph) FinanceProviders(ctx context.Context) ([]*FinanceProviderView, error) {
	if _, _, err := self.requireAgentPerson(ctx); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	providers := []*FinanceProviderView{}
	for _, providerKind := range config.AgentFinanceProviders {
		if configuration.Agent.Finance.Offers(providerKind) {
			providers = append(providers, &FinanceProviderView{
				ProviderKind: providerKind, IsBrowserRequired: providerKind == config.AgentFinanceProviderPlaid,
			})
		}
	}
	return providers, nil
}

// ownFinanceSource is one of the caller's finance sources, locked, or not
// found: somebody else's and a source of another kind alike.
func ownFinanceSource(tx db.Transaction, found *models.Agent, sourceId string) (*models.AgentKnowledgeSource, error) {
	if strings.TrimSpace(sourceId) == "" {
		return nil, fmt.Errorf("%w: which finance source", api.ErrInvalidArguments)
	}
	source, err := tx.LockAgentSource(found.ID, strings.TrimSpace(sourceId))
	if err != nil {
		return nil, err
	}
	if source == nil || source.Kind != models.SourceFinance {
		return nil, api.ErrNotFound
	}
	return source, nil
}

// financeCredentialOf opens a finance source's credential.
func (self *graph) financeCredentialOf(tx db.Transaction, worker *agent.Agent, source *models.AgentKnowledgeSource) (string, error) {
	secrets, err := tx.ListAgentSourceSecrets(source.ID)
	if err != nil {
		return "", err
	}
	for _, secret := range secrets {
		if secret.Key == models.FinanceCredentialSecretKey && secret.Value != "" {
			return worker.OpenSecret(secret.Value)
		}
	}
	return "", fmt.Errorf("%w: this finance source has no credential; delete it and link the institution again", api.ErrInvalidArguments)
}

func (self *graph) CreateFinanceLinkToken(ctx context.Context, arguments CreateFinanceLinkTokenArguments) (*FinanceLinkTokenView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceProviderOffered(config.AgentFinanceProviderPlaid); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	credentialForRepair := ""
	sourceId := strings.TrimSpace(arguments.SourceID)
	if sourceId != "" {
		tx := self.writing(ctx)
		source, err := ownFinanceSource(tx, found, sourceId)
		if err != nil {
			return nil, err
		}
		if source.Specification.Type != config.AgentFinanceProviderPlaid {
			return nil, fmt.Errorf("%w: this finance source is not linked through Plaid", api.ErrInvalidArguments)
		}
		worker := self.agentWorker()
		if worker == nil {
			return nil, agent.ErrUnavailable
		}
		if credentialForRepair, err = self.financeCredentialOf(tx, worker, source); err != nil {
			return nil, err
		}
	}
	linker, err := newFinanceLinker(configuration)
	if err != nil {
		return nil, err
	}
	// The agent's id is the person reference Plaid is given: stable, and
	// saying nothing about who the person is.
	linkToken, err := linker.CreateLinkToken(ctx, found.ID, credentialForRepair)
	if err != nil {
		return nil, err
	}
	return &FinanceLinkTokenView{LinkToken: linkToken, SourceID: sourceId}, nil
}

func (self *graph) CompleteFinanceLink(ctx context.Context, arguments CompleteFinanceLinkArguments) (*FinanceSourceView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceProviderOffered(config.AgentFinanceProviderPlaid); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	publicToken := strings.TrimSpace(arguments.PublicToken)
	if publicToken == "" {
		return nil, fmt.Errorf("%w: the public token Plaid Link handed over is needed", api.ErrInvalidArguments)
	}
	// Checked before the exchange: after it there is a finance source at
	// Plaid that has to be made here or ended there.
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	linker, err := newFinanceLinker(configuration)
	if err != nil {
		return nil, err
	}
	credential, providerReference, err := linker.ExchangePublicToken(ctx, publicToken)
	if err != nil {
		return nil, err
	}
	institutionId := strings.TrimSpace(arguments.InstitutionID)
	institutionName := strings.TrimSpace(arguments.InstitutionName)
	if institutionName == "" && institutionId != "" {
		// A name is what the person recognizes the source by; without
		// one the source is named for the provider, and the sync does
		// not rename it.
		if named, err := linker.InstitutionName(ctx, institutionId); err == nil {
			institutionName = named
		}
	}
	source, err := self.createFinanceSourceCommitted(ctx, worker, found, config.AgentFinanceProviderPlaid, models.FinanceSourceSettings{
		InstitutionID: institutionId, InstitutionName: institutionName, ProviderReference: providerReference,
	}, credential)
	if errors.Is(err, errFinanceSourceExists) {
		// The link is one the person already has here, which ending it
		// would break.
		return nil, err
	}
	if err != nil {
		// Ended at Plaid at once: a finance source nobody can reach still
		// costs the operator, and on some plans uses up a slot for good.
		removeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), financeRemoveTimeout)
		defer cancel()
		if removeErr := linker.Remove(removeContext, credential); removeErr != nil {
			log.Warningf("a Plaid link for agent %q could not be kept and could not be ended at Plaid: %s", found.ID, removeErr)
		}
		return nil, err
	}
	// The finance source is committed and kept from here on, whatever
	// reading it back says.
	return financeSourceView(self.transaction(ctx), found, source)
}

func (self *graph) LinkSimpleFIN(ctx context.Context, arguments LinkSimpleFINArguments) (*FinanceSourceView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceProviderOffered(config.AgentFinanceProviderSimpleFIN); err != nil {
		return nil, err
	}
	setupToken := strings.TrimSpace(arguments.SetupToken)
	if setupToken == "" {
		return nil, fmt.Errorf("%w: the setup token from the SimpleFIN Bridge is needed", api.ErrInvalidArguments)
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	claimer := newFinanceClaimer()
	credential, err := claimer.Claim(ctx, setupToken)
	if err != nil {
		return nil, fmt.Errorf("%w: the SimpleFIN Bridge did not accept the setup token: %s", api.ErrInvalidArguments, err)
	}
	source, err := self.createFinanceSourceCommitted(ctx, worker, found, config.AgentFinanceProviderSimpleFIN, models.FinanceSourceSettings{}, credential)
	if err != nil {
		removeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), financeRemoveTimeout)
		defer cancel()
		_ = claimer.Remove(removeContext, credential)
		// A setup token is claimed once: the one given is used up.
		return nil, fmt.Errorf("the finance source could not be made, and the setup token is used up; make a new one on the SimpleFIN Bridge: %w", err)
	}
	return financeSourceView(self.transaction(ctx), found, source)
}

func (self *graph) ImportFinanceCredential(ctx context.Context, arguments ImportFinanceCredentialArguments) (*FinanceSourceView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	providerKind := strings.ToLower(strings.TrimSpace(arguments.ProviderKind))
	if _, isProvider := financeProviderNames[providerKind]; !isProvider {
		return nil, fmt.Errorf("%w: providerKind is %s or %s", api.ErrInvalidArguments, config.AgentFinanceProviderPlaid, config.AgentFinanceProviderSimpleFIN)
	}
	if err := self.requireFinanceProviderOffered(providerKind); err != nil {
		return nil, err
	}
	credential := strings.TrimSpace(arguments.Credential)
	if credential == "" {
		return nil, fmt.Errorf("%w: the credential is needed: for Plaid its access token, for SimpleFIN its access URL", api.ErrInvalidArguments)
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	institutionName := strings.TrimSpace(arguments.InstitutionName)
	settings := models.FinanceSourceSettings{}
	switch providerKind {
	case config.AgentFinanceProviderPlaid:
		linker, err := newFinanceLinker(self.config.Current())
		if err != nil {
			return nil, err
		}
		description, err := linker.DescribeCredential(ctx, credential)
		if err != nil {
			return nil, credentialRefusal(providerKind, credential, err)
		}
		if institutionName == "" && description.InstitutionID != "" {
			if named, err := linker.InstitutionName(ctx, description.InstitutionID); err == nil {
				institutionName = strings.TrimSpace(named)
			}
		}
		settings = models.FinanceSourceSettings{
			InstitutionID: description.InstitutionID, InstitutionName: institutionName, ProviderReference: description.ProviderReference,
		}
	case config.AgentFinanceProviderSimpleFIN:
		// The address is the person's to choose, so it has to be one
		// safefetch would connect to, over https, before anything is sent.
		if err := finance.CheckSimpleFINCredential(credential); err != nil {
			return nil, credentialRefusal(providerKind, credential, err)
		}
		description, err := newFinanceClaimer().DescribeCredential(ctx, credential)
		if err != nil {
			return nil, credentialRefusal(providerKind, credential, err)
		}
		if institutionName == "" {
			institutionName = description.InstitutionName
		}
		settings = models.FinanceSourceSettings{InstitutionName: institutionName}
	}
	source, err := self.createFinanceSourceCommitted(ctx, worker, found, providerKind, settings, credential)
	if err != nil {
		// Unlike a link made here, nothing is ended at the provider: the
		// connection was the person's before it came here, and stays so.
		return nil, err
	}
	return financeSourceView(self.transaction(ctx), found, source)
}

// credentialRefusal is a provider's refusal of a credential brought from
// elsewhere, in words the person can act on. The credential never appears
// in it, even where a provider's own message would have repeated it.
func credentialRefusal(providerKind, credential string, err error) error {
	providerName := financeProviderNames[providerKind]
	if errors.Is(err, finance.ErrCredentialRefused) {
		if providerKind == config.AgentFinanceProviderPlaid {
			return fmt.Errorf("%w: Plaid does not accept this credential: it was removed, is mistyped, or was made with a Plaid client id other than this server's", api.ErrInvalidArguments)
		}
		return fmt.Errorf("%w: %s refused this credential: it may have been revoked", api.ErrInvalidArguments, providerName)
	}
	reason := strings.ReplaceAll(err.Error(), credential, "the credential")
	return fmt.Errorf("%w: %s did not accept this credential: %s", api.ErrInvalidArguments, providerName, reason)
}

// createFinanceSourceCommitted makes the finance source in a transaction
// of its own, committed before it returns. The request's transaction
// commits only after the resolver has answered, and a commit that failed
// then would leave the link open at the provider with nothing here to end
// it; committed here, a failure is an error the caller sees while it can
// still end the link.
func (self *graph) createFinanceSourceCommitted(ctx context.Context, worker *agent.Agent, found *models.Agent, providerKind string, settings models.FinanceSourceSettings, credential string) (*models.AgentKnowledgeSource, error) {
	var source *models.AgentKnowledgeSource
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorPerson)
		var err error
		source, err = self.createFinanceSource(tx, worker, found, providerKind, settings, credential)
		return err
	})
	if err != nil {
		return nil, err
	}
	return source, nil
}

// errFinanceSourceExists refuses a second finance source for a link the
// person already has here: two would sync the same accounts twice.
var errFinanceSourceExists = fmt.Errorf("%w: this connection is already one of your finance sources", api.ErrInvalidArguments)

// createFinanceSource makes a finance source for a credential a provider
// handed over or the person brought: the source row, due to sync now on
// the default schedule, the credential sealed beside it through the same
// call SetAgentKnowledgeSourceSecret makes, and the default spending
// categories for a person who has none yet. A provider reference one of
// the person's finance sources already holds is refused.
func (self *graph) createFinanceSource(tx db.Transaction, worker *agent.Agent, found *models.Agent, providerKind string, settings models.FinanceSourceSettings, credential string) (*models.AgentKnowledgeSource, error) {
	if settings.ProviderReference != "" {
		existingSources, err := financeSourcesOf(tx, found.ID)
		if err != nil {
			return nil, err
		}
		for _, existingSource := range existingSources {
			existingSettings, _ := existingSource.FinanceSourceSettings()
			if existingSource.Specification.Type == providerKind && existingSettings.ProviderReference == settings.ProviderReference {
				return nil, fmt.Errorf("%w (%s)", errFinanceSourceExists, existingSource.Name)
			}
		}
	}
	sealed, err := worker.SealSecret(credential)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	name := settings.InstitutionName
	if name == "" {
		name = financeProviderNames[providerKind]
	}
	now := time.Now()
	source := &models.AgentKnowledgeSource{
		AgentID: found.ID, Kind: models.SourceFinance, Name: name, Enabled: true,
		Specification: models.AgentKnowledgeSpecification{Type: providerKind, Settings: encoded},
		Cron:          models.FinanceSourceCron, NextRunAt: &now,
	}
	saved, err := tx.PutAgentSource(source)
	if err != nil {
		return nil, financeError(err)
	}
	if err := tx.PutAgentSourceSecret(found.ID, &models.AgentSourceSecret{SourceID: saved.ID, Key: models.FinanceCredentialSecretKey, Value: sealed}); err != nil {
		return nil, err
	}
	if _, err := tx.EnsureDefaultSpendingCategories(found.ID); err != nil {
		return nil, err
	}
	return saved, nil
}

func (self *graph) CompleteFinanceRepair(ctx context.Context, arguments FinanceSourceArguments) (*FinanceSourceView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	tx := self.writing(ctx)
	source, err := ownFinanceSource(tx, found, arguments.SourceID)
	if err != nil {
		return nil, err
	}
	// Only the sign-in flag is cleared: the last error stays until the
	// next sync says how it went, so a repair that did not take is not
	// reported as fixed. A credential the provider revoked is not cleared
	// either, since signing in again does not bring back a credential
	// that no longer exists; that finance source is deleted and linked
	// again.
	delete(source.Cursor, models.FinanceCursorIsSignInRequired)
	now := time.Now()
	source.NextRunAt = &now
	saved, err := tx.PutAgentSource(source)
	if err != nil {
		return nil, financeError(err)
	}
	return financeSourceView(tx, found, saved)
}

func (self *graph) ReportingCurrency(ctx context.Context) (*ReportingCurrencyView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	currencyCode, err := reportingCurrency(self.transaction(ctx), found, "")
	if err != nil {
		return nil, err
	}
	return &ReportingCurrencyView{ReportingCurrencyCode: currencyCode, IsChosen: found.ReportingCurrencyCode != ""}, nil
}

func (self *graph) SetReportingCurrency(ctx context.Context, arguments SetReportingCurrencyArguments) (string, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return "", err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return "", err
	}
	currencyCode, err := currencyArgument("currencyCode", arguments.CurrencyCode, "")
	if err != nil {
		return "", err
	}
	updated, err := self.writing(ctx).UpdateAgent(found.ID, func(changed *models.Agent) error {
		changed.ReportingCurrencyCode = currencyCode
		return nil
	})
	if err != nil {
		return "", financeError(err)
	}
	return updated.ReportingCurrencyCode, nil
}

// --- sources and accounts -----------------------------------------------

// financeSourceView is a finance source as the API shows it, with its
// finance accounts.
func financeSourceView(tx db.Transaction, found *models.Agent, source *models.AgentKnowledgeSource) (*FinanceSourceView, error) {
	settings, _ := source.FinanceSourceSettings()
	view := &FinanceSourceView{
		ID: source.ID, Name: source.Name, ProviderKind: source.Specification.Type,
		InstitutionID: settings.InstitutionID, InstitutionName: settings.InstitutionName,
		IsEnabled: source.Enabled, Cron: source.Cron, LastRunAt: source.LastRunAt, NextRunAt: source.NextRunAt,
		LastError: source.LastError, IsSignInRequired: source.IsFinanceSignInRequired(),
		FinanceAccounts: []*FinanceAccountView{}, CreatedAt: source.CreatedAt,
	}
	accounts, err := tx.ListFinanceAccounts(found.ID, source.ID)
	if err != nil {
		return nil, err
	}
	institutionNames := map[string]bool{}
	for _, account := range accounts {
		accountView := financeAccountView(account, source)
		view.FinanceAccounts = append(view.FinanceAccounts, accountView)
		if accountView.InstitutionName != "" {
			institutionNames[accountView.InstitutionName] = true
		}
	}
	// A SimpleFIN finance source can reach several institutions, each
	// account naming its own; the source has one name when they agree.
	if view.InstitutionName == "" && len(institutionNames) == 1 {
		for institutionName := range institutionNames {
			view.InstitutionName = institutionName
		}
	}
	return view, nil
}

// financeAccountView is a finance account with its source's institution
// and state; the conversion is the caller's to fill in.
func financeAccountView(account *models.FinanceAccount, source *models.AgentKnowledgeSource) *FinanceAccountView {
	view := &FinanceAccountView{
		ID: account.ID, SourceID: account.SourceID, AccountName: account.AccountName, AccountMask: account.AccountMask,
		AccountKind: account.AccountKind, CurrencyCode: account.CurrencyCode, CurrentBalance: account.CurrentBalance,
		AvailableBalance: account.AvailableBalance, BalanceAt: account.BalanceAt, CreditLimitAmount: account.CreditLimitAmount,
		ProviderMetadata: account.ProviderMetadata, CreatedAt: account.CreatedAt, ModifiedAt: account.ModifiedAt,
	}
	view.InstitutionName = accountInstitutionName(account.ProviderMetadata)
	if source != nil {
		settings, _ := source.FinanceSourceSettings()
		if settings.InstitutionName != "" {
			view.InstitutionName = settings.InstitutionName
		}
		view.ProviderKind = source.Specification.Type
		view.IsSignInRequired = source.IsFinanceSignInRequired()
	}
	return view
}

// accountInstitutionName is the institution a finance account's provider
// metadata names: SimpleFIN gives each account its institution ("org"),
// since one of its finance sources can reach several, and the finance
// source itself keeps none; an imported statement names the institution
// that wrote it ("institutionOrganization").
func accountInstitutionName(providerMetadata json.RawMessage) string {
	if len(providerMetadata) == 0 {
		return ""
	}
	var metadata struct {
		Organization struct {
			Name string `json:"name"`
		} `json:"org"`
		InstitutionOrganization string `json:"institutionOrganization"`
	}
	if json.Unmarshal(providerMetadata, &metadata) != nil {
		return ""
	}
	if name := strings.TrimSpace(metadata.Organization.Name); name != "" {
		return name
	}
	return strings.TrimSpace(metadata.InstitutionOrganization)
}

// financeSourcesOf is the agent's finance sources by id.
func financeSourcesOf(tx db.Transaction, agentId string) ([]*models.AgentKnowledgeSource, error) {
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return nil, err
	}
	financeSources := []*models.AgentKnowledgeSource{}
	for _, source := range sources {
		if source.Kind == models.SourceFinance {
			financeSources = append(financeSources, source)
		}
	}
	return financeSources, nil
}

func (self *graph) FinanceSources(ctx context.Context) ([]*FinanceSourceView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	sources, err := financeSourcesOf(tx, found.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*FinanceSourceView, 0, len(sources))
	for _, source := range sources {
		view, err := financeSourceView(tx, found, source)
		if err != nil {
			return nil, err
		}
		// The statement source is made when somebody first looks at their
		// import address, before anything was imported; it is a finance
		// source to list once it holds an account, and not before, so
		// having looked does not read as having linked something.
		if agent.IsStatementSource(source) && len(view.FinanceAccounts) == 0 {
			continue
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) FinanceAccounts(ctx context.Context, arguments FinanceAccountsArguments) ([]*FinanceAccountView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	sources, err := financeSourcesOf(tx, found.ID)
	if err != nil {
		return nil, err
	}
	sourceById := map[string]*models.AgentKnowledgeSource{}
	for _, source := range sources {
		sourceById[source.ID] = source
	}
	accounts, err := tx.ListFinanceAccounts(found.ID, "")
	if err != nil {
		return nil, err
	}
	converter := rates.NewConverter(ctx, self.exchangeRateFetcher(), tx)
	today := personToday(principal)
	views := make([]*FinanceAccountView, 0, len(accounts))
	for _, account := range accounts {
		view := financeAccountView(account, sourceById[account.SourceID])
		view.ReportingCurrencyCode = currencyCode
		if currencyCode != "" && account.CurrentBalance != "" {
			on := today
			if account.BalanceAt != nil {
				on = account.BalanceAt.In(agent.Location(principal.User)).Format(time.DateOnly)
			}
			converted, isConverted, err := convertOrSkip(converter, account.CurrentBalance, account.CurrencyCode, currencyCode, on)
			if err != nil {
				return nil, err
			}
			if isConverted {
				view.ConvertedCurrentBalance = finance.FormatAmount(converted)
			}
		}
		views = append(views, view)
	}
	return views, nil
}

// --- transactions and the spending summary -------------------------------

func (self *graph) FinanceTransactions(ctx context.Context, arguments FinanceTransactionsArguments) (*FinanceTransactionPageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	filter := &db.FinanceTransactionFilter{
		FinanceAccountID: strings.TrimSpace(arguments.FinanceAccountID), Text: strings.TrimSpace(arguments.Text),
		ProviderCategory: strings.TrimSpace(arguments.ProviderCategory), SpendingCategoryID: strings.TrimSpace(arguments.SpendingCategoryID),
		DuplicateOfTransactionID: strings.TrimSpace(arguments.DuplicateOfTransactionID), After: strings.TrimSpace(arguments.After),
	}
	// A blank id is refused rather than dropped: dropping the only one
	// would list every finance transaction instead of none.
	for _, financeTransactionId := range arguments.FinanceTransactionIDs {
		trimmed := strings.TrimSpace(financeTransactionId)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: financeTransactionIds holds an empty id", api.ErrInvalidArguments)
		}
		filter.FinanceTransactionIDs = append(filter.FinanceTransactionIDs, trimmed)
	}
	if filter.From, err = dayArgument("from", arguments.From, ""); err != nil {
		return nil, err
	}
	if filter.To, err = dayArgument("to", arguments.To, ""); err != nil {
		return nil, err
	}
	if filter.MinimumAmount, err = optionalAmountArgument("minimumAmount", arguments.MinimumAmount); err != nil {
		return nil, err
	}
	if filter.MaximumAmount, err = optionalAmountArgument("maximumAmount", arguments.MaximumAmount); err != nil {
		return nil, err
	}
	if arguments.IsUncategorized != nil {
		filter.IsUncategorized = *arguments.IsUncategorized
	}
	isDuplicateIncluded := arguments.IsDuplicateIncluded != nil && *arguments.IsDuplicateIncluded
	filter.IsDuplicateExcluded = !isDuplicateIncluded && filter.DuplicateOfTransactionID == "" && len(filter.FinanceTransactionIDs) == 0
	if arguments.Limit != nil {
		if *arguments.Limit < 0 {
			return nil, fmt.Errorf("%w: limit cannot be negative", api.ErrInvalidArguments)
		}
		filter.Limit = *arguments.Limit
	}
	if filter.Offset, err = offsetArgument(arguments.Offset); err != nil {
		return nil, err
	}
	filter.ShouldCountTotal, filter.ShouldReadIDsOnly = financeTransactionsSelection(ctx)
	page, err := self.transaction(ctx).ListFinanceTransactions(found.ID, filter)
	if err != nil {
		return nil, financeError(err)
	}
	transactions := page.FinanceTransactions
	if transactions == nil {
		transactions = []*models.FinanceTransaction{}
	}
	return &FinanceTransactionPageView{
		FinanceTransactions: transactions, NextCursor: page.NextCursor, TotalCount: page.TotalCount, LeftOutDuplicateCount: page.LeftOutDuplicateCount,
	}, nil
}

// financeTransactionsSelection is what a read of FinanceTransactions
// asks for, so the work for what it does not is left out: the counts
// unless totalCount or leftOutDuplicateCount is selected, and every field
// of the rows but the id when the id is all that is selected of them, as
// Select all reads them. A query whose fields cannot be followed gets
// everything.
func financeTransactionsSelection(ctx context.Context) (shouldCountTotal bool, shouldReadIDsOnly bool) {
	fieldNames, isKnown := graphapi.SelectedFieldNames(ctx)
	if !isKnown {
		return true, false
	}
	shouldCountTotal = slices.Contains(fieldNames, "totalCount") || slices.Contains(fieldNames, "leftOutDuplicateCount")
	rowFieldNames, isKnown := graphapi.SelectedFieldNames(ctx, "financeTransactions")
	if !isKnown {
		return shouldCountTotal, false
	}
	shouldReadIDsOnly = !slices.ContainsFunc(rowFieldNames, func(fieldName string) bool {
		return fieldName != "id" && fieldName != "__typename"
	})
	return shouldCountTotal, shouldReadIDsOnly
}

func (self *graph) FinanceTrades(ctx context.Context, arguments FinanceTradesArguments) (*FinanceTradePageView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	filter := &db.FinanceTradeFilter{
		FinanceAccountID: strings.TrimSpace(arguments.FinanceAccountID), FinanceSecurityID: strings.TrimSpace(arguments.FinanceSecurityID),
		After: strings.TrimSpace(arguments.After),
	}
	if filter.From, err = dayArgument("from", arguments.From, ""); err != nil {
		return nil, err
	}
	if filter.To, err = dayArgument("to", arguments.To, ""); err != nil {
		return nil, err
	}
	if arguments.Limit != nil {
		if *arguments.Limit < 0 {
			return nil, fmt.Errorf("%w: limit cannot be negative", api.ErrInvalidArguments)
		}
		filter.Limit = *arguments.Limit
	}
	if filter.Offset, err = offsetArgument(arguments.Offset); err != nil {
		return nil, err
	}
	filter.ShouldCountTotal = true
	page, err := self.transaction(ctx).ListFinanceTrades(found.ID, filter)
	if err != nil {
		return nil, financeError(err)
	}
	trades := page.FinanceTrades
	if trades == nil {
		trades = []*models.FinanceTrade{}
	}
	return &FinanceTradePageView{FinanceTrades: trades, NextCursor: page.NextCursor, TotalCount: page.TotalCount}, nil
}

// offsetArgument is how many rows a page passes over: none when it is
// not given, refused when negative.
func offsetArgument(offset *int) (int, error) {
	if offset == nil {
		return 0, nil
	}
	if *offset < 0 {
		return 0, fmt.Errorf("%w: offset cannot be negative", api.ErrInvalidArguments)
	}
	return *offset, nil
}

// financeTransactionGroupKey is the key a finance transaction is summed
// under, the same as the spending summary's query gives it.
func financeTransactionGroupKey(financeTransaction *models.FinanceTransaction, groupBy models.FinanceSpendingSummaryGroupBy) string {
	switch groupBy {
	case models.FinanceSpendingSummaryGroupByProviderCategory:
		if financeTransaction.ProviderCategoryPrimary != "" {
			return financeTransaction.ProviderCategoryPrimary
		}
		return financeTransaction.ProviderCategoryDetailed
	case models.FinanceSpendingSummaryGroupByMerchant:
		if financeTransaction.MerchantName != "" {
			return financeTransaction.MerchantName
		}
		return financeTransaction.Description
	case models.FinanceSpendingSummaryGroupByMonth:
		if len(financeTransaction.PostedOn) >= len("2006-01") {
			return financeTransaction.PostedOn[:len("2006-01")]
		}
		return financeTransaction.PostedOn
	case models.FinanceSpendingSummaryGroupByFinanceAccount:
		return financeTransaction.FinanceAccountID
	}
	return financeTransaction.SpendingCategoryID
}

// financeTransactionsScanned is the most finance transactions one
// conversion reads, two hundred at a time: years of a household's
// accounts, and a bound on a request that asks for all of time.
const financeTransactionsScanned = 100000

// convertedGroups is money out and money in per group in one currency:
// the rows already in it as they are, and every finance transaction in
// another currency converted at the rate of the day it posted. A currency
// with no rate is left out entirely and named.
type convertedGroups struct {
	moneyOutByGroup map[string]*big.Rat
	moneyInByGroup  map[string]*big.Rat
	unconverted     map[string]bool
}

func convertSpendingGroups(tx db.Transaction, converter *rates.Converter, agentId string, filter *db.FinanceSpendingSummaryFilter, groupBy models.FinanceSpendingSummaryGroupBy, currencyCode string, rows []*models.FinanceSpendingSummaryRow) (*convertedGroups, error) {
	groups := &convertedGroups{moneyOutByGroup: map[string]*big.Rat{}, moneyInByGroup: map[string]*big.Rat{}, unconverted: map[string]bool{}}
	isForeign := map[string]bool{}
	for _, row := range rows {
		if row.CurrencyCode != currencyCode {
			isForeign[row.CurrencyCode] = true
			continue
		}
		var err error
		if groups.moneyOutByGroup[row.GroupKey], err = addAmount(groups.moneyOutByGroup[row.GroupKey], row.MoneyOut); err != nil {
			return nil, err
		}
		if groups.moneyInByGroup[row.GroupKey], err = addAmount(groups.moneyInByGroup[row.GroupKey], row.MoneyIn); err != nil {
			return nil, err
		}
	}
	if len(isForeign) == 0 {
		return groups, nil
	}
	type currencyGroup struct {
		currencyCode string
		groupKey     string
	}
	foreignOut, foreignIn := map[currencyGroup]*big.Rat{}, map[currencyGroup]*big.Rat{}
	after := ""
	for scanned := 0; scanned < financeTransactionsScanned; {
		page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{
			From: filter.From, To: filter.To, FinanceAccountID: filter.FinanceAccountID, IsTransferExcluded: true, IsDuplicateExcluded: true,
			Limit: db.FinanceTransactionLimitMost, After: after,
		})
		if err != nil {
			return nil, err
		}
		for _, financeTransaction := range page.FinanceTransactions {
			scanned++
			if !isForeign[financeTransaction.CurrencyCode] || groups.unconverted[financeTransaction.CurrencyCode] {
				continue
			}
			converted, isConverted, err := convertOrSkip(converter, financeTransaction.Amount, financeTransaction.CurrencyCode, currencyCode, financeTransaction.PostedOn)
			if err != nil {
				return nil, err
			}
			if !isConverted {
				groups.unconverted[financeTransaction.CurrencyCode] = true
				continue
			}
			key := currencyGroup{currencyCode: financeTransaction.CurrencyCode, groupKey: financeTransactionGroupKey(financeTransaction, groupBy)}
			sums := foreignIn
			if converted.Sign() < 0 {
				sums = foreignOut
				converted.Neg(converted)
			}
			if sums[key] == nil {
				sums[key] = new(big.Rat)
			}
			sums[key].Add(sums[key], converted)
		}
		if page.NextCursor == "" || len(page.FinanceTransactions) == 0 {
			break
		}
		after = page.NextCursor
	}
	for key, amount := range foreignOut {
		if groups.unconverted[key.currencyCode] {
			continue
		}
		if groups.moneyOutByGroup[key.groupKey] == nil {
			groups.moneyOutByGroup[key.groupKey] = new(big.Rat)
		}
		groups.moneyOutByGroup[key.groupKey].Add(groups.moneyOutByGroup[key.groupKey], amount)
	}
	for key, amount := range foreignIn {
		if groups.unconverted[key.currencyCode] {
			continue
		}
		if groups.moneyInByGroup[key.groupKey] == nil {
			groups.moneyInByGroup[key.groupKey] = new(big.Rat)
		}
		groups.moneyInByGroup[key.groupKey].Add(groups.moneyInByGroup[key.groupKey], amount)
	}
	return groups, nil
}

func (self *graph) FinanceSpendingSummary(ctx context.Context, arguments FinanceSpendingSummaryArguments) (*FinanceSpendingSummaryView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	groupBy := models.FinanceSpendingSummaryGroupBy(strings.TrimSpace(arguments.GroupBy))
	if groupBy == "" {
		groupBy = models.FinanceSpendingSummaryGroupBySpendingCategory
	}
	if !groupBy.IsValid() {
		return nil, fmt.Errorf("%w: groupBy %q is not spendingCategory, providerCategory, merchant, month or financeAccount", api.ErrInvalidArguments, groupBy)
	}
	filter := &db.FinanceSpendingSummaryFilter{GroupBy: groupBy, FinanceAccountID: strings.TrimSpace(arguments.FinanceAccountID)}
	if filter.From, err = dayArgument("from", arguments.From, ""); err != nil {
		return nil, err
	}
	if filter.To, err = dayArgument("to", arguments.To, ""); err != nil {
		return nil, err
	}
	currencyCode, err := reportingCurrency(tx, found, arguments.CurrencyCode)
	if err != nil {
		return nil, err
	}
	rows, err := tx.FinanceSpendingSummary(found.ID, filter)
	if err != nil {
		return nil, financeError(err)
	}
	view := &FinanceSpendingSummaryView{
		GroupBy: groupBy, SpendingSummaryRows: rows, CurrencyTotals: []*FinanceCurrencyTotal{},
		ReportingCurrencyCode: currencyCode, ConvertedSpendingSummaryRows: []*FinanceConvertedSummaryRow{},
		UnconvertedCurrencyCodes: []string{},
	}
	if view.SpendingSummaryRows == nil {
		view.SpendingSummaryRows = []*models.FinanceSpendingSummaryRow{}
	}
	totalByCurrency := map[string]*FinanceCurrencyTotal{}
	moneyOutByCurrency, moneyInByCurrency := map[string]*big.Rat{}, map[string]*big.Rat{}
	labelByGroup := map[string]string{}
	groupOrder := []string{}
	for _, row := range rows {
		if _, isSeen := labelByGroup[row.GroupKey]; !isSeen {
			groupOrder = append(groupOrder, row.GroupKey)
		}
		labelByGroup[row.GroupKey] = row.GroupLabel
		total := totalByCurrency[row.CurrencyCode]
		if total == nil {
			total = &FinanceCurrencyTotal{CurrencyCode: row.CurrencyCode}
			totalByCurrency[row.CurrencyCode] = total
			view.CurrencyTotals = append(view.CurrencyTotals, total)
		}
		total.FinanceTransactionCount += row.FinanceTransactionCount
		if moneyOutByCurrency[row.CurrencyCode], err = addAmount(moneyOutByCurrency[row.CurrencyCode], row.MoneyOut); err != nil {
			return nil, err
		}
		if moneyInByCurrency[row.CurrencyCode], err = addAmount(moneyInByCurrency[row.CurrencyCode], row.MoneyIn); err != nil {
			return nil, err
		}
	}
	for _, total := range view.CurrencyTotals {
		total.MoneyOut = formatOptionalAmount(moneyOutByCurrency[total.CurrencyCode])
		total.MoneyIn = formatOptionalAmount(moneyInByCurrency[total.CurrencyCode])
	}
	if currencyCode == "" {
		return view, nil
	}
	groups, err := convertSpendingGroups(tx, rates.NewConverter(ctx, self.exchangeRateFetcher(), tx), found.ID, filter, groupBy, currencyCode, rows)
	if err != nil {
		return nil, financeError(err)
	}
	totalOut, totalIn := new(big.Rat), new(big.Rat)
	for _, groupKey := range groupOrder {
		moneyOut, moneyIn := groups.moneyOutByGroup[groupKey], groups.moneyInByGroup[groupKey]
		if moneyOut == nil && moneyIn == nil {
			continue
		}
		view.ConvertedSpendingSummaryRows = append(view.ConvertedSpendingSummaryRows, &FinanceConvertedSummaryRow{
			GroupKey: groupKey, GroupLabel: labelByGroup[groupKey],
			MoneyOut: formatOptionalAmount(moneyOut), MoneyIn: formatOptionalAmount(moneyIn),
		})
		if moneyOut != nil {
			totalOut.Add(totalOut, moneyOut)
		}
		if moneyIn != nil {
			totalIn.Add(totalIn, moneyIn)
		}
	}
	sort.SliceStable(view.ConvertedSpendingSummaryRows, func(left, right int) bool {
		leftOut, _ := finance.ParseAmount(view.ConvertedSpendingSummaryRows[left].MoneyOut)
		rightOut, _ := finance.ParseAmount(view.ConvertedSpendingSummaryRows[right].MoneyOut)
		return leftOut != nil && rightOut != nil && leftOut.Cmp(rightOut) > 0
	})
	view.ConvertedMoneyOut = finance.FormatAmount(totalOut)
	view.ConvertedMoneyIn = finance.FormatAmount(totalIn)
	view.UnconvertedCurrencyCodes = sortedCurrencyCodes(groups.unconverted)
	return view, nil
}

// --- exchange rates -----------------------------------------------------

func (self *graph) ExchangeRate(ctx context.Context, arguments ExchangeRateArguments) (*models.CurrencyPairRate, error) {
	principal, _, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	fromCurrencyCode, toCurrencyCode, rateOn, err := currencyPairArguments(principal, arguments.FromCurrencyCode, arguments.ToCurrencyCode, arguments.RateOn)
	if err != nil {
		return nil, err
	}
	rate, err := self.lookUpExchangeRate(ctx, fromCurrencyCode, toCurrencyCode, rateOn)
	if err != nil {
		return nil, financeError(err)
	}
	return rate, nil
}

func (self *graph) ConvertCurrency(ctx context.Context, arguments ConvertCurrencyArguments) (*CurrencyConversionView, error) {
	principal, _, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireFinanceOffered(); err != nil {
		return nil, err
	}
	amount, err := amountArgument("amount", arguments.Amount)
	if err != nil {
		return nil, err
	}
	fromCurrencyCode, toCurrencyCode, rateOn, err := currencyPairArguments(principal, arguments.FromCurrencyCode, arguments.ToCurrencyCode, arguments.RateOn)
	if err != nil {
		return nil, err
	}
	rate, err := self.lookUpExchangeRate(ctx, fromCurrencyCode, toCurrencyCode, rateOn)
	if err != nil {
		return nil, financeError(err)
	}
	converted, err := finance.ConvertAmount(amount, rate.Rate)
	if err != nil {
		return nil, err
	}
	return &CurrencyConversionView{
		Amount: amount, FromCurrencyCode: fromCurrencyCode, ConvertedAmount: converted, ToCurrencyCode: toCurrencyCode,
		Rate: rate.Rate, RateOn: rate.RateOn, RateSource: rate.RateSource,
	}, nil
}

// currencyPairArguments reads two currency codes and a day, today in the
// caller's zone when left out.
func currencyPairArguments(principal *api.Principal, fromCurrencyCode, toCurrencyCode, rateOn string) (string, string, string, error) {
	from, err := currencyArgument("fromCurrencyCode", fromCurrencyCode, "")
	if err != nil {
		return "", "", "", err
	}
	to, err := currencyArgument("toCurrencyCode", toCurrencyCode, "")
	if err != nil {
		return "", "", "", err
	}
	if from == "" || to == "" {
		return "", "", "", fmt.Errorf("%w: both currencies are needed, as codes like USD", api.ErrInvalidArguments)
	}
	day, err := dayArgument("rateOn", rateOn, personToday(principal))
	return from, to, day, err
}

// lookUpExchangeRate is one day's rate between two currencies, fetching
// first when the stored rates are behind; the same currency twice is one.
func (self *graph) lookUpExchangeRate(ctx context.Context, fromCurrencyCode, toCurrencyCode, rateOn string) (*models.CurrencyPairRate, error) {
	if fromCurrencyCode == toCurrencyCode {
		return &models.CurrencyPairRate{
			FromCurrencyCode: fromCurrencyCode, ToCurrencyCode: toCurrencyCode, Rate: "1", RateOn: rateOn, RateSource: models.RateSourceECB,
		}, nil
	}
	_, rate, err := rates.NewConverter(ctx, self.exchangeRateFetcher(), self.transaction(ctx)).Convert("1", fromCurrencyCode, toCurrencyCode, rateOn)
	return rate, err
}
