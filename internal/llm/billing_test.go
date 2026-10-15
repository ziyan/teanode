package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The invented bill: five days of a chat model whose input list price is
// 0.2 per million tokens but whose rate on the day varies (the first four
// are clock skew, the last a discounted day), plus a transcription model
// billed 6 per million audio tokens.
var (
	billingFirstDay      = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	billingInputRates    = []float64{0.1974, 0.1959, 0.1978, 0.164, 0.0003}
	billingCacheRates    = []float64{0.245, 0.2452, 0.2387, 0.2409, 0.2338}
	billingOutputRate    = 1.18
	billingAudioRate     = 6.0
	billingInputTokens   = 5_000_000.0
	billingCacheTokens   = 2_000_000.0
	billingOutputTokens  = 100_000.0
	billingAudioTokens   = 20_000.0
	billingSingleTokens  = 3_000_000.0
	billingSingleRate    = 0.5
	billingPageDayCounts = []int{3, 2}
)

func billingCostBucket(dayIndex int) map[string]any {
	start := billingFirstDay.AddDate(0, 0, dayIndex)
	amount := func(value float64) map[string]any {
		return map[string]any{"value": value, "currency": "usd"}
	}
	results := []map[string]any{
		{"line_item": "chat-model-small, input", "amount": amount(billingInputRates[dayIndex] * billingInputTokens / 1e6)},
		{"line_item": "chat-model-small, cache writes", "amount": amount(billingCacheRates[dayIndex] * billingCacheTokens / 1e6)},
		{"line_item": "chat-model-small, cache writes - 12hr", "amount": amount(0.01)},
		{"line_item": "chat-model-small, output", "amount": amount(billingOutputRate * billingOutputTokens / 1e6)},
		{"line_item": "transcribe-model audio, input", "amount": amount(billingAudioRate * billingAudioTokens / 1e6)},
		{"line_item": "transcribe-model audio, output", "amount": amount(0.05)},
		{"line_item": "text-embedding-3-small", "amount": amount(0.02)},
		{"line_item": nil, "amount": amount(0.03)},
	}
	singleTokens := 10.0
	if dayIndex == 0 {
		singleTokens = billingSingleTokens
	}
	results = append(results, map[string]any{
		"line_item": "single-day-model, input",
		"amount":    amount(billingSingleRate * singleTokens / 1e6),
	})
	return map[string]any{"start_time": start.Unix(), "end_time": start.AddDate(0, 0, 1).Unix(), "results": results}
}

func billingUsageBucket(dayIndex int) map[string]any {
	start := billingFirstDay.AddDate(0, 0, dayIndex)
	singleTokens := 10.0
	if dayIndex == 0 {
		singleTokens = billingSingleTokens
	}
	return map[string]any{
		"start_time": start.Unix(),
		"end_time":   start.AddDate(0, 0, 1).Unix(),
		"results": []map[string]any{
			{
				"model":                    "chat-model-small",
				"input_uncached_tokens":    billingInputTokens,
				"input_cache_write_tokens": billingCacheTokens,
				"output_tokens":            billingOutputTokens,
				"unknown_field":            "ignored",
			},
			{"model": "transcribe-model", "input_audio_tokens": billingAudioTokens, "output_audio_tokens": 500},
			{"model": "single-day-model", "input_uncached_tokens": singleTokens},
		},
	}
}

