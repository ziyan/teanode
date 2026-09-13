package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// Reading a zone the machine has never heard of.
//
// A file names its zone with a TZID and carries a VTIMEZONE describing it.
// The library resolves the name with time.LoadLocation and ignores the
// description entirely -- which works for "Europe/Berlin" and fails for
// everything Microsoft writes, because Exchange and Outlook name zones their
// own way: "W. Europe Standard Time", "Pacific Standard Time". Those are the
// commonest invitations there are.
//
// Failing there is quiet and bad: the moment comes back as the zero time, the
// event is stored starting in the year one, and it then appears in no window
// anybody ever asks about -- not the calendar, not free-busy, not a phone.
//
// So when the name means nothing, the description beside it is used. It is
// there in the file, it is what the sender meant, and it is what every other
// calendar program falls back to.

// zoneFromFile is the zone a file describes under this name, or nil.
//
// The observance in force at a given moment is the one whose own start is the
// latest that is not after it. That is what the format says, and for the
// ordinary file -- one STANDARD and one DAYLIGHT, each with a yearly rule --
// it is enough to get the offset right without expanding those rules: the
// pair differ only in which half of the year they cover, so the question is
// which of the two the date falls in.
func zoneFromFile(cal *ical.Calendar, tzid string, at time.Time) *time.Location {
	wanted := strings.TrimSpace(tzid)
	if cal == nil || wanted == "" {
		return nil
	}
	for _, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			continue
		}
		name, err := child.Props.Text(ical.PropTimezoneID)
		if err != nil || !strings.EqualFold(strings.TrimSpace(name), wanted) {
			continue
		}
		if offset, found := offsetIn(child, at); found {
			// Named with the file's own name rather than with an offset,
			// so anything written back out says what the sender said.
			return time.FixedZone(wanted, offset)
		}
	}
	return nil
}

// offsetIn is the offset an observance gives at a moment, in seconds.
func offsetIn(zone *ical.Component, at time.Time) (int, bool) {
	best, bestAt, found := 0, time.Time{}, false
	for _, observance := range zone.Children {
		if observance.Name != ical.CompTimezoneStandard && observance.Name != ical.CompTimezoneDaylight {
			continue
		}
		offset, ok := offsetOf(observance.Props.Get(ical.PropTimezoneOffsetTo))
		if !ok {
			continue
		}
		// The observance's own start, which is a local time with no zone.
		// Compared against the event's local wall clock, which is the only
		// comparison the format defines here.
		start := observance.Props.Get(ical.PropDateTimeStart)
		if start == nil {
			continue
		}
		began, err := time.ParseInLocation("20060102T150405", strings.TrimSpace(start.Value), time.UTC)
		if err != nil {
			continue
		}
		// Which half of the year: an observance with a yearly rule repeats,
		// so only the month and day it begins on matter for choosing
		// between the two. Shifted to the event's own year.
		began = time.Date(at.Year(), began.Month(), began.Day(),
			began.Hour(), began.Minute(), began.Second(), 0, time.UTC)
		if began.After(at) {
			// It has not come round yet this year, so the one in force is
			// last year's turn of the same observance.
			began = began.AddDate(-1, 0, 0)
		}
		if !found || began.After(bestAt) {
			best, bestAt, found = offset, began, true
		}
	}
	return best, found
}

// offsetOf reads "+0100" or "-053000" as seconds.
func offsetOf(property *ical.Prop) (int, bool) {
	if property == nil {
		return 0, false
	}
	value := strings.TrimSpace(property.Value)
	if len(value) < 5 {
		return 0, false
	}
	sign := 1
	switch value[0] {
	case '+':
	case '-':
		sign = -1
	default:
		return 0, false
	}
	digits := value[1:]
	for _, letter := range digits {
		if letter < '0' || letter > '9' {
			return 0, false
		}
	}
	if len(digits) != 4 && len(digits) != 6 {
		return 0, false
	}
	hours := int(digits[0]-'0')*10 + int(digits[1]-'0')
	minutes := int(digits[2]-'0')*10 + int(digits[3]-'0')
	seconds := 0
	if len(digits) == 6 {
		seconds = int(digits[4]-'0')*10 + int(digits[5]-'0')
	}
	return sign * (hours*3600 + minutes*60 + seconds), true
}

