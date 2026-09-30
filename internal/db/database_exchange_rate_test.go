package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// A weekday has its own rates; a Sunday takes Friday's, and says so; two
// currencies other than the euro convert through it; a currency nobody
// published is named as having no rate.
func TestExchangeRateFallsBackToTheLatestPublishedDay(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if latest, err := tx.LatestExchangeRateDay(models.RateSourceECB); err != nil || latest != "" {
			t.Fatalf("an empty table has no latest day: %v %q", err, latest)
		}
		keptCount, err := tx.UpsertExchangeRates([]models.ExchangeRate{
			{RateOn: "2026-09-03", CurrencyCode: "USD", EuroRate: "1.0900", RateSource: models.RateSourceECB},
			{RateOn: "2026-09-03", CurrencyCode: "JPY", EuroRate: "158.00", RateSource: models.RateSourceECB},
			{RateOn: "2026-09-04", CurrencyCode: "USD", EuroRate: "1.1000", RateSource: models.RateSourceECB},
			{RateOn: "2026-09-04", CurrencyCode: "JPY", EuroRate: "160.00", RateSource: models.RateSourceECB},
			{RateOn: "2026-09-07", CurrencyCode: "USD", EuroRate: "1.1200", RateSource: models.RateSourceECB},
		})
		if err != nil || keptCount != 5 {
			t.Fatalf("UpsertExchangeRates: %v %d", err, keptCount)
		}
		// Fetching the same day again replaces rather than duplicates.
		if _, err := tx.UpsertExchangeRates([]models.ExchangeRate{{RateOn: "2026-09-07", CurrencyCode: "USD", EuroRate: "1.1100"}}); err != nil {
			t.Fatalf("UpsertExchangeRates again: %s", err)
		}
		if latest, err := tx.LatestExchangeRateDay(models.RateSourceECB); err != nil || latest != "2026-09-07" {
			t.Errorf("LatestExchangeRateDay: %v %q", err, latest)
		}

		weekday, err := tx.ExchangeRate("EUR", "USD", "2026-09-03")
		if err != nil || weekday.Rate != "1.0900000000" || weekday.RateOn != "2026-09-03" {
			t.Errorf("a weekday's own rate: %v %+v", err, weekday)
		}
		sunday, err := tx.ExchangeRate("USD", "EUR", "2026-09-06")
		if err != nil || sunday.Rate != "0.9090909091" || sunday.RateOn != "2026-09-04" || sunday.RateSource != models.RateSourceECB {
			t.Errorf("a Sunday takes Friday's rate: %v %+v", err, sunday)
		}
		cross, err := tx.ExchangeRate("usd", "JPY", "2026-09-06")
		if err != nil || cross.Rate != "145.4545454545" || cross.RateOn != "2026-09-04" {
			t.Errorf("a cross rate through the euro: %v %+v", err, cross)
		}
		// Monday has a dollar rate but no yen rate: the yen's Friday is
		// the older day, and the one reported.
		mixed, err := tx.ExchangeRate("USD", "JPY", "2026-09-07")
		if err != nil || mixed.RateOn != "2026-09-04" || mixed.Rate != "144.1441441441" {
			t.Errorf("the older of the two days: %v %+v", err, mixed)
		}
		same, err := tx.ExchangeRate("USD", "USD", "2026-09-06")
		if err != nil || same.Rate != "1.0000000000" {
			t.Errorf("a currency against itself: %v %+v", err, same)
		}

		_, err = tx.ExchangeRate("USD", "XTS", "2026-09-06")
		var noRate *finance.ErrNoExchangeRate
		if !errors.As(err, &noRate) || noRate.CurrencyCode != "XTS" {
			t.Errorf("an unpublished currency must name itself: %v", err)
		}
		if _, err := tx.ExchangeRate("USD", "EUR", "2026-09-02"); !errors.As(err, &noRate) || noRate.CurrencyCode != "USD" {
			t.Errorf("a day before any rate has none: %v", err)
		}

		rates, err := tx.EuroRatesOn([]string{"USD", "JPY", "EUR", "XTS"}, "2026-09-05")
		if err != nil || len(rates) != 3 || rates["EUR"].EuroRate != "1.0000000000" || rates["USD"].RateOn != "2026-09-04" || rates["XTS"] != nil {
			t.Errorf("EuroRatesOn: %v %+v", err, rates)
		}

		// A week after its last rate the yen still converts, dated the day
		// the rate is from; a day later that rate is too old to use, and
		// the yen has none.
		weekLater, err := tx.ExchangeRate("EUR", "JPY", "2026-09-11")
		if err != nil || weekLater.RateOn != "2026-09-04" {
			t.Errorf("a rate seven days old is used and dated: %v %+v", err, weekLater)
		}
		if _, err := tx.ExchangeRate("EUR", "JPY", "2026-09-12"); !errors.As(err, &noRate) || noRate.CurrencyCode != "JPY" {
			t.Errorf("a rate more than seven days old is none: %v", err)
		}
		if stale, err := tx.EuroRatesOn([]string{"JPY", "USD"}, "2026-09-12"); err != nil || stale["JPY"] != nil || stale["USD"] == nil {
			t.Errorf("EuroRatesOn leaves out a stale rate and keeps a recent one: %v %+v", err, stale)
		}

		if _, err := tx.UpsertExchangeRates([]models.ExchangeRate{{RateOn: "2026-09-08", CurrencyCode: "USD", EuroRate: "-1"}}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a rate that is not positive must be refused: %v", err)
		}
	})
}
