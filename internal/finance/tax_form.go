package finance

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/models"
	normalization "golang.org/x/text/unicode/norm"
)

// A tax form is stored box by box as printed. Each kind's boxes are a
// catalog here, so a form is refused when it names a box its kind does not
// have; TaxFormFindings works out what the form says about itself (a W-2's
// Social Security and Medicare against their rates) each time it is read.

// taxFormBoxQualifier says what tells boxes of one number apart.
type taxFormBoxQualifier int

const (
	// qualifierNone: the box is one, its qualifier empty.
	qualifierNone taxFormBoxQualifier = iota
	// qualifierCode: one of a fixed list of codes (box 12's letters).
	qualifierCode
	// qualifierCheckbox: one of a fixed list of checkboxes (box 13's).
	qualifierCheckbox
	// qualifierState: a state's two-letter code (boxes 15 to 17).
	qualifierState
	// qualifierLabel: free text (box 14's label, a locality in boxes 18
	// to 20).
	qualifierLabel
)

type taxFormBox struct {
	boxQualifier taxFormBoxQualifier
	// isText: the box holds text (an employer's state id), not an amount.
	isText bool
	// isBusinessNumber: the text may hold nine digits, being a business's
	// number and not a person's.
	isBusinessNumber bool
}

// w2BoxCodes are the codes box 12 takes.
var w2BoxCodes = map[string]bool{}

func init() {
	for _, code := range strings.Fields("A B C D E F G H J K L M N P Q R S T V W Y Z AA BB DD EE FF GG HH II") {
		w2BoxCodes[code] = true
	}
}

// w2Checkboxes are box 13's.
var w2Checkboxes = map[string]bool{"statutory_employee": true, "retirement_plan": true, "third_party_sick_pay": true}

var taxFormBoxesByKind = map[models.TaxFormKind]map[string]taxFormBox{
	models.TaxFormKindW2: {
		"1": {}, "2": {}, "3": {}, "4": {}, "5": {}, "6": {}, "7": {}, "8": {}, "10": {}, "11": {},
		"12": {boxQualifier: qualifierCode},
		"13": {boxQualifier: qualifierCheckbox},
		"14": {boxQualifier: qualifierLabel},
		"15": {boxQualifier: qualifierState, isText: true, isBusinessNumber: true},
		"16": {boxQualifier: qualifierState},
		"17": {boxQualifier: qualifierState},
		"18": {boxQualifier: qualifierLabel},
		"19": {boxQualifier: qualifierLabel},
		"20": {boxQualifier: qualifierLabel, isText: true},
	},
}

