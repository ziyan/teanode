package finance

import (
	"math/big"

	"github.com/ziyan/teanode/internal/models"
)

// A tax year's estimate sets the tax the year's income owes beside what
// was withheld, for the ring that shows a year's income as the tax already
// paid, the tax still to come out of paychecks, what will be owed at
// filing, and what is left. Income is wages and what the year's 1099s
// report (see tax_investment.go); itemized deductions, credits and other
// income are not on those forms, so the federal figure is the tax on that
// income less the standard deduction. Social Security and Medicare are
// exact. State and local income tax are taken as withheld, since each
// state's rules are its own, so a state's tax on investment income is not
// counted.

// TaxFilingStatus is how a return is filed.
type TaxFilingStatus string

const (
	TaxFilingStatusSingle                  TaxFilingStatus = "single"
	TaxFilingStatusMarriedFilingJointly    TaxFilingStatus = "married_filing_jointly"
	TaxFilingStatusMarriedFilingSeparately TaxFilingStatus = "married_filing_separately"
	TaxFilingStatusHeadOfHousehold         TaxFilingStatus = "head_of_household"
)

// TaxFilingStatuses are every filing status, in the order they are
// offered.
var TaxFilingStatuses = []TaxFilingStatus{
	TaxFilingStatusSingle, TaxFilingStatusMarriedFilingJointly, TaxFilingStatusMarriedFilingSeparately, TaxFilingStatusHeadOfHousehold,
}

// IsValid says the filing status is one of TaxFilingStatuses, the ones
// an agent can keep (models.TaxFilingStatuses).
func (self TaxFilingStatus) IsValid() bool {
	return models.IsTaxFilingStatus(string(self))
}

