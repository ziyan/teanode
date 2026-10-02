package agent

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// spendingMonths is count months of one phase, each with the same budget
// and spending (nil spending for months to come).
func spendingMonths(count int, phase BudgetMonthPhase, budget, spending int64) []*SpendingCategoryYearMonth {
	months := make([]*SpendingCategoryYearMonth, 0, count)
	for range count {
		month := &SpendingCategoryYearMonth{BudgetMonthPhase: phase, BudgetAmount: big.NewRat(budget, 1)}
		if phase != BudgetMonthPhaseToCome {
			month.SpendingAmount = big.NewRat(spending, 1)
		}
		months = append(months, month)
	}
	return months
}

func TestProjectSpendingCategoryYear(t *testing.T) {
	inProgress := func(dayOfMonth int, budget, spending, projected, budgetToDate int64) *SpendingCategoryYearMonth {
		return &SpendingCategoryYearMonth{
			BudgetMonthPhase: BudgetMonthPhaseInProgress, BudgetAmount: big.NewRat(budget, 1), SpendingAmount: big.NewRat(spending, 1),
			ProjectedAmount: big.NewRat(projected, 1), BudgetToDateAmount: big.NewRat(budgetToDate, 1), DayOfMonth: dayOfMonth,
		}
	}
	for _, testCase := range []struct {
		name                 string
		input                *SpendingCategoryYearInput
		expectedBudget       int64
		expectedBudgetToDate int64
		expectedSpending     int64
		expectedProjected    int64
		expectedMonthCount   int
		expectedPace         models.BudgetPace
	}{
		{
			// Raised from 400 to 600 in July: each half of the year at the
			// budget it had then, not twelve months of the latest.
			name: "a budget raised mid-year counts each month at what it was then",
			input: &SpendingCategoryYearInput{
				Months:     append(spendingMonths(6, BudgetMonthPhaseOver, 400, 350), spendingMonths(6, BudgetMonthPhaseOver, 600, 350)...),
				IsYearOver: true,
			},
			expectedBudget: 6000, expectedBudgetToDate: 6000, expectedSpending: 4200, expectedProjected: 4200, expectedMonthCount: 12,
			expectedPace: models.BudgetPaceUnder,
		},
		{
			// Ended in May: January to April are the year's budget, and
			// nothing is carried on into months that have none.
			name: "a budget ended mid-year counts only the months it was in force",
			input: &SpendingCategoryYearInput{
				Months: spendingMonths(4, BudgetMonthPhaseOver, 200, 190),
			},
			expectedBudget: 800, expectedBudgetToDate: 800, expectedSpending: 760, expectedProjected: 760, expectedMonthCount: 4,
			expectedPace: models.BudgetPaceOnTrack,
		},
		{
			// Eight months at 500, September heading for 500: the three
			// months to come at that average land well past the budget.
			name: "the months to come carry on at the average of the months begun",
			input: &SpendingCategoryYearInput{
				Months: append(append(spendingMonths(8, BudgetMonthPhaseOver, 400, 500), inProgress(15, 400, 200, 500, 200)),
					spendingMonths(3, BudgetMonthPhaseToCome, 400, 0)...),
			},
			expectedBudget: 4800, expectedBudgetToDate: 3400, expectedSpending: 4200, expectedProjected: 6000, expectedMonthCount: 12,
			expectedPace: models.BudgetPaceAtRisk,
		},
		{
			name: "with no month begun the months to come count at their budget",
			input: &SpendingCategoryYearInput{
				Months: spendingMonths(12, BudgetMonthPhaseToCome, 100, 0),
			},
			expectedBudget: 1200, expectedBudgetToDate: 0, expectedSpending: 0, expectedProjected: 1200, expectedMonthCount: 12,
			expectedPace: models.BudgetPaceOnTrack,
		},
		{
			// January's first days projected across the year are noise,
			// as a month's first week is: January counts at its budget.
			name: "a hot first week of the year is not heading over",
			input: &SpendingCategoryYearInput{
				Months: append([]*SpendingCategoryYearMonth{inProgress(5, 100, 90, 1000, 16)}, spendingMonths(11, BudgetMonthPhaseToCome, 100, 0)...),
			},
			expectedBudget: 1200, expectedBudgetToDate: 16, expectedSpending: 90, expectedProjected: 1200, expectedMonthCount: 12,
			expectedPace: models.BudgetPaceOnTrack,
		},
		{
			// A budget that starts in October, one dinner on its first day:
			// the month's straight line heads for 3100, and carried on into
			// November and December it would read as at_risk on a day the
			// month itself is on track.
			name: "the first week of a budget that starts late in the year is not heading over",
			input: &SpendingCategoryYearInput{
				Months: append([]*SpendingCategoryYearMonth{inProgress(1, 400, 100, 3100, 13)}, spendingMonths(2, BudgetMonthPhaseToCome, 400, 0)...),
			},
			expectedBudget: 1200, expectedBudgetToDate: 13, expectedSpending: 100, expectedProjected: 1200, expectedMonthCount: 3,
			expectedPace: models.BudgetPaceOnTrack,
		},
		{
			// Past the budget in its first week: the month counts at what
			// was spent, and the year is no worse off than the month.
			name: "a first week already past the month's budget counts at its spending",
			input: &SpendingCategoryYearInput{
				Months: append([]*SpendingCategoryYearMonth{inProgress(3, 400, 500, 5000, 39)}, spendingMonths(2, BudgetMonthPhaseToCome, 400, 0)...),
			},
			expectedBudget: 1200, expectedBudgetToDate: 39, expectedSpending: 500, expectedProjected: 1500, expectedMonthCount: 3,
			expectedPace: models.BudgetPaceOnTrack,
		},
		{
			// The year settles a week after the budget's first month began:
			// on the tenth of October the straight line counts again.
			name: "a budget that starts late in the year settles after its first week",
			input: &SpendingCategoryYearInput{
				Months: append([]*SpendingCategoryYearMonth{inProgress(10, 400, 300, 930, 129)}, spendingMonths(2, BudgetMonthPhaseToCome, 400, 0)...),
			},
			expectedBudget: 1200, expectedBudgetToDate: 129, expectedSpending: 300, expectedProjected: 2790, expectedMonthCount: 3,
			expectedPace: models.BudgetPaceAtRisk,
		},
		{
			name: "spending past the year's budget is over",
			input: &SpendingCategoryYearInput{
				Months: append(spendingMonths(3, BudgetMonthPhaseOver, 100, 500), spendingMonths(9, BudgetMonthPhaseToCome, 100, 0)...),
			},
			expectedBudget: 1200, expectedBudgetToDate: 300, expectedSpending: 1500, expectedProjected: 6000, expectedMonthCount: 12,
			expectedPace: models.BudgetPaceOver,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projection := ProjectSpendingCategoryYear(testCase.input)
			if projection.BudgetPace != testCase.expectedPace {
				t.Errorf("the pace is %s, not %s", projection.BudgetPace, testCase.expectedPace)
			}
			if projection.BudgetedMonthCount != testCase.expectedMonthCount {
				t.Errorf("%d months budgeted, not %d", projection.BudgetedMonthCount, testCase.expectedMonthCount)
			}
			for _, compared := range []struct {
				what     string
				got      *big.Rat
				expected int64
			}{
				{"budget", projection.BudgetAmount, testCase.expectedBudget},
				{"budget to date", projection.BudgetToDateAmount, testCase.expectedBudgetToDate},
				{"spending", projection.SpendingAmount, testCase.expectedSpending},
				{"projection", projection.ProjectedAmount, testCase.expectedProjected},
			} {
				if compared.got.Cmp(big.NewRat(compared.expected, 1)) != 0 {
					t.Errorf("the %s is %s, not %d", compared.what, compared.got.FloatString(2), compared.expected)
				}
			}
		})
	}
}