// momentOf reads a date-time property, resolving the zone the way the library
// does except that a TZID it cannot resolve falls back to the description in
// the file rather than failing.
//
// The library returns an error there, and the caller then had a zero time it
// could not tell from a missing one. Everything else -- a value ending in Z,
// a plain date, a name this machine knows -- is left to the library so there
// is one implementation of the ordinary case.
func momentOf(cal *ical.Calendar, property *ical.Prop, fallback *time.Location) (time.Time, error) {
	if property == nil {
		return time.Time{}, nil
	}
	tzid := strings.TrimSpace(property.Params.Get(ical.ParamTimezoneID))
	if tzid == "" {
		return property.DateTime(fallback)
	}
	if _, err := time.LoadLocation(tzid); err == nil {
		return property.DateTime(fallback)
	}
	// A name this machine does not know. The value is a local time; the
	// file says what the offset is.
	value := strings.TrimSpace(property.Value)
	local, err := time.ParseInLocation("20060102T150405", value, time.UTC)
	if err != nil {
		// Not a date-time at all: a date, which has no zone anyway.
		return property.DateTime(fallback)
	}
	where := zoneFromFile(cal, tzid, local)
	if where == nil {
		return time.Time{}, fmt.Errorf("calendar: this server does not know the time zone %q, and the event does not describe it", tzid)
	}
	return time.ParseInLocation("20060102T150405", value, where)
}

// lengthOf is how long an event lasts, from its end or from its duration, the
// way the library works it out -- but reading the end with momentOf so an
// unknown zone does not lose it.
func lengthOf(cal *ical.Calendar, event *ical.Event, starts time.Time, fallback *time.Location) (time.Time, error) {
	if end := event.Props.Get(ical.PropDateTimeEnd); end != nil {
		return momentOf(cal, end, fallback)
	}
	if duration := event.Props.Get(ical.PropDuration); duration != nil {
		length, err := duration.Duration()
		if err != nil {
			return time.Time{}, err
		}
		return starts.Add(length), nil
	}
	if start := event.Props.Get(ical.PropDateTimeStart); start != nil && start.ValueType() == ical.ValueDate {
		// A whole day, which is what a date with no end means.
		return starts.Add(24 * time.Hour), nil
	}
	return starts, nil
}

// wallClockCopy is the event with its zone names taken off the properties that
// decide a recurrence, so the rule can be expanded as wall-clock times.
//
// The library resolves a TZID with LoadLocation and fails on a name this
// machine does not know, which is every name Microsoft writes -- so a
// recurring invitation from Exchange could not be expanded at all. Stripped
// of the name, the same properties parse as the local times they are, and the
// caller puts the offset back one occurrence at a time. Doing it that way
// round is what keeps "ten o'clock every Monday" at ten through a change of
// offset: the rule works in wall clock, which is what it means.
func wallClockCopy(event *ical.Event) *ical.Event {
	copied := ical.NewEvent()
	for name, properties := range event.Props {
		kept := make([]ical.Prop, 0, len(properties))
		for _, property := range properties {
			one := property
			one.Params = ical.Params{}
			for key, values := range property.Params {
				if strings.EqualFold(key, ical.ParamTimezoneID) {
					continue
				}
				one.Params[key] = append([]string(nil), values...)
			}
			kept = append(kept, one)
		}
		copied.Props[name] = kept
	}
	return copied
}

// atOffsetIn is the moment a wall-clock time names in the zone a file
// describes: the same clock face, with the offset that zone had on that date.
func atOffsetIn(cal *ical.Calendar, tzid string, wall time.Time) time.Time {
	where := zoneFromFile(cal, tzid, wall)
	if where == nil {
		return wall
	}
	return time.Date(wall.Year(), wall.Month(), wall.Day(),
		wall.Hour(), wall.Minute(), wall.Second(), 0, where)
}

// describedZone is the zone name a file uses that this machine cannot resolve,
// or empty when there is no such problem to work around.
func describedZone(event *ical.Event) string {
	start := event.Props.Get(ical.PropDateTimeStart)
	if start == nil {
		return ""
	}
	tzid := strings.TrimSpace(start.Params.Get(ical.ParamTimezoneID))
	if tzid == "" {
		return ""
	}
	if _, err := time.LoadLocation(tzid); err == nil {
		return ""
	}
	return tzid
}
