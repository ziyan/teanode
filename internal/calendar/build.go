package calendar

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ziyan/teanode/internal/util/security"
)

// Fields are the boxes a form shows, as an instruction to change an event.
//
// Every one is a pointer, because leaving a box out and emptying it are
// different instructions. A caller that sends only a new title must not
// thereby delete the location and the people; a form whose box the person
// cleared must be able to clear what was there. Nil means leave it alone, and
// a pointer to an empty value means take it away.
type Fields struct {
	Summary     *string
	Location    *string
	Description *string

	// StartsAt and EndsAt are the moment it begins and ends. AllDay makes
	// it a date rather than a moment: a birthday belongs to the day
	// everywhere, and is written without a time or a zone so that it does
	// not move when it is read somewhere else.
	StartsAt *time.Time
	EndsAt   *time.Time
	AllDay   *bool

	// Timezone is the zone the times above are wall-clock times in, as an
	// IANA name. It is what makes "every Monday at ten" stay at ten when
	// the clocks change; empty writes the times in UTC.
	Timezone string

	// Recurrence is the rule, as the text of an RRULE without the property
	// name: "FREQ=WEEKLY;BYDAY=MO;COUNT=10". A pointer to an empty string
	// stops it recurring.
	Recurrence *string

	// Status is CONFIRMED, TENTATIVE or CANCELLED.
	Status *string
}

// Build writes an event: the fields applied to whatever is already kept.
//
// Applied to it, not instead of it. A phone puts things on an event that no
// form here shows -- an alarm, a conferencing link, travel time, a colour --
// and correcting a title in a browser must not throw those away. So the file
// that is already there is what is edited, and only what the fields name is
// touched.
func Build(previous []byte, fields *Fields) (*Parsed, error) {
	if fields == nil {
		fields = &Fields{}
	}
	var cal *ical.Calendar
	var event *ical.Event
	if len(bytes.TrimSpace(previous)) > 0 {
		decoded, err := ical.NewDecoder(bytes.NewReader(previous)).Decode()
		if err != nil {
			return nil, fmt.Errorf("calendar: what is already kept cannot be read: %w", err)
		}
		cal = decoded
		event = firstEvent(decoded)
	}
	if cal == nil {
		cal = ical.NewCalendar()
		cal.Props.SetText(ical.PropProductID, productID)
		cal.Props.SetText(ical.PropVersion, "2.0")
	}
	if event == nil {
		event = ical.NewEvent()
		event.Props.SetText(ical.PropUID, newUID())
		cal.Children = append(cal.Children, event.Component)
	}

	// DTSTAMP says when this version of the event was written down, and is
	// how a client and a mail recipient tell a newer copy from an older one.
	// It is always now: this is a new version by definition.
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	setText(event, ical.PropSummary, fields.Summary)
	setText(event, ical.PropLocation, fields.Location)
	setText(event, ical.PropDescription, fields.Description)
	if fields.Status != nil {
		status := strings.ToUpper(strings.TrimSpace(*fields.Status))
		switch status {
		case "", "CONFIRMED", "TENTATIVE", "CANCELLED":
		default:
			return nil, fmt.Errorf("calendar: an event is confirmed, tentative or cancelled, not %q", status)
		}
		setText(event, ical.PropStatus, &status)
	}

	// Before the times are written, because how long the event goes on
	// decides how much of its time zone has to be described beside it: an
	// event that repeats for years needs every change of offset in those
	// years, and one that happens once needs only the day it is on.
	if fields.Recurrence != nil {
		rule := strings.ToUpper(strings.TrimSpace(*fields.Recurrence))
		event.Props.Del(ical.PropRecurrenceRule)
		if rule != "" {
			property := ical.NewProp(ical.PropRecurrenceRule)
			property.Value = rule
			event.Props.Set(property)
			// Read it back before writing it down. A rule the server
			// cannot expand is an event that shows up nowhere, and a
			// person told so while they are typing can fix it; told
			// nothing, they have an appointment that silently is not
			// in their calendar.
			if _, err := event.Props.RecurrenceRule(); err != nil {
				return nil, fmt.Errorf("calendar: that repeat cannot be read: %w", err)
			}
		}
	}

	if err := setWhen(cal, event, fields); err != nil {
		return nil, err
	}
	// Checked after, not inside, because setWhen has nothing to do when the
	// caller said nothing about the time -- and a brand new event where
	// nobody said when it is would otherwise be written down as a file
	// describing an appointment at no time at all.
	if event.Props.Get(ical.PropDateTimeStart) == nil {
		return nil, fmt.Errorf("calendar: an event has to start somewhere")
	}

	written, err := Encode(cal)
	if err != nil {
		return nil, err
	}
	return Parse(written)
}