// An income budget raised in July, a short August and a September whose
// pay has not landed: the year adds each month at what was expected then,
// expects September's share by its days, and is behind.
func TestProjectIncomeCategoryYear(t *testing.T) {
	months := []*IncomeCategoryYearMonth{}
	for monthNumber := 1; monthNumber <= 12; monthNumber++ {
		budget := int64(3000)
		if monthNumber >= 7 {
			budget = 3500
		}
		month := &IncomeCategoryYearMonth{BudgetAmount: big.NewRat(budget, 1)}
		switch {
		case monthNumber <= 8:
			month.BudgetMonthPhase = BudgetMonthPhaseOver
			month.IncomeAmount = big.NewRat(budget, 1)
			if monthNumber == 8 {
				month.IncomeAmount = new(big.Rat)
			}
		case monthNumber == 9:
			month.BudgetMonthPhase = BudgetMonthPhaseInProgress
			month.IncomeAmount, month.ProjectedAmount, month.ExpectedByTodayAmount = new(big.Rat), big.NewRat(3500, 1), big.NewRat(1750, 1)
		default:
			month.BudgetMonthPhase = BudgetMonthPhaseToCome
		}
		months = append(months, month)
	}
	projection := ProjectIncomeCategoryYear(&IncomeCategoryYearInput{Months: months})
	for _, compared := range []struct {
		what     string
		got      *big.Rat
		expected int64
	}{
		{"expected income", projection.BudgetAmount, 39000},
		{"income", projection.IncomeAmount, 21500},
		{"income expected by today", projection.ExpectedByTodayAmount, 26750},
		{"projection", projection.ProjectedAmount, 35500},
	} {
		if compared.got.Cmp(big.NewRat(compared.expected, 1)) != 0 {
			t.Errorf("the %s is %s, not %d", compared.what, compared.got.FloatString(2), compared.expected)
		}
	}
	if projection.IncomePace != models.IncomePaceBehind || projection.BudgetedMonthCount != 12 {
		t.Errorf("behind, over twelve budgeted months: %+v", projection)
	}
}

