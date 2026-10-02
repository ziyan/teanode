package rates

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// fakeCentralBank serves the three ECB files, each holding the weekdays of
// a range the test sets, and counts what it was asked for.
type fakeCentralBank struct {
	mutex         sync.Mutex
	lastDayByFile map[string]string
	requestCount  map[string]int
}

func (self *fakeCentralBank) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fileName := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
		self.mutex.Lock()
		self.requestCount[fileName]++
		lastDay := self.lastDayByFile[fileName]
		self.mutex.Unlock()
		if lastDay == "" {
			http.NotFound(writer, request)
			return
		}
		last, _ := time.Parse(time.DateOnly, lastDay)
		first := last.AddDate(0, 0, -100)
		if fileName == "eurofxref-daily.xml" {
			first = last
		}
		var builder strings.Builder
		builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Envelope><Cube>`)
		for day := last; !day.Before(first); day = day.AddDate(0, 0, -1) {
			if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
				continue
			}
			fmt.Fprintf(&builder, `<Cube time="%s"><Cube currency="USD" rate="1.%04d"/><Cube currency="JPY" rate="160.50"/></Cube>`, day.Format(time.DateOnly), day.YearDay())
		}
		builder.WriteString(`</Cube></Envelope>`)
		_, _ = writer.Write([]byte(builder.String()))
	}))
	t.Cleanup(server.Close)
	return server
}

func (self *fakeCentralBank) set(fileName, lastDay string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.lastDayByFile[fileName] = lastDay
}

func (self *fakeCentralBank) count(fileName string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.requestCount[fileName]
}

// The fetch policy, day by day: the whole history into an empty store; a
// Sunday answered from Friday without asking; the ninety-day file once
// the store is behind by more than a day; the latest day's file when one
// day is missing; no second fetch within a quarter of an hour of one that
// brought nothing new, and one after.
func TestExchangeRateFetchPolicy(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	bank := &fakeCentralBank{lastDayByFile: map[string]string{}, requestCount: map[string]int{}}
	server := bank.serve(t)
	fetcher := New(database, finance.NewExchangeRateSourceAt(server.URL))
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC) // a Friday evening
	fetcher.now = func() time.Time { return now }

	bank.set("eurofxref-hist.xml", "2026-09-04")
	rate, err := fetcher.ExchangeRate(t.Context(), "USD", "JPY", "2026-09-04")
	if err != nil {
		t.Fatalf("ExchangeRate: %s", err)
	}
	if bank.count("eurofxref-hist.xml") != 1 || rate.RateOn != "2026-09-04" || rate.Rate == "" {
		t.Fatalf("an empty store fetches the whole history: %d fetches, %+v", bank.count("eurofxref-hist.xml"), rate)
	}

	now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) // the Sunday after
	rate, err = fetcher.ExchangeRate(t.Context(), "USD", "EUR", "2026-09-06")
	if err != nil {
		t.Fatalf("ExchangeRate: %s", err)
	}
	if rate.RateOn != "2026-09-04" || bank.count("eurofxref-hist.xml") != 1 || bank.count("eurofxref-hist-90d.xml") != 0 || bank.count("eurofxref-daily.xml") != 0 {
		t.Fatalf("a Sunday is Friday's rate, without a fetch: %+v", rate)
	}

	// Twelve days on, before the day's rates are out: the ninety-day file
	// brings everything up to yesterday.
	now = time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	bank.set("eurofxref-hist-90d.xml", "2026-09-15")
	bank.set("eurofxref-daily.xml", "2026-09-15")
	rate, err = fetcher.ExchangeRate(t.Context(), "USD", "EUR", "2026-09-16")
	if err != nil {
		t.Fatalf("ExchangeRate: %s", err)
	}
	if bank.count("eurofxref-hist-90d.xml") != 1 || rate.RateOn != "2026-09-15" {
		t.Fatalf("a store behind by days fetches the ninety-day file: %d fetches, %+v", bank.count("eurofxref-hist-90d.xml"), rate)
	}

	// One day missing: the latest day's file, which has nothing new yet.
	if _, err := fetcher.ExchangeRate(t.Context(), "USD", "EUR", "2026-09-16"); err != nil {
		t.Fatalf("ExchangeRate: %s", err)
	}
	if bank.count("eurofxref-daily.xml") != 1 {
		t.Fatalf("one day missing fetches the latest day's file: %d fetches", bank.count("eurofxref-daily.xml"))
	}

	// Nothing came of it, so ten minutes later nothing is fetched.
	now = now.Add(10 * time.Minute)
	if _, err := fetcher.ExchangeRate(t.Context(), "USD", "EUR", "2026-09-16"); err != nil {
		t.Fatalf("ExchangeRate: %s", err)
	}
	if bank.count("eurofxref-daily.xml") != 1 || bank.count("eurofxref-hist-90d.xml") != 1 {
		t.Fatalf("no fetch within fifteen minutes of a fruitless one: %d daily fetches", bank.count("eurofxref-daily.xml"))
	}

	// Past the pause, and the day is out.
	now = now.Add(10 * time.Minute)
	bank.set("eurofxref-daily.xml", "2026-09-16")
	rate, err = fetcher.ExchangeRate(t.Context(), "USD", "EUR", "2026-09-16")
	if err != nil {
		t.Fatalf("ExchangeRate: %s", err)
	}
	if bank.count("eurofxref-daily.xml") != 2 || rate.RateOn != "2026-09-16" {
		t.Fatalf("after the pause the day is fetched: %d fetches, %+v", bank.count("eurofxref-daily.xml"), rate)
	}

	var noRate *finance.ErrNoExchangeRate
	if _, err := fetcher.ExchangeRate(t.Context(), "USD", "XTS", "2026-09-16"); !errors.As(err, &noRate) || noRate.CurrencyCode != "XTS" {
		t.Fatalf("a currency the ECB does not publish has no rate: %v", err)
	}
}

// An ECB that does not answer costs the caller that fetches a bounded
// wait, and holds neither the fetch lock nor a transaction while it waits;
// a second caller meanwhile does not fetch again, waits no longer than the
// first, and answers from what is stored.
func TestExchangeRateFetchThatHangsIsBounded(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	requestArrived := make(chan struct{}, 4)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		requestArrived <- struct{}{}
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	fetcher := New(database, finance.NewExchangeRateSourceAt(server.URL))
	fetcher.fetchTimeout = 500 * time.Millisecond
	fetcher.historyFetchTimeout = 500 * time.Millisecond

	started := time.Now()
	firstDone := make(chan error, 1)
	go func() { firstDone <- fetcher.EnsureRates(t.Context(), time.Now().UTC().Format(time.DateOnly)) }()
	<-requestArrived

	// While the fetch waits on the ECB, the fetch lock is free: nothing
	// holds a transaction open across the network call.
	if err := database.Transaction(func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(fetchLockKey)
		if err != nil {
			return err
		}
		if !isLocked {
			return errors.New("the fetch lock is held while the ECB is being asked")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := fetcher.EnsureRates(t.Context(), time.Now().UTC().Format(time.DateOnly)); err != nil {
		t.Fatalf("a caller behind a fetch in flight answers from what is stored: %s", err)
	}
	if err := <-firstDone; err == nil {
		t.Fatal("a fetch the ECB never answered is an error")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the wait is bounded by the fetch timeout, took %s", elapsed)
	}
	if requestCount.Load() != 1 {
		t.Fatalf("one fetch in flight at a time: %d requests", requestCount.Load())
	}
	if err := fetcher.EnsureRates(t.Context(), time.Now().UTC().Format(time.DateOnly)); err != nil || requestCount.Load() != 1 {
		t.Fatalf("a failed fetch is not tried again within the pause: %v, %d requests", err, requestCount.Load())
	}
}

// A converter asked for every day of a long range, the way a cash flow
// over years converts each day, brings the store up to date once rather
// than once for each later day it reaches, and still has today's rate.
func TestConverterEnsuresRatesOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	bank := &fakeCentralBank{lastDayByFile: map[string]string{}, requestCount: map[string]int{}}
	server := bank.serve(t)
	fetcher := New(database, finance.NewExchangeRateSourceAt(server.URL))
	now := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC) // a Friday evening
	fetcher.now = func() time.Time { return now }
	bank.set("eurofxref-hist.xml", "2026-09-04")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		converter := fetcher.Converter(t.Context(), tx)
		ensureCount := 0
		ensureRates := converter.ensureRates
		converter.ensureRates = func(ctx context.Context, on string) error {
			ensureCount++
			return ensureRates(ctx, on)
		}
		var newestRate *models.CurrencyPairRate
		for day := now.AddDate(0, 0, -90); !day.After(now); day = day.AddDate(0, 0, 1) {
			_, rate, err := converter.Convert("10.00", "USD", "JPY", day.Format(time.DateOnly))
			if err != nil {
				t.Fatalf("Convert on %s: %s", day.Format(time.DateOnly), err)
			}
			newestRate = rate
		}
		if ensureCount != 1 {
			t.Errorf("ninety-one days converted asked for the rates to be brought up to date %d times, not once", ensureCount)
		}
		if newestRate == nil || newestRate.RateOn != "2026-09-04" {
			t.Errorf("today converts at today's rate: %+v", newestRate)
		}
	})
	if bank.count("eurofxref-hist.xml") != 1 {
		t.Errorf("one fetch of the history: %d", bank.count("eurofxref-hist.xml"))
	}
}

func TestLatestBusinessDay(t *testing.T) {
	for day, expected := range map[string]string{
		"2026-09-18": "2026-09-18", // Friday
		"2026-09-19": "2026-09-18", // Saturday
		"2026-09-20": "2026-09-18", // Sunday
		"2026-09-21": "2026-09-21", // Monday
	} {
		parsed, _ := time.Parse(time.DateOnly, day)
		if got := LatestBusinessDay(parsed).Format(time.DateOnly); got != expected {
			t.Fatalf("the latest business day on or before %s is %s, not %s", day, expected, got)
		}
	}
}

func TestSpanToFetch(t *testing.T) {
	today := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC) // a Wednesday
	for latest, expected := range map[string]finance.ExchangeRateSpan{
		"":           finance.ExchangeRateSpanHistory,
		"2026-05-01": finance.ExchangeRateSpanHistory,
		"2026-09-01": finance.ExchangeRateSpanNinetyDays,
		"2026-09-15": finance.ExchangeRateSpanLatest,
	} {
		if got := spanToFetch(latest, today); got != expected {
			t.Fatalf("with %q stored the span is %s, not %s", latest, expected, got)
		}
	}
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if got := spanToFetch("2026-09-18", monday); got != finance.ExchangeRateSpanLatest {
		t.Fatalf("on a Monday with Friday stored only Monday is missing: %s", got)
	}
}
