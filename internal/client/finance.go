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
	CreditLimitAmount       string     `json:"creditLimitAmount,omitempty"`
	ReportingCurrencyCode   string     `json:"reportingCurrencyCode,omitempty"`
	ConvertedCurrentBalance string     `json:"convertedCurrentBalance,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	ModifiedAt              time.Time  `json:"modifiedAt"`
}

// CreditUsage is what is owed on the credit cards against their credit
// limits, overall in the reporting currency and per card.
type CreditUsage struct {
	ReportingCurrencyCode    string             `json:"reportingCurrencyCode,omitempty"`
	UnconvertedCurrencyCodes []string           `json:"unconvertedCurrencyCodes"`
	TotalOwedAmount          string             `json:"totalOwedAmount"`
	TotalCreditLimitAmount   string             `json:"totalCreditLimitAmount"`
	UsageShare               *float64           `json:"usageShare,omitempty"`
	LeftOutCardCount         int                `json:"leftOutCardCount"`
	LeftOutOwedAmount        string             `json:"leftOutOwedAmount"`
	CreditCards              []*CreditCardUsage `json:"creditCards"`
}

// CreditCardUsage is one credit card's owed amount against its credit
// limit, and where the limit comes from (provider, derived or unknown).
type CreditCardUsage struct {
	FinanceAccountID           string     `json:"financeAccountId"`
	AccountName                string     `json:"accountName"`
	AccountMask                string     `json:"accountMask,omitempty"`
	InstitutionName            string     `json:"institutionName,omitempty"`
	CurrencyCode               string     `json:"currencyCode"`
	OwedAmount                 string     `json:"owedAmount,omitempty"`
	CreditLimitAmount          string     `json:"creditLimitAmount,omitempty"`
	CreditLimitSource          string     `json:"creditLimitSource"`
	UsageShare                 *float64   `json:"usageShare,omitempty"`
	ConvertedOwedAmount        string     `json:"convertedOwedAmount,omitempty"`
	ConvertedCreditLimitAmount string     `json:"convertedCreditLimitAmount,omitempty"`
	BalanceAt                  *time.Time `json:"balanceAt,omitempty"`
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
// DuplicateOfTransactionID names the counted copy when it is a mirrored
// copy, left out of every total, and DuplicateDecidedBy what decided
// (mirror_detection, or person when the person counted it).
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
	DuplicateOfTransactionID string     `json:"duplicateOfTransactionId,omitempty"`
	DuplicateDecidedBy       string     `json:"duplicateDecidedBy,omitempty"`
	Annotation               string     `json:"annotation,omitempty"`
	AnnotatedBy              string     `json:"annotatedBy,omitempty"`
	ReceiptCount             int        `json:"receiptCount"`
}

// FinanceTransactionPage is one page of finance transactions, the cursor
// for the next, empty on the last, how many match on every page, and how
// many mirrored copies were left out because they were not asked for.
type FinanceTransactionPage struct {
	FinanceTransactions   []*FinanceTransaction `json:"financeTransactions"`
	NextCursor            string                `json:"nextCursor,omitempty"`
	TotalCount            int                   `json:"totalCount"`
	LeftOutDuplicateCount int                   `json:"leftOutDuplicateCount"`
}

// FinanceSecurity is something an investment account can hold, as the
// provider that reported it describes it.
type FinanceSecurity struct {
	ID           string `json:"id"`
	TickerSymbol string `json:"tickerSymbol,omitempty"`
	SecurityName string `json:"securityName"`
	SecurityKind string `json:"securityKind"`
	CurrencyCode string `json:"currencyCode,omitempty"`
	ClosePrice   string `json:"closePrice,omitempty"`
	ClosePriceOn string `json:"closePriceOn,omitempty"`
}

// FinanceTrade is one trade in an investment account: a buy, a sell, a
// cancelled trade or a security moved in or out. Description is written by
// the institution.
type FinanceTrade struct {
	ID                string           `json:"id"`
	FinanceAccountID  string           `json:"financeAccountId"`
	FinanceSecurityID string           `json:"financeSecurityId,omitempty"`
	FinanceSecurity   *FinanceSecurity `json:"financeSecurity,omitempty"`
	ProviderTradeID   string           `json:"providerTradeId"`
	TradedOn          string           `json:"tradedOn"`
	TradeKind         string           `json:"tradeKind"`
	TradeSubkind      string           `json:"tradeSubkind,omitempty"`
	TradedQuantity    string           `json:"tradedQuantity,omitempty"`
	UnitPrice         string           `json:"unitPrice,omitempty"`
	TradeAmount       string           `json:"tradeAmount"`
	FeeAmount         string           `json:"feeAmount,omitempty"`
	CurrencyCode      string           `json:"currencyCode"`
	Description       string           `json:"description"`
}

// FinanceTradePage is one page of trades and the cursor for the next,
// empty on the last.
type FinanceTradePage struct {
	FinanceTrades []*FinanceTrade `json:"financeTrades"`
	NextCursor    string          `json:"nextCursor,omitempty"`
	TotalCount    int             `json:"totalCount"`
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
	HeldQuantity    string   `json:"heldQuantity,omitempty"`
	UnitPrice       string   `json:"unitPrice,omitempty"`
	CostBasis       string   `json:"costBasis,omitempty"`
}

// Asset is anything that counts toward net worth, what is owed included.
type Asset struct {
	ID                  string           `json:"id"`
	AssetName           string           `json:"assetName"`
	AssetKind           string           `json:"assetKind"`
	IsLiability         bool             `json:"isLiability"`
	CurrencyCode        string           `json:"currencyCode"`
	FinanceAccountID    string           `json:"financeAccountId,omitempty"`
	FinanceSecurityID   string           `json:"financeSecurityId,omitempty"`
	FinanceSecurity     *FinanceSecurity `json:"financeSecurity,omitempty"`
	ValuationSource     string           `json:"valuationSource"`
	EstimateDescription string           `json:"estimateDescription,omitempty"`
	IsEstimateAllowed   bool             `json:"isEstimateAllowed"`
	ClosedOn            string           `json:"closedOn,omitempty"`
	LatestValuation     *AssetValuation  `json:"latestValuation,omitempty"`
}

// AssetHistory is one asset and its valuations, newest day first.
type AssetHistory struct {
	Asset           *Asset            `json:"asset"`
	AssetValuations []*AssetValuation `json:"assetValuations"`
}

// SpendingCategory is a label in the person's own list. IsTransfer marks
// the built-in transfer category: what is in it is neither spending nor
// income. IsOther marks the built-in other category: what fits no other
// spending category.
type SpendingCategory struct {
	ID                       string `json:"id"`
	SpendingCategoryName     string `json:"spendingCategoryName"`
	ParentSpendingCategoryID string `json:"parentSpendingCategoryId,omitempty"`
	IsIncome                 bool   `json:"isIncome"`
	IsHidden                 bool   `json:"isHidden"`
	IsTransfer               bool   `json:"isTransfer"`
	IsOther                  bool   `json:"isOther"`
}

// SpendingRule assigns a spending category to the finance transactions
// that match it; the transfer category marks them transfers.
type SpendingRule struct {
	ID                 string `json:"id"`
	MatchText          string `json:"matchText"`
	FinanceAccountID   string `json:"financeAccountId,omitempty"`
	MinimumAmount      string `json:"minimumAmount,omitempty"`
	MaximumAmount      string `json:"maximumAmount,omitempty"`
	SpendingCategoryID string `json:"spendingCategoryId"`
	RulePriority       int    `json:"rulePriority"`
}

// CategorizedTransaction is a finance transaction as categorized, and the
// spending rule made for its merchant, when one was asked for.
type CategorizedTransaction struct {
	FinanceTransaction *FinanceTransaction `json:"financeTransaction"`
	SpendingRule       *SpendingRule       `json:"spendingRule,omitempty"`
}

// CategorizedTransactions are finance transactions categorized together,
// and the spending rules saved for them, when they were asked for.
type CategorizedTransactions struct {
	FinanceTransactions []*FinanceTransaction `json:"financeTransactions"`
	SpendingRules       []*SpendingRule       `json:"spendingRules"`
}

// SpendingRuleProposals is what ProposeSpendingRules offers: the spending
// rules, and how many distinct match texts it left out and why.
type SpendingRuleProposals struct {
	SpendingRuleProposals        []*SpendingRuleProposal `json:"spendingRuleProposals"`
	TooGenericMatchTextCount     int                     `json:"tooGenericMatchTextCount"`
	ChangingNumberMatchTextCount int                     `json:"changingNumberMatchTextCount"`
	OverLimitMatchTextCount      int                     `json:"overLimitMatchTextCount"`
}

// SpendingRuleProposal is a spending rule CategorizeTransactions would
// save once confirmed: what it matches and assigns, how many of the
// finance transactions named it matches, how many others it would
// recategorize, and the spending rule it goes ahead of, if any.
type SpendingRuleProposal struct {
	MatchText               string        `json:"matchText"`
	SpendingCategoryID      string        `json:"spendingCategoryId"`
	FinanceTransactionCount int           `json:"financeTransactionCount"`
	ChangedTransactionCount int           `json:"changedTransactionCount"`
	AheadOfSpendingRule     *SpendingRule `json:"aheadOfSpendingRule,omitempty"`
}

// ConfirmedSpendingRule is a proposed spending rule as it is sent back to
// CategorizeTransactions to be saved.
type ConfirmedSpendingRule struct {
	MatchText             string `json:"matchText"`
	SpendingCategoryID    string `json:"spendingCategoryId"`
	AheadOfSpendingRuleID string `json:"aheadOfSpendingRuleId,omitempty"`
}

// Confirmed is the spending rules proposed, as CategorizeTransactions
// takes them to save exactly those.
func (self *SpendingRuleProposals) Confirmed() []ConfirmedSpendingRule {
	confirmed := make([]ConfirmedSpendingRule, 0, len(self.SpendingRuleProposals))
	for _, proposal := range self.SpendingRuleProposals {
		spendingRule := ConfirmedSpendingRule{MatchText: proposal.MatchText, SpendingCategoryID: proposal.SpendingCategoryID}
		if proposal.AheadOfSpendingRule != nil {
			spendingRule.AheadOfSpendingRuleID = proposal.AheadOfSpendingRule.ID
		}
		confirmed = append(confirmed, spendingRule)
	}
	return confirmed
}

// MaximumSpendingRuleProposalCount is the most spending rules one
// ProposeSpendingRules offers and one CategorizeTransactions saves.
const MaximumSpendingRuleProposalCount = 50

// LeftOutReasons says, a phrase each, how many match texts the proposal
// left out and why; none when it left out nothing.
func (self *SpendingRuleProposals) LeftOutReasons() []string {
	var reasons []string
	if self.TooGenericMatchTextCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d left out: too short or too generic to be a rule", self.TooGenericMatchTextCount))
	}
	switch {
	case self.ChangingNumberMatchTextCount == 1:
		reasons = append(reasons, "1 left out: it holds a number that changes each time")
	case self.ChangingNumberMatchTextCount > 1:
		reasons = append(reasons, fmt.Sprintf("%d left out: they hold a number that changes each time", self.ChangingNumberMatchTextCount))
	}
	if self.OverLimitMatchTextCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d more left out: at most %d spending rules at a time", self.OverLimitMatchTextCount, MaximumSpendingRuleProposalCount))
	}
	return reasons
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
	SpendingCategoryID               string                  `json:"spendingCategoryId"`
	SpendingCategoryName             string                  `json:"spendingCategoryName"`
	BudgetAmount                     string                  `json:"budgetAmount"`
	CurrencyCode                     string                  `json:"currencyCode"`
	BudgetToDateAmount               string                  `json:"budgetToDateAmount"`
	BudgetedMonthCount               int                     `json:"budgetedMonthCount"`
	FirstBudgetedMonth               string                  `json:"firstBudgetedMonth"`
	LastBudgetedMonth                string                  `json:"lastBudgetedMonth"`
	SpendingAmount                   string                  `json:"spendingAmount"`
	SpendingBySameDayLastMonthAmount string                  `json:"spendingBySameDayLastMonthAmount"`
	FixedChargesDueAmount            string                  `json:"fixedChargesDueAmount"`
	ExpectedRepeatCharges            []*ExpectedRepeatCharge `json:"expectedRepeatCharges"`
	ProjectedAmount                  string                  `json:"projectedAmount"`
	BudgetPace                       string                  `json:"budgetPace"`
	UnconvertedSpending              []*CurrencyAmount       `json:"unconvertedSpending"`
}

// ExpectedRepeatCharge is a merchant that charged a spending category in
// each of the last three full months, expected to charge it again this
// month and not seen yet.
type ExpectedRepeatCharge struct {
	MerchantName   string `json:"merchantName"`
	ExpectedAmount string `json:"expectedAmount"`
	CurrencyCode   string `json:"currencyCode"`
}

// IncomeCategoryBudgetStatus is one income spending category against the
// income expected of it, in the budget's currency.
type IncomeCategoryBudgetStatus struct {
	SpendingCategoryID             string            `json:"spendingCategoryId"`
	SpendingCategoryName           string            `json:"spendingCategoryName"`
	BudgetAmount                   string            `json:"budgetAmount"`
	CurrencyCode                   string            `json:"currencyCode"`
	BudgetedMonthCount             int               `json:"budgetedMonthCount"`
	FirstBudgetedMonth             string            `json:"firstBudgetedMonth"`
	LastBudgetedMonth              string            `json:"lastBudgetedMonth"`
	IncomeAmount                   string            `json:"incomeAmount"`
	IncomeBySameDayLastMonthAmount string            `json:"incomeBySameDayLastMonthAmount"`
	ExpectedByTodayAmount          string            `json:"expectedByTodayAmount"`
	ProjectedAmount                string            `json:"projectedAmount"`
	IncomePace                     string            `json:"incomePace"`
	UnconvertedIncome              []*CurrencyAmount `json:"unconvertedIncome"`
}

// BudgetStatus is every spending category with a budget in a month, or in
// any month of a year: the spending budgets, and the income budgets apart.
type BudgetStatus struct {
	Month              string                          `json:"month"`
	AsOf               string                          `json:"asOf"`
	DayOfMonth         int                             `json:"dayOfMonth"`
	DaysInMonth        int                             `json:"daysInMonth"`
	Year               string                          `json:"year"`
	MonthsElapsedCount int                             `json:"monthsElapsedCount"`
	DayOfYear          int                             `json:"dayOfYear"`
	DaysInYear         int                             `json:"daysInYear"`
	SpendingCategories []*SpendingCategoryBudgetStatus `json:"spendingCategories"`
	IncomeCategories   []*IncomeCategoryBudgetStatus   `json:"incomeCategories"`
}

// SavingSummary is a month's or a year's saving in the reporting
// currency: what its budgets expect, what it is so far, and where it is
// heading.
type SavingSummary struct {
	Month                      string   `json:"month"`
	AsOf                       string   `json:"asOf"`
	DayOfMonth                 int      `json:"dayOfMonth"`
	DaysInMonth                int      `json:"daysInMonth"`
	Year                       string   `json:"year"`
	MonthsElapsedCount         int      `json:"monthsElapsedCount"`
	DayOfYear                  int      `json:"dayOfYear"`
	DaysInYear                 int      `json:"daysInYear"`
	BudgetedMonths             []string `json:"budgetedMonths"`
	BudgetedMonthCount         int      `json:"budgetedMonthCount"`
	BudgetedMonthsElapsedCount int      `json:"budgetedMonthsElapsedCount"`
	ReportingCurrencyCode      string   `json:"reportingCurrencyCode"`
	IncomeBudgetCount          int      `json:"incomeBudgetCount"`
	SpendingBudgetCount        int      `json:"spendingBudgetCount"`
	ExpectedIncomeAmount       string   `json:"expectedIncomeAmount"`
	ExpectedSpendingAmount     string   `json:"expectedSpendingAmount"`
	ExpectedSavingAmount       string   `json:"expectedSavingAmount"`
	IncomeAmount               string   `json:"incomeAmount"`
	SpendingAmount             string   `json:"spendingAmount"`
	SavingAmount               string   `json:"savingAmount"`
	ProjectedIncomeAmount      string   `json:"projectedIncomeAmount"`
	ProjectedSpendingAmount    string   `json:"projectedSpendingAmount"`
	ProjectedSavingAmount      string   `json:"projectedSavingAmount"`
	SavingDifferenceAmount     string   `json:"savingDifferenceAmount"`
	SavingPace                 string   `json:"savingPace"`
	UnconvertedCurrencyCodes   []string `json:"unconvertedCurrencyCodes"`
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
	FinanceAccountIDs []string `json:"financeAccountIds"`
}

// SavingsTargetStanding is a savings target and its progress.
type SavingsTargetStanding struct {
	SavingsTarget         *SavingsTarget         `json:"savingsTarget"`
	SavingsTargetProgress *SavingsTargetProgress `json:"savingsTargetProgress"`
}

// FinanceStatementImport is what one import of statement files did.
type FinanceStatementImport struct {
	ImportedAt                   time.Time `json:"importedAt"`
	StatementImportOrigin        string    `json:"statementImportOrigin"`
	StatementFileNames           []string  `json:"statementFileNames"`
	AddedTransactionCount        int       `json:"addedTransactionCount"`
	UpdatedTransactionCount      int       `json:"updatedTransactionCount"`
	UnchangedTransactionCount    int       `json:"unchangedTransactionCount"`
	SkippedTransactionCount      int       `json:"skippedTransactionCount"`
	TransactionWithoutFITIDCount int       `json:"transactionWithoutFitIdCount"`
	FinanceAccountIDs            []string  `json:"financeAccountIds"`
	FinanceAccountNames          []string  `json:"financeAccountNames"`
	ImportErrorMessage           string    `json:"importErrorMessage,omitempty"`
}

// TransactionRowsPreview is what an import of transaction rows would do:
// the account (FinanceAccountID empty for a new one), the rows it would
// add and the rows it holds already, the days and money of the new rows,
// and what was checked.
type TransactionRowsPreview struct {
	FinanceAccountID       string                   `json:"financeAccountId,omitempty"`
	AccountName            string                   `json:"accountName"`
	IsNewAccount           bool                     `json:"isNewAccount"`
	AccountMatch           string                   `json:"accountMatch"`
	CurrencyCode           string                   `json:"currencyCode"`
	NewTransactionRows     []*TransactionRowPreview `json:"newTransactionRows"`
	PresentTransactionRows []*TransactionRowPreview `json:"presentTransactionRows"`
	FirstPostedOn          string                   `json:"firstPostedOn,omitempty"`
	LastPostedOn           string                   `json:"lastPostedOn,omitempty"`
	MoneyInAmount          string                   `json:"moneyInAmount"`
	MoneyOutAmount         string                   `json:"moneyOutAmount"`
	VerificationSummary    string                   `json:"verificationSummary"`
	LedgerBalanceAmount    string                   `json:"ledgerBalanceAmount,omitempty"`
	LedgerBalanceOn        string                   `json:"ledgerBalanceOn,omitempty"`
	LedgerBalanceTimeZone  string                   `json:"ledgerBalanceTimeZone,omitempty"`
}

// TransactionRowPreview is one row of a preview, by its number among the
// rows as sent.
type TransactionRowPreview struct {
	RowNumber                  int    `json:"rowNumber"`
	PostedOn                   string `json:"postedOn"`
	Description                string `json:"description"`
	Amount                     string `json:"amount"`
	HasNearbyStoredTransaction bool   `json:"hasNearbyStoredTransaction"`
}

// StatementAccountDeleted is what deleting an account of imported
// statements removed.
type StatementAccountDeleted struct {
	FinanceAccountID        string `json:"financeAccountId"`
	DeletedTransactionCount int    `json:"deletedTransactionCount"`
	DeletedAssetCount       int    `json:"deletedAssetCount"`
}

// StatementImport is the address to mail statements to, whether importing
// is on, and what the last import did.
type StatementImport struct {
	SourceID              string                  `json:"sourceId"`
	ImportAddress         string                  `json:"importAddress,omitempty"`
	IsEnabled             bool                    `json:"isEnabled"`
	MaximumStatementBytes int                     `json:"maximumStatementBytes"`
	LastStatementImport   *FinanceStatementImport `json:"lastStatementImport,omitempty"`
}

// FinanceReceiptPage is one page of receipts, the cursor for the next,
// empty on the last, and how many match on every page.
type FinanceReceiptPage struct {
	FinanceReceipts []*FinanceReceipt `json:"financeReceipts"`
	NextCursor      string            `json:"nextCursor,omitempty"`
	TotalCount      int               `json:"totalCount"`
}

// FinanceReceipt is one merchant's record of one purchase, stored line by
// line as printed, checked against its totals and matched to the charges
// it explains. MailboxItemID is where its message is now, for opening it.
type FinanceReceipt struct {
	ID                    string                 `json:"id"`
	ReceiptSourceKind     string                 `json:"receiptSourceKind"`
	MailID                string                 `json:"mailId,omitempty"`
	MailboxItemID         string                 `json:"mailboxItemId,omitempty"`
	GmailMessageID        string                 `json:"gmailMessageId,omitempty"`
	AgentAttachmentID     string                 `json:"agentAttachmentId,omitempty"`
	MerchantName          string                 `json:"merchantName"`
	MerchantReceiptNumber string                 `json:"merchantReceiptNumber,omitempty"`
	PurchasedOn           string                 `json:"purchasedOn,omitempty"`
	PurchasedAt           *time.Time             `json:"purchasedAt,omitempty"`
	CurrencyCode          string                 `json:"currencyCode"`
	SubtotalAmount        string                 `json:"subtotalAmount,omitempty"`
	TotalAmount           string                 `json:"totalAmount"`
	PaymentAccountMask    string                 `json:"paymentAccountMask,omitempty"`
	ReceiptCheckState     string                 `json:"receiptCheckState"`
	CheckDifferenceAmount string                 `json:"checkDifferenceAmount"`
	IsFeeAfterSubtotal    bool                   `json:"isFeeAfterSubtotal"`
	ReceiptLines          []*FinanceReceiptLine  `json:"receiptLines"`
	ReceiptMatches        []*FinanceReceiptMatch `json:"receiptMatches"`
	CreatedAt             time.Time              `json:"createdAt"`
	ModifiedAt            time.Time              `json:"modifiedAt"`
}

// FinanceReceiptLine is one printed line of a receipt, its amount signed
// as printed.
type FinanceReceiptLine struct {
	ID                   string `json:"id"`
	LineNumber           int    `json:"lineNumber"`
	ReceiptLineKind      string `json:"receiptLineKind"`
	Description          string `json:"description"`
	Quantity             string `json:"quantity,omitempty"`
	QuantityUnit         string `json:"quantityUnit,omitempty"`
	UnitPriceAmount      string `json:"unitPriceAmount,omitempty"`
	LineAmount           string `json:"lineAmount"`
	TaxClassCode         string `json:"taxClassCode,omitempty"`
	DiscountedLineNumber int    `json:"discountedLineNumber,omitempty"`
	DiscountedLineID     string `json:"discountedLineId,omitempty"`
}

// FinanceReceiptMatch is how much of a finance transaction a receipt
// explains, and what matched them (receipt_matcher or person).
type FinanceReceiptMatch struct {
	ReceiptID            string    `json:"receiptId"`
	FinanceTransactionID string    `json:"financeTransactionId"`
	MatchedAmount        string    `json:"matchedAmount"`
	ReceiptMatchSource   string    `json:"receiptMatchSource"`
	MatchConfidence      string    `json:"matchConfidence,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
}

