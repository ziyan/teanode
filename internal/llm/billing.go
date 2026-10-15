package llm

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The kinds of token a bill can be broken into, named as the price fields
// of a model are.
const (
	BilledTokenKindInput       = "input"
	BilledTokenKindCachedInput = "cachedInput"
	BilledTokenKindCacheWrite  = "cacheWrite"
	BilledTokenKindOutput      = "output"
	BilledTokenKindAudioInput  = "audioInput"
)

const (
	billingRequestTimeout = 30 * time.Second

	// A report is read a page at a time; the cap only stops a provider
	// that keeps answering has_more from holding the check forever. Thirty
	// one days fit a page, so a few weeks never come close.
	billingPageLimit = 20

	// A day with fewer tokens than this says little. The bill's amounts
	// are not rounded to cents, so even a small day gives an exact rate;
	// the floor only drops days with a handful of tokens.
	billingMinimumDayTokens = 1_000

	// How many days must come close to a price before it is believed.
	billingMinimumDayCount = 2
)

// Billing is what an organization was billed over a window: dollars per
// line item per day from the costs report, and token counts per model per
// day from the completions usage report.
type Billing struct {
	// Currency is the currency of the amounts, as the provider names it
	// (usd), empty when the window had no costs.
	Currency string

	// amounts maps the start of a day (unix seconds) to the amount of each
	// line item, such as "chat-model-small, input".
	amounts map[int64]map[string]float64

	// tokens maps the start of a day to the token counts of each model.
	tokens map[int64]map[string]billedTokenCounts
}

type billedTokenCounts struct {
	InputUncachedTokens   float64
	InputCachedTokens     float64
	InputCacheWriteTokens float64
	OutputTokens          float64
	InputAudioTokens      float64
	OutputAudioTokens     float64
	InputTextTokens       float64
	OutputTextTokens      float64
}

// BilledRate is the price per million tokens that the bill implies for one
// model and one kind of token.
type BilledRate struct {
	// Model is named as the report names it, which may carry a snapshot
	// date.
	Model string

	// TokenKind is one of the BilledTokenKind constants.
	TokenKind string

	// PerMillion is dollars per million tokens, rounded to two significant
	// figures.
	PerMillion float64

	// DayCount is the number of days whose rate came within five percent of
	// the price before rounding.
	DayCount int

	// BilledAmount is what the window billed for this line item in all.
	BilledAmount float64
}

// BilledDay is what one model was billed on one day, all kinds of token
// together.
type BilledDay struct {
	Day          time.Time
	Model        string
	BilledAmount float64
	Currency     string
}

type billingPage struct {
	Data []struct {
		StartTime int64 `json:"start_time"`
		Results   []struct {
			LineItem *string `json:"line_item"`
			Amount   struct {
				Value    float64 `json:"value"`
				Currency string  `json:"currency"`
			} `json:"amount"`

			Model                 string  `json:"model"`
			InputUncachedTokens   float64 `json:"input_uncached_tokens"`
			InputCachedTokens     float64 `json:"input_cached_tokens"`
			InputCacheWriteTokens float64 `json:"input_cache_write_tokens"`
			OutputTokens          float64 `json:"output_tokens"`
			InputAudioTokens      float64 `json:"input_audio_tokens"`
			OutputAudioTokens     float64 `json:"output_audio_tokens"`
			InputTextTokens       float64 `json:"input_text_tokens"`
			OutputTextTokens      float64 `json:"output_text_tokens"`
		} `json:"results"`
	} `json:"data"`
	HasMore  bool   `json:"has_more"`
	NextPage string `json:"next_page"`
}

// ReadBilling reads the organization's daily costs and daily completions
// usage from since until now. baseURL is the provider's API root (the part
// before /organization), and billingKey an organization admin key: project
// and service account keys are refused by these reports.
func ReadBilling(ctx context.Context, client *http.Client, baseURL, billingKey string, since time.Time) (*Billing, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	billing := &Billing{
		amounts: map[int64]map[string]float64{},
		tokens:  map[int64]map[string]billedTokenCounts{},
	}
	for _, report := range []struct {
		path    string
		groupBy string
	}{
		{"/organization/costs", "line_item"},
		{"/organization/usage/completions", "model"},
	} {
		nextPage := ""
		for pageCount := 0; pageCount < billingPageLimit; pageCount++ {
			query := url.Values{}
			query.Set("start_time", strconv.FormatInt(since.Unix(), 10))
			query.Set("bucket_width", "1d")
			query.Set("group_by", report.groupBy)
			query.Set("limit", "31")
			if nextPage != "" {
				query.Set("page", nextPage)
			}
			var page billingPage
			if err := getBillingPage(ctx, client, baseURL+report.path+"?"+query.Encode(), billingKey, &page); err != nil {
				return nil, err
			}
			billing.add(report.path == "/organization/costs", &page)
			if !page.HasMore || page.NextPage == "" {
				break
			}
			nextPage = page.NextPage
		}
	}
	return billing, nil
}

