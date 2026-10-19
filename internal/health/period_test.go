package health

import (
	"testing"
)

// A week starts on Monday, a month on its first, and each ends on the day
// before the next begins.
func TestAPeriodStartsAndEndsOnTheCalendar(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		period     Period
		day        string
		start, end string
	}{
		// 2026-01-04 is a Sunday, the last day of a week that began on the 29th.
		{PeriodWeek, "2026-01-04", "2025-12-29", "2026-01-04"},
		{PeriodWeek, "2026-01-05", "2026-01-05", "2026-01-11"},
		{PeriodWeek, "2026-01-07", "2026-01-05", "2026-01-11"},
		{PeriodWeek, "2026-03-01", "2026-02-23", "2026-03-01"},
		{PeriodMonth, "2026-02-14", "2026-02-01", "2026-02-28"},
		{PeriodMonth, "2028-02-29", "2028-02-01", "2028-02-29"},
		{PeriodMonth, "2026-12-31", "2026-12-01", "2026-12-31"},
		{PeriodDay, "2026-06-15", "2026-06-15", "2026-06-15"},
	} {
		day, err := ParseDay(test.day)
		if err != nil {
			t.Fatal(err)
		}
		if got := test.period.Start(day).Format(DayLayout); got != test.start {
			t.Errorf("a %s holding %s starts on %s, want %s", test.period, test.day, got, test.start)
		}
		if got := test.period.End(day).Format(DayLayout); got != test.end {
			t.Errorf("a %s holding %s ends on %s, want %s", test.period, test.day, got, test.end)
		}
	}
}

func TestCountingThePeriodsOfARange(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		period   Period
		from, to string
		want     int
	}{
		{PeriodDay, "2026-01-01", "2026-01-01", 1},
		{PeriodDay, "2026-01-01", "2026-01-31", 31},
		{PeriodDay, "2025-12-31", "2026-01-01", 2},
		{PeriodWeek, "2026-01-05", "2026-01-11", 1},
		{PeriodWeek, "2026-01-04", "2026-01-05", 2},
		{PeriodWeek, "2025-12-29", "2026-01-26", 5},
		{PeriodMonth, "2026-01-31", "2026-02-01", 2},
		{PeriodMonth, "2025-11-15", "2026-02-10", 4},
		{PeriodMonth, "2026-03-01", "2026-03-31", 1},
		{PeriodDay, "2026-01-02", "2026-01-01", 0},
	} {
		from, err := ParseDay(test.from)
		if err != nil {
			t.Fatal(err)
		}
		to, err := ParseDay(test.to)
		if err != nil {
			t.Fatal(err)
		}
		if got := test.period.Count(from, to); got != test.want {
			t.Errorf("%s from %s to %s: %d periods, want %d", test.period, test.from, test.to, got, test.want)
		}
	}
}

func TestOnlyARealDayWrittenInFullIsADay(t *testing.T) {
	t.Parallel()

	for _, good := range []string{"2026-01-31", "2028-02-29", "0001-01-01"} {
		if _, err := ParseDay(good); err != nil {
			t.Errorf("%q is a day: %s", good, err)
		}
	}
	for _, bad := range []string{"", "2026-02-30", "2027-02-29", "2026-1-5", "2026-01-05T00:00:00Z", "01-05-2026", "2026/01/05", " 2026-01-05", "2026-13-01", "today"} {
		if _, err := ParseDay(bad); err == nil {
			t.Errorf("%q is not a day", bad)
		}
	}
	for _, period := range []Period{PeriodDay, PeriodWeek, PeriodMonth} {
		if !period.IsValid() {
			t.Errorf("%s is a period", period)
		}
	}
	for _, period := range []Period{"", "year", "Day"} {
		if period.IsValid() {
			t.Errorf("%q is not a period", period)
		}
	}
}