// CheckTaxForm checks a tax form as read and normalizes it in place: text
// NFKC with its spaces collapsed, box codes and qualifiers in their
// canonical case, amounts canonical, the recipient's taxpayer number cut
// to its last four digits. It refuses a kind it does not know, a year
// outside 1900 to 2999, a box its kind does not have, a qualifier its box
// does not take, the same box and qualifier twice, an amount below zero
// or with more places than dollars have, a box with neither amount nor
// text nor mark, and any text holding a person's taxpayer number.
func CheckTaxForm(taxForm *models.FinanceTaxForm) error {
	if taxForm == nil {
		return refuseTaxDocument("there is no tax form")
	}
	boxes, isKnown := taxFormBoxesByKind[taxForm.TaxFormKind]
	if !isKnown {
		return refuseTaxDocument("a tax form of kind %q is not one TeaNode reads; it reads w2", taxForm.TaxFormKind)
	}
	if taxForm.TaxYear < 1900 || taxForm.TaxYear > 2999 {
		return refuseTaxDocument("the tax year %d is not a year", taxForm.TaxYear)
	}
	for _, field := range []struct {
		what string
		text *string
	}{{"the payer's name", &taxForm.PayerName}, {"the recipient's name", &taxForm.RecipientName}} {
		if err := cleanTaxDocumentText(field.what, field.text); err != nil {
			return err
		}
	}
	var err error
	if taxForm.PayerTaxID, err = cleanBusinessTaxID("the payer's identification number", taxForm.PayerTaxID); err != nil {
		return err
	}
	if taxForm.RecipientTaxIDMask, err = taxIDMask("the recipient's taxpayer number", taxForm.RecipientTaxIDMask, refuseTaxDocument); err != nil {
		return err
	}
	if len(taxForm.TaxFormEntries) == 0 {
		return refuseTaxDocument("give the form's boxes that print something")
	}
	isNumbered := false
	for _, entry := range taxForm.TaxFormEntries {
		isNumbered = isNumbered || (entry != nil && entry.EntryNumber != 0)
	}
	seenEntryNumbers, seenBoxes := map[int]bool{}, map[string]bool{}
	for index, entry := range taxForm.TaxFormEntries {
		if entry == nil {
			return refuseTaxDocument("box entry %d is empty", index+1)
		}
		if !isNumbered {
			entry.EntryNumber = index + 1
		}
		if entry.EntryNumber <= 0 || seenEntryNumbers[entry.EntryNumber] {
			return refuseTaxDocument("entry numbers start at 1 and are each used once; %d is not", entry.EntryNumber)
		}
		seenEntryNumbers[entry.EntryNumber] = true
		entry.BoxCode = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(entry.BoxCode)), "box"))
		box, isBox := boxes[entry.BoxCode]
		if !isBox {
			return refuseTaxDocument("a %s has no box %q", taxFormKindName(taxForm.TaxFormKind), entry.BoxCode)
		}
		label := "box " + entry.BoxCode
		entry.BoxQualifier = strings.Join(strings.Fields(normalization.NFKC.String(entry.BoxQualifier)), " ")
		switch box.boxQualifier {
		case qualifierNone:
			entry.BoxQualifier = ""
		case qualifierCode:
			entry.BoxQualifier = strings.ToUpper(entry.BoxQualifier)
			if !w2BoxCodes[entry.BoxQualifier] {
				return refuseTaxDocument("%s takes a code letter such as D or W; %q is not one", label, entry.BoxQualifier)
			}
		case qualifierCheckbox:
			entry.BoxQualifier = strings.ReplaceAll(strings.ToLower(entry.BoxQualifier), " ", "_")
			if !w2Checkboxes[entry.BoxQualifier] {
				return refuseTaxDocument("%s is one of statutory_employee, retirement_plan or third_party_sick_pay; %q is not one", label, entry.BoxQualifier)
			}
		case qualifierState:
			entry.BoxQualifier = strings.ToUpper(entry.BoxQualifier)
			if len(entry.BoxQualifier) != 2 || strings.Trim(entry.BoxQualifier, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
				return refuseTaxDocument("%s takes the state's two-letter code; %q is not one", label, entry.BoxQualifier)
			}
		case qualifierLabel:
			if err := cleanTaxDocumentText(label+"'s label", &entry.BoxQualifier); err != nil {
				return err
			}
		}
		if entry.BoxQualifier != "" {
			label += " " + entry.BoxQualifier
		}
		boxKey := entry.BoxCode + "\x00" + entry.BoxQualifier
		if seenBoxes[boxKey] {
			return refuseTaxDocument("%s is given twice; a form prints each box once", label)
		}
		seenBoxes[boxKey] = true
		entry.TextValue = strings.Join(strings.Fields(normalization.NFKC.String(entry.TextValue)), " ")
		if (!box.isBusinessNumber && HasTaxpayerNumber(entry.TextValue)) || HasDashedTaxpayerNumber(entry.TextValue) {
			return refuseTaxDocument("%s holds a number shaped like a taxpayer number; leave it out", label)
		}
		amountValue, err := optionalTaxDocumentAmount(label, &entry.Amount, "USD", false)
		if err != nil {
			return err
		}
		if box.boxQualifier == qualifierCheckbox {
			entry.Amount, entry.TextValue = "", ""
			if !entry.IsChecked {
				return refuseTaxDocument("%s is a checkbox; give only the ones that are checked", label)
			}
			continue
		}
		entry.IsChecked = false
		if amountValue == nil && entry.TextValue == "" {
			return refuseTaxDocument("%s prints nothing; leave out boxes that are empty", label)
		}
		if box.isText {
			continue
		}
		if amountValue == nil {
			return refuseTaxDocument("%s is an amount; give it as a decimal", label)
		}
	}
	sort.SliceStable(taxForm.TaxFormEntries, func(left, right int) bool {
		return taxForm.TaxFormEntries[left].EntryNumber < taxForm.TaxFormEntries[right].EntryNumber
	})
	return nil
}

