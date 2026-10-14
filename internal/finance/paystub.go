package finance

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
	normalization "golang.org/x/text/unicode/norm"
)

// A paystub is stored as printed and checked against its own totals:
// earnings add up to gross pay, taxes and deductions to their printed
// totals, and gross pay less the taxes and the employee's deductions is
// take-home pay. Nothing is spread or recomputed when storing.

// ErrTaxDocumentRefused begins every refusal of a paystub or a tax form.
var ErrTaxDocumentRefused = errors.New("the tax document was not recorded")

func refuseTaxDocument(format string, arguments ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrTaxDocumentRefused}, arguments...)...)
}

// parseTaxDocumentAmount reads one of a document's amounts, refusing more
// places than its currency has, which is an amount misread.
func parseTaxDocumentAmount(what, text, currencyCode string) (*big.Rat, error) {
	text = strings.ReplaceAll(strings.TrimSpace(normalization.NFKC.String(text)), "−", "-")
	amountValue, err := parseDecimal(text)
	if err != nil {
		return nil, refuseTaxDocument("%s %q is not a decimal amount written without separators", what, text)
	}
	if _, fraction, hasPoint := strings.Cut(text, "."); hasPoint && len(strings.TrimRight(fraction, "0")) > CurrencyMinorUnits(currencyCode) {
		return nil, refuseTaxDocument("%s %s has more places than %s has", what, text, currencyCode)
	}
	return amountValue, nil
}

// optionalTaxDocumentAmount reads an amount that may be absent, writing it
// back canonical (four places) or empty, and refusing one below zero
// unless isNegativeAllowed.
func optionalTaxDocumentAmount(what string, text *string, currencyCode string, isNegativeAllowed bool) (*big.Rat, error) {
	if strings.TrimSpace(*text) == "" {
		*text = ""
		return nil, nil
	}
	amountValue, err := parseTaxDocumentAmount(what, *text, currencyCode)
	if err != nil {
		return nil, err
	}
	if !isNegativeAllowed && amountValue.Sign() < 0 {
		return nil, refuseTaxDocument("%s %s is below zero; give amounts as printed, without a sign", what, *text)
	}
	*text = FormatAmount(amountValue)
	return amountValue, nil
}

// cleanTaxDocumentText normalizes a text field and refuses one holding a
// taxpayer number.
func cleanTaxDocumentText(what string, text *string) error {
	*text = strings.Join(strings.Fields(normalization.NFKC.String(*text)), " ")
	if HasTaxpayerNumber(*text) {
		return refuseTaxDocument("%s holds a number shaped like a taxpayer number; leave it out", what)
	}
	return nil
}

// cleanTaxDocumentDay checks a day written 2006-01-02, which may be
// absent unless isRequired.
func cleanTaxDocumentDay(what string, text *string, isRequired bool) (time.Time, error) {
	*text = strings.TrimSpace(*text)
	if *text == "" {
		if isRequired {
			return time.Time{}, refuseTaxDocument("give %s, as 2006-01-02", what)
		}
		return time.Time{}, nil
	}
	day, err := time.Parse(time.DateOnly, *text)
	if err != nil {
		return time.Time{}, refuseTaxDocument("%s %q is not a day written 2006-01-02", what, *text)
	}
	return day, nil
}

func cleanCurrencyCode(text string) (string, error) {
	currencyCode := strings.ToUpper(strings.TrimSpace(text))
	if currencyCode == "" {
		currencyCode = "USD"
	}
	if len(currencyCode) != 3 {
		return "", refuseTaxDocument("%q is not a currency code like USD", text)
	}
	return currencyCode, nil
}

