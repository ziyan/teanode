package agent

import (
	"math/big"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// days is a month's spending so far, one whole-unit amount per day.
func days(amounts ...int64) []*big.Rat {
	spending := make([]*big.Rat, 0, len(amounts))
	for _, amount := range amounts {
		spending = append(spending, big.NewRat(amount, 1))
	}
	return spending
}

func TestProjectSpendingCategoryMonth(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		input              *SpendingCategoryMonthInput
		expectedPace       models.BudgetPace
		expectedProjection int64
	}{
		{
			// Rent lands on the first and is a fixed charge seen: day two
			// projects the month at the rent, not at fifteen times it.
			name: "rent on the first is not at risk on day two",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(1600, 1), SpendingByDay: days(1500, 0),
				FixedChargesSeenAmount: big.NewRat(1500, 1), DayOfMonth: 2, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceOnTrack, expectedProjection: 1500,
		},
		{
			// The same rent on day ten with nothing else is still fine.
			name: "rent and nothing else on day ten",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(1600, 1), SpendingByDay: days(1500, 0, 0, 0, 0, 0, 0, 0, 0, 0),
				FixedChargesSeenAmount: big.NewRat(1500, 1), DayOfMonth: 10, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceOnTrack, expectedProjection: 1500,
		},
		{
			// Without knowing the rent is fixed, the straight line says the
			// month ends at fifteen times the budget; with it, the rent due
			// later is counted before it lands.
			name: "rent still due is counted before it lands",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(1600, 1), SpendingByDay: days(0, 0, 0),
				FixedChargesDueAmount: big.NewRat(1500, 1), DayOfMonth: 3, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceOnTrack, expectedProjection: 1500,
		},
		{
			// Dining at thirty a day against a four hundred budget: 360 by
			// the twelfth, heading for 900.
			name: "hot dining is at risk by day twelve",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(400, 1), SpendingByDay: days(30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30),
				DayOfMonth: 12, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceAtRisk, expectedProjection: 900,
		},
		{
			// The same pace in the first week is not yet at risk.
			name: "a hot first week is on track",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(400, 1), SpendingByDay: days(30, 30, 30),
				DayOfMonth: 3, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceOnTrack, expectedProjection: 900,
		},
		{
			name: "past the budget is over whatever the day",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(400, 1), SpendingByDay: days(420),
				DayOfMonth: 1, DaysInMonth: 31,
			},
			expectedPace: models.BudgetPaceOver, expectedProjection: 13020,
		},
		{
			name: "a quiet month is under",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(400, 1), SpendingByDay: days(5, 5, 5, 5, 5, 5, 5, 5, 5, 5),
				DayOfMonth: 10, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceUnder, expectedProjection: 150,
		},
		{
			// 380 projected is within ten percent either side: on track.
			name: "near the budget is on track",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(400, 1), SpendingByDay: days(114, 0, 0, 0, 0, 0, 0, 0, 0),
				DayOfMonth: 9, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceOnTrack, expectedProjection: 380,
		},
		{
			// A refund larger than the spending does not project negative
			// spending forward.
			name: "refunds do not make the rest negative",
			input: &SpendingCategoryMonthInput{
				BudgetAmount: big.NewRat(400, 1), SpendingByDay: days(50, -80),
				FixedChargesSeenAmount: big.NewRat(50, 1), DayOfMonth: 2, DaysInMonth: 30,
			},
			expectedPace: models.BudgetPaceUnder, expectedProjection: -30,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projection := ProjectSpendingCategoryMonth(testCase.input)
			if projection.BudgetPace != testCase.expectedPace {
				t.Fatalf("the pace is %s, not %s (projected %s)", testCase.expectedPace, projection.BudgetPace, projection.ProjectedAmount.FloatString(2))
			}
			if projection.ProjectedAmount.Cmp(big.NewRat(testCase.expectedProjection, 1)) != 0 {
				t.Fatalf("the projection is %d, not %s", testCase.expectedProjection, projection.ProjectedAmount.FloatString(2))
			}
		})
	}
}
