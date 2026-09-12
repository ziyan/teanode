package agent

import (
	"testing"
	"time"
)

// A cron line is read in the person's zone: every weekday at eight is
// eight where they are, whatever the server's clock says.
func TestNextCron(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	after := time.Date(2026, 9, 10, 10, 0, 0, 0, berlin) // Thursday
	for expression, want := range map[string]string{
		"0 8 * * 1-5":   "2026-09-11T08:00:00+02:00",
		"30 18 * * *":   "2026-09-10T18:30:00+02:00",
		"0 9 * * 1":     "2026-09-14T09:00:00+02:00",
		"*/15 * * * *":  "2026-09-10T10:15:00+02:00",
		"0 0 1 * *":     "2026-09-30T22:00:00Z", // the first of October, midnight in Berlin
		"0 7 * * 0,6":   "2026-09-12T07:00:00+02:00",
		"0 7 * * 7":     "2026-09-13T07:00:00+02:00",
		"0 12 25 12 *":  "2026-12-25T12:00:00+01:00",
		"15 8,17 * * *": "2026-09-10T17:15:00+02:00",
	} {
		next, err := nextCron(expression, after, berlin)
		if err != nil {
			t.Fatalf("%q: %s", expression, err)
		}
		if got := next.Format(time.RFC3339); got != want && next.UTC().Format(time.RFC3339) != want {
			t.Fatalf("%q = %s, want %s", expression, got, want)
		}
	}
	for _, bad := range []string{"", "0 8 * *", "60 8 * * *", "0 25 * * *", "a b c d e", "0 8 32 * *"} {
		if _, err := nextCron(bad, after, berlin); err == nil {
			t.Fatalf("%q should not parse", bad)
		}
	}
}
