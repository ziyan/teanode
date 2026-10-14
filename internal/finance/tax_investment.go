package finance

import (
	"math/big"

	"github.com/ziyan/teanode/internal/models"
)

// Investment income from 1099s: interest (1099-INT), dividends and
// capital gain distributions (1099-DIV), and gains and losses on sales
// (1099-B), each added up for the year. The estimate taxes interest,
// ordinary dividends and short-term gains as wages are taxed, qualified
// dividends and long-term gains at 0%, 15% or 20% stacked on top of the
// rest of taxable income, a net loss against at most 3,000 of other income
// (1,500 filing separately), and adds the 3.8% net investment income tax
// above the filing status's threshold. Foreign tax paid is taken as a
// credit, as it may be without Form 1116 below a few hundred dollars. A
// loss carried over from an earlier year is not known and not counted.

// TaxYearInvestmentFigures are a year's investment income from its 1099s.
// Amounts are dollars with cents.
type TaxYearInvestmentFigures struct {
	// InterestAmount is 1099-INT boxes 1 and 3; TaxExemptInterestAmount
	// 1099-INT box 8 and 1099-DIV box 12, not taxed.
	InterestAmount          string `json:"interestAmount"`
	TaxExemptInterestAmount string `json:"taxExemptInterestAmount"`
	// OrdinaryDividendsAmount is 1099-DIV box 1a, QualifiedDividendsAmount
	// box 1b (part of 1a), CapitalGainDistributionsAmount box 2a.
	OrdinaryDividendsAmount        string `json:"ordinaryDividendsAmount"`
	QualifiedDividendsAmount       string `json:"qualifiedDividendsAmount"`
	CapitalGainDistributionsAmount string `json:"capitalGainDistributionsAmount"`
	// ShortTermGainAmount and LongTermGainAmount are the 1099-Bs' sales,
	// proceeds less cost basis plus wash sale loss disallowed, by term; a
	// loss is negative. Sales of unknown term count as short term.
	ShortTermGainAmount string `json:"shortTermGainAmount"`
	LongTermGainAmount  string `json:"longTermGainAmount"`
	// FederalIncomeTaxWithheldAmount is box 4 of every 1099;
	// ForeignTaxPaidAmount 1099-INT box 6 and 1099-DIV box 7.
	FederalIncomeTaxWithheldAmount string `json:"federalIncomeTaxWithheldAmount"`
	ForeignTaxPaidAmount           string `json:"foreignTaxPaidAmount"`
	// IsCostBasisMissing says a 1099-B prints proceeds without a cost
	// basis for some of its sales, so their gain counts the whole proceeds.
	IsCostBasisMissing bool `json:"isCostBasisMissing"`
	// TaxFormIDs are the 1099s added up: of each payer and kind the latest
	// read, a correction in the place of the form it corrects.
	TaxFormIDs []string `json:"taxFormIds"`
}

// investmentSums is TaxYearInvestmentFigures while being added up.
type investmentSums struct {
	interest, taxExemptInterest, ordinaryDividends, qualifiedDividends, capitalGainDistributions *big.Rat
	shortTermGain, longTermGain, withheld, foreignTaxPaid                                        *big.Rat
	isCostBasisMissing                                                                           bool
	taxFormIds                                                                                   []string
	// recipientName is the name the first 1099 added is made out to.
	recipientName string
}

func newInvestmentSums() *investmentSums {
	sums := &investmentSums{}
	for _, field := range sums.fields() {
		*field = new(big.Rat)
	}
	return sums
}

func (self *investmentSums) fields() []**big.Rat {
	return []**big.Rat{&self.interest, &self.taxExemptInterest, &self.ordinaryDividends, &self.qualifiedDividends, &self.capitalGainDistributions,
		&self.shortTermGain, &self.longTermGain, &self.withheld, &self.foreignTaxPaid}
}

