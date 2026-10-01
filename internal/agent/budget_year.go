package agent

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// A year's budgets and saving, built from its months: each month that has
// begun is the month's own BudgetStatus or SavingSummary, so a year never
// says a different number for a month than the month does, and each month
// still to come is the budgets in force for it as they stand today.

// budgetYear is where a year stands on the day it is computed for.
type budgetYear struct {
	year               string
	asOf               time.Time
	isYearOver         bool
	monthsElapsedCount int
	dayOfYear          int
	daysInYear         int
}

// newBudgetYear is a year ("2006") as of today ("2006-01-02"): the as-of
// day is today within the year, its last day once it is over, and its
// first before it begins.
func newBudgetYear(year, today string) (*budgetYear, error) {
	yearStart, err := time.Parse("2006", year)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a year written 2006", db.ErrInvalidArguments, year)
	}
	todayDay, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a day written 2006-01-02", db.ErrInvalidArguments, today)
	}
	yearEnd := yearStart.AddDate(1, 0, -1)
	frame := &budgetYear{year: yearStart.Format("2006"), asOf: todayDay, isYearOver: todayDay.After(yearEnd), daysInYear: yearEnd.YearDay()}
	switch {
	case todayDay.Before(yearStart):
		frame.asOf = yearStart
	case frame.isYearOver:
		frame.asOf = yearEnd
		frame.monthsElapsedCount, frame.dayOfYear = 12, frame.daysInYear
	default:
		frame.monthsElapsedCount, frame.dayOfYear = int(todayDay.Month()), todayDay.YearDay()
	}
	return frame, nil
}

// month is the year's month by its number, from one, as "2006-01".
func (self *budgetYear) month(monthNumber int) string {
	return fmt.Sprintf("%s-%02d", self.year, monthNumber)
}

// phaseOf is where a month of the year stands on the as-of day.
func (self *budgetYear) phaseOf(monthNumber int) BudgetMonthPhase {
	switch {
	case monthNumber > self.monthsElapsedCount:
		return BudgetMonthPhaseToCome
	case monthNumber == self.monthsElapsedCount && !self.isYearOver:
		return BudgetMonthPhaseInProgress
	}
	return BudgetMonthPhaseOver
}

// budgetYearMonth is one month of the year for one budget, in the
// budget's currency that month: the month's own status row once it has
// begun, its budget alone for a month to come.
type budgetYearMonth struct {
	budgetMonthPhase BudgetMonthPhase
	currencyCode     string
	convertOn        string
	budgetAmount     string
	spendingRow      *models.SpendingCategoryBudgetStatus
	incomeRow        *models.IncomeCategoryBudgetStatus
}

