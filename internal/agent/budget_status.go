package agent

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/finance/rates"
	"github.com/ziyan/teanode/internal/models"
)

// Budget status, and the budget alert candidates written from it after
// every sync. The status is computed here for the API and for the
// candidates alike, so the dashboard, the command line, the tool and the
// alert say the same numbers.

const (
	// fixedChargeMonths is how many full months in a row a merchant must
	// have charged a spending category to be expected again.
	fixedChargeMonths = 3

	// The crossings a budget alert candidate is written for, the least
	// severe first.
	budgetCrossingEightyPercent = "80_percent"
	budgetCrossingAtRisk        = "at_risk"
	budgetCrossingOver          = "over"
	savingsTargetCrossingBehind = "behind"
)

// budgetCrossings is every crossing in order of severity: once one is
// written for a month, a less severe one that follows says nothing new.
var budgetCrossings = []string{budgetCrossingEightyPercent, budgetCrossingAtRisk, budgetCrossingOver}

// budgetAlertShare is the share of a budget whose crossing is worth a
// word: most of it gone with the month still running.
var budgetAlertShare = big.NewRat(8, 10)

// exchangeRateFetcher is the fetcher budgets convert with.
func (self *Agent) exchangeRateFetcher() *rates.Fetcher {
	if self.exchangeRates != nil {
		return self.exchangeRates
	}
	return rates.Shared(self.settings.Database)
}

