package finance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The three files share one shape. These keep the ECB's element names and
// prefixes but use placeholder namespaces, which the reader ignores.
const euroRatesDailyXml = `<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="urn:example:gesmes" xmlns="urn:example:eurofxref">
	<gesmes:subject>Reference rates</gesmes:subject>
	<Cube>
		<Cube time="2026-09-18">
			<Cube currency="USD" rate="1.1000"/>
			<Cube currency="JPY" rate="160.50"/>
			<Cube currency="GBP" rate="0.84000"/>
		</Cube>
	</Cube>
</gesmes:Envelope>`

const euroRatesNinetyDaysXml = `<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="urn:example:gesmes" xmlns="urn:example:eurofxref">
	<Cube>
		<Cube time="2026-09-18"><Cube currency="USD" rate="1.1000"/><Cube currency="JPY" rate="160.50"/></Cube>
		<Cube time="2026-09-17"><Cube currency="USD" rate="1.0950"/><Cube currency="JPY" rate="159.80"/></Cube>
		<Cube time="2026-09-16"><Cube currency="USD" rate="1.0900"/></Cube>
	</Cube>
</gesmes:Envelope>`

// The history file, written without any namespace at all.
const euroRatesHistoryXml = `<Envelope>
	<Cube>
		<Cube time="2026-09-18"><Cube currency="USD" rate="1.1"/></Cube>
		<Cube time="1999-01-04"><Cube currency="USD" rate="1.1789"/><Cube currency="CYP" rate="0.58231"/></Cube>
	</Cube>
</Envelope>`