func (self *investmentSums) add(other *investmentSums) {
	otherFields := other.fields()
	for index, field := range self.fields() {
		(*field).Add(*field, *otherFields[index])
	}
	self.isCostBasisMissing = self.isCostBasisMissing || other.isCostBasisMissing
	self.taxFormIds = append(self.taxFormIds, other.taxFormIds...)
}

// addTaxForm adds one 1099; a W-2 adds nothing.
func (self *investmentSums) addTaxForm(taxForm *models.FinanceTaxForm) {
	amountOf := func(boxCode string, qualifiers ...string) *big.Rat {
		amountValue, _ := taxFormAmount(taxForm, boxCode, qualifiers...)
		return amountValue
	}
	switch taxForm.TaxFormKind {
	case models.TaxFormKind1099Int:
		self.interest.Add(self.interest, amountOf("1")).Add(self.interest, amountOf("3"))
		self.taxExemptInterest.Add(self.taxExemptInterest, amountOf("8"))
		self.withheld.Add(self.withheld, amountOf("4"))
		self.foreignTaxPaid.Add(self.foreignTaxPaid, amountOf("6"))
	case models.TaxFormKind1099Div:
		self.ordinaryDividends.Add(self.ordinaryDividends, amountOf("1a"))
		self.qualifiedDividends.Add(self.qualifiedDividends, amountOf("1b"))
		self.capitalGainDistributions.Add(self.capitalGainDistributions, amountOf("2a"))
		self.taxExemptInterest.Add(self.taxExemptInterest, amountOf("12"))
		self.withheld.Add(self.withheld, amountOf("4"))
		self.foreignTaxPaid.Add(self.foreignTaxPaid, amountOf("7"))
	case models.TaxFormKind1099B:
		for term := range brokerTerms {
			proceeds, hasProceeds := taxFormAmount(taxForm, "1d", term)
			basis, hasBasis := taxFormAmount(taxForm, "1e", term)
			if !hasProceeds && !hasBasis {
				continue
			}
			if hasProceeds && proceeds.Sign() != 0 && !hasBasis {
				self.isCostBasisMissing = true
			}
			gain := new(big.Rat).Sub(proceeds, basis)
			gain.Add(gain, amountOf("1g", term))
			if IsShortTermBrokerTerm(term) {
				self.shortTermGain.Add(self.shortTermGain, gain)
			} else {
				self.longTermGain.Add(self.longTermGain, gain)
			}
		}
		self.withheld.Add(self.withheld, amountOf("4"))
	default:
		return
	}
	self.taxFormIds = append(self.taxFormIds, taxForm.ID)
	if self.recipientName == "" {
		self.recipientName = taxForm.RecipientName
	}
}

// investmentSumsByEmployee adds up the year's 1099s by the person they are
// made out to, in the order the people first appear: of each payer, kind
// and person the latest read, so a corrected form stands in the place of
// the one it corrects.
func investmentSumsByEmployee(taxYear int, taxForms []*models.FinanceTaxForm, identities *taxYearIdentities) (map[string]*investmentSums, []string) {
	latestByKey := map[string]*models.FinanceTaxForm{}
	var keys []string
	for _, taxForm := range taxForms {
		if taxForm == nil || taxForm.TaxYear != taxYear || !IsInvestmentTaxForm(taxForm.TaxFormKind) {
			continue
		}
		key := string(taxForm.TaxFormKind) + "\x00" + identities.employeeKey(taxForm.RecipientName, taxForm.RecipientTaxIDMask) + "\x00" +
			identities.employerKey(taxForm.PayerName, taxForm.PayerTaxID)
		earlier := latestByKey[key]
		if earlier == nil {
			keys = append(keys, key)
		}
		if earlier == nil || !taxForm.CreatedAt.Before(earlier.CreatedAt) {
			latestByKey[key] = taxForm
		}
	}
	byEmployee := map[string]*investmentSums{}
	var order []string
	for _, key := range keys {
		taxForm := latestByKey[key]
		employeeKey := identities.employeeKey(taxForm.RecipientName, taxForm.RecipientTaxIDMask)
		if byEmployee[employeeKey] == nil {
			byEmployee[employeeKey] = newInvestmentSums()
			order = append(order, employeeKey)
		}
		byEmployee[employeeKey].addTaxForm(taxForm)
	}
	return byEmployee, order
}

