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
// that have no budget of their own.
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
	status := &models.BudgetStatus{
		Month: month, AsOf: asOf.Format(time.DateOnly), DayOfMonth: asOf.Day(), DaysInMonth: monthEnd.Day(),
		SpendingCategories: []*models.SpendingCategoryBudgetStatus{},
	}
	budgets, err := tx.BudgetsForMonth(agentId, month)
	if err != nil || len(budgets) == 0 {
		return status, err
	}
	spendingCategories, err := tx.ListSpendingCategories(agentId)
	if err != nil {
		return nil, err
	}
	previousMonth := monthStart.AddDate(0, -1, 0)
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
		row := &models.SpendingCategoryBudgetStatus{
			SpendingCategoryID: budget.SpendingCategoryID, SpendingCategoryName: nameById[budget.SpendingCategoryID],
			BudgetAmount: finance.FormatAmount(budgetAmount), CurrencyCode: budget.CurrencyCode,
			UnconvertedSpending: []*models.CurrencyAmount{},
		}
		unconverted := map[string]*big.Rat{}

		spendingByDay := make([]*big.Rat, status.DayOfMonth)
		for _, day := range thisMonthDays {
			if !counted[day.SpendingCategoryID] || day.SpentOn > status.AsOf {
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
		lastMonthDayBound := min(status.DayOfMonth, previousMonthLastDay)
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
			if isConverted {
				lastMonthSpending.Add(lastMonthSpending, amount)
			}
		}

		fixedChargesDue, fixedChargesSeen, err := fixedCharges(converter, counted, merchantHistory, merchantRecent, month, budget.CurrencyCode, status.AsOf)
		if err != nil {
			return nil, err
		}
		projection := ProjectSpendingCategoryMonth(&SpendingCategoryMonthInput{
			BudgetAmount: budgetAmount, SpendingByDay: spendingByDay,
			FixedChargesDueAmount: fixedChargesDue, FixedChargesSeenAmount: fixedChargesSeen,
			DayOfMonth: status.DayOfMonth, DaysInMonth: status.DaysInMonth,
		})
		row.SpendingAmount = finance.FormatAmount(projection.SpendingAmount)
		row.SpendingBySameDayLastMonthAmount = finance.FormatAmount(lastMonthSpending)
		row.FixedChargesDueAmount = finance.FormatAmount(fixedChargesDue)
		row.ProjectedAmount = finance.FormatAmount(projection.ProjectedAmount)
		row.BudgetPace = projection.BudgetPace
		for _, currencyCode := range sortedKeys(unconverted) {
			row.UnconvertedSpending = append(row.UnconvertedSpending, &models.CurrencyAmount{CurrencyCode: currencyCode, Amount: finance.FormatAmount(unconverted[currencyCode])})
		}
		status.SpendingCategories = append(status.SpendingCategories, row)
	}
	sort.SliceStable(status.SpendingCategories, func(left, right int) bool {
		return status.SpendingCategories[left].SpendingCategoryName < status.SpendingCategories[right].SpendingCategoryName
	})
	return status, nil
}

// fixedCharges is what merchants that charged the counted spending
// categories in each of the three full months before this one are
// expected to charge again (their median, converted at asOf's rate) and
// have not yet, and what those merchants have charged this month already.
// A merchant is one merchant in one currency.
func fixedCharges(converter *rates.Converter, counted map[string]bool, history, recent []*models.MerchantMonthSpending, month, currencyCode, asOf string) (*big.Rat, *big.Rat, error) {
	type merchantKey struct {
		merchantName string
		currencyCode string
	}
	amountsByMerchant := map[merchantKey]map[string]*big.Rat{}
	for _, row := range history {
		if !counted[row.SpendingCategoryID] || row.SpendingMonth == month {
			continue
		}
		amount, err := finance.ParseAmount(row.SpendingAmount)
		if err != nil {
			return nil, nil, err
		}
		key := merchantKey{merchantName: strings.ToLower(strings.TrimSpace(row.MerchantName)), currencyCode: row.CurrencyCode}
		if amountsByMerchant[key] == nil {
			amountsByMerchant[key] = map[string]*big.Rat{}
		}
		if amountsByMerchant[key][row.SpendingMonth] == nil {
			amountsByMerchant[key][row.SpendingMonth] = new(big.Rat)
		}
		amountsByMerchant[key][row.SpendingMonth].Add(amountsByMerchant[key][row.SpendingMonth], amount)
	}
	seenThisMonth := map[merchantKey]string{}
	for _, row := range recent {
		if !counted[row.SpendingCategoryID] || row.SpendingMonth != month {
			continue
		}
		key := merchantKey{merchantName: strings.ToLower(strings.TrimSpace(row.MerchantName)), currencyCode: row.CurrencyCode}
		seenThisMonth[key] = row.SpendingAmount
	}
	due, seen := new(big.Rat), new(big.Rat)
	for key, byMonth := range amountsByMerchant {
		monthly := make([]*big.Rat, 0, len(byMonth))
		for _, amount := range byMonth {
			if amount.Sign() > 0 {
				monthly = append(monthly, amount)
			}
		}
		if len(monthly) < fixedChargeMonths {
			continue
		}
		if seenAmount, isSeen := seenThisMonth[key]; isSeen {
			converted, isConverted, err := convertedAmount(converter, seenAmount, key.currencyCode, currencyCode, asOf)
			if err != nil {
				return nil, nil, err
			}
			if isConverted && converted.Sign() > 0 {
				seen.Add(seen, converted)
			}
			continue
		}
		sort.Slice(monthly, func(left, right int) bool { return monthly[left].Cmp(monthly[right]) < 0 })
		median := monthly[len(monthly)/2]
		converted, isConverted, err := convertedAmount(converter, median.FloatString(finance.AmountDecimalPlaces), key.currencyCode, currencyCode, asOf)
		if err != nil {
			return nil, nil, err
		}
		if isConverted {
			due.Add(due, converted)
		}
	}
	return due, seen, nil
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
	value, err := finance.ParseAmount(converted)
	return value, err == nil, err
}