// BudgetStatus is every spending category with a budget in a month
// ("2006-01"), against its budget, as of today ("2006-01-02", the person's
// local day): the last day for a past month. Spending in another currency
// is converted into the budget's at the rate of the day it posted, and
// spending in a currency with no rate is listed apart rather than
// guessed. A nil fetcher converts with the rates already stored.
//
// A budget on a spending category counts its child spending categories
// that have no budget of their own. A budget on an income spending
// category is the income expected, and is listed apart with a pace of its
// own (ProjectIncomeCategoryMonth); budget alerts read only the spending.
func BudgetStatus(ctx context.Context, tx db.Transaction, fetcher *rates.Fetcher, agentId, month, today string) (*models.BudgetStatus, error) {
	monthStart, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a month written 2006-01", db.ErrInvalidArguments, month)
	}
	todayDay, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a day written 2006-01-02", db.ErrInvalidArguments, today)
	}
	monthEnd := monthStart.AddDate(0, 1, -1)
	asOf := todayDay
	if asOf.After(monthEnd) {
		asOf = monthEnd
	}
	if asOf.Before(monthStart) {
		asOf = monthStart
	}
	budgetStatus := &models.BudgetStatus{
		Month: month, AsOf: asOf.Format(time.DateOnly), DayOfMonth: asOf.Day(), DaysInMonth: monthEnd.Day(),
		SpendingCategories: []*models.SpendingCategoryBudgetStatus{}, IncomeCategories: []*models.IncomeCategoryBudgetStatus{},
	}
	budgets, err := tx.BudgetsForMonth(agentId, month)
	if err != nil || len(budgets) == 0 {
		return budgetStatus, err
	}
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		return nil, err
	}
	isIncome := map[string]bool{}
	for _, spendingCategory := range spendingCategories {
		isIncome[spendingCategory.ID] = spendingCategory.IsIncome
	}
	previousMonth := monthStart.AddDate(0, -1, 0)
	var thisMonthIncomeDays, previousMonthIncomeDays []*models.IncomeCategoryDay
	if slices.ContainsFunc(budgets, func(budget *models.Budget) bool { return isIncome[budget.SpendingCategoryID] }) {
		if thisMonthIncomeDays, err = tx.ListIncomeCategoryDays(agentId, month); err != nil {
			return nil, err
		}
		if previousMonthIncomeDays, err = tx.ListIncomeCategoryDays(agentId, previousMonth.Format("2006-01")); err != nil {
			return nil, err
		}
	}
	thisMonthDays, err := tx.ListSpendingCategoryDays(agentId, month)
	if err != nil {
		return nil, err
	}
	previousMonthDays, err := tx.ListSpendingCategoryDays(agentId, previousMonth.Format("2006-01"))
	if err != nil {
		return nil, err
	}
	merchantHistory, err := tx.ListMerchantMonthSpending(agentId, month)
	if err != nil {
		return nil, err
	}
	// The three months before next month end with this one, so its rows
	// are what the merchants have charged so far this month.
	merchantRecent, err := tx.ListMerchantMonthSpending(agentId, monthStart.AddDate(0, 1, 0).Format("2006-01"))
	if err != nil {
		return nil, err
	}
	converter := rates.NewConverter(ctx, fetcher, tx)

	hasBudget := map[string]bool{}
	for _, budget := range budgets {
		hasBudget[budget.SpendingCategoryID] = true
	}
	nameById := map[string]string{}
	for _, spendingCategory := range spendingCategories {
		nameById[spendingCategory.ID] = spendingCategory.SpendingCategoryName
	}
	previousMonthLastDay := monthStart.AddDate(0, 0, -1).Day()
	for _, budget := range budgets {
		counted := map[string]bool{budget.SpendingCategoryID: true}
		for _, spendingCategory := range spendingCategories {
			if spendingCategory.ParentSpendingCategoryID == budget.SpendingCategoryID && !hasBudget[spendingCategory.ID] {
				counted[spendingCategory.ID] = true
			}
		}
		budgetAmount, err := finance.ParseAmount(budget.MonthlyAmount)
		if err != nil {
			return nil, err
		}
		if isIncome[budget.SpendingCategoryID] {
			incomeRow, err := incomeCategoryBudgetStatus(converter, budgetStatus, budget, budgetAmount, counted, thisMonthIncomeDays, previousMonthIncomeDays,
				previousMonthLastDay, todayDay.After(monthEnd))
			if err != nil {
				return nil, err
			}
			incomeRow.SpendingCategoryName = nameById[budget.SpendingCategoryID]
			budgetStatus.IncomeCategories = append(budgetStatus.IncomeCategories, incomeRow)
			continue
		}
		row := &models.SpendingCategoryBudgetStatus{
			SpendingCategoryID: budget.SpendingCategoryID, SpendingCategoryName: nameById[budget.SpendingCategoryID],
			BudgetAmount: finance.FormatAmount(budgetAmount), CurrencyCode: budget.CurrencyCode,
		}
		unconverted := map[string]*big.Rat{}
		unconvertedLastMonth := map[string]*big.Rat{}

		spendingByDay := make([]*big.Rat, budgetStatus.DayOfMonth)
		for _, day := range thisMonthDays {
			if !counted[day.SpendingCategoryID] || day.SpentOn > budgetStatus.AsOf {
				continue
			}
			spentOn, err := time.Parse(time.DateOnly, day.SpentOn)
			if err != nil {
				return nil, err
			}
			amount, isConverted, err := convertedAmount(converter, day.SpendingAmount, day.CurrencyCode, budget.CurrencyCode, day.SpentOn)
			if err != nil {
				return nil, err
			}
			if !isConverted {
				addTo(unconverted, day.CurrencyCode, day.SpendingAmount)
				continue
			}
			index := spentOn.Day() - 1
			if spendingByDay[index] == nil {
				spendingByDay[index] = new(big.Rat)
			}
			spendingByDay[index].Add(spendingByDay[index], amount)
		}

		lastMonthSpending := new(big.Rat)
		lastMonthDayBound := min(budgetStatus.DayOfMonth, previousMonthLastDay)
		for _, day := range previousMonthDays {
			spentOn, err := time.Parse(time.DateOnly, day.SpentOn)
			if err != nil {
				return nil, err
			}
			if !counted[day.SpendingCategoryID] || spentOn.Day() > lastMonthDayBound {
				continue
			}
			amount, isConverted, err := convertedAmount(converter, day.SpendingAmount, day.CurrencyCode, budget.CurrencyCode, day.SpentOn)
			if err != nil {
				return nil, err
			}
			if !isConverted {
				addTo(unconvertedLastMonth, day.CurrencyCode, day.SpendingAmount)
				continue
			}
			lastMonthSpending.Add(lastMonthSpending, amount)
		}

		charges, err := fixedCharges(converter, counted, merchantHistory, merchantRecent, month, budget.CurrencyCode, budgetStatus.AsOf)
		if err != nil {
			return nil, err
		}
		projection := ProjectSpendingCategoryMonth(&SpendingCategoryMonthInput{
			BudgetAmount: budgetAmount, SpendingByDay: spendingByDay,
			FixedChargesDueAmount: charges.dueAmount, FixedChargesSeenAmount: charges.seenAmount,
			DayOfMonth: budgetStatus.DayOfMonth, DaysInMonth: budgetStatus.DaysInMonth,
		})
		row.SpendingAmount = finance.FormatAmount(projection.SpendingAmount)
		row.SpendingBySameDayLastMonthAmount = finance.FormatAmount(lastMonthSpending)
		row.FixedChargesDueAmount = finance.FormatAmount(charges.dueAmount)
		row.ExpectedRepeatCharges = charges.dueCharges
		row.ProjectedAmount = finance.FormatAmount(projection.ProjectedAmount)
		row.BudgetPace = projection.BudgetPace
		row.UnconvertedSpending = currencyAmountsOf(unconverted)
		row.UnconvertedSpendingBySameDayLastMonth = currencyAmountsOf(unconvertedLastMonth)
		row.UnconvertedFixedChargesDue = currencyAmountsOf(charges.unconvertedDue)
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

// incomeCategoryBudgetStatus is one income budget against what came in:
// the counted income spending categories' income this month up to the
// status's day, and last month's by the same day, each converted into the
// budget's currency at the rate of the day it came in, with what has no
// rate reported apart.
func incomeCategoryBudgetStatus(converter *rates.Converter, budgetStatus *models.BudgetStatus, budget *models.Budget, budgetAmount *big.Rat,
	counted map[string]bool, thisMonthDays, previousMonthDays []*models.IncomeCategoryDay, previousMonthLastDay int, isMonthOver bool,
) (*models.IncomeCategoryBudgetStatus, error) {
	unconverted := map[string]*big.Rat{}
	unconvertedLastMonth := map[string]*big.Rat{}
	incomeAmount := new(big.Rat)
	for _, day := range thisMonthDays {
		if !counted[day.SpendingCategoryID] || day.ReceivedOn > budgetStatus.AsOf {
			continue
		}
		amount, isConverted, err := convertedAmount(converter, day.IncomeAmount, day.CurrencyCode, budget.CurrencyCode, day.ReceivedOn)
		if err != nil {
			return nil, err
		}
		if !isConverted {
			addTo(unconverted, day.CurrencyCode, day.IncomeAmount)
			continue
		}
		incomeAmount.Add(incomeAmount, amount)
	}
	lastMonthIncome := new(big.Rat)
	lastMonthDayBound := min(budgetStatus.DayOfMonth, previousMonthLastDay)
	for _, day := range previousMonthDays {
		receivedOn, err := time.Parse(time.DateOnly, day.ReceivedOn)
		if err != nil {
			return nil, err
		}
		if !counted[day.SpendingCategoryID] || receivedOn.Day() > lastMonthDayBound {
			continue
		}
		amount, isConverted, err := convertedAmount(converter, day.IncomeAmount, day.CurrencyCode, budget.CurrencyCode, day.ReceivedOn)
		if err != nil {
			return nil, err
		}
		if !isConverted {
			addTo(unconvertedLastMonth, day.CurrencyCode, day.IncomeAmount)
			continue
		}
		lastMonthIncome.Add(lastMonthIncome, amount)
	}
	projection := ProjectIncomeCategoryMonth(&IncomeCategoryMonthInput{
		BudgetAmount: budgetAmount, IncomeAmount: incomeAmount,
		DayOfMonth: budgetStatus.DayOfMonth, DaysInMonth: budgetStatus.DaysInMonth, IsMonthOver: isMonthOver,
	})
	return &models.IncomeCategoryBudgetStatus{
		SpendingCategoryID: budget.SpendingCategoryID, BudgetAmount: finance.FormatAmount(budgetAmount), CurrencyCode: budget.CurrencyCode,
		IncomeAmount: finance.FormatAmount(incomeAmount), IncomeBySameDayLastMonthAmount: finance.FormatAmount(lastMonthIncome),
		ExpectedByTodayAmount: finance.FormatAmount(projection.ExpectedByTodayAmount),
		ProjectedAmount:       finance.FormatAmount(projection.ProjectedAmount), IncomePace: projection.IncomePace,
		UnconvertedIncome: currencyAmountsOf(unconverted), UnconvertedIncomeBySameDayLastMonth: currencyAmountsOf(unconvertedLastMonth),
	}, nil
}

// fixedChargeAmounts is what fixedCharges finds, in the budget's currency:
// what regular merchants are still expected to charge this month, what
// they have charged already, and, per currency, what is still expected
// in a currency with no exchange rate into the budget's. What they have
// charged already in such a currency is spending, and is reported with
// the rest of the month's unconverted spending. dueCharges is what is
// still expected, merchant by merchant, largest first.
type fixedChargeAmounts struct {
	dueAmount      *big.Rat
	seenAmount     *big.Rat
	unconvertedDue map[string]*big.Rat
	dueCharges     []*models.ExpectedRepeatCharge
}

// fixedCharges is what merchants that charged the counted spending
// categories in each of the three full months before this one are
// expected to charge again (their median, converted at asOf's rate) and
// have not yet, and what those merchants have charged this month already.
// A merchant is one merchant in one currency; what it charged several of
// the counted spending categories is added together.
func fixedCharges(converter *rates.Converter, counted map[string]bool, history, recent []*models.MerchantMonthSpending, month, currencyCode, asOf string) (*fixedChargeAmounts, error) {
	type merchantKey struct {
		merchantName string
		currencyCode string
	}
	amountsByMerchant := map[merchantKey]map[string]*big.Rat{}
	// The name a merchant is shown by is how it was written in the latest
	// month it charged, since the key folds case.
	type merchantDisplay struct {
		merchantName  string
		spendingMonth string
	}
	displayByMerchant := map[merchantKey]merchantDisplay{}
	for _, row := range history {
		if !counted[row.SpendingCategoryID] || row.SpendingMonth == month {
			continue
		}
		amount, err := finance.ParseAmount(row.SpendingAmount)
		if err != nil {
			return nil, err
		}
		key := merchantKey{merchantName: strings.ToLower(strings.TrimSpace(row.MerchantName)), currencyCode: row.CurrencyCode}
		if display, isKnown := displayByMerchant[key]; !isKnown || row.SpendingMonth >= display.spendingMonth {
			displayByMerchant[key] = merchantDisplay{merchantName: strings.TrimSpace(row.MerchantName), spendingMonth: row.SpendingMonth}
		}
		if amountsByMerchant[key] == nil {
			amountsByMerchant[key] = map[string]*big.Rat{}
		}
		if amountsByMerchant[key][row.SpendingMonth] == nil {
			amountsByMerchant[key][row.SpendingMonth] = new(big.Rat)
		}
		amountsByMerchant[key][row.SpendingMonth].Add(amountsByMerchant[key][row.SpendingMonth], amount)
	}
	seenThisMonth := map[merchantKey]*big.Rat{}
	for _, row := range recent {
		if !counted[row.SpendingCategoryID] || row.SpendingMonth != month {
			continue
		}
		amount, err := finance.ParseAmount(row.SpendingAmount)
		if err != nil {
			return nil, err
		}
		key := merchantKey{merchantName: strings.ToLower(strings.TrimSpace(row.MerchantName)), currencyCode: row.CurrencyCode}
		if seenThisMonth[key] == nil {
			seenThisMonth[key] = new(big.Rat)
		}
		seenThisMonth[key].Add(seenThisMonth[key], amount)
	}
	charges := &fixedChargeAmounts{dueAmount: new(big.Rat), seenAmount: new(big.Rat), unconvertedDue: map[string]*big.Rat{}, dueCharges: []*models.ExpectedRepeatCharge{}}
	dueAmounts := map[*models.ExpectedRepeatCharge]*big.Rat{}
	for key, byMonth := range amountsByMerchant {
		monthlyAmounts := make([]*big.Rat, 0, len(byMonth))
		for _, amount := range byMonth {
			if amount.Sign() > 0 {
				monthlyAmounts = append(monthlyAmounts, amount)
			}
		}
		if len(monthlyAmounts) < fixedChargeMonths {
			continue
		}
		if seenAmount, isSeen := seenThisMonth[key]; isSeen {
			converted, isConverted, err := convertedAmount(converter, finance.FormatAmount(seenAmount), key.currencyCode, currencyCode, asOf)
			if err != nil {
				return nil, err
			}
			if isConverted && converted.Sign() > 0 {
				charges.seenAmount.Add(charges.seenAmount, converted)
			}
			continue
		}
		sort.Slice(monthlyAmounts, func(left, right int) bool { return monthlyAmounts[left].Cmp(monthlyAmounts[right]) < 0 })
		medianAmount := monthlyAmounts[len(monthlyAmounts)/2]
		converted, isConverted, err := convertedAmount(converter, finance.FormatAmount(medianAmount), key.currencyCode, currencyCode, asOf)
		if err != nil {
			return nil, err
		}
		if !isConverted {
			addTo(charges.unconvertedDue, key.currencyCode, finance.FormatAmount(medianAmount))
			dueCharge := &models.ExpectedRepeatCharge{MerchantName: displayByMerchant[key].merchantName, ExpectedAmount: finance.FormatAmount(medianAmount), CurrencyCode: key.currencyCode}
			charges.dueCharges = append(charges.dueCharges, dueCharge)
			dueAmounts[dueCharge] = medianAmount
			continue
		}
		charges.dueAmount.Add(charges.dueAmount, converted)
		dueCharge := &models.ExpectedRepeatCharge{MerchantName: displayByMerchant[key].merchantName, ExpectedAmount: finance.FormatAmount(converted), CurrencyCode: currencyCode}
		charges.dueCharges = append(charges.dueCharges, dueCharge)
		dueAmounts[dueCharge] = converted
	}
	// The budget's currency first, then largest first within a currency,
	// then by name, so the list reads the same on every load.
	sort.Slice(charges.dueCharges, func(left, right int) bool {
		leftCharge, rightCharge := charges.dueCharges[left], charges.dueCharges[right]
		if (leftCharge.CurrencyCode == currencyCode) != (rightCharge.CurrencyCode == currencyCode) {
			return leftCharge.CurrencyCode == currencyCode
		}
		if leftCharge.CurrencyCode != rightCharge.CurrencyCode {
			return leftCharge.CurrencyCode < rightCharge.CurrencyCode
		}
		if comparison := dueAmounts[leftCharge].Cmp(dueAmounts[rightCharge]); comparison != 0 {
			return comparison > 0
		}
		return leftCharge.MerchantName < rightCharge.MerchantName
	})
	return charges, nil
}

// currencyAmountsOf is sums per currency as a list, by currency code.
func currencyAmountsOf(sums map[string]*big.Rat) []*models.CurrencyAmount {
	currencyAmounts := []*models.CurrencyAmount{}
	for _, currencyCode := range sortedKeys(sums) {
		currencyAmounts = append(currencyAmounts, &models.CurrencyAmount{CurrencyCode: currencyCode, Amount: finance.FormatAmount(sums[currencyCode])})
	}
	return currencyAmounts
}

// convertedAmount is an amount in another currency at a day's rate, and
// false when there is no rate for it, which is not an error.
func convertedAmount(converter *rates.Converter, amount, fromCurrencyCode, toCurrencyCode, on string) (*big.Rat, bool, error) {
	converted, _, err := converter.Convert(amount, fromCurrencyCode, toCurrencyCode, on)
	var noRate *finance.ErrNoExchangeRate
	if errors.As(err, &noRate) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	convertedValue, err := finance.ParseAmount(converted)
	return convertedValue, err == nil, err
}

func addTo(sums map[string]*big.Rat, currencyCode, amount string) {
	amountValue, err := finance.ParseAmount(amount)
	if err != nil {
		return
	}
	if sums[currencyCode] == nil {
		sums[currencyCode] = new(big.Rat)
	}
	sums[currencyCode].Add(sums[currencyCode], amountValue)
}

// displayAmount is an amount as a person reads it: two places.
func displayAmount(amountValue *big.Rat) string {
	return amountValue.FloatString(2)
}

// noteBudgetCandidates writes the budget alert candidates a sync gave rise
// to: for each spending category of this month that crossed eighty percent
// of its budget, became at_risk or went over, and for each savings target
// that fell behind. Each crossing is written once, and a less severe one
// after a more severe one of the same month not at all; a muted spending
// category, or budget alerts muted altogether, writes nothing.
func (self *Agent) noteBudgetCandidates(ctx context.Context, run *Run, now time.Time) error {
	if !run.Agent.Active() || !run.Agent.IsAlertsEnabled {
		return nil
	}
	local := now.In(Location(run.Owner))
	today := local.Format(time.DateOnly)
	month := local.Format("2006-01")
	agentId := run.Agent.ID
	// The rates are brought up to date before the transaction opens, and
	// the computation inside it converts with what is stored: an ECB that
	// does not answer must not hold the transaction open while it waits.
	ensureRatesOutsideTransaction(ctx, run.Database(), self.exchangeRateFetcher(), agentId, today)
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		mutes, err := tx.ListAgentAlertMutes(agentId)
		if err != nil {
			return err
		}
		if mutedBy(mutes, &alertFacts{kinds: []string{models.AlertKindBudget}}) != nil {
			return nil
		}
		budgetStatus, err := BudgetStatus(ctx, tx, nil, agentId, month, today)
		if err != nil {
			return err
		}
		isWritten := false
		for _, row := range budgetStatus.SpendingCategories {
			crossing := budgetCrossingOf(row)
			if crossing == "" || mutedBy(mutes, &alertFacts{spendingCategoryId: row.SpendingCategoryID}) != nil {
				continue
			}
			isToldAlready := false
			for index := slices.Index(budgetCrossings, crossing); index < len(budgetCrossings) && !isToldAlready; index++ {
				if isToldAlready, err = tx.HasAgentBudgetAlert(agentId, spendingCategoryBudgetKey(row.SpendingCategoryID, month, budgetCrossings[index])); err != nil {
					return err
				}
			}
			if isToldAlready {
				continue
			}
			if _, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{
				AgentID: agentId, CandidateKind: models.AlertCandidateBudget, AlertSignal: models.AlertSignalSoon,
				CandidateReason: budgetCrossingReason(row, crossing, budgetStatus),
				BudgetKey:       spendingCategoryBudgetKey(row.SpendingCategoryID, month, crossing),
			}); err != nil {
				return err
			}
			isWritten = true
		}
		behind, err := savingsTargetsBehind(tx, rates.NewConverter(ctx, nil, tx), agentId, local)
		if err != nil {
			return err
		}
		for _, targetBehind := range behind {
			budgetKey := "savings-target:" + targetBehind.savingsTarget.ID + ":" + month + ":" + savingsTargetCrossingBehind
			isToldAlready, err := tx.HasAgentBudgetAlert(agentId, budgetKey)
			if err != nil {
				return err
			}
			if isToldAlready {
				continue
			}
			if _, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{
				AgentID: agentId, CandidateKind: models.AlertCandidateBudget, AlertSignal: models.AlertSignalSoon,
				CandidateReason: targetBehind.candidateReason, BudgetKey: budgetKey,
			}); err != nil {
				return err
			}
			isWritten = true
		}
		if !isWritten {
			return nil
		}
		return queueAlert(tx, agentId, false, now)
	})
}