// fakeBilling serves both reports, each over two pages, and counts the
// requests so a test can see that both pages were read.
func fakeBilling(test *testing.T, requestCount *int) *httptest.Server {
	test.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		*requestCount++
		if request.Header.Get("Authorization") != "Bearer billing-key-1" {
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"error": "Missing scopes: api.usage.read"}`))
			return
		}
		query := request.URL.Query()
		if query.Get("bucket_width") != "1d" || query.Get("start_time") != "1000" || query.Get("limit") != "31" {
			test.Errorf("unexpected query %q", request.URL.RawQuery)
		}
		var bucket func(int) map[string]any
		switch request.URL.Path {
		case "/v1/organization/costs":
			bucket = billingCostBucket
			if query.Get("group_by") != "line_item" {
				test.Errorf("costs grouped by %q", query.Get("group_by"))
			}
		case "/v1/organization/usage/completions":
			bucket = billingUsageBucket
			if query.Get("group_by") != "model" {
				test.Errorf("usage grouped by %q", query.Get("group_by"))
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		firstIndex, lastIndex, nextPage := 0, billingPageDayCounts[0], "page-two"
		if query.Get("page") == "page-two" {
			firstIndex, lastIndex, nextPage = billingPageDayCounts[0], billingPageDayCounts[0]+billingPageDayCounts[1], ""
		}
		data := []map[string]any{}
		for index := firstIndex; index < lastIndex; index++ {
			data = append(data, bucket(index))
		}
		encoded, err := json.Marshal(map[string]any{"data": data, "has_more": nextPage != "", "next_page": nextPage})
		if err != nil {
			test.Error(err)
		}
		_, _ = writer.Write(encoded)
	}))
}

func TestBillingRates(test *testing.T) {
	requestCount := 0
	server := fakeBilling(test, &requestCount)
	defer server.Close()

	// A trailing slash on the base URL must not double up.
	billing, err := ReadBilling(context.Background(), server.Client(), server.URL+"/v1/", "billing-key-1", time.Unix(1000, 0))
	if err != nil {
		test.Fatal(err)
	}
	if requestCount != 4 {
		test.Errorf("expected 4 requests (2 reports, 2 pages each), got %d", requestCount)
	}
	if billing.Currency != "usd" {
		test.Errorf("currency %q", billing.Currency)
	}

	rates := billing.Rates()
	type expectation struct {
		model      string
		tokenKind  string
		perMillion float64
		dayCount   int
	}
	expected := []expectation{
		{"chat-model-small", BilledTokenKindCacheWrite, 0.25, 5},
		// The discounted days (0.164, 0.0003) do not lower the price.
		{"chat-model-small", BilledTokenKindInput, 0.2, 3},
		{"chat-model-small", BilledTokenKindOutput, 1.2, 5},
		{"transcribe-model", BilledTokenKindAudioInput, 6, 5},
	}
	if len(rates) != len(expected) {
		test.Fatalf("expected %d rates, got %+v", len(expected), rates)
	}
	for index, want := range expected {
		got := rates[index]
		if got.Model != want.model || got.TokenKind != want.tokenKind || got.PerMillion != want.perMillion || got.DayCount != want.dayCount {
			test.Errorf("rate %d: want %+v, got %+v", index, want, got)
		}
	}
	wantInputAmount := 0.0
	for _, rate := range billingInputRates {
		wantInputAmount += rate * billingInputTokens / 1e6
	}
	if math.Abs(rates[1].BilledAmount-wantInputAmount) > 1e-9 {
		test.Errorf("input billed amount %v, want %v", rates[1].BilledAmount, wantInputAmount)
	}
}

func TestBillingDailyAmounts(test *testing.T) {
	requestCount := 0
	server := fakeBilling(test, &requestCount)
	defer server.Close()
	billing, err := ReadBilling(context.Background(), server.Client(), server.URL+"/v1", "billing-key-1", time.Unix(1000, 0))
	if err != nil {
		test.Fatal(err)
	}
	days := billing.DailyAmounts()
	// Per day: chat-model-small, single-day-model, text-embedding-3-small,
	// transcribe-model.
	if len(days) != 5*4 {
		test.Fatalf("expected 20 model days, got %d", len(days))
	}
	first := days[0]
	wantChat := billingInputRates[0]*billingInputTokens/1e6 + billingCacheRates[0]*billingCacheTokens/1e6 + 0.01 + billingOutputRate*billingOutputTokens/1e6
	if first.Model != "chat-model-small" || !first.Day.Equal(billingFirstDay) || first.Currency != "usd" || math.Abs(first.BilledAmount-wantChat) > 1e-9 {
		test.Errorf("first model day %+v, want chat-model-small on %v with %v", first, billingFirstDay, wantChat)
	}
	transcribe := days[3]
	wantTranscribe := billingAudioRate*billingAudioTokens/1e6 + 0.05
	if transcribe.Model != "transcribe-model" || math.Abs(transcribe.BilledAmount-wantTranscribe) > 1e-9 {
		test.Errorf("transcription model day %+v, want %v", transcribe, wantTranscribe)
	}
	if !days[4].Day.Equal(billingFirstDay.AddDate(0, 0, 1)) {
		test.Errorf("model days are not ordered by day: %+v", days[4])
	}
}

func TestBillingRefusedKey(test *testing.T) {
	requestCount := 0
	server := fakeBilling(test, &requestCount)
	defer server.Close()
	_, err := ReadBilling(context.Background(), server.Client(), server.URL+"/v1", "sk-proj-wrong", time.Unix(1000, 0))
	if err == nil {
		test.Fatal("expected an error")
	}
	for _, want := range []string{"billing key was refused", "Missing scopes: api.usage.read"} {
		if !strings.Contains(err.Error(), want) {
			test.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestBillingRefusedKeyNestedMessage(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error": {"message": "Incorrect API key provided"}}`))
	}))
	defer server.Close()
	_, err := ReadBilling(context.Background(), server.Client(), server.URL, "key", time.Unix(1000, 0))
	if err == nil || !strings.Contains(err.Error(), "Incorrect API key provided") {
		test.Errorf("unexpected error %v", err)
	}
}

