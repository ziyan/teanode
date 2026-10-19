package health

import (
	"fmt"
	"time"
)

// DayLayout is how a day is written: 2026-01-31.
const DayLayout = "2006-01-02"

// ParseDay reads a day written YYYY-MM-DD, and nothing else: no time, no
// other order, no day that is not on the calendar.
func ParseDay(text string) (time.Time, error) {
	day, err := time.Parse(DayLayout, text)
	if err != nil || day.Format(DayLayout) != text {
		return time.Time{}, fmt.Errorf("%q is not a day written YYYY-MM-DD", text)
	}
	return day, nil
}

// Period is how many days a bar of a chart covers.
type Period string

// The periods.
const (
	PeriodDay Period = "day"
	// PeriodWeek is a week that starts on Monday.
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
)

// IsValid reports whether this is one of the periods.
func (self Period) IsValid() bool {
	switch self {
	case PeriodDay, PeriodWeek, PeriodMonth:
		return true
	}
	return false
}

// Start is the first day of the period that holds the day, which is a day of
// the calendar and has no time of day.
func (self Period) Start(day time.Time) time.Time {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	switch self {
	case PeriodWeek:
		// Sunday is 0 in Go, and the week starts the day after it.
		return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	case PeriodMonth:
		return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return day
}

// End is the last day of the period that holds the day.
func (self Period) End(day time.Time) time.Time {
	start := self.Start(day)
	switch self {
	case PeriodWeek:
		return start.AddDate(0, 0, 6)
	case PeriodMonth:
		return start.AddDate(0, 1, -1)
	}
	return start
}

// Count is how many periods run from the one that holds the first day to the
// one that holds the last, both included. Zero when the last day is before
// the first.
func (self Period) Count(fromDay, toDay time.Time) int {
	if toDay.Before(fromDay) {
		return 0
	}
	first, last := self.Start(fromDay), self.Start(toDay)
	switch self {
	case PeriodWeek:
		return int(last.Sub(first).Hours()/24/7) + 1
	case PeriodMonth:
		return (last.Year()-first.Year())*12 + int(last.Month()) - int(first.Month()) + 1
	}
	return int(last.Sub(first).Hours()/24) + 1
}