// TaxYearEstimate is a year's tax on its wages beside what was withheld.
// Amounts are dollars with cents, the household's, to December 31 when
// the year is projected.
type TaxYearEstimate struct {
	// TaxFilingStatus is the one asked for, else the one the person
	// keeps, else married filing jointly when the documents name two
	// people and single otherwise (IsTaxFilingStatusAssumed). Filing
	// separately, or single with two people, is a return each.
	TaxFilingStatus          TaxFilingStatus `json:"taxFilingStatus"`
	IsTaxFilingStatusAssumed bool            `json:"isTaxFilingStatusAssumed"`
	IsProjected              bool            `json:"isProjected"`
	// PayAmount is Medicare wages (box 5): pay before income tax,
	// retirement savings included, pre-tax health benefits not; box 1 for
	// documents that print no Medicare wages. InvestmentIncomeAmount is
	// interest, ordinary dividends and the capital result from 1099s (a
	// net loss counted only up to the limit), and IncomeAmount the two
	// together, the ring's whole.
	PayAmount              string `json:"payAmount"`
	InvestmentIncomeAmount string `json:"investmentIncomeAmount"`
	IncomeAmount           string `json:"incomeAmount"`
	// AdjustedGrossIncomeAmount is wages (box 1) and investment income.
	AdjustedGrossIncomeAmount string `json:"adjustedGrossIncomeAmount"`
	StandardDeductionAmount   string `json:"standardDeductionAmount"`
	TaxableIncomeAmount       string `json:"taxableIncomeAmount"`
	// PreferentialIncomeAmount is the part of taxable income taxed at the
	// lower rates (qualified dividends and net long-term gain), and
	// PreferentialIncomeTaxAmount that tax, part of the federal income
	// tax. CapitalLossDeductionAmount is a net loss taken against other
	// income, ForeignTaxCreditAmount the foreign tax taken off the
	// federal income tax.
	PreferentialIncomeAmount    string `json:"preferentialIncomeAmount"`
	PreferentialIncomeTaxAmount string `json:"preferentialIncomeTaxAmount"`
	CapitalLossDeductionAmount  string `json:"capitalLossDeductionAmount"`
	ForeignTaxCreditAmount      string `json:"foreignTaxCreditAmount"`
	// NetInvestmentIncomeTaxAmount is the 3.8% tax on investment income
	// above the filing status's threshold.
	NetInvestmentIncomeTaxAmount string `json:"netInvestmentIncomeTaxAmount"`
	// IsInvestmentIncomePreviewed says investment income counted comes
	// from the linked accounts' transactions or the gains given in the
	// settings rather than from 1099s.
	IsInvestmentIncomePreviewed bool `json:"isInvestmentIncomePreviewed"`
	// OtherIncomeAmount is the settings' other household income, part of
	// adjusted gross income.
	OtherIncomeAmount string `json:"otherIncomeAmount"`
	// DeductionAmount is the deduction taken, the larger of the standard
	// deduction and ItemizedDeductionAmount (IsItemized when that one):
	// mortgage interest and points from 1098s, state and local taxes up
	// to the year's cap, and charitable gifts.
	DeductionAmount                 string `json:"deductionAmount"`
	IsItemized                      bool   `json:"isItemized"`
	ItemizedDeductionAmount         string `json:"itemizedDeductionAmount"`
	MortgageInterestAmount          string `json:"mortgageInterestAmount"`
	StateAndLocalTaxDeductionAmount string `json:"stateAndLocalTaxDeductionAmount"`
	CharitableContributionAmount    string `json:"charitableContributionAmount"`
	// ChildTaxCreditAmount is the child tax credit and the credit for
	// other dependents; DependentCareCreditAmount the child and dependent
	// care credit. Both are taken off the federal income tax, never below
	// nothing.
	ChildTaxCreditAmount      string `json:"childTaxCreditAmount"`
	DependentCareCreditAmount string `json:"dependentCareCreditAmount"`
	// The tax owed for the year, each kind, and all of it.
	FederalIncomeTaxAmount  string `json:"federalIncomeTaxAmount"`
	SocialSecurityTaxAmount string `json:"socialSecurityTaxAmount"`
	MedicareTaxAmount       string `json:"medicareTaxAmount"`
	StateIncomeTaxAmount    string `json:"stateIncomeTaxAmount"`
	LocalIncomeTaxAmount    string `json:"localIncomeTaxAmount"`
	TotalTaxAmount          string `json:"totalTaxAmount"`
	// PaidTaxAmount is the tax withheld through the latest paystub or W-2;
	// UpcomingWithheldTaxAmount what the rest of the year's paychecks will
	// withhold at the same pace.
	PaidTaxAmount             string `json:"paidTaxAmount"`
	UpcomingWithheldTaxAmount string `json:"upcomingWithheldTaxAmount"`
	// OwingTaxAmount is due at filing when the year withholds less than it
	// owes, RefundAmount comes back when it withholds more; one of them is
	// "0.00".
	OwingTaxAmount string `json:"owingTaxAmount"`
	RefundAmount   string `json:"refundAmount"`
	// RemainingIncomeAmount is income less what was withheld or is owed,
	// whichever is more: what the person keeps before any refund. The
	// ring's slices, paid, upcoming, owing and remaining, add up to
	// income.
	RemainingIncomeAmount string `json:"remainingIncomeAmount"`
}

// federalTaxTable is a year's standard deductions and brackets by filing
// status: each bracket's rate applies to taxable income above its floor.
type federalTaxTable struct {
	standardDeductionByStatus map[TaxFilingStatus]int64
	// bracketFloorsByStatus are the floors of the 12%, 22%, 24%, 32%, 35%
	// and 37% brackets; 10% starts at 0.
	bracketFloorsByStatus map[TaxFilingStatus][6]int64
}

var federalBracketRates = []*big.Rat{
	big.NewRat(10, 100), big.NewRat(12, 100), big.NewRat(22, 100), big.NewRat(24, 100), big.NewRat(32, 100), big.NewRat(35, 100), big.NewRat(37, 100),
}