// ReceiptMatchCandidate is a charge a receipt could explain, with the
// finance transaction itself.
type ReceiptMatchCandidate struct {
	FinanceTransactionID string              `json:"financeTransactionId"`
	FinanceTransaction   *FinanceTransaction `json:"financeTransaction,omitempty"`
	MatchedAmount        string              `json:"matchedAmount"`
	IsExactAmount        bool                `json:"isExactAmount"`
	IsSameAccount        bool                `json:"isSameAccount"`
	IsMerchantNameShared bool                `json:"isMerchantNameShared"`
	DayDistanceCount     int                 `json:"dayDistanceCount"`
	IsAutomatic          bool                `json:"isAutomatic"`
	MatchConfidence      string              `json:"matchConfidence,omitempty"`
}

// ReceiptReading is the receipt job queued to read a receipt.
type ReceiptReading struct {
	AgentJobID string `json:"agentJobId"`
}

// RecordedReceipt is what recording a receipt did.
type RecordedReceipt struct {
	FinanceReceipt         *FinanceReceipt          `json:"financeReceipt"`
	ReceiptCheckSummary    string                   `json:"receiptCheckSummary"`
	ReceiptMatchCandidates []*ReceiptMatchCandidate `json:"receiptMatchCandidates"`
	IsReplaced             bool                     `json:"isReplaced"`
}

