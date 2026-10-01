package agent

import (
	"context"
	"math/big"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// SavingSummary is a month's saving ("2006-01") in one currency as of
// today ("2006-01-02", the person's local day): what its income and
// spending budgets expect, what income less spending has come to so far,
// and where it is heading. It reads BudgetStatus for the budgets and their
// projections, so the figures agree with the budgets shown beside it. A
// nil fetcher converts with the rates already stored.
//
// The expected saving is every income budget less every spending budget,
// each converted at the rate of the status's day. The actual saving counts
// all income and all spending the way cash flow does, budgeted or not,
// each day at its own rate; a currency with a day that has no rate is left
// out whole and named, as cash flow leaves it out.
func SavingSummary(ctx context.Context, tx db.Transaction, fetcher *rates.Fetcher, agentId, month, today, currencyCode string) (*models.SavingSummary, error) {
	budgetStatus, err := BudgetStatus(ctx, tx, fetcher, agentId, month, today)
	if err != nil {
		return nil, err
	}
	zero := finance.FormatAmount(new(big.Rat))
	summary := &models.SavingSummary{
		Month: budgetStatus.Month, AsOf: budgetStatus.AsOf, DayOfMonth: budgetStatus.DayOfMonth, DaysInMonth: budgetStatus.DaysInMonth,
		ReportingCurrencyCode: currencyCode, IncomeBudgetCount: len(budgetStatus.IncomeCategories), SpendingBudgetCount: len(budgetStatus.SpendingCategories),
		ExpectedIncomeAmount: zero, ExpectedSpendingAmount: zero, ExpectedSavingAmount: zero,
		IncomeAmount: zero, SpendingAmount: zero, SavingAmount: zero,
		ProjectedIncomeAmount: zero, ProjectedSpendingAmount: zero, ProjectedSavingAmount: zero,
		SavingDifferenceAmount: zero, SavingPace: models.SavingPaceOnTrack, UnconvertedCurrencyCodes: []string{},
	}
	if currencyCode == "" {
		return summary, nil
	}
	monthStart, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, err
	}
	todayDay, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, err
	}
	isMonthOver := todayDay.After(monthStart.AddDate(0, 1, -1))
	converter := rates.NewConverter(ctx, fetcher, tx)
	unconverted := map[string]bool{}

	// The budgets, and what each income budget still expects: its
	// projection less what came in, nothing once the month is over.
	expectedIncome, expectedSpending, stillExpectedIncome := new(big.Rat), new(big.Rat), new(big.Rat)
	for _, row := range budgetStatus.IncomeCategories {
		budgetAmount, isConverted, err := convertedAmount(converter, row.BudgetAmount, row.CurrencyCode, currencyCode, budgetStatus.AsOf)
		if err != nil {
			return nil, err
		}
		if !isConverted {
			unconverted[row.CurrencyCode] = true
			continue
		}
		expectedIncome.Add(expectedIncome, budgetAmount)
		projectedAmount, projectedErr := finance.ParseAmount(row.ProjectedAmount)
		incomeAmount, incomeErr := finance.ParseAmount(row.IncomeAmount)
		if projectedErr != nil || incomeErr != nil {
			continue
		}
		remainingAmount := new(big.Rat).Sub(projectedAmount, incomeAmount)
		if remainingAmount.Sign() <= 0 {
			continue
		}
		remaining, isRemainingConverted, err := convertedAmount(converter, finance.FormatAmount(remainingAmount), row.CurrencyCode, currencyCode, budgetStatus.AsOf)
		if err != nil {
			return nil, err
		}
		if isRemainingConverted {
			stillExpectedIncome.Add(stillExpectedIncome, remaining)
		}
	}
	for _, row := range budgetStatus.SpendingCategories {
		budgetAmount, isConverted, err := convertedAmount(converter, row.BudgetAmount, row.CurrencyCode, currencyCode, budgetStatus.AsOf)
		if err != nil {
			return nil, err
		}
		if !isConverted {
			unconverted[row.CurrencyCode] = true
			continue
		}
		expectedSpending.Add(expectedSpending, budgetAmount)
	}

	// The month's income and spending, day by day, converted per currency
	// first so a currency with a day that has no rate is left out whole.
	days, err := tx.ListCashFlowDays(agentId, monthStart.Format(time.DateOnly), budgetStatus.AsOf)
	if err != nil {
		return nil, err
	}
	type currencyDay struct {
		currencyCode string
		dayIndex     int
	}
	incomeByCurrency := map[string]*big.Rat{}
	spendingByCurrencyDay := map[currencyDay]*big.Rat{}
	for _, day := range days {
		if unconverted[day.CurrencyCode] {
			continue
		}
		income, isIncomeConverted, err := convertedAmount(converter, day.IncomeAmount, day.CurrencyCode, currencyCode, day.CashFlowOn)
		if err != nil {
			return nil, err
		}
		spending, isSpendingConverted, err := convertedAmount(converter, day.SpendingAmount, day.CurrencyCode, currencyCode, day.CashFlowOn)
		if err != nil {
			return nil, err
		}
		if !isIncomeConverted || !isSpendingConverted {
			unconverted[day.CurrencyCode] = true
			continue
		}
		cashFlowOn, err := time.Parse(time.DateOnly, day.CashFlowOn)
		if err != nil {
			return nil, err
		}
		if incomeByCurrency[day.CurrencyCode] == nil {
			incomeByCurrency[day.CurrencyCode] = new(big.Rat)
		}
		incomeByCurrency[day.CurrencyCode].Add(incomeByCurrency[day.CurrencyCode], income)
		key := currencyDay{currencyCode: day.CurrencyCode, dayIndex: cashFlowOn.Day() - 1}
		if spendingByCurrencyDay[key] == nil {
			spendingByCurrencyDay[key] = new(big.Rat)
		}
		spendingByCurrencyDay[key].Add(spendingByCurrencyDay[key], spending)
	}
	incomeAmount := new(big.Rat)
	for currency, income := range incomeByCurrency {
		if !unconverted[currency] {
			incomeAmount.Add(incomeAmount, income)
		}
	}
	spendingByDay := make([]*big.Rat, budgetStatus.DayOfMonth)
	spendingAmount := new(big.Rat)
	for key, spending := range spendingByCurrencyDay {
		if unconverted[key.currencyCode] || key.dayIndex >= len(spendingByDay) {
			continue
		}
		if spendingByDay[key.dayIndex] == nil {
			spendingByDay[key.dayIndex] = new(big.Rat)
		}
		spendingByDay[key.dayIndex].Add(spendingByDay[key.dayIndex], spending)
		spendingAmount.Add(spendingAmount, spending)
	}

	projectedIncome := new(big.Rat).Add(incomeAmount, stillExpectedIncome)
	projectedSpending := new(big.Rat).Set(spendingAmount)
	if !isMonthOver {
		// All of the month's spending projected the way one budget's is:
		// repeat charges still due counted before they land, and the rest
		// at the rate it has come so far.
		projection, err := projectAllSpending(tx, converter, agentId, budgetStatus, currencyCode, spendingByDay, unconverted)
		if err != nil {
			return nil, err
		}
		projectedSpending = projection
	}
	saving := ProjectSavingMonth(&SavingMonthInput{
		ExpectedIncomeAmount: expectedIncome, ExpectedSpendingAmount: expectedSpending,
		ProjectedIncomeAmount: projectedIncome, ProjectedSpendingAmount: projectedSpending,
		DayOfMonth: budgetStatus.DayOfMonth, IsMonthOver: isMonthOver,
	})
	summary.ExpectedIncomeAmount = finance.FormatAmount(expectedIncome)
	summary.ExpectedSpendingAmount = finance.FormatAmount(expectedSpending)
	summary.ExpectedSavingAmount = finance.FormatAmount(saving.ExpectedSavingAmount)
	summary.IncomeAmount = finance.FormatAmount(incomeAmount)
	summary.SpendingAmount = finance.FormatAmount(spendingAmount)
	summary.SavingAmount = finance.FormatAmount(new(big.Rat).Sub(incomeAmount, spendingAmount))
	summary.ProjectedIncomeAmount = finance.FormatAmount(projectedIncome)
	summary.ProjectedSpendingAmount = finance.FormatAmount(projectedSpending)
	summary.ProjectedSavingAmount = finance.FormatAmount(saving.ProjectedSavingAmount)
	summary.SavingDifferenceAmount = finance.FormatAmount(saving.SavingDifferenceAmount)
	summary.SavingPace = saving.SavingPace
	summary.UnconvertedCurrencyCodes = sortedKeys(unconverted)
	return summary, nil
}