// productID names what wrote a file. It is what other calendar programs show
// when they say where an invitation came from.
const productID = "-//TeaNode//Calendar//EN"

// setWhen writes the start and the end, and the zone they are in.
//
// The three have to be decided together. Whether the event is all day decides
// whether a date or a moment is written; whether it has a zone decides
// whether a VTIMEZONE goes beside it; and how far the recurrence reaches
// decides how much of the zone that component has to describe.
func setWhen(cal *ical.Calendar, event *ical.Event, fields *Fields) error {
	if fields.StartsAt == nil && fields.EndsAt == nil && fields.AllDay == nil && fields.Timezone == "" {
		return nil
	}
	starts, err := event.DateTimeStart(time.UTC)
	if err != nil {
		starts = time.Time{}
	}
	ends, err := event.DateTimeEnd(time.UTC)
	if err != nil {
		ends = time.Time{}
	}
	allDay := false
	if existing := event.Props.Get(ical.PropDateTimeStart); existing != nil {
		allDay = existing.ValueType() == ical.ValueDate
	}
	if fields.StartsAt != nil {
		starts = *fields.StartsAt
	}
	if fields.EndsAt != nil {
		ends = *fields.EndsAt
	}
	if fields.AllDay != nil {
		allDay = *fields.AllDay
	}
	if starts.IsZero() {
		return fmt.Errorf("calendar: an event has to start somewhere")
	}
	if ends.IsZero() || ends.Before(starts) {
		// Nothing said, or something impossible said. An hour is what a
		// calendar means by an appointment, and a day by an all-day one.
		if allDay {
			ends = starts.Add(24 * time.Hour)
		} else {
			ends = starts.Add(time.Hour)
		}
	}

	// An event written afresh replaces whatever described its time before,
	// including a length given instead of an end -- leaving that behind
	// would have it argue with the end now being written.
	event.Props.Del(ical.PropDateTimeStart)
	event.Props.Del(ical.PropDateTimeEnd)
	event.Props.Del(ical.PropDuration)
	cal.Children = withoutZones(cal.Children)

	if allDay {
		// No zone, deliberately. A date is the same date everywhere, and
		// giving it one is how a birthday ends up on the ninth of
		// December for somebody reading it from further west.
		event.Props.SetDate(ical.PropDateTimeStart, starts.UTC())
		event.Props.SetDate(ical.PropDateTimeEnd, ends.UTC())
		return nil
	}

	if fields.Timezone == "" {
		event.Props.SetDateTime(ical.PropDateTimeStart, starts.UTC())
		event.Props.SetDateTime(ical.PropDateTimeEnd, ends.UTC())
		return nil
	}
	loc, err := time.LoadLocation(fields.Timezone)
	if err != nil {
		return fmt.Errorf("calendar: this server does not know the time zone %q: %w", fields.Timezone, err)
	}
	writeLocal(event, ical.PropDateTimeStart, starts.In(loc), fields.Timezone)
	writeLocal(event, ical.PropDateTimeEnd, ends.In(loc), fields.Timezone)

	// The zone has to be described for as long as the event goes on. A
	// recurrence with no end is described for a working lifetime, which is
	// as far as anybody's calendar is ever read and well inside the bound
	// on how many changes of offset are written.
	until := ends.Add(24 * time.Hour)
	if event.Props.Get(ical.PropRecurrenceRule) != nil {
		until = ends.AddDate(40, 0, 0)
	}
	zone, err := zoneComponent(loc, starts.Add(-24*time.Hour), until)
	if err != nil {
		return err
	}
	if zone != nil {
		// Before the event, as the format asks: a reader meets the zone
		// before it meets something anchored to it.
		cal.Children = append([]*ical.Component{zone}, cal.Children...)
	}
	return nil
}

