package journal

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func TestADayIsCutInItsZone(test *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		test.Skip("no zone database")
	}
	from, until, err := DayRange("2026-10-22", location)
	if err != nil {
		test.Fatal(err)
	}
	if got := from.UTC().Format(time.RFC3339); got != "2026-10-22T04:00:00Z" {
		test.Fatalf("the day starts at %s", got)
	}
	if hours := until.Sub(from).Hours(); hours != 24 {
		test.Fatalf("the day is %v hours long", hours)
	}
	// The autumn clock change makes the first Sunday of November 25 hours.
	from, until, _ = DayRange("2026-11-01", location)
	if hours := until.Sub(from).Hours(); hours != 25 {
		test.Fatalf("the day of the clock change is %v hours long, want 25", hours)
	}
}

func TestAPeriodContainsItsDay(test *testing.T) {
	cases := []struct {
		periodKind  models.JournalPeriodKind
		day         string
		weekStart   string
		first, last string
	}{
		{models.JournalPeriodWeek, "2026-10-22", models.WeekStartsMonday, "2026-10-19", "2026-10-25"},
		{models.JournalPeriodWeek, "2026-10-22", models.WeekStartsSunday, "2026-10-18", "2026-10-24"},
		{models.JournalPeriodWeek, "2026-10-19", "", "2026-10-19", "2026-10-25"},
		{models.JournalPeriodMonth, "2026-02-14", "", "2026-02-01", "2026-02-28"},
		{models.JournalPeriodYear, "2026-10-22", "", "2026-01-01", "2026-12-31"},
	}
	for _, testCase := range cases {
		first, last, err := PeriodContaining(testCase.periodKind, testCase.day, testCase.weekStart)
		if err != nil || first != testCase.first || last != testCase.last {
			test.Errorf("%s of %s starting %q: %s to %s (%v), want %s to %s", testCase.periodKind, testCase.day, testCase.weekStart, first, last, err, testCase.first, testCase.last)
		}
	}
	if _, _, err := PeriodContaining("fortnight", "2026-10-22", ""); err == nil {
		test.Fatal("a fortnight was accepted")
	}
}

func TestADayBeyondTheDepthIsNotWrittenOnItsOwn(test *testing.T) {
	if IsBeyondDepth("2026-10-01", "2026-10-26") {
		test.Fatal("a day of this month is beyond the depth")
	}
	if !IsBeyondDepth("2026-06-01", "2026-10-26") {
		test.Fatal("a day five months back is within the depth")
	}
}
