package finance

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// EuroCurrencyCode is the currency the ECB's rates are quoted against. It
// has no row of its own: its rate is one, always.
const EuroCurrencyCode = "EUR"

// ExchangeRateSpan is which of the ECB's files to fetch.
type ExchangeRateSpan string

const (
	// ExchangeRateSpanLatest is the latest published day.
	ExchangeRateSpanLatest ExchangeRateSpan = "latest"
	// ExchangeRateSpanNinetyDays is the last ninety days.
	ExchangeRateSpanNinetyDays ExchangeRateSpan = "ninetyDays"
	// ExchangeRateSpanHistory is every day since the euro began in 1999.
	ExchangeRateSpanHistory ExchangeRateSpan = "history"
)

const (
	exchangeRateBaseUrl = "https://www.ecb.europa.eu/stats/eurofxref"

	// The full history is a few megabytes of XML and grows by a day's
	// rates every business day.
	exchangeRateResponseByteLimit = 64 << 20

	exchangeRateTimeout = 2 * time.Minute
)

// The file for each span. The ECB publishes each as CSV inside a zip and as
// XML; XML needs no zip handling.
var exchangeRateFileBySpan = map[ExchangeRateSpan]string{
	ExchangeRateSpanLatest:     "eurofxref-daily.xml",
	ExchangeRateSpanNinetyDays: "eurofxref-hist-90d.xml",
	ExchangeRateSpanHistory:    "eurofxref-hist.xml",
}

// EuroRate is what one euro bought in one currency on one day.
type EuroRate struct {
	RateOn       string // "2006-01-02"
	CurrencyCode string
	EuroRate     string // decimal
}

// ExchangeRateSource fetches the European Central Bank's euro reference
// rates: about thirty currencies, every business day, public, with no key
// and a history back to 1999, which is what a self-hosted server can use
// without an account anywhere.
//
// The hosts are fixed, so this uses an ordinary client rather than
// safefetch.
type ExchangeRateSource struct {
	baseUrl string
	http    *http.Client
}

// NewExchangeRateSource builds a source. It opens no connection.
func NewExchangeRateSource() *ExchangeRateSource {
	return &ExchangeRateSource{
		baseUrl: exchangeRateBaseUrl,
		http:    &http.Client{Timeout: exchangeRateTimeout},
	}
}

// euroRateEnvelope is the shape all three files share: a Cube holding a
// Cube per day, each holding a Cube per currency. Names are matched
// without their namespaces, which the ECB has changed before.
type euroRateEnvelope struct {
	Days []struct {
		RateOn string `xml:"time,attr"`
		Rates  []struct {
			CurrencyCode string `xml:"currency,attr"`
			EuroRate     string `xml:"rate,attr"`
		} `xml:"Cube"`
	} `xml:"Cube>Cube"`
}

// FetchEuroRates fetches one of the ECB's files and returns every rate in
// it. Days with no rates (weekends, TARGET holidays) are simply absent;
// finding the latest earlier day is the caller's job, since it has the
// stored history to look in.
func (self *ExchangeRateSource) FetchEuroRates(ctx context.Context, span ExchangeRateSpan) ([]EuroRate, error) {
	fileName, isKnown := exchangeRateFileBySpan[span]
	if !isKnown {
		return nil, fmt.Errorf("finance: no exchange rate file for %q", span)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(self.baseUrl, "/")+"/"+fileName, nil)
	if err != nil {
		return nil, fmt.Errorf("finance: %w", err)
	}
	request.Header.Set("User-Agent", UserAgent())

	response, err := self.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("finance: cannot reach the ECB: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("finance: the ECB answered %s", response.Status)
	}
	answer, err := io.ReadAll(io.LimitReader(response.Body, exchangeRateResponseByteLimit+1))
	if err != nil {
		return nil, fmt.Errorf("finance: cannot read the ECB's answer: %w", err)
	}
	if len(answer) > exchangeRateResponseByteLimit {
		return nil, errors.New("finance: the ECB's answer was too large")
	}
	return parseEuroRates(answer)
}

