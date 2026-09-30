package models

import "time"

// CategorizedBy is what gave a finance transaction its spending category.
type CategorizedBy string

// Who may categorize, in the order they are asked: the person, then their
// spending rules, then the fixed mapping from provider categories, then
// the categorize model. What the person chose nothing else overwrites.
const (
	CategorizedByPerson                  CategorizedBy = "person"
	CategorizedBySpendingRule            CategorizedBy = "spending_rule"
	CategorizedByProviderCategoryMapping CategorizedBy = "provider_category_mapping"
	CategorizedByCategorizeModel         CategorizedBy = "categorize_model"
)

// IsValid says it is one of the four.
func (self CategorizedBy) IsValid() bool {
	switch self {
	case CategorizedByPerson, CategorizedBySpendingRule, CategorizedByProviderCategoryMapping, CategorizedByCategorizeModel:
		return true
	}
	return false
}

// SpendingCategory is a label in the person's own list. Never called just
// "category", so it cannot be confused with the provider's.
type SpendingCategory struct {
	ID                   string `json:"id"`
	AgentID              string `json:"agentId"`
	SpendingCategoryName string `json:"spendingCategoryName"`

	// ParentSpendingCategoryID is the one level of parent, empty for a
	// top-level spending category.
	ParentSpendingCategoryID string `json:"parentSpendingCategoryId,omitempty" graphapi:"nullable"`

	// IsIncome says money in it is income rather than a refund of
	// spending; IsHidden that it is left out of lists and charts.
	IsIncome bool `json:"isIncome"`
	IsHidden bool `json:"isHidden"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// SpendingRule assigns a spending category, or marks a transfer, to the
// finance transactions that match it.
type SpendingRule struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// MatchText is matched case-insensitively as a substring of the
	// merchant, or of the description when there is no merchant.
	MatchText string `json:"matchText"`

	// FinanceAccountID limits it to one finance account; empty is any.
	// MinimumAmount and MaximumAmount bound the signed amount, both ends
	// included; empty is unbounded.
	FinanceAccountID string `json:"financeAccountId,omitempty" graphapi:"nullable"`
	MinimumAmount    string `json:"minimumAmount,omitempty" graphapi:"nullable"`
	MaximumAmount    string `json:"maximumAmount,omitempty" graphapi:"nullable"`

	// SpendingCategoryID is what it assigns; IsTransfer marks matches as
	// transfers. At least one of the two.
	SpendingCategoryID string `json:"spendingCategoryId,omitempty" graphapi:"nullable"`
	IsTransfer         bool   `json:"isTransfer"`

	// RulePriority orders the rules: the lowest that matches wins.
	RulePriority int `json:"rulePriority"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Budget is an amount for one spending category for each month from a
// given month on, until a later row for the same spending category.
type Budget struct {
	ID                 string `json:"id"`
	AgentID            string `json:"agentId"`
	SpendingCategoryID string `json:"spendingCategoryId"`

	// MonthlyAmount is a decimal; zero ends the budget.
	MonthlyAmount string `json:"monthlyAmount"`
	CurrencyCode  string `json:"currencyCode"`

	// EffectiveFrom is the first day of the month it starts, "2006-01-02".
	EffectiveFrom string `json:"effectiveFrom"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// TargetMeasure is how a savings target's progress is measured.
type TargetMeasure string

// The two measures: money not spent (income minus spending since the
// start), or what chosen assets are worth now against what they were.
const (
	TargetMeasureCashFlow   TargetMeasure = "cash_flow"
	TargetMeasureAssetValue TargetMeasure = "asset_value"
)

// IsValid says the measure is one of the two.
func (self TargetMeasure) IsValid() bool {
	return self == TargetMeasureCashFlow || self == TargetMeasureAssetValue
}

// SavingsTarget is an amount to save by a date. Not an agent goal, which
// is a sentence a conversation works toward.
type SavingsTarget struct {
	ID                string `json:"id"`
	AgentID           string `json:"agentId"`
	SavingsTargetName string `json:"savingsTargetName"`

	// TargetAmount is a decimal, in CurrencyCode, to reach by TargetOn
	// ("2006-01-02").
	TargetAmount  string        `json:"targetAmount"`
	CurrencyCode  string        `json:"currencyCode"`
	TargetOn      string        `json:"targetOn"`
	TargetMeasure TargetMeasure `json:"targetMeasure"`

	// StartingAmount is what the assets were worth when it started, for
	// an asset_value target; StartedOn when it started.
	StartingAmount string `json:"startingAmount,omitempty" graphapi:"nullable"`
	StartedOn      string `json:"startedOn"`
	ClosedOn       string `json:"closedOn,omitempty" graphapi:"nullable"`

	// AssetIDs are the assets an asset_value target measures.
	AssetIDs []string `json:"assetIds"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// SpendingCategoryDay is one day's spending in one spending category and
// one currency: money out less refunds, transfers left out. Positive is
// spending; a day of refunds can be negative. SpendingCategoryID is empty
// for money out that is not categorized yet.
type SpendingCategoryDay struct {
	SpendingCategoryID string `json:"spendingCategoryId"`
	CurrencyCode       string `json:"currencyCode"`

	// SpentOn is the day, "2006-01-02".
	SpentOn        string `json:"spentOn"`
	SpendingAmount string `json:"spendingAmount"`
}

// MerchantMonthSpending is what one merchant charged one spending category
// in one month, in one currency: what the budget pace reads to expect a
// fixed monthly charge before it lands.
type MerchantMonthSpending struct {
	SpendingCategoryID string `json:"spendingCategoryId"`

	// MerchantName is the merchant, or the description when there is none.
	MerchantName string `json:"merchantName"`

	// SpendingMonth is the month, "2006-01".
	SpendingMonth           string `json:"spendingMonth"`
	CurrencyCode            string `json:"currencyCode"`
	SpendingAmount          string `json:"spendingAmount"`
	FinanceTransactionCount int    `json:"financeTransactionCount"`
}

// BudgetPace is how a spending category's month is going against its
// budget.
type BudgetPace string

// The four paces: the month will end well under the budget, near it, well
// over it (projected, after the first week), or spending is past it
// already.
const (
	BudgetPaceUnder   BudgetPace = "under"
	BudgetPaceOnTrack BudgetPace = "on_track"
	BudgetPaceAtRisk  BudgetPace = "at_risk"
	BudgetPaceOver    BudgetPace = "over"
)

// IsValid says the pace is one of the four.
func (self BudgetPace) IsValid() bool {
	switch self {
	case BudgetPaceUnder, BudgetPaceOnTrack, BudgetPaceAtRisk, BudgetPaceOver:
		return true
	}
	return false
}

// BudgetStatus is every spending category with a budget in one month,
// against that budget, as of one day.
type BudgetStatus struct {
	// Month is "2006-01"; AsOf the day it is computed for, "2006-01-02",
	// which is the last day of a past month.
	Month       string `json:"month"`
	AsOf        string `json:"asOf"`
	DayOfMonth  int    `json:"dayOfMonth"`
	DaysInMonth int    `json:"daysInMonth"`

	SpendingCategories []*SpendingCategoryBudgetStatus `json:"spendingCategories"`
}

// SpendingCategoryBudgetStatus is one spending category against its
// budget. Every amount is a decimal in CurrencyCode, the budget's:
// spending in another currency is converted at the rate of the day it
// posted.
type SpendingCategoryBudgetStatus struct {
	SpendingCategoryID   string `json:"spendingCategoryId"`
	SpendingCategoryName string `json:"spendingCategoryName"`

	BudgetAmount string `json:"budgetAmount"`
	CurrencyCode string `json:"currencyCode"`

	// SpendingAmount is the month's spending so far, and
	// SpendingBySameDayLastMonthAmount last month's by the same day.
	SpendingAmount                   string `json:"spendingAmount"`
	SpendingBySameDayLastMonthAmount string `json:"spendingBySameDayLastMonthAmount"`

	// FixedChargesDueAmount is what merchants that charged this spending
	// category in each of the last three full months are expected to
	// charge again this month and have not yet.
	FixedChargesDueAmount string `json:"fixedChargesDueAmount"`

	// ProjectedAmount is where the month is expected to end.
	ProjectedAmount string     `json:"projectedAmount"`
	BudgetPace      BudgetPace `json:"budgetPace"`

	// UnconvertedSpending is spending left out because its currency has no
	// exchange rate into the budget's, and
	// UnconvertedSpendingBySameDayLastMonth the same for last month's
	// spending by the same day. UnconvertedFixedChargesDue is the regular
	// charges still expected this month that were left out of
	// FixedChargesDueAmount, and so of the projection, for the same reason.
	UnconvertedSpending                   []*CurrencyAmount `json:"unconvertedSpending"`
	UnconvertedSpendingBySameDayLastMonth []*CurrencyAmount `json:"unconvertedSpendingBySameDayLastMonth"`
	UnconvertedFixedChargesDue            []*CurrencyAmount `json:"unconvertedFixedChargesDue"`
}

// CurrencyAmount is an amount in one currency, a decimal.
type CurrencyAmount struct {
	CurrencyCode string `json:"currencyCode"`
	Amount       string `json:"amount"`
}