func taxFormKindName(taxFormKind models.TaxFormKind) string {
	if taxFormKind == models.TaxFormKindW2 {
		return "W-2"
	}
	return string(taxFormKind)
}

// TaxFormFindingCode names what a finding checks.
type TaxFormFindingCode string

const (
	// TaxFormFindingSocialSecurityRate: box 4 is 6.2% of boxes 3 and 7.
	TaxFormFindingSocialSecurityRate TaxFormFindingCode = "social_security_rate"
	// TaxFormFindingSocialSecurityWageBase: boxes 3 and 7 are no more
	// than the year's Social Security wage base.
	TaxFormFindingSocialSecurityWageBase TaxFormFindingCode = "social_security_wage_base"
	// TaxFormFindingMedicareRate: box 6 is 1.45% of box 5 and 0.9% more
	// of what box 5 holds above 200,000.
	TaxFormFindingMedicareRate TaxFormFindingCode = "medicare_rate"
)

// TaxFormFinding is one thing a tax form says about itself: what was
// expected, what is printed, and whether they agree.
type TaxFormFinding struct {
	TaxFormFindingCode TaxFormFindingCode `json:"taxFormFindingCode"`
	IsAgreeing         bool               `json:"isAgreeing"`
	ExpectedAmount     string             `json:"expectedAmount"`
	PrintedAmount      string             `json:"printedAmount"`
	// FindingText says it in a sentence for the person.
	FindingText string `json:"findingText"`
}

var (
	socialSecurityRate            = big.NewRat(62, 1000)
	medicareRate                  = big.NewRat(145, 10000)
	additionalMedicareRate        = big.NewRat(9, 1000)
	additionalMedicareThreshold   = big.NewRat(200000, 1)
	taxFormRoundingToleranceValue = big.NewRat(1, 1)
)

// socialSecurityWageBaseByYear is the most wages Social Security tax
// applies to in a year, as the Social Security Administration publishes
// it each October for the year after.
var socialSecurityWageBaseByYear = map[int]int64{
	2021: 142800, 2022: 147000, 2023: 160200, 2024: 168600, 2025: 176100, 2026: 184500,
}

// SocialSecurityWageBase is the year's Social Security wage base, and
// whether TeaNode knows it.
func SocialSecurityWageBase(taxYear int) (*big.Rat, bool) {
	base, isKnown := socialSecurityWageBaseByYear[taxYear]
	if !isKnown {
		return nil, false
	}
	return big.NewRat(base, 1), true
}

// taxFormAmount is the sum of a form's boxes of one code (every
// qualifier when qualifiers is empty), and whether any is printed.
func taxFormAmount(taxForm *models.FinanceTaxForm, boxCode string, qualifiers ...string) (*big.Rat, bool) {
	total, isPrinted := new(big.Rat), false
	for _, entry := range taxForm.TaxFormEntries {
		if entry == nil || entry.BoxCode != boxCode || entry.Amount == "" {
			continue
		}
		if len(qualifiers) > 0 {
			isWanted := false
			for _, qualifier := range qualifiers {
				isWanted = isWanted || strings.EqualFold(entry.BoxQualifier, qualifier)
			}
			if !isWanted {
				continue
			}
		}
		if amountValue, err := parseDecimal(entry.Amount); err == nil {
			total.Add(total, amountValue)
			isPrinted = true
		}
	}
	return total, isPrinted
}

