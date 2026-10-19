package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// A reading is written with as many decimals as its size is worth, and a
// time the way the Health app writes it.
func TestHealthWords(t *testing.T) {
	for value, want := range map[float64]string{
		8412: "8,412", 10250.4: "10,250", 1234567: "1,234,567", 72.46: "72.46", 72.4: "72.4", 180.34: "180.3",
		0.35: "0.35", 0: "0", -1250: "-1,250", 999: "999", 100: "100",
	} {
		if got := HealthNumberWords(value); got != want {
			t.Errorf("HealthNumberWords(%v) = %q, want %q", value, got, want)
		}
	}
	for seconds, want := range map[float64]string{0: "0s", 45: "45s", 60: "1m", 2100: "35m", 3600: "1h", 25920: "7h 12m", 26100: "7h 15m", 89: "1m"} {
		if got := HealthDurationWords(seconds); got != want {
			t.Errorf("HealthDurationWords(%v) = %q, want %q", seconds, got, want)
		}
	}
	if HealthValueWords(72, "count/min") != "72 count/min" || HealthValueWords(72, "") != "72" {
		t.Error("a reading with its unit, when it has one")
	}
}

// The stages of a night come as an object or as a string holding one, and
// are listed with the longest first.
func TestHealthCategorySecondsAcceptsAnObjectOrAString(t *testing.T) {
	for _, encoded := range []string{
		`{"categorySeconds":{"asleepCore":12600,"awake":600,"asleepDeep":0}}`,
		`{"categorySeconds":"{\"asleepCore\":12600,\"awake\":600}"}`,
	} {
		var period HealthPeriodView
		if err := json.Unmarshal([]byte(encoded), &period); err != nil {
			t.Fatalf("%s: %s", encoded, err)
		}
		if got := period.CategorySeconds.Words(); got != "asleepCore 3h 30m, awake 10m" {
			t.Errorf("%s: %q", encoded, got)
		}
	}
	for _, encoded := range []string{`{}`, `{"categorySeconds":null}`, `{"categorySeconds":""}`, `{"categorySeconds":{}}`} {
		var period HealthPeriodView
		if err := json.Unmarshal([]byte(encoded), &period); err != nil {
			t.Fatalf("%s: %s", encoded, err)
		}
		if got := period.CategorySeconds.Words(); got != "" {
			t.Errorf("%s: %q", encoded, got)
		}
	}
	var period HealthPeriodView
	if err := json.Unmarshal([]byte(`{"categorySeconds":42}`), &period); err == nil {
		t.Error("a number is not stages")
	}
}

// Every document names its operation the way the plan does, and the ones that
// change something are mutations, so a read-only client refuses them.
func TestHealthDocumentsNameTheirOperations(t *testing.T) {
	for operation, document := range map[string]string{
		"ListHealthTypes": DocumentListHealthTypes, "ReadHealthSummary": DocumentReadHealthSummary, "ListHealthSamples": DocumentListHealthSamples,
		"ReadHealthRoute": DocumentReadHealthRoute, "ReadHealthSettings": DocumentReadHealthSettings, "UpdateHealthSettings": DocumentUpdateHealthSettings,
		"EraseHealthData": DocumentEraseHealthData, "ListHealthShares": DocumentListHealthShares, "SetHealthShare": DocumentSetHealthShare,
	} {
		isMutation := operation == "UpdateHealthSettings" || operation == "EraseHealthData" || operation == "SetHealthShare"
		if IsMutationDocument(document) != isMutation {
			t.Errorf("%s: mutation is %v", operation, !isMutation)
		}
		if !strings.Contains(document, operation+"(") && !strings.Contains(document, operation+" ") {
			t.Errorf("%s is not in its document", operation)
		}
	}
}

// A percentage is kept as a fraction, as HealthKit keeps it, and written as
// the percentage a person reads.
func TestHealthValueWordsWritesAFractionAsAPercentage(t *testing.T) {
	for _, testCase := range []struct {
		value float64
		unit  string
		want  string
	}{
		{0.97, "%", "97%"},
		{0.255, "%", "25.5%"},
		{72, "kg", "72 kg"},
		{8412, "", "8,412"},
	} {
		if got := HealthValueWords(testCase.value, testCase.unit); got != testCase.want {
			t.Errorf("HealthValueWords(%v, %q) = %q, want %q", testCase.value, testCase.unit, got, testCase.want)
		}
	}
}
