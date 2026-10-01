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

func TestProjectIncomeCategoryMonth(t *testing.T) {
	for _, testCase := range []struct {
		name                    string
		input                   *IncomeCategoryMonthInput
		expectedPace            models.IncomePace
		expectedProjection      int64
		expectedByTodayExpected int64
	}{
		{
			// Pay day is later in the month: nothing in yet in the first
			// week is not behind, and the month still heads for the salary.
			name: "nothing in during the first week is on track",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(0, 1), DayOfMonth: 6, DaysInMonth: 30,
			},
			expectedPace: models.IncomePaceOnTrack, expectedProjection: 3000, expectedByTodayExpected: 600,
		},
		{
			// Half the month gone and nothing in: behind the 1500 expected
			// by today, while the projection still assumes it arrives.
			name: "nothing in by mid month is behind",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(0, 1), DayOfMonth: 15, DaysInMonth: 30,
			},
			expectedPace: models.IncomePaceBehind, expectedProjection: 3000, expectedByTodayExpected: 1500,
		},
		{
			// One of two pay days in by the middle: what was expected by
			// today, on track.
			name: "half in by mid month is on track",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(1500, 1), DayOfMonth: 15, DaysInMonth: 30,
			},
			expectedPace: models.IncomePaceOnTrack, expectedProjection: 3000, expectedByTodayExpected: 1500,
		},
		{
			// A bonus on top of the salary: past the whole month by more
			// than a tenth, and the projection is what came in.
			name: "more than the month already is ahead",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(4000, 1), DayOfMonth: 3, DaysInMonth: 30,
			},
			expectedPace: models.IncomePaceAhead, expectedProjection: 4000, expectedByTodayExpected: 300,
		},
		{
			// The whole salary on the first is a lot of the month by
			// prorating, but not past the month: on track, not ahead.
			name: "the whole salary on the first is on track",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(3000, 1), DayOfMonth: 1, DaysInMonth: 30,
			},
			expectedPace: models.IncomePaceOnTrack, expectedProjection: 3000, expectedByTodayExpected: 100,
		},
		{
			// A month that is over ends at what came in, and is judged
			// against all of it.
			name: "a past month short of its income is behind",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(2000, 1), DayOfMonth: 30, DaysInMonth: 30, IsMonthOver: true,
			},
			expectedPace: models.IncomePaceBehind, expectedProjection: 2000, expectedByTodayExpected: 3000,
		},
		{
			name: "a past month near its income is on track",
			input: &IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: big.NewRat(2900, 1), DayOfMonth: 31, DaysInMonth: 31, IsMonthOver: true,
			},
			expectedPace: models.IncomePaceOnTrack, expectedProjection: 2900, expectedByTodayExpected: 3000,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projection := ProjectIncomeCategoryMonth(testCase.input)
			if projection.IncomePace != testCase.expectedPace {
				t.Fatalf("the pace is %s, not %s", projection.IncomePace, testCase.expectedPace)
			}
			if projection.ProjectedAmount.Cmp(big.NewRat(testCase.expectedProjection, 1)) != 0 {
				t.Fatalf("the projection is %s, not %d", projection.ProjectedAmount.FloatString(2), testCase.expectedProjection)
			}
			if projection.ExpectedByTodayAmount.Cmp(big.NewRat(testCase.expectedByTodayExpected, 1)) != 0 {
				t.Fatalf("expected by today is %s, not %d", projection.ExpectedByTodayAmount.FloatString(2), testCase.expectedByTodayExpected)
			}
		})
	}
}