// Two spending categories of the same name keep one order, by id, however
// the map they were gathered in handed them over.
func TestSortYearBudgetRowsBreaksTiesById(t *testing.T) {
	budgetStatus := &models.BudgetStatus{
		SpendingCategories: []*models.SpendingCategoryBudgetStatus{
			{SpendingCategoryID: "category-b", SpendingCategoryName: "Travel"},
			{SpendingCategoryID: "category-c", SpendingCategoryName: "Books"},
			{SpendingCategoryID: "category-a", SpendingCategoryName: "Travel"},
		},
		IncomeCategories: []*models.IncomeCategoryBudgetStatus{
			{SpendingCategoryID: "income-b", SpendingCategoryName: "Pay"},
			{SpendingCategoryID: "income-a", SpendingCategoryName: "Pay"},
		},
	}
	sortYearBudgetRows(budgetStatus)
	spendingIds := []string{}
	for _, row := range budgetStatus.SpendingCategories {
		spendingIds = append(spendingIds, row.SpendingCategoryID)
	}
	if fmt.Sprint(spendingIds) != "[category-c category-a category-b]" {
		t.Errorf("the spending rows are %v", spendingIds)
	}
	if budgetStatus.IncomeCategories[0].SpendingCategoryID != "income-a" {
		t.Errorf("the income rows start with %s", budgetStatus.IncomeCategories[0].SpendingCategoryID)
	}
}

// The years a year's budgets may be asked for, on the first of October
// 2026: 1900 to 2036. Year zero parses as a year and is refused.
func TestIsBudgetYear(t *testing.T) {
	for year, expected := range map[string]bool{
		"0000": false, "1899": false, "1900": true, "2026": true, "2036": true, "2037": false, "twenty": false,
	} {
		if IsBudgetYear(year, "2026-10-01") != expected {
			t.Errorf("the year %q is a budget year: %t", year, !expected)
		}
		if _, err := newBudgetYear(year, "2026-10-01"); (err == nil) != expected {
			t.Errorf("newBudgetYear(%q): %v", year, err)
		}
	}
}

