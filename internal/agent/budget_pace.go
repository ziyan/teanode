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

// BudgetMonthPhase is where one month of a year stands on the day a
// year's budgets are computed for.
type BudgetMonthPhase string

// The three phases: the month is over, it is the month in progress, or it
// has not begun.
const (
	BudgetMonthPhaseOver       BudgetMonthPhase = "over"
	BudgetMonthPhaseInProgress BudgetMonthPhase = "in_progress"
	BudgetMonthPhaseToCome     BudgetMonthPhase = "to_come"
)

// SpendingCategoryYearMonth is one month of a year in which a spending
// category had a budget, every amount in the year's currency.
type SpendingCategoryYearMonth struct {
	BudgetMonthPhase BudgetMonthPhase

	// BudgetAmount is the month's budget as it was in force that month.
	BudgetAmount *big.Rat

	// SpendingAmount is the month's spending, so far for the month in
	// progress; nil for a month to come.
	SpendingAmount *big.Rat

	// ProjectedAmount and BudgetToDateAmount are the month in progress's
	// projection (ProjectSpendingCategoryMonth) and its budget over the
	// days so far; ignored for the other phases.
	ProjectedAmount    *big.Rat
	BudgetToDateAmount *big.Rat
}

// SpendingCategoryYearInput is what ProjectSpendingCategoryYear reads: the
// months of the year that had a budget, whichever they were.
type SpendingCategoryYearInput struct {
	Months []*SpendingCategoryYearMonth

	// DayOfYear is the day computed for, from one, or zero for a year not
	// begun; IsYearOver says that day is past the year's last.
	DayOfYear  int
	IsYearOver bool
}

// SpendingCategoryYearProjection is where a spending category's year is
// heading against the budgets of its months.
type SpendingCategoryYearProjection struct {
	BudgetAmount       *big.Rat
	BudgetToDateAmount *big.Rat
	SpendingAmount     *big.Rat
	ProjectedAmount    *big.Rat
	BudgetedMonthCount int
	BudgetPace         models.BudgetPace
}

// ProjectSpendingCategoryYear adds up a spending category's budgeted
// months and projects the year's end. The budget is each month's budget
// as it was in force that month, so a budget changed or ended part way
// through the year counts each month at what it was then, and the
// spending is that of the months that had a budget.
//
// The projection is built from the monthly ones where they exist: a month
// that is over as it ended, the month in progress as it is projected
// (repeat charges and its pace counted), and each budgeted month still to
// come at the average of those, which is the year carried on the way it
// has gone. With no month begun there is nothing to carry on, and a month
// to come counts at its budget.
//
// The pace is a month's thresholds over the year: over once spending
// passes the year's budget; at_risk, after the first week of the year,
// when the projection passes it by more than ten percent; under below
// ninety percent of it; on_track otherwise.
func ProjectSpendingCategoryYear(input *SpendingCategoryYearInput) *SpendingCategoryYearProjection {
	projection := &SpendingCategoryYearProjection{
		BudgetAmount: new(big.Rat), BudgetToDateAmount: new(big.Rat), SpendingAmount: new(big.Rat), BudgetedMonthCount: len(input.Months),
	}
	monthFigures := []*big.Rat{}
	toComeCount := 0
	toComeBudget := new(big.Rat)
	for _, month := range input.Months {
		budget := ratOrZero(month.BudgetAmount)
		projection.BudgetAmount.Add(projection.BudgetAmount, budget)
		switch month.BudgetMonthPhase {
		case BudgetMonthPhaseOver:
			spending := ratOrZero(month.SpendingAmount)
			projection.BudgetToDateAmount.Add(projection.BudgetToDateAmount, budget)
			projection.SpendingAmount.Add(projection.SpendingAmount, spending)
			monthFigures = append(monthFigures, spending)
		case BudgetMonthPhaseInProgress:
			projection.BudgetToDateAmount.Add(projection.BudgetToDateAmount, ratOrZero(month.BudgetToDateAmount))
			projection.SpendingAmount.Add(projection.SpendingAmount, ratOrZero(month.SpendingAmount))
			monthFigures = append(monthFigures, ratOrZero(month.ProjectedAmount))
		default:
			toComeCount++
			toComeBudget.Add(toComeBudget, budget)
		}
	}
	projection.ProjectedAmount = carriedOnYear(monthFigures, toComeCount, toComeBudget)

	budget := projection.BudgetAmount
	isSettled := input.IsYearOver || input.DayOfYear > budgetPaceSettlingDays
	switch {
	case budget.Sign() > 0 && projection.SpendingAmount.Cmp(budget) > 0:
		projection.BudgetPace = models.BudgetPaceOver
	case budget.Sign() > 0 && isSettled && projection.ProjectedAmount.Cmp(new(big.Rat).Mul(budget, budgetPaceAtRiskShare)) > 0:
		projection.BudgetPace = models.BudgetPaceAtRisk
	case projection.ProjectedAmount.Cmp(new(big.Rat).Mul(budget, budgetPaceUnderShare)) < 0:
		projection.BudgetPace = models.BudgetPaceUnder
	default:
		projection.BudgetPace = models.BudgetPaceOnTrack
	}
	return projection
}

