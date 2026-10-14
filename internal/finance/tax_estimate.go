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
// after (Rev. Proc. 2022-38, 2023-34, 2024-40, 2025-32; 2025's standard
// deduction as Public Law 119-21 raised it).
var federalTaxTableByYear = map[int]*federalTaxTable{
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
func estimateTaxYear(taxYear int, status TaxFilingStatus, people []*personSums, isProjected bool) *TaxYearEstimate {
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
	for _, members := range returns {
		wages, medicareWages := new(big.Rat), new(big.Rat)
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
			}
			localTax.Add(localTax, year.localIncomeTax)
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
		adjustedGrossIncome.Add(adjustedGrossIncome, grossIncome)
		deduction := big.NewRat(table.standardDeductionByStatus[status], 1)
		standardDeduction.Add(standardDeduction, deduction)
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
		credit := new(big.Rat).Set(investment.foreignTaxPaid)
		if credit.Cmp(returnTax) > 0 {
			credit.Set(returnTax)
		}
		foreignTaxCredit.Add(foreignTaxCredit, credit)
		federal.Add(federal, new(big.Rat).Sub(returnTax, credit))
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
		NetInvestmentIncomeTaxAmount: formatDollars(netInvestmentIncomeTax),
		FederalIncomeTaxAmount:       formatDollars(federal), SocialSecurityTaxAmount: formatDollars(socialSecurity), MedicareTaxAmount: formatDollars(medicare),
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