// hsaFamilyLimitByYear is the most that may go into a person's HSA in a
// year with family coverage, theirs and their employer's together, before
// the catch-up from 55 (IRS Rev. Proc. 2022-24, 2023-23, 2024-25,
// 2025-19).
var hsaFamilyLimitByYear = map[int]int64{2023: 7750, 2024: 8300, 2025: 8550, 2026: 8750}

func (self *investmentSums) figures() *TaxYearInvestmentFigures {
	return &TaxYearInvestmentFigures{
		InterestAmount: formatDollars(self.interest), TaxExemptInterestAmount: formatDollars(self.taxExemptInterest),
		OrdinaryDividendsAmount: formatDollars(self.ordinaryDividends), QualifiedDividendsAmount: formatDollars(self.qualifiedDividends),
		CapitalGainDistributionsAmount: formatDollars(self.capitalGainDistributions),
		ShortTermGainAmount:            formatDollars(self.shortTermGain), LongTermGainAmount: formatDollars(self.longTermGain),
		FederalIncomeTaxWithheldAmount: formatDollars(self.withheld), ForeignTaxPaidAmount: formatDollars(self.foreignTaxPaid),
		IsCostBasisMissing: self.isCostBasisMissing, TaxFormIDs: append([]string{}, self.taxFormIds...),
	}
}

// IsInvestmentTaxForm says a kind is a 1099 the year's investment income
// is added up from.
func IsInvestmentTaxForm(taxFormKind models.TaxFormKind) bool {
	return taxFormKind == models.TaxFormKind1099Int || taxFormKind == models.TaxFormKind1099Div || taxFormKind == models.TaxFormKind1099B
}

// preferentialRateCeilingsByYear are, by filing status, the taxable income
// up to which qualified dividends and long-term gains are taxed at 0%, and
// up to which at 15%; 20% above (Rev. Proc. 2022-38, 2023-34, 2024-40,
// 2025-32).
var preferentialRateCeilingsByYear = map[int]map[TaxFilingStatus][2]int64{
	2023: {TaxFilingStatusSingle: {44625, 492300}, TaxFilingStatusMarriedFilingJointly: {89250, 553850}, TaxFilingStatusMarriedFilingSeparately: {44625, 276900}, TaxFilingStatusHeadOfHousehold: {59750, 523050}},
	2024: {TaxFilingStatusSingle: {47025, 518900}, TaxFilingStatusMarriedFilingJointly: {94050, 583750}, TaxFilingStatusMarriedFilingSeparately: {47025, 291850}, TaxFilingStatusHeadOfHousehold: {63000, 551350}},
	2025: {TaxFilingStatusSingle: {48350, 533400}, TaxFilingStatusMarriedFilingJointly: {96700, 600050}, TaxFilingStatusMarriedFilingSeparately: {48350, 300000}, TaxFilingStatusHeadOfHousehold: {64750, 566700}},
	2026: {TaxFilingStatusSingle: {49450, 545500}, TaxFilingStatusMarriedFilingJointly: {98900, 613700}, TaxFilingStatusMarriedFilingSeparately: {49450, 306850}, TaxFilingStatusHeadOfHousehold: {66200, 579600}},
}

// netInvestmentIncomeThresholdByStatus is the modified adjusted gross
// income above which the 3.8% net investment income tax applies; it is not
// indexed.
var netInvestmentIncomeThresholdByStatus = map[TaxFilingStatus]int64{
	TaxFilingStatusSingle: 200000, TaxFilingStatusMarriedFilingJointly: 250000, TaxFilingStatusMarriedFilingSeparately: 125000, TaxFilingStatusHeadOfHousehold: 200000,
}

