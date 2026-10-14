package finance

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// inventedBrokerStatement is an invented brokerage's 1099s for 2026: 1,000
// of interest, 2,000 of dividends (1,500 qualified) with 500 of capital
// gain distributions, a short-term loss of 2,000 with 100 of it a wash
// sale, and a long-term gain of 10,000.
func inventedBrokerStatement() []*models.FinanceTaxForm {
	form := func(id string, kind models.TaxFormKind, entries ...*models.FinanceTaxFormEntry) *models.FinanceTaxForm {
		return &models.FinanceTaxForm{ID: id, TaxFormKind: kind, TaxYear: 2026, PayerName: "Example Brokerage", PayerTaxID: "98-7654321",
			RecipientName: "Pat Example", RecipientTaxIDMask: "0123", TaxFormEntries: entries, CreatedAt: time.Date(2027, 2, 15, 0, 0, 0, 0, time.UTC)}
	}
	return []*models.FinanceTaxForm{
		form("form-int", models.TaxFormKind1099Int, &models.FinanceTaxFormEntry{BoxCode: "1", Amount: "1000.00"}),
		form("form-div", models.TaxFormKind1099Div, &models.FinanceTaxFormEntry{BoxCode: "1a", Amount: "2000.00"},
			&models.FinanceTaxFormEntry{BoxCode: "1b", Amount: "1500.00"}, &models.FinanceTaxFormEntry{BoxCode: "2a", Amount: "500.00"}),
		form("form-b", models.TaxFormKind1099B,
			&models.FinanceTaxFormEntry{BoxCode: "1d", BoxQualifier: "short_covered", Amount: "10000.00"},
			&models.FinanceTaxFormEntry{BoxCode: "1e", BoxQualifier: "short_covered", Amount: "12000.00"},
			&models.FinanceTaxFormEntry{BoxCode: "1g", BoxQualifier: "short_covered", Amount: "100.00"},
			&models.FinanceTaxFormEntry{BoxCode: "1d", BoxQualifier: "long_covered", Amount: "30000.00"},
			&models.FinanceTaxFormEntry{BoxCode: "1e", BoxQualifier: "long_covered", Amount: "20000.00"}),
	}
}

func TestCheckTaxFormReads1099s(t *testing.T) {
	broker := &models.FinanceTaxForm{TaxFormKind: models.TaxFormKind1099B, TaxYear: 2026, PayerName: "Example Brokerage", RecipientName: "Pat Example",
		TaxFormEntries: []*models.FinanceTaxFormEntry{{BoxCode: "1D", BoxQualifier: "Short Covered", Amount: "10.00"}}}
	if err := CheckTaxForm(broker); err != nil || broker.TaxFormEntries[0].BoxCode != "1d" || broker.TaxFormEntries[0].BoxQualifier != "short_covered" {
		t.Fatalf("a 1099-B total is kept by its term: %+v %v", broker.TaxFormEntries[0], err)
	}
	broker.TaxFormEntries[0].BoxQualifier = "short"
	if err := CheckTaxForm(broker); !errors.Is(err, ErrTaxDocumentRefused) {
		t.Fatalf("a term that is none of them is refused: %v", err)
	}
	dividends := &models.FinanceTaxForm{TaxFormKind: models.TaxFormKind1099Div, TaxYear: 2026, PayerName: "Example Fund", RecipientName: "Pat Example",
		TaxFormEntries: []*models.FinanceTaxFormEntry{{BoxCode: "1a", Amount: "100.00"}, {BoxCode: "1b", Amount: "150.00"}}}
	if err := CheckTaxForm(dividends); err != nil {
		t.Fatalf("CheckTaxForm: %s", err)
	}
	if findings := TaxFormFindings(dividends); len(findings) != 1 || findings[0].IsAgreeing {
		t.Fatalf("qualified dividends above the ordinary disagree: %+v", findings)
	}
	unknownBasis := inventedBrokerStatement()[2]
	unknownBasis.TaxFormEntries = unknownBasis.TaxFormEntries[3:4]
	if findings := TaxFormFindings(unknownBasis); len(findings) != 1 || findings[0].IsAgreeing {
		t.Fatalf("proceeds without a basis disagree: %+v", findings)
	}
}

