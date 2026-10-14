package finance

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/models"
	normalization "golang.org/x/text/unicode/norm"
)

// A tax year is put together from the paystubs paid in it and the W-2s
// for it, per person and employer: the W-2's figures when there is one,
// the latest paystub's year to date when there is not, and a projection
// to December 31 from the pay cadence. Where both exist each W-2 box is
// set beside the paystubs' figure for it. Paystubs that do not chain, and
// gaps in the pay dates, are findings, so a missing paystub is noticed
// before the W-2 arrives.

// TaxYearFigureSource says where an employment's figures come from.
type TaxYearFigureSource string

const (
	TaxYearFigureSourceW2       TaxYearFigureSource = "w2"
	TaxYearFigureSourcePaystubs TaxYearFigureSource = "paystubs"
)

// TaxYearStateFigures is one state's wages and income tax withheld.
type TaxYearStateFigures struct {
	StateCode            string `json:"stateCode"`
	StateWagesAmount     string `json:"stateWagesAmount"`
	StateIncomeTaxAmount string `json:"stateIncomeTaxAmount"`
}

// TaxYearFigures are a year's wages and withholding as a W-2 names them.
// Amounts are dollars with cents.
type TaxYearFigures struct {
	// WagesAmount is W-2 box 1, FederalIncomeTaxAmount box 2.
	WagesAmount            string `json:"wagesAmount"`
	FederalIncomeTaxAmount string `json:"federalIncomeTaxAmount"`
	// Boxes 3 and 4.
	SocialSecurityWagesAmount string `json:"socialSecurityWagesAmount"`
	SocialSecurityTaxAmount   string `json:"socialSecurityTaxAmount"`
	// Boxes 5 and 6, additional Medicare included.
	MedicareWagesAmount string `json:"medicareWagesAmount"`
	MedicareTaxAmount   string `json:"medicareTaxAmount"`
	// DependentCareAmount is box 10.
	DependentCareAmount string `json:"dependentCareAmount"`
	// RetirementTraditionalAmount is box 12 D, E, F, G, H and S;
	// RetirementRothAmount box 12 AA, BB and EE.
	RetirementTraditionalAmount string `json:"retirementTraditionalAmount"`
	RetirementRothAmount        string `json:"retirementRothAmount"`
	// HSAAmount is box 12 W, the employer's and the employee's.
	HSAAmount string `json:"hsaAmount"`
	// HealthCoverageAmount is box 12 DD, the cost of the employer's
	// health coverage, the employer's part and the employee's.
	HealthCoverageAmount string `json:"healthCoverageAmount"`
	// LocalIncomeTaxAmount is box 19, every locality.
	LocalIncomeTaxAmount string                 `json:"localIncomeTaxAmount"`
	TaxYearStateFigures  []*TaxYearStateFigures `json:"taxYearStateFigures"`
}

// TaxYearComparison is one W-2 box beside the paystubs' figure for it.
type TaxYearComparison struct {
	BoxCode      string `json:"boxCode"`
	BoxQualifier string `json:"boxQualifier,omitempty" graphapi:"nullable"`
	// BoxLabel names the box for the person ("Federal income tax withheld").
	BoxLabel         string `json:"boxLabel"`
	W2Amount         string `json:"w2Amount"`
	PaystubAmount    string `json:"paystubAmount"`
	DifferenceAmount string `json:"differenceAmount"`
	IsMatching       bool   `json:"isMatching"`
}

// TaxYearFindingCode names what a tax year finding is about.
type TaxYearFindingCode string

const (
	// TaxYearFindingBrokenChain: a paystub's year to date is not the one
	// before it plus this period; a paystub in between is missing or one
	// of them was misread.
	TaxYearFindingBrokenChain TaxYearFindingCode = "broken_chain"
	// TaxYearFindingPayDateGap: more time between two paystubs than the
	// pay cadence; a paystub in between is missing.
	TaxYearFindingPayDateGap TaxYearFindingCode = "pay_date_gap"
	// TaxYearFindingDuplicatePaystub: two paystubs of the same pay date
	// and take-home pay.
	TaxYearFindingDuplicatePaystub TaxYearFindingCode = "duplicate_paystub"
	// TaxYearFindingW2Missing: the year is over, there are paystubs, and
	// no W-2 from that employer.
	TaxYearFindingW2Missing TaxYearFindingCode = "w2_missing"
	// TaxYearFindingW2DiffersFromPaystubs: the W-2 and the year's last
	// paystub disagree on a box.
	TaxYearFindingW2DiffersFromPaystubs TaxYearFindingCode = "w2_differs_from_paystubs"
	// TaxYearFindingSeveralW2s: more than one W-2 from one employer for
	// the year, none of them corrected; the latest is used.
	TaxYearFindingSeveralW2s TaxYearFindingCode = "several_w2s"
	// TaxYearFindingExcessSocialSecurity: a person's Social Security tax
	// across employers is more than the year's maximum; the excess comes
	// back as a credit on the return.
	TaxYearFindingExcessSocialSecurity TaxYearFindingCode = "excess_social_security"
)

