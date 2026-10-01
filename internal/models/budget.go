package models

import "time"

// CategorizedBy is what gave a finance transaction its spending category.
type CategorizedBy string

// Who may categorize, in the order they are asked: the person, then their
// spending rules, then the fixed mapping from provider categories, then
// the categorize model. What the person chose nothing else overwrites.
// Transfer detection, pairing money out of one account with the same
// amount into another, gives only the transfer category, and a transfer
// it or the mapping gave is not taken over by a spending rule.
const (
	CategorizedByPerson                  CategorizedBy = "person"
	CategorizedBySpendingRule            CategorizedBy = "spending_rule"
	CategorizedByProviderCategoryMapping CategorizedBy = "provider_category_mapping"
	CategorizedByCategorizeModel         CategorizedBy = "categorize_model"
	CategorizedByTransferDetection       CategorizedBy = "transfer_detection"
)

// IsValid says it is one of the five.
func (self CategorizedBy) IsValid() bool {
	switch self {
	case CategorizedByPerson, CategorizedBySpendingRule, CategorizedByProviderCategoryMapping, CategorizedByCategorizeModel,
		CategorizedByTransferDetection:
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

	// IsTransfer says it is the agent's transfer category, built in and
	// one per agent: a finance transaction in it moved money between the
	// person's own accounts and is neither spending nor income. It cannot
	// be deleted, be income, or have a parent or children.
	IsTransfer bool `json:"isTransfer"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// SpendingRule assigns a spending category to the finance transactions
// that match it; the transfer category marks them transfers.
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

	// SpendingCategoryID is what it assigns.
	SpendingCategoryID string `json:"spendingCategoryId"`

	// RulePriority orders the rules: the lowest that matches wins.
	RulePriority int `json:"rulePriority"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Budget is an amount for one spending category for each month from a
// given month on, until a later row for the same spending category. On a
// spending category that is income it is the income expected each month
// rather than a limit on spending.
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

// The three measures: money not spent (income minus spending since the
// start), what chosen assets and finance accounts are worth now against
// what they were, or net worth now against what it was.
const (
	TargetMeasureCashFlow   TargetMeasure = "cash_flow"
	TargetMeasureAssetValue TargetMeasure = "asset_value"
	TargetMeasureNetWorth   TargetMeasure = "net_worth"
)

// IsValid says the measure is one of the three.
func (self TargetMeasure) IsValid() bool {
	return self == TargetMeasureCashFlow || self == TargetMeasureAssetValue || self == TargetMeasureNetWorth
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
	// an asset_value target, or the net worth then, for a net_worth one;
	// StartedOn when it started.
	StartingAmount string `json:"startingAmount,omitempty" graphapi:"nullable"`
	StartedOn      string `json:"startedOn"`
	ClosedOn       string `json:"closedOn,omitempty" graphapi:"nullable"`

	// AssetIDs are the assets an asset_value target measures, and
	// FinanceAccountIDs the finance accounts it measures whole: every
	// asset the account values, its holdings included, as they are on the
	// day the progress is read. An asset reached both ways counts once.
	AssetIDs          []string `json:"assetIds"`
	FinanceAccountIDs []string `json:"financeAccountIds"`

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

// IncomeCategoryDay is one day's income in one income spending category
// and one currency: money in less money taken back, transfers left out.
type IncomeCategoryDay struct {
	SpendingCategoryID string `json:"spendingCategoryId"`
	CurrencyCode       string `json:"currencyCode"`

	// ReceivedOn is the day, "2006-01-02".
	ReceivedOn   string `json:"receivedOn"`
	IncomeAmount string `json:"incomeAmount"`
}

// CashFlowDay is one day's income and spending in one currency, as the
// Spending section and cash flow count them: spending is money out less
// refunds in spending categories that are not income, and money out with no
// spending category; income is what income categories took in, and money in
// with no spending category. Transfers are left out of both.
type CashFlowDay struct {
	// CashFlowOn is the day, "2006-01-02".
	CashFlowOn     string `json:"cashFlowOn"`
	CurrencyCode   string `json:"currencyCode"`
	IncomeAmount   string `json:"incomeAmount"`
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

// IncomePace is how an income spending category's month is going against
// the income expected of it. Falling short is the bad direction, so it is
// not a BudgetPace.
type IncomePace string

// The three paces: less has come in than was expected by this day of the
// month (after the first week), about what was expected, or more than the
// whole month was expected to bring already.
const (
	IncomePaceBehind  IncomePace = "behind"
	IncomePaceOnTrack IncomePace = "on_track"
	IncomePaceAhead   IncomePace = "ahead"
)

// IsValid says the pace is one of the three.
func (self IncomePace) IsValid() bool {
	return self == IncomePaceBehind || self == IncomePaceOnTrack || self == IncomePaceAhead
}

// SavingPace is how a month's saving, income less spending, is heading
// against the saving its budgets expect.
type SavingPace string

// The three paces: the month is heading for less saved than its budgets
// expect, about what they expect, or more.
const (
	SavingPaceBehind  SavingPace = "behind"
	SavingPaceOnTrack SavingPace = "on_track"
	SavingPaceAhead   SavingPace = "ahead"
)

// IsValid says the pace is one of the three.
func (self SavingPace) IsValid() bool {
	return self == SavingPaceBehind || self == SavingPaceOnTrack || self == SavingPaceAhead
}

// BudgetStatus is every spending category with a budget in one month, or
// in any month of one year, against that budget, as of one day.
type BudgetStatus struct {
	// Month is "2006-01"; AsOf the day it is computed for, "2006-01-02",
	// which is the last day of a past month. For a year, Month is empty and
	// DayOfMonth and DaysInMonth are of AsOf's month.
	Month       string `json:"month"`
	AsOf        string `json:"asOf"`
	DayOfMonth  int    `json:"dayOfMonth"`
	DaysInMonth int    `json:"daysInMonth"`

	// Year is "2006" for the status of a year, empty for a month's. Of a
	// year, MonthsElapsedCount is how many of its months have begun by
	// AsOf (twelve once it is over, none before it begins), and DayOfYear
	// and DaysInYear say how far through it AsOf is (DayOfYear none
	// before it begins). All three are zero for a month.
	Year               string `json:"year"`
	MonthsElapsedCount int    `json:"monthsElapsedCount"`
	DayOfYear          int    `json:"dayOfYear"`
	DaysInYear         int    `json:"daysInYear"`

	// SpendingCategories are the budgets on spending, and
	// IncomeCategories the budgets on income spending categories: the
	// income expected each month.
	SpendingCategories []*SpendingCategoryBudgetStatus `json:"spendingCategories"`
	IncomeCategories   []*IncomeCategoryBudgetStatus   `json:"incomeCategories"`
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

	// BudgetToDateAmount is the part of BudgetAmount for the days so far:
	// the month's budget spread evenly over its days up to AsOf, the whole
	// of it once the month is over. For a year, each month's budget in
	// force that month, whole for the months that are over and spread
	// over the days so far for the month in progress.
	BudgetToDateAmount string `json:"budgetToDateAmount"`

	// BudgetedMonthCount is how many months the budget was in force: one
	// for a month, and for a year the months of it that had a budget for
	// this spending category, which BudgetAmount adds up.
	BudgetedMonthCount int `json:"budgetedMonthCount"`

	// SpendingAmount is the month's spending so far, and
	// SpendingBySameDayLastMonthAmount last month's by the same day. For a
	// year, SpendingAmount is the spending of the months that had this
	// budget, and nothing is compared with last month.
	SpendingAmount                   string `json:"spendingAmount"`
	SpendingBySameDayLastMonthAmount string `json:"spendingBySameDayLastMonthAmount"`

	// FixedChargesDueAmount is what merchants that charged this spending
	// category in each of the last three full months are expected to
	// charge again this month and have not yet. For a year, those of the
	// month in progress.
	FixedChargesDueAmount string `json:"fixedChargesDueAmount"`

	// ExpectedRepeatCharges are those merchants, one by one, so a
	// projection can be explained: the ones in the budget's currency, or
	// converted into it, add up to FixedChargesDueAmount, and the rest are
	// in their own currency and in UnconvertedFixedChargesDue. Largest
	// first.
	ExpectedRepeatCharges []*ExpectedRepeatCharge `json:"expectedRepeatCharges"`

	// ProjectedAmount is where the month is expected to end: SpendingAmount,
	// plus FixedChargesDueAmount, plus the rest of the spending at the rate
	// it has come this month for the days left. For a year
	// (ProjectSpendingCategoryYear), the months that are over as they
	// ended, the month in progress as projected, and each budgeted month
	// still to come at the average of those.
	ProjectedAmount string     `json:"projectedAmount"`
	BudgetPace      BudgetPace `json:"budgetPace"`

	// UnconvertedSpending is spending left out because its currency has no
	// exchange rate into the budget's, and
	// UnconvertedSpendingBySameDayLastMonth the same for last month's
	// spending by the same day. UnconvertedFixedChargesDue is the repeat
	// charges still expected this month that were left out of
	// FixedChargesDueAmount, and so of the projection, for the same reason.
	UnconvertedSpending                   []*CurrencyAmount `json:"unconvertedSpending"`
	UnconvertedSpendingBySameDayLastMonth []*CurrencyAmount `json:"unconvertedSpendingBySameDayLastMonth"`
	UnconvertedFixedChargesDue            []*CurrencyAmount `json:"unconvertedFixedChargesDue"`
}

// ExpectedRepeatCharge is a merchant that charged a spending category in
// each of the last three full months and has not yet this month: what it
// is expected to charge, its median over those months.
type ExpectedRepeatCharge struct {
	MerchantName   string `json:"merchantName"`
	ExpectedAmount string `json:"expectedAmount"`
	CurrencyCode   string `json:"currencyCode"`
}

// IncomeCategoryBudgetStatus is one income spending category against the
// income expected of it. Every amount is a decimal in CurrencyCode, the
// budget's: income in another currency is converted at the rate of the day
// it came in.
type IncomeCategoryBudgetStatus struct {
	SpendingCategoryID   string `json:"spendingCategoryId"`
	SpendingCategoryName string `json:"spendingCategoryName"`

	// BudgetAmount is the income expected in the whole month, or, for a
	// year, in each of its months that had this budget, added up.
	BudgetAmount string `json:"budgetAmount"`
	CurrencyCode string `json:"currencyCode"`

	// BudgetedMonthCount is how many months the budget was in force: one
	// for a month, and for a year the months of it BudgetAmount adds up.
	BudgetedMonthCount int `json:"budgetedMonthCount"`

	// IncomeAmount is what came in this month so far, and
	// IncomeBySameDayLastMonthAmount what came in last month by the same
	// day. For a year, IncomeAmount is what came in during the months that
	// had this budget, and nothing is compared with last month.
	IncomeAmount                   string `json:"incomeAmount"`
	IncomeBySameDayLastMonthAmount string `json:"incomeBySameDayLastMonthAmount"`

	// ExpectedByTodayAmount is the month's expected income spread evenly
	// over its days, up to and including today: what the pace compares
	// IncomeAmount with. For a year, the expected income of the months that
	// are over and the month in progress's spread over its days so far.
	ExpectedByTodayAmount string `json:"expectedByTodayAmount"`

	// ProjectedAmount is where the month is expected to end: for a month
	// in progress, the expected income, or what came in when that is more
	// already, since income comes in a few large amounts that a straight
	// line cannot project; for a month that is over, what came in. For a
	// year (ProjectIncomeCategoryYear), the months added up that way, each
	// month still to come at its expected income.
	ProjectedAmount string     `json:"projectedAmount"`
	IncomePace      IncomePace `json:"incomePace"`

	// UnconvertedIncome is income left out because its currency has no
	// exchange rate into the budget's, and
	// UnconvertedIncomeBySameDayLastMonth the same for last month's.
	UnconvertedIncome                   []*CurrencyAmount `json:"unconvertedIncome"`
	UnconvertedIncomeBySameDayLastMonth []*CurrencyAmount `json:"unconvertedIncomeBySameDayLastMonth"`
}

// SavingSummary is one month's saving, income less spending, as its
// budgets expect it and as it is going, in one currency. Spending and
// income are counted the way cash flow counts them, transfers left out and
// every spending category included, whether or not it has a budget.
type SavingSummary struct {
	// Month is "2006-01"; AsOf the day it is computed for, "2006-01-02",
	// which is the last day of a past month. For a year, Month is empty and
	// DayOfMonth and DaysInMonth are of AsOf's month.
	Month       string `json:"month"`
	AsOf        string `json:"asOf"`
	DayOfMonth  int    `json:"dayOfMonth"`
	DaysInMonth int    `json:"daysInMonth"`

	// Year, MonthsElapsedCount, DayOfYear and DaysInYear are as a
	// BudgetStatus's: set for the saving of a year, zero for a month's.
	Year               string `json:"year"`
	MonthsElapsedCount int    `json:"monthsElapsedCount"`
	DayOfYear          int    `json:"dayOfYear"`
	DaysInYear         int    `json:"daysInYear"`

	// BudgetedMonths are the months, "2006-01" in order, in which at least
	// one budget, income or spending, was in force; BudgetedMonthCount is
	// how many, and BudgetedMonthsElapsedCount how many of them have begun
	// by AsOf. Of a year, every expected, actual and projected amount
	// counts these months only, so a year whose budgets began in September
	// compares September onward with September onward. A year with none
	// counts all its months instead, expects nothing, and its SavingPace
	// says nothing. Of a month, the month itself when it has a budget.
	BudgetedMonths             []string `json:"budgetedMonths"`
	BudgetedMonthCount         int      `json:"budgetedMonthCount"`
	BudgetedMonthsElapsedCount int      `json:"budgetedMonthsElapsedCount"`

	// ReportingCurrencyCode is what every amount is in. A budget is
	// converted at AsOf's rate, and income and spending at the rate of the
	// day each came in or went out. Empty, and every amount zero, when
	// there is no currency to report in yet.
	ReportingCurrencyCode string `json:"reportingCurrencyCode"`

	// IncomeBudgetCount and SpendingBudgetCount are how many budgets of
	// each kind are in force in the month, or for a year, how many
	// spending categories of each kind had a budget in any of its months.
	IncomeBudgetCount   int `json:"incomeBudgetCount"`
	SpendingBudgetCount int `json:"spendingBudgetCount"`

	// ExpectedIncomeAmount is the income budgets added up, and
	// ExpectedSpendingAmount the spending budgets; ExpectedSavingAmount is
	// the first less the second.
	ExpectedIncomeAmount   string `json:"expectedIncomeAmount"`
	ExpectedSpendingAmount string `json:"expectedSpendingAmount"`
	ExpectedSavingAmount   string `json:"expectedSavingAmount"`

	// IncomeAmount, SpendingAmount and SavingAmount are the month's so
	// far: the whole month for a month that is over.
	IncomeAmount   string `json:"incomeAmount"`
	SpendingAmount string `json:"spendingAmount"`
	SavingAmount   string `json:"savingAmount"`

	// ProjectedIncomeAmount is the income so far plus what each income
	// budget still expects; ProjectedSpendingAmount the spending projected
	// to the month's end the way a budget projects a spending category's;
	// ProjectedSavingAmount the first less the second. For a month that is
	// over they are the month's own figures.
	ProjectedIncomeAmount   string `json:"projectedIncomeAmount"`
	ProjectedSpendingAmount string `json:"projectedSpendingAmount"`
	ProjectedSavingAmount   string `json:"projectedSavingAmount"`

	// SavingDifferenceAmount is ProjectedSavingAmount less
	// ExpectedSavingAmount: how far the month is heading from what its
	// budgets expect, or for a month that is over, how far it ended.
	SavingDifferenceAmount string     `json:"savingDifferenceAmount"`
	SavingPace             SavingPace `json:"savingPace"`

	// UnconvertedCurrencyCodes are the currencies left out for want of an
	// exchange rate into ReportingCurrencyCode.
	UnconvertedCurrencyCodes []string `json:"unconvertedCurrencyCodes"`
}

// CurrencyAmount is an amount in one currency, a decimal.
type CurrencyAmount struct {
	CurrencyCode string `json:"currencyCode"`
	Amount       string `json:"amount"`
}
