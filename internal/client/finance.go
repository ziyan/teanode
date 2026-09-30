package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The finance area: finance sources, finance accounts and transactions,
// exchange rates, net worth, spending categories and rules, budgets and
// savings targets. teanode finance and the agent's finance tool send these
// same documents, so the two and the dashboard say the same numbers. None
// of them selects a provider's metadata or anything secret.

// FinanceProvider is one provider people may link through.
type FinanceProvider struct {
	ProviderKind      string `json:"providerKind"`
	IsBrowserRequired bool   `json:"isBrowserRequired"`
}

// FinanceLinkToken is what the linking page opens Plaid Link with.
type FinanceLinkToken struct {
	LinkToken string `json:"linkToken"`
	SourceID  string `json:"sourceId,omitempty"`
}

// FinanceAccount is one finance account and its balance.
type FinanceAccount struct {
	ID                      string     `json:"id"`
	SourceID                string     `json:"sourceId"`
	InstitutionName         string     `json:"institutionName,omitempty"`
	ProviderKind            string     `json:"providerKind"`
	AccountName             string     `json:"accountName"`
	AccountMask             string     `json:"accountMask,omitempty"`
	AccountKind             string     `json:"accountKind"`
	CurrencyCode            string     `json:"currencyCode"`
	CurrentBalance          string     `json:"currentBalance,omitempty"`
	AvailableBalance        string     `json:"availableBalance,omitempty"`
	BalanceAt               *time.Time `json:"balanceAt,omitempty"`
	IsSignInRequired        bool       `json:"isSignInRequired"`
	ReportingCurrencyCode   string     `json:"reportingCurrencyCode,omitempty"`
	ConvertedCurrentBalance string     `json:"convertedCurrentBalance,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	ModifiedAt              time.Time  `json:"modifiedAt"`
}

// FinanceSource is one login at one institution through one provider.
type FinanceSource struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	ProviderKind     string            `json:"providerKind"`
	InstitutionID    string            `json:"institutionId,omitempty"`
	InstitutionName  string            `json:"institutionName,omitempty"`
	IsEnabled        bool              `json:"isEnabled"`
	Cron             string            `json:"cron,omitempty"`
	LastRunAt        *time.Time        `json:"lastRunAt,omitempty"`
	NextRunAt        *time.Time        `json:"nextRunAt,omitempty"`
	LastError        string            `json:"lastError,omitempty"`
	IsSignInRequired bool              `json:"isSignInRequired"`
	FinanceAccounts  []*FinanceAccount `json:"financeAccounts"`
	CreatedAt        time.Time         `json:"createdAt"`
}

// FinanceTransaction is one transaction on a finance account. Description
// and MerchantName are written by whoever charged the account.
type FinanceTransaction struct {
	ID                       string     `json:"id"`
	FinanceAccountID         string     `json:"financeAccountId"`
	PostedOn                 string     `json:"postedOn"`
	TransactedAt             *time.Time `json:"transactedAt,omitempty"`
	Amount                   string     `json:"amount"`
	CurrencyCode             string     `json:"currencyCode"`
	Description              string     `json:"description"`
	MerchantName             string     `json:"merchantName,omitempty"`
	ProviderCategoryPrimary  string     `json:"providerCategoryPrimary,omitempty"`
	ProviderCategoryDetailed string     `json:"providerCategoryDetailed,omitempty"`
	IsPending                bool       `json:"isPending"`
	SpendingCategoryID       string     `json:"spendingCategoryId,omitempty"`
	CategorizedBy            string     `json:"categorizedBy,omitempty"`
	CategorizationConfidence string     `json:"categorizationConfidence,omitempty"`
	IsTransfer               bool       `json:"isTransfer"`
	TransferMarkedBy         string     `json:"transferMarkedBy,omitempty"`
}

// FinanceTransactionPage is one page of finance transactions and the cursor
// for the next, empty on the last.
type FinanceTransactionPage struct {
	FinanceTransactions []*FinanceTransaction `json:"financeTransactions"`
	NextCursor          string                `json:"nextCursor,omitempty"`
}

// FinanceSpendingSummaryRow is one group's money out and money in, in one
// currency.
type FinanceSpendingSummaryRow struct {
	GroupKey                string `json:"groupKey"`
	GroupLabel              string `json:"groupLabel"`
	CurrencyCode            string `json:"currencyCode"`
	MoneyOut                string `json:"moneyOut"`
	MoneyIn                 string `json:"moneyIn"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}