// federalTaxTableByYear is as the IRS publishes each autumn for the year
// after (Rev. Proc. 2021-45, 2022-38, 2023-34, 2024-40, 2025-32; 2025's standard
// deduction as Public Law 119-21 raised it).
var federalTaxTableByYear = map[int]*federalTaxTable{
	2022: {
		standardDeductionByStatus: map[TaxFilingStatus]int64{TaxFilingStatusSingle: 12950, TaxFilingStatusMarriedFilingJointly: 25900, TaxFilingStatusMarriedFilingSeparately: 12950, TaxFilingStatusHeadOfHousehold: 19400},
		bracketFloorsByStatus: map[TaxFilingStatus][6]int64{
			TaxFilingStatusSingle:                  {10275, 41775, 89075, 170050, 215950, 539900},
			TaxFilingStatusMarriedFilingJointly:    {20550, 83550, 178150, 340100, 431900, 647850},
			TaxFilingStatusMarriedFilingSeparately: {10275, 41775, 89075, 170050, 215950, 323925},
			TaxFilingStatusHeadOfHousehold:         {14650, 55900, 89050, 170050, 215950, 539900},
		},
	},
	2023: {
		standardDeductionByStatus: map[TaxFilingStatus]int64{TaxFilingStatusSingle: 13850, TaxFilingStatusMarriedFilingJointly: 27700, TaxFilingStatusMarriedFilingSeparately: 13850, TaxFilingStatusHeadOfHousehold: 20800},
		bracketFloorsByStatus: map[TaxFilingStatus][6]int64{
			TaxFilingStatusSingle:                  {11000, 44725, 95375, 182100, 231250, 578125},
			TaxFilingStatusMarriedFilingJointly:    {22000, 89450, 190750, 364200, 462500, 693750},
			TaxFilingStatusMarriedFilingSeparately: {11000, 44725, 95375, 182100, 231250, 346875},
			TaxFilingStatusHeadOfHousehold:         {15700, 59850, 95350, 182100, 231250, 578100},
		},
	},
	2024: {
		standardDeductionByStatus: map[TaxFilingStatus]int64{TaxFilingStatusSingle: 14600, TaxFilingStatusMarriedFilingJointly: 29200, TaxFilingStatusMarriedFilingSeparately: 14600, TaxFilingStatusHeadOfHousehold: 21900},
		bracketFloorsByStatus: map[TaxFilingStatus][6]int64{
			TaxFilingStatusSingle:                  {11600, 47150, 100525, 191950, 243725, 609350},
			TaxFilingStatusMarriedFilingJointly:    {23200, 94300, 201050, 383900, 487450, 731200},
			TaxFilingStatusMarriedFilingSeparately: {11600, 47150, 100525, 191950, 243725, 365600},
			TaxFilingStatusHeadOfHousehold:         {16550, 63100, 100500, 191950, 243700, 609350},
		},
	},
	2025: {
		standardDeductionByStatus: map[TaxFilingStatus]int64{TaxFilingStatusSingle: 15750, TaxFilingStatusMarriedFilingJointly: 31500, TaxFilingStatusMarriedFilingSeparately: 15750, TaxFilingStatusHeadOfHousehold: 23625},
		bracketFloorsByStatus: map[TaxFilingStatus][6]int64{
			TaxFilingStatusSingle:                  {11925, 48475, 103350, 197300, 250525, 626350},
			TaxFilingStatusMarriedFilingJointly:    {23850, 96950, 206700, 394600, 501050, 751600},
			TaxFilingStatusMarriedFilingSeparately: {11925, 48475, 103350, 197300, 250525, 375800},
			TaxFilingStatusHeadOfHousehold:         {17000, 64850, 103350, 197300, 250500, 626350},
		},
	},
	2026: {
		standardDeductionByStatus: map[TaxFilingStatus]int64{TaxFilingStatusSingle: 16100, TaxFilingStatusMarriedFilingJointly: 32200, TaxFilingStatusMarriedFilingSeparately: 16100, TaxFilingStatusHeadOfHousehold: 24150},
		bracketFloorsByStatus: map[TaxFilingStatus][6]int64{
			TaxFilingStatusSingle:                  {12400, 50400, 105700, 201775, 256225, 640600},
			TaxFilingStatusMarriedFilingJointly:    {24800, 100800, 211400, 403550, 512450, 768700},
			TaxFilingStatusMarriedFilingSeparately: {12400, 50400, 105700, 201775, 256225, 384350},
			TaxFilingStatusHeadOfHousehold:         {17700, 67450, 105700, 201750, 256200, 640600},
		},
	},
}

// additionalMedicareThresholdByStatus is the wages above which a return
// owes the 0.9% additional Medicare tax. An employer withholds it above
// $200,000 whatever the status, so a couple can owe more or less than was
// withheld.
var additionalMedicareThresholdByStatus = map[TaxFilingStatus]int64{
	TaxFilingStatusSingle: 200000, TaxFilingStatusMarriedFilingJointly: 250000, TaxFilingStatusMarriedFilingSeparately: 125000, TaxFilingStatusHeadOfHousehold: 200000,
}