// ensureRatesOutsideTransaction brings the stored exchange rates up to
// today, outside any transaction, when the agent's finance accounts,
// budgets and savings targets are in more than one currency; an agent
// whose money is all in one never fetches. A fetch that fails is logged,
// and the computation that follows converts with what is stored, leaving
// what it cannot convert reported as unconverted.
func ensureRatesOutsideTransaction(ctx context.Context, database db.Database, fetcher *rates.Fetcher, agentId, today string) {
	if fetcher == nil {
		return
	}
	isCurrencyCode := map[string]bool{}
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		financeAccounts, err := tx.ListFinanceAccounts(agentId, "")
		if err != nil {
			return err
		}
		for _, financeAccount := range financeAccounts {
			isCurrencyCode[financeAccount.CurrencyCode] = true
		}
		budgets, err := tx.ListBudgets(agentId)
		if err != nil {
			return err
		}
		for _, budget := range budgets {
			isCurrencyCode[budget.CurrencyCode] = true
		}
		savingsTargets, err := tx.ListSavingsTargets(agentId)
		if err != nil {
			return err
		}
		for _, savingsTarget := range savingsTargets {
			isCurrencyCode[savingsTarget.CurrencyCode] = true
		}
		return nil
	}); err != nil {
		log.Warningf("cannot tell which currencies agent %q uses to bring the exchange rates up to date: %s", agentId, err)
		return
	}
	if len(isCurrencyCode) < 2 {
		return
	}
	if err := fetcher.EnsureRates(ctx, today); err != nil {
		log.Warningf("cannot bring the exchange rates up to %s, converting with what is stored: %s", today, err)
	}
}