// TaxYearFinding is one thing worth knowing about a tax year, in a
// sentence.
type TaxYearFinding struct {
	TaxYearFindingCode TaxYearFindingCode `json:"taxYearFindingCode"`
	EmployeeName       string             `json:"employeeName"`
	EmployerName       string             `json:"employerName,omitempty" graphapi:"nullable"`
	FindingText        string             `json:"findingText"`
	// PaystubIDs are the paystubs it is about, when any.
	PaystubIDs []string `json:"paystubIds"`
}

// TaxYearEmployment is one person's work for one employer in the year.
type TaxYearEmployment struct {
	EmployeeName  string `json:"employeeName"`
	EmployerName  string `json:"employerName"`
	EmployerTaxID string `json:"employerTaxId,omitempty" graphapi:"nullable"`

	// TaxYearFigureSource says whether Figures are the W-2's or the
	// latest paystub's year to date.
	TaxYearFigureSource TaxYearFigureSource `json:"taxYearFigureSource"`
	Figures             *TaxYearFigures     `json:"figures"`

	// TaxFormID is the W-2 used, when there is one.
	TaxFormID string `json:"taxFormId,omitempty" graphapi:"nullable"`
	// PaystubFigures are the latest paystub's year to date, when there
	// are paystubs; LatestPayDate is its pay date.
	PaystubFigures *TaxYearFigures `json:"paystubFigures,omitempty" graphapi:"nullable"`
	LatestPayDate  string          `json:"latestPayDate,omitempty" graphapi:"nullable"`
	PaystubCount   int             `json:"paystubCount"`
	// PayCadenceDayCount is the usual number of days between paychecks.
	PayCadenceDayCount int `json:"payCadenceDayCount,omitempty" graphapi:"nullable"`
	// IsPaystubYearComplete says the latest paystub is the year's last
	// one by the pay cadence.
	IsPaystubYearComplete bool `json:"isPaystubYearComplete"`

	// ProjectedFigures are the year to December 31, when there is no W-2
	// and the year is not over: the latest paystub's year to date plus
	// RemainingPayCount more paychecks like it.
	ProjectedFigures  *TaxYearFigures `json:"projectedFigures,omitempty" graphapi:"nullable"`
	RemainingPayCount int             `json:"remainingPayCount"`

	// TaxYearComparisons set each W-2 box beside the paystubs' figure,
	// when both exist.
	TaxYearComparisons []*TaxYearComparison `json:"taxYearComparisons"`
}

// TaxYearPerson is one person's totals across their employers.
type TaxYearPerson struct {
	EmployeeName string          `json:"employeeName"`
	Figures      *TaxYearFigures `json:"figures"`
	// ProjectedFigures are the totals to December 31: the W-2s where
	// there are some and the projections where there are not; empty when
	// nothing is projected.
	ProjectedFigures *TaxYearFigures `json:"projectedFigures,omitempty" graphapi:"nullable"`
	// ExcessSocialSecurityTaxAmount is Social Security tax withheld
	// beyond the year's maximum across several employers, "0.00" when
	// none or when the year's wage base is not known.
	ExcessSocialSecurityTaxAmount string `json:"excessSocialSecurityTaxAmount"`
}

// TaxYearSummary is a tax year put together from paystubs and W-2s.
type TaxYearSummary struct {
	TaxYear int `json:"taxYear"`
	// SocialSecurityWageBaseAmount is empty when TeaNode does not know
	// the year's.
	SocialSecurityWageBaseAmount string               `json:"socialSecurityWageBaseAmount,omitempty" graphapi:"nullable"`
	IsYearOver                   bool                 `json:"isYearOver"`
	TaxYearEmployments           []*TaxYearEmployment `json:"taxYearEmployments"`
	TaxYearPeople                []*TaxYearPerson     `json:"taxYearPeople"`
	// Figures are the household's totals; ProjectedFigures to December
	// 31 when anything is projected.
	Figures          *TaxYearFigures   `json:"figures"`
	ProjectedFigures *TaxYearFigures   `json:"projectedFigures,omitempty" graphapi:"nullable"`
	TaxYearFindings  []*TaxYearFinding `json:"taxYearFindings"`
	PaystubCount     int               `json:"paystubCount"`
	TaxFormCount     int               `json:"taxFormCount"`
}

// figureSums is TaxYearFigures while being added up.
type figureSums struct {
	wages, federalIncomeTax, socialSecurityWages, socialSecurityTax, medicareWages, medicareTax *big.Rat
	dependentCare, retirementTraditional, retirementRoth, hsa, healthCoverage, localIncomeTax   *big.Rat
	stateWagesByCode, stateTaxByCode                                                            map[string]*big.Rat
}

func newFigureSums() *figureSums {
	sums := &figureSums{stateWagesByCode: map[string]*big.Rat{}, stateTaxByCode: map[string]*big.Rat{}}
	for _, field := range sums.fields() {
		*field = new(big.Rat)
	}
	return sums
}