func TestProjectSavingMonth(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		input              *SavingMonthInput
		expectedPace       models.SavingPace
		expectedSaving     int64
		expectedProjected  int64
		expectedDifference int64
	}{
		{
			// 4000 expected in, 3000 budgeted out: 1000 to save. Heading for
			// 4000 in and 3100 out is 900, within 300 of it.
			name: "close to the expected saving is on track",
			input: &SavingMonthInput{
				ExpectedIncomeAmount: big.NewRat(4000, 1), ExpectedSpendingAmount: big.NewRat(3000, 1),
				ProjectedIncomeAmount: big.NewRat(4000, 1), ProjectedSpendingAmount: big.NewRat(3100, 1), DayOfMonth: 12,
			},
			expectedPace: models.SavingPaceOnTrack, expectedSaving: 1000, expectedProjected: 900, expectedDifference: -100,
		},
		{
			name: "spending well over the budgets is behind",
			input: &SavingMonthInput{
				ExpectedIncomeAmount: big.NewRat(4000, 1), ExpectedSpendingAmount: big.NewRat(3000, 1),
				ProjectedIncomeAmount: big.NewRat(4000, 1), ProjectedSpendingAmount: big.NewRat(3600, 1), DayOfMonth: 12,
			},
			expectedPace: models.SavingPaceBehind, expectedSaving: 1000, expectedProjected: 400, expectedDifference: -600,
		},
		{
			// The same in the first week: a hot first week projects
			// spending high, so it is not called behind yet.
			name: "a hot first week is on track",
			input: &SavingMonthInput{
				ExpectedIncomeAmount: big.NewRat(4000, 1), ExpectedSpendingAmount: big.NewRat(3000, 1),
				ProjectedIncomeAmount: big.NewRat(4000, 1), ProjectedSpendingAmount: big.NewRat(3600, 1), DayOfMonth: 4,
			},
			expectedPace: models.SavingPaceOnTrack, expectedSaving: 1000, expectedProjected: 400, expectedDifference: -600,
		},
		{
			name: "a bonus on top is ahead",
			input: &SavingMonthInput{
				ExpectedIncomeAmount: big.NewRat(4000, 1), ExpectedSpendingAmount: big.NewRat(3000, 1),
				ProjectedIncomeAmount: big.NewRat(5000, 1), ProjectedSpendingAmount: big.NewRat(3000, 1), DayOfMonth: 2,
			},
			expectedPace: models.SavingPaceAhead, expectedSaving: 1000, expectedProjected: 2000, expectedDifference: 1000,
		},
		{
			name: "a past month that saved less is behind",
			input: &SavingMonthInput{
				ExpectedIncomeAmount: big.NewRat(4000, 1), ExpectedSpendingAmount: big.NewRat(3000, 1),
				ProjectedIncomeAmount: big.NewRat(3500, 1), ProjectedSpendingAmount: big.NewRat(3000, 1), DayOfMonth: 30, IsMonthOver: true,
			},
			expectedPace: models.SavingPaceBehind, expectedSaving: 1000, expectedProjected: 500, expectedDifference: -500,
		},
		{
			// No spending budgets: the band is a tenth of the income budgets.
			name: "with only an income budget the band is a tenth of it",
			input: &SavingMonthInput{
				ExpectedIncomeAmount: big.NewRat(4000, 1), ProjectedIncomeAmount: big.NewRat(4000, 1),
				ProjectedSpendingAmount: big.NewRat(300, 1), DayOfMonth: 20,
			},
			expectedPace: models.SavingPaceOnTrack, expectedSaving: 4000, expectedProjected: 3700, expectedDifference: -300,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projection := ProjectSavingMonth(testCase.input)
			if projection.SavingPace != testCase.expectedPace {
				t.Fatalf("the pace is %s, not %s", projection.SavingPace, testCase.expectedPace)
			}
			for _, compared := range []struct {
				what     string
				got      *big.Rat
				expected int64
			}{
				{"expected saving", projection.ExpectedSavingAmount, testCase.expectedSaving},
				{"projected saving", projection.ProjectedSavingAmount, testCase.expectedProjected},
				{"difference", projection.SavingDifferenceAmount, testCase.expectedDifference},
			} {
				if compared.got.Cmp(big.NewRat(compared.expected, 1)) != 0 {
					t.Errorf("the %s is %s, not %d", compared.what, compared.got.FloatString(2), compared.expected)
				}
			}
		})
	}
}