// carriedOnYear is the months begun added up (monthFigures: each month
// that is over as it ended, the month in progress as projected), and
// toComeCount months more at their average; with no month begun, the
// months to come at toComeFallback, what they were budgeted.
func carriedOnYear(monthFigures []*big.Rat, toComeCount int, toComeFallback *big.Rat) *big.Rat {
	total := new(big.Rat)
	for _, figure := range monthFigures {
		total.Add(total, figure)
	}
	if toComeCount == 0 {
		return total
	}
	if len(monthFigures) == 0 {
		return total.Add(total, ratOrZero(toComeFallback))
	}
	carried := new(big.Rat).Mul(total, big.NewRat(int64(toComeCount), int64(len(monthFigures))))
	return total.Add(total, carried)
}

// IncomeCategoryYearMonth is one month of a year in which an income
// spending category had a budget, every amount in the year's currency.
type IncomeCategoryYearMonth struct {
	BudgetMonthPhase BudgetMonthPhase

	// BudgetAmount is the income expected that month.
	BudgetAmount *big.Rat

	// IncomeAmount is what came in, so far for the month in progress; nil
	// for a month to come.
	IncomeAmount *big.Rat

	// ProjectedAmount and ExpectedByTodayAmount are the month in
	// progress's (ProjectIncomeCategoryMonth); ignored for the other
	// phases.
	ProjectedAmount       *big.Rat
	ExpectedByTodayAmount *big.Rat
}

// IncomeCategoryYearInput is what ProjectIncomeCategoryYear reads.
type IncomeCategoryYearInput struct {
	Months     []*IncomeCategoryYearMonth
	DayOfYear  int
	IsYearOver bool
}

// IncomeCategoryYearProjection is where an income spending category's
// year is heading against the income expected in its months.
type IncomeCategoryYearProjection struct {
	BudgetAmount          *big.Rat
	IncomeAmount          *big.Rat
	ExpectedByTodayAmount *big.Rat
	ProjectedAmount       *big.Rat
	BudgetedMonthCount    int
	IncomePace            models.IncomePace
}

// ProjectIncomeCategoryYear adds up an income spending category's
// budgeted months the way ProjectIncomeCategoryMonth does one: a month that
// is over ends at what came in, the month in progress at its projection,
// and a month to come at the income expected of it. The income expected by
// today is the months that are over whole and the month in progress's
// share by its days.
//
// The pace is a month's over the year: ahead once more than the whole
// year's expected income, by over ten percent, came in; behind, after the
// first week of the year, when less than ninety percent of what was
// expected by today came in; on_track otherwise.
func ProjectIncomeCategoryYear(input *IncomeCategoryYearInput) *IncomeCategoryYearProjection {
	projection := &IncomeCategoryYearProjection{
		BudgetAmount: new(big.Rat), IncomeAmount: new(big.Rat), ExpectedByTodayAmount: new(big.Rat), ProjectedAmount: new(big.Rat),
		BudgetedMonthCount: len(input.Months),
	}
	for _, month := range input.Months {
		budget := ratOrZero(month.BudgetAmount)
		projection.BudgetAmount.Add(projection.BudgetAmount, budget)
		switch month.BudgetMonthPhase {
		case BudgetMonthPhaseOver:
			income := ratOrZero(month.IncomeAmount)
			projection.IncomeAmount.Add(projection.IncomeAmount, income)
			projection.ExpectedByTodayAmount.Add(projection.ExpectedByTodayAmount, budget)
			projection.ProjectedAmount.Add(projection.ProjectedAmount, income)
		case BudgetMonthPhaseInProgress:
			projection.IncomeAmount.Add(projection.IncomeAmount, ratOrZero(month.IncomeAmount))
			projection.ExpectedByTodayAmount.Add(projection.ExpectedByTodayAmount, ratOrZero(month.ExpectedByTodayAmount))
			projection.ProjectedAmount.Add(projection.ProjectedAmount, ratOrZero(month.ProjectedAmount))
		default:
			projection.ProjectedAmount.Add(projection.ProjectedAmount, budget)
		}
	}
	budget := projection.BudgetAmount
	isSettled := input.IsYearOver || input.DayOfYear > budgetPaceSettlingDays
	switch {
	case budget.Sign() > 0 && projection.IncomeAmount.Cmp(new(big.Rat).Mul(budget, incomePaceAheadShare)) > 0:
		projection.IncomePace = models.IncomePaceAhead
	case budget.Sign() > 0 && isSettled && projection.IncomeAmount.Cmp(new(big.Rat).Mul(projection.ExpectedByTodayAmount, incomePaceBehindShare)) < 0:
		projection.IncomePace = models.IncomePaceBehind
	default:
		projection.IncomePace = models.IncomePaceOnTrack
	}
	return projection
}
