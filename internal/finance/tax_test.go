package finance

import (
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// inventedPaystub is a paycheck that adds up: 4,000.00 gross, 880.00 in
// taxes, 400.00 in deductions, 2,720.00 take-home. Federal and state wages
// are gross less the 401(k) and the HSA; Social Security and Medicare
// wages are gross less the HSA alone. yearToDateCount is how many such
// paychecks the year to date holds, this one included.
func inventedPaystub(id, payDate string, yearToDateCount int) *models.FinancePaystub {
	times := func(amount float64) string {
		return formatDollars(ratOf(amount * float64(yearToDateCount)))
	}
	return &models.FinancePaystub{
		ID: id, EmployerName: "Example Employer LLC", EmployeeName: "Pat Example", EmployeeTaxIDMask: "XXX-XX-0123",
		PayDate: payDate, CurrencyCode: "usd",
		GrossAmount: "4000.00", YearToDateGrossAmount: times(4000), TotalTaxAmount: "880.00", TotalDeductionAmount: "400.00", NetAmount: "2720.00",
		PaystubLines: []*models.FinancePaystubLine{
			{PaystubLineKind: models.PaystubLineKindEarning, Description: "Regular", Hours: "80", CurrentAmount: "4000.00", YearToDateAmount: times(4000)},
			{PaystubLineKind: models.PaystubLineKindTax, Description: "Federal", PaystubTaxKind: models.PaystubTaxKindFederalIncome,
				CurrentAmount: "500.00", YearToDateAmount: times(500), CurrentWagesAmount: "3600.00", YearToDateWagesAmount: times(3600)},
			{PaystubLineKind: models.PaystubLineKindTax, Description: "Social Security", PaystubTaxKind: models.PaystubTaxKindSocialSecurity,
				CurrentAmount: "229.40", YearToDateAmount: times(229.40), CurrentWagesAmount: "3700.00", YearToDateWagesAmount: times(3700)},
			{PaystubLineKind: models.PaystubLineKindTax, Description: "Medicare", PaystubTaxKind: models.PaystubTaxKindMedicare,
				CurrentAmount: "53.65", YearToDateAmount: times(53.65), CurrentWagesAmount: "3700.00", YearToDateWagesAmount: times(3700)},
			{PaystubLineKind: models.PaystubLineKindTax, Description: "State", PaystubTaxKind: models.PaystubTaxKindStateIncome, TaxJurisdictionCode: "ca",
				CurrentAmount: "96.95", YearToDateAmount: times(96.95), CurrentWagesAmount: "3600.00", YearToDateWagesAmount: times(3600)},
			{PaystubLineKind: models.PaystubLineKindDeduction, Description: "401(k)", PaystubDeductionKind: models.PaystubDeductionKindRetirementTraditional,
				CurrentAmount: "300.00", YearToDateAmount: times(300)},
			{PaystubLineKind: models.PaystubLineKindDeduction, Description: "HSA", PaystubDeductionKind: models.PaystubDeductionKindHSA,
				CurrentAmount: "100.00", YearToDateAmount: times(100), EmployerCurrentAmount: "50.00", EmployerYearToDateAmount: times(50)},
		},
	}
}

func ratOf(amount float64) *big.Rat {
	return roundToCents(new(big.Rat).SetFloat64(amount))
}

// checkedPaystub is inventedPaystub after CheckPaystub, as it is stored.
func checkedPaystub(t *testing.T, id, payDate string, yearToDateCount int) *models.FinancePaystub {
	t.Helper()
	paystub := inventedPaystub(id, payDate, yearToDateCount)
	if state, _, err := CheckPaystub(paystub); err != nil || state != models.PaystubCheckStateBalanced {
		t.Fatalf("the invented paystub adds up: %s %v", state, err)
	}
	return paystub
}

func TestCheckPaystubAddsUpAndNormalizes(t *testing.T) {
	paystub := inventedPaystub("paystub-1", "2026-03-06", 5)
	state, difference, err := CheckPaystub(paystub)
	if err != nil || state != models.PaystubCheckStateBalanced || difference != "0" {
		t.Fatalf("a paystub whose lines add up is balanced: %s %s %v", state, difference, err)
	}
	if paystub.CurrencyCode != "USD" || paystub.EmployeeTaxIDMask != "0123" || paystub.NetAmount != "2720.0000" {
		t.Fatalf("normalized in place: %+v", paystub)
	}
	if paystub.PaystubLines[4].TaxJurisdictionCode != "CA" || paystub.PaystubLines[0].LineNumber != 1 || paystub.PaystubLines[6].LineNumber != 7 {
		t.Fatalf("a state's code is upper case and unnumbered lines are numbered: %+v", paystub.PaystubLines)
	}

	paystub = inventedPaystub("paystub-1", "2026-03-06", 5)
	paystub.NetAmount = "2720.01"
	if state, difference, err := CheckPaystub(paystub); err != nil || state != models.PaystubCheckStateUnbalanced || difference != "-0.0100" {
		t.Fatalf("take-home pay a cent off is unbalanced by that cent: %s %s %v", state, difference, err)
	}
	paystub = inventedPaystub("paystub-1", "2026-03-06", 5)
	paystub.TotalTaxAmount = "890.00"
	if state, difference, _ := CheckPaystub(paystub); state != models.PaystubCheckStateUnbalanced || difference != "-10.0000" {
		t.Fatalf("taxes that miss their printed total are unbalanced: %s %s", state, difference)
	}
}

func TestCheckPaystubRefuses(t *testing.T) {
	for name, change := range map[string]func(*models.FinancePaystub){
		"no pay date":            func(paystub *models.FinancePaystub) { paystub.PayDate = "" },
		"a full taxpayer number": func(paystub *models.FinancePaystub) { paystub.EmployeeTaxIDMask = "123-45-0123" },
		"a number in the text":   func(paystub *models.FinancePaystub) { paystub.PaystubLines[0].Description = "Regular 987654321" },
		"a dashed number":        func(paystub *models.FinancePaystub) { paystub.EmployeeName = "Pat Example 987-65-4321" },
		"hours only": func(paystub *models.FinancePaystub) {
			paystub.PaystubLines = append(paystub.PaystubLines, &models.FinancePaystubLine{PaystubLineKind: models.PaystubLineKindEarning, Description: "Worked Hours", Hours: "72"})
		},
		"an unknown tax":   func(paystub *models.FinancePaystub) { paystub.PaystubLines[1].PaystubTaxKind = "income" },
		"a deduction kind": func(paystub *models.FinancePaystub) { paystub.PaystubLines[5].PaystubDeductionKind = "" },
		"a negative tax":   func(paystub *models.FinancePaystub) { paystub.PaystubLines[1].CurrentAmount = "-500.00" },
		"too many places":  func(paystub *models.FinancePaystub) { paystub.GrossAmount = "4000.001" },
		"a backwards period": func(paystub *models.FinancePaystub) {
			paystub.PeriodStartOn, paystub.PeriodEndOn = "2026-03-10", "2026-03-01"
		},
	} {
		paystub := inventedPaystub("paystub-1", "2026-03-06", 5)
		change(paystub)
		if _, _, err := CheckPaystub(paystub); !errors.Is(err, ErrTaxDocumentRefused) {
			t.Errorf("%s is refused: %v", name, err)
		}
	}
}

func TestMaskTaxpayerNumbers(t *testing.T) {
	masked := MaskTaxpayerNumbers("a 123-45-6789 b 123 45 6789 c 123456789 d 12-3456789 e 1234567890 f 654321.98 g 123456789.00")
	want := "a XXX-XX-6789 b XXX-XX-6789 c XXXXX6789 d 12-3456789 e 1234567890 f 654321.98 g 123456789.00"
	if masked != want {
		t.Fatalf("masked\n got %q\nwant %q", masked, want)
	}
	if HasTaxpayerNumber("Regular 80.00 4000.00") || !HasTaxpayerNumber("id 123456789") {
		t.Fatal("HasTaxpayerNumber finds nine digits standing alone and nothing else")
	}
}

// inventedW2 is the W-2 the two invented December paystubs add up to.
func inventedW2(id string, taxYear int, payerName string) *models.FinanceTaxForm {
	return &models.FinanceTaxForm{
		ID: id, TaxFormKind: models.TaxFormKindW2, TaxYear: taxYear, PayerName: payerName, PayerTaxID: "12-3456789",
		RecipientName: "PAT EXAMPLE", RecipientTaxIDMask: "0123",
		TaxFormEntries: []*models.FinanceTaxFormEntry{
			{BoxCode: "1", Amount: "21600.00"}, {BoxCode: "2", Amount: "3000.00"},
			{BoxCode: "3", Amount: "22200.00"}, {BoxCode: "4", Amount: "1376.40"},
			{BoxCode: "5", Amount: "22200.00"}, {BoxCode: "6", Amount: "321.90"},
			{BoxCode: "box 12", BoxQualifier: "d", Amount: "1800.00"}, {BoxCode: "12", BoxQualifier: "W", Amount: "900.00"},
			{BoxCode: "13", BoxQualifier: "Retirement plan", IsChecked: true},
			{BoxCode: "15", BoxQualifier: "ca", TextValue: "123-4567-8"},
			{BoxCode: "16", BoxQualifier: "CA", Amount: "21600.00"}, {BoxCode: "17", BoxQualifier: "CA", Amount: "581.70"},
		},
	}
}

func TestCheckTaxFormNormalizesAndRefuses(t *testing.T) {
	taxForm := inventedW2("form-1", 2025, "Example Employer LLC")
	if err := CheckTaxForm(taxForm); err != nil {
		t.Fatalf("the invented W-2 is recorded: %v", err)
	}
	if entry := taxForm.TaxFormEntries[6]; entry.BoxCode != "12" || entry.BoxQualifier != "D" || entry.Amount != "1800.0000" {
		t.Fatalf("box 12 D normalized: %+v", entry)
	}
	if entry := taxForm.TaxFormEntries[8]; entry.BoxQualifier != "retirement_plan" {
		t.Fatalf("box 13's checkbox normalized: %+v", entry)
	}
	for name, change := range map[string]func(*models.FinanceTaxForm){
		"box 9":             func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormEntries[0].BoxCode = "9" },
		"an unknown code":   func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormEntries[6].BoxQualifier = "ZZ" },
		"a box twice":       func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormEntries[1].BoxCode = "1" },
		"an unchecked mark": func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormEntries[8].IsChecked = false },
		"a state name":      func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormEntries[10].BoxQualifier = "California" },
		"a full number":     func(taxForm *models.FinanceTaxForm) { taxForm.RecipientTaxIDMask = "987654321" },
		"an unknown kind":   func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormKind = "1099" },
		"an empty box":      func(taxForm *models.FinanceTaxForm) { taxForm.TaxFormEntries[0].Amount = "" },
	} {
		taxForm := inventedW2("form-1", 2025, "Example Employer LLC")
		change(taxForm)
		if err := CheckTaxForm(taxForm); !errors.Is(err, ErrTaxDocumentRefused) {
			t.Errorf("%s is refused: %v", name, err)
		}
	}
}