// ReceiptPreview is what recording a receipt would do.
type ReceiptPreview struct {
	ReceiptCheckState      string                   `json:"receiptCheckState"`
	CheckDifferenceAmount  string                   `json:"checkDifferenceAmount"`
	ReceiptCheckSummary    string                   `json:"receiptCheckSummary"`
	ReceiptMatchCandidates []*ReceiptMatchCandidate `json:"receiptMatchCandidates"`
	IsReplacing            bool                     `json:"isReplacing"`
}

// The fields each document selects, so a type is read the same way by
// every document that returns it.
const (
	financeAccountFields = `{ id sourceId institutionName providerKind accountName accountMask accountKind currencyCode currentBalance availableBalance balanceAt isSignInRequired creditLimitAmount reportingCurrencyCode convertedCurrentBalance createdAt modifiedAt }`

	financeSourceFields = `{ id name providerKind institutionId institutionName isEnabled cron lastRunAt nextRunAt lastError isSignInRequired createdAt financeAccounts ` + financeAccountFields + ` }`

	financeTransactionFields = `{ id financeAccountId postedOn transactedAt amount currencyCode description merchantName providerCategoryPrimary providerCategoryDetailed isPending spendingCategoryId categorizedBy categorizationConfidence duplicateOfTransactionId duplicateDecidedBy annotation annotatedBy receiptCount }`

	financeReceiptFields = `{ id receiptSourceKind mailId mailboxItemId gmailMessageId agentAttachmentId merchantName merchantReceiptNumber purchasedOn purchasedAt currencyCode subtotalAmount totalAmount paymentAccountMask receiptCheckState checkDifferenceAmount isFeeAfterSubtotal createdAt modifiedAt
    receiptLines { id lineNumber receiptLineKind description quantity quantityUnit unitPriceAmount lineAmount taxClassCode discountedLineNumber discountedLineId }
    receiptMatches { receiptId financeTransactionId matchedAmount receiptMatchSource matchConfidence createdAt } }`

	receiptMatchCandidateFields = `{ financeTransactionId financeTransaction ` + financeTransactionFields + ` matchedAmount isExactAmount isSameAccount isMerchantNameShared dayDistanceCount isAutomatic matchConfidence }`

	// The variables and arguments RecordReceipt and its preview share.
	receiptVariables = `($mailboxItemId: String, $gmailMessageId: String, $agentAttachmentId: String, $merchantName: String!, $merchantReceiptNumber: String, $purchasedOn: String, $purchasedAt: String, $currencyCode: String!, $subtotalAmount: String, $totalAmount: String!, $paymentAccountMask: String, $receiptLines: [ReceiptLineInput!]!, $financeTransactionId: String, $isUnbalancedAccepted: Boolean)`

	receiptArguments = `(mailboxItemId: $mailboxItemId, gmailMessageId: $gmailMessageId, agentAttachmentId: $agentAttachmentId, merchantName: $merchantName, merchantReceiptNumber: $merchantReceiptNumber, purchasedOn: $purchasedOn, purchasedAt: $purchasedAt, currencyCode: $currencyCode, subtotalAmount: $subtotalAmount, totalAmount: $totalAmount, paymentAccountMask: $paymentAccountMask, receiptLines: $receiptLines, financeTransactionId: $financeTransactionId, isUnbalancedAccepted: $isUnbalancedAccepted)`

	currencyPairRateFields = `{ fromCurrencyCode toCurrencyCode rate rateOn rateSource }`

	assetValuationFields = `{ id assetId valuedOn value currencyCode valuationSource estimateLow estimateHigh valuationNote evidenceUrls heldQuantity unitPrice costBasis }`

	financeSecurityFields = `{ id tickerSymbol securityName securityKind currencyCode closePrice closePriceOn }`

	assetFields = `{ id assetName assetKind isLiability currencyCode financeAccountId financeSecurityId financeSecurity ` + financeSecurityFields + ` valuationSource estimateDescription isEstimateAllowed closedOn latestValuation ` + assetValuationFields + ` }`

	financeTradeFields = `{ id financeAccountId financeSecurityId financeSecurity ` + financeSecurityFields + ` providerTradeId tradedOn tradeKind tradeSubkind tradedQuantity unitPrice tradeAmount feeAmount currencyCode description }`

	spendingCategoryFields = `{ id spendingCategoryName parentSpendingCategoryId isIncome isHidden isTransfer isOther }`

	spendingRuleFields = `{ id matchText financeAccountId minimumAmount maximumAmount spendingCategoryId rulePriority }`

	budgetFields = `{ id spendingCategoryId monthlyAmount currencyCode effectiveFrom }`

	financeStatementImportFields = `{ importedAt statementImportOrigin statementFileNames addedTransactionCount updatedTransactionCount unchangedTransactionCount skippedTransactionCount transactionWithoutFitIdCount financeAccountIds financeAccountNames importErrorMessage }`

	statementImportFields = `{ sourceId importAddress isEnabled maximumStatementBytes lastStatementImport ` + financeStatementImportFields + ` }`

	transactionRowPreviewFields = `{ rowNumber postedOn description amount hasNearbyStoredTransaction }`

	transactionRowsPreviewFields = `{ financeAccountId accountName isNewAccount accountMatch currencyCode newTransactionRows ` + transactionRowPreviewFields +
		` presentTransactionRows ` + transactionRowPreviewFields + ` firstPostedOn lastPostedOn moneyInAmount moneyOutAmount verificationSummary ledgerBalanceAmount ledgerBalanceOn ledgerBalanceTimeZone }`

	// The variables and arguments ImportTransactions and its preview
	// share.
	transactionRowsVariables = `($financeAccountId: String, $isNewAccount: Boolean, $institutionName: String!, $accountName: String, $accountNumber: String, $isAccountNumberPartial: Boolean, $statementAccountKind: String!, $currencyCode: String!, $bankCode: String, $transactionRows: [TransactionRowInput!]!, $ledgerBalanceAmount: String, $ledgerBalanceOn: String, $ledgerBalanceTimeZone: String, $monthlyTotals: [MonthlyTotalInput!])`

	transactionRowsArguments = `(financeAccountId: $financeAccountId, isNewAccount: $isNewAccount, institutionName: $institutionName, accountName: $accountName, accountNumber: $accountNumber, isAccountNumberPartial: $isAccountNumberPartial, statementAccountKind: $statementAccountKind, currencyCode: $currencyCode, bankCode: $bankCode, transactionRows: $transactionRows, ledgerBalanceAmount: $ledgerBalanceAmount, ledgerBalanceOn: $ledgerBalanceOn, ledgerBalanceTimeZone: $ledgerBalanceTimeZone, monthlyTotals: $monthlyTotals)`

	savingsTargetStandingFields = `{ savingsTarget { id savingsTargetName targetAmount currencyCode targetOn targetMeasure startingAmount startedOn closedOn assetIds financeAccountIds } savingsTargetProgress { savedAmount remainingAmount monthsLeftCount requiredMonthlyAmount isBehind unconvertedCurrencyCodes } }`
)