// YearBudgetStatus is every spending category with a budget in any month
// of a year ("2006"), against the budgets of its months, as of today
// ("2006-01-02"). Each month that has begun is read with BudgetStatus and
// each month still to come with the budgets in force for it, then
// ProjectSpendingCategoryYear and ProjectIncomeCategoryYear add them up.
//
// A spending category is reported in the currency of its budget in the
// latest month begun (the first month to come for a budget that has not
// started). A month whose budget was in another currency is converted at
// the rate of that month's as-of day; one with no rate is left out, its
// spending or income reported as unconverted.
func YearBudgetStatus(ctx context.Context, tx db.Transaction, fetcher *rates.Fetcher, agentId, year, today string) (*models.BudgetStatus, error) {
	frame, err := newBudgetYear(year, today)
	if err != nil {
		return nil, err
	}
	asOf := frame.asOf.Format(time.DateOnly)
	budgetStatus := &models.BudgetStatus{
		AsOf: asOf, DayOfMonth: frame.asOf.Day(), DaysInMonth: frame.asOf.AddDate(0, 1, -frame.asOf.Day()).Day(),
		Year: frame.year, MonthsElapsedCount: frame.monthsElapsedCount, DayOfYear: frame.dayOfYear, DaysInYear: frame.daysInYear,
		SpendingCategories: []*models.SpendingCategoryBudgetStatus{}, IncomeCategories: []*models.IncomeCategoryBudgetStatus{},
	}
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		return nil, err
	}
	isIncome, nameById := map[string]bool{}, map[string]string{}
	for _, spendingCategory := range spendingCategories {
		isIncome[spendingCategory.ID] = spendingCategory.IsIncome
		nameById[spendingCategory.ID] = spendingCategory.SpendingCategoryName
	}

	monthsByCategory := map[string][]*budgetYearMonth{}
	for monthNumber := 1; monthNumber <= 12; monthNumber++ {
		month := frame.month(monthNumber)
		phase := frame.phaseOf(monthNumber)
		if phase == BudgetMonthPhaseToCome {
			budgets, err := tx.BudgetsForMonth(agentId, month)
			if err != nil {
				return nil, err
			}
			for _, budget := range budgets {
				monthsByCategory[budget.SpendingCategoryID] = append(monthsByCategory[budget.SpendingCategoryID], &budgetYearMonth{
					budgetMonthPhase: phase, currencyCode: budget.CurrencyCode, convertOn: asOf, budgetAmount: budget.MonthlyAmount,
				})
			}
			continue
		}
		monthStatus, err := BudgetStatus(ctx, tx, fetcher, agentId, month, today)
		if err != nil {
			return nil, err
		}
		for _, row := range monthStatus.SpendingCategories {
			monthsByCategory[row.SpendingCategoryID] = append(monthsByCategory[row.SpendingCategoryID], &budgetYearMonth{
				budgetMonthPhase: phase, currencyCode: row.CurrencyCode, convertOn: monthStatus.AsOf, budgetAmount: row.BudgetAmount, spendingRow: row,
			})
		}
		for _, row := range monthStatus.IncomeCategories {
			monthsByCategory[row.SpendingCategoryID] = append(monthsByCategory[row.SpendingCategoryID], &budgetYearMonth{
				budgetMonthPhase: phase, currencyCode: row.CurrencyCode, convertOn: monthStatus.AsOf, budgetAmount: row.BudgetAmount, incomeRow: row,
			})
		}
	}

	converter := rates.NewConverter(ctx, fetcher, tx)
	for spendingCategoryId, months := range monthsByCategory {
		currencyCode := yearCurrencyOf(months)
		if isIncome[spendingCategoryId] {
			row, err := incomeCategoryYearStatus(converter, frame, months, currencyCode)
			if err != nil {
				return nil, err
			}
			row.SpendingCategoryID, row.SpendingCategoryName = spendingCategoryId, nameById[spendingCategoryId]
			budgetStatus.IncomeCategories = append(budgetStatus.IncomeCategories, row)
			continue
		}
		row, err := spendingCategoryYearStatus(converter, frame, months, currencyCode)
		if err != nil {
			return nil, err
		}
		row.SpendingCategoryID, row.SpendingCategoryName = spendingCategoryId, nameById[spendingCategoryId]
		budgetStatus.SpendingCategories = append(budgetStatus.SpendingCategories, row)
	}
	sort.SliceStable(budgetStatus.SpendingCategories, func(left, right int) bool {
		return budgetStatus.SpendingCategories[left].SpendingCategoryName < budgetStatus.SpendingCategories[right].SpendingCategoryName
	})
	sort.SliceStable(budgetStatus.IncomeCategories, func(left, right int) bool {
		return budgetStatus.IncomeCategories[left].SpendingCategoryName < budgetStatus.IncomeCategories[right].SpendingCategoryName
	})
	return budgetStatus, nil
}

// yearCurrencyOf is the currency a budget's year is reported in: its
// budget's in the latest month begun, or in the first month to come when
// none has begun. months are in the year's order.
func yearCurrencyOf(months []*budgetYearMonth) string {
	currencyCode := ""
	for _, month := range months {
		if month.budgetMonthPhase != BudgetMonthPhaseToCome {
			currencyCode = month.currencyCode
		}
	}
	if currencyCode == "" && len(months) > 0 {
		currencyCode = months[0].currencyCode
	}
	return currencyCode
}

// yearAmount is an amount of one month in the year's currency, converted
// at the month's as-of day when the month's budget was in another one, and
// false when there is no rate for it.
func yearAmount(converter *rates.Converter, month *budgetYearMonth, amount, currencyCode string) (*big.Rat, bool, error) {
	if month.currencyCode == currencyCode {
		value, err := finance.ParseAmount(amount)
		return value, err == nil, err
	}
	return convertedAmount(converter, amount, month.currencyCode, currencyCode, month.convertOn)
}

