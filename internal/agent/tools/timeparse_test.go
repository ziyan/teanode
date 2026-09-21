package tools

import (
	"testing"
	"time"
)

func TestDatetimeParsing(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, berlin) // a Thursday
	for phrase, want := range map[string]string{
		"tomorrow 3pm":              "2026-09-11T15:00:00+02:00",
		"next monday":               "2026-09-14T00:00:00+02:00",
		"friday at 9:30":            "2026-09-11T09:30:00+02:00",
		"in 2d":                     "2026-09-12T10:00:00+02:00",
		"2026-10-01 08:00":          "2026-10-01T08:00:00+02:00",
		"today 12am":                "2026-09-10T00:00:00+02:00",
		"2026-12-24T18:00:00+02:00": "2026-12-24T18:00:00+02:00",
	} {
		moment, err := ParseTime(phrase, berlin, now)
		if err != nil {
			t.Fatalf("%q: %s", phrase, err)
		}
		if got := moment.Format(time.RFC3339); got != want {
			t.Fatalf("%q = %s, want %s", phrase, got, want)
		}
	}
	if duration, err := ParseDuration("2h30m"); err != nil || duration != 2*time.Hour+30*time.Minute {
		t.Fatalf("2h30m = %v %v", duration, err)
	}
	if duration, err := ParseDuration("-1w"); err != nil || duration != -7*24*time.Hour {
		t.Fatalf("-1w = %v %v", duration, err)
	}
	if _, err := ParsePhrase("whenever", berlin, now); err == nil {
		t.Fatal("nonsense should not parse")
	}
}