// federalIncomeTax is the tax on taxable income by the year's brackets.
func federalIncomeTax(table *federalTaxTable, status TaxFilingStatus, taxableIncome *big.Rat) *big.Rat {
	floors := table.bracketFloorsByStatus[status]
	tax := new(big.Rat)
	for index, rate := range federalBracketRates {
		floor := new(big.Rat)
		if index > 0 {
			floor.SetInt64(floors[index-1])
		}
		if taxableIncome.Cmp(floor) <= 0 {
			break
		}
		ceiling := new(big.Rat).Set(taxableIncome)
		if index < len(floors) {
			if bracketCeiling := big.NewRat(floors[index], 1); bracketCeiling.Cmp(ceiling) < 0 {
				ceiling = bracketCeiling
			}
		}
		tax.Add(tax, new(big.Rat).Mul(new(big.Rat).Sub(ceiling, floor), rate))
	}
	return roundToCents(tax)
}

// DefaultTaxFilingStatus is the filing status assumed when none is asked
// for: married filing jointly for two people's documents, else single.
func DefaultTaxFilingStatus(personCount int) TaxFilingStatus {
	if personCount >= 2 {
		return TaxFilingStatusMarriedFilingJointly
	}
	return TaxFilingStatusSingle
}

// estimateTaxYear sets the tax a year's wages owe beside what was
// withheld, from each person's figures to date and to December 31, or nil
// when TeaNode does not know the year's brackets or there are no wages.
func estimateTaxYear(taxYear int, status TaxFilingStatus, people []*personSums, isProjected bool, extras *estimateExtras) *TaxYearEstimate {
	if extras == nil {
		extras = &estimateExtras{}
	}
	setting := extras.setting
	if setting == nil {
		setting = &models.TaxYearSetting{}
	}
	if extras.mortgageInterest == nil {
		extras.mortgageInterest = new(big.Rat)
	}
	if extras.mortgagePropertyTax == nil {
		extras.mortgagePropertyTax = new(big.Rat)
	}
	table := federalTaxTableByYear[taxYear]
	if table == nil || len(people) == 0 {
		return nil
	}
	wageBase, isWageBaseKnown := SocialSecurityWageBase(taxYear)
	// A joint return puts everyone's wages together; any other status is
	// a return each.
	returns := [][]*personSums{people}
	if status != TaxFilingStatusMarriedFilingJointly && len(people) > 1 {
		returns = nil
		for _, person := range people {
			returns = append(returns, []*personSums{person})
		}
	}
	pay, standardDeduction, taxableIncome, federal, socialSecurity, medicare := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	stateTax, localTax, withheld, paid := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	investmentIncome, adjustedGrossIncome, preferential, preferentialTax := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	capitalLossDeduction, foreignTaxCredit, netInvestmentIncomeTax := new(big.Rat), new(big.Rat), new(big.Rat)
	otherIncome, deductionTaken, itemizedDeduction, mortgageInterest := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	stateAndLocalDeduction, charitable, childCredit, careCredit := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	isItemized := false
	for returnIndex, members := range returns {
		// The settings and the 1098s are the household's, on the first
		// return when there are several.
		isFirstReturn := returnIndex == 0
		wages, medicareWages := new(big.Rat), new(big.Rat)
		returnStateAndLocal, employerCare := new(big.Rat), new(big.Rat)
		var earnedIncomes []*big.Rat
		investment := newInvestmentSums()
		for _, person := range members {
			if person.investment != nil {
				investment.add(person.investment)
			}
			year := person.projected
			wages.Add(wages, year.wages)
			medicareWages.Add(medicareWages, year.medicareWages)
			// Pay is Medicare wages, or wages for documents that print none.
			if year.medicareWages.Sign() > 0 {
				pay.Add(pay, year.medicareWages)
			} else {
				pay.Add(pay, year.wages)
			}
			personSocialSecurityWages := new(big.Rat).Set(year.socialSecurityWages)
			if isWageBaseKnown && personSocialSecurityWages.Cmp(wageBase) > 0 {
				personSocialSecurityWages.Set(wageBase)
			}
			if isWageBaseKnown {
				socialSecurity.Add(socialSecurity, roundToCents(new(big.Rat).Mul(personSocialSecurityWages, socialSecurityRate)))
			} else {
				socialSecurity.Add(socialSecurity, year.socialSecurityTax)
			}
			for _, amountValue := range year.stateTaxByCode {
				stateTax.Add(stateTax, amountValue)
				returnStateAndLocal.Add(returnStateAndLocal, amountValue)
			}
			localTax.Add(localTax, year.localIncomeTax)
			returnStateAndLocal.Add(returnStateAndLocal, year.localIncomeTax)
			employerCare.Add(employerCare, year.dependentCare)
			earned := new(big.Rat).Set(year.medicareWages)
			if earned.Sign() == 0 {
				earned.Set(year.wages)
			}
			earnedIncomes = append(earnedIncomes, earned)
			withheld.Add(withheld, withheldTax(year))
			paid.Add(paid, withheldTax(person.figures))
		}
		// A 1099's withholding was paid already, whatever the year.
		withheld.Add(withheld, investment.withheld)
		paid.Add(paid, investment.withheld)
		invested := figureInvestmentReturn(investment, status)
		investmentIncome.Add(investmentIncome, invested.income)
		capitalLossDeduction.Add(capitalLossDeduction, invested.capitalLossDeduction)
		grossIncome := new(big.Rat).Add(wages, invested.income)
		returnOtherIncome := new(big.Rat)
		if isFirstReturn {
			returnOtherIncome = settingAmount(setting.OtherIncomeAmount)
			otherIncome.Add(otherIncome, returnOtherIncome)
			grossIncome.Add(grossIncome, returnOtherIncome)
		}
		adjustedGrossIncome.Add(adjustedGrossIncome, grossIncome)
		standard := big.NewRat(table.standardDeductionByStatus[status], 1)
		standardDeduction.Add(standardDeduction, standard)
		// Itemized: state and local taxes up to the cap, and on the first
		// return mortgage interest, property tax and charitable gifts.
		returnItemized := new(big.Rat)
		if isFirstReturn {
			returnStateAndLocal.Add(returnStateAndLocal, settingAmount(setting.PropertyTaxAmount))
			returnStateAndLocal.Add(returnStateAndLocal, settingAmount(setting.StateIncomeTaxPaidAmount))
			returnStateAndLocal.Add(returnStateAndLocal, extras.mortgagePropertyTax)
		}
		salt := saltCap(taxYear, status, grossIncome)
		if returnStateAndLocal.Cmp(salt) < 0 {
			salt = returnStateAndLocal
		}
		returnItemized.Add(returnItemized, salt)
		stateAndLocalDeduction.Add(stateAndLocalDeduction, salt)
		if isFirstReturn {
			gifts := settingAmount(setting.CharitableContributionAmount)
			returnItemized.Add(returnItemized, extras.mortgageInterest).Add(returnItemized, gifts)
			mortgageInterest.Add(mortgageInterest, extras.mortgageInterest)
			charitable.Add(charitable, gifts)
		}
		itemizedDeduction.Add(itemizedDeduction, returnItemized)
		deduction := standard
		if returnItemized.Cmp(standard) > 0 {
			deduction = returnItemized
			isItemized = true
		}
		deductionTaken.Add(deductionTaken, deduction)
		taxable := new(big.Rat).Sub(grossIncome, deduction)
		if taxable.Sign() < 0 {
			taxable = new(big.Rat)
		}
		taxableIncome.Add(taxableIncome, taxable)
		// Qualified dividends and net long-term gain at the lower rates,
		// on top of the rest; as ordinary income in a year whose ceilings
		// TeaNode does not know.
		returnPreferential := new(big.Rat).Set(invested.preferential)
		if returnPreferential.Cmp(taxable) > 0 {
			returnPreferential.Set(taxable)
		}
		ceilings, isKnown := preferentialRateCeilingsByYear[taxYear][status]
		if !isKnown {
			returnPreferential = new(big.Rat)
		}
		ordinaryTaxable := new(big.Rat).Sub(taxable, returnPreferential)
		returnTax := federalIncomeTax(table, status, ordinaryTaxable)
		if returnPreferential.Sign() > 0 {
			tax := preferentialIncomeTax(ceilings, ordinaryTaxable, returnPreferential)
			preferential.Add(preferential, returnPreferential)
			preferentialTax.Add(preferentialTax, tax)
			returnTax.Add(returnTax, tax)
		}
		// Foreign tax taken as a credit without Form 1116, which allows at
		// most 300 (600 filing jointly); more needs the form, and the
		// estimate does not count it.
		credit := new(big.Rat).Set(investment.foreignTaxPaid)
		simpleCreditLimit := big.NewRat(300, 1)
		if status == TaxFilingStatusMarriedFilingJointly {
			simpleCreditLimit = big.NewRat(600, 1)
		}
		if credit.Cmp(simpleCreditLimit) > 0 {
			credit = simpleCreditLimit
		}
		if credit.Cmp(returnTax) > 0 {
			credit.Set(returnTax)
		}
		foreignTaxCredit.Add(foreignTaxCredit, credit)
		returnTax.Sub(returnTax, credit)
		if isFirstReturn {
			// The credits for children and care, on the tax left.
			children := childTaxCredit(taxYear, status, setting.ChildCount, setting.OtherDependentCount, grossIncome)
			// A joint return's care credit is up to the lower earner's
			// pay: the other income stands for a spouse no W-2 shows.
			if returnOtherIncome.Sign() > 0 {
				earnedIncomes = append(earnedIncomes, returnOtherIncome)
			}
			lowerEarned := new(big.Rat)
			if len(earnedIncomes) > 0 {
				lowerEarned = earnedIncomes[0]
				for _, earned := range earnedIncomes[1:] {
					if earned.Cmp(lowerEarned) < 0 {
						lowerEarned = earned
					}
				}
			}
			if status == TaxFilingStatusMarriedFilingJointly && len(earnedIncomes) < 2 {
				// Only one earner known on a joint return.
				lowerEarned = new(big.Rat)
			}
			care := dependentCareCredit(taxYear, status, setting.ChildCount, settingAmount(setting.DependentCareExpenseAmount), employerCare, lowerEarned, grossIncome)
			for _, pair := range []struct{ credit, total *big.Rat }{{children, childCredit}, {care, careCredit}} {
				if pair.credit.Cmp(returnTax) > 0 {
					pair.credit = new(big.Rat).Set(returnTax)
				}
				returnTax.Sub(returnTax, pair.credit)
				pair.total.Add(pair.total, pair.credit)
			}
		}
		federal.Add(federal, returnTax)
		if above := new(big.Rat).Sub(grossIncome, big.NewRat(netInvestmentIncomeThresholdByStatus[status], 1)); above.Sign() > 0 && invested.netInvestmentIncome.Sign() > 0 {
			if invested.netInvestmentIncome.Cmp(above) < 0 {
				above = invested.netInvestmentIncome
			}
			netInvestmentIncomeTax.Add(netInvestmentIncomeTax, roundToCents(new(big.Rat).Mul(above, netInvestmentIncomeTaxRate)))
		}
		medicare.Add(medicare, roundToCents(new(big.Rat).Mul(medicareWages, medicareRate)))
		if above := new(big.Rat).Sub(medicareWages, big.NewRat(additionalMedicareThresholdByStatus[status], 1)); above.Sign() > 0 {
			medicare.Add(medicare, roundToCents(new(big.Rat).Mul(above, additionalMedicareRate)))
		}
	}
	total := new(big.Rat).Add(federal, socialSecurity)
	total.Add(total, medicare).Add(total, stateTax).Add(total, localTax).Add(total, netInvestmentIncomeTax)
	income := new(big.Rat).Add(pay, investmentIncome)
	owing, refund := new(big.Rat).Sub(total, withheld), new(big.Rat).Sub(withheld, total)
	if owing.Sign() < 0 {
		owing = new(big.Rat)
	}
	if refund.Sign() < 0 {
		refund = new(big.Rat)
	}
	upcoming := new(big.Rat).Sub(withheld, paid)
	if upcoming.Sign() < 0 {
		upcoming = new(big.Rat)
	}
	remaining := new(big.Rat).Sub(income, total)
	remaining.Sub(remaining, refund)
	if remaining.Sign() < 0 {
		remaining = new(big.Rat)
	}
	return &TaxYearEstimate{
		TaxFilingStatus: status, IsProjected: isProjected,
		PayAmount: formatDollars(pay), InvestmentIncomeAmount: formatDollars(investmentIncome), IncomeAmount: formatDollars(income),
		AdjustedGrossIncomeAmount: formatDollars(adjustedGrossIncome),
		StandardDeductionAmount:   formatDollars(standardDeduction), TaxableIncomeAmount: formatDollars(taxableIncome),
		PreferentialIncomeAmount: formatDollars(preferential), PreferentialIncomeTaxAmount: formatDollars(preferentialTax),
		CapitalLossDeductionAmount: formatDollars(capitalLossDeduction), ForeignTaxCreditAmount: formatDollars(foreignTaxCredit),
		NetInvestmentIncomeTaxAmount: formatDollars(netInvestmentIncomeTax), IsInvestmentIncomePreviewed: extras.isPreviewed,
		OtherIncomeAmount: formatDollars(otherIncome), DeductionAmount: formatDollars(deductionTaken), IsItemized: isItemized,
		ItemizedDeductionAmount: formatDollars(itemizedDeduction), MortgageInterestAmount: formatDollars(mortgageInterest),
		StateAndLocalTaxDeductionAmount: formatDollars(stateAndLocalDeduction), CharitableContributionAmount: formatDollars(charitable),
		ChildTaxCreditAmount: formatDollars(childCredit), DependentCareCreditAmount: formatDollars(careCredit),
		FederalIncomeTaxAmount: formatDollars(federal), SocialSecurityTaxAmount: formatDollars(socialSecurity), MedicareTaxAmount: formatDollars(medicare),
		StateIncomeTaxAmount: formatDollars(stateTax), LocalIncomeTaxAmount: formatDollars(localTax), TotalTaxAmount: formatDollars(total),
		PaidTaxAmount: formatDollars(paid), UpcomingWithheldTaxAmount: formatDollars(upcoming),
		OwingTaxAmount: formatDollars(owing), RefundAmount: formatDollars(refund), RemainingIncomeAmount: formatDollars(remaining),
	}
}

