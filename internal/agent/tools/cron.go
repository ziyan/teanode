package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// When a schedule comes round: a cron line of five fields in the person's
// zone, a single moment written "@at 2026-09-12 09:00", or a distance from
// now written "@in 20m". Shared by the schedule tool and the worker that
// queues what is due.

type cronField struct {
	values map[int]bool
	any    bool
}

func (self cronField) admits(value int) bool {
	return self.any || self.values[value]
}

// parseCron reads five fields — minute hour day month weekday — with *,
// lists, ranges and steps; weekday 0 and 7 are both Sunday.
func parseCron(expression string) ([5]cronField, error) {
	var fields [5]cronField
	parts := strings.Fields(strings.TrimSpace(expression))
	if len(parts) != 5 {
		return fields, fmt.Errorf("a schedule is five fields: minute hour day month weekday")
	}
	bounds := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for index, part := range parts {
		field := cronField{values: map[int]bool{}}
		for _, piece := range strings.Split(part, ",") {
			step := 1
			if slash := strings.Index(piece, "/"); slash >= 0 {
				parsed, err := strconv.Atoi(piece[slash+1:])
				if err != nil || parsed <= 0 {
					return fields, fmt.Errorf("%q is not a step", piece)
				}
				step = parsed
				piece = piece[:slash]
			}
			low, high := bounds[index][0], bounds[index][1]
			switch {
			case piece == "*":
				if step == 1 {
					field.any = true
				}
			case strings.Contains(piece, "-"):
				ends := strings.SplitN(piece, "-", 2)
				var err error
				if low, err = strconv.Atoi(ends[0]); err != nil {
					return fields, fmt.Errorf("%q is not a range", piece)
				}
				if high, err = strconv.Atoi(ends[1]); err != nil {
					return fields, fmt.Errorf("%q is not a range", piece)
				}
			default:
				value, err := strconv.Atoi(piece)
				if err != nil {
					return fields, fmt.Errorf("%q is not a number", piece)
				}
				low, high = value, value
			}
			if low < bounds[index][0] || high > bounds[index][1] || low > high {
				return fields, fmt.Errorf("%q is out of range", piece)
			}
			for value := low; value <= high; value += step {
				field.values[value] = true
			}
		}
		if index == 4 {
			if field.values[7] {
				field.values[0] = true
			}
		}
		fields[index] = field
	}
	return fields, nil
}

// NextCron is the first moment after the given one that a cron line
// admits, in the zone, or zero when none is found in four years.
func NextCron(expression string, after time.Time, location *time.Location) (time.Time, error) {
	// A schedule for one moment: "@at 2026-09-12 09:00", in the person's
	// zone. It comes once, and a schedule with no next time turns itself
	// off after it.
	if moment, ok := strings.CutPrefix(strings.TrimSpace(expression), "@at "); ok {
		at, err := ParseMoment(strings.TrimSpace(moment), location)
		if err != nil {
			return time.Time{}, err
		}
		if !at.After(after) {
			return time.Time{}, fmt.Errorf("the moment has passed")
		}
		return at, nil
	}
	fields, err := parseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	candidate := after.In(location).Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(4, 0, 0)
	for candidate.Before(limit) {
		if !fields[3].admits(int(candidate.Month())) {
			candidate = time.Date(candidate.Year(), candidate.Month()+1, 1, 0, 0, 0, 0, location)
			continue
		}
		if !fields[2].admits(candidate.Day()) || !fields[4].admits(int(candidate.Weekday())) {
			candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day()+1, 0, 0, 0, 0, location)
			continue
		}
		if !fields[1].admits(candidate.Hour()) {
			candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day(), candidate.Hour()+1, 0, 0, 0, location)
			continue
		}
		if !fields[0].admits(candidate.Minute()) {
			candidate = candidate.Add(time.Minute)
			continue
		}
		return candidate, nil
	}
	return time.Time{}, fmt.Errorf("the schedule never comes round")
}

// relativePattern is "@in 5m", "@in 2 hours", "@in 1 day": a moment said
// from now, which is how a person asks to be reminded.
var relativePattern = regexp.MustCompile(`^@in\s+(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)$`)

// ResolveRelative turns "@in 5m" into the "@at" moment it means from now,
// so the schedule stores a moment and not a distance that would move with
// every look. Anything else is returned as it came.
func ResolveRelative(expression string, now time.Time, location *time.Location) (string, error) {
	trimmed := strings.TrimSpace(expression)
	if !strings.HasPrefix(trimmed, "@in ") {
		return expression, nil
	}
	var distance time.Duration
	if match := relativePattern.FindStringSubmatch(trimmed); match != nil {
		count, _ := strconv.Atoi(match[1])
		switch match[2][0] {
		case 'm':
			distance = time.Duration(count) * time.Minute
		case 'h':
			distance = time.Duration(count) * time.Hour
		default:
			distance = time.Duration(count) * 24 * time.Hour
		}
	} else if parsed, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(trimmed, "@in "))); err == nil {
		distance = parsed
	} else {
		return "", fmt.Errorf("a distance from now is written @in 5m, @in 2h or @in 1 day")
	}
	if distance < time.Minute {
		return "", fmt.Errorf("the soonest is @in 1m")
	}
	return "@at " + now.Add(distance).In(location).Format("2006-01-02 15:04"), nil
}

// ParseMoment reads a date and time the way a person writes one, in their
// zone unless the text carries its own offset.
func ParseMoment(text string, location *time.Location) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return at, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if at, err := time.ParseInLocation(layout, text, location); err == nil {
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("a moment is written 2026-09-12 09:00")
}