// budgetCrossingOf is the most severe crossing a spending category is
// past this month, or empty.
func budgetCrossingOf(row *models.SpendingCategoryBudgetStatus) string {
	switch row.BudgetPace {
	case models.BudgetPaceOver:
		return budgetCrossingOver
	case models.BudgetPaceAtRisk:
		return budgetCrossingAtRisk
	}
	budgetAmount, budgetErr := finance.ParseAmount(row.BudgetAmount)
	spendingAmount, spendingErr := finance.ParseAmount(row.SpendingAmount)
	if budgetErr != nil || spendingErr != nil || budgetAmount.Sign() <= 0 {
		return ""
	}
	if spendingAmount.Cmp(new(big.Rat).Mul(budgetAmount, budgetAlertShare)) >= 0 {
		return budgetCrossingEightyPercent
	}
	return ""
}

// spendingCategoryBudgetKey names one crossing of one spending category's
// budget in one month.
func spendingCategoryBudgetKey(spendingCategoryId, month, crossing string) string {
	return "spending-category:" + spendingCategoryId + ":" + month + ":" + crossing
}

// spendingCategoryOfBudgetKey is the spending category a budget key names,
// or empty for a savings target's.
func spendingCategoryOfBudgetKey(budgetKey string) string {
	rest, isSpendingCategory := strings.CutPrefix(budgetKey, "spending-category:")
	if !isSpendingCategory {
		return ""
	}
	spendingCategoryId, _, _ := strings.Cut(rest, ":")
	return spendingCategoryId
}

