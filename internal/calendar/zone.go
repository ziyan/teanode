package calendar

import (
	"fmt"
	"time"

	"github.com/emersion/go-ical"
)

// A time zone in an iCalendar file is not a name. It is a component carrying
// the offsets themselves and the moments they change, because a file has to
// mean the same thing on a machine whose zone database is older than the
// rules in it -- and because "Europe/London" means nothing at all to a client
// that keeps its own table under different names.
//
// So a file written here that anchors an event to a zone carries a VTIMEZONE
// describing that zone over the span the event covers. This matters most for
// something that recurs: "every Monday at ten" written as an offset from UTC
// is at nine o'clock for half the year, and the whole point of the zone is
// that it is ten all year.

// maximumObservances is how many changes of offset one written zone carries.
//
// Two a year is the usual number, so this is a century of them -- far past
// any event a person keeps, and a bound in case a zone's table is pathological
// or the span asked for is absurd.
const maximumObservances = 200

// zoneComponent describes loc over the span an event covers, as the component
// a file carries beside the event.
//
// Every change of offset in the span is written out as its own moment rather
// than as a rule that generates them. A rule would have to be inferred from
// the zone database and would be wrong the moment a government changed its
// mind, which they do; the moments themselves are what this machine's zone
// database actually says, and a client reading them does not have to agree
// with us about the rule to agree about the time.
func zoneComponent(loc *time.Location, from, until time.Time) (*ical.Component, error) {
	if loc == nil || loc == time.UTC {
		return nil, nil
	}
	name := loc.String()
	if name == "" || name == "UTC" || name == "Local" {
		// "Local" would mean whatever the server happens to be set to,
		// which is not a thing anybody chose and not a thing a phone can
		// resolve. An event with no zone of its own is written in UTC.
		return nil, nil
	}
	if !until.After(from) {
		until = from.Add(24 * time.Hour)
	}

	component := ical.NewComponent(ical.CompTimezone)
	component.Props.SetText(ical.PropTimezoneID, name)

	// The offset in force when the span opens, which is what an observance
	// before the first change describes.
	previousName, previousOffset := from.In(loc).Zone()
	started := from

	observances := 0
	// Bounded by steps rather than by what is written down. Not every step
	// writes an observance -- a zone may change only its abbreviation --
	// and a bound that only counts what was written is not a bound at all
	// on the steps that write nothing.
	for at, steps := from, 0; steps < maximumObservances*2; steps++ {
		_, ends := at.In(loc).ZoneBounds()
		if ends.IsZero() || !ends.Before(until) {
			break
		}
		// Past the end of what this machine's zone database says, the
		// bounds of the last stretch are the moment itself, which is not
		// an error and is easy to mistake for one: it means there is
		// nothing further to say, and a loop that does not notice spins on
		// the same moment for ever. A client reading a date past the end
		// of our table has a table of its own.
		if !ends.After(at) {
			break
		}
		nextName, nextOffset := ends.In(loc).Zone()
		if nextOffset == previousOffset {
			// Some zones change only their abbreviation. Nothing about
			// the time changed, so there is nothing to write.
			at = ends
			continue
		}
		if observances >= maximumObservances {
			break
		}
		// The observance begins at the moment of the change, written in
		// the offset that was in force before it -- which is what the
		// format means by the local time of an observance, and getting it
		// wrong moves every occurrence by an hour.
		component.Children = append(component.Children, observance(
			previousOffset, nextOffset, nextName, ends))
		observances++
		previousName, previousOffset = nextName, nextOffset
		started = ends
		at = ends
	}

	if len(component.Children) == 0 {
		// A zone that never changes over the span -- or one that never
		// changes at all, which is most of the world. One observance
		// saying what the offset is.
		component.Children = append(component.Children, observance(
			previousOffset, previousOffset, previousName, started))
	}
	if err := checkZone(component); err != nil {
		return nil, err
	}
	return component, nil
}

// observance is one stretch of a zone during which the offset does not change.
//
// STANDARD or DAYLIGHT by which side of the change has the larger offset,
// which is what daylight saving is: clocks forward. A client uses the offsets
// and not the label, but a file that called them the wrong way round would
// read as wrong to anybody looking at it.
func observance(fromOffset, toOffset int, name string, at time.Time) *ical.Component {
	kind := ical.CompTimezoneStandard
	if toOffset > fromOffset {
		kind = ical.CompTimezoneDaylight
	}
	child := ical.NewComponent(kind)
	// Written in the offset in force before the change: the wall clock
	// moment at which somebody standing there saw the clocks move.
	local := at.In(time.FixedZone("", fromOffset))
	setRaw(child, ical.PropDateTimeStart, local.Format("20060102T150405"))
	// Set directly rather than as text. An offset's own value type is
	// UTC-OFFSET, so writing it as text makes the encoder say VALUE=TEXT
	// out loud, and a client reading that is being told the offset is a
	// piece of prose rather than an offset.
	setRaw(child, ical.PropTimezoneOffsetFrom, offsetText(fromOffset))
	setRaw(child, ical.PropTimezoneOffsetTo, offsetText(toOffset))
	if name != "" {
		child.Props.SetText(ical.PropTimezoneName, name)
	}
	return child
}

// setRaw writes a property's value as it stands, leaving the format to read
// it as whatever that property's own type is.
func setRaw(component *ical.Component, name, value string) {
	property := ical.NewProp(name)
	property.Value = value
	component.Props.Set(property)
}

// offsetText writes an offset the way the format wants it: "+0100", "-0500",
// and the seconds too for the handful of zones whose offset is not a whole
// number of minutes.
func offsetText(offset int) string {
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	seconds := offset % 60
	if seconds != 0 {
		return fmt.Sprintf("%s%02d%02d%02d", sign, hours, minutes, seconds)
	}
	return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
}

// checkZone refuses a component that would not describe a zone.
//
// A VTIMEZONE that a client cannot read is worse than no zone at all: the
// event falls back to being read as floating or as UTC, and every occurrence
// moves. Better to find that here than on somebody's phone.
func checkZone(component *ical.Component) error {
	if component.Props.Get(ical.PropTimezoneID) == nil {
		return fmt.Errorf("calendar: a written zone must say which zone it is")
	}
	for _, child := range component.Children {
		if child.Props.Get(ical.PropDateTimeStart) == nil ||
			child.Props.Get(ical.PropTimezoneOffsetFrom) == nil ||
			child.Props.Get(ical.PropTimezoneOffsetTo) == nil {
			return fmt.Errorf("calendar: a written zone's observance must say when it begins and what the offset is")
		}
	}
	return nil
}
