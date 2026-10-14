package finance

import (
	"regexp"
	"strings"
)

// A person's taxpayer number (a Social Security number) is nine digits,
// printed as 123-45-6789, 123 45 6789 or 123456789. Paystubs and tax forms
// print it, and nothing TeaNode does needs more than its last four digits,
// so it is masked before a model reads the text and refused in anything
// written.
var (
	dashedTaxpayerNumberPattern = regexp.MustCompile(`\b[0-9]{3}[- ][0-9]{2}[- ]([0-9]{4})\b`)
	digitRunPattern             = regexp.MustCompile(`[0-9]+`)
)

// MaskTaxpayerNumbers hides all but the last four digits of every number
// in the text shaped like a taxpayer number: written with dashes or
// spaces (XXX-XX-6789), or nine digits in a row standing alone
// (XXXXX6789). An employer's identification number written with its dash
// (12-3456789) is a business's and is left alone; written without it, it
// cannot be told from a person's and is masked too.
func MaskTaxpayerNumbers(text string) string {
	text = dashedTaxpayerNumberPattern.ReplaceAllString(text, "XXX-XX-$1")
	return replaceStandingNineDigitRuns(text, func(run string) string { return "XXXXX" + run[5:] })
}

// taxDocumentMarkers are words a text holding a person's taxpayer number
// carries: a W-2, a paystub, a tax form.
var taxDocumentMarkers = []string{"social security", "soc. sec", "ssn", "w-2", "wage and tax statement", "taxpayer", "employee's soc"}

// MaskTaxpayerNumbersInFile is what any file's text keeps of the
// taxpayer numbers in it, whatever it was handed in as: one written with
// dashes or spaces is masked always, since nothing else is written that
// way, and nine digits standing alone are masked too when the text reads
// like a tax document, where they are a person's number; in any other
// text they may be an order or a tracking number and are kept.
func MaskTaxpayerNumbersInFile(text string) string {
	lowered := strings.ToLower(text)
	for _, marker := range taxDocumentMarkers {
		if strings.Contains(lowered, marker) {
			return MaskTaxpayerNumbers(text)
		}
	}
	return dashedTaxpayerNumberPattern.ReplaceAllString(text, "XXX-XX-$1")
}

// HasDashedTaxpayerNumber says the text holds a number written as a
// taxpayer number is, with dashes or spaces (123-45-6789), which nothing
// else is written as, such as a business's number in a business's field.
func HasDashedTaxpayerNumber(text string) bool {
	return dashedTaxpayerNumberPattern.MatchString(text)
}

// HasTaxpayerNumber says the text holds a number shaped like a taxpayer
// number, as MaskTaxpayerNumbers finds them.
func HasTaxpayerNumber(text string) bool {
	if dashedTaxpayerNumberPattern.MatchString(text) {
		return true
	}
	hasRun := false
	replaceStandingNineDigitRuns(text, func(run string) string {
		hasRun = true
		return run
	})
	return hasRun
}

// replaceStandingNineDigitRuns replaces each run of exactly nine digits
// that is not part of a decimal (no point right before or after it).
func replaceStandingNineDigitRuns(text string, replace func(string) string) string {
	var builder strings.Builder
	last := 0
	for _, bounds := range digitRunPattern.FindAllStringIndex(text, -1) {
		start, end := bounds[0], bounds[1]
		if end-start != 9 || (start > 0 && text[start-1] == '.') || (end < len(text) && text[end] == '.' && end+1 < len(text) && isDigitByte(text[end+1])) {
			continue
		}
		builder.WriteString(text[last:start])
		builder.WriteString(replace(text[start:end]))
		last = end
	}
	builder.WriteString(text[last:])
	return builder.String()
}

func isDigitByte(character byte) bool {
	return character >= '0' && character <= '9'
}

// taxIDMask reads the last four digits of a taxpayer number however they
// were written (4321, XXX-XX-4321, ***4321), refusing more than four
// digits, which would be more of the number than is ever kept.
func taxIDMask(what, text string, refuse func(string, ...any) error) (string, error) {
	digits := strings.Map(func(character rune) rune {
		if character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, text)
	switch {
	case digits == "":
		return "", nil
	case len(digits) == 4:
		return digits, nil
	case len(digits) > 4:
		return "", refuse("%s holds more than the last four digits of a taxpayer number; give only the last four", what)
	}
	return "", refuse("%s %q is not the last four digits of a taxpayer number", what, text)
}