// A salary expected from October, its pay not yet in: on the third its
// year is a few days old, not nine months, and is no more behind than its
// month; a week and more into October it is.
func TestProjectIncomeCategoryYearSettlesFromItsFirstMonth(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		dayOfMonth   int
		expectedPace models.IncomePace
	}{
		{"on the third of the first month", 3, models.IncomePaceOnTrack},
		{"on the tenth of the first month", 10, models.IncomePaceBehind},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			months := []*IncomeCategoryYearMonth{{
				BudgetMonthPhase: BudgetMonthPhaseInProgress, BudgetAmount: big.NewRat(3000, 1), IncomeAmount: new(big.Rat),
				ProjectedAmount: big.NewRat(3000, 1), ExpectedByTodayAmount: big.NewRat(3000*int64(testCase.dayOfMonth), 31),
				DayOfMonth: testCase.dayOfMonth,
			}}
			for range 2 {
				months = append(months, &IncomeCategoryYearMonth{BudgetMonthPhase: BudgetMonthPhaseToCome, BudgetAmount: big.NewRat(3000, 1)})
			}
			monthProjection := ProjectIncomeCategoryMonth(&IncomeCategoryMonthInput{
				BudgetAmount: big.NewRat(3000, 1), IncomeAmount: new(big.Rat), DayOfMonth: testCase.dayOfMonth, DaysInMonth: 31,
			})
			projection := ProjectIncomeCategoryYear(&IncomeCategoryYearInput{Months: months})
			if projection.IncomePace != testCase.expectedPace || monthProjection.IncomePace != testCase.expectedPace {
				t.Errorf("the year is %s and the month %s, not %s", projection.IncomePace, monthProjection.IncomePace, testCase.expectedPace)
			}
		})
	}
}