// budgetCrossingReason is the candidate's line: the numbers, which the
// alert job words.
func budgetCrossingReason(row *models.SpendingCategoryBudgetStatus, crossing string, budgetStatus *models.BudgetStatus) string {
	budgetAmount, _ := finance.ParseAmount(row.BudgetAmount)
	spendingAmount, _ := finance.ParseAmount(row.SpendingAmount)
	projectedAmount, _ := finance.ParseAmount(row.ProjectedAmount)
	spentPercent := 0
	if budgetAmount != nil && budgetAmount.Sign() > 0 && spendingAmount != nil {
		spentPercentValue, _ := new(big.Rat).Quo(new(big.Rat).Mul(spendingAmount, big.NewRat(100, 1)), budgetAmount).Float64()
		spentPercent = int(spentPercentValue)
	}
	crossingDescription := ""
	switch crossing {
	case budgetCrossingOver:
		crossingDescription = "is over its monthly budget"
	case budgetCrossingAtRisk:
		crossingDescription = "is heading past its monthly budget"
	default:
		crossingDescription = "has used most of its monthly budget"
	}
	candidateReason := fmt.Sprintf("Spending category %q %s: %s of %s %s spent (%d%%) by day %d of %d; the month is projected to end at %s %s.",
		row.SpendingCategoryName, crossingDescription, displayAmount(ratOrZero(spendingAmount)), displayAmount(ratOrZero(budgetAmount)), row.CurrencyCode, spentPercent,
		budgetStatus.DayOfMonth, budgetStatus.DaysInMonth, displayAmount(ratOrZero(projectedAmount)), row.CurrencyCode)
	if fixedChargesDueAmount, err := finance.ParseAmount(row.FixedChargesDueAmount); err == nil && fixedChargesDueAmount.Sign() > 0 {
		candidateReason += fmt.Sprintf(" That counts %s %s of repeat charges still expected this month (merchants that charged it in each of the last three months)%s.",
			displayAmount(fixedChargesDueAmount), row.CurrencyCode, repeatChargeList(row.ExpectedRepeatCharges))
	}
	if lastMonthSpendingAmount, err := finance.ParseAmount(row.SpendingBySameDayLastMonthAmount); err == nil {
		candidateReason += fmt.Sprintf(" By the same day last month it was %s %s.", displayAmount(lastMonthSpendingAmount), row.CurrencyCode)
	}
	return candidateReason
}