// withheldTax is every income and payroll tax a year's figures withheld.
func withheldTax(sums *figureSums) *big.Rat {
	withheld := new(big.Rat).Add(sums.federalIncomeTax, sums.socialSecurityTax)
	withheld.Add(withheld, sums.medicareTax).Add(withheld, sums.localIncomeTax)
	for _, amountValue := range sums.stateTaxByCode {
		withheld.Add(withheld, amountValue)
	}
	return withheld
}

// personSums are one person's figures to date and to December 31 (the
// same when nothing is projected), and the investment income of the 1099s
// made out to them.
type personSums struct {
	figures, projected *figureSums
	investment         *investmentSums
}

// retirementDeferralLimitByYear is the most a person may put into
// workplace retirement plans (401(k), 403(b), the federal Thrift Savings
// Plan) from pay in a year, traditional and Roth together, and the
// catch-up allowed on top from age 50, and from 60 to 63 where the law
// allows more.
var retirementDeferralLimitByYear = map[int][3]int64{
	2021: {19500, 6500, 6500}, 2022: {20500, 6500, 6500}, 2023: {22500, 7500, 7500},
	2024: {23000, 7500, 7500}, 2025: {23500, 7500, 11250}, 2026: {24500, 8000, 11250},
}

// RetirementDeferralLimit is the year's limit on a person's retirement
// plan contributions from pay, before any catch-up, and whether TeaNode
// knows it.
func RetirementDeferralLimit(taxYear int) (*big.Rat, bool) {
	limits, isKnown := retirementDeferralLimitByYear[taxYear]
	if !isKnown {
		return nil, false
	}
	return big.NewRat(limits[0], 1), true
}

// retirementDeferralCeiling is the limit a projection stops a person's
// contributions at, given what they have put in so far: the year's limit,
// or with the catch-up from age 50 once they are past it, or with the one
// from 60 to 63 once past that; nil when they are past every one or the
// year's limits are not known, since then the paystubs know something
// TeaNode does not.
func retirementDeferralCeiling(taxYear int, yearToDate *big.Rat) *big.Rat {
	limits, isKnown := retirementDeferralLimitByYear[taxYear]
	if !isKnown {
		return nil
	}
	for _, ceiling := range []int64{limits[0], limits[0] + limits[1], limits[0] + limits[2]} {
		if ceilingValue := big.NewRat(ceiling, 1); yearToDate.Cmp(ceilingValue) <= 0 {
			return ceilingValue
		}
	}
	return nil
}

// estimateExtras are what the estimate takes besides each person's
// documents: the year's settings and its 1098s, and whether investment
// income counted is previewed rather than read from 1099s.
type estimateExtras struct {
	setting                               *models.TaxYearSetting
	mortgageInterest, mortgagePropertyTax *big.Rat
	isPreviewed                           bool
}
