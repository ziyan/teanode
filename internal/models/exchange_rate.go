package models

import "time"

// RateSource is who published an exchange rate.
type RateSource string

// RateSourceECB is the European Central Bank's daily euro reference rates,
// the one source today.
const RateSourceECB RateSource = "ecb"

// IsValid says the source is a known one.
func (self RateSource) IsValid() bool {
	return self == RateSourceECB
}

// IsCurrencyCode says a word is shaped like an ISO 4217 code: three
// capital letters. Whether anyone publishes a rate for it is another
// question, answered by the rates stored.
func IsCurrencyCode(currencyCode string) bool {
	if len(currencyCode) != 3 {
		return false
	}
	for _, letter := range currencyCode {
		if letter < 'A' || letter > 'Z' {
			return false
		}
	}
	return true
}

// ExchangeRate is what one euro bought in one currency on one day, as a
// rate source published it. Rates between two other currencies are the
// ratio of their euro rates.
type ExchangeRate struct {
	// RateOn is the day it was published for, "2006-01-02".
	RateOn       string `json:"rateOn"`
	CurrencyCode string `json:"currencyCode"`

	// EuroRate is a decimal: how many of CurrencyCode one euro bought.
	EuroRate   string     `json:"euroRate"`
	RateSource RateSource `json:"rateSource"`

	CreatedAt time.Time `json:"createdAt"`
}

// CurrencyPairRate is what one unit of one currency bought in another on
// one day, and which published day that came from: the day asked, or the
// latest earlier one when nothing was published that day.
type CurrencyPairRate struct {
	FromCurrencyCode string `json:"fromCurrencyCode"`
	ToCurrencyCode   string `json:"toCurrencyCode"`

	// Rate is a decimal: how many of ToCurrencyCode one FromCurrencyCode
	// bought.
	Rate string `json:"rate"`

	// RateOn is the published day the rate is from, "2006-01-02".
	RateOn     string     `json:"rateOn"`
	RateSource RateSource `json:"rateSource"`
}