// repeatChargeList is the repeat charges still expected, each merchant
// with its amount, after a colon, or nothing when there are none.
func repeatChargeList(charges []*models.ExpectedRepeatCharge) string {
	if len(charges) == 0 {
		return ""
	}
	lines := make([]string, 0, len(charges))
	for _, charge := range charges {
		expectedAmount, err := finance.ParseAmount(charge.ExpectedAmount)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", charge.MerchantName, displayAmount(expectedAmount), charge.CurrencyCode))
	}
	return ": " + strings.Join(lines, ", ")
}

// savingsTargetBehind is a savings target that fell behind, with the
// candidate's line and the currencies its cash flow left out for want of
// an exchange rate into the target's.
type savingsTargetBehind struct {
	savingsTarget            *models.SavingsTarget
	candidateReason          string
	unconvertedCurrencyCodes []string
}

// savingsTargetsBehind is the open savings targets measured by cash flow
// whose last two full months both saved less than the pace they needed:
// what remained at the start of each month, over the months left to the
// target day. A target started after the first of those two months has
// not had two full months yet. Targets measured by asset value are not
// judged here: their valuations do not arrive with a sync.
//
// A month's cash flow is money in less money out, transfers left out,
// per currency, converted at the rate of the month's last day. A currency
// with no rate is left out and named in the line, since the target may
// look behind only for what was left out.
func savingsTargetsBehind(tx db.Transaction, converter *rates.Converter, agentId string, local time.Time) ([]*savingsTargetBehind, error) {
	savingsTargets, err := tx.ListSavingsTargets(agentId)
	if err != nil {
		return nil, err
	}
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	firstMonth, secondMonth := monthStart.AddDate(0, -2, 0), monthStart.AddDate(0, -1, 0)
	var behind []*savingsTargetBehind
	for _, savingsTarget := range savingsTargets {
		if savingsTarget.ClosedOn != "" || savingsTarget.TargetMeasure != models.TargetMeasureCashFlow {
			continue
		}
		startedOn, err := time.Parse(time.DateOnly, savingsTarget.StartedOn)
		if err != nil || startedOn.After(firstMonth) {
			continue
		}
		targetOn, err := time.Parse(time.DateOnly, savingsTarget.TargetOn)
		if err != nil || targetOn.Before(monthStart) {
			continue
		}
		targetAmount, err := finance.ParseAmount(savingsTarget.TargetAmount)
		if err != nil {
			continue
		}
		cashFlowByMonth, unconvertedCurrencyCodes, err := savingsTargetCashFlowByMonth(tx, converter, agentId, savingsTarget, monthStart.AddDate(0, 0, -1))
		if err != nil {
			return nil, err
		}
		isFirstShort, firstSaved, firstRequired := savingsTargetMonthShortfall(cashFlowByMonth, targetAmount, targetOn, firstMonth)
		isSecondShort, secondSaved, secondRequired := savingsTargetMonthShortfall(cashFlowByMonth, targetAmount, targetOn, secondMonth)
		if !isFirstShort || !isSecondShort {
			continue
		}
		candidateReason := fmt.Sprintf("Savings target %q (%s %s by %s) is behind: %s saved %s %s against the %s %s it needed, and %s saved %s %s against %s %s.",
			savingsTarget.SavingsTargetName, displayAmount(targetAmount), savingsTarget.CurrencyCode, savingsTarget.TargetOn,
			firstMonth.Format("January"), displayAmount(firstSaved), savingsTarget.CurrencyCode, displayAmount(firstRequired), savingsTarget.CurrencyCode,
			secondMonth.Format("January"), displayAmount(secondSaved), savingsTarget.CurrencyCode, displayAmount(secondRequired), savingsTarget.CurrencyCode)
		if len(unconvertedCurrencyCodes) > 0 {
			candidateReason += fmt.Sprintf(" Money in %s is left out: there is no exchange rate from it into %s.",
				strings.Join(unconvertedCurrencyCodes, ", "), savingsTarget.CurrencyCode)
		}
		behind = append(behind, &savingsTargetBehind{
			savingsTarget: savingsTarget, candidateReason: candidateReason, unconvertedCurrencyCodes: unconvertedCurrencyCodes,
		})
	}
	return behind, nil
}