func newExchangeRateTestServer(t *testing.T) (*ExchangeRateSource, *[]string) {
	t.Helper()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		if !strings.HasPrefix(request.UserAgent(), "teanode/") {
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		switch request.URL.Path {
		case "/stats/eurofxref/eurofxref-daily.xml":
			_, _ = writer.Write([]byte(euroRatesDailyXml))
		case "/stats/eurofxref/eurofxref-hist-90d.xml":
			_, _ = writer.Write([]byte(euroRatesNinetyDaysXml))
		case "/stats/eurofxref/eurofxref-hist.xml":
			_, _ = writer.Write([]byte(euroRatesHistoryXml))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	source := NewExchangeRateSource()
	source.baseUrl = server.URL + "/stats/eurofxref"
	return source, &paths
}

func TestFetchEuroRatesReadsEveryFile(t *testing.T) {
	source, paths := newExchangeRateTestServer(t)

	for _, testCase := range []struct {
		span      ExchangeRateSpan
		wantCount int
		wantFirst EuroRate
		wantLast  EuroRate
	}{
		{ExchangeRateSpanLatest, 3, EuroRate{"2026-09-18", "USD", "1.1000"}, EuroRate{"2026-09-18", "GBP", "0.84000"}},
		{ExchangeRateSpanNinetyDays, 5, EuroRate{"2026-09-18", "USD", "1.1000"}, EuroRate{"2026-09-16", "USD", "1.0900"}},
		{ExchangeRateSpanHistory, 3, EuroRate{"2026-09-18", "USD", "1.1"}, EuroRate{"1999-01-04", "CYP", "0.58231"}},
	} {
		rates, err := source.FetchEuroRates(context.Background(), testCase.span)
		if err != nil {
			t.Fatalf("%s: %v", testCase.span, err)
		}
		if len(rates) != testCase.wantCount {
			t.Fatalf("%s: %d rates, want %d: %v", testCase.span, len(rates), testCase.wantCount, rates)
		}
		if rates[0] != testCase.wantFirst || rates[len(rates)-1] != testCase.wantLast {
			t.Errorf("%s: first %v last %v", testCase.span, rates[0], rates[len(rates)-1])
		}
		for _, rate := range rates {
			if rate.CurrencyCode == EuroCurrencyCode {
				t.Errorf("%s: the euro has a row of its own", testCase.span)
			}
		}
	}
	if len(*paths) != 3 {
		t.Errorf("paths %v", *paths)
	}

	if _, err := source.FetchEuroRates(context.Background(), ExchangeRateSpan("weekly")); err == nil {
		t.Error("an unknown span was fetched")
	}
}

func TestParseEuroRatesRefusesMalformedFiles(t *testing.T) {
	for name, document := range map[string]string{
		"not xml":        `{"rates":[]}`,
		"no rates":       `<Envelope><Cube></Cube></Envelope>`,
		"bad day":        `<Envelope><Cube><Cube time="18.09.2026"><Cube currency="USD" rate="1.1"/></Cube></Cube></Envelope>`,
		"bad currency":   `<Envelope><Cube><Cube time="2026-09-18"><Cube currency="usd" rate="1.1"/></Cube></Cube></Envelope>`,
		"bad rate":       `<Envelope><Cube><Cube time="2026-09-18"><Cube currency="USD" rate="1,1"/></Cube></Cube></Envelope>`,
		"zero rate":      `<Envelope><Cube><Cube time="2026-09-18"><Cube currency="USD" rate="0"/></Cube></Cube></Envelope>`,
		"exponent rate":  `<Envelope><Cube><Cube time="2026-09-18"><Cube currency="USD" rate="1e3"/></Cube></Cube></Envelope>`,
		"missing rate":   `<Envelope><Cube><Cube time="2026-09-18"><Cube currency="USD"/></Cube></Cube></Envelope>`,
		"negative rate":  `<Envelope><Cube><Cube time="2026-09-18"><Cube currency="USD" rate="-1.1"/></Cube></Cube></Envelope>`,
		"empty document": ``,
	} {
		if rates, err := parseEuroRates([]byte(document)); err == nil {
			t.Errorf("%s: accepted as %v", name, rates)
		}
	}
}

func TestCrossRate(t *testing.T) {
	for _, testCase := range []struct {
		fromEuroRate string
		toEuroRate   string
		want         string
	}{
		// One dollar in yen: 160.50 / 1.1000.
		{"1.1000", "160.50", "145.9090909091"},
		// One yen in dollars.
		{"160.50", "1.1000", "0.0068535826"},
		// From the euro itself.
		{"1", "1.1000", "1.1000000000"},
		{"1.1", "1.1", "1.0000000000"},
	} {
		got, err := CrossRate(testCase.fromEuroRate, testCase.toEuroRate)
		if err != nil {
			t.Errorf("%s to %s: %v", testCase.fromEuroRate, testCase.toEuroRate, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("%s to %s is %s, want %s", testCase.fromEuroRate, testCase.toEuroRate, got, testCase.want)
		}
	}
	for _, refused := range [][2]string{{"0", "1.1"}, {"1.1", "-2"}, {"abc", "1"}} {
		if _, err := CrossRate(refused[0], refused[1]); err == nil {
			t.Errorf("%v was accepted", refused)
		}
	}
}

func TestCrossRateFromEuroRates(t *testing.T) {
	euroRateByCurrencyCode := map[string]string{"USD": "1.1000", "JPY": "160.50"}

	got, err := CrossRateFromEuroRates("USD", "JPY", euroRateByCurrencyCode)
	if err != nil || got != "145.9090909091" {
		t.Errorf("USD to JPY %s %v", got, err)
	}
	got, err = CrossRateFromEuroRates("EUR", "USD", euroRateByCurrencyCode)
	if err != nil || got != "1.1000000000" {
		t.Errorf("EUR to USD %s %v", got, err)
	}
	got, err = CrossRateFromEuroRates("USD", "EUR", euroRateByCurrencyCode)
	if err != nil || got != "0.9090909091" {
		t.Errorf("USD to EUR %s %v", got, err)
	}

	_, err = CrossRateFromEuroRates("USD", "XBT", euroRateByCurrencyCode)
	var noExchangeRate *ErrNoExchangeRate
	if !errors.As(err, &noExchangeRate) || noExchangeRate.CurrencyCode != "XBT" {
		t.Errorf("error %v, want ErrNoExchangeRate naming XBT", err)
	}
	if !strings.Contains(err.Error(), "XBT") {
		t.Errorf("error %q does not name the currency", err)
	}
}

func TestConvertAmount(t *testing.T) {
	for _, testCase := range []struct {
		amount string
		rate   string
		want   string
	}{
		{"100", "145.9090909091", "14590.9091"},
		{"-42.17", "1.1", "-46.3870"},
		// Halves round away from zero, in both directions.
		{"0.00005", "1", "0.0001"},
		{"-0.00005", "1", "-0.0001"},
		{"0.00015", "1", "0.0002"},
		{"0.000049", "1", "0.0000"},
		{"0", "1.5", "0.0000"},
	} {
		got, err := ConvertAmount(testCase.amount, testCase.rate)
		if err != nil {
			t.Errorf("%s at %s: %v", testCase.amount, testCase.rate, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("%s at %s is %s, want %s", testCase.amount, testCase.rate, got, testCase.want)
		}
	}
	for _, refused := range [][2]string{{"1", "0"}, {"1", "-1"}, {"ten", "1"}, {"1", ""}} {
		if _, err := ConvertAmount(refused[0], refused[1]); err == nil {
			t.Errorf("%v was accepted", refused)
		}
	}
}

func TestParseDecimal(t *testing.T) {
	for _, accepted := range []string{"0", "-1", "+1.5", "001.10", " 12.34 "} {
		if _, err := parseDecimal(accepted); err != nil {
			t.Errorf("%q refused: %v", accepted, err)
		}
	}
	for _, refused := range []string{"", "-", ".5", "5.", "1.2.3", "1e5", "--1", "+-1", "1,000", "0x10", "Inf"} {
		if _, err := parseDecimal(refused); err == nil {
			t.Errorf("%q accepted", refused)
		}
	}
	if got, err := CanonicalAmount("12.5"); err != nil || got != "12.5000" {
		t.Errorf("canonical 12.5 is %q %v", got, err)
	}
}