// parseEuroRates reads one of the ECB's files. A row that does not read is
// an error rather than a row skipped: the file is machine written, so a bad
// row means the format changed, and a rate misread is a conversion wrong
// without anybody noticing.
func parseEuroRates(document []byte) ([]EuroRate, error) {
	var envelope euroRateEnvelope
	if err := xml.Unmarshal(document, &envelope); err != nil {
		return nil, fmt.Errorf("finance: the ECB's rates were not readable: %w", err)
	}
	var rates []EuroRate
	for _, day := range envelope.Days {
		if _, err := time.Parse(time.DateOnly, day.RateOn); err != nil {
			return nil, fmt.Errorf("finance: the ECB's rates have a day %q that is not a date", day.RateOn)
		}
		for _, rate := range day.Rates {
			currencyCode := strings.TrimSpace(rate.CurrencyCode)
			if !isCurrencyCode(currencyCode) {
				return nil, fmt.Errorf("finance: the ECB's rates on %s name a currency %q", day.RateOn, currencyCode)
			}
			value, err := parseDecimal(rate.EuroRate)
			if err != nil || value.Sign() <= 0 {
				return nil, fmt.Errorf("finance: the ECB's rate for %s on %s is not a positive number", currencyCode, day.RateOn)
			}
			rates = append(rates, EuroRate{
				RateOn:       day.RateOn,
				CurrencyCode: currencyCode,
				EuroRate:     strings.TrimSpace(rate.EuroRate),
			})
		}
	}
	if len(rates) == 0 {
		return nil, errors.New("finance: the ECB's answer held no rates")
	}
	return rates, nil
}

func isCurrencyCode(text string) bool {
	if len(text) != 3 {
		return false
	}
	for _, character := range text {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

// ErrNoExchangeRate is a currency with no rate: the ECB does not publish
// it (most currencies, and every cryptocurrency), or not for the day
// asked. An amount in it is reported unconverted rather than guessed.
type ErrNoExchangeRate struct {
	CurrencyCode string
}

func (self *ErrNoExchangeRate) Error() string {
	return "finance: no exchange rate for " + self.CurrencyCode
}

// CrossRate is what one unit of the from currency buys in the to currency,
// given what one euro buys in each: the ratio of the two, written with
// RateDecimalPlaces places, halves rounded away from zero.
func CrossRate(fromEuroRate, toEuroRate string) (string, error) {
	fromValue, err := parseDecimal(fromEuroRate)
	if err != nil {
		return "", err
	}
	toValue, err := parseDecimal(toEuroRate)
	if err != nil {
		return "", err
	}
	if fromValue.Sign() <= 0 || toValue.Sign() <= 0 {
		return "", errors.New("finance: an exchange rate must be positive")
	}
	return formatDecimal(new(big.Rat).Quo(toValue, fromValue), RateDecimalPlaces), nil
}

// CrossRateFromEuroRates is CrossRate between two currencies by code, from
// one day's euro rates by currency code. The euro needs no entry.
func CrossRateFromEuroRates(fromCurrencyCode, toCurrencyCode string, euroRateByCurrencyCode map[string]string) (string, error) {
	fromEuroRate, err := euroRateOf(fromCurrencyCode, euroRateByCurrencyCode)
	if err != nil {
		return "", err
	}
	toEuroRate, err := euroRateOf(toCurrencyCode, euroRateByCurrencyCode)
	if err != nil {
		return "", err
	}
	return CrossRate(fromEuroRate, toEuroRate)
}

func euroRateOf(currencyCode string, euroRateByCurrencyCode map[string]string) (string, error) {
	if currencyCode == EuroCurrencyCode {
		return "1", nil
	}
	euroRate, hasRate := euroRateByCurrencyCode[currencyCode]
	if !hasRate || euroRate == "" {
		return "", &ErrNoExchangeRate{CurrencyCode: currencyCode}
	}
	return euroRate, nil
}

// ConvertAmount multiplies an amount by a rate, written with
// AmountDecimalPlaces places, halves rounded away from zero. The product is
// exact before that one rounding, so converting never compounds error.
func ConvertAmount(amount, rate string) (string, error) {
	amountValue, err := parseDecimal(amount)
	if err != nil {
		return "", err
	}
	rateValue, err := parseDecimal(rate)
	if err != nil {
		return "", err
	}
	if rateValue.Sign() <= 0 {
		return "", errors.New("finance: an exchange rate must be positive")
	}
	return formatDecimal(new(big.Rat).Mul(amountValue, rateValue), AmountDecimalPlaces), nil
}