func (self *figureSums) fields() []**big.Rat {
	return []**big.Rat{&self.wages, &self.federalIncomeTax, &self.socialSecurityWages, &self.socialSecurityTax, &self.medicareWages, &self.medicareTax,
		&self.dependentCare, &self.retirementTraditional, &self.retirementRoth, &self.hsa, &self.healthCoverage, &self.localIncomeTax}
}

func (self *figureSums) add(other *figureSums) {
	otherFields := other.fields()
	for index, field := range self.fields() {
		(*field).Add(*field, *otherFields[index])
	}
	for code, amountValue := range other.stateWagesByCode {
		self.addState(code, amountValue, nil)
	}
	for code, amountValue := range other.stateTaxByCode {
		self.addState(code, nil, amountValue)
	}
}

func (self *figureSums) addState(stateCode string, wagesAmount, taxAmount *big.Rat) {
	for _, pair := range []struct {
		byCode      map[string]*big.Rat
		amountValue *big.Rat
	}{{self.stateWagesByCode, wagesAmount}, {self.stateTaxByCode, taxAmount}} {
		if pair.amountValue == nil {
			continue
		}
		if pair.byCode[stateCode] == nil {
			pair.byCode[stateCode] = new(big.Rat)
		}
		pair.byCode[stateCode].Add(pair.byCode[stateCode], pair.amountValue)
	}
}