// writeLocal writes a wall-clock time anchored to a named zone.
func writeLocal(event *ical.Event, name string, at time.Time, zone string) {
	property := ical.NewProp(name)
	property.SetValueType(ical.ValueDateTime)
	property.Params.Set(ical.ParamTimezoneID, zone)
	property.Value = at.Format("20060102T150405")
	event.Props.Set(property)
}

// withoutZones drops the zone components a file carries, so that the one
// written now is the only one. Leaving an old one behind would leave two
// components claiming the same identifier, and which of them a client
// believes is not something this server should be guessing at.
func withoutZones(children []*ical.Component) []*ical.Component {
	kept := make([]*ical.Component, 0, len(children))
	for _, child := range children {
		if child.Name == ical.CompTimezone {
			continue
		}
		kept = append(kept, child)
	}
	return kept
}

// setText writes a property, or takes it away when the instruction is to
// empty it.
func setText(event *ical.Event, name string, value *string) {
	if value == nil {
		return
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		event.Props.Del(name)
		return
	}
	event.Props.SetText(name, text)
}

// newUID names an event in a way that will not collide with one made
// anywhere else, since the same identifier in two places is the same event.
// The host part is what the format asks for and what other calendar programs
// show when they say where an invitation came from.
func newUID() string {
	return security.NewULID() + "@teanode"
}

// Answer applies what somebody said about coming to an event that is already
// kept.
//
// A reply carries the answering attendee's own line and nothing else worth
// keeping -- the summary and the times in it are the organizer's own words
// echoed back, and a program that trusted them would let an invitee rewrite
// the meeting by answering it. So exactly one thing is taken: the
// participation of an attendee the event already names.
//
// An answer from somebody who was never invited is refused rather than added.
// Adding them would let anybody who learns an event's identifier put
// themselves on the guest list by sending a message.
func Answer(previous []byte, answers []Attendee) (*Parsed, error) {
	if len(bytes.TrimSpace(previous)) == 0 {
		return nil, fmt.Errorf("calendar: there is no event to answer")
	}
	decoded, err := ical.NewDecoder(bytes.NewReader(previous)).Decode()
	if err != nil {
		return nil, fmt.Errorf("calendar: what is already kept cannot be read: %w", err)
	}
	event := firstEvent(decoded)
	if event == nil {
		return nil, fmt.Errorf("calendar: there is no event in that file")
	}
	changed := false
	for _, answer := range answers {
		address := strings.ToLower(strings.TrimSpace(answer.Address))
		if address == "" || answer.Participation == "" {
			continue
		}
		for index := range event.Props[ical.PropAttendee] {
			property := &event.Props[ical.PropAttendee][index]
			if !strings.EqualFold(addressOf(property), address) {
				continue
			}
			property.Params.Set(ical.ParamParticipationStatus, answer.Participation)
			// An answer settles it: the organizer no longer needs the
			// reminder that this person has not said.
			property.Params.Del("RSVP")
			changed = true
		}
	}
	if !changed {
		return nil, fmt.Errorf("calendar: that answer is from somebody this event does not invite")
	}
	written, err := Encode(decoded)
	if err != nil {
		return nil, err
	}
	return Parse(written)
}