// The year's budgets read from the database: groceries raised in July,
// dining ended in May (its June dinner counts toward no budget), the pay
// expected all year, and the saving summary of the same months; then the
// same year once it is over, with nothing left to project.
func TestYearBudgetStatusAndSavingSummary(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	agentId := fixture.agent.ID
	groceriesId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryGroceries)
	diningId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryDining)
	incomeId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryIncome)
	added := []finance.Transaction{}
	categoryByIdentifier := map[string]string{}
	// A different grocer each month, so no repeat charge is expected and
	// September's projection is its pace alone.
	for _, month := range []string{"01", "02", "03", "04", "05", "06", "07", "08"} {
		grocer := "grocer-" + month
		added = append(added, inventedTransaction(grocer, "2026-"+month+"-12", "-300.00", "GROCER "+month, "Invented Grocer "+month, ""))
		categoryByIdentifier[grocer] = groceriesId
		pay := "pay-" + month
		added = append(added, inventedTransaction(pay, "2026-"+month+"-01", "3000.00", "PAYROLL EXAMPLE CO", "", ""))
		categoryByIdentifier[pay] = incomeId
	}
	added = append(added, inventedTransaction("grocer-09", "2026-09-10", "-150.00", "GROCER 09", "Invented Grocer 09", ""))
	categoryByIdentifier["grocer-09"] = groceriesId
	for identifier, dinner := range map[string][2]string{"dinner-01": {"2026-01-20", "-100.00"}, "dinner-02": {"2026-02-20", "-250.00"}, "dinner-06": {"2026-06-20", "-80.00"}} {
		added = append(added, inventedTransaction(identifier, dinner[0], dinner[1], "BISTRO "+identifier, "Invented Bistro "+identifier, ""))
		categoryByIdentifier[identifier] = diningId
	}
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: added})

	var status, pastStatus *models.BudgetStatus
	var summary, pastSummary *models.SavingSummary
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{Limit: 100})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			if _, err := tx.SetTransactionCategorization(agentId, financeTransaction.ID, categoryByIdentifier[financeTransaction.ProviderTransactionID], models.CategorizedByPerson, nil); err != nil {
				t.Fatalf("SetTransactionCategorization: %s", err)
			}
		}
		for _, budget := range []*models.Budget{
			{SpendingCategoryID: groceriesId, MonthlyAmount: "400", EffectiveFrom: "2026-01"},
			{SpendingCategoryID: groceriesId, MonthlyAmount: "600", EffectiveFrom: "2026-07"},
			{SpendingCategoryID: diningId, MonthlyAmount: "200", EffectiveFrom: "2026-01"},
			{SpendingCategoryID: diningId, MonthlyAmount: "0", EffectiveFrom: "2026-05"},
			{SpendingCategoryID: incomeId, MonthlyAmount: "3000", EffectiveFrom: "2026-01"},
		} {
			budget.AgentID, budget.CurrencyCode = agentId, "USD"
			if _, err := tx.SetBudget(budget); err != nil {
				t.Fatalf("SetBudget: %s", err)
			}
		}
		if status, err = YearBudgetStatus(t.Context(), tx, nil, agentId, "2026", "2026-09-15"); err != nil {
			t.Fatalf("YearBudgetStatus: %s", err)
		}
		if summary, err = YearSavingSummary(t.Context(), tx, nil, agentId, "2026", "2026-09-15", "USD"); err != nil {
			t.Fatalf("YearSavingSummary: %s", err)
		}
		if pastStatus, err = YearBudgetStatus(t.Context(), tx, nil, agentId, "2026", "2027-01-10"); err != nil {
			t.Fatalf("YearBudgetStatus: %s", err)
		}
		if pastSummary, err = YearSavingSummary(t.Context(), tx, nil, agentId, "2026", "2027-01-10", "USD"); err != nil {
			t.Fatalf("YearSavingSummary: %s", err)
		}
	})

	if status.Year != "2026" || status.Month != "" || status.MonthsElapsedCount != 9 || status.DayOfYear != 258 || status.DaysInYear != 365 || status.AsOf != "2026-09-15" {
		t.Errorf("the year to the fifteenth of September: %+v", status)
	}
	rowsById := map[string]*models.SpendingCategoryBudgetStatus{}
	for _, row := range status.SpendingCategories {
		rowsById[row.SpendingCategoryID] = row
	}
	groceries, dining := rowsById[groceriesId], rowsById[diningId]
	if len(status.SpendingCategories) != 2 || groceries == nil || dining == nil {
		t.Fatalf("groceries and dining: %+v", status.SpendingCategories)
	}
	// Six months at 400 and six at 600; to date the six, July, August and
	// half of September; September at its pace (150 in fifteen days) heads
	// for 300, and the nine months begun average 300 for the three to come.
	for what, compared := range map[string][2]string{
		"groceries budget":         {groceries.BudgetAmount, "6000.0000"},
		"groceries budget to date": {groceries.BudgetToDateAmount, "3900.0000"},
		"groceries spending":       {groceries.SpendingAmount, "2550.0000"},
		"groceries projection":     {groceries.ProjectedAmount, "3600.0000"},
		"dining budget":            {dining.BudgetAmount, "800.0000"},
		"dining spending":          {dining.SpendingAmount, "350.0000"},
		"dining projection":        {dining.ProjectedAmount, "350.0000"},
	} {
		if compared[0] != compared[1] {
			t.Errorf("the %s is %s, want %s", what, compared[0], compared[1])
		}
	}
	if groceries.BudgetedMonthCount != 12 || dining.BudgetedMonthCount != 4 || groceries.BudgetPace != models.BudgetPaceUnder {
		t.Errorf("twelve budgeted months of groceries, four of dining: %+v %+v", groceries, dining)
	}
	// Dining's budget ended at zero in May: January to April.
	if groceries.FirstBudgetedMonth != "2026-01" || groceries.LastBudgetedMonth != "2026-12" ||
		dining.FirstBudgetedMonth != "2026-01" || dining.LastBudgetedMonth != "2026-04" {
		t.Errorf("groceries budgeted %s to %s, dining %s to %s", groceries.FirstBudgetedMonth, groceries.LastBudgetedMonth,
			dining.FirstBudgetedMonth, dining.LastBudgetedMonth)
	}
	if len(status.IncomeCategories) != 1 {
		t.Fatalf("the pay is listed apart: %+v", status.IncomeCategories)
	}
	pay := status.IncomeCategories[0]
	if pay.BudgetAmount != "36000.0000" || pay.IncomeAmount != "24000.0000" || pay.ExpectedByTodayAmount != "25500.0000" ||
		pay.ProjectedAmount != "36000.0000" || pay.IncomePace != models.IncomePaceOnTrack {
		t.Errorf("eight pays in, September's still expected: %+v", pay)
	}

	// All the year's spending counts toward the saving, the June dinner
	// too: the nine months begun head for 3130 between them.
	for what, compared := range map[string][2]string{
		"expected income":    {summary.ExpectedIncomeAmount, "36000.0000"},
		"expected spending":  {summary.ExpectedSpendingAmount, "6800.0000"},
		"expected saving":    {summary.ExpectedSavingAmount, "29200.0000"},
		"income":             {summary.IncomeAmount, "24000.0000"},
		"spending":           {summary.SpendingAmount, "2980.0000"},
		"projected income":   {summary.ProjectedIncomeAmount, "36000.0000"},
		"projected spending": {summary.ProjectedSpendingAmount, "4173.3333"},
	} {
		if compared[0] != compared[1] {
			t.Errorf("the year's %s is %s, want %s", what, compared[0], compared[1])
		}
	}
	if summary.IncomeBudgetCount != 1 || summary.SpendingBudgetCount != 2 || summary.Year != "2026" || summary.MonthsElapsedCount != 9 {
		t.Errorf("the year's saving: %+v", summary)
	}
	// Budgets in every month: the year counts all twelve, as it always did.
	if summary.BudgetedMonthCount != 12 || summary.BudgetedMonthsElapsedCount != 9 || len(summary.BudgetedMonths) != 12 ||
		summary.BudgetedMonths[0] != "2026-01" || summary.BudgetedMonths[11] != "2026-12" {
		t.Errorf("every month of the year budgeted, nine begun: %+v", summary)
	}

	if pastStatus.MonthsElapsedCount != 12 || pastStatus.DayOfYear != 365 || pastStatus.AsOf != "2026-12-31" {
		t.Errorf("the year once it is over: %+v", pastStatus)
	}
	for _, row := range pastStatus.SpendingCategories {
		if row.ProjectedAmount != row.SpendingAmount || row.BudgetToDateAmount != row.BudgetAmount {
			t.Errorf("a year that is over is its own figures: %+v", row)
		}
	}
	if pastSummary.ProjectedSpendingAmount != pastSummary.SpendingAmount || pastSummary.ProjectedIncomeAmount != pastSummary.IncomeAmount {
		t.Errorf("a year that is over projects nothing: %+v", pastSummary)
	}
}

