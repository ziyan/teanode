package finance

import (
	"math/big"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A joint 2025 return with 200,000 of wages, 10,000 of a spouse's pay, a
// child, 5,000 of care, 20,000 of mortgage interest, 9,000 of state tax
// withheld, 3,000 of property tax and 2,000 of gifts: itemized is 34,000,
// more than the 31,500 standard deduction, so 176,000 is taxable.
func TestEstimateTaxYearItemizesAndTakesCredits(t *testing.T) {
	wages := newFigureSums()
	wages.wages, wages.medicareWages, wages.socialSecurityWages = big.NewRat(200000, 1), big.NewRat(210000, 1), big.NewRat(176100, 1)
	wages.addState("GA", big.NewRat(200000, 1), big.NewRat(9000, 1))
	setting := &models.TaxYearSetting{TaxYear: 2025, ChildCount: 1, DependentCareExpenseAmount: "5000", OtherIncomeAmount: "10000",
		CharitableContributionAmount: "2000", PropertyTaxAmount: "3000"}
	estimate := estimateTaxYear(2025, TaxFilingStatusMarriedFilingJointly, []*personSums{{figures: wages, projected: wages}}, false,
		&estimateExtras{setting: setting, mortgage: sumMortgages(2025, TaxFilingStatusMarriedFilingJointly,
			[]*models.FinanceTaxForm{invented1098("form-1098", "1", "20000", "400000", "2021-05-01")})})
	if !estimate.IsItemized || estimate.ItemizedDeductionAmount != "34000.00" || estimate.StateAndLocalTaxDeductionAmount != "12000.00" ||
		estimate.TaxableIncomeAmount != "176000.00" || estimate.AdjustedGrossIncomeAmount != "210000.00" {
		t.Fatalf("itemized: %+v", estimate)
	}
	// 2,385 + 8,772 + 17,391 = 28,548, less 2,200 for the child and 20% of
	// 3,000 of care (the lower earner made 10,000).
	if estimate.ChildTaxCreditAmount != "2200.00" || estimate.DependentCareCreditAmount != "600.00" || estimate.FederalIncomeTaxAmount != "25748.00" {
		t.Fatalf("credits: %+v", estimate)
	}
	// Without the spouse's pay a joint return has one known earner, and
	// no care credit.
	setting.OtherIncomeAmount = "0"
	if estimate := estimateTaxYear(2025, TaxFilingStatusMarriedFilingJointly, []*personSums{{figures: wages, projected: wages}}, false,
		&estimateExtras{setting: setting}); estimate.DependentCareCreditAmount != "0.00" || estimate.IsItemized {
		t.Fatalf("one earner, no 1098: %+v", estimate)
	}
}

func TestTaxCreditsAndCaps(t *testing.T) {
	for _, check := range []struct {
		taxYear int
		status  TaxFilingStatus
		income  int64
		percent int64
	}{
		{2025, TaxFilingStatusMarriedFilingJointly, 210000, 20},
		{2025, TaxFilingStatusSingle, 20000, 32},
		{2026, TaxFilingStatusMarriedFilingJointly, 160000, 32},
		{2026, TaxFilingStatusMarriedFilingJointly, 100000, 35},
		{2026, TaxFilingStatusSingle, 20000, 47},
	} {
		if rate := dependentCareRate(check.taxYear, check.status, big.NewRat(check.income, 1)); rate.Cmp(big.NewRat(check.percent, 100)) != 0 {
			t.Errorf("%d %s %d: %s, want %d%%", check.taxYear, check.status, check.income, rate.FloatString(2), check.percent)
		}
	}
	for _, check := range []struct {
		taxYear int
		status  TaxFilingStatus
		income  int64
		cap     string
	}{
		{2024, TaxFilingStatusMarriedFilingJointly, 300000, "10000.00"},
		{2025, TaxFilingStatusSingle, 550000, "25000.00"},
		{2025, TaxFilingStatusSingle, 700000, "10000.00"},
		{2026, TaxFilingStatusMarriedFilingJointly, 515000, "37400.00"},
		{2026, TaxFilingStatusMarriedFilingSeparately, 100000, "20200.00"},
	} {
		if cap := formatDollars(saltCap(check.taxYear, check.status, big.NewRat(check.income, 1))); cap != check.cap {
			t.Errorf("%d %s %d: %s, want %s", check.taxYear, check.status, check.income, cap, check.cap)
		}
	}
	// 10,500 above 400,000 is 11 thousands or part: 550 off two children's
	// 4,400.
	if credit := formatDollars(childTaxCredit(2026, TaxFilingStatusMarriedFilingJointly, 2, 0, big.NewRat(410500, 1))); credit != "3850.00" {
		t.Errorf("phased out: %s", credit)
	}
}

// On April 1 the accounts' interest and dividends so far are projected to
// December 31, an IRA's left out; a 1099-INT read for the year takes the
// interest's place, and gains given count until a 1099-B is read.
func TestSummarizeTaxYearPreviewsInvestmentIncome(t *testing.T) {
	inputs := &TaxYearInputs{
		AccountIncomes: []*models.AccountInvestmentIncome{
			{FinanceAccountID: "account-savings", AccountName: "Example Savings", AccountSubtype: "savings", IncomeKind: "interest", Amount: "91.00"},
			{FinanceAccountID: "account-ira", AccountName: "Example IRA", AccountSubtype: "ira", IncomeKind: "dividend", Amount: "400.00"},
			{FinanceAccountID: "account-hsa", AccountName: "Example HSA", AccountSubtype: "HSA", IncomeKind: "dividend", Amount: "100.00"},
			{FinanceAccountID: "account-brokerage", AccountName: "Example Brokerage", AccountSubtype: "brokerage", IncomeKind: "dividend", Amount: "182.00"},
		},
		Setting: &models.TaxYearSetting{TaxYear: 2026, EstimatedShortTermGainAmount: "1000", EstimatedLongTermGainAmount: "2000"},
	}
	april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	summary := SummarizeTaxYear(2026, nil, []*models.FinanceTaxForm{inventedW2("form-w2", 2026, "Example Employer LLC")}, april, TaxFilingStatusSingle, inputs)
	preview := summary.TaxYearInvestmentPreview
	// April 1 is the 91st day: 91 a year of 365 days later is 365.
	if preview == nil || preview.ProjectedInterestAmount != "365.00" || preview.ProjectedDividendsAmount != "730.00" || preview.TaxAdvantagedAmount != "500.00" ||
		!preview.IsInterestCounted || len(preview.TaxYearAccountIncomes) != 4 || !preview.TaxYearAccountIncomes[1].IsTaxAdvantaged || !preview.TaxYearAccountIncomes[2].IsTaxAdvantaged {
		t.Fatalf("the preview: %+v", preview)
	}
	// 365 + 730 + 1,000 + 2,000.
	if estimate := summary.TaxYearEstimate; estimate.InvestmentIncomeAmount != "4095.00" || !estimate.IsInvestmentIncomePreviewed {
		t.Fatalf("the estimate counts it: %+v", estimate)
	}
	withForms := append([]*models.FinanceTaxForm{inventedW2("form-w2", 2026, "Example Employer LLC")}, inventedBrokerStatement()...)
	summary = SummarizeTaxYear(2026, nil, withForms, april, TaxFilingStatusSingle, inputs)
	// The 1099s: 1,000 + 2,000 + 8,600, nothing previewed.
	if summary.TaxYearInvestmentPreview.IsInterestCounted || summary.TaxYearEstimate.InvestmentIncomeAmount != "11600.00" || summary.TaxYearEstimate.IsInvestmentIncomePreviewed {
		t.Fatalf("1099s take the preview's place: %+v %+v", summary.TaxYearInvestmentPreview, summary.TaxYearEstimate)
	}
	if projection := yearProjection(2025, april); projection.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatalf("a year that is over is not projected: %s", projection.FloatString(3))
	}
}