// savingsTargetCashFlowByMonth is a cash flow savings target's cash flow
// per month ("2006-01") from the day it started to the day given, both
// included, in its currency: money in less money out, transfers left out,
// each currency converted at the rate of the month's last day. The
// currencies with no rate into the target's are left out and named.
func savingsTargetCashFlowByMonth(tx db.Transaction, converter *rates.Converter, agentId string, savingsTarget *models.SavingsTarget, through time.Time) (map[string]*big.Rat, []string, error) {
	cashFlowByMonth := map[string]*big.Rat{}
	if through.Format(time.DateOnly) < savingsTarget.StartedOn {
		return cashFlowByMonth, nil, nil
	}
	rows, err := tx.FinanceSpendingSummary(agentId, &db.FinanceSpendingSummaryFilter{
		From: savingsTarget.StartedOn, To: through.Format(time.DateOnly),
		GroupBy: models.FinanceSpendingSummaryGroupByMonth,
	})
	if err != nil {
		return nil, nil, err
	}
	unconverted := map[string]bool{}
	for _, row := range rows {
		rowMonth, err := time.Parse("2006-01", row.GroupKey)
		if err != nil {
			continue
		}
		moneyIn, inErr := finance.ParseAmount(row.MoneyIn)
		moneyOut, outErr := finance.ParseAmount(row.MoneyOut)
		if inErr != nil || outErr != nil {
			continue
		}
		netCashFlowAmount := new(big.Rat).Sub(moneyIn, moneyOut)
		converted, isConverted, err := convertedAmount(converter, finance.FormatAmount(netCashFlowAmount), row.CurrencyCode, savingsTarget.CurrencyCode, rowMonth.AddDate(0, 1, -1).Format(time.DateOnly))
		if err != nil {
			return nil, nil, err
		}
		if !isConverted {
			unconverted[row.CurrencyCode] = true
			continue
		}
		if cashFlowByMonth[row.GroupKey] == nil {
			cashFlowByMonth[row.GroupKey] = new(big.Rat)
		}
		cashFlowByMonth[row.GroupKey].Add(cashFlowByMonth[row.GroupKey], converted)
	}
	return cashFlowByMonth, sortedKeys(unconverted), nil
}

// savingsTargetMonthShortfall says whether a month saved less than the
// pace it needed -- what remained at its start over the months left to
// the target day, that month included -- with what it saved and what it
// needed.
func savingsTargetMonthShortfall(cashFlowByMonth map[string]*big.Rat, targetAmount *big.Rat, targetOn, month time.Time) (bool, *big.Rat, *big.Rat) {
	savedBeforeAmount := new(big.Rat)
	for key, cashFlow := range cashFlowByMonth {
		if key < month.Format("2006-01") {
			savedBeforeAmount.Add(savedBeforeAmount, cashFlow)
		}
	}
	remainingAmount := new(big.Rat).Sub(targetAmount, savedBeforeAmount)
	requiredAmount := new(big.Rat).Quo(remainingAmount, big.NewRat(int64(max(monthsThrough(month, targetOn), 1)), 1))
	savedAmount := ratOrZero(cashFlowByMonth[month.Format("2006-01")])
	return remainingAmount.Sign() > 0 && savedAmount.Cmp(requiredAmount) < 0, savedAmount, requiredAmount
}

// monthsThrough is how many months from one month to the month of a day,
// both included: one for the same month.
func monthsThrough(month, day time.Time) int {
	return (day.Year()-month.Year())*12 + int(day.Month()) - int(month.Month()) + 1
}

// SavingsTargetProgress is how a savings target stands, in its currency:
// what it has saved, what remains, and the monthly pace that would still
// reach it by its day.
type SavingsTargetProgress struct {
	// SavedAmount is, for a cash flow target, income less spending since
	// it started; for an asset value target, what its assets and the
	// assets of its finance accounts are worth now less its starting
	// amount; for a net worth target, net worth now less its starting
	// amount. RemainingAmount is the target less that, never below zero.
	SavedAmount     string `json:"savedAmount"`
	RemainingAmount string `json:"remainingAmount"`

	// MonthsLeftCount is the months from this one to the target's, both
	// included, and RequiredMonthlyAmount what remains over them: the pace
	// needed.
	MonthsLeftCount       int    `json:"monthsLeftCount"`
	RequiredMonthlyAmount string `json:"requiredMonthlyAmount"`

	// IsBehind says a cash flow target's last two full months both saved
	// less than the pace they needed: the same test its alert is written
	// by. Always false for an asset value or net worth target.
	IsBehind bool `json:"isBehind"`

	// UnconvertedCurrencyCodes are the currencies left out for want of an
	// exchange rate into the target's.
	UnconvertedCurrencyCodes []string `json:"unconvertedCurrencyCodes"`
}

// assetValuationOn is the valuation of an asset that counts on a day: the
// winning one of the latest day on or before it, or nil when there is none
// yet. A valuation recorded ahead for a later day does not count today.
func assetValuationOn(tx db.Transaction, agentId, assetId, day string) (*models.AssetValuation, error) {
	valuations, err := tx.ListAssetValuations(agentId, assetId)
	if err != nil {
		return nil, err
	}
	// Newest day first, and within a day the winner first.
	for _, valuation := range valuations {
		if valuation.ValuedOn <= day {
			return valuation, nil
		}
	}
	return nil, nil
}

// isSavingsTargetAsset says an asset_value savings target measures the
// asset: chosen itself, or valued by a finance account it chose. Asked of
// each asset once, so one reached both ways counts once, and a holding a
// chosen account came to hold after the target started counts too.
func isSavingsTargetAsset(savingsTarget *models.SavingsTarget, asset *models.Asset) bool {
	if slices.Contains(savingsTarget.AssetIDs, asset.ID) {
		return true
	}
	return asset.FinanceAccountID != "" && slices.Contains(savingsTarget.FinanceAccountIDs, asset.FinanceAccountID)
}

// netWorthIn is net worth on a day in one currency, converted the way the
// Net worth section converts it: each currency's total at that day's rate.
// A currency with no rate is left out and named in the second answer.
func netWorthIn(converter *rates.Converter, tx db.Transaction, agentId, currencyCode, day string) (*big.Rat, []string, error) {
	points, err := tx.NetWorthSeries(agentId, day, day)
	if err != nil {
		return nil, nil, err
	}
	netWorthAmount := new(big.Rat)
	unconverted := map[string]bool{}
	for _, point := range points {
		converted, isConverted, err := convertedAmount(converter, point.NetWorthAmount, point.CurrencyCode, currencyCode, point.NetWorthOn)
		if err != nil {
			return nil, nil, err
		}
		if !isConverted {
			unconverted[point.CurrencyCode] = true
			continue
		}
		netWorthAmount.Add(netWorthAmount, converted)
	}
	return netWorthAmount, sortedKeys(unconverted), nil
}

