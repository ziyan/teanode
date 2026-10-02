// Package rates keeps the stored exchange rates current and converts
// amounts with them. It sits between internal/finance, which fetches the
// European Central Bank's files and holds no database, and internal/db,
// which stores the rates and answers from them, so that the API and the
// agent decide when to fetch the same way.
package rates

import (
	"context"
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

	// fetchLockKey is the advisory lock storing a fetch holds for its
	// transaction, so two servers sharing a database that fetched the same
	// file together do not both write it. An arbitrary constant;
	// "exchange" in ASCII.
	fetchLockKey int64 = 0x65786368616e6765

	// fetchTimeout bounds the fetch of the latest day's file or the
	// ninety-day one. Every caller is a request or a budget computation
	// waiting on the answer, so an ECB that does not answer costs the one
	// caller that fetches this long, and the callers behind it no longer.
	fetchTimeout = 20 * time.Second

	// historyFetchTimeout bounds the fetch of the whole history, a few
	// megabytes, which happens once for an empty store or one left months
	// behind.
	historyFetchTimeout = time.Minute
)

// Fetcher decides when the stored rates are too old and fetches newer
// ones. One per process: it remembers when a fetch brought nothing new.
//
// The fetch itself holds neither the mutex nor a database transaction:
// the mutex is held to decide whether to fetch and to record what came of
// it, and the rates are stored in a transaction of their own once they
// have arrived.
type Fetcher struct {
	database db.Database
	source   *finance.ExchangeRateSource
	now      func() time.Time

	// fetchTimeout and historyFetchTimeout bound one fetch; a test
	// shortens them.
	fetchTimeout        time.Duration
	historyFetchTimeout time.Duration

	// mutex guards fruitlessFetchAt and fetchInFlight, which is closed
	// when the fetch this process is running ends, and nil when none is.
	mutex            sync.Mutex
	fruitlessFetchAt time.Time
	fetchInFlight    chan struct{}
}

// New builds a fetcher over a database and an ECB source.
func New(database db.Database, source *finance.ExchangeRateSource) *Fetcher {
	return &Fetcher{
		database: database, source: source, now: time.Now,
		fetchTimeout: fetchTimeout, historyFetchTimeout: historyFetchTimeout,
	}
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
// current, or within a quarter of an hour of a fetch that brought nothing
// new or failed. While this process is already fetching, it waits for that
// fetch at most as long as a fetch may take, and answers nil. A failed
// fetch is an error, and the caller may still answer from what is stored.
//
// Call it outside any transaction of the caller's: it waits on the
// network.
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
	if !self.fruitlessFetchAt.IsZero() && now.Sub(self.fruitlessFetchAt) < fruitlessFetchPause {
		self.mutex.Unlock()
		return nil
	}
	if inFlight := self.fetchInFlight; inFlight != nil {
		self.mutex.Unlock()
		waitTimer := time.NewTimer(self.fetchTimeout)
		defer waitTimer.Stop()
		select {
		case <-inFlight:
		case <-waitTimer.C:
			log.Debugf("the exchange rate fetch this process is running has taken %s; answering from what is stored", self.fetchTimeout)
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	done := make(chan struct{})
	self.fetchInFlight = done
	self.mutex.Unlock()
	defer func() {
		self.mutex.Lock()
		self.fetchInFlight = nil
		close(done)
		self.mutex.Unlock()
	}()

	err = self.fetch(ctx, latest, today)
	if err != nil && ctx.Err() == nil {
		// The ECB unreachable is paused on as well: every conversion
		// asking again would only wait out the timeout each time.
		self.mutex.Lock()
		self.fruitlessFetchAt = self.now().UTC()
		self.mutex.Unlock()
	}
	return err
}

// fetch fetches the file that fills the store from latest up to today,
// holding nothing while it waits on the ECB, then stores what came in a
// transaction of its own holding the fetch lock. Another server storing
// at that moment is storing the same rates, so this one's are dropped.
func (self *Fetcher) fetch(ctx context.Context, latest string, today time.Time) error {
	span := spanToFetch(latest, today)
	timeout := self.fetchTimeout
	if span == finance.ExchangeRateSpanHistory {
		timeout = self.historyFetchTimeout
	}
	fetchContext, cancel := context.WithTimeout(ctx, timeout)
	fetched, err := self.source.FetchEuroRates(fetchContext, span)
	cancel()
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
	isStored := false
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(fetchLockKey)
		if err != nil || !isLocked {
			return err
		}
		if _, err := tx.UpsertExchangeRates(exchangeRates); err != nil {
			return err
		}
		isStored = true
		return nil
	}); err != nil {
		return err
	}
	if !isStored {
		log.Debugf("another server was storing the exchange rates; this fetch of the ECB's %s rates is dropped", span)
		return nil
	}
	self.mutex.Lock()
	if newest <= latest {
		self.fruitlessFetchAt = self.now().UTC()
	} else {
		self.fruitlessFetchAt = time.Time{}
	}
	self.mutex.Unlock()
	log.Debugf("fetched the ECB's %s exchange rates: %d rates, the newest for %s", span, len(exchangeRates), newest)
	return nil
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
// brought up to today once, the first time an amount needs converting
// between two different currencies, so a computation in one currency never
// fetches, and one that converts every day of twenty years asks once rather
// than once a day (each ask opens a transaction of its own). Today is the
// newest day an amount can need, since EnsureRates brings a later day to
// today. Such a fetch waits on the network with the caller's transaction
// open, for at most the fetch's bound; a caller that must not do that
// ensures the rates first and converts with a nil fetcher.
type Converter struct {
	ctx         context.Context
	transaction db.Transaction
	rateByKey   map[string]*models.CurrencyPairRate

	// ensureRates is the fetcher's EnsureRates, nil to read only what is
	// stored; today is the day it is asked for, and isEnsured says it
	// has been.
	ensureRates func(ctx context.Context, on string) error
	today       string
	isEnsured   bool
}

// Converter builds a converter that reads through a transaction. A nil
// fetcher reads only what is stored.
func (self *Fetcher) Converter(ctx context.Context, transaction db.Transaction) *Converter {
	return NewConverter(ctx, self, transaction)
}

// NewConverter is Fetcher.Converter for a fetcher that may be nil.
func NewConverter(ctx context.Context, fetcher *Fetcher, transaction db.Transaction) *Converter {
	converter := &Converter{ctx: ctx, transaction: transaction, rateByKey: map[string]*models.CurrencyPairRate{}}
	if fetcher != nil {
		converter.ensureRates = fetcher.EnsureRates
		converter.today = fetcher.now().UTC().Format(time.DateOnly)
	}
	return converter
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
		if self.ensureRates != nil && !self.isEnsured {
			if err := self.ensureRates(self.ctx, self.today); err != nil {
				log.Warningf("cannot bring the exchange rates up to %s, converting with what is stored: %s", self.today, err)
			}
			self.isEnsured = true
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
