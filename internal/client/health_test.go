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
	for seconds, want := range map[float64]string{0: "0s", 45: "45s", 59.6: "1m", 60: "1m", 2100: "35m", 3600: "1h", 25920: "7h 12m", 26100: "7h 15m", 89: "1m"} {
		if got := HealthDurationWords(seconds); got != want {
			t.Errorf("HealthDurationWords(%v) = %q, want %q", seconds, got, want)
		}
	}
	if HealthValueWords("heartRate", 72, "count/min") != "72 bpm" || HealthValueWords("bodyMass", 72, "") != "72" {
		t.Error("a reading with its unit, when it has one")
	}
}

// The stages of a night come as an object or as a string holding one, and
// are listed from the deepest up, in the words of the dashboard.
func TestHealthCategorySecondsAcceptsAnObjectOrAString(t *testing.T) {
	for _, encoded := range []string{
		`{"categorySeconds":{"asleepCore":12600,"awake":600,"asleepDeep":0}}`,
		`{"categorySeconds":"{\"asleepCore\":12600,\"awake\":600}"}`,
	} {
		var period HealthPeriodView
		if err := json.Unmarshal([]byte(encoded), &period); err != nil {
			t.Fatalf("%s: %s", encoded, err)
		}
		if got := period.CategorySeconds.Words(); got != "Core 3h 30m, Awake 10m" {
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
		if got := HealthValueWords("", testCase.value, testCase.unit); got != testCase.want {
			t.Errorf("HealthValueWords(%v, %q) = %q, want %q", testCase.value, testCase.unit, got, testCase.want)
		}
	}
}

// A week of sleep is told as the average night, with the total, the nights it
// is averaged over and the stages of an average night; a week of steps as the
// total and the average day over the days that have data; a level as its
// average and range with the unit's word.
func TestHealthPeriodWordsTellAWeekAsTheAverageDay(t *testing.T) {
	number := func(value float64) *float64 { return &value }
	sleep := &HealthPeriodView{
		StartDay: "2026-03-02", EndDay: "2026-03-08", DurationSeconds: number(5 * 7 * 3600), SampleCount: 40, DayCount: 5,
		CategorySeconds: HealthCategorySeconds{"asleep": 5 * 7 * 3600, "asleepDeep": 5 * 3600, "asleepCore": 15 * 3600, "asleepREM": 7.5 * 3600, "awake": 5 * 1200},
	}
	want := "2026-03-02 to 2026-03-08: asleep average 7h a night over 5 nights, total 35h (a night: Deep 1h, Core 3h, REM 1h 30m, Awake 20m); 40 readings"
	if got := HealthPeriodWords(sleep, "sleepAnalysis", ""); got != want {
		t.Errorf("a week of sleep:\n got %q\nwant %q", got, want)
	}
	night := &HealthPeriodView{
		StartDay: "2026-03-02", EndDay: "2026-03-02", DurationSeconds: number(27000), SampleCount: 6, DayCount: 1,
		CategorySeconds: HealthCategorySeconds{"asleep": 25200, "asleepCore": 12600, "awake": 600},
	}
	want = "2026-03-02: asleep 7h (Core 3h 30m, Awake 10m); 6 readings"
	if got := HealthPeriodWords(night, "sleepAnalysis", ""); got != want {
		t.Errorf("a night of sleep:\n got %q\nwant %q", got, want)
	}
	steps := &HealthPeriodView{StartDay: "2026-03-01", EndDay: "2026-03-31", SumValue: number(120000), SampleCount: 900, DayCount: 12}
	want = "2026-03-01 to 2026-03-31: total 120,000 steps; average 10,000 steps a day over 12 days; 900 readings"
	if got := HealthPeriodWords(steps, "stepCount", "count"); got != want {
		t.Errorf("a month of steps:\n got %q\nwant %q", got, want)
	}
	// A server that does not say how many days a period holds is told as it was.
	steps.DayCount = 0
	want = "2026-03-01 to 2026-03-31: total 120,000 steps; 900 readings"
	if got := HealthPeriodWords(steps, "stepCount", "count"); got != want {
		t.Errorf("a month of steps from an older server:\n got %q\nwant %q", got, want)
	}
	heart := &HealthPeriodView{StartDay: "2026-03-02", EndDay: "2026-03-02", AverageValue: number(75.86), MinimumValue: number(48), MaximumValue: number(151.2), SampleCount: 300, DayCount: 1}
	want = "2026-03-02: average 76 bpm (lowest 48 bpm, highest 151 bpm); 300 readings"
	if got := HealthPeriodWords(heart, "heartRate", "count/min"); got != want {
		t.Errorf("a day of heart rate:\n got %q\nwant %q", got, want)
	}
	oxygen := &HealthPeriodView{StartDay: "2026-03-02", EndDay: "2026-03-02", AverageValue: number(0.97), MinimumValue: number(0.95), MaximumValue: number(0.99), SampleCount: 3, DayCount: 1}
	want = "2026-03-02: average 97% (lowest 95%, highest 99%); 3 readings"
	if got := HealthPeriodWords(oxygen, "oxygenSaturation", "%"); got != want {
		t.Errorf("a percentage keeps its sign in the range:\n got %q\nwant %q", got, want)
	}
	if got := HealthPeriodWords(&HealthPeriodView{StartDay: "2026-03-02", EndDay: "2026-03-02"}, "stepCount", "count"); got != "2026-03-02: no readings" {
		t.Errorf("a day with nothing: %q", got)
	}
}
