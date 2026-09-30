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

func ratOrZero(value *big.Rat) *big.Rat {
	if value == nil {
		return new(big.Rat)
	}
	return value
}
