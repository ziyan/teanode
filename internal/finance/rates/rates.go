// Package rates keeps the stored exchange rates current and converts
// amounts with them. It sits between internal/finance, which fetches the
// European Central Bank's files and holds no database, and internal/db,
// which stores the rates and answers from them, so that the API and the
// agent decide when to fetch the same way.
package rates

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

var log = logging.MustGetLogger("rates")

const (
	// fruitlessFetchPause is how long a fetch that brought nothing newer
	// than what was stored, or failed, keeps this process from fetching
	// again. On a
	// TARGET holiday, or on a weekday morning before the ECB publishes at
	// about four in the afternoon in Frankfurt, every conversion would
	// otherwise fetch the file again.
	fruitlessFetchPause = 15 * time.Minute

	// ninetyDayFileDays is how far back the ECB's ninety-day file reaches.
	// A store whose newest day is older than that is fetched the whole
	// history again, so no gap is left between the two.
	ninetyDayFileDays = 90

	// fetchLockKey is the advisory lock one fetch holds for its
	// transaction, so two servers sharing a database do not fetch the same
	// file together. An arbitrary constant; "exchange" in ASCII.
	fetchLockKey int64 = 0x65786368616e6765

	// fetchLockWait is how long a caller whose fetch another server is
	// running waits for it before answering from what is stored.
	fetchLockWait = 20 * time.Second

	// fetchLockPoll is how often that caller looks again.
	fetchLockPoll = 500 * time.Millisecond
)

// errFetchElsewhere is another transaction holding the fetch lock.
var errFetchElsewhere = errors.New("rates: another server is fetching the exchange rates")

// Fetcher decides when the stored rates are too old and fetches newer
// ones. One per process: it remembers when a fetch brought nothing new.
type Fetcher struct {
	database db.Database
	source   *finance.ExchangeRateSource
	now      func() time.Time

	// mutex serializes this process's fetches, and guards
	// fruitlessFetchAt.
	mutex            sync.Mutex
	fruitlessFetchAt time.Time
}

// New builds a fetcher over a database and an ECB source.
func New(database db.Database, source *finance.ExchangeRateSource) *Fetcher {
	return &Fetcher{database: database, source: source, now: time.Now}
}

var (
	sharedMutex   sync.Mutex
	sharedFetcher *Fetcher
)

// Shared is the process's fetcher, over the ECB itself. A process has one
// database; the first caller's is the one kept.
func Shared(database db.Database) *Fetcher {
	sharedMutex.Lock()
	defer sharedMutex.Unlock()
	if sharedFetcher == nil {
		sharedFetcher = New(database, finance.NewExchangeRateSource())
	}
	return sharedFetcher
}

// LatestBusinessDay is the latest weekday on or before a day, which is
// the latest day the ECB can have published rates for. TARGET holidays
// are not modelled: on one, the fetch finds nothing newer and the day
// falls back to the one before, which is what the rates for a holiday are.
func LatestBusinessDay(day time.Time) time.Time {
	switch day.Weekday() {
	case time.Saturday:
		return day.AddDate(0, 0, -1)
	case time.Sunday:
		return day.AddDate(0, 0, -2)
	}
	return day
}

// EnsureRates fetches the ECB's rates when the newest stored day is older
// than the latest business day on or before the day given (today, for a
// day in the future). It answers nil without fetching when the store is
// current, when another server is fetching, or within a quarter of an
// hour of a fetch that brought nothing new; a failed fetch is an error,
// and the caller may still answer from what is stored.
func (self *Fetcher) EnsureRates(ctx context.Context, on string) error {
	day, err := time.Parse(time.DateOnly, on)
	if err != nil {
		return fmt.Errorf("rates: %q is not a day", on)
	}
	now := self.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if day.After(today) {
		day = today
	}
	needed := LatestBusinessDay(day).Format(time.DateOnly)

	latest, err := self.latestStoredDay(ctx)
	if err != nil {
		return err
	}
	if latest != "" && latest >= needed {
		return nil
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.fruitlessFetchAt.IsZero() && now.Sub(self.fruitlessFetchAt) < fruitlessFetchPause {
		return nil
	}
	waitUntil := now.Add(fetchLockWait)
	for {
		err = self.fetchUnderLock(ctx, needed, today)
		if err != nil && !errors.Is(err, errFetchElsewhere) && ctx.Err() == nil {
			// The ECB unreachable is paused on as well: every conversion
			// asking again would only wait out the timeout each time.
			self.fruitlessFetchAt = self.now().UTC()
		}
		if !errors.Is(err, errFetchElsewhere) {
			return err
		}
		if self.now().After(waitUntil) {
			log.Debugf("another server has been fetching the exchange rates for %s; answering from what is stored", fetchLockWait)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(fetchLockPoll):
		}
	}
}

// fetchUnderLock fetches, in one transaction holding the fetch lock. It
// looks at the store again once it holds the lock: the server that held
// it before may have fetched what is needed.
func (self *Fetcher) fetchUnderLock(ctx context.Context, needed string, today time.Time) error {
	return self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(fetchLockKey)
		if err != nil {
			return err
		}
		if !isLocked {
			return errFetchElsewhere
		}
		latest, err := tx.LatestExchangeRateDay(models.RateSourceECB)
		if err != nil {
			return err
		}
		if latest != "" && latest >= needed {
			return nil
		}
		span := spanToFetch(latest, today)
		fetched, err := self.source.FetchEuroRates(ctx, span)
		if err != nil {
			return err
		}
		exchangeRates := make([]models.ExchangeRate, 0, len(fetched))
		newest := ""
		for _, rate := range fetched {
			exchangeRates = append(exchangeRates, models.ExchangeRate{
				RateOn: rate.RateOn, CurrencyCode: rate.CurrencyCode, EuroRate: rate.EuroRate, RateSource: models.RateSourceECB,
			})
			if rate.RateOn > newest {
				newest = rate.RateOn
			}
		}
		if _, err := tx.UpsertExchangeRates(exchangeRates); err != nil {
			return err
		}
		if newest <= latest {
			self.fruitlessFetchAt = self.now().UTC()
		} else {
			self.fruitlessFetchAt = time.Time{}
		}
		log.Debugf("fetched the ECB's %s exchange rates: %d rates, the newest for %s", span, len(exchangeRates), newest)
		return nil
	})
}

