// Package journal is a person's journal: what happened on a day, read from
// each source by the day's time range, and the day's numbers. It calls no
// model; the agent's writing of narratives and touch-ups is in
// internal/agent. docs/planning/journal-execplan.md has the design.
package journal

import (
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// DayLayout is how a day is written.
const DayLayout = "2006-01-02"

// Zone is the zone a person's new days are cut in: their own, or the
// server's when they have none.
func Zone(user *models.User) *time.Location {
	if user != nil && user.Timezone != "" {
		if location, err := time.LoadLocation(user.Timezone); err == nil {
			return location
		}
	}
	return time.Local
}

// ZoneOfDay is the zone a stored day was cut in, or the person's zone for a
// day with no row (timeZone empty) or a zone this server does not know.
func ZoneOfDay(user *models.User, timeZone string) *time.Location {
	if timeZone != "" {
		if location, err := time.LoadLocation(timeZone); err == nil {
			return location
		}
	}
	return Zone(user)
}

// Today is the person's calendar date now.
func Today(user *models.User, now time.Time) string {
	return now.In(Zone(user)).Format(DayLayout)
}

// ParseDay reads a day written YYYY-MM-DD.
func ParseDay(day string) (time.Time, error) {
	parsed, err := time.Parse(DayLayout, day)
	if err != nil {
		return time.Time{}, fmt.Errorf("journal: a day is written YYYY-MM-DD, not %q", day)
	}
	return parsed, nil
}

// DayRange is the instants a day covers in a zone: from its first moment,
// included, to the next day's first, excluded. A day a clock change makes
// 23 or 25 hours long is exactly that long.
func DayRange(day string, location *time.Location) (from, until time.Time, err error) {
	parsed, err := ParseDay(day)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, location)
	until = time.Date(parsed.Year(), parsed.Month(), parsed.Day()+1, 0, 0, 0, 0, location)
	return from, until, nil
}

// AddDays is the day count days after day (before it for a negative count).
func AddDays(day string, count int) (string, error) {
	parsed, err := ParseDay(day)
	if err != nil {
		return "", err
	}
	return parsed.AddDate(0, 0, count).Format(DayLayout), nil
}

// Weekday is a day's weekday, lowercase English: monday to sunday.
func Weekday(day string) (string, error) {
	parsed, err := ParseDay(day)
	if err != nil {
		return "", err
	}
	return weekdayNames[parsed.Weekday()], nil
}

var weekdayNames = map[time.Weekday]string{
	time.Sunday: "sunday", time.Monday: "monday", time.Tuesday: "tuesday", time.Wednesday: "wednesday",
	time.Thursday: "thursday", time.Friday: "friday", time.Saturday: "saturday",
}

// PeriodContaining is the period of a kind that contains a day: its first
// and last day (both included). A week starts on weekStart, "sunday" or
// "monday" (models.WeekStartsSunday, models.WeekStartsMonday); anything else
// is Monday.
func PeriodContaining(periodKind models.JournalPeriodKind, day, weekStart string) (first, last string, err error) {
	parsed, err := ParseDay(day)
	if err != nil {
		return "", "", err
	}
	var start, end time.Time
	switch periodKind {
	case models.JournalPeriodWeek:
		firstWeekday := time.Monday
		if weekStart == models.WeekStartsSunday {
			firstWeekday = time.Sunday
		}
		back := (int(parsed.Weekday()) - int(firstWeekday) + 7) % 7
		start = parsed.AddDate(0, 0, -back)
		end = start.AddDate(0, 0, 6)
	case models.JournalPeriodMonth:
		start = time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, -1)
	case models.JournalPeriodYear:
		start = time.Date(parsed.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		end = time.Date(parsed.Year(), time.December, 31, 0, 0, 0, 0, time.UTC)
	default:
		return "", "", fmt.Errorf("journal: a period is a week, a month or a year, not %q", periodKind)
	}
	return start.Format(DayLayout), end.Format(DayLayout), nil
}

// IsBeyondDepth says a day is older than the depth dreams write narratives
// back to on their own (models.JournalNarrativeDepthDays before today).
func IsBeyondDepth(day, today string) bool {
	limit, err := AddDays(today, -models.JournalNarrativeDepthDays)
	if err != nil {
		return false
	}
	return day < limit
}