// spendingCategoryYearStatus is one spending budget's year from its
// months, in currencyCode. The repeat charges are the month in progress's,
// the ones its projection, and so the year's, counts.
func spendingCategoryYearStatus(converter *rates.Converter, frame *budgetYear, months []*budgetYearMonth, currencyCode string) (*models.SpendingCategoryBudgetStatus, error) {
	unconverted := map[string]*big.Rat{}
	unconvertedDue := map[string]*big.Rat{}
	input := &SpendingCategoryYearInput{DayOfYear: frame.dayOfYear, IsYearOver: frame.isYearOver}
	dueAmount := new(big.Rat)
	dueCharges := []*models.ExpectedRepeatCharge{}
	for _, month := range months {
		yearMonth := &SpendingCategoryYearMonth{BudgetMonthPhase: month.budgetMonthPhase}
		budgetAmount, isConverted, err := yearAmount(converter, month, month.budgetAmount, currencyCode)
		if err != nil {
			return nil, err
		}
		row := month.spendingRow
		if row != nil {
			// What the month itself could not convert into its budget's
			// currency is reported in its own currency already.
			for _, currencyAmount := range row.UnconvertedSpending {
				addTo(unconverted, currencyAmount.CurrencyCode, currencyAmount.Amount)
			}
		}
		if !isConverted {
			if row != nil {
				addTo(unconverted, month.currencyCode, row.SpendingAmount)
			}
			continue
		}
		yearMonth.BudgetAmount = budgetAmount
		if row != nil {
			// The budget converted, so the rest of the month does at the
			// same day's rate.
			for _, field := range []struct {
				amount string
				target **big.Rat
			}{{row.SpendingAmount, &yearMonth.SpendingAmount}, {row.ProjectedAmount, &yearMonth.ProjectedAmount}, {row.BudgetToDateAmount, &yearMonth.BudgetToDateAmount}} {
				if *field.target, _, err = yearAmount(converter, month, field.amount, currencyCode); err != nil {
					return nil, err
				}
			}
			if month.budgetMonthPhase == BudgetMonthPhaseInProgress {
				due, _, err := yearAmount(converter, month, row.FixedChargesDueAmount, currencyCode)
				if err != nil {
					return nil, err
				}
				if due != nil {
					dueAmount.Add(dueAmount, due)
				}
				dueCharges = row.ExpectedRepeatCharges
				for _, currencyAmount := range row.UnconvertedFixedChargesDue {
					addTo(unconvertedDue, currencyAmount.CurrencyCode, currencyAmount.Amount)
				}
			}
		}
		input.Months = append(input.Months, yearMonth)
	}
	projection := ProjectSpendingCategoryYear(input)
	zero := finance.FormatAmount(new(big.Rat))
	return &models.SpendingCategoryBudgetStatus{
		BudgetAmount: finance.FormatAmount(projection.BudgetAmount), CurrencyCode: currencyCode,
		BudgetToDateAmount: finance.FormatAmount(projection.BudgetToDateAmount), BudgetedMonthCount: projection.BudgetedMonthCount,
		SpendingAmount: finance.FormatAmount(projection.SpendingAmount), SpendingBySameDayLastMonthAmount: zero,
		FixedChargesDueAmount: finance.FormatAmount(dueAmount), ExpectedRepeatCharges: dueCharges,
		ProjectedAmount: finance.FormatAmount(projection.ProjectedAmount), BudgetPace: projection.BudgetPace,
		UnconvertedSpending: currencyAmountsOf(unconverted), UnconvertedSpendingBySameDayLastMonth: []*models.CurrencyAmount{},
		UnconvertedFixedChargesDue: currencyAmountsOf(unconvertedDue),
	}, nil
}