// spanToFetch is which of the ECB's files fills the store up to today:
// the whole history for an empty store, or for one whose newest day is
// older than the ninety-day file reaches, so no gap is left; the latest
// day's file when the store holds the business day before the latest one,
// so only one day is missing; otherwise the ninety-day file.
func spanToFetch(latest string, today time.Time) finance.ExchangeRateSpan {
	if latest == "" {
		return finance.ExchangeRateSpanHistory
	}
	latestDay, err := time.Parse(time.DateOnly, latest)
	if err != nil || today.Sub(latestDay) > ninetyDayFileDays*24*time.Hour {
		return finance.ExchangeRateSpanHistory
	}
	latestBusinessDay := LatestBusinessDay(today)
	if LatestBusinessDay(latestBusinessDay.AddDate(0, 0, -1)).Equal(latestDay) {
		return finance.ExchangeRateSpanLatest
	}
	return finance.ExchangeRateSpanNinetyDays
}

func (self *Fetcher) latestStoredDay(ctx context.Context) (string, error) {
	var latest string
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		latest, err = tx.LatestExchangeRateDay(models.RateSourceECB)
		return err
	})
	return latest, err
}

// ExchangeRate is what one unit of one currency bought in another on a
// day, fetching first when the store is behind. A fetch that fails is
// logged and the rate answered from what is stored, which may be an
// earlier day's; the answer says which day it is from.
func (self *Fetcher) ExchangeRate(ctx context.Context, fromCurrencyCode, toCurrencyCode, on string) (*models.CurrencyPairRate, error) {
	if err := self.EnsureRates(ctx, on); err != nil {
		log.Warningf("cannot bring the exchange rates up to %s, answering from what is stored: %s", on, err)
	}
	var rate *models.CurrencyPairRate
	err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		rate, err = tx.ExchangeRate(fromCurrencyCode, toCurrencyCode, on)
		return err
	})
	return rate, err
}

// Converter converts amounts, each at the rate of its own day, reading the
// rates through one transaction and remembering each it read. The store is
// brought up to a day only the first time an amount of that day or later
// needs converting between two different currencies, so a computation in
// one currency never fetches.
type Converter struct {
	ctx         context.Context
	fetcher     *Fetcher
	transaction db.Transaction
	ensuredUpTo string
	rateByKey   map[string]*models.CurrencyPairRate
}

// Converter builds a converter that reads through a transaction. A nil
// fetcher reads only what is stored.
func (self *Fetcher) Converter(ctx context.Context, transaction db.Transaction) *Converter {
	return NewConverter(ctx, self, transaction)
}

// NewConverter is Fetcher.Converter for a fetcher that may be nil.
func NewConverter(ctx context.Context, fetcher *Fetcher, transaction db.Transaction) *Converter {
	return &Converter{ctx: ctx, fetcher: fetcher, transaction: transaction, rateByKey: map[string]*models.CurrencyPairRate{}}
}

// Convert is an amount in one currency in another, at the rate of the day
// given, with the rate used (nil when the currencies are the same). A
// currency with no rate is a *finance.ErrNoExchangeRate.
func (self *Converter) Convert(amount, fromCurrencyCode, toCurrencyCode, on string) (string, *models.CurrencyPairRate, error) {
	if fromCurrencyCode == toCurrencyCode {
		canonical, err := finance.CanonicalAmount(amount)
		return canonical, nil, err
	}
	key := fromCurrencyCode + "|" + toCurrencyCode + "|" + on
	rate, isKnown := self.rateByKey[key]
	if !isKnown {
		if self.fetcher != nil && on > self.ensuredUpTo {
			if err := self.fetcher.EnsureRates(self.ctx, on); err != nil {
				log.Warningf("cannot bring the exchange rates up to %s, converting with what is stored: %s", on, err)
			}
			self.ensuredUpTo = on
		}
		var err error
		if rate, err = self.transaction.ExchangeRate(fromCurrencyCode, toCurrencyCode, on); err != nil {
			return "", nil, err
		}
		self.rateByKey[key] = rate
	}
	converted, err := finance.ConvertAmount(amount, rate.Rate)
	return converted, rate, err
}