// projectAllSpending is where all of a month's spending is heading, in one
// currency: ProjectSpendingCategoryMonth over every spending category at
// once, with the repeat charges of every one of them. A repeat charge in
// a currency with no rate is left out and its currency added to
// unconverted.
func projectAllSpending(tx db.Transaction, converter *rates.Converter, agentId string, budgetStatus *models.BudgetStatus, currencyCode string,
	spendingByDay []*big.Rat, unconverted map[string]bool,
) (*big.Rat, error) {
	monthStart, err := time.Parse("2006-01", budgetStatus.Month)
	if err != nil {
		return nil, err
	}
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		return nil, err
	}
	counted := map[string]bool{}
	for _, spendingCategory := range spendingCategories {
		if !spendingCategory.IsIncome {
			counted[spendingCategory.ID] = true
		}
	}
	merchantHistory, err := tx.ListMerchantMonthSpending(agentId, budgetStatus.Month)
	if err != nil {
		return nil, err
	}
	merchantRecent, err := tx.ListMerchantMonthSpending(agentId, monthStart.AddDate(0, 1, 0).Format("2006-01"))
	if err != nil {
		return nil, err
	}
	charges, err := fixedCharges(converter, counted, merchantHistory, merchantRecent, budgetStatus.Month, currencyCode, budgetStatus.AsOf)
	if err != nil {
		return nil, err
	}
	for currency := range charges.unconvertedDue {
		unconverted[currency] = true
	}
	projection := ProjectSpendingCategoryMonth(&SpendingCategoryMonthInput{
		SpendingByDay: spendingByDay, FixedChargesDueAmount: charges.dueAmount, FixedChargesSeenAmount: charges.seenAmount,
		DayOfMonth: budgetStatus.DayOfMonth, DaysInMonth: budgetStatus.DaysInMonth,
	})
	return projection.ProjectedAmount, nil
}