// FinanceCurrencyTotal is money out and money in in one currency.
type FinanceCurrencyTotal struct {
	CurrencyCode            string `json:"currencyCode"`
	MoneyOut                string `json:"moneyOut"`
	MoneyIn                 string `json:"moneyIn"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}

// FinanceConvertedSummaryRow is one group in the reporting currency.
type FinanceConvertedSummaryRow struct {
	GroupKey   string `json:"groupKey"`
	GroupLabel string `json:"groupLabel"`
	MoneyOut   string `json:"moneyOut"`
	MoneyIn    string `json:"moneyIn"`
}

// FinanceSpendingSummary is a spending summary per currency and converted.
type FinanceSpendingSummary struct {
	GroupBy                      string                        `json:"groupBy"`
	SpendingSummaryRows          []*FinanceSpendingSummaryRow  `json:"spendingSummaryRows"`
	CurrencyTotals               []*FinanceCurrencyTotal       `json:"currencyTotals"`
	ReportingCurrencyCode        string                        `json:"reportingCurrencyCode,omitempty"`
	ConvertedSpendingSummaryRows []*FinanceConvertedSummaryRow `json:"convertedSpendingSummaryRows"`
	ConvertedMoneyOut            string                        `json:"convertedMoneyOut,omitempty"`
	ConvertedMoneyIn             string                        `json:"convertedMoneyIn,omitempty"`
	UnconvertedCurrencyCodes     []string                      `json:"unconvertedCurrencyCodes"`
}

// CurrencyPairRate is what one unit of one currency bought in another on a
// published day.
type CurrencyPairRate struct {
	FromCurrencyCode string `json:"fromCurrencyCode"`
	ToCurrencyCode   string `json:"toCurrencyCode"`
	Rate             string `json:"rate"`
	RateOn           string `json:"rateOn"`
	RateSource       string `json:"rateSource"`
}

// ReportingCurrency is the currency totals are shown in, and whether the
// person chose it or it falls back to their first finance account's.
type ReportingCurrency struct {
	ReportingCurrencyCode string `json:"reportingCurrencyCode"`
	IsChosen              bool   `json:"isChosen"`
}

// CurrencyConversion is an amount converted, with its rate and day.
type CurrencyConversion struct {
	Amount           string `json:"amount"`
	FromCurrencyCode string `json:"fromCurrencyCode"`
	ConvertedAmount  string `json:"convertedAmount"`
	ToCurrencyCode   string `json:"toCurrencyCode"`
	Rate             string `json:"rate"`
	RateOn           string `json:"rateOn"`
	RateSource       string `json:"rateSource"`
}

// NetWorthPoint is one day's net worth in one currency.
type NetWorthPoint struct {
	NetWorthOn     string `json:"netWorthOn"`
	CurrencyCode   string `json:"currencyCode,omitempty"`
	NetWorthAmount string `json:"netWorthAmount"`
}

// NetWorth is net worth per day, per currency and converted.
type NetWorth struct {
	From                     string           `json:"from"`
	To                       string           `json:"to"`
	NetWorthPoints           []*NetWorthPoint `json:"netWorthPoints"`
	ReportingCurrencyCode    string           `json:"reportingCurrencyCode,omitempty"`
	ConvertedNetWorthPoints  []*NetWorthPoint `json:"convertedNetWorthPoints"`
	UnconvertedCurrencyCodes []string         `json:"unconvertedCurrencyCodes"`
}

// AssetValuation is one value of one asset on one day. ValuationNote and
// EvidenceURLs may come from web pages the agent read.
type AssetValuation struct {
	ID              string   `json:"id"`
	AssetID         string   `json:"assetId"`
	ValuedOn        string   `json:"valuedOn"`
	Value           string   `json:"value"`
	CurrencyCode    string   `json:"currencyCode"`
	ValuationSource string   `json:"valuationSource"`
	EstimateLow     string   `json:"estimateLow,omitempty"`
	EstimateHigh    string   `json:"estimateHigh,omitempty"`
	ValuationNote   string   `json:"valuationNote,omitempty"`
	EvidenceURLs    []string `json:"evidenceUrls"`
}

// Asset is anything that counts toward net worth, what is owed included.
type Asset struct {
	ID                  string          `json:"id"`
	AssetName           string          `json:"assetName"`
	AssetKind           string          `json:"assetKind"`
	IsLiability         bool            `json:"isLiability"`
	CurrencyCode        string          `json:"currencyCode"`
	FinanceAccountID    string          `json:"financeAccountId,omitempty"`
	ValuationSource     string          `json:"valuationSource"`
	EstimateDescription string          `json:"estimateDescription,omitempty"`
	IsEstimateAllowed   bool            `json:"isEstimateAllowed"`
	ClosedOn            string          `json:"closedOn,omitempty"`
	LatestValuation     *AssetValuation `json:"latestValuation,omitempty"`
}

// AssetHistory is one asset and its valuations, newest day first.
type AssetHistory struct {
	Asset           *Asset            `json:"asset"`
	AssetValuations []*AssetValuation `json:"assetValuations"`
}

// SpendingCategory is a label in the person's own list.
type SpendingCategory struct {
	ID                       string `json:"id"`
	SpendingCategoryName     string `json:"spendingCategoryName"`
	ParentSpendingCategoryID string `json:"parentSpendingCategoryId,omitempty"`
	IsIncome                 bool   `json:"isIncome"`
	IsHidden                 bool   `json:"isHidden"`
}

// SpendingRule assigns a spending category, or marks a transfer, to the
// finance transactions that match it.
type SpendingRule struct {
	ID                 string `json:"id"`
	MatchText          string `json:"matchText"`
	FinanceAccountID   string `json:"financeAccountId,omitempty"`
	MinimumAmount      string `json:"minimumAmount,omitempty"`
	MaximumAmount      string `json:"maximumAmount,omitempty"`
	SpendingCategoryID string `json:"spendingCategoryId,omitempty"`
	IsTransfer         bool   `json:"isTransfer"`
	RulePriority       int    `json:"rulePriority"`
}

// CategorizedTransaction is a finance transaction as categorized, and the
// spending rule made for its merchant, when one was asked for.
type CategorizedTransaction struct {
	FinanceTransaction *FinanceTransaction `json:"financeTransaction"`
	SpendingRule       *SpendingRule       `json:"spendingRule,omitempty"`
}

// Budget is an amount for one spending category for each month from a
// month on.
type Budget struct {
	ID                 string `json:"id"`
	SpendingCategoryID string `json:"spendingCategoryId"`
	MonthlyAmount      string `json:"monthlyAmount"`
	CurrencyCode       string `json:"currencyCode"`
	EffectiveFrom      string `json:"effectiveFrom"`
}

// CurrencyAmount is an amount in one currency.
type CurrencyAmount struct {
	CurrencyCode string `json:"currencyCode"`
	Amount       string `json:"amount"`
}

// SpendingCategoryBudgetStatus is one spending category against its
// budget, in the budget's currency.
type SpendingCategoryBudgetStatus struct {
	SpendingCategoryID               string            `json:"spendingCategoryId"`
	SpendingCategoryName             string            `json:"spendingCategoryName"`
	BudgetAmount                     string            `json:"budgetAmount"`
	CurrencyCode                     string            `json:"currencyCode"`
	SpendingAmount                   string            `json:"spendingAmount"`
	SpendingBySameDayLastMonthAmount string            `json:"spendingBySameDayLastMonthAmount"`
	FixedChargesDueAmount            string            `json:"fixedChargesDueAmount"`
	ProjectedAmount                  string            `json:"projectedAmount"`
	BudgetPace                       string            `json:"budgetPace"`
	UnconvertedSpending              []*CurrencyAmount `json:"unconvertedSpending"`
}

// BudgetStatus is every spending category with a budget in a month.
type BudgetStatus struct {
	Month              string                          `json:"month"`
	AsOf               string                          `json:"asOf"`
	DayOfMonth         int                             `json:"dayOfMonth"`
	DaysInMonth        int                             `json:"daysInMonth"`
	SpendingCategories []*SpendingCategoryBudgetStatus `json:"spendingCategories"`
}

// SpendingDay is one day's spending and the month's up to it.
type SpendingDay struct {
	SpentOn                  string `json:"spentOn"`
	SpendingAmount           string `json:"spendingAmount"`
	CumulativeSpendingAmount string `json:"cumulativeSpendingAmount"`
}

// SpendingByDay is cumulative spending per day of two months.
type SpendingByDay struct {
	Month                    string         `json:"month"`
	CompareMonth             string         `json:"compareMonth"`
	ReportingCurrencyCode    string         `json:"reportingCurrencyCode,omitempty"`
	MonthDays                []*SpendingDay `json:"monthDays"`
	CompareMonthDays         []*SpendingDay `json:"compareMonthDays"`
	UnconvertedCurrencyCodes []string       `json:"unconvertedCurrencyCodes"`
}

// CashFlowMonth is one month's income, spending and difference, in one
// currency (CurrencyCode empty when it is the reporting currency's).
type CashFlowMonth struct {
	CashFlowMonth  string `json:"cashFlowMonth"`
	CurrencyCode   string `json:"currencyCode,omitempty"`
	IncomeAmount   string `json:"incomeAmount"`
	SpendingAmount string `json:"spendingAmount"`
	NetAmount      string `json:"netAmount"`
}

// CashFlow is income and spending per month, per currency and converted.
type CashFlow struct {
	FromMonth                string           `json:"fromMonth"`
	ToMonth                  string           `json:"toMonth"`
	CurrencyCashFlowMonths   []*CashFlowMonth `json:"currencyCashFlowMonths"`
	ReportingCurrencyCode    string           `json:"reportingCurrencyCode,omitempty"`
	CashFlowMonths           []*CashFlowMonth `json:"cashFlowMonths"`
	UnconvertedCurrencyCodes []string         `json:"unconvertedCurrencyCodes"`
}

// SavingsTargetProgress is how a savings target stands, in its currency.
type SavingsTargetProgress struct {
	SavedAmount              string   `json:"savedAmount"`
	RemainingAmount          string   `json:"remainingAmount"`
	MonthsLeftCount          int      `json:"monthsLeftCount"`
	RequiredMonthlyAmount    string   `json:"requiredMonthlyAmount"`
	IsBehind                 bool     `json:"isBehind"`
	UnconvertedCurrencyCodes []string `json:"unconvertedCurrencyCodes"`
}

// SavingsTarget is an amount to save by a day.
type SavingsTarget struct {
	ID                string   `json:"id"`
	SavingsTargetName string   `json:"savingsTargetName"`
	TargetAmount      string   `json:"targetAmount"`
	CurrencyCode      string   `json:"currencyCode"`
	TargetOn          string   `json:"targetOn"`
	TargetMeasure     string   `json:"targetMeasure"`
	StartingAmount    string   `json:"startingAmount,omitempty"`
	StartedOn         string   `json:"startedOn"`
	ClosedOn          string   `json:"closedOn,omitempty"`
	AssetIDs          []string `json:"assetIds"`
}

// SavingsTargetStanding is a savings target and its progress.
type SavingsTargetStanding struct {
	SavingsTarget         *SavingsTarget         `json:"savingsTarget"`
	SavingsTargetProgress *SavingsTargetProgress `json:"savingsTargetProgress"`
}

// The fields each document selects, so a type is read the same way by
// every document that returns it.
const (
	financeAccountFields = `{ id sourceId institutionName providerKind accountName accountMask accountKind currencyCode currentBalance availableBalance balanceAt isSignInRequired reportingCurrencyCode convertedCurrentBalance createdAt modifiedAt }`

	financeSourceFields = `{ id name providerKind institutionId institutionName isEnabled cron lastRunAt nextRunAt lastError isSignInRequired createdAt financeAccounts ` + financeAccountFields + ` }`

	financeTransactionFields = `{ id financeAccountId postedOn transactedAt amount currencyCode description merchantName providerCategoryPrimary providerCategoryDetailed isPending spendingCategoryId categorizedBy categorizationConfidence isTransfer transferMarkedBy }`

	currencyPairRateFields = `{ fromCurrencyCode toCurrencyCode rate rateOn rateSource }`

	assetValuationFields = `{ id assetId valuedOn value currencyCode valuationSource estimateLow estimateHigh valuationNote evidenceUrls }`

	assetFields = `{ id assetName assetKind isLiability currencyCode financeAccountId valuationSource estimateDescription isEstimateAllowed closedOn latestValuation ` + assetValuationFields + ` }`

	spendingCategoryFields = `{ id spendingCategoryName parentSpendingCategoryId isIncome isHidden }`

	spendingRuleFields = `{ id matchText financeAccountId minimumAmount maximumAmount spendingCategoryId isTransfer rulePriority }`

	budgetFields = `{ id spendingCategoryId monthlyAmount currencyCode effectiveFrom }`

	savingsTargetStandingFields = `{ savingsTarget { id savingsTargetName targetAmount currencyCode targetOn targetMeasure startingAmount startedOn closedOn assetIds } savingsTargetProgress { savedAmount remainingAmount monthsLeftCount requiredMonthlyAmount isBehind unconvertedCurrencyCodes } }`
)

// One document per finance operation, named for it.
const (
	DocumentFinanceProviders = `query { FinanceProviders { providerKind isBrowserRequired } }`

	DocumentFinanceSources = `query { FinanceSources ` + financeSourceFields + ` }`

	DocumentFinanceAccounts = `query ($currencyCode: String) { FinanceAccounts(currencyCode: $currencyCode) ` + financeAccountFields + ` }`

	DocumentFinanceTransactions = `query ($from: String, $to: String, $financeAccountId: String, $text: String, $minimumAmount: String, $maximumAmount: String, $providerCategory: String, $spendingCategoryId: String, $isUncategorized: Boolean, $limit: Int, $after: String) {
  FinanceTransactions(from: $from, to: $to, financeAccountId: $financeAccountId, text: $text, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, providerCategory: $providerCategory, spendingCategoryId: $spendingCategoryId, isUncategorized: $isUncategorized, limit: $limit, after: $after) {
    financeTransactions ` + financeTransactionFields + ` nextCursor
  }
}`

	DocumentFinanceSpendingSummary = `query ($from: String, $to: String, $groupBy: String, $financeAccountId: String, $currencyCode: String) {
  FinanceSpendingSummary(from: $from, to: $to, groupBy: $groupBy, financeAccountId: $financeAccountId, currencyCode: $currencyCode) {
    groupBy
    spendingSummaryRows { groupKey groupLabel currencyCode moneyOut moneyIn financeTransactionCount }
    currencyTotals { currencyCode moneyOut moneyIn financeTransactionCount }
    reportingCurrencyCode
    convertedSpendingSummaryRows { groupKey groupLabel moneyOut moneyIn }
    convertedMoneyOut convertedMoneyIn unconvertedCurrencyCodes
  }
}`

	DocumentExchangeRate = `query ($fromCurrencyCode: String!, $toCurrencyCode: String!, $rateOn: String) {
  ExchangeRate(fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode, rateOn: $rateOn) ` + currencyPairRateFields + `
}`

	DocumentConvertCurrency = `query ($amount: String!, $fromCurrencyCode: String!, $toCurrencyCode: String!, $rateOn: String) {
  ConvertCurrency(amount: $amount, fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode, rateOn: $rateOn) { amount fromCurrencyCode convertedAmount toCurrencyCode rate rateOn rateSource }
}`

	DocumentNetWorth = `query ($from: String, $to: String, $currencyCode: String) {
  NetWorth(from: $from, to: $to, currencyCode: $currencyCode) {
    from to netWorthPoints { netWorthOn currencyCode netWorthAmount }
    reportingCurrencyCode convertedNetWorthPoints { netWorthOn netWorthAmount } unconvertedCurrencyCodes
  }
}`

	DocumentAssets = `query { Assets ` + assetFields + ` }`

	DocumentAssetHistory = `query ($assetId: String!) { AssetHistory(assetId: $assetId) { asset ` + assetFields + ` assetValuations ` + assetValuationFields + ` } }`

	DocumentSpendingCategories = `query { SpendingCategories ` + spendingCategoryFields + ` }`

	DocumentSpendingRules = `query { SpendingRules ` + spendingRuleFields + ` }`

	DocumentBudgets = `query { Budgets ` + budgetFields + ` }`

	DocumentBudgetStatus = `query ($month: String) {
  BudgetStatus(month: $month) {
    month asOf dayOfMonth daysInMonth
    spendingCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode spendingAmount spendingBySameDayLastMonthAmount fixedChargesDueAmount projectedAmount budgetPace unconvertedSpending { currencyCode amount } }
  }
}`

	DocumentSpendingByDay = `query ($month: String, $compareMonth: String, $currencyCode: String) {
  SpendingByDay(month: $month, compareMonth: $compareMonth, currencyCode: $currencyCode) {
    month compareMonth reportingCurrencyCode
    monthDays { spentOn spendingAmount cumulativeSpendingAmount }
    compareMonthDays { spentOn spendingAmount cumulativeSpendingAmount }
    unconvertedCurrencyCodes
  }
}`

	DocumentCashFlow = `query ($fromMonth: String, $toMonth: String, $currencyCode: String) {
  CashFlow(fromMonth: $fromMonth, toMonth: $toMonth, currencyCode: $currencyCode) {
    fromMonth toMonth
    currencyCashFlowMonths { cashFlowMonth currencyCode incomeAmount spendingAmount netAmount }
    reportingCurrencyCode
    cashFlowMonths { cashFlowMonth incomeAmount spendingAmount netAmount }
    unconvertedCurrencyCodes
  }
}`

	DocumentSavingsTargets = `query { SavingsTargets ` + savingsTargetStandingFields + ` }`

	DocumentReportingCurrency = `query { ReportingCurrency { reportingCurrencyCode isChosen } }`

	DocumentCreateFinanceLinkToken = `mutation ($sourceId: String) { CreateFinanceLinkToken(sourceId: $sourceId) { linkToken sourceId } }`

	DocumentCompleteFinanceLink = `mutation ($publicToken: String!, $institutionId: String, $institutionName: String) {
  CompleteFinanceLink(publicToken: $publicToken, institutionId: $institutionId, institutionName: $institutionName) ` + financeSourceFields + `
}`

	DocumentCompleteFinanceRepair = `mutation ($sourceId: String!) { CompleteFinanceRepair(sourceId: $sourceId) ` + financeSourceFields + ` }`

	DocumentLinkSimpleFIN = `mutation ($setupToken: String!) { LinkSimpleFIN(setupToken: $setupToken) ` + financeSourceFields + ` }`

	DocumentImportFinanceCredential = `mutation ($providerKind: String!, $credential: String!, $institutionName: String) {
  ImportFinanceCredential(providerKind: $providerKind, credential: $credential, institutionName: $institutionName) ` + financeSourceFields + `
}`

	DocumentSetReportingCurrency = `mutation ($currencyCode: String!) { SetReportingCurrency(currencyCode: $currencyCode) }`

	DocumentCreateAsset = `mutation ($assetName: String!, $assetKind: String!, $currencyCode: String!, $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean, $value: String, $valuedOn: String) {
  CreateAsset(assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode, valuationSource: $valuationSource, estimateDescription: $estimateDescription, isEstimateAllowed: $isEstimateAllowed, value: $value, valuedOn: $valuedOn) ` + assetFields + `
}`

	DocumentUpdateAsset = `mutation ($assetId: String!, $assetName: String, $assetKind: String, $currencyCode: String, $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean) {
  UpdateAsset(assetId: $assetId, assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode, valuationSource: $valuationSource, estimateDescription: $estimateDescription, isEstimateAllowed: $isEstimateAllowed) ` + assetFields + `
}`

	DocumentCloseAsset = `mutation ($assetId: String!, $closedOn: String, $shouldReopen: Boolean) {
  CloseAsset(assetId: $assetId, closedOn: $closedOn, shouldReopen: $shouldReopen) ` + assetFields + `
}`

	DocumentDeleteAsset = `mutation ($assetId: String!) { DeleteAsset(assetId: $assetId) }`

	DocumentRecordValuation = `mutation ($assetId: String!, $value: String!, $valuedOn: String, $valuationSource: String, $estimateLow: String, $estimateHigh: String, $valuationNote: String, $evidenceUrls: [String!]) {
  RecordValuation(assetId: $assetId, value: $value, valuedOn: $valuedOn, valuationSource: $valuationSource, estimateLow: $estimateLow, estimateHigh: $estimateHigh, valuationNote: $valuationNote, evidenceUrls: $evidenceUrls) ` + assetValuationFields + `
}`

	DocumentDeleteValuation = `mutation ($valuationId: String!) { DeleteValuation(valuationId: $valuationId) }`

	DocumentCreateSpendingCategory = `mutation ($spendingCategoryName: String!, $parentSpendingCategoryId: String, $isIncome: Boolean, $isHidden: Boolean) {
  CreateSpendingCategory(spendingCategoryName: $spendingCategoryName, parentSpendingCategoryId: $parentSpendingCategoryId, isIncome: $isIncome, isHidden: $isHidden) ` + spendingCategoryFields + `
}`

	DocumentUpdateSpendingCategory = `mutation ($spendingCategoryId: String!, $spendingCategoryName: String, $parentSpendingCategoryId: String, $isIncome: Boolean, $isHidden: Boolean) {
  UpdateSpendingCategory(spendingCategoryId: $spendingCategoryId, spendingCategoryName: $spendingCategoryName, parentSpendingCategoryId: $parentSpendingCategoryId, isIncome: $isIncome, isHidden: $isHidden) ` + spendingCategoryFields + `
}`

	DocumentDeleteSpendingCategory = `mutation ($spendingCategoryId: String!) { DeleteSpendingCategory(spendingCategoryId: $spendingCategoryId) }`

	DocumentCreateSpendingRule = `mutation ($matchText: String!, $spendingCategoryId: String, $isTransfer: Boolean, $financeAccountId: String, $minimumAmount: String, $maximumAmount: String, $rulePriority: Int) {
  CreateSpendingRule(matchText: $matchText, spendingCategoryId: $spendingCategoryId, isTransfer: $isTransfer, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, rulePriority: $rulePriority) ` + spendingRuleFields + `
}`

	DocumentUpdateSpendingRule = `mutation ($spendingRuleId: String!, $matchText: String, $spendingCategoryId: String, $isTransfer: Boolean, $financeAccountId: String, $minimumAmount: String, $maximumAmount: String, $rulePriority: Int) {
  UpdateSpendingRule(spendingRuleId: $spendingRuleId, matchText: $matchText, spendingCategoryId: $spendingCategoryId, isTransfer: $isTransfer, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, rulePriority: $rulePriority) ` + spendingRuleFields + `
}`

	DocumentDeleteSpendingRule = `mutation ($spendingRuleId: String!) { DeleteSpendingRule(spendingRuleId: $spendingRuleId) }`

	DocumentCategorizeTransaction = `mutation ($financeTransactionId: String!, $spendingCategoryId: String, $shouldCreateSpendingRule: Boolean) {
  CategorizeTransaction(financeTransactionId: $financeTransactionId, spendingCategoryId: $spendingCategoryId, shouldCreateSpendingRule: $shouldCreateSpendingRule) {
    financeTransaction ` + financeTransactionFields + ` spendingRule ` + spendingRuleFields + `
  }
}`

	DocumentMarkTransfer = `mutation ($financeTransactionId: String!, $isTransfer: Boolean!) {
  MarkTransfer(financeTransactionId: $financeTransactionId, isTransfer: $isTransfer) ` + financeTransactionFields + `
}`

	DocumentSetBudget = `mutation ($spendingCategoryId: String!, $monthlyAmount: String!, $currencyCode: String, $effectiveFrom: String) {
  SetBudget(spendingCategoryId: $spendingCategoryId, monthlyAmount: $monthlyAmount, currencyCode: $currencyCode, effectiveFrom: $effectiveFrom) ` + budgetFields + `
}`

	DocumentCreateSavingsTarget = `mutation ($savingsTargetName: String!, $targetAmount: String!, $currencyCode: String, $targetOn: String!, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!]) {
  CreateSavingsTarget(savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn, assetIds: $assetIds) ` + savingsTargetStandingFields + `
}`

	DocumentUpdateSavingsTarget = `mutation ($savingsTargetId: String!, $savingsTargetName: String, $targetAmount: String, $currencyCode: String, $targetOn: String, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!]) {
  UpdateSavingsTarget(savingsTargetId: $savingsTargetId, savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn, assetIds: $assetIds) ` + savingsTargetStandingFields + `
}`

	DocumentCloseSavingsTarget = `mutation ($savingsTargetId: String!, $closedOn: String, $shouldReopen: Boolean) {
  CloseSavingsTarget(savingsTargetId: $savingsTargetId, closedOn: $closedOn, shouldReopen: $shouldReopen) ` + savingsTargetStandingFields + `
}`
)

// FinanceDocuments is every finance document by the operation it runs, for
// the test that checks each against the schema.
var FinanceDocuments = map[string]string{
	"FinanceProviders": DocumentFinanceProviders, "FinanceSources": DocumentFinanceSources,
	"FinanceAccounts": DocumentFinanceAccounts, "FinanceTransactions": DocumentFinanceTransactions,
	"FinanceSpendingSummary": DocumentFinanceSpendingSummary, "ExchangeRate": DocumentExchangeRate,
	"ConvertCurrency": DocumentConvertCurrency, "NetWorth": DocumentNetWorth, "Assets": DocumentAssets,
	"AssetHistory": DocumentAssetHistory, "SpendingCategories": DocumentSpendingCategories,
	"SpendingRules": DocumentSpendingRules, "Budgets": DocumentBudgets, "BudgetStatus": DocumentBudgetStatus,
	"SpendingByDay": DocumentSpendingByDay, "CashFlow": DocumentCashFlow, "SavingsTargets": DocumentSavingsTargets,
	"ReportingCurrency":      DocumentReportingCurrency,
	"CreateFinanceLinkToken": DocumentCreateFinanceLinkToken, "CompleteFinanceLink": DocumentCompleteFinanceLink,
	"CompleteFinanceRepair": DocumentCompleteFinanceRepair, "LinkSimpleFIN": DocumentLinkSimpleFIN,
	"ImportFinanceCredential": DocumentImportFinanceCredential, "SetReportingCurrency": DocumentSetReportingCurrency,
	"CreateAsset": DocumentCreateAsset,
	"UpdateAsset": DocumentUpdateAsset, "CloseAsset": DocumentCloseAsset, "DeleteAsset": DocumentDeleteAsset,
	"RecordValuation": DocumentRecordValuation, "DeleteValuation": DocumentDeleteValuation,
	"CreateSpendingCategory": DocumentCreateSpendingCategory, "UpdateSpendingCategory": DocumentUpdateSpendingCategory,
	"DeleteSpendingCategory": DocumentDeleteSpendingCategory, "CreateSpendingRule": DocumentCreateSpendingRule,
	"UpdateSpendingRule": DocumentUpdateSpendingRule, "DeleteSpendingRule": DocumentDeleteSpendingRule,
	"CategorizeTransaction": DocumentCategorizeTransaction, "MarkTransfer": DocumentMarkTransfer,
	"SetBudget": DocumentSetBudget, "CreateSavingsTarget": DocumentCreateSavingsTarget,
	"UpdateSavingsTarget": DocumentUpdateSavingsTarget, "CloseSavingsTarget": DocumentCloseSavingsTarget,
}

// Executor runs a document: a Client, or the agent's operations as the
// person.
type Executor interface {
	Execute(ctx context.Context, query string, variables map[string]any, result any) error
}

// RunFinance runs one finance operation's document and decodes what the
// operation answered into result. Variables left out are sent as absent,
// which each optional argument reads as "not given".
func RunFinance(ctx context.Context, executor Executor, operation string, variables map[string]any, result any) error {
	document, isKnown := FinanceDocuments[operation]
	if !isKnown {
		return fmt.Errorf("client: there is no finance operation %q", operation)
	}
	var envelope map[string]json.RawMessage
	if err := executor.Execute(ctx, document, variables, &envelope); err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	answered, isAnswered := envelope[operation]
	if !isAnswered {
		return fmt.Errorf("client: the server did not answer %s", operation)
	}
	return json.Unmarshal(answered, result)
}