// invented1098 is a 1098 of an invented lender for 2025.
func invented1098(id, recipientTaxIDMask, interest, principal, origination string, entries ...*models.FinanceTaxFormEntry) *models.FinanceTaxForm {
	return &models.FinanceTaxForm{
		ID: id, TaxFormKind: models.TaxFormKind1098, TaxYear: 2025, PayerName: "Example Mortgage Co", PayerTaxID: "12-3456789",
		RecipientName: "PAT EXAMPLE", RecipientTaxIDMask: recipientTaxIDMask,
		TaxFormEntries: append([]*models.FinanceTaxFormEntry{
			{BoxCode: "1", Amount: interest}, {BoxCode: "2", Amount: principal}, {BoxCode: "3", TextValue: origination},
			{BoxCode: "account", TextValue: "7788"},
		}, entries...),
	}
}

func TestSumMortgagesLimitsTheDebt(t *testing.T) {
	overLimit := func(status TaxFilingStatus, origination string) *mortgageSums {
		return sumMortgages(2025, status, []*models.FinanceTaxForm{invented1098("form-1098", "1111", "45000", "1200000", origination)})
	}
	for _, check := range []struct {
		name, origination string
		status            TaxFilingStatus
		interest          string
	}{
		// 45,000 of interest on 1,200,000: 750,000 of it counts.
		{"after 2017", "3/22/2022", TaxFilingStatusMarriedFilingJointly, "28125.00"},
		{"filing separately", "2022-03-22", TaxFilingStatusMarriedFilingSeparately, "14062.50"},
		// Taken out before December 16, 2017: 1,000,000 counts.
		{"grandfathered", "06/01/2015", TaxFilingStatusMarriedFilingJointly, "37500.00"},
		{"no origination date", "", TaxFilingStatusSingle, "28125.00"},
	} {
		sums := overLimit(check.status, check.origination)
		if formatDollars(sums.interest) != check.interest || !sums.isOverLimit() || sums.isOriginationUnknown != (check.origination == "") {
			t.Errorf("%s: %s, want %s (%+v)", check.name, formatDollars(sums.interest), check.interest, sums)
		}
	}
	// Under the limit all of it counts; a co-borrower's copy of the same
	// loan counts once; box 4 is income; box 10 counts when it is a tax.
	sums := sumMortgages(2025, TaxFilingStatusMarriedFilingJointly, []*models.FinanceTaxForm{
		invented1098("form-a", "1111", "20000", "500000", "2021-01-15", &models.FinanceTaxFormEntry{BoxCode: "4", Amount: "300"},
			&models.FinanceTaxFormEntry{BoxCode: "10", BoxQualifier: "Real estate taxes", Amount: "5000"}),
		invented1098("form-b", "2222", "20000", "500000", "2021-01-15", &models.FinanceTaxFormEntry{BoxCode: "4", Amount: "300"},
			&models.FinanceTaxFormEntry{BoxCode: "10", BoxQualifier: "Real estate taxes", Amount: "5000"}),
		invented1098("form-c", "1111", "1000", "50000", "2023-08-01", &models.FinanceTaxFormEntry{BoxCode: "10", BoxQualifier: "Principal paid", Amount: "9000"}),
	})
	if formatDollars(sums.interest) != "21000.00" || formatDollars(sums.refundedInterest) != "300.00" || formatDollars(sums.propertyTax) != "5000.00" ||
		len(sums.taxFormIds) != 2 || sums.isOverLimit() {
		t.Fatalf("under the limit: %+v", sums)
	}
}