func addTo(sums map[string]*big.Rat, currencyCode, amount string) {
	value, err := finance.ParseAmount(amount)
	if err != nil {
		return
	}
	if sums[currencyCode] == nil {
		sums[currencyCode] = new(big.Rat)
	}
	sums[currencyCode].Add(sums[currencyCode], value)
}

// displayAmount is an amount as a person reads it: two places.
func displayAmount(value *big.Rat) string {
	return value.FloatString(2)
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
	fetcher := self.exchangeRateFetcher()
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		mutes, err := tx.ListAgentAlertMutes(agentId)
		if err != nil {
			return err
		}
		if mutedBy(mutes, &alertFacts{kinds: []string{models.AlertKindBudget}}) != nil {
			return nil
		}
		status, err := BudgetStatus(ctx, tx, fetcher, agentId, month, today)
		if err != nil {
			return err
		}
		isWritten := false
		for _, row := range status.SpendingCategories {
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
				CandidateReason: budgetCrossingReason(row, crossing, status),
				BudgetKey:       spendingCategoryBudgetKey(row.SpendingCategoryID, month, crossing),
			}); err != nil {
				return err
			}
			isWritten = true
		}
		behind, err := savingsTargetsBehind(tx, rates.NewConverter(ctx, fetcher, tx), agentId, local)
		if err != nil {
			return err
		}
		for _, target := range behind {
			budgetKey := "savings-target:" + target.savingsTarget.ID + ":" + month + ":" + savingsTargetCrossingBehind
			isToldAlready, err := tx.HasAgentBudgetAlert(agentId, budgetKey)
			if err != nil {
				return err
			}
			if isToldAlready {
				continue
			}
			if _, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{
				AgentID: agentId, CandidateKind: models.AlertCandidateBudget, AlertSignal: models.AlertSignalSoon,
				CandidateReason: target.reason, BudgetKey: budgetKey,
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

// budgetCrossingOf is the most severe crossing a spending category is
// past this month, or empty.
func budgetCrossingOf(row *models.SpendingCategoryBudgetStatus) string {
	switch row.BudgetPace {
	case models.BudgetPaceOver:
		return budgetCrossingOver
	case models.BudgetPaceAtRisk:
		return budgetCrossingAtRisk
	}
	budget, budgetErr := finance.ParseAmount(row.BudgetAmount)
	spending, spendingErr := finance.ParseAmount(row.SpendingAmount)
	if budgetErr != nil || spendingErr != nil || budget.Sign() <= 0 {
		return ""
	}
	if spending.Cmp(new(big.Rat).Mul(budget, budgetAlertShare)) >= 0 {
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
func budgetCrossingReason(row *models.SpendingCategoryBudgetStatus, crossing string, status *models.BudgetStatus) string {
	budget, _ := finance.ParseAmount(row.BudgetAmount)
	spending, _ := finance.ParseAmount(row.SpendingAmount)
	projected, _ := finance.ParseAmount(row.ProjectedAmount)
	share := 0
	if budget != nil && budget.Sign() > 0 && spending != nil {
		percent, _ := new(big.Rat).Quo(new(big.Rat).Mul(spending, big.NewRat(100, 1)), budget).Float64()
		share = int(percent)
	}
	what := ""
	switch crossing {
	case budgetCrossingOver:
		what = "is over its monthly budget"
	case budgetCrossingAtRisk:
		what = "is heading past its monthly budget"
	default:
		what = "has used most of its monthly budget"
	}
	reason := fmt.Sprintf("Spending category %q %s: %s of %s %s spent (%d%%) by day %d of %d; the month is projected to end at %s %s.",
		row.SpendingCategoryName, what, displayAmount(ratOrZero(spending)), displayAmount(ratOrZero(budget)), row.CurrencyCode, share,
		status.DayOfMonth, status.DaysInMonth, displayAmount(ratOrZero(projected)), row.CurrencyCode)
	if fixedDue, err := finance.ParseAmount(row.FixedChargesDueAmount); err == nil && fixedDue.Sign() > 0 {
		reason += fmt.Sprintf(" That counts %s %s of regular charges still expected this month.", displayAmount(fixedDue), row.CurrencyCode)
	}
	if lastMonth, err := finance.ParseAmount(row.SpendingBySameDayLastMonthAmount); err == nil {
		reason += fmt.Sprintf(" By the same day last month it was %s %s.", displayAmount(lastMonth), row.CurrencyCode)
	}
	return reason
}

// savingsTargetBehind is a savings target that fell behind, with the
// candidate's line.
type savingsTargetBehind struct {
	savingsTarget *models.SavingsTarget
	reason        string
}

// savingsTargetsBehind is the open savings targets measured by cash flow
// whose last two full months both saved less than the pace they needed:
// what remained at the start of each month, over the months left to the
// target day. A target started after the first of those two months has
// not had two full months yet. Targets measured by asset value are not
// judged here: their valuations do not arrive with a sync.
//
// A month's cash flow is money in less money out, transfers left out,
// per currency, converted at the rate of the month's last day.
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
		rows, err := tx.FinanceSpendingSummary(agentId, &db.FinanceSpendingSummaryFilter{
			From: savingsTarget.StartedOn, To: monthStart.AddDate(0, 0, -1).Format(time.DateOnly),
			GroupBy: models.FinanceSpendingSummaryGroupByMonth,
		})
		if err != nil {
			return nil, err
		}
		cashFlowByMonth := map[string]*big.Rat{}
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
			net := new(big.Rat).Sub(moneyIn, moneyOut)
			converted, isConverted, err := convertedAmount(converter, net.FloatString(finance.AmountDecimalPlaces), row.CurrencyCode, savingsTarget.CurrencyCode, rowMonth.AddDate(0, 1, -1).Format(time.DateOnly))
			if err != nil {
				return nil, err
			}
			if !isConverted {
				continue
			}
			if cashFlowByMonth[row.GroupKey] == nil {
				cashFlowByMonth[row.GroupKey] = new(big.Rat)
			}
			cashFlowByMonth[row.GroupKey].Add(cashFlowByMonth[row.GroupKey], converted)
		}
		isShort := func(month time.Time) (bool, *big.Rat, *big.Rat) {
			savedBefore := new(big.Rat)
			for key, cashFlow := range cashFlowByMonth {
				if key < month.Format("2006-01") {
					savedBefore.Add(savedBefore, cashFlow)
				}
			}
			remaining := new(big.Rat).Sub(targetAmount, savedBefore)
			monthsLeft := (targetOn.Year()-month.Year())*12 + int(targetOn.Month()) - int(month.Month()) + 1
			required := new(big.Rat).Quo(remaining, big.NewRat(int64(max(monthsLeft, 1)), 1))
			saved := ratOrZero(cashFlowByMonth[month.Format("2006-01")])
			return remaining.Sign() > 0 && saved.Cmp(required) < 0, saved, required
		}
		isFirstShort, firstSaved, firstRequired := isShort(firstMonth)
		isSecondShort, secondSaved, secondRequired := isShort(secondMonth)
		if !isFirstShort || !isSecondShort {
			continue
		}
		behind = append(behind, &savingsTargetBehind{
			savingsTarget: savingsTarget,
			reason: fmt.Sprintf("Savings target %q (%s %s by %s) is behind: %s saved %s %s against the %s %s it needed, and %s saved %s %s against %s %s.",
				savingsTarget.SavingsTargetName, displayAmount(targetAmount), savingsTarget.CurrencyCode, savingsTarget.TargetOn,
				firstMonth.Format("January"), displayAmount(firstSaved), savingsTarget.CurrencyCode, displayAmount(firstRequired), savingsTarget.CurrencyCode,
				secondMonth.Format("January"), displayAmount(secondSaved), savingsTarget.CurrencyCode, displayAmount(secondRequired), savingsTarget.CurrencyCode),
		})
	}
	return behind, nil
}