// incomeCategoryYearStatus is one income budget's year from its months,
// in currencyCode.
func incomeCategoryYearStatus(converter *rates.Converter, frame *budgetYear, months []*budgetYearMonth, currencyCode string) (*models.IncomeCategoryBudgetStatus, error) {
	unconverted := map[string]*big.Rat{}
	input := &IncomeCategoryYearInput{DayOfYear: frame.dayOfYear, IsYearOver: frame.isYearOver}
	for _, month := range months {
		yearMonth := &IncomeCategoryYearMonth{BudgetMonthPhase: month.budgetMonthPhase}
		budgetAmount, isConverted, err := yearAmount(converter, month, month.budgetAmount, currencyCode)
		if err != nil {
			return nil, err
		}
		row := month.incomeRow
		if row != nil {
			for _, currencyAmount := range row.UnconvertedIncome {
				addTo(unconverted, currencyAmount.CurrencyCode, currencyAmount.Amount)
			}
		}
		if !isConverted {
			if row != nil {
				addTo(unconverted, month.currencyCode, row.IncomeAmount)
			}
			continue
		}
		yearMonth.BudgetAmount = budgetAmount
		if row != nil {
			for _, field := range []struct {
				amount string
				target **big.Rat
			}{{row.IncomeAmount, &yearMonth.IncomeAmount}, {row.ProjectedAmount, &yearMonth.ProjectedAmount}, {row.ExpectedByTodayAmount, &yearMonth.ExpectedByTodayAmount}} {
				if *field.target, _, err = yearAmount(converter, month, field.amount, currencyCode); err != nil {
					return nil, err
				}
			}
		}
		input.Months = append(input.Months, yearMonth)
	}
	projection := ProjectIncomeCategoryYear(input)
	return &models.IncomeCategoryBudgetStatus{
		BudgetAmount: finance.FormatAmount(projection.BudgetAmount), CurrencyCode: currencyCode, BudgetedMonthCount: projection.BudgetedMonthCount,
		IncomeAmount: finance.FormatAmount(projection.IncomeAmount), IncomeBySameDayLastMonthAmount: finance.FormatAmount(new(big.Rat)),
		ExpectedByTodayAmount: finance.FormatAmount(projection.ExpectedByTodayAmount),
		ProjectedAmount:       finance.FormatAmount(projection.ProjectedAmount), IncomePace: projection.IncomePace,
		UnconvertedIncome: currencyAmountsOf(unconverted), UnconvertedIncomeBySameDayLastMonth: []*models.CurrencyAmount{},
	}, nil
}

