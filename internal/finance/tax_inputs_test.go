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
		&estimateExtras{setting: setting, mortgageInterest: big.NewRat(20000, 1)})
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
