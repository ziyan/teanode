package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
)

// ExchangeRateOperation is the stored euro reference rates, server-wide:
// rates are public facts, not anyone's data. Fetching them is the caller's
// (internal/finance), which asks LatestExchangeRateDay first.
type ExchangeRateOperation interface {
	// UpsertExchangeRates keeps rates, replacing any for the same day,
	// currency and source. Fetching the same file twice changes nothing.
	UpsertExchangeRates(exchangeRates []models.ExchangeRate) (int, error)

	// LatestExchangeRateDay is the newest day stored from a source,
	// "2006-01-02", or empty when there is none.
	LatestExchangeRateDay(rateSource models.RateSource) (string, error)

	// EuroRatesOn is, for each currency asked, what one euro bought on the
	// latest day on or before the day given that the ECB published it. The
	// euro is one, dated the day given. A currency with no rate is absent.
	EuroRatesOn(currencyCodes []string, on string) (map[string]*models.ExchangeRate, error)

	// ExchangeRate is what one unit of one currency bought in the other on
	// a day, from the latest published rates on or before it, with the day
	// those are from (the older of the two currencies'). A currency with
	// no rate is a *finance.ErrNoExchangeRate naming it.
	ExchangeRate(fromCurrencyCode, toCurrencyCode, on string) (*models.CurrencyPairRate, error)
}

// exchangeRateBatchSize is how many rates go in one statement. The ECB's
// full history is about two hundred thousand rows.
const exchangeRateBatchSize = 1000