func getBillingPage(ctx context.Context, client *http.Client, pageURL, billingKey string, page *billingPage) error {
	ctx, cancel := context.WithTimeout(ctx, billingRequestTimeout)
	defer cancel()
	headers := map[string]string{"Authorization": "Bearer " + billingKey}
	err := doJSON(ctx, client, http.MethodGet, pageURL, headers, nil, page)
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden {
			return fmt.Errorf("llm: the billing key was refused: %s", apiErr.Message)
		}
		return fmt.Errorf("llm: reading the bill failed with %d: %s", apiErr.Status, apiErr.Message)
	}
	return err
}

func (self *Billing) add(isCosts bool, page *billingPage) {
	for _, bucket := range page.Data {
		for _, result := range bucket.Results {
			if isCosts {
				if result.LineItem == nil || *result.LineItem == "" {
					continue
				}
				if self.amounts[bucket.StartTime] == nil {
					self.amounts[bucket.StartTime] = map[string]float64{}
				}
				self.amounts[bucket.StartTime][*result.LineItem] += result.Amount.Value
				if self.Currency == "" {
					self.Currency = result.Amount.Currency
				}
				continue
			}
			if result.Model == "" {
				continue
			}
			if self.tokens[bucket.StartTime] == nil {
				self.tokens[bucket.StartTime] = map[string]billedTokenCounts{}
			}
			counts := self.tokens[bucket.StartTime][result.Model]
			counts.InputUncachedTokens += result.InputUncachedTokens
			counts.InputCachedTokens += result.InputCachedTokens
			counts.InputCacheWriteTokens += result.InputCacheWriteTokens
			counts.OutputTokens += result.OutputTokens
			counts.InputAudioTokens += result.InputAudioTokens
			counts.OutputAudioTokens += result.OutputAudioTokens
			counts.InputTextTokens += result.InputTextTokens
			counts.OutputTextTokens += result.OutputTextTokens
			self.tokens[bucket.StartTime][result.Model] = counts
		}
	}
}

// parsedLineItem is a cost line item such as "chat-model-small text,
// output" taken apart.
type parsedLineItem struct {
	Model string

	// Medium is "text", "audio" or empty when the line item names none.
	Medium string

	// Kind is the right-hand part: input, cached input, cache writes,
	// output, or something TeaNode has no price for. Empty when the line
	// item has no comma (an embedding model's bare name).
	Kind string
}

// parseLineItem splits at the last comma, because a model name never has
// one but the kind of a line item such as "cache writes - 12hr" may have
// further words after it.
func parseLineItem(lineItem string) parsedLineItem {
	parsed := parsedLineItem{}
	model := strings.TrimSpace(lineItem)
	if index := strings.LastIndex(lineItem, ","); index >= 0 {
		model = strings.TrimSpace(lineItem[:index])
		parsed.Kind = strings.TrimSpace(lineItem[index+1:])
	}
	for _, medium := range []string{"audio", "text"} {
		if strings.HasSuffix(model, " "+medium) {
			model = strings.TrimSpace(strings.TrimSuffix(model, medium))
			parsed.Medium = medium
			break
		}
	}
	parsed.Model = model
	return parsed
}

// tokenKindAndCount says which kind of token a line item bills and how many
// of those tokens the model used that day, or isPriced false for a line item with
// no price slot or no matching usage.
func (parsed parsedLineItem) tokenKindAndCount(counts billedTokenCounts) (kind string, tokens float64, isPriced bool) {
	switch parsed.Kind {
	case "input":
		switch parsed.Medium {
		case "audio":
			return BilledTokenKindAudioInput, counts.InputAudioTokens, true
		case "text":
			return BilledTokenKindInput, counts.InputTextTokens, true
		}
		return BilledTokenKindInput, counts.InputUncachedTokens, true
	case "cached input":
		return BilledTokenKindCachedInput, counts.InputCachedTokens, true
	case "cache writes":
		return BilledTokenKindCacheWrite, counts.InputCacheWriteTokens, true
	case "output":
		switch parsed.Medium {
		case "audio":
			return "", 0, false
		case "text":
			return BilledTokenKindOutput, counts.OutputTextTokens, true
		}
		return BilledTokenKindOutput, counts.OutputTokens, true
	}
	return "", 0, false
}