var (
	netInvestmentIncomeTaxRate = big.NewRat(38, 1000)
	preferentialRates          = [3]*big.Rat{new(big.Rat), big.NewRat(15, 100), big.NewRat(20, 100)}
)

// preferentialIncomeTax is the tax on preferential income (qualified
// dividends and net long-term gain) stacked above ordinaryTaxable, by the
// year's 0%, 15% and 20% ceilings.
func preferentialIncomeTax(ceilings [2]int64, ordinaryTaxable, preferential *big.Rat) *big.Rat {
	tax := new(big.Rat)
	floor := new(big.Rat).Set(ordinaryTaxable)
	top := new(big.Rat).Add(ordinaryTaxable, preferential)
	for index, rate := range preferentialRates {
		ceiling := new(big.Rat).Set(top)
		if index < len(ceilings) {
			if bandCeiling := big.NewRat(ceilings[index], 1); bandCeiling.Cmp(ceiling) < 0 {
				ceiling = bandCeiling
			}
		}
		if ceiling.Cmp(floor) > 0 {
			tax.Add(tax, new(big.Rat).Mul(new(big.Rat).Sub(ceiling, floor), rate))
			floor = ceiling
		}
	}
	return roundToCents(tax)
}

// investmentReturn is what one return's investment income comes to.
type investmentReturn struct {
	// income is interest, ordinary dividends and the capital result (a
	// net gain, or a loss up to the limit), added to wages for gross income.
	income *big.Rat
	// preferential is qualified dividends and net long-term gain, taxed at
	// the lower rates; capitalLossDeduction the loss taken against other
	// income; netInvestmentIncome what the 3.8% tax looks at.
	preferential, capitalLossDeduction, netInvestmentIncome *big.Rat
}

// figureInvestmentReturn works a return's investment income out from its
// sums, a net loss limited as the filing status limits it.
func figureInvestmentReturn(sums *investmentSums, status TaxFilingStatus) *investmentReturn {
	longTerm := new(big.Rat).Add(sums.longTermGain, sums.capitalGainDistributions)
	netGain := new(big.Rat).Add(sums.shortTermGain, longTerm)
	result := &investmentReturn{income: new(big.Rat).Add(sums.interest, sums.ordinaryDividends), capitalLossDeduction: new(big.Rat)}
	if netGain.Sign() >= 0 {
		result.income.Add(result.income, netGain)
	} else {
		lossLimit := big.NewRat(3000, 1)
		if status == TaxFilingStatusMarriedFilingSeparately {
			lossLimit = big.NewRat(1500, 1)
		}
		loss := new(big.Rat).Neg(netGain)
		if loss.Cmp(lossLimit) > 0 {
			loss = lossLimit
		}
		result.capitalLossDeduction = loss
		result.income.Sub(result.income, loss)
	}
	// Net capital gain: the long-term gain the short-term result leaves,
	// never below nothing.
	netCapitalGain := new(big.Rat).Set(longTerm)
	if netGain.Cmp(netCapitalGain) < 0 {
		netCapitalGain.Set(netGain)
	}
	if netCapitalGain.Sign() < 0 {
		netCapitalGain = new(big.Rat)
	}
	result.preferential = new(big.Rat).Add(sums.qualifiedDividends, netCapitalGain)
	result.netInvestmentIncome = new(big.Rat).Add(sums.interest, sums.ordinaryDividends)
	if netGain.Sign() > 0 {
		result.netInvestmentIncome.Add(result.netInvestmentIncome, netGain)
	}
	return result
}

// investmentFindingText says what the year's 1099s leave out, or "".
func investmentFindingText(sums *investmentSums) string {
	if sums.isCostBasisMissing {
		return "A 1099-B prints proceeds without a cost basis for some sales, so their gain counts the whole proceeds; add the basis from your records"
	}
	return ""
}
