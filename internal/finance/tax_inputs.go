package finance

import (
	"math/big"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// What the estimate takes besides paystubs, W-2s, 1099s and 1098s:
//
//   - A preview of the year's interest and dividends from the person's
//     linked accounts, for a year whose 1099s have not come: what the
//     accounts were paid so far, projected to December 31 at the same
//     pace, leaving out accounts whose income is not taxed (IRAs, Roth
//     accounts, 401(k)s, HSAs, 529s). A 1099 of the kind replaces it, and
//     dividends count as ordinary until a 1099-DIV says which are
//     qualified.
//   - The gains realized so far, as the person or the agent gave them
//     from a brokerage, for a year whose 1099-Bs have not come.
//   - Form 1098 and the year's settings, for itemizing: mortgage interest
//     and points, state and local taxes up to the year's cap, charitable
//     gifts; the larger of that and the standard deduction is taken.
//   - The child tax credit and the credit for other dependents, phased
//     out above the filing status's threshold, and the child and
//     dependent care credit, on care paid less what an employer's
//     dependent care plan covered, up to the lower earner's pay.
//   - Other household income, such as a spouse's pay no W-2 here shows,
//     taxed as wages are and counted as the spouse's earned income.

// TaxYearInputs are what the estimate takes besides the documents.
type TaxYearInputs struct {
	// Setting is the year's settings, or nil.
	Setting *models.TaxYearSetting
	// AccountIncomes are the interest and dividends the linked accounts
	// were paid in the year so far (all of it, for a year that is over).
	AccountIncomes []*models.AccountInvestmentIncome
}

// TaxYearAccountIncome is one account's interest or dividends in the
// preview.
type TaxYearAccountIncome struct {
	FinanceAccountID string `json:"financeAccountId"`
	AccountName      string `json:"accountName"`
	// IncomeKind is "interest" or "dividend".
	IncomeKind string `json:"incomeKind"`
	Amount     string `json:"amount"`
	// IsTaxAdvantaged says the account's income is not taxed (an IRA, a
	// Roth account, a 401(k), an HSA, a 529), so the preview leaves it out.
	IsTaxAdvantaged bool `json:"isTaxAdvantaged"`
}

// TaxYearInvestmentPreview is the year's interest and dividends as the
// linked accounts were paid them. Amounts are dollars with cents.
type TaxYearInvestmentPreview struct {
	// InterestAmount and DividendsAmount are the taxed accounts' so far;
	// the projected ones to December 31 at the same pace (the same for a
	// year that is over).
	InterestAmount           string `json:"interestAmount"`
	DividendsAmount          string `json:"dividendsAmount"`
	ProjectedInterestAmount  string `json:"projectedInterestAmount"`
	ProjectedDividendsAmount string `json:"projectedDividendsAmount"`
	// IsInterestCounted and IsDividendsCounted say the estimate counts
	// them: no 1099-INT, or no 1099-DIV, has been read for the year.
	IsInterestCounted  bool `json:"isInterestCounted"`
	IsDividendsCounted bool `json:"isDividendsCounted"`
	// TaxAdvantagedAmount is what accounts whose income is not taxed were
	// paid, left out.
	TaxAdvantagedAmount   string                  `json:"taxAdvantagedAmount"`
	TaxYearAccountIncomes []*TaxYearAccountIncome `json:"taxYearAccountIncomes"`
}

// taxAdvantagedSubtypes are the provider account subtypes whose interest
// and dividends are not taxed in the year they are paid.
var taxAdvantagedSubtypes = map[string]bool{
	"ira": true, "roth": true, "roth ira": true, "sep ira": true, "simple ira": true, "sarsep": true, "keogh": true,
	"401a": true, "401k": true, "403b": true, "457b": true, "roth 401k": true, "roth 403b": true, "thrift savings plan": true,
	"pension": true, "retirement": true, "profit sharing plan": true, "non-taxable brokerage account": true,
	"hsa": true, "health reimbursement arrangement": true, "529": true, "education savings account": true,
}

// IsTaxAdvantagedSubtype says an account subtype's income is not taxed.
func IsTaxAdvantagedSubtype(subtype string) bool {
	return taxAdvantagedSubtypes[strings.ToLower(strings.TrimSpace(subtype))]
}

// previewSums are the preview while being added up.
type previewSums struct {
	interest, dividends, taxAdvantaged *big.Rat
	accountIncomes                     []*TaxYearAccountIncome
}

// sumAccountIncomes adds the accounts' income up, taxed and not.
func sumAccountIncomes(incomes []*models.AccountInvestmentIncome) *previewSums {
	sums := &previewSums{interest: new(big.Rat), dividends: new(big.Rat), taxAdvantaged: new(big.Rat), accountIncomes: []*TaxYearAccountIncome{}}
	for _, income := range incomes {
		if income == nil {
			continue
		}
		amountValue, err := parseDecimal(income.Amount)
		if err != nil {
			continue
		}
		isTaxAdvantaged := IsTaxAdvantagedSubtype(income.AccountSubtype)
		sums.accountIncomes = append(sums.accountIncomes, &TaxYearAccountIncome{
			FinanceAccountID: income.FinanceAccountID, AccountName: income.AccountName, IncomeKind: income.IncomeKind,
			Amount: formatDollars(amountValue), IsTaxAdvantaged: isTaxAdvantaged,
		})
		switch {
		case isTaxAdvantaged:
			sums.taxAdvantaged.Add(sums.taxAdvantaged, amountValue)
		case income.IncomeKind == "dividend":
			sums.dividends.Add(sums.dividends, amountValue)
		default:
			sums.interest.Add(sums.interest, amountValue)
		}
	}
	return sums
}

// yearProjection is how much a year's income so far is multiplied by to
// reach December 31: the days in the year over the days gone, 1 for a year
// that is over and 0 for one not begun.
func yearProjection(taxYear int, today time.Time) *big.Rat {
	start := time.Date(taxYear, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(taxYear+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if !day.Before(end) {
		return big.NewRat(1, 1)
	}
	if day.Before(start) {
		return new(big.Rat)
	}
	gone := int64(day.Sub(start).Hours()/24) + 1
	return big.NewRat(int64(end.Sub(start).Hours()/24), gone)
}

// childTaxCreditByYear is the credit for each child under 17, before the
// phase-out; the credit for another dependent is 500.
var childTaxCreditByYear = map[int]int64{2022: 2000, 2023: 2000, 2024: 2000, 2025: 2200, 2026: 2200}

// childTaxCreditThresholdByStatus is the modified adjusted gross income
// above which the child tax credit goes down by 50 for each 1,000 or part
// of it; it is not indexed.
var childTaxCreditThresholdByStatus = map[TaxFilingStatus]int64{
	TaxFilingStatusSingle: 200000, TaxFilingStatusMarriedFilingJointly: 400000, TaxFilingStatusMarriedFilingSeparately: 200000, TaxFilingStatusHeadOfHousehold: 200000,
}

// childTaxCredit is the child tax credit and the credit for other
// dependents together, after the phase-out.
func childTaxCredit(taxYear int, status TaxFilingStatus, childCount, otherDependentCount int, adjustedGrossIncome *big.Rat) *big.Rat {
	perChild, isKnown := childTaxCreditByYear[taxYear]
	if !isKnown || childCount+otherDependentCount == 0 {
		return new(big.Rat)
	}
	credit := big.NewRat(perChild*int64(childCount)+500*int64(otherDependentCount), 1)
	// 50 for each 1,000 above, a part of 1,000 counting whole.
	thousands := partsAbove(adjustedGrossIncome, childTaxCreditThresholdByStatus[status], 1000)
	credit.Sub(credit, big.NewRat(50*thousands, 1))
	if credit.Sign() < 0 {
		return new(big.Rat)
	}
	return credit
}

// dependentCareCredit is the child and dependent care credit: the care
// paid, up to 3,000 for one child or 6,000 for more, less what an
// employer's plan covered, and up to the lower earner's earned income, at
// the year's rate for the adjusted gross income.
func dependentCareCredit(taxYear int, status TaxFilingStatus, childCount int, expense, employerCovered, lowerEarnedIncome, adjustedGrossIncome *big.Rat) *big.Rat {
	if childCount == 0 || expense.Sign() <= 0 {
		return new(big.Rat)
	}
	limit := big.NewRat(3000, 1)
	if childCount > 1 {
		limit = big.NewRat(6000, 1)
	}
	limit.Sub(limit, employerCovered)
	allowed := new(big.Rat).Set(expense)
	for _, bound := range []*big.Rat{limit, lowerEarnedIncome} {
		if bound.Cmp(allowed) < 0 {
			allowed = new(big.Rat).Set(bound)
		}
	}
	if allowed.Sign() <= 0 {
		return new(big.Rat)
	}
	return roundToCents(new(big.Rat).Mul(allowed, dependentCareRate(taxYear, status, adjustedGrossIncome)))
}

// dependentCareRate is the share of care paid the credit is: 35% down by a
// point for each 2,000 of income above 15,000, to 20%; from 2026 50% down
// to 35% the same way, then down by a point for each 2,000 (4,000 filing
// jointly) above 75,000 (150,000 filing jointly), to 20%.
func dependentCareRate(taxYear int, status TaxFilingStatus, adjustedGrossIncome *big.Rat) *big.Rat {
	stepsAbove := func(floor, step int64) int64 {
		return partsAbove(adjustedGrossIncome, floor, step)
	}
	if taxYear < 2026 {
		return big.NewRat(max(35-stepsAbove(15000, 2000), 20), 100)
	}
	percent := max(50-stepsAbove(15000, 2000), 35)
	floor, step := int64(75000), int64(2000)
	if status == TaxFilingStatusMarriedFilingJointly {
		floor, step = 150000, 4000
	}
	return big.NewRat(max(percent-stepsAbove(floor, step), 20), 100)
}

// saltCap is the most state and local taxes itemizing counts: 10,000
// (5,000 filing separately) until 2025; from 2025 40,000, then 40,400,
// going down by 30% of the modified adjusted gross income above 500,000
// (505,000), not below 10,000; half of each filing separately.
func saltCap(taxYear int, status TaxFilingStatus, adjustedGrossIncome *big.Rat) *big.Rat {
	isSeparate := status == TaxFilingStatusMarriedFilingSeparately
	halve := func(amount int64) int64 {
		if isSeparate {
			return amount / 2
		}
		return amount
	}
	var cap, threshold int64
	switch {
	case taxYear < 2025:
		return big.NewRat(halve(10000), 1)
	case taxYear == 2025:
		cap, threshold = 40000, 500000
	default:
		cap, threshold = 40400, 505000
	}
	result := big.NewRat(halve(cap), 1)
	if above := new(big.Rat).Sub(adjustedGrossIncome, big.NewRat(halve(threshold), 1)); above.Sign() > 0 {
		result.Sub(result, new(big.Rat).Mul(above, big.NewRat(3, 10)))
	}
	if floor := big.NewRat(halve(10000), 1); result.Cmp(floor) < 0 {
		result = floor
	}
	return result
}

// acquisitionDebtLimit is the most mortgage debt whose interest a return
// deducts: 750,000 (375,000 filing separately), or up to 1,000,000
// (500,000) of debt taken out before December 16, 2017, which also uses
// up the 750,000 (IRS Publication 936).
func acquisitionDebtLimit(status TaxFilingStatus, grandfatheredPrincipal *big.Rat) *big.Rat {
	limit, grandfatheredLimit := big.NewRat(750000, 1), big.NewRat(1000000, 1)
	if status == TaxFilingStatusMarriedFilingSeparately {
		limit, grandfatheredLimit = big.NewRat(375000, 1), big.NewRat(500000, 1)
	}
	grandfathered := new(big.Rat).Set(grandfatheredPrincipal)
	if grandfathered.Cmp(grandfatheredLimit) > 0 {
		grandfathered = grandfatheredLimit
	}
	if grandfathered.Cmp(limit) > 0 {
		return grandfathered
	}
	return limit
}

// grandfatheredBefore is the day from which a mortgage's debt counts
// against the 750,000 limit rather than the 1,000,000 one.
var grandfatheredBefore = time.Date(2017, time.December, 16, 0, 0, 0, 0, time.UTC)

// originationDate is a 1098's box 3, as the lender printed it.
func originationDate(text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	for _, layout := range []string{"2006-01-02", "1/2/2006", "01/02/2006", "1/2/06", "01/02/06"} {
		if day, err := time.Parse(layout, text); err == nil {
			return day, true
		}
	}
	return time.Time{}, false
}

// mortgageSums are what a year's 1098s give the estimate.
type mortgageSums struct {
	// interest is the interest and points the return deducts, scaled
	// down when the loans' principal is more than the limit.
	interest *big.Rat
	// printedInterest is boxes 1 and 6 as printed, before the limit.
	printedInterest *big.Rat
	// propertyTax is the real estate taxes lenders paid from escrow.
	propertyTax *big.Rat
	// refundedInterest is box 4, interest overpaid in an earlier year and
	// paid back in this one, which is income when it was deducted then.
	refundedInterest *big.Rat
	// principal is the loans' outstanding principal (box 2) together,
	// and debtLimit the most of it whose interest is deducted.
	principal, debtLimit *big.Rat
	// isOriginationUnknown: a loan over the limit printed no origination
	// date, so it is counted as taken out after 2017.
	isOriginationUnknown bool
	taxFormIds           []string
}

// isOverLimit is whether the loans' principal is more than the limit.
func (self *mortgageSums) isOverLimit() bool {
	return self.principal.Cmp(self.debtLimit) > 0
}

// sumMortgages adds up a year's 1098s. One loan's statement counts once:
// co-borrowers each get a copy, and a statement may be read twice, so
// forms of one lender, loan number and principal are one loan, of them a
// correction over the form it corrects, else the latest read. Box 10
// counts only when its label says it is a tax, since lenders print
// principal paid and insurance there as well.
func sumMortgages(taxYear int, status TaxFilingStatus, taxForms []*models.FinanceTaxForm) *mortgageSums {
	sums := &mortgageSums{
		interest: new(big.Rat), printedInterest: new(big.Rat), propertyTax: new(big.Rat), refundedInterest: new(big.Rat),
		principal: new(big.Rat), taxFormIds: []string{},
	}
	latestByKey := map[string]*models.FinanceTaxForm{}
	var keys []string
	for _, taxForm := range taxForms {
		if taxForm == nil || taxForm.TaxYear != taxYear || taxForm.TaxFormKind != models.TaxFormKind1098 {
			continue
		}
		principal, _ := taxFormAmount(taxForm, "2")
		payer := strings.ToLower(strings.TrimSpace(taxForm.PayerTaxID))
		if payer == "" {
			payer = strings.ToLower(strings.Join(strings.Fields(taxForm.PayerName), " "))
		}
		key := payer + "\x00" + TaxFormAccount(taxForm) + "\x00" + principal.RatString()
		earlier := latestByKey[key]
		if earlier == nil {
			keys = append(keys, key)
		}
		switch {
		case earlier == nil, taxForm.IsCorrected && !earlier.IsCorrected:
			latestByKey[key] = taxForm
		case taxForm.IsCorrected == earlier.IsCorrected && !taxForm.CreatedAt.Before(earlier.CreatedAt):
			latestByKey[key] = taxForm
		}
	}
	grandfathered := new(big.Rat)
	for _, key := range keys {
		taxForm := latestByKey[key]
		mortgageInterest, _ := taxFormAmount(taxForm, "1")
		points, _ := taxFormAmount(taxForm, "6")
		refunded, _ := taxFormAmount(taxForm, "4")
		principal, _ := taxFormAmount(taxForm, "2")
		sums.printedInterest.Add(sums.printedInterest, mortgageInterest).Add(sums.printedInterest, points)
		sums.refundedInterest.Add(sums.refundedInterest, refunded)
		sums.principal.Add(sums.principal, principal)
		origination, isKnown := originationDate(taxFormText(taxForm, "3"))
		if isKnown && origination.Before(grandfatheredBefore) {
			grandfathered.Add(grandfathered, principal)
		} else if !isKnown && principal.Sign() > 0 {
			sums.isOriginationUnknown = true
		}
		for _, entry := range taxForm.TaxFormEntries {
			if entry == nil || entry.BoxCode != "10" || !strings.Contains(strings.ToLower(entry.BoxQualifier), "tax") {
				continue
			}
			if amountValue, err := parseDecimal(entry.Amount); err == nil {
				sums.propertyTax.Add(sums.propertyTax, amountValue)
			}
		}
		sums.taxFormIds = append(sums.taxFormIds, taxForm.ID)
	}
	sums.debtLimit = acquisitionDebtLimit(status, grandfathered)
	sums.interest.Set(sums.printedInterest)
	if sums.isOverLimit() {
		// Interest in the share of the debt under the limit. Publication
		// 936 divides by the year's average balance; box 2 is the balance
		// at the start of the year, close to it for a loan a year old.
		sums.interest = roundToCents(new(big.Rat).Mul(sums.printedInterest, new(big.Rat).Quo(sums.debtLimit, sums.principal)))
	} else {
		sums.isOriginationUnknown = false
	}
	return sums
}

// taxFormText is a box's text, or "".
func taxFormText(taxForm *models.FinanceTaxForm, boxCode string) string {
	for _, entry := range taxForm.TaxFormEntries {
		if entry != nil && entry.BoxCode == boxCode {
			return entry.TextValue
		}
	}
	return ""
}

// partsAbove is how many steps of a size an amount is above a floor, a
// part of a step counting whole ("or fraction thereof").
func partsAbove(amountValue *big.Rat, floor, step int64) int64 {
	above := new(big.Rat).Sub(amountValue, big.NewRat(floor, 1))
	if above.Sign() <= 0 {
		return 0
	}
	steps := new(big.Rat).Quo(above, big.NewRat(step, 1))
	whole := new(big.Int).Quo(steps.Num(), steps.Denom())
	if !steps.IsInt() {
		whole.Add(whole, big.NewInt(1))
	}
	return whole.Int64()
}

// settingAmount is one of the settings' amounts, nothing when empty or
// not a decimal.
func settingAmount(text string) *big.Rat {
	if amountValue, err := parseDecimal(text); err == nil {
		return amountValue
	}
	return new(big.Rat)
}