func TestTaxFormFindings(t *testing.T) {
	taxForm := inventedW2("form-1", 2025, "Example Employer LLC")
	findings := TaxFormFindings(taxForm)
	if len(findings) != 3 {
		t.Fatalf("three findings: %+v", findings)
	}
	for _, finding := range findings {
		if !finding.IsAgreeing {
			t.Errorf("the invented W-2 agrees with itself: %+v", finding)
		}
	}
	// Above 200,000 of Medicare wages another 0.9% is withheld.
	taxForm.TaxFormEntries[4].Amount, taxForm.TaxFormEntries[5].Amount = "250000.00", "4075.00"
	for _, finding := range TaxFormFindings(taxForm) {
		if finding.TaxFormFindingCode == TaxFormFindingMedicareRate && (!finding.IsAgreeing || finding.ExpectedAmount != "4075.00") {
			t.Errorf("1.45%% of 250,000 and 0.9%% of 50,000 is 4,075.00: %+v", finding)
		}
	}
	taxForm.TaxFormEntries[5].Amount = "3625.00"
	for _, finding := range TaxFormFindings(taxForm) {
		if finding.TaxFormFindingCode == TaxFormFindingMedicareRate && finding.IsAgreeing {
			t.Errorf("Medicare without the additional 0.9%% does not agree: %+v", finding)
		}
	}
	taxForm.TaxFormEntries[2].Amount = "200000.00"
	for _, finding := range TaxFormFindings(taxForm) {
		if finding.TaxFormFindingCode == TaxFormFindingSocialSecurityWageBase && finding.IsAgreeing {
			t.Errorf("Social Security wages above 2025's base do not agree: %+v", finding)
		}
	}
}