// NetWorthOn is net worth on a day ("2006-01-02") in one currency, as a
// decimal, with the currencies left out for want of a rate: what a net
// worth savings target records as its starting amount. A nil fetcher
// converts with the rates already stored.
func NetWorthOn(ctx context.Context, tx db.Transaction, fetcher *rates.Fetcher, agentId, currencyCode, day string) (string, []string, error) {
	netWorthAmount, unconverted, err := netWorthIn(rates.NewConverter(ctx, fetcher, tx), tx, agentId, currencyCode, day)
	if err != nil {
		return "", nil, err
	}
	return finance.FormatAmount(netWorthAmount), unconverted, nil
}

// SavingsTargetProgressOf is how one savings target stands as of today
// ("2006-01-02", the person's local day), measured the way its alert is.
// A nil fetcher converts with the rates already stored.
func SavingsTargetProgressOf(ctx context.Context, tx db.Transaction, fetcher *rates.Fetcher, agentId string, savingsTarget *models.SavingsTarget, today string) (*SavingsTargetProgress, error) {
	local, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a day written 2006-01-02", db.ErrInvalidArguments, today)
	}
	targetAmount, err := finance.ParseAmount(savingsTarget.TargetAmount)
	if err != nil {
		return nil, err
	}
	targetOn, err := time.Parse(time.DateOnly, savingsTarget.TargetOn)
	if err != nil {
		return nil, err
	}
	converter := rates.NewConverter(ctx, fetcher, tx)
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	progress := &SavingsTargetProgress{UnconvertedCurrencyCodes: []string{}}
	savedAmount := new(big.Rat)
	switch savingsTarget.TargetMeasure {
	case models.TargetMeasureNetWorth:
		netWorthAmount, unconverted, err := netWorthIn(converter, tx, agentId, savingsTarget.CurrencyCode, today)
		if err != nil {
			return nil, err
		}
		savedAmount.Set(netWorthAmount)
		if savingsTarget.StartingAmount != "" {
			startingAmount, err := finance.ParseAmount(savingsTarget.StartingAmount)
			if err != nil {
				return nil, err
			}
			savedAmount.Sub(savedAmount, startingAmount)
		}
		progress.UnconvertedCurrencyCodes = append(progress.UnconvertedCurrencyCodes, unconverted...)
	case models.TargetMeasureAssetValue:
		assets, err := tx.ListAssets(agentId)
		if err != nil {
			return nil, err
		}
		unconverted := map[string]bool{}
		for _, asset := range assets {
			// An asset sold before today no longer counts, as in net worth,
			// which counts it through the day it was sold and not after.
			if !isSavingsTargetAsset(savingsTarget, asset) || (asset.ClosedOn != "" && asset.ClosedOn < today) {
				continue
			}
			valuation, err := assetValuationOn(tx, agentId, asset.ID, today)
			if err != nil {
				return nil, err
			}
			if valuation == nil {
				continue
			}
			assetValue, isConverted, err := convertedAmount(converter, valuation.Value, valuation.CurrencyCode, savingsTarget.CurrencyCode, today)
			if err != nil {
				return nil, err
			}
			if !isConverted {
				unconverted[valuation.CurrencyCode] = true
				continue
			}
			if asset.IsLiability {
				assetValue.Neg(assetValue)
			}
			savedAmount.Add(savedAmount, assetValue)
		}
		if savingsTarget.StartingAmount != "" {
			startingAmount, err := finance.ParseAmount(savingsTarget.StartingAmount)
			if err != nil {
				return nil, err
			}
			savedAmount.Sub(savedAmount, startingAmount)
		}
		progress.UnconvertedCurrencyCodes = append(progress.UnconvertedCurrencyCodes, sortedKeys(unconverted)...)
	default:
		cashFlowByMonth, unconverted, err := savingsTargetCashFlowByMonth(tx, converter, agentId, savingsTarget, local)
		if err != nil {
			return nil, err
		}
		for _, cashFlow := range cashFlowByMonth {
			savedAmount.Add(savedAmount, cashFlow)
		}
		progress.UnconvertedCurrencyCodes = append(progress.UnconvertedCurrencyCodes, unconverted...)
		startedOn, startedOnErr := time.Parse(time.DateOnly, savingsTarget.StartedOn)
		firstMonth, secondMonth := monthStart.AddDate(0, -2, 0), monthStart.AddDate(0, -1, 0)
		if savingsTarget.ClosedOn == "" && startedOnErr == nil && !startedOn.After(firstMonth) && !targetOn.Before(monthStart) {
			isFirstShort, _, _ := savingsTargetMonthShortfall(cashFlowByMonth, targetAmount, targetOn, firstMonth)
			isSecondShort, _, _ := savingsTargetMonthShortfall(cashFlowByMonth, targetAmount, targetOn, secondMonth)
			progress.IsBehind = isFirstShort && isSecondShort
		}
	}
	remainingAmount := new(big.Rat).Sub(targetAmount, savedAmount)
	if remainingAmount.Sign() < 0 {
		remainingAmount = new(big.Rat)
	}
	progress.MonthsLeftCount = max(monthsThrough(monthStart, targetOn), 0)
	requiredMonthlyAmount := new(big.Rat).Set(remainingAmount)
	if progress.MonthsLeftCount > 0 {
		requiredMonthlyAmount.Quo(remainingAmount, big.NewRat(int64(progress.MonthsLeftCount), 1))
	}
	progress.SavedAmount = finance.FormatAmount(savedAmount)
	progress.RemainingAmount = finance.FormatAmount(remainingAmount)
	progress.RequiredMonthlyAmount = finance.FormatAmount(requiredMonthlyAmount)
	return progress, nil
}