// roundToCents rounds half away from zero to two places.
func roundToCents(amountValue *big.Rat) *big.Rat {
	scaled := new(big.Rat).Mul(amountValue, big.NewRat(100, 1))
	numerator, denominator := scaled.Num(), scaled.Denom()
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if new(big.Int).Mul(new(big.Int).Abs(remainder), big.NewInt(2)).Cmp(denominator) >= 0 {
		if numerator.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	return new(big.Rat).SetFrac(quotient, big.NewInt(100))
}

func isWithinTolerance(leftAmount, rightAmount *big.Rat) bool {
	difference := new(big.Rat).Sub(leftAmount, rightAmount)
	return difference.Abs(difference).Cmp(taxFormRoundingToleranceValue) <= 0
}

func formatDollars(amountValue *big.Rat) string {
	return amountValue.FloatString(2)
}

// TaxFormFindings works out what a W-2 says about itself, within a dollar,
// since payroll rounds each paycheck's tax to the cent: Social Security
// tax against 6.2% of Social Security wages and tips, those wages against
// the year's wage base when TeaNode knows it, and Medicare tax against
// 1.45% of Medicare wages plus 0.9% of what they hold above 200,000. A
// form of another kind, or one that prints neither side of a check, has
// no finding for it.
func TaxFormFindings(taxForm *models.FinanceTaxForm) []TaxFormFinding {
	findings := []TaxFormFinding{}
	if taxForm == nil || taxForm.TaxFormKind != models.TaxFormKindW2 {
		return findings
	}
	socialSecurityWages, hasSocialSecurityWages := taxFormAmount(taxForm, "3")
	socialSecurityTips, _ := taxFormAmount(taxForm, "7")
	socialSecurityTax, hasSocialSecurityTax := taxFormAmount(taxForm, "4")
	if hasSocialSecurityWages || hasSocialSecurityTax {
		taxedWages := new(big.Rat).Add(socialSecurityWages, socialSecurityTips)
		expected := roundToCents(new(big.Rat).Mul(taxedWages, socialSecurityRate))
		isAgreeing := isWithinTolerance(expected, socialSecurityTax)
		findingText := fmt.Sprintf("Social Security tax %s is 6.2%% of %s in Social Security wages", formatDollars(socialSecurityTax), formatDollars(taxedWages))
		if !isAgreeing {
			findingText = fmt.Sprintf("Social Security tax %s is not 6.2%% of %s in Social Security wages, which is %s", formatDollars(socialSecurityTax), formatDollars(taxedWages), formatDollars(expected))
		}
		findings = append(findings, TaxFormFinding{
			TaxFormFindingCode: TaxFormFindingSocialSecurityRate, IsAgreeing: isAgreeing,
			ExpectedAmount: formatDollars(expected), PrintedAmount: formatDollars(socialSecurityTax), FindingText: findingText,
		})
		if wageBase, isKnown := SocialSecurityWageBase(taxForm.TaxYear); isKnown {
			isAgreeing := taxedWages.Cmp(wageBase) <= 0
			findingText := fmt.Sprintf("Social Security wages %s are within %d's wage base of %s", formatDollars(taxedWages), taxForm.TaxYear, formatDollars(wageBase))
			if !isAgreeing {
				findingText = fmt.Sprintf("Social Security wages %s are more than %d's wage base of %s", formatDollars(taxedWages), taxForm.TaxYear, formatDollars(wageBase))
			}
			findings = append(findings, TaxFormFinding{
				TaxFormFindingCode: TaxFormFindingSocialSecurityWageBase, IsAgreeing: isAgreeing,
				ExpectedAmount: formatDollars(wageBase), PrintedAmount: formatDollars(taxedWages), FindingText: findingText,
			})
		}
	}
	medicareWages, hasMedicareWages := taxFormAmount(taxForm, "5")
	medicareTax, hasMedicareTax := taxFormAmount(taxForm, "6")
	if hasMedicareWages || hasMedicareTax {
		expected := new(big.Rat).Mul(medicareWages, medicareRate)
		if aboveThreshold := new(big.Rat).Sub(medicareWages, additionalMedicareThreshold); aboveThreshold.Sign() > 0 {
			expected.Add(expected, new(big.Rat).Mul(aboveThreshold, additionalMedicareRate))
		}
		expected = roundToCents(expected)
		isAgreeing := isWithinTolerance(expected, medicareTax)
		findingText := fmt.Sprintf("Medicare tax %s agrees with %s in Medicare wages", formatDollars(medicareTax), formatDollars(medicareWages))
		if !isAgreeing {
			findingText = fmt.Sprintf("Medicare tax %s does not agree with %s in Medicare wages, which come to %s", formatDollars(medicareTax), formatDollars(medicareWages), formatDollars(expected))
		}
		findings = append(findings, TaxFormFinding{
			TaxFormFindingCode: TaxFormFindingMedicareRate, IsAgreeing: isAgreeing,
			ExpectedAmount: formatDollars(expected), PrintedAmount: formatDollars(medicareTax), FindingText: findingText,
		})
	}
	return findings
}