func TestSummarizeTaxYearComparesTheW2WithTheLastPaystub(t *testing.T) {
	paystubs := []*models.FinancePaystub{checkedPaystub(t, "paystub-2", "2025-12-26", 6), checkedPaystub(t, "paystub-1", "2025-12-12", 5)}
	taxForm := inventedW2("form-1", 2025, "EXAMPLE EMPLOYER")
	if err := CheckTaxForm(taxForm); err != nil {
		t.Fatal(err)
	}
	summary := SummarizeTaxYear(2025, paystubs, []*models.FinanceTaxForm{taxForm}, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if len(summary.TaxYearEmployments) != 1 || len(summary.TaxYearPeople) != 1 {
		t.Fatalf("one person, one employer, however the names are written: %+v", summary.TaxYearEmployments)
	}
	employment := summary.TaxYearEmployments[0]
	if employment.TaxYearFigureSource != TaxYearFigureSourceW2 || employment.TaxFormID != "form-1" || !employment.IsPaystubYearComplete || employment.LatestPayDate != "2025-12-26" {
		t.Fatalf("the W-2's figures, and the paystubs reach the year's end: %+v", employment)
	}
	if len(employment.TaxYearComparisons) != 10 {
		t.Fatalf("every box either prints is compared: %+v", employment.TaxYearComparisons)
	}
	for _, comparison := range employment.TaxYearComparisons {
		if !comparison.IsMatching {
			t.Errorf("box %s %s matches: %+v", comparison.BoxCode, comparison.BoxQualifier, comparison)
		}
	}
	if len(summary.TaxYearFindings) != 0 {
		t.Fatalf("nothing to point out: %+v", summary.TaxYearFindings[0])
	}
	if summary.Figures.WagesAmount != "21600.00" || summary.Figures.HSAAmount != "900.00" || summary.ProjectedFigures != nil {
		t.Fatalf("the household's figures are the W-2's: %+v", summary.Figures)
	}

	taxForm.TaxFormEntries[1].Amount = "2900.0000"
	summary = SummarizeTaxYear(2025, paystubs, []*models.FinanceTaxForm{taxForm}, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if len(summary.TaxYearFindings) != 1 || summary.TaxYearFindings[0].TaxYearFindingCode != TaxYearFindingW2DiffersFromPaystubs {
		t.Fatalf("a box that differs is pointed out: %+v", summary.TaxYearFindings)
	}
}

func TestSummarizeTaxYearProjectsTheYear(t *testing.T) {
	paystubs := []*models.FinancePaystub{checkedPaystub(t, "paystub-1", "2026-03-06", 5), checkedPaystub(t, "paystub-2", "2026-03-20", 6)}
	summary := SummarizeTaxYear(2026, paystubs, nil, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	employment := summary.TaxYearEmployments[0]
	if employment.TaxYearFigureSource != TaxYearFigureSourcePaystubs || employment.PayCadenceDayCount != 14 || employment.RemainingPayCount != 20 {
		t.Fatalf("paystubs every 14 days, 20 more to December 31: %+v", employment)
	}
	if employment.Figures.WagesAmount != "21600.00" || employment.ProjectedFigures.WagesAmount != "93600.00" || employment.ProjectedFigures.FederalIncomeTaxAmount != "13000.00" {
		t.Fatalf("the year so far and to December 31: %+v %+v", employment.Figures, employment.ProjectedFigures)
	}
	if state := employment.ProjectedFigures.TaxYearStateFigures; len(state) != 1 || state[0].StateCode != "CA" || state[0].StateIncomeTaxAmount != "2520.70" {
		t.Fatalf("the state's tax to December 31 is 581.70 and 20 times 96.95: %+v", state)
	}
	if summary.ProjectedFigures == nil || len(summary.TaxYearFindings) != 0 {
		t.Fatalf("the household is projected and nothing is wrong: %+v", summary.TaxYearFindings)
	}
}

func TestSummarizeTaxYearFindsMissingPaystubs(t *testing.T) {
	broken := checkedPaystub(t, "paystub-2", "2026-03-20", 6)
	broken.PaystubLines[1].YearToDateAmount = "3100.0000"
	summary := SummarizeTaxYear(2026, []*models.FinancePaystub{checkedPaystub(t, "paystub-1", "2026-03-06", 5), broken}, nil, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if len(summary.TaxYearFindings) != 1 || summary.TaxYearFindings[0].TaxYearFindingCode != TaxYearFindingBrokenChain || !strings.Contains(summary.TaxYearFindings[0].FindingText, "3000.00") {
		t.Fatalf("a year to date that does not follow on is pointed out: %+v", summary.TaxYearFindings)
	}

	paystubs := []*models.FinancePaystub{
		checkedPaystub(t, "paystub-1", "2026-02-20", 4), checkedPaystub(t, "paystub-2", "2026-03-06", 5),
		checkedPaystub(t, "paystub-3", "2026-03-20", 6), checkedPaystub(t, "paystub-4", "2026-04-17", 8),
	}
	summary = SummarizeTaxYear(2026, paystubs, nil, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if len(summary.TaxYearFindings) != 1 || summary.TaxYearFindings[0].TaxYearFindingCode != TaxYearFindingPayDateGap {
		t.Fatalf("four weeks between paychecks every two is a gap: %+v", summary.TaxYearFindings)
	}

	summary = SummarizeTaxYear(2026, paystubs[:2], nil, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC))
	if codes := findingCodes(summary); len(codes) != 1 || codes[0] != TaxYearFindingW2Missing {
		t.Fatalf("after the year, paystubs without a W-2 are pointed out: %v", codes)
	}
}

func TestSummarizeTaxYearFindsExcessSocialSecurity(t *testing.T) {
	first, second := inventedW2("form-1", 2025, "Example Employer LLC"), inventedW2("form-2", 2025, "Another Example Inc")
	for _, taxForm := range []*models.FinanceTaxForm{first, second} {
		taxForm.TaxFormEntries[2].Amount, taxForm.TaxFormEntries[3].Amount = "100000.00", "6200.00"
	}
	summary := SummarizeTaxYear(2025, nil, []*models.FinanceTaxForm{first, second}, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if len(summary.TaxYearPeople) != 1 || summary.TaxYearPeople[0].ExcessSocialSecurityTaxAmount != "1481.80" {
		t.Fatalf("12,400.00 withheld against 2025's maximum of 10,918.20: %+v", summary.TaxYearPeople)
	}
	if codes := findingCodes(summary); len(codes) != 1 || codes[0] != TaxYearFindingExcessSocialSecurity {
		t.Fatalf("the excess is pointed out: %v", codes)
	}
}

func findingCodes(summary *TaxYearSummary) []TaxYearFindingCode {
	codes := []TaxYearFindingCode{}
	for _, finding := range summary.TaxYearFindings {
		codes = append(codes, finding.TaxYearFindingCode)
	}
	return codes
}