// One document per finance operation, named for it.
const (
	DocumentFinanceProviders = `query { FinanceProviders { providerKind isBrowserRequired } }`

	DocumentFinanceSources = `query { FinanceSources ` + financeSourceFields + ` }`

	DocumentFinanceAccounts = `query ($currencyCode: String) { FinanceAccounts(currencyCode: $currencyCode) ` + financeAccountFields + ` }`

	DocumentCreditUsage = `query ($currencyCode: String) {
  CreditUsage(currencyCode: $currencyCode) {
    reportingCurrencyCode unconvertedCurrencyCodes totalOwedAmount totalCreditLimitAmount usageShare leftOutCardCount leftOutOwedAmount
    creditCards { financeAccountId accountName accountMask institutionName currencyCode owedAmount creditLimitAmount creditLimitSource usageShare convertedOwedAmount convertedCreditLimitAmount balanceAt }
  }
}`

	DocumentFinanceTransactions = `query ($from: String, $to: String, $financeAccountId: String, $text: String, $minimumAmount: String, $maximumAmount: String, $providerCategory: String, $spendingCategoryId: String, $isUncategorized: Boolean, $duplicateOfTransactionId: String, $isDuplicateIncluded: Boolean, $limit: Int, $after: String, $offset: Int) {
  FinanceTransactions(from: $from, to: $to, financeAccountId: $financeAccountId, text: $text, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, providerCategory: $providerCategory, spendingCategoryId: $spendingCategoryId, isUncategorized: $isUncategorized, duplicateOfTransactionId: $duplicateOfTransactionId, isDuplicateIncluded: $isDuplicateIncluded, limit: $limit, after: $after, offset: $offset) {
    financeTransactions ` + financeTransactionFields + ` nextCursor totalCount leftOutDuplicateCount
  }
}`

	DocumentFinanceTrades = `query ($from: String, $to: String, $financeAccountId: String, $financeSecurityId: String, $limit: Int, $after: String, $offset: Int) {
  FinanceTrades(from: $from, to: $to, financeAccountId: $financeAccountId, financeSecurityId: $financeSecurityId, limit: $limit, after: $after, offset: $offset) {
    financeTrades ` + financeTradeFields + ` nextCursor totalCount
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

	DocumentAssets = `query ($assetKind: String, $text: String, $financeAccountId: String, $isHolding: Boolean) {
  Assets(assetKind: $assetKind, text: $text, financeAccountId: $financeAccountId, isHolding: $isHolding) ` + assetFields + `
}`

	DocumentAssetHistory = `query ($assetId: String!) { AssetHistory(assetId: $assetId) { asset ` + assetFields + ` assetValuations ` + assetValuationFields + ` } }`

	DocumentSpendingCategories = `query { SpendingCategories ` + spendingCategoryFields + ` }`

	DocumentSpendingRules = `query { SpendingRules ` + spendingRuleFields + ` }`

	DocumentProposeSpendingRules = `query ($financeTransactionIds: [String!]!, $spendingCategoryId: String!) {
  ProposeSpendingRules(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId) {
    spendingRuleProposals { matchText spendingCategoryId financeTransactionCount changedTransactionCount aheadOfSpendingRule ` + spendingRuleFields + ` }
    tooGenericMatchTextCount changingNumberMatchTextCount overLimitMatchTextCount
  }
}`

	DocumentBudgets = `query { Budgets ` + budgetFields + ` }`

	DocumentBudgetStatus = `query ($month: String, $year: String) {
  BudgetStatus(month: $month, year: $year) {
    month asOf dayOfMonth daysInMonth year monthsElapsedCount dayOfYear daysInYear
    spendingCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode budgetToDateAmount budgetedMonthCount firstBudgetedMonth lastBudgetedMonth spendingAmount spendingBySameDayLastMonthAmount fixedChargesDueAmount expectedRepeatCharges { merchantName expectedAmount currencyCode } projectedAmount budgetPace unconvertedSpending { currencyCode amount } }
    incomeCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode budgetedMonthCount firstBudgetedMonth lastBudgetedMonth incomeAmount incomeBySameDayLastMonthAmount expectedByTodayAmount projectedAmount incomePace unconvertedIncome { currencyCode amount } }
  }
}`

	DocumentSavingSummary = `query ($month: String, $year: String, $currencyCode: String) {
  SavingSummary(month: $month, year: $year, currencyCode: $currencyCode) {
    month asOf dayOfMonth daysInMonth year monthsElapsedCount dayOfYear daysInYear budgetedMonths budgetedMonthCount budgetedMonthsElapsedCount
    reportingCurrencyCode incomeBudgetCount spendingBudgetCount
    expectedIncomeAmount expectedSpendingAmount expectedSavingAmount incomeAmount spendingAmount savingAmount
    projectedIncomeAmount projectedSpendingAmount projectedSavingAmount savingDifferenceAmount savingPace unconvertedCurrencyCodes
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

	DocumentStatementImport = `query { StatementImport ` + statementImportFields + ` }`

	DocumentImportStatement = `mutation ($agentAttachmentId: String, $mailboxItemId: String) {
  ImportStatement(agentAttachmentId: $agentAttachmentId, mailboxItemId: $mailboxItemId) ` + financeStatementImportFields + `
}`

	DocumentRegenerateStatementImportAddress = `mutation { RegenerateStatementImportAddress ` + statementImportFields + ` }`

	DocumentImportTransactions = `mutation ` + transactionRowsVariables + ` {
  ImportTransactions` + transactionRowsArguments + ` ` + financeStatementImportFields + `
}`

	DocumentPreviewImportTransactions = `query ` + transactionRowsVariables + ` {
  PreviewImportTransactions` + transactionRowsArguments + ` ` + transactionRowsPreviewFields + `
}`

	DocumentRenameStatementAccount = `mutation ($financeAccountId: String!, $accountName: String, $accountMask: String) {
  RenameStatementAccount(financeAccountId: $financeAccountId, accountName: $accountName, accountMask: $accountMask) ` + financeAccountFields + `
}`

	DocumentDeleteStatementAccount = `mutation ($financeAccountId: String!) {
  DeleteStatementAccount(financeAccountId: $financeAccountId) { financeAccountId deletedTransactionCount deletedAssetCount }
}`

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

	DocumentCreateSpendingRule = `mutation ($matchText: String!, $spendingCategoryId: String!, $financeAccountId: String, $minimumAmount: String, $maximumAmount: String, $rulePriority: Int) {
  CreateSpendingRule(matchText: $matchText, spendingCategoryId: $spendingCategoryId, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, rulePriority: $rulePriority) ` + spendingRuleFields + `
}`

	DocumentUpdateSpendingRule = `mutation ($spendingRuleId: String!, $matchText: String, $spendingCategoryId: String, $financeAccountId: String, $minimumAmount: String, $maximumAmount: String, $rulePriority: Int) {
  UpdateSpendingRule(spendingRuleId: $spendingRuleId, matchText: $matchText, spendingCategoryId: $spendingCategoryId, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, rulePriority: $rulePriority) ` + spendingRuleFields + `
}`

	DocumentDeleteSpendingRule = `mutation ($spendingRuleId: String!) { DeleteSpendingRule(spendingRuleId: $spendingRuleId) }`

	DocumentCategorizeTransaction = `mutation ($financeTransactionId: String!, $spendingCategoryId: String, $shouldCreateSpendingRule: Boolean) {
  CategorizeTransaction(financeTransactionId: $financeTransactionId, spendingCategoryId: $spendingCategoryId, shouldCreateSpendingRule: $shouldCreateSpendingRule) {
    financeTransaction ` + financeTransactionFields + ` spendingRule ` + spendingRuleFields + `
  }
}`

	DocumentCategorizeTransactions = `mutation ($financeTransactionIds: [String!]!, $spendingCategoryId: String, $spendingRules: [ConfirmedSpendingRuleInput!]) {
  CategorizeTransactions(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId, spendingRules: $spendingRules) {
    financeTransactions ` + financeTransactionFields + ` spendingRules ` + spendingRuleFields + `
  }
}`

	DocumentCountTransaction = `mutation ($financeTransactionId: String!) {
  CountTransaction(financeTransactionId: $financeTransactionId) ` + financeTransactionFields + `
}`

	DocumentUndoCountTransaction = `mutation ($financeTransactionId: String!) {
  UndoCountTransaction(financeTransactionId: $financeTransactionId) ` + financeTransactionFields + `
}`

	DocumentAnnotateTransaction = `mutation ($financeTransactionId: String!, $annotation: String, $isAskedByPerson: Boolean) {
  AnnotateTransaction(financeTransactionId: $financeTransactionId, annotation: $annotation, isAskedByPerson: $isAskedByPerson) ` + financeTransactionFields + `
}`

	DocumentFinanceReceipts = `query ($financeTransactionId: String, $from: String, $to: String, $isUndated: Boolean, $isUnmatched: Boolean, $limit: Int, $after: String, $offset: Int) {
  FinanceReceipts(financeTransactionId: $financeTransactionId, from: $from, to: $to, isUndated: $isUndated, isUnmatched: $isUnmatched, limit: $limit, after: $after, offset: $offset) {
    financeReceipts ` + financeReceiptFields + ` nextCursor totalCount
  }
}`

	DocumentFinanceReceipt = `query ($receiptId: String!) { FinanceReceipt(receiptId: $receiptId) ` + financeReceiptFields + ` }`

	DocumentProposeReceiptMatches = `query ($receiptId: String!) { ProposeReceiptMatches(receiptId: $receiptId) ` + receiptMatchCandidateFields + ` }`

	DocumentRecordReceipt = `mutation ` + receiptVariables + ` {
  RecordReceipt` + receiptArguments + ` { financeReceipt ` + financeReceiptFields + ` receiptCheckSummary receiptMatchCandidates ` + receiptMatchCandidateFields + ` isReplaced }
}`

	DocumentPreviewRecordReceipt = `query ` + receiptVariables + ` {
  PreviewRecordReceipt` + receiptArguments + ` { receiptCheckState checkDifferenceAmount receiptCheckSummary receiptMatchCandidates ` + receiptMatchCandidateFields + ` isReplacing }
}`

	DocumentMatchReceipt = `mutation ($receiptId: String!, $financeTransactionId: String!, $matchedAmount: String) {
  MatchReceipt(receiptId: $receiptId, financeTransactionId: $financeTransactionId, matchedAmount: $matchedAmount) ` + financeReceiptFields + `
}`

	DocumentUnmatchReceipt = `mutation ($receiptId: String!, $financeTransactionId: String!) {
  UnmatchReceipt(receiptId: $receiptId, financeTransactionId: $financeTransactionId) ` + financeReceiptFields + `
}`

	DocumentDeleteReceipt = `mutation ($receiptId: String!) { DeleteReceipt(receiptId: $receiptId) }`

	DocumentReadReceipt = `mutation ($agentAttachmentId: String, $mailboxItemId: String, $financeTransactionId: String) {
  ReadReceipt(agentAttachmentId: $agentAttachmentId, mailboxItemId: $mailboxItemId, financeTransactionId: $financeTransactionId) { agentJobId }
}`

	DocumentSetBudget = `mutation ($spendingCategoryId: String!, $monthlyAmount: String!, $currencyCode: String, $effectiveFrom: String) {
  SetBudget(spendingCategoryId: $spendingCategoryId, monthlyAmount: $monthlyAmount, currencyCode: $currencyCode, effectiveFrom: $effectiveFrom) ` + budgetFields + `
}`

	DocumentCreateSavingsTarget = `mutation ($savingsTargetName: String!, $targetAmount: String!, $currencyCode: String, $targetOn: String!, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!], $financeAccountIds: [String!]) {
  CreateSavingsTarget(savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn, assetIds: $assetIds, financeAccountIds: $financeAccountIds) ` + savingsTargetStandingFields + `
}`

	DocumentUpdateSavingsTarget = `mutation ($savingsTargetId: String!, $savingsTargetName: String, $targetAmount: String, $currencyCode: String, $targetOn: String, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!], $financeAccountIds: [String!]) {
  UpdateSavingsTarget(savingsTargetId: $savingsTargetId, savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn, assetIds: $assetIds, financeAccountIds: $financeAccountIds) ` + savingsTargetStandingFields + `
}`

	DocumentCloseSavingsTarget = `mutation ($savingsTargetId: String!, $closedOn: String, $shouldReopen: Boolean) {
  CloseSavingsTarget(savingsTargetId: $savingsTargetId, closedOn: $closedOn, shouldReopen: $shouldReopen) ` + savingsTargetStandingFields + `
}`
)

// FinanceDocuments is every finance document by the operation it runs, for
// the test that checks each against the schema.
var FinanceDocuments = map[string]string{
	"FinanceProviders": DocumentFinanceProviders, "FinanceSources": DocumentFinanceSources,
	"FinanceAccounts": DocumentFinanceAccounts, "CreditUsage": DocumentCreditUsage, "FinanceTransactions": DocumentFinanceTransactions,
	"FinanceTrades":          DocumentFinanceTrades,
	"FinanceSpendingSummary": DocumentFinanceSpendingSummary, "ExchangeRate": DocumentExchangeRate,
	"ConvertCurrency": DocumentConvertCurrency, "NetWorth": DocumentNetWorth, "Assets": DocumentAssets,
	"AssetHistory": DocumentAssetHistory, "SpendingCategories": DocumentSpendingCategories,
	"SpendingRules": DocumentSpendingRules, "ProposeSpendingRules": DocumentProposeSpendingRules,
	"Budgets": DocumentBudgets, "BudgetStatus": DocumentBudgetStatus,
	"SavingSummary": DocumentSavingSummary, "SpendingByDay": DocumentSpendingByDay, "CashFlow": DocumentCashFlow, "SavingsTargets": DocumentSavingsTargets,
	"ReportingCurrency": DocumentReportingCurrency,
	"StatementImport":   DocumentStatementImport, "ImportStatement": DocumentImportStatement,
	"RegenerateStatementImportAddress": DocumentRegenerateStatementImportAddress,
	"ImportTransactions":               DocumentImportTransactions, "PreviewImportTransactions": DocumentPreviewImportTransactions,
	"RenameStatementAccount": DocumentRenameStatementAccount,
	"DeleteStatementAccount": DocumentDeleteStatementAccount,
	"CreateFinanceLinkToken": DocumentCreateFinanceLinkToken, "CompleteFinanceLink": DocumentCompleteFinanceLink,
	"CompleteFinanceRepair": DocumentCompleteFinanceRepair, "LinkSimpleFIN": DocumentLinkSimpleFIN,
	"ImportFinanceCredential": DocumentImportFinanceCredential, "SetReportingCurrency": DocumentSetReportingCurrency,
	"CreateAsset": DocumentCreateAsset,
	"UpdateAsset": DocumentUpdateAsset, "CloseAsset": DocumentCloseAsset, "DeleteAsset": DocumentDeleteAsset,
	"RecordValuation": DocumentRecordValuation, "DeleteValuation": DocumentDeleteValuation,
	"CreateSpendingCategory": DocumentCreateSpendingCategory, "UpdateSpendingCategory": DocumentUpdateSpendingCategory,
	"DeleteSpendingCategory": DocumentDeleteSpendingCategory, "CreateSpendingRule": DocumentCreateSpendingRule,
	"UpdateSpendingRule": DocumentUpdateSpendingRule, "DeleteSpendingRule": DocumentDeleteSpendingRule,
	"CategorizeTransaction": DocumentCategorizeTransaction, "CategorizeTransactions": DocumentCategorizeTransactions,
	"SetBudget":        DocumentSetBudget,
	"CountTransaction": DocumentCountTransaction, "UndoCountTransaction": DocumentUndoCountTransaction,
	"CreateSavingsTarget": DocumentCreateSavingsTarget,
	"UpdateSavingsTarget": DocumentUpdateSavingsTarget, "CloseSavingsTarget": DocumentCloseSavingsTarget,
	"AnnotateTransaction": DocumentAnnotateTransaction, "FinanceReceipts": DocumentFinanceReceipts, "FinanceReceipt": DocumentFinanceReceipt,
	"ProposeReceiptMatches": DocumentProposeReceiptMatches, "RecordReceipt": DocumentRecordReceipt,
	"PreviewRecordReceipt": DocumentPreviewRecordReceipt, "MatchReceipt": DocumentMatchReceipt,
	"UnmatchReceipt": DocumentUnmatchReceipt, "DeleteReceipt": DocumentDeleteReceipt, "ReadReceipt": DocumentReadReceipt,
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