// Rates works out the price per million tokens of each model and kind of
// token that the bill shows.
//
// A day's amount divided by that day's tokens is not the list price: costs
// and token counts are bucketed by different clocks, which puts a busy day's
// rate a few percent under it, and a discounted day (free daily tokens) is
// lower still by however much was free. Both only lower a rate, so the price
// is the highest daily rate, rounded to two significant figures to shed the
// few percent (0.1978 becomes 0.2, 0.2469 becomes 0.25). The highest rate
// counts only when another day's rate is within five percent of it, so one
// odd bucket cannot set a price; otherwise the next highest is tried. The
// bill's amounts are not rounded to cents, so a small day gives a rate as
// exact as a busy one, which matters for a model whose busy days were all
// discounted. Line items with no price slot (audio output, the 12-hour
// cache writes) and models with no usage in the window yield nothing.
func (self *Billing) Rates() []BilledRate {
	lineItems := map[string]bool{}
	for _, amounts := range self.amounts {
		for lineItem := range amounts {
			lineItems[lineItem] = true
		}
	}
	rates := []BilledRate{}
	for lineItem := range lineItems {
		parsed := parseLineItem(lineItem)
		var kind string
		var dailyRates []float64
		billedAmount := 0.0
		for day, amounts := range self.amounts {
			amount, isBilled := amounts[lineItem]
			if !isBilled {
				continue
			}
			billedAmount += amount
			itemKind, tokens, isPriced := parsed.tokenKindAndCount(self.tokens[day][parsed.Model])
			if !isPriced {
				continue
			}
			kind = itemKind
			if tokens < billingMinimumDayTokens || amount <= 0 {
				continue
			}
			dailyRates = append(dailyRates, amount/tokens*1e6)
		}
		if kind == "" {
			continue
		}
		perMillion, dayCount := agreedHighestRate(dailyRates)
		if dayCount < billingMinimumDayCount {
			continue
		}
		rates = append(rates, BilledRate{
			Model:        parsed.Model,
			TokenKind:    kind,
			PerMillion:   perMillion,
			DayCount:     dayCount,
			BilledAmount: billedAmount,
		})
	}
	sort.Slice(rates, func(left, right int) bool {
		if rates[left].Model != rates[right].Model {
			return rates[left].Model < rates[right].Model
		}
		return rates[left].TokenKind < rates[right].TokenKind
	})
	return rates
}

// billingAgreement is how close another day's rate must be to the highest
// for the highest to count.
const billingAgreement = 0.05

// agreedHighestRate is the highest daily rate that at least one other day
// comes within five percent of, rounded to two significant figures, with
// the number of days that close to it.
func agreedHighestRate(dailyRates []float64) (float64, int) {
	sorted := slices.Clone(dailyRates)
	slices.SortFunc(sorted, func(left, right float64) int { return cmp.Compare(right, left) })
	for index, highest := range sorted {
		dayCount := 1
		for _, other := range sorted[index+1:] {
			if other >= highest*(1-billingAgreement) {
				dayCount++
			}
		}
		if dayCount >= billingMinimumDayCount {
			return roundToSignificant(highest, 2), dayCount
		}
	}
	return 0, 0
}

// DailyAmounts sums everything each model was billed per day, oldest day
// first and models in name order within a day.
func (self *Billing) DailyAmounts() []BilledDay {
	type dayModel struct {
		day   int64
		model string
	}
	sums := map[dayModel]float64{}
	for day, amounts := range self.amounts {
		for lineItem, amount := range amounts {
			sums[dayModel{day, parseLineItem(lineItem).Model}] += amount
		}
	}
	days := make([]BilledDay, 0, len(sums))
	for key, amount := range sums {
		days = append(days, BilledDay{
			Day:          time.Unix(key.day, 0).UTC(),
			Model:        key.model,
			BilledAmount: amount,
			Currency:     self.Currency,
		})
	}
	sort.Slice(days, func(left, right int) bool {
		if !days[left].Day.Equal(days[right].Day) {
			return days[left].Day.Before(days[right].Day)
		}
		return days[left].Model < days[right].Model
	})
	return days
}

// roundToSignificant rounds to the given number of significant figures.
// Formatting and parsing again is exact in decimal, where multiplying and
// dividing by a power of ten would leave 0.2 as 0.20000000000000001.
func roundToSignificant(value float64, digits int) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(value, 'g', digits, 64), 64)
	if err != nil {
		return value
	}
	return rounded
}