type exchangeRateModel struct {
	RateOn       string    `gorm:"column:rate_on;primaryKey"`
	CurrencyCode string    `gorm:"column:currency_code;primaryKey"`
	EuroRate     string    `gorm:"column:euro_rate"`
	RateSource   string    `gorm:"column:rate_source;primaryKey"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (exchangeRateModel) TableName() string { return "exchange_rate" }

func (self *transaction) UpsertExchangeRates(exchangeRates []models.ExchangeRate) (int, error) {
	if len(exchangeRates) == 0 {
		return 0, nil
	}
	now := time.Now()
	rows := make([]exchangeRateModel, 0, len(exchangeRates))
	for _, exchangeRate := range exchangeRates {
		rateOn, err := parseDay(exchangeRate.RateOn)
		if err != nil {
			return 0, err
		}
		rateSource := exchangeRate.RateSource
		if rateSource == "" {
			rateSource = models.RateSourceECB
		}
		if !rateSource.IsValid() {
			return 0, fmt.Errorf("%w: %q is not a rate source", ErrInvalidArguments, rateSource)
		}
		currencyCode := strings.ToUpper(strings.TrimSpace(exchangeRate.CurrencyCode))
		if currencyCode == "" || currencyCode == finance.EuroCurrencyCode {
			return 0, fmt.Errorf("%w: %q is not a currency with a euro rate", ErrInvalidArguments, exchangeRate.CurrencyCode)
		}
		// The rate against one euro is the rate itself: this checks it is a
		// positive decimal and writes it with the ten places the column keeps.
		euroRate, err := finance.CrossRate("1", exchangeRate.EuroRate)
		if err != nil {
			return 0, fmt.Errorf("%w: the euro rate %q of %s is not a positive decimal", ErrInvalidArguments, exchangeRate.EuroRate, currencyCode)
		}
		rows = append(rows, exchangeRateModel{
			RateOn: rateOn, CurrencyCode: currencyCode, EuroRate: euroRate, RateSource: string(rateSource), CreatedAt: now,
		})
	}
	result := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "rate_on"}, {Name: "currency_code"}, {Name: "rate_source"}},
		DoUpdates: clause.AssignmentColumns([]string{"euro_rate"}),
	}).CreateInBatches(rows, exchangeRateBatchSize)
	if result.Error != nil {
		return 0, result.Error
	}
	return len(rows), nil
}

func (self *transaction) LatestExchangeRateDay(rateSource models.RateSource) (string, error) {
	var latest string
	if err := self.tx.Raw(`SELECT COALESCE(to_char(MAX("rate_on"), 'YYYY-MM-DD'), '') FROM "exchange_rate" WHERE "rate_source" = ?`,
		string(rateSource)).Scan(&latest).Error; err != nil {
		return "", err
	}
	return latest, nil
}

func (self *transaction) EuroRatesOn(currencyCodes []string, on string) (map[string]*models.ExchangeRate, error) {
	on, err := parseDay(on)
	if err != nil {
		return nil, err
	}
	rates := map[string]*models.ExchangeRate{}
	asked := []string{}
	for _, currencyCode := range currencyCodes {
		currencyCode = strings.ToUpper(strings.TrimSpace(currencyCode))
		if currencyCode == finance.EuroCurrencyCode {
			rates[currencyCode] = &models.ExchangeRate{
				RateOn: on, CurrencyCode: currencyCode, EuroRate: "1.0000000000", RateSource: models.RateSourceECB,
			}
			continue
		}
		if currencyCode != "" {
			asked = append(asked, currencyCode)
		}
	}
	if len(asked) == 0 {
		return rates, nil
	}
	var found []struct {
		RateOn       time.Time `gorm:"column:rate_on"`
		CurrencyCode string    `gorm:"column:currency_code"`
		EuroRate     string    `gorm:"column:euro_rate"`
		RateSource   string    `gorm:"column:rate_source"`
		CreatedAt    time.Time `gorm:"column:created_at"`
	}
	if err := self.tx.Raw(`SELECT DISTINCT ON ("currency_code") "rate_on", "currency_code", "euro_rate"::text AS "euro_rate",
			"rate_source", "created_at"
		FROM "exchange_rate"
		WHERE "currency_code" = ANY(?::text[]) AND "rate_source" = ? AND "rate_on" <= ?::date
		ORDER BY "currency_code", "rate_on" DESC`, pq.Array(asked), string(models.RateSourceECB), on).
		Scan(&found).Error; err != nil {
		return nil, err
	}
	for _, row := range found {
		rates[row.CurrencyCode] = &models.ExchangeRate{
			RateOn: formatDay(row.RateOn), CurrencyCode: row.CurrencyCode, EuroRate: row.EuroRate,
			RateSource: models.RateSource(row.RateSource), CreatedAt: row.CreatedAt.In(time.Local),
		}
	}
	return rates, nil
}

func (self *transaction) ExchangeRate(fromCurrencyCode, toCurrencyCode, on string) (*models.CurrencyPairRate, error) {
	fromCurrencyCode = strings.ToUpper(strings.TrimSpace(fromCurrencyCode))
	toCurrencyCode = strings.ToUpper(strings.TrimSpace(toCurrencyCode))
	on, err := parseDay(on)
	if err != nil {
		return nil, err
	}
	if fromCurrencyCode == "" || toCurrencyCode == "" {
		return nil, fmt.Errorf("%w: an exchange rate needs two currencies", ErrInvalidArguments)
	}
	if fromCurrencyCode == toCurrencyCode {
		return &models.CurrencyPairRate{
			FromCurrencyCode: fromCurrencyCode, ToCurrencyCode: toCurrencyCode, Rate: "1.0000000000",
			RateOn: on, RateSource: models.RateSourceECB,
		}, nil
	}
	rates, err := self.EuroRatesOn([]string{fromCurrencyCode, toCurrencyCode}, on)
	if err != nil {
		return nil, err
	}
	fromRate, toRate := rates[fromCurrencyCode], rates[toCurrencyCode]
	if fromRate == nil {
		return nil, &finance.ErrNoExchangeRate{CurrencyCode: fromCurrencyCode}
	}
	if toRate == nil {
		return nil, &finance.ErrNoExchangeRate{CurrencyCode: toCurrencyCode}
	}
	rate, err := finance.CrossRate(fromRate.EuroRate, toRate.EuroRate)
	if err != nil {
		return nil, err
	}
	// The euro's own day is the day asked, so the other currency's is the
	// one that says how old the rate is; between two others, the older.
	rateOn := fromRate.RateOn
	if fromCurrencyCode == finance.EuroCurrencyCode || (toCurrencyCode != finance.EuroCurrencyCode && toRate.RateOn < rateOn) {
		rateOn = toRate.RateOn
	}
	return &models.CurrencyPairRate{
		FromCurrencyCode: fromCurrencyCode, ToCurrencyCode: toCurrencyCode, Rate: rate, RateOn: rateOn, RateSource: models.RateSourceECB,
	}, nil
}