// Budgets that begin in September: the year's saving counts September
// onward only, expected, actual and projected alike, so the pay and the
// groceries of January to August are not set against four months of
// budgets. Before the budgets are set, the year has none at all and
// counts every month's income and spending with nothing expected.
func TestYearSavingSummaryCountsOnlyBudgetedMonths(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	agentId := fixture.agent.ID
	groceriesId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryGroceries)
	incomeId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryIncome)
	added := []finance.Transaction{}
	categoryByIdentifier := map[string]string{}
	// A different grocer each month, so no repeat charge is expected and
	// October's projection is its pace alone.
	for _, month := range []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10"} {
		grocer := "grocer-" + month
		added = append(added, inventedTransaction(grocer, "2026-"+month+"-12", "-300.00", "GROCER "+month, "Invented Grocer "+month, ""))
		categoryByIdentifier[grocer] = groceriesId
		pay := "pay-" + month
		added = append(added, inventedTransaction(pay, "2026-"+month+"-01", "3000.00", "PAYROLL EXAMPLE CO", "", ""))
		categoryByIdentifier[pay] = incomeId
	}
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: added})

	var unbudgeted, budgeted, august, september *models.SavingSummary
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{Limit: 100})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			if _, err := tx.SetTransactionCategorization(agentId, financeTransaction.ID, categoryByIdentifier[financeTransaction.ProviderTransactionID], models.CategorizedByPerson, nil); err != nil {
				t.Fatalf("SetTransactionCategorization: %s", err)
			}
		}
		if unbudgeted, err = YearSavingSummary(t.Context(), tx, nil, agentId, "2026", "2026-10-15", "USD"); err != nil {
			t.Fatalf("YearSavingSummary: %s", err)
		}
		for _, budget := range []*models.Budget{
			{SpendingCategoryID: groceriesId, MonthlyAmount: "400", EffectiveFrom: "2026-09"},
			{SpendingCategoryID: incomeId, MonthlyAmount: "3000", EffectiveFrom: "2026-09"},
		} {
			budget.AgentID, budget.CurrencyCode = agentId, "USD"
			if _, err := tx.SetBudget(budget); err != nil {
				t.Fatalf("SetBudget: %s", err)
			}
		}
		if budgeted, err = YearSavingSummary(t.Context(), tx, nil, agentId, "2026", "2026-10-15", "USD"); err != nil {
			t.Fatalf("YearSavingSummary: %s", err)
		}
		if august, err = SavingSummary(t.Context(), tx, nil, agentId, "2026-08", "2026-10-15", "USD"); err != nil {
			t.Fatalf("SavingSummary: %s", err)
		}
		if september, err = SavingSummary(t.Context(), tx, nil, agentId, "2026-09", "2026-10-15", "USD"); err != nil {
			t.Fatalf("SavingSummary: %s", err)
		}
	})

	// No budget in any month: every month's income and spending, nothing
	// expected, and no difference or pace against it.
	if unbudgeted.BudgetedMonthCount != 0 || len(unbudgeted.BudgetedMonths) != 0 || unbudgeted.BudgetedMonthsElapsedCount != 0 {
		t.Errorf("no month budgeted: %+v", unbudgeted)
	}
	for what, compared := range map[string][2]string{
		"expected saving":   {unbudgeted.ExpectedSavingAmount, "0.0000"},
		"income":            {unbudgeted.IncomeAmount, "30000.0000"},
		"spending":          {unbudgeted.SpendingAmount, "3000.0000"},
		"saving":            {unbudgeted.SavingAmount, "27000.0000"},
		"saving difference": {unbudgeted.SavingDifferenceAmount, "0.0000"},
	} {
		if compared[0] != compared[1] {
			t.Errorf("the unbudgeted year's %s is %s, want %s", what, compared[0], compared[1])
		}
	}
	if unbudgeted.SavingPace != models.SavingPaceOnTrack {
		t.Errorf("a year with nothing expected has no pace: %s", unbudgeted.SavingPace)
	}

	if len(budgeted.BudgetedMonths) != 4 || budgeted.BudgetedMonths[0] != "2026-09" || budgeted.BudgetedMonths[3] != "2026-12" ||
		budgeted.BudgetedMonthCount != 4 || budgeted.BudgetedMonthsElapsedCount != 2 {
		t.Errorf("September to December budgeted, two of them begun: %+v", budgeted)
	}
	// September and October so far: two pays and two grocers. October at
	// its pace (300 in fifteen days) heads for 620, and November and
	// December carry on at the average of September and October.
	for what, compared := range map[string][2]string{
		"expected income":    {budgeted.ExpectedIncomeAmount, "12000.0000"},
		"expected spending":  {budgeted.ExpectedSpendingAmount, "1600.0000"},
		"expected saving":    {budgeted.ExpectedSavingAmount, "10400.0000"},
		"income":             {budgeted.IncomeAmount, "6000.0000"},
		"spending":           {budgeted.SpendingAmount, "600.0000"},
		"saving":             {budgeted.SavingAmount, "5400.0000"},
		"projected income":   {budgeted.ProjectedIncomeAmount, "12000.0000"},
		"projected spending": {budgeted.ProjectedSpendingAmount, "1840.0000"},
		"projected saving":   {budgeted.ProjectedSavingAmount, "10160.0000"},
		"saving difference":  {budgeted.SavingDifferenceAmount, "-240.0000"},
	} {
		if compared[0] != compared[1] {
			t.Errorf("the budgeted months' %s is %s, want %s", what, compared[0], compared[1])
		}
	}
	if budgeted.SavingPace != models.SavingPaceBehind {
		t.Errorf("240 short of the expected saving, past a tenth of the spending budgets, is behind: %s", budgeted.SavingPace)
	}

	// A month says itself when it has a budget, and nothing when not.
	if august.BudgetedMonthCount != 0 || len(august.BudgetedMonths) != 0 || september.BudgetedMonthCount != 1 ||
		len(september.BudgetedMonths) != 1 || september.BudgetedMonths[0] != "2026-09" || september.BudgetedMonthsElapsedCount != 1 {
		t.Errorf("August unbudgeted, September budgeted: %+v %+v", august, september)
	}
}