// A W-2 and a 1098 over the limit: the finding says so, the estimate takes
// the share under the limit, and the refunded interest is income.
func TestSummarizeTaxYearReadsA1098(t *testing.T) {
	taxForms := []*models.FinanceTaxForm{
		inventedW2("form-w2", 2025, "Example Employer LLC"),
		invented1098("form-1098", "0123", "45000", "1200000", "2022-03-22", &models.FinanceTaxFormEntry{BoxCode: "4", Amount: "250"}),
	}
	summary := SummarizeTaxYear(2025, nil, taxForms, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), TaxFilingStatusSingle, &TaxYearInputs{})
	if summary.TaxFormCount != 2 || len(summary.TaxYearFindings) != 1 || summary.TaxYearFindings[0].TaxYearFindingCode != TaxYearFindingMortgageOverLimit {
		t.Fatalf("the 1098 and its finding: %+v %+v", summary, summary.TaxYearFindings)
	}
	// 28,125 of interest is more than the 15,750 standard deduction.
	if estimate := summary.TaxYearEstimate; estimate.MortgageInterestAmount != "28125.00" || estimate.OtherIncomeAmount != "250.00" || !estimate.IsItemized {
		t.Fatalf("the estimate: %+v", estimate)
	}
}

// State tax paid besides withholding counts with property tax under the
// cap, which is half filing separately; head of household is uncapped
// here, and itemizing is not worth it.
func TestEstimateTaxYearStateAndLocalByStatus(t *testing.T) {
	wages := newFigureSums()
	wages.wages, wages.medicareWages, wages.socialSecurityWages = big.NewRat(200000, 1), big.NewRat(200000, 1), big.NewRat(176100, 1)
	wages.addState("GA", big.NewRat(200000, 1), big.NewRat(9000, 1))
	people := []*personSums{{figures: wages, projected: wages}}
	setting := &models.TaxYearSetting{TaxYear: 2025, PropertyTaxAmount: "3000", StateIncomeTaxPaidAmount: "4000"}
	estimate := estimateTaxYear(2025, TaxFilingStatusHeadOfHousehold, people, false, &estimateExtras{setting: setting})
	if estimate.StateAndLocalTaxDeductionAmount != "16000.00" || estimate.IsItemized || estimate.DeductionAmount != "23625.00" {
		t.Fatalf("head of household: %+v", estimate)
	}
	setting.PropertyTaxAmount = "10000"
	setting.StateIncomeTaxPaidAmount = "6000"
	estimate = estimateTaxYear(2025, TaxFilingStatusMarriedFilingSeparately, people, false, &estimateExtras{setting: setting})
	if estimate.StateAndLocalTaxDeductionAmount != "20000.00" || !estimate.IsItemized || estimate.DeductionAmount != "20000.00" {
		t.Fatalf("filing separately: %+v", estimate)
	}
}

