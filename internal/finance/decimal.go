package finance

import (
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// AmountDecimalPlaces is how many places an amount this package writes has.
// The tables hold amounts as numeric(19,4), so four places is every digit
// they keep and no more; writing the same number the same way every time
// also means two syncs of an unchanged transaction compare equal as text.
const AmountDecimalPlaces = 4

// QuantityDecimalPlaces is how many places a quantity or a unit price this
// package writes has, matching the numeric(24,8) the tables hold them as:
// fractional shares and coins go to eight places.
const QuantityDecimalPlaces = 8

// RateDecimalPlaces is how many places an exchange rate this package writes
// has, matching the numeric(20,10) the exchange rate table holds. The ECB
// publishes at most six significant digits, so ten places loses nothing
// from a rate and keeps a cross rate between two small currencies exact
// enough that converting a large amount does not drift by a cent.
const RateDecimalPlaces = 10

// parseDecimal reads a decimal written the way a person or a provider's
// string field writes one: an optional sign, digits, and an optional
// fraction. No exponent and no thousands separators, because a provider
// that sends those has changed its format and a silent misreading of an
// amount is worse than a failed sync.
func parseDecimal(text string) (*big.Rat, error) {
	text = strings.TrimSpace(text)
	digits := strings.TrimLeft(text, "+-")
	if len(text)-len(digits) > 1 {
		return nil, errors.New("finance: not a decimal: more than one sign")
	}
	whole, fraction, hasPoint := strings.Cut(digits, ".")
	if whole == "" || !isAllDigits(whole) || (hasPoint && (fraction == "" || !isAllDigits(fraction))) {
		return nil, errors.New("finance: not a decimal")
	}
	// Longer than any amount or rate anybody has, and short enough that a
	// hostile provider cannot make this allocate.
	if len(digits) > 64 {
		return nil, errors.New("finance: not a decimal: too long")
	}
	decimalValue, isParsed := new(big.Rat).SetString(text)
	if !isParsed {
		return nil, errors.New("finance: not a decimal")
	}
	return decimalValue, nil
}

// parseJsonNumber reads a number out of a JSON document exactly, which a
// float64 cannot do: 0.1 has no binary representation, and an amount that
// went through one comes out as 0.1000000000000000055511151231257827.
//
// JSON allows an exponent, and a provider whose encoder writes small values
// as 1e-05 is within its rights, so one is accepted here where
// parseDecimal refuses it; but only a small one, because big.Rat will
// happily try to build 1e999999999.
func parseJsonNumber(number json.Number) (*big.Rat, error) {
	text := strings.TrimSpace(number.String())
	if text == "" {
		return nil, errors.New("finance: no number")
	}
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(text), "e")
	if hasExponent {
		exponentValue, err := strconv.Atoi(exponent)
		if err != nil || exponentValue > 20 || exponentValue < -20 {
			return nil, errors.New("finance: not a usable number")
		}
	}
	if _, err := parseDecimal(mantissa); err != nil {
		return nil, err
	}
	decimalValue, isParsed := new(big.Rat).SetString(text)
	if !isParsed {
		return nil, errors.New("finance: not a number")
	}
	return decimalValue, nil
}

// formatDecimal writes a value with a fixed number of places. big.Rat
// rounds the last digit to nearest with halves away from zero, which is
// the rounding people do by hand and the one a bank statement shows.
func formatDecimal(decimalValue *big.Rat, places int) string {
	return decimalValue.FloatString(places)
}

// ParseAmount reads an amount or a rate as parseDecimal does, for a caller
// that does arithmetic on amounts (the budget pace) and must not do it in
// floating point.
func ParseAmount(text string) (*big.Rat, error) {
	return parseDecimal(text)
}

// FormatAmount writes a value as an amount, with AmountDecimalPlaces
// places.
func FormatAmount(amountValue *big.Rat) string {
	return formatDecimal(amountValue, AmountDecimalPlaces)
}

// CanonicalAmount writes an amount with exactly AmountDecimalPlaces places,
// so "12.5" becomes "12.5000".
func CanonicalAmount(amount string) (string, error) {
	amountValue, err := parseDecimal(amount)
	if err != nil {
		return "", err
	}
	return formatDecimal(amountValue, AmountDecimalPlaces), nil
}

// negatedJsonAmount turns one of Plaid's amounts, where positive is money
// leaving the account, into this program's, where negative is.
func negatedJsonAmount(number json.Number) (string, error) {
	amountValue, err := parseJsonNumber(number)
	if err != nil {
		return "", err
	}
	return formatDecimal(amountValue.Neg(amountValue), AmountDecimalPlaces), nil
}

// canonicalJsonAmount is a JSON number written as an amount, unchanged in
// sign, or empty when the provider sent none.
func canonicalJsonAmount(number json.Number) (string, error) {
	if number == "" {
		return "", nil
	}
	amountValue, err := parseJsonNumber(number)
	if err != nil {
		return "", err
	}
	return formatDecimal(amountValue, AmountDecimalPlaces), nil
}

// canonicalJsonQuantity is a JSON number written as a quantity or a unit
// price, or empty when the provider sent none.
func canonicalJsonQuantity(number json.Number) (string, error) {
	if number == "" {
		return "", nil
	}
	quantityValue, err := parseJsonNumber(number)
	if err != nil {
		return "", err
	}
	return formatDecimal(quantityValue, QuantityDecimalPlaces), nil
}

func isAllDigits(text string) bool {
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