func (self *figureSums) figures() *TaxYearFigures {
	figures := &TaxYearFigures{
		WagesAmount: formatDollars(self.wages), FederalIncomeTaxAmount: formatDollars(self.federalIncomeTax),
		SocialSecurityWagesAmount: formatDollars(self.socialSecurityWages), SocialSecurityTaxAmount: formatDollars(self.socialSecurityTax),
		MedicareWagesAmount: formatDollars(self.medicareWages), MedicareTaxAmount: formatDollars(self.medicareTax),
		DependentCareAmount: formatDollars(self.dependentCare), RetirementTraditionalAmount: formatDollars(self.retirementTraditional),
		RetirementRothAmount: formatDollars(self.retirementRoth), HSAAmount: formatDollars(self.hsa),
		HealthCoverageAmount: formatDollars(self.healthCoverage), LocalIncomeTaxAmount: formatDollars(self.localIncomeTax),
		TaxYearStateFigures: []*TaxYearStateFigures{},
	}
	stateCodes := map[string]bool{}
	for code := range self.stateWagesByCode {
		stateCodes[code] = true
	}
	for code := range self.stateTaxByCode {
		stateCodes[code] = true
	}
	for _, code := range sortedKeys(stateCodes) {
		wagesAmount, taxAmount := self.stateWagesByCode[code], self.stateTaxByCode[code]
		if wagesAmount == nil {
			wagesAmount = new(big.Rat)
		}
		if taxAmount == nil {
			taxAmount = new(big.Rat)
		}
		figures.TaxYearStateFigures = append(figures.TaxYearStateFigures, &TaxYearStateFigures{
			StateCode: code, StateWagesAmount: formatDollars(wagesAmount), StateIncomeTaxAmount: formatDollars(taxAmount),
		})
	}
	return figures
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var (
	retirementTraditionalCodes = []string{"D", "E", "F", "G", "H", "S"}
	retirementRothCodes        = []string{"AA", "BB", "EE"}
)

// w2Sums are a W-2's figures.
func w2Sums(taxForm *models.FinanceTaxForm) *figureSums {
	sums := newFigureSums()
	for _, pair := range []struct {
		field   **big.Rat
		boxCode string
		codes   []string
	}{
		{&sums.wages, "1", nil}, {&sums.federalIncomeTax, "2", nil}, {&sums.socialSecurityWages, "3", nil}, {&sums.socialSecurityTax, "4", nil},
		{&sums.medicareWages, "5", nil}, {&sums.medicareTax, "6", nil}, {&sums.dependentCare, "10", nil},
		{&sums.retirementTraditional, "12", retirementTraditionalCodes}, {&sums.retirementRoth, "12", retirementRothCodes},
		{&sums.hsa, "12", []string{"W"}}, {&sums.healthCoverage, "12", []string{"DD"}}, {&sums.localIncomeTax, "19", nil},
	} {
		amountValue, _ := taxFormAmount(taxForm, pair.boxCode, pair.codes...)
		*pair.field = amountValue
	}
	for _, entry := range taxForm.TaxFormEntries {
		if entry == nil || entry.Amount == "" || (entry.BoxCode != "16" && entry.BoxCode != "17") {
			continue
		}
		amountValue, err := parseDecimal(entry.Amount)
		if err != nil {
			continue
		}
		if entry.BoxCode == "16" {
			sums.addState(entry.BoxQualifier, amountValue, nil)
		} else {
			sums.addState(entry.BoxQualifier, nil, amountValue)
		}
	}
	return sums
}

// paystubSums are a paystub's figures, this period when isCurrent and
// for the year to date otherwise: each tax's wages and amount, and the
// deductions a W-2 names, the employer's part with the employee's where
// the W-2 counts both. When the federal income tax line prints no wages,
// box 1 is gross pay less the deductions taken before income tax.
func paystubSums(paystub *models.FinancePaystub, isCurrent bool) *figureSums {
	sums := newFigureSums()
	pick := func(currentText, yearToDateText string) *big.Rat {
		text := yearToDateText
		if isCurrent {
			text = currentText
		}
		if amountValue, err := parseDecimal(text); err == nil {
			return amountValue
		}
		return nil
	}
	hasFederalWages := false
	beforeIncomeTax := new(big.Rat)
	for _, line := range paystub.PaystubLines {
		if line == nil {
			continue
		}
		amountValue := pick(line.CurrentAmount, line.YearToDateAmount)
		wagesAmount := pick(line.CurrentWagesAmount, line.YearToDateWagesAmount)
		employerAmount := pick(line.EmployerCurrentAmount, line.EmployerYearToDateAmount)
		if amountValue == nil {
			amountValue = new(big.Rat)
		}
		switch line.PaystubLineKind {
		case models.PaystubLineKindTax:
			switch line.PaystubTaxKind {
			case models.PaystubTaxKindFederalIncome:
				sums.federalIncomeTax.Add(sums.federalIncomeTax, amountValue)
				if wagesAmount != nil {
					sums.wages.Add(sums.wages, wagesAmount)
					hasFederalWages = true
				}
			case models.PaystubTaxKindSocialSecurity:
				sums.socialSecurityTax.Add(sums.socialSecurityTax, amountValue)
				if wagesAmount != nil {
					sums.socialSecurityWages.Add(sums.socialSecurityWages, wagesAmount)
				}
			case models.PaystubTaxKindMedicare:
				sums.medicareTax.Add(sums.medicareTax, amountValue)
				if wagesAmount != nil {
					sums.medicareWages.Add(sums.medicareWages, wagesAmount)
				}
			case models.PaystubTaxKindMedicareAdditional:
				// Its wages are only the part above the threshold, already in
				// Medicare's own.
				sums.medicareTax.Add(sums.medicareTax, amountValue)
			case models.PaystubTaxKindStateIncome:
				sums.addState(line.TaxJurisdictionCode, wagesAmount, amountValue)
			case models.PaystubTaxKindLocalIncome:
				sums.localIncomeTax.Add(sums.localIncomeTax, amountValue)
			}
		case models.PaystubLineKindDeduction:
			withEmployer := new(big.Rat).Set(amountValue)
			if employerAmount != nil {
				withEmployer.Add(withEmployer, employerAmount)
			}
			switch line.PaystubDeductionKind {
			case models.PaystubDeductionKindRetirementTraditional:
				sums.retirementTraditional.Add(sums.retirementTraditional, amountValue)
				beforeIncomeTax.Add(beforeIncomeTax, amountValue)
			case models.PaystubDeductionKindRetirementRoth:
				sums.retirementRoth.Add(sums.retirementRoth, amountValue)
			case models.PaystubDeductionKindHSA:
				sums.hsa.Add(sums.hsa, withEmployer)
				beforeIncomeTax.Add(beforeIncomeTax, amountValue)
			case models.PaystubDeductionKindHealthPremium:
				sums.healthCoverage.Add(sums.healthCoverage, withEmployer)
				beforeIncomeTax.Add(beforeIncomeTax, amountValue)
			case models.PaystubDeductionKindFSADependentCare:
				sums.dependentCare.Add(sums.dependentCare, withEmployer)
				beforeIncomeTax.Add(beforeIncomeTax, amountValue)
			case models.PaystubDeductionKindFSAHealth, models.PaystubDeductionKindCommuter:
				beforeIncomeTax.Add(beforeIncomeTax, amountValue)
			}
		}
	}
	if !hasFederalWages {
		gross := pick(paystub.GrossAmount, paystub.YearToDateGrossAmount)
		if gross != nil {
			sums.wages = new(big.Rat).Sub(gross, beforeIncomeTax)
		}
	}
	return sums
}

// taxYearNameKey reads a person's or an employer's name for telling them
// apart: NFKC, upper case, letters and digits only, and an employer's
// legal suffixes dropped, so "Example Corp." and "EXAMPLE CORP" are one.
func taxYearNameKey(name string, isEmployer bool) string {
	words := strings.FieldsFunc(strings.ToUpper(normalization.NFKC.String(name)), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	kept := words[:0]
	for _, word := range words {
		if isEmployer && employerSuffixes[word] {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

var employerSuffixes = map[string]bool{"INC": true, "INCORPORATED": true, "CORP": true, "CORPORATION": true, "CO": true, "COMPANY": true, "LLC": true, "LTD": true, "LP": true, "LLP": true, "PLLC": true, "PC": true}

type employmentGroup struct {
	key, employeeKey   string
	employment         *TaxYearEmployment
	paystubs           []*models.FinancePaystub
	taxForms           []*models.FinanceTaxForm
	figures, projected *figureSums
	hasProjection      bool
}

// SummarizeTaxYear puts a tax year together from the paystubs paid in it
// and the W-2s for it (others are left out), as of today.
func SummarizeTaxYear(taxYear int, paystubs []*models.FinancePaystub, taxForms []*models.FinanceTaxForm, today time.Time) *TaxYearSummary {
	summary := &TaxYearSummary{
		TaxYear: taxYear, IsYearOver: today.Year() > taxYear,
		TaxYearEmployments: []*TaxYearEmployment{}, TaxYearPeople: []*TaxYearPerson{}, TaxYearFindings: []*TaxYearFinding{},
	}
	wageBase, isWageBaseKnown := SocialSecurityWageBase(taxYear)
	if isWageBaseKnown {
		summary.SocialSecurityWageBaseAmount = formatDollars(wageBase)
	}
	groups := map[string]*employmentGroup{}
	var order []string
	groupOf := func(employeeName, employerName, employerTaxID string) *employmentGroup {
		employeeKey := taxYearNameKey(employeeName, false)
		key := employeeKey + "\x00" + taxYearNameKey(employerName, true)
		group := groups[key]
		if group == nil {
			group = &employmentGroup{key: key, employeeKey: employeeKey, employment: &TaxYearEmployment{
				EmployeeName: employeeName, EmployerName: employerName, EmployerTaxID: employerTaxID, TaxYearComparisons: []*TaxYearComparison{},
			}}
			groups[key] = group
			order = append(order, key)
		}
		if group.employment.EmployerTaxID == "" {
			group.employment.EmployerTaxID = employerTaxID
		}
		return group
	}
	for _, paystub := range paystubs {
		if paystub == nil || !strings.HasPrefix(paystub.PayDate, fmt.Sprintf("%04d-", taxYear)) {
			continue
		}
		group := groupOf(paystub.EmployeeName, paystub.EmployerName, paystub.EmployerTaxID)
		group.paystubs = append(group.paystubs, paystub)
		summary.PaystubCount++
	}
	for _, taxForm := range taxForms {
		if taxForm == nil || taxForm.TaxYear != taxYear || taxForm.TaxFormKind != models.TaxFormKindW2 {
			continue
		}
		group := groupOf(taxForm.RecipientName, taxForm.PayerName, taxForm.PayerTaxID)
		group.taxForms = append(group.taxForms, taxForm)
		summary.TaxFormCount++
	}

	household, householdProjected := newFigureSums(), newFigureSums()
	isAnythingProjected := false
	for _, key := range order {
		group := groups[key]
		employment := group.employment
		sort.SliceStable(group.paystubs, func(left, right int) bool {
			if group.paystubs[left].PayDate != group.paystubs[right].PayDate {
				return group.paystubs[left].PayDate < group.paystubs[right].PayDate
			}
			return group.paystubs[left].ID < group.paystubs[right].ID
		})
		employment.PaystubCount = len(group.paystubs)
		summary.TaxYearFindings = append(summary.TaxYearFindings, paystubChainFindings(employment, group.paystubs)...)
		var paystubYear *figureSums
		if len(group.paystubs) > 0 {
			latest := group.paystubs[len(group.paystubs)-1]
			employment.LatestPayDate = latest.PayDate
			paystubYear = paystubSums(latest, false)
			employment.PaystubFigures = paystubYear.figures()
			employment.PayCadenceDayCount = payCadenceDayCount(group.paystubs)
			latestPayDate, _ := time.Parse(time.DateOnly, latest.PayDate)
			yearEnd := time.Date(taxYear, time.December, 31, 0, 0, 0, 0, time.UTC)
			employment.RemainingPayCount = int(yearEnd.Sub(latestPayDate).Hours()/24) / employment.PayCadenceDayCount
			employment.IsPaystubYearComplete = employment.RemainingPayCount == 0
		}
		taxForm := chooseW2(group.taxForms)
		if len(group.taxForms) > 1 && !taxForm.IsCorrected {
			summary.TaxYearFindings = append(summary.TaxYearFindings, &TaxYearFinding{
				TaxYearFindingCode: TaxYearFindingSeveralW2s, EmployeeName: employment.EmployeeName, EmployerName: employment.EmployerName,
				FindingText: fmt.Sprintf("%d W-2s from %s for %d; the latest is used", len(group.taxForms), employment.EmployerName, taxYear), PaystubIDs: []string{},
			})
		}
		switch {
		case taxForm != nil:
			employment.TaxYearFigureSource, employment.TaxFormID = TaxYearFigureSourceW2, taxForm.ID
			group.figures = w2Sums(taxForm)
			group.projected = group.figures
			if paystubYear != nil {
				employment.TaxYearComparisons = compareW2WithPaystubs(group.figures, paystubYear)
				differing := 0
				for _, comparison := range employment.TaxYearComparisons {
					if !comparison.IsMatching {
						differing++
					}
				}
				if differing > 0 && employment.IsPaystubYearComplete {
					summary.TaxYearFindings = append(summary.TaxYearFindings, &TaxYearFinding{
						TaxYearFindingCode: TaxYearFindingW2DiffersFromPaystubs, EmployeeName: employment.EmployeeName, EmployerName: employment.EmployerName,
						FindingText: fmt.Sprintf("the W-2 from %s differs from the year's last paystub in %d %s", employment.EmployerName, differing, pluralize(differing, "box", "boxes")),
						PaystubIDs:  []string{group.paystubs[len(group.paystubs)-1].ID},
					})
				}
			}
		default:
			employment.TaxYearFigureSource = TaxYearFigureSourcePaystubs
			group.figures = paystubYear
			group.projected = paystubYear
			if summary.IsYearOver {
				summary.TaxYearFindings = append(summary.TaxYearFindings, &TaxYearFinding{
					TaxYearFindingCode: TaxYearFindingW2Missing, EmployeeName: employment.EmployeeName, EmployerName: employment.EmployerName,
					FindingText: fmt.Sprintf("no W-2 from %s for %d yet; employers send them by January 31", employment.EmployerName, taxYear), PaystubIDs: []string{},
				})
			} else if employment.RemainingPayCount > 0 {
				group.projected = projectPaystubs(paystubYear, paystubSums(group.paystubs[len(group.paystubs)-1], true), employment.RemainingPayCount, wageBase, isWageBaseKnown)
				employment.ProjectedFigures = group.projected.figures()
				group.hasProjection = true
				isAnythingProjected = true
			}
		}
		employment.Figures = group.figures.figures()
		household.add(group.figures)
		householdProjected.add(group.projected)
		summary.TaxYearEmployments = append(summary.TaxYearEmployments, employment)
	}

	// People, in the order they first appear.
	var employeeOrder []string
	byEmployee := map[string][]*employmentGroup{}
	for _, key := range order {
		group := groups[key]
		if byEmployee[group.employeeKey] == nil {
			employeeOrder = append(employeeOrder, group.employeeKey)
		}
		byEmployee[group.employeeKey] = append(byEmployee[group.employeeKey], group)
	}
	for _, employeeKey := range employeeOrder {
		personGroups := byEmployee[employeeKey]
		figures, projected := newFigureSums(), newFigureSums()
		hasProjection := false
		for _, group := range personGroups {
			figures.add(group.figures)
			projected.add(group.projected)
			hasProjection = hasProjection || group.hasProjection
		}
		person := &TaxYearPerson{EmployeeName: personGroups[0].employment.EmployeeName, Figures: figures.figures(), ExcessSocialSecurityTaxAmount: "0.00"}
		if hasProjection {
			person.ProjectedFigures = projected.figures()
		}
		if isWageBaseKnown && len(personGroups) > 1 {
			maximumTax := roundToCents(new(big.Rat).Mul(wageBase, socialSecurityRate))
			if excess := new(big.Rat).Sub(figures.socialSecurityTax, maximumTax); excess.Sign() > 0 {
				person.ExcessSocialSecurityTaxAmount = formatDollars(excess)
				summary.TaxYearFindings = append(summary.TaxYearFindings, &TaxYearFinding{
					TaxYearFindingCode: TaxYearFindingExcessSocialSecurity, EmployeeName: person.EmployeeName,
					FindingText: fmt.Sprintf("%s had %s in Social Security tax withheld across employers, %s more than %d's maximum of %s; the excess comes back as a credit on the return",
						person.EmployeeName, formatDollars(figures.socialSecurityTax), formatDollars(excess), taxYear, formatDollars(maximumTax)),
					PaystubIDs: []string{},
				})
			}
		}
		summary.TaxYearPeople = append(summary.TaxYearPeople, person)
	}
	summary.Figures = household.figures()
	if isAnythingProjected {
		summary.ProjectedFigures = householdProjected.figures()
	}
	return summary
}

func pluralize(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// chooseW2 is the W-2 to use among an employer's for the year: the latest
// corrected one, else the latest.
func chooseW2(taxForms []*models.FinanceTaxForm) *models.FinanceTaxForm {
	var chosen *models.FinanceTaxForm
	for _, taxForm := range taxForms {
		switch {
		case chosen == nil:
			chosen = taxForm
		case taxForm.IsCorrected != chosen.IsCorrected:
			if taxForm.IsCorrected {
				chosen = taxForm
			}
		case taxForm.CreatedAt.After(chosen.CreatedAt):
			chosen = taxForm
		}
	}
	return chosen
}

// payCadenceDayCount is the usual number of days between paychecks: the
// median of the gaps between pay dates, else the pay period's length,
// else 14.
func payCadenceDayCount(paystubs []*models.FinancePaystub) int {
	var gaps []int
	for index := 1; index < len(paystubs); index++ {
		previous, errPrevious := time.Parse(time.DateOnly, paystubs[index-1].PayDate)
		current, errCurrent := time.Parse(time.DateOnly, paystubs[index].PayDate)
		if errPrevious == nil && errCurrent == nil {
			if gap := int(current.Sub(previous).Hours() / 24); gap > 0 {
				gaps = append(gaps, gap)
			}
		}
	}
	if len(gaps) > 0 {
		sort.Ints(gaps)
		return gaps[len(gaps)/2]
	}
	latest := paystubs[len(paystubs)-1]
	start, errStart := time.Parse(time.DateOnly, latest.PeriodStartOn)
	end, errEnd := time.Parse(time.DateOnly, latest.PeriodEndOn)
	if errStart == nil && errEnd == nil {
		if length := int(end.Sub(start).Hours()/24) + 1; length >= 7 {
			return length
		}
	}
	return 14
}

// paystubChainFindings are the duplicates, the gaps and the broken chains
// among one employment's paystubs in pay date order.
func paystubChainFindings(employment *TaxYearEmployment, paystubs []*models.FinancePaystub) []*TaxYearFinding {
	findings := []*TaxYearFinding{}
	if len(paystubs) < 2 {
		return findings
	}
	cadence := payCadenceDayCount(paystubs)
	for index := 1; index < len(paystubs); index++ {
		previous, current := paystubs[index-1], paystubs[index]
		ids := []string{previous.ID, current.ID}
		if previous.PayDate == current.PayDate && previous.NetAmount == current.NetAmount {
			findings = append(findings, &TaxYearFinding{
				TaxYearFindingCode: TaxYearFindingDuplicatePaystub, EmployeeName: employment.EmployeeName, EmployerName: employment.EmployerName,
				FindingText: fmt.Sprintf("two paystubs from %s for %s with the same take-home pay; one may be a copy", employment.EmployerName, current.PayDate), PaystubIDs: ids,
			})
			continue
		}
		previousDay, _ := time.Parse(time.DateOnly, previous.PayDate)
		currentDay, _ := time.Parse(time.DateOnly, current.PayDate)
		if gap := int(currentDay.Sub(previousDay).Hours() / 24); gap*2 > cadence*3 {
			findings = append(findings, &TaxYearFinding{
				TaxYearFindingCode: TaxYearFindingPayDateGap, EmployeeName: employment.EmployeeName, EmployerName: employment.EmployerName,
				FindingText: fmt.Sprintf("no paystub from %s between %s and %s, %d days apart when paychecks come every %d", employment.EmployerName, previous.PayDate, current.PayDate, gap, cadence),
				PaystubIDs:  ids,
			})
			continue
		}
		if line := brokenChainLine(previous, current); line != "" {
			findings = append(findings, &TaxYearFinding{
				TaxYearFindingCode: TaxYearFindingBrokenChain, EmployeeName: employment.EmployeeName, EmployerName: employment.EmployerName,
				FindingText: fmt.Sprintf("the paystub of %s does not follow on from the one of %s: %s", current.PayDate, previous.PayDate, line),
				PaystubIDs:  ids,
			})
		}
	}
	return findings
}

// brokenChainLine says the first figure where the previous paystub's year
// to date plus this one's period is not this one's year to date, or "".
// Gross pay is looked at first, then each tax by kind and jurisdiction.
func brokenChainLine(previous, current *models.FinancePaystub) string {
	check := func(what, previousYearToDate, currentAmount, currentYearToDate string) string {
		previousValue, errPrevious := parseDecimal(previousYearToDate)
		currentValue, errCurrent := parseDecimal(currentAmount)
		currentYearToDateValue, errCurrentYearToDate := parseDecimal(currentYearToDate)
		if errPrevious != nil || errCurrent != nil || errCurrentYearToDate != nil {
			return ""
		}
		expected := new(big.Rat).Add(previousValue, currentValue)
		if expected.Cmp(currentYearToDateValue) == 0 {
			return ""
		}
		return fmt.Sprintf("%s year to date should be %s and is %s", what, formatDollars(expected), formatDollars(currentYearToDateValue))
	}
	if line := check("gross pay", previous.YearToDateGrossAmount, current.GrossAmount, current.YearToDateGrossAmount); line != "" {
		return line
	}
	previousTaxes := map[string]*models.FinancePaystubLine{}
	for _, line := range previous.PaystubLines {
		if line != nil && line.PaystubLineKind == models.PaystubLineKindTax {
			previousTaxes[string(line.PaystubTaxKind)+"\x00"+line.TaxJurisdictionCode] = line
		}
	}
	for _, line := range current.PaystubLines {
		if line == nil || line.PaystubLineKind != models.PaystubLineKindTax {
			continue
		}
		previousLine := previousTaxes[string(line.PaystubTaxKind)+"\x00"+line.TaxJurisdictionCode]
		if previousLine == nil {
			continue
		}
		what := line.Description
		if what == "" {
			what = string(line.PaystubTaxKind)
		}
		currentAmount := line.CurrentAmount
		if currentAmount == "" {
			currentAmount = "0"
		}
		if text := check(what, previousLine.YearToDateAmount, currentAmount, line.YearToDateAmount); text != "" {
			return text
		}
	}
	return ""
}

// projectPaystubs is the year to December 31: the year to date plus
// remainingPayCount paychecks like the latest, Social Security stopping at
// the year's wage base when TeaNode knows it.
func projectPaystubs(yearToDate, current *figureSums, remainingPayCount int, wageBase *big.Rat, isWageBaseKnown bool) *figureSums {
	projected := newFigureSums()
	count := big.NewRat(int64(remainingPayCount), 1)
	currentFields := current.fields()
	yearFields := yearToDate.fields()
	for index, field := range projected.fields() {
		*field = new(big.Rat).Add(*yearFields[index], new(big.Rat).Mul(*currentFields[index], count))
	}
	for code, amountValue := range yearToDate.stateWagesByCode {
		projected.addState(code, amountValue, nil)
	}
	for code, amountValue := range yearToDate.stateTaxByCode {
		projected.addState(code, nil, amountValue)
	}
	for code, amountValue := range current.stateWagesByCode {
		projected.addState(code, new(big.Rat).Mul(amountValue, count), nil)
	}
	for code, amountValue := range current.stateTaxByCode {
		projected.addState(code, nil, new(big.Rat).Mul(amountValue, count))
	}
	if isWageBaseKnown && projected.socialSecurityWages.Cmp(wageBase) > 0 {
		projected.socialSecurityWages = new(big.Rat).Set(wageBase)
		maximumTax := roundToCents(new(big.Rat).Mul(wageBase, socialSecurityRate))
		if projected.socialSecurityTax.Cmp(maximumTax) > 0 {
			projected.socialSecurityTax = maximumTax
		}
	}
	return projected
}

// compareW2WithPaystubs sets each W-2 figure beside the paystubs', for the
// boxes either prints. Box 12 DD is left out: it counts the cost of
// medical coverage only, while a paystub's premiums often include dental
// and vision plans that the W-2 leaves out, so the two need not agree.
func compareW2WithPaystubs(w2, paystubs *figureSums) []*TaxYearComparison {
	comparisons := []*TaxYearComparison{}
	add := func(boxCode, boxQualifier, boxLabel string, w2Amount, paystubAmount *big.Rat) {
		if w2Amount == nil {
			w2Amount = new(big.Rat)
		}
		if paystubAmount == nil {
			paystubAmount = new(big.Rat)
		}
		if w2Amount.Sign() == 0 && paystubAmount.Sign() == 0 {
			return
		}
		difference := new(big.Rat).Sub(w2Amount, paystubAmount)
		comparisons = append(comparisons, &TaxYearComparison{
			BoxCode: boxCode, BoxQualifier: boxQualifier, BoxLabel: boxLabel,
			W2Amount: formatDollars(w2Amount), PaystubAmount: formatDollars(paystubAmount), DifferenceAmount: formatDollars(difference),
			IsMatching: roundToCents(difference).Sign() == 0,
		})
	}
	add("1", "", "Wages", w2.wages, paystubs.wages)
	add("2", "", "Federal income tax withheld", w2.federalIncomeTax, paystubs.federalIncomeTax)
	add("3", "", "Social Security wages", w2.socialSecurityWages, paystubs.socialSecurityWages)
	add("4", "", "Social Security tax withheld", w2.socialSecurityTax, paystubs.socialSecurityTax)
	add("5", "", "Medicare wages", w2.medicareWages, paystubs.medicareWages)
	add("6", "", "Medicare tax withheld", w2.medicareTax, paystubs.medicareTax)
	add("10", "", "Dependent care benefits", w2.dependentCare, paystubs.dependentCare)
	add("12", "D", "Traditional retirement plan", w2.retirementTraditional, paystubs.retirementTraditional)
	add("12", "AA", "Roth retirement plan", w2.retirementRoth, paystubs.retirementRoth)
	add("12", "W", "HSA", w2.hsa, paystubs.hsa)
	stateCodes := map[string]bool{}
	for code := range w2.stateWagesByCode {
		stateCodes[code] = true
	}
	for code := range paystubs.stateWagesByCode {
		stateCodes[code] = true
	}
	for code := range w2.stateTaxByCode {
		stateCodes[code] = true
	}
	for code := range paystubs.stateTaxByCode {
		stateCodes[code] = true
	}
	for _, code := range sortedKeys(stateCodes) {
		add("16", code, "State wages", w2.stateWagesByCode[code], paystubs.stateWagesByCode[code])
		add("17", code, "State income tax", w2.stateTaxByCode[code], paystubs.stateTaxByCode[code])
	}
	add("19", "", "Local income tax", w2.localIncomeTax, paystubs.localIncomeTax)
	return comparisons
}