// Budgets that start in October, read from the database: one dinner on the
// first and the pay not yet in on the third. The year is as young as its
// first month, so it says on track where the month does, and its
// projection is the three budgeted months rather than a month of dinners
// carried on into November and December.
func TestYearBudgetStatusSettlesFromTheFirstBudgetedMonth(t *testing.T) {
	fixture := newFinanceFixture(t, "")
	agentId := fixture.agent.ID
	diningId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryDining)
	incomeId := fixture.spendingCategoryIdNamed(t, agentId, finance.SpendingCategoryIncome)
	fixture.applySync(t, &finance.SyncResult{Accounts: []finance.Account{inventedAccount()}, Added: []finance.Transaction{
		inventedTransaction("dinner-10", "2026-10-01", "-100.00", "BISTRO 10", "Invented Bistro", ""),
	}})

	statusByToday := map[string]*models.BudgetStatus{}
	monthStatusByToday := map[string]*models.BudgetStatus{}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		page, err := tx.ListFinanceTransactions(agentId, &db.FinanceTransactionFilter{Limit: 10})
		if err != nil {
			t.Fatalf("ListFinanceTransactions: %s", err)
		}
		for _, financeTransaction := range page.FinanceTransactions {
			if _, err := tx.SetTransactionCategorization(agentId, financeTransaction.ID, diningId, models.CategorizedByPerson, nil); err != nil {
				t.Fatalf("SetTransactionCategorization: %s", err)
			}
		}
		for _, budget := range []*models.Budget{
			{SpendingCategoryID: diningId, MonthlyAmount: "400", EffectiveFrom: "2026-10"},
			{SpendingCategoryID: incomeId, MonthlyAmount: "3000", EffectiveFrom: "2026-10"},
		} {
			budget.AgentID, budget.CurrencyCode = agentId, "USD"
			if _, err := tx.SetBudget(budget); err != nil {
				t.Fatalf("SetBudget: %s", err)
			}
		}
		for _, today := range []string{"2026-10-01", "2026-10-03"} {
			if statusByToday[today], err = YearBudgetStatus(t.Context(), tx, nil, agentId, "2026", today); err != nil {
				t.Fatalf("YearBudgetStatus: %s", err)
			}
			if monthStatusByToday[today], err = BudgetStatus(t.Context(), tx, nil, agentId, "2026-10", today); err != nil {
				t.Fatalf("BudgetStatus: %s", err)
			}
		}
	})

	for today, status := range statusByToday {
		monthStatus := monthStatusByToday[today]
		if len(status.SpendingCategories) != 1 || len(status.IncomeCategories) != 1 ||
			len(monthStatus.SpendingCategories) != 1 || len(monthStatus.IncomeCategories) != 1 {
			t.Fatalf("on %s, dining and the pay: %+v %+v", today, status, monthStatus)
		}
		dining, monthDining := status.SpendingCategories[0], monthStatus.SpendingCategories[0]
		if dining.ProjectedAmount != "1200.0000" || dining.BudgetPace != models.BudgetPaceOnTrack || monthDining.BudgetPace != models.BudgetPaceOnTrack {
			t.Errorf("on %s the year's dining heads for %s and is %s, the month's is %s", today, dining.ProjectedAmount, dining.BudgetPace, monthDining.BudgetPace)
		}
		pay, monthPay := status.IncomeCategories[0], monthStatus.IncomeCategories[0]
		if pay.IncomePace != models.IncomePaceOnTrack || monthPay.IncomePace != models.IncomePaceOnTrack {
			t.Errorf("on %s the year's pay is %s, the month's %s", today, pay.IncomePace, monthPay.IncomePace)
		}
	}
}
