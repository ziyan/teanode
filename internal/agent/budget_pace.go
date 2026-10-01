package agent

import (
	"math/big"

	"github.com/ziyan/teanode/internal/models"
)

// The budget pace thresholds. Whether a month is on track is computed
// here, never by a model: the numbers have to be right and a threshold
// cannot be argued with.
const (
	// budgetPaceSettlingDays is how many days of a month pass before a
	// projection may say at_risk. Straight-line projection from a few
	// days of spending is mostly noise: one dinner out on the second is a
	// hot month by arithmetic.
	budgetPaceSettlingDays = 7
)

var (
	// budgetPaceAtRiskShare is how far past the budget a projection must
	// land to be at_risk: ten percent, so a month heading for 103 percent
	// is on track rather than an alert.
	budgetPaceAtRiskShare = big.NewRat(11, 10)

	// budgetPaceUnderShare is where under ends and on_track begins: a
	// month projected to end below ninety percent of its budget is under.
	budgetPaceUnderShare = big.NewRat(9, 10)

	// incomePaceBehindShare is how much of the income expected by today
	// must have come in for an income spending category not to be behind:
	// ninety percent, the mirror of budgetPaceUnderShare.
	incomePaceBehindShare = big.NewRat(9, 10)

	// incomePaceAheadShare is how far past the whole month's expected
	// income what came in must be to be ahead: ten percent, the mirror of
	// budgetPaceAtRiskShare. Measured against the whole month rather than
	// the days so far, so a pay day early in the month is not ahead for a
	// week and then on track for the rest of it.
	incomePaceAheadShare = big.NewRat(11, 10)

	// savingPaceShare is how far from the expected saving a projection may
	// land and still be on track: a tenth of the month's spending budgets,
	// the band a spending budget is on track within.
	savingPaceShare = big.NewRat(1, 10)
)

// SpendingCategoryMonthInput is what ProjectSpendingCategoryMonth reads,
// every amount already in the budget's currency.
type SpendingCategoryMonthInput struct {
	// BudgetAmount is the month's budget.
	BudgetAmount *big.Rat

	// SpendingByDay is each day's spending so far, the first of the month
	// first, up to and including today. A day of refunds may be negative.
	SpendingByDay []*big.Rat

	// FixedChargesDueAmount is what merchants that charged this spending
	// category in each of the last three full months are expected to
	// charge again and have not yet this month, at their median amounts.
	// FixedChargesSeenAmount is what those merchants have charged this
	// month already, which is in SpendingByDay and is left out of the
	// rate the rest of the month is projected at.
	FixedChargesDueAmount  *big.Rat
	FixedChargesSeenAmount *big.Rat

	// DayOfMonth is today, from one; DaysInMonth how many days the month
	// has.
	DayOfMonth  int
	DaysInMonth int
}

// SpendingCategoryMonthProjection is where a spending category's month is
// heading.
type SpendingCategoryMonthProjection struct {
	SpendingAmount  *big.Rat
	ProjectedAmount *big.Rat
	BudgetPace      models.BudgetPace
}

// ProjectSpendingCategoryMonth projects a spending category's month end:
// the spending so far, plus the fixed charges expected and not yet seen,
// plus the rest of the spending (what is not a fixed charge) at the rate
// it has come so far, for the days left.
//
// The pace is over once spending passes the budget; at_risk after the
// first week when the projection passes the budget by more than ten
// percent; under when the projection stays below ninety percent of it;
// on_track otherwise, which includes a first week that looks hot.
func ProjectSpendingCategoryMonth(input *SpendingCategoryMonthInput) *SpendingCategoryMonthProjection {
	spending := new(big.Rat)
	for _, daySpending := range input.SpendingByDay {
		if daySpending != nil {
			spending.Add(spending, daySpending)
		}
	}
	fixedChargesDue := ratOrZero(input.FixedChargesDueAmount)
	fixedChargesSeen := ratOrZero(input.FixedChargesSeenAmount)
	budget := ratOrZero(input.BudgetAmount)

	daysInMonth := max(input.DaysInMonth, 1)
	dayOfMonth := min(max(input.DayOfMonth, 1), daysInMonth)
	daysLeft := daysInMonth - dayOfMonth

	// The rest of the spending: what came from anywhere but the fixed
	// charges. Rent on the first is not a rate to project at.
	variableSpending := new(big.Rat).Sub(spending, fixedChargesSeen)
	if variableSpending.Sign() < 0 {
		variableSpending.SetInt64(0)
	}
	variableRest := new(big.Rat).Mul(variableSpending, big.NewRat(int64(daysLeft), int64(dayOfMonth)))

	projected := new(big.Rat).Add(spending, fixedChargesDue)
	projected.Add(projected, variableRest)

	projection := &SpendingCategoryMonthProjection{SpendingAmount: spending, ProjectedAmount: projected}
	switch {
	case budget.Sign() > 0 && spending.Cmp(budget) > 0:
		projection.BudgetPace = models.BudgetPaceOver
	case budget.Sign() > 0 && dayOfMonth > budgetPaceSettlingDays && projected.Cmp(new(big.Rat).Mul(budget, budgetPaceAtRiskShare)) > 0:
		projection.BudgetPace = models.BudgetPaceAtRisk
	case projected.Cmp(new(big.Rat).Mul(budget, budgetPaceUnderShare)) < 0:
		projection.BudgetPace = models.BudgetPaceUnder
	default:
		projection.BudgetPace = models.BudgetPaceOnTrack
	}
	return projection
}