// The invented statement on a joint 2026 return with 180,000 of wages:
// 1,900 of short-term loss against 10,500 of long-term gain (distributions
// included) is a net gain of 8,600, all of it long term; with 1,500 of
// qualified dividends, 10,100 is taxed at 15%.
func TestEstimateTaxYearTaxesInvestmentIncome(t *testing.T) {
	sums := newInvestmentSums()
	for _, taxForm := range inventedBrokerStatement() {
		sums.addTaxForm(taxForm)
	}
	wages := newFigureSums()
	wages.wages, wages.medicareWages, wages.socialSecurityWages = big.NewRat(180000, 1), big.NewRat(180000, 1), big.NewRat(180000, 1)
	estimate := estimateTaxYear(2026, TaxFilingStatusMarriedFilingJointly, []*personSums{{figures: wages, projected: wages, investment: sums}}, false, nil)
	// 180,000 + 1,000 + 2,000 + 8,600 = 191,600; less 32,200 is 159,400.
	if estimate.InvestmentIncomeAmount != "11600.00" || estimate.AdjustedGrossIncomeAmount != "191600.00" || estimate.TaxableIncomeAmount != "159400.00" {
		t.Fatalf("the income: %+v", estimate)
	}
	// 149,300 at ordinary rates is 2,480 + 9,120 + 10,670; 10,100 at 15%
	// is 1,515.
	if estimate.PreferentialIncomeAmount != "10100.00" || estimate.PreferentialIncomeTaxAmount != "1515.00" || estimate.FederalIncomeTaxAmount != "23785.00" ||
		estimate.NetInvestmentIncomeTaxAmount != "0.00" || estimate.IncomeAmount != "191600.00" {
		t.Fatalf("the tax: %+v", estimate)
	}

	loss := newInvestmentSums()
	loss.shortTermGain, loss.interest = big.NewRat(-10000, 1), big.NewRat(500, 1)
	estimate = estimateTaxYear(2026, TaxFilingStatusSingle, []*personSums{{figures: wages, projected: wages, investment: loss}}, false, nil)
	if estimate.CapitalLossDeductionAmount != "3000.00" || estimate.InvestmentIncomeAmount != "-2500.00" {
		t.Fatalf("a net loss is taken up to 3,000: %+v", estimate)
	}

	// Single, 210,000 of wages and 20,000 of interest: 30,000 above the
	// threshold, of which the 20,000 of investment income is taxed at 3.8%.
	rich := newFigureSums()
	rich.wages, rich.medicareWages = big.NewRat(210000, 1), big.NewRat(210000, 1)
	interest := newInvestmentSums()
	interest.interest = big.NewRat(20000, 1)
	estimate = estimateTaxYear(2026, TaxFilingStatusSingle, []*personSums{{figures: rich, projected: rich, investment: interest}}, false, nil)
	if estimate.NetInvestmentIncomeTaxAmount != "760.00" {
		t.Fatalf("the net investment income tax: %+v", estimate)
	}
}

// A year with 1099s adds them up for the person they are made out to,
// and an HSA past the family limit is a finding.
func TestSummarizeTaxYearAddsUp1099s(t *testing.T) {
	w2 := inventedW2("form-w2", 2026, "Example Employer LLC")
	w2.TaxFormEntries = append(w2.TaxFormEntries, &models.FinanceTaxFormEntry{BoxCode: "12", BoxQualifier: "W", Amount: "9000.00"})
	w2.TaxFormEntries = removeEntry(w2.TaxFormEntries, "12", "W", "900.00")
	forms := append([]*models.FinanceTaxForm{w2}, inventedBrokerStatement()...)
	summary := SummarizeTaxYear(2026, nil, forms, time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), TaxFilingStatusSingle, nil)
	investment := summary.TaxYearInvestment
	if investment == nil || investment.ShortTermGainAmount != "-1900.00" || investment.LongTermGainAmount != "10000.00" || len(investment.TaxFormIDs) != 3 ||
		summary.TaxFormCount != 4 || len(summary.TaxYearPeople) != 1 {
		t.Fatalf("the 1099s added up for the W-2's person: %+v %+v", investment, summary.TaxYearPeople)
	}
	if codes := findingCodes(summary); !containsCode(codes, TaxYearFindingHSAOverLimit) {
		t.Fatalf("9,000 into an HSA is over 2026's family limit: %v", codes)
	}
}

func removeEntry(entries []*models.FinanceTaxFormEntry, boxCode, boxQualifier, amount string) []*models.FinanceTaxFormEntry {
	kept := entries[:0]
	for _, entry := range entries {
		if entry.BoxCode == boxCode && entry.BoxQualifier == boxQualifier && entry.Amount == amount {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func containsCode(codes []TaxYearFindingCode, wanted TaxYearFindingCode) bool {
	for _, code := range codes {
		if code == wanted {
			return true
		}
	}
	return false
}

// Two accounts at one broker are two forms; one account's form read twice
// counts once, and a correction stands in for the form it corrects.
func TestInvestmentFormsAreToldApartByAccount(t *testing.T) {
	dividends := func(id, account, amount string, isCorrected bool, created time.Time) *models.FinanceTaxForm {
		return &models.FinanceTaxForm{ID: id, TaxFormKind: models.TaxFormKind1099Div, TaxYear: 2026, PayerName: "Example Brokerage", RecipientName: "Pat Example",
			IsCorrected: isCorrected, CreatedAt: created, TaxFormEntries: []*models.FinanceTaxFormEntry{
				{BoxCode: "1a", Amount: amount}, {BoxCode: "account", TextValue: account}}}
	}
	day := time.Date(2027, 2, 15, 0, 0, 0, 0, time.UTC)
	forms := []*models.FinanceTaxForm{
		dividends("joint", "5282", "100.00", false, day),
		dividends("individual", "7006", "40.00", false, day),
		dividends("individual-again", "7006", "40.00", false, day.Add(time.Hour)),
		dividends("joint-corrected", "5282", "120.00", true, day.Add(-time.Hour)),
	}
	byEmployee, order := investmentSumsByEmployee(2026, forms, newTaxYearIdentities())
	sums := byEmployee[order[0]]
	if formatDollars(sums.ordinaryDividends) != "160.00" || len(sums.taxFormIds) != 2 {
		t.Fatalf("120 corrected and 40 once: %s %v", formatDollars(sums.ordinaryDividends), sums.taxFormIds)
	}
	if account := lastFourAlphanumerics("XXXX-xx52-82"); account != "5282" {
		t.Fatalf("an account number's last four: %q", account)
	}
}