// YearSavingSummary is a year's saving ("2006") in one currency as of
// today ("2006-01-02"), built from its months: each month begun is its
// SavingSummary, and each month still to come expects what the budgets
// in force for it expect, converted at the as-of day's rate.
//
// The expected saving is every month's income budgets less its spending
// budgets. The projected income is the months begun as their summaries
// project them and each month to come at its income budgets; the projected
// spending is the months begun the same way and each month to come at
// their average (or at its spending budgets when no month has begun),
// the way ProjectSpendingCategoryYear carries a budget's year on. The
// counts are of the spending categories with a budget in any month.
func YearSavingSummary(ctx context.Context, tx db.Transaction, fetcher *rates.Fetcher, agentId, year, today, currencyCode string) (*models.SavingSummary, error) {
	frame, err := newBudgetYear(year, today)
	if err != nil {
		return nil, err
	}
	asOf := frame.asOf.Format(time.DateOnly)
	zero := finance.FormatAmount(new(big.Rat))
	summary := &models.SavingSummary{
		AsOf: asOf, DayOfMonth: frame.asOf.Day(), DaysInMonth: frame.asOf.AddDate(0, 1, -frame.asOf.Day()).Day(),
		Year: frame.year, MonthsElapsedCount: frame.monthsElapsedCount, DayOfYear: frame.dayOfYear, DaysInYear: frame.daysInYear,
		ReportingCurrencyCode: currencyCode,
		ExpectedIncomeAmount:  zero, ExpectedSpendingAmount: zero, ExpectedSavingAmount: zero,
		IncomeAmount: zero, SpendingAmount: zero, SavingAmount: zero,
		ProjectedIncomeAmount: zero, ProjectedSpendingAmount: zero, ProjectedSavingAmount: zero,
		SavingDifferenceAmount: zero, SavingPace: models.SavingPaceOnTrack, UnconvertedCurrencyCodes: []string{},
	}
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		return nil, err
	}
	isIncome := map[string]bool{}
	for _, spendingCategory := range spendingCategories {
		isIncome[spendingCategory.ID] = spendingCategory.IsIncome
	}
	isBudgetedIncome, isBudgetedSpending := map[string]bool{}, map[string]bool{}
	for monthNumber := 1; monthNumber <= 12; monthNumber++ {
		budgets, err := tx.BudgetsForMonth(agentId, frame.month(monthNumber))
		if err != nil {
			return nil, err
		}
		for _, budget := range budgets {
			if isIncome[budget.SpendingCategoryID] {
				isBudgetedIncome[budget.SpendingCategoryID] = true
			} else {
				isBudgetedSpending[budget.SpendingCategoryID] = true
			}
		}
	}
	summary.IncomeBudgetCount, summary.SpendingBudgetCount = len(isBudgetedIncome), len(isBudgetedSpending)
	if currencyCode == "" {
		return summary, nil
	}

	converter := rates.NewConverter(ctx, fetcher, tx)
	unconverted := map[string]bool{}
	expectedIncome, expectedSpending := new(big.Rat), new(big.Rat)
	income, spending, projectedIncome := new(big.Rat), new(big.Rat), new(big.Rat)
	toComeSpendingBudget := new(big.Rat)
	spendingFigures := []*big.Rat{}
	toComeCount := 0
	add := func(sum *big.Rat, amount string) error {
		value, err := finance.ParseAmount(amount)
		if err == nil {
			sum.Add(sum, value)
		}
		return err
	}
	for monthNumber := 1; monthNumber <= 12; monthNumber++ {
		month := frame.month(monthNumber)
		phase := frame.phaseOf(monthNumber)
		if phase == BudgetMonthPhaseToCome {
			toComeCount++
			budgets, err := tx.BudgetsForMonth(agentId, month)
			if err != nil {
				return nil, err
			}
			for _, budget := range budgets {
				amount, isConverted, err := convertedAmount(converter, budget.MonthlyAmount, budget.CurrencyCode, currencyCode, asOf)
				if err != nil {
					return nil, err
				}
				if !isConverted {
					unconverted[budget.CurrencyCode] = true
					continue
				}
				if isIncome[budget.SpendingCategoryID] {
					expectedIncome.Add(expectedIncome, amount)
					projectedIncome.Add(projectedIncome, amount)
				} else {
					expectedSpending.Add(expectedSpending, amount)
					toComeSpendingBudget.Add(toComeSpendingBudget, amount)
				}
			}
			continue
		}
		monthSummary, err := SavingSummary(ctx, tx, fetcher, agentId, month, today, currencyCode)
		if err != nil {
			return nil, err
		}
		for _, unconvertedCurrencyCode := range monthSummary.UnconvertedCurrencyCodes {
			unconverted[unconvertedCurrencyCode] = true
		}
		for _, pair := range []struct {
			sum    *big.Rat
			amount string
		}{
			{expectedIncome, monthSummary.ExpectedIncomeAmount}, {expectedSpending, monthSummary.ExpectedSpendingAmount},
			{income, monthSummary.IncomeAmount}, {spending, monthSummary.SpendingAmount}, {projectedIncome, monthSummary.ProjectedIncomeAmount},
		} {
			if err := add(pair.sum, pair.amount); err != nil {
				return nil, err
			}
		}
		// A month that is over has its own figures as its projection.
		figure, err := finance.ParseAmount(monthSummary.ProjectedSpendingAmount)
		if err != nil {
			return nil, err
		}
		spendingFigures = append(spendingFigures, figure)
	}
	projectedSpending := carriedOnYear(spendingFigures, toComeCount, toComeSpendingBudget)
	saving := ProjectSavingMonth(&SavingMonthInput{
		ExpectedIncomeAmount: expectedIncome, ExpectedSpendingAmount: expectedSpending,
		ProjectedIncomeAmount: projectedIncome, ProjectedSpendingAmount: projectedSpending,
		// The month's thresholds over the year: no behind in its first week.
		DayOfMonth: frame.dayOfYear, IsMonthOver: frame.isYearOver,
	})
	summary.ExpectedIncomeAmount = finance.FormatAmount(expectedIncome)
	summary.ExpectedSpendingAmount = finance.FormatAmount(expectedSpending)
	summary.ExpectedSavingAmount = finance.FormatAmount(saving.ExpectedSavingAmount)
	summary.IncomeAmount = finance.FormatAmount(income)
	summary.SpendingAmount = finance.FormatAmount(spending)
	summary.SavingAmount = finance.FormatAmount(new(big.Rat).Sub(income, spending))
	summary.ProjectedIncomeAmount = finance.FormatAmount(projectedIncome)
	summary.ProjectedSpendingAmount = finance.FormatAmount(projectedSpending)
	summary.ProjectedSavingAmount = finance.FormatAmount(saving.ProjectedSavingAmount)
	summary.SavingDifferenceAmount = finance.FormatAmount(saving.SavingDifferenceAmount)
	summary.SavingPace = saving.SavingPace
	summary.UnconvertedCurrencyCodes = sortedKeys(unconverted)
	return summary, nil
}