// From 2026 gifts are deducted beside the standard deduction up to 2,000
// filing jointly, and itemizing takes only those above 0.5% of income.
func TestEstimateTaxYearCharitableFrom2026(t *testing.T) {
	wages := newFigureSums()
	wages.wages, wages.medicareWages, wages.socialSecurityWages = big.NewRat(210000, 1), big.NewRat(210000, 1), big.NewRat(184500, 1)
	people := []*personSums{{figures: wages, projected: wages}}
	setting := &models.TaxYearSetting{TaxYear: 2026, CharitableContributionAmount: "3000"}
	estimate := estimateTaxYear(2026, TaxFilingStatusMarriedFilingJointly, people, false, &estimateExtras{setting: setting})
	if estimate.IsItemized || estimate.DeductionAmount != "34200.00" || estimate.CharitableContributionAmount != "2000.00" || estimate.TaxableIncomeAmount != "175800.00" {
		t.Fatalf("beside the standard deduction: %+v", estimate)
	}
	// 40,000 of gifts less 1,050 is 38,950: itemized.
	setting.CharitableContributionAmount = "40000"
	estimate = estimateTaxYear(2026, TaxFilingStatusMarriedFilingJointly, people, false, &estimateExtras{setting: setting})
	if !estimate.IsItemized || estimate.CharitableContributionAmount != "38950.00" || estimate.DeductionAmount != "38950.00" {
		t.Fatalf("itemized: %+v", estimate)
	}
	// Until 2026 neither applies.
	setting.TaxYear, setting.CharitableContributionAmount = 2025, "3000"
	if estimate := estimateTaxYear(2025, TaxFilingStatusMarriedFilingJointly, people, false, &estimateExtras{setting: setting}); estimate.DeductionAmount != "31500.00" {
		t.Fatalf("2025: %+v", estimate)
	}
	// Fifty cents above the threshold is a part of 1,000.
	if credit := formatDollars(childTaxCredit(2026, TaxFilingStatusMarriedFilingJointly, 1, 0, big.NewRat(800001, 2))); credit != "2150.00" {
		t.Fatalf("a part of 1,000: %s", credit)
	}
}
