package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// Times as a person writes them and as a tool answers them: the person's
// zone, a date and time in several shapes, a duration, a phrase like
// "next Tuesday 3pm". Shared by the datetime tool and by every tool that
// takes a since or a before.

// Location is the zone a person's times are told in: their own, or the
// server's when they have none yet.
func Location(user *models.User) *time.Location {
	if user != nil && user.Timezone != "" {
		if location, err := time.LoadLocation(user.Timezone); err == nil {
			return location
		}
	}
	return time.Local
}

// ParseTime reads an ISO time, or a date, or a date and a time, in the
// given zone; empty is now.
func ParseTime(value string, location *time.Location, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "now") {
		return now, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02", "15:04"} {
		if moment, err := time.ParseInLocation(layout, value, location); err == nil {
			if layout == "15:04" {
				local := now.In(location)
				moment = time.Date(local.Year(), local.Month(), local.Day(), moment.Hour(), moment.Minute(), 0, 0, location)
			}
			return moment, nil
		}
	}
	return ParsePhrase(value, location, now)
}

var durationPattern = regexp.MustCompile(`^\s*(-?)(\d+)\s*(w|d|h|m|s)\s*`)

// ParseDuration reads 3d, 2h30m, -1w and Go's own forms.
func ParseDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, fmt.Errorf("no duration")
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return duration, nil
	}
	rest := value
	var total time.Duration
	negative := strings.HasPrefix(rest, "-")
	rest = strings.TrimPrefix(rest, "-")
	for rest != "" {
		match := durationPattern.FindStringSubmatch(rest)
		if match == nil {
			return 0, fmt.Errorf("%q is not a duration; write 3d, 2h30m or -1w", value)
		}
		amount, _ := strconv.Atoi(match[2])
		unit := map[string]time.Duration{"w": 7 * 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "s": time.Second}[match[3]]
		total += time.Duration(amount) * unit
		rest = rest[len(match[0]):]
	}
	if negative {
		total = -total
	}
	return total, nil
}

func DurationWords(difference time.Duration) string {
	if difference < 0 {
		return "-" + DurationWords(-difference)
	}
	days := int(difference.Hours()) / 24
	hours := int(difference.Hours()) % 24
	minutes := int(difference.Minutes()) % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day(s)", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour(s)", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d minute(s)", minutes))
	}
	return strings.Join(parts, " ")
}

var clockPattern = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)

// ParsePhrase reads the phrases people write: today, tomorrow, yesterday,
// next Monday, in 3 days, 3pm, and a clock time after any of them.
func ParsePhrase(text string, location *time.Location, now time.Time) (time.Time, error) {
	phrase := strings.ToLower(strings.TrimSpace(text))
	if phrase == "" {
		return time.Time{}, fmt.Errorf("nothing to parse")
	}
	local := now.In(location)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	rest := phrase
	switch {
	case strings.HasPrefix(rest, "today"):
		rest = strings.TrimPrefix(rest, "today")
	case strings.HasPrefix(rest, "tomorrow"):
		day = day.AddDate(0, 0, 1)
		rest = strings.TrimPrefix(rest, "tomorrow")
	case strings.HasPrefix(rest, "yesterday"):
		day = day.AddDate(0, 0, -1)
		rest = strings.TrimPrefix(rest, "yesterday")
	case strings.HasPrefix(rest, "in "):
		duration, err := ParseDuration(strings.TrimSpace(strings.TrimPrefix(rest, "in ")))
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(duration), nil
	default:
		for name, weekday := range map[string]time.Weekday{"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday} {
			for _, prefix := range []string{"next " + name, "this " + name, name} {
				if strings.HasPrefix(rest, prefix) {
					ahead := (int(weekday) - int(day.Weekday()) + 7) % 7
					if ahead == 0 || strings.HasPrefix(prefix, "next ") && ahead == 0 {
						ahead = 7
					}
					day = day.AddDate(0, 0, ahead)
					rest = strings.TrimPrefix(rest, prefix)
					goto clock
				}
			}
		}
		if moment, err := time.ParseInLocation("2006-01-02", rest[:min(10, len(rest))], location); err == nil {
			day = moment
			rest = rest[10:]
		} else {
			return time.Time{}, fmt.Errorf("cannot read %q; write an ISO date, today, tomorrow, next Monday, or in 3 days", text)
		}
	}
clock:
	rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "at"))
	if rest == "" {
		return day, nil
	}
	match := clockPattern.FindStringSubmatch(rest)
	if match == nil {
		return time.Time{}, fmt.Errorf("cannot read the time in %q", text)
	}
	hour, _ := strconv.Atoi(match[1])
	minute := 0
	if match[2] != "" {
		minute, _ = strconv.Atoi(match[2])
	}
	if strings.EqualFold(match[3], "pm") && hour < 12 {
		hour += 12
	}
	if strings.EqualFold(match[3], "am") && hour == 12 {
		hour = 0
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, location), nil
}