// IncomeCategoryMonthInput is what ProjectIncomeCategoryMonth reads, every
// amount already in the budget's currency.
type IncomeCategoryMonthInput struct {
	// BudgetAmount is the income expected in the whole month, and
	// IncomeAmount what has come in so far.
	BudgetAmount *big.Rat
	IncomeAmount *big.Rat

	// DayOfMonth is today, from one; DaysInMonth how many days the month
	// has; IsMonthOver says today is past the month's last day.
	DayOfMonth  int
	DaysInMonth int
	IsMonthOver bool
}

// IncomeCategoryMonthProjection is where an income spending category's
// month is heading.
type IncomeCategoryMonthProjection struct {
	ExpectedByTodayAmount *big.Rat
	ProjectedAmount       *big.Rat
	IncomePace            models.IncomePace
}

// ProjectIncomeCategoryMonth projects an income spending category's month
// end and names its pace. Income comes in a few large amounts (a salary on
// one day, a payment on another), so the straight line spending is
// projected with would read a month before its pay day as heading for
// nothing. The projection is the expected income instead, or what came in
// when that is more already; a month that is over ends at what came in.
//
// The pace compares what came in with the expected income spread evenly
// over the month up to today: behind after the first week (or in a month
// that is over) when less than ninety percent of that came in; ahead when
// more than the whole month's expected income, by over ten percent, came
// in; on_track otherwise.
func ProjectIncomeCategoryMonth(input *IncomeCategoryMonthInput) *IncomeCategoryMonthProjection {
	budget := ratOrZero(input.BudgetAmount)
	income := ratOrZero(input.IncomeAmount)
	daysInMonth := max(input.DaysInMonth, 1)
	dayOfMonth := min(max(input.DayOfMonth, 1), daysInMonth)
	if input.IsMonthOver {
		dayOfMonth = daysInMonth
	}
	expectedByToday := new(big.Rat).Mul(budget, big.NewRat(int64(dayOfMonth), int64(daysInMonth)))

	projected := new(big.Rat).Set(income)
	if !input.IsMonthOver && income.Cmp(budget) < 0 {
		projected.Set(budget)
	}

	projection := &IncomeCategoryMonthProjection{ExpectedByTodayAmount: expectedByToday, ProjectedAmount: projected}
	switch {
	case budget.Sign() > 0 && income.Cmp(new(big.Rat).Mul(budget, incomePaceAheadShare)) > 0:
		projection.IncomePace = models.IncomePaceAhead
	case budget.Sign() > 0 && (input.IsMonthOver || dayOfMonth > budgetPaceSettlingDays) &&
		income.Cmp(new(big.Rat).Mul(expectedByToday, incomePaceBehindShare)) < 0:
		projection.IncomePace = models.IncomePaceBehind
	default:
		projection.IncomePace = models.IncomePaceOnTrack
	}
	return projection
}

// SavingMonthInput is what ProjectSavingMonth reads, every amount in one
// currency.
type SavingMonthInput struct {
	// ExpectedIncomeAmount and ExpectedSpendingAmount are the month's
	// income and spending budgets added up.
	ExpectedIncomeAmount   *big.Rat
	ExpectedSpendingAmount *big.Rat

	// ProjectedIncomeAmount and ProjectedSpendingAmount are where the
	// month's income and spending are heading, or ended for a month that
	// is over.
	ProjectedIncomeAmount   *big.Rat
	ProjectedSpendingAmount *big.Rat

	// DayOfMonth is today, from one; IsMonthOver says today is past the
	// month's last day.
	DayOfMonth  int
	IsMonthOver bool
}

// SavingMonthProjection is a month's saving as its budgets expect it and
// as it is heading.
type SavingMonthProjection struct {
	ExpectedSavingAmount   *big.Rat
	ProjectedSavingAmount  *big.Rat
	SavingDifferenceAmount *big.Rat
	SavingPace             models.SavingPace
}

// ProjectSavingMonth is a month's expected saving (income budgets less
// spending budgets), its projected saving (projected income less projected
// spending) and the difference. The pace is on_track while the projection
// lands within a tenth of the spending budgets (of the income budgets when
// there are no spending budgets) either side of the expected saving;
// behind below that, only after the first week or in a month that is
// over, since a hot first week projects spending high; ahead above it.
func ProjectSavingMonth(input *SavingMonthInput) *SavingMonthProjection {
	expectedSaving := new(big.Rat).Sub(ratOrZero(input.ExpectedIncomeAmount), ratOrZero(input.ExpectedSpendingAmount))
	projectedSaving := new(big.Rat).Sub(ratOrZero(input.ProjectedIncomeAmount), ratOrZero(input.ProjectedSpendingAmount))
	difference := new(big.Rat).Sub(projectedSaving, expectedSaving)

	toleranceBase := ratOrZero(input.ExpectedSpendingAmount)
	if toleranceBase.Sign() <= 0 {
		toleranceBase = ratOrZero(input.ExpectedIncomeAmount)
	}
	tolerance := new(big.Rat).Mul(new(big.Rat).Abs(toleranceBase), savingPaceShare)

	projection := &SavingMonthProjection{ExpectedSavingAmount: expectedSaving, ProjectedSavingAmount: projectedSaving, SavingDifferenceAmount: difference}
	switch {
	case difference.Cmp(new(big.Rat).Neg(tolerance)) < 0 && (input.IsMonthOver || input.DayOfMonth > budgetPaceSettlingDays):
		projection.SavingPace = models.SavingPaceBehind
	case difference.Cmp(tolerance) > 0:
		projection.SavingPace = models.SavingPaceAhead
	default:
		projection.SavingPace = models.SavingPaceOnTrack
	}
	return projection
}

func ratOrZero(value *big.Rat) *big.Rat {
	if value == nil {
		return new(big.Rat)
	}
	return value
}