func TestParseLineItem(test *testing.T) {
	cases := map[string]parsedLineItem{
		"chat-model-small, input":               {"chat-model-small", "", "input"},
		"chat-model-small text, output":         {"chat-model-small", "text", "output"},
		"transcribe-model audio, input":         {"transcribe-model", "audio", "input"},
		"chat-model-small, cache writes - 12hr": {"chat-model-small", "", "cache writes - 12hr"},
		"text-embedding-3-small":                {"text-embedding-3-small", "", ""},
	}
	for lineItem, want := range cases {
		if got := parseLineItem(lineItem); got != want {
			test.Errorf("%q: want %+v, got %+v", lineItem, want, got)
		}
	}
}

func TestRoundToSignificant(test *testing.T) {
	cases := []struct{ value, want float64 }{
		{0.1978, 0.2},
		{0.2452, 0.25},
		{1.186, 1.2},
		{6.0, 6},
		{0.0193, 0.019},
		{12.4, 12},
		{0, 0},
	}
	for _, testCase := range cases {
		if got := roundToSignificant(testCase.value, 2); got != testCase.want {
			test.Errorf("roundToSignificant(%v, 2) = %v, want %v", testCase.value, got, testCase.want)
		}
	}
}

// A model whose busy days were all discounted is priced from its small days
// billed in full, a single odd high day sets nothing, and rates that never
// come close to one another give no price.
func TestTheHighestRateAnotherDayAgreesWith(test *testing.T) {
	for _, testCase := range []struct {
		name           string
		dailyRates     []float64
		wantPerMillion float64
		wantDayCount   int
	}{
		{"busy days discounted", []float64{0.115, 0.147, 0.309, 0.985, 1.639, 1.97, 2.0, 2.0, 2.0}, 2, 4},
		{"one odd high day", []float64{5.0, 2.0, 1.99, 0.5}, 2, 2},
		{"busy days a little under the list price", []float64{0.2469, 0.2452, 0.2441, 0.2409, 0.2387, 0.2338}, 0.25, 5},
		{"no two days close", []float64{3.0, 1.0}, 0, 0},
		{"one day", []float64{2.0}, 0, 0},
	} {
		perMillion, dayCount := agreedHighestRate(testCase.dailyRates)
		if perMillion != testCase.wantPerMillion || dayCount != testCase.wantDayCount {
			test.Errorf("%s: got %v over %d days, want %v over %d", testCase.name, perMillion, dayCount, testCase.wantPerMillion, testCase.wantDayCount)
		}
	}
}