// CheckPaystub checks a paystub as read and normalizes it in place: text
// NFKC with its spaces collapsed, amounts canonical, the taxpayer number
// cut to its last four digits. It refuses a paystub with no pay date, no
// gross pay or no take-home pay, a day that is not one, a period that ends
// before it starts, a line of a kind it does not know, a tax line without
// its tax or a deduction line without what it pays for, a line with no
// amount, an amount below zero (other than take-home pay, which a paycheck
// that only repays an advance can bring below zero), an amount with more
// places than the currency has, and any text holding a taxpayer number.
// Then it answers whether the lines add up and by how much they miss:
// gross less taxes less deductions against the printed take-home pay
// first, then the earnings against gross pay, the taxes and the
// deductions against their printed totals, and the earnings' year to
// date against the year-to-date gross; the difference is what the lines
// come to less what is printed for the first that misses, "0" when
// balanced.
func CheckPaystub(paystub *models.FinancePaystub) (models.PaystubCheckState, string, error) {
	if paystub == nil {
		return "", "", refuseTaxDocument("there is no paystub")
	}
	currencyCode, err := cleanCurrencyCode(paystub.CurrencyCode)
	if err != nil {
		return "", "", err
	}
	paystub.CurrencyCode = currencyCode
	for _, field := range []struct {
		what string
		text *string
	}{{"the employer's name", &paystub.EmployerName}, {"the employee's name", &paystub.EmployeeName}} {
		if err := cleanTaxDocumentText(field.what, field.text); err != nil {
			return "", "", err
		}
	}
	paystub.EmployerTaxID = strings.TrimSpace(normalization.NFKC.String(paystub.EmployerTaxID))
	if paystub.EmployeeTaxIDMask, err = taxIDMask("the employee's taxpayer number", paystub.EmployeeTaxIDMask, refuseTaxDocument); err != nil {
		return "", "", err
	}
	if _, err := cleanTaxDocumentDay("the pay date", &paystub.PayDate, true); err != nil {
		return "", "", err
	}
	periodStart, err := cleanTaxDocumentDay("the first day of the pay period", &paystub.PeriodStartOn, false)
	if err != nil {
		return "", "", err
	}
	periodEnd, err := cleanTaxDocumentDay("the last day of the pay period", &paystub.PeriodEndOn, false)
	if err != nil {
		return "", "", err
	}
	if !periodStart.IsZero() && !periodEnd.IsZero() && periodEnd.Before(periodStart) {
		return "", "", refuseTaxDocument("the pay period ends (%s) before it starts (%s)", paystub.PeriodEndOn, paystub.PeriodStartOn)
	}
	if strings.TrimSpace(paystub.GrossAmount) == "" {
		return "", "", refuseTaxDocument("give the gross pay the paystub prints for this period")
	}
	if strings.TrimSpace(paystub.NetAmount) == "" {
		return "", "", refuseTaxDocument("give the take-home (net) pay the paystub prints for this period")
	}
	grossAmount, err := optionalTaxDocumentAmount("the gross pay", &paystub.GrossAmount, currencyCode, false)
	if err != nil {
		return "", "", err
	}
	netAmount, err := optionalTaxDocumentAmount("the take-home pay", &paystub.NetAmount, currencyCode, true)
	if err != nil {
		return "", "", err
	}
	yearToDateGrossAmount, err := optionalTaxDocumentAmount("the year-to-date gross pay", &paystub.YearToDateGrossAmount, currencyCode, false)
	if err != nil {
		return "", "", err
	}
	totalTaxAmount, err := optionalTaxDocumentAmount("the total of the taxes", &paystub.TotalTaxAmount, currencyCode, false)
	if err != nil {
		return "", "", err
	}
	totalDeductionAmount, err := optionalTaxDocumentAmount("the total of the deductions", &paystub.TotalDeductionAmount, currencyCode, false)
	if err != nil {
		return "", "", err
	}
	if len(paystub.PaystubLines) == 0 {
		return "", "", refuseTaxDocument("give the paystub's lines with an amount: its earnings, taxes and deductions")
	}

	isNumbered := false
	for _, line := range paystub.PaystubLines {
		isNumbered = isNumbered || line.LineNumber != 0
	}
	seenLineNumbers := map[int]bool{}
	earningsAmount, earningsYearToDateAmount, taxesAmount, deductionsAmount := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	hasEarningAmount, hasEarningYearToDate := false, false
	for index, line := range paystub.PaystubLines {
		if line == nil {
			return "", "", refuseTaxDocument("line %d is empty", index+1)
		}
		if !isNumbered {
			line.LineNumber = index + 1
		}
		if line.LineNumber <= 0 || seenLineNumbers[line.LineNumber] {
			return "", "", refuseTaxDocument("line numbers start at 1 and are each used once; %d is not", line.LineNumber)
		}
		seenLineNumbers[line.LineNumber] = true
		label := fmt.Sprintf("line %d", line.LineNumber)
		if err := cleanTaxDocumentText(label+"'s description", &line.Description); err != nil {
			return "", "", err
		}
		if line.Description != "" {
			label += fmt.Sprintf(" (%s)", line.Description)
		}
		if !line.PaystubLineKind.IsValid() {
			return "", "", refuseTaxDocument("%s is of kind %q; a line is an earning, a tax or a deduction", label, line.PaystubLineKind)
		}
		line.TaxJurisdictionCode = strings.TrimSpace(normalization.NFKC.String(line.TaxJurisdictionCode))
		switch line.PaystubLineKind {
		case models.PaystubLineKindTax:
			if !line.PaystubTaxKind.IsValid() {
				return "", "", refuseTaxDocument("%s is a tax of kind %q; say which tax: federal_income, social_security, medicare, medicare_additional, state_income, local_income, state_disability or other", label, line.PaystubTaxKind)
			}
			if line.PaystubTaxKind == models.PaystubTaxKindStateIncome || line.PaystubTaxKind == models.PaystubTaxKindStateDisability {
				line.TaxJurisdictionCode = strings.ToUpper(line.TaxJurisdictionCode)
			}
			line.PaystubDeductionKind = ""
		case models.PaystubLineKindDeduction:
			if !line.PaystubDeductionKind.IsValid() {
				return "", "", refuseTaxDocument("%s is a deduction of kind %q; say what it pays for: retirement_traditional, retirement_roth, hsa, health_premium, fsa_health, fsa_dependent_care, commuter, insurance or other", label, line.PaystubDeductionKind)
			}
			line.PaystubTaxKind, line.TaxJurisdictionCode = "", ""
		default:
			line.PaystubTaxKind, line.PaystubDeductionKind, line.TaxJurisdictionCode = "", "", ""
		}
		if line.PaystubLineKind != models.PaystubLineKindTax {
			line.CurrentWagesAmount, line.YearToDateWagesAmount = "", ""
		}
		if line.PaystubLineKind != models.PaystubLineKindDeduction {
			line.EmployerCurrentAmount, line.EmployerYearToDateAmount = "", ""
		}
		if line.PaystubLineKind != models.PaystubLineKindEarning {
			line.Hours, line.RateAmount = "", ""
		}
		currentAmount, err := optionalTaxDocumentAmount(label+"'s amount this period", &line.CurrentAmount, currencyCode, line.PaystubLineKind == models.PaystubLineKindEarning)
		if err != nil {
			return "", "", err
		}
		yearToDateAmount, err := optionalTaxDocumentAmount(label+"'s amount for the year to date", &line.YearToDateAmount, currencyCode, line.PaystubLineKind == models.PaystubLineKindEarning)
		if err != nil {
			return "", "", err
		}
		amounts := []struct {
			what string
			text *string
		}{
			{"'s wages this period", &line.CurrentWagesAmount}, {"'s wages for the year to date", &line.YearToDateWagesAmount},
			{"'s employer part this period", &line.EmployerCurrentAmount}, {"'s employer part for the year to date", &line.EmployerYearToDateAmount},
			{"'s rate", &line.RateAmount},
		}
		hasOtherAmount := false
		for _, amount := range amounts {
			otherAmount, err := optionalTaxDocumentAmount(label+amount.what, amount.text, currencyCode, false)
			if err != nil {
				return "", "", err
			}
			hasOtherAmount = hasOtherAmount || otherAmount != nil
		}
		if line.Hours = strings.TrimSpace(line.Hours); line.Hours != "" {
			if line.Hours, err = CanonicalQuantity(line.Hours); err != nil {
				return "", "", refuseTaxDocument("%s's hours %q are not a decimal", label, line.Hours)
			}
		}
		if currentAmount == nil && yearToDateAmount == nil && !hasOtherAmount {
			return "", "", refuseTaxDocument("%s has no amount; leave out lines of hours only and lines that print nothing", label)
		}
		switch line.PaystubLineKind {
		case models.PaystubLineKindEarning:
			if currentAmount != nil {
				earningsAmount.Add(earningsAmount, currentAmount)
				hasEarningAmount = true
			}
			if yearToDateAmount != nil {
				earningsYearToDateAmount.Add(earningsYearToDateAmount, yearToDateAmount)
				hasEarningYearToDate = true
			}
		case models.PaystubLineKindTax:
			if currentAmount != nil {
				taxesAmount.Add(taxesAmount, currentAmount)
			}
		case models.PaystubLineKindDeduction:
			if currentAmount != nil {
				deductionsAmount.Add(deductionsAmount, currentAmount)
			}
		}
	}

	// Take-home pay first: it is the figure that reaches the bank.
	computedNetAmount := new(big.Rat).Sub(grossAmount, taxesAmount)
	computedNetAmount.Sub(computedNetAmount, deductionsAmount)
	differences := []struct{ computed, printed *big.Rat }{{computedNetAmount, netAmount}}
	if hasEarningAmount {
		differences = append(differences, struct{ computed, printed *big.Rat }{earningsAmount, grossAmount})
	}
	if totalTaxAmount != nil {
		differences = append(differences, struct{ computed, printed *big.Rat }{taxesAmount, totalTaxAmount})
	}
	if totalDeductionAmount != nil {
		differences = append(differences, struct{ computed, printed *big.Rat }{deductionsAmount, totalDeductionAmount})
	}
	if hasEarningYearToDate && yearToDateGrossAmount != nil {
		differences = append(differences, struct{ computed, printed *big.Rat }{earningsYearToDateAmount, yearToDateGrossAmount})
	}
	for _, difference := range differences {
		if missAmount := new(big.Rat).Sub(difference.computed, difference.printed); missAmount.Sign() != 0 {
			return models.PaystubCheckStateUnbalanced, FormatAmount(missAmount), nil
		}
	}
	return models.PaystubCheckStateBalanced, "0", nil
}
