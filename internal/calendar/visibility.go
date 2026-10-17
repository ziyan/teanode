package calendar

import (
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ziyan/teanode/internal/models"
)

// What somebody who is not the owner is shown of an event.
//
// A calendar is shared at a level (docs/decisions/
// 20261016-a-calendar-is-shared-at-a-level.md), and the level decides how much
// of each event's file the reader is given. Redact is the one function that
// decides it, and the database function that lists or fetches an event for a
// reader who is not its owner is the one place that calls it, so a door that
// forgets cannot hand out more than the level allows.

// BusyOnlyProperty marks an event that was reduced to when it is. It is how
// a screen tells "Busy" the label from an event somebody really called Busy,
// and how a reader's own copy says it is not the whole event. This server's
// own: anything that does not know the property ignores it.
const BusyOnlyProperty = "X-TEANODE-BUSY"

// busyLabel is the title a reader is given for time that is only busy.
// Screens translate it; a phone shows it as it is.
const busyLabel = "Busy"

// busyProperties are what an event keeps when the reader may know only that
// the owner is busy: when it is, how it repeats, which repeats were moved or
// called off, and whether it makes the owner busy at all (STATUS and TRANSP are
// two of the four judgments). Nothing about what it is.
var busyProperties = map[string]bool{
	ical.PropUID:             true,
	ical.PropDateTimeStamp:   true,
	ical.PropSequence:        true,
	ical.PropDateTimeStart:   true,
	ical.PropDateTimeEnd:     true,
	ical.PropDuration:        true,
	ical.PropRecurrenceRule:  true,
	ical.PropRecurrenceDates: true,
	ical.PropExceptionDates:  true,
	ical.PropRecurrenceID:    true,
	ical.PropTransparency:    true,
	ical.PropStatus:          true,
}

// titleProperties add what the reader of titles is told: the title and the
// place. The description, the guests, the organizer, links and attachments
// are details.
var titleProperties = func() map[string]bool {
	kept := map[string]bool{ical.PropSummary: true, ical.PropLocation: true}
	for name := range busyProperties {
		kept[name] = true
	}
	return kept
}()

// calendarProperties are the properties of the VCALENDAR itself that survive a
// redaction. Anything else, such as a calendar's name or its description, is
// the owner's.
var calendarProperties = map[string]bool{
	ical.PropVersion:       true,
	ical.PropProductID:     true,
	ical.PropCalendarScale: true,
}

// Redact is an event as a reader at the level may see it.
//
// busy returns when it is and nothing else, titled "Busy". titles adds the
// title and the place. details returns the whole event except its alarms, which
// belong to the owner's phone and would otherwise ring on the reader's. edit and
// the owner's own get the event as it is, so that a phone that edits it writes
// back everything it was given.
//
// An event its owner marked private (CLASS:PRIVATE or CLASS:CONFIDENTIAL) is
// busy to everybody but the owner, an editor included, and so is a file this
// server cannot read at any level below edit: what cannot be read cannot be
// shown safely. An editor is not the owner, so an editor is shown a private
// event as busy and the write path refuses to change or delete it (see
// IsPrivateFile). A calendar a circle owns has no owner to keep an event
// private from, so the database does not call Redact for it at all.
//
// The columns beside the file are redacted with it, because they are what a
// listing reads, and the ETag is taken over what is given, because it names a
// version of the bytes the reader holds.
func Redact(object models.CalendarObject, shareLevel models.ShareLevel) models.CalendarObject {
	if shareLevel == models.ShareLevelOwner {
		return object
	}
	decoded, err := ical.NewDecoder(strings.NewReader(object.Data)).Decode()
	if err != nil {
		if shareLevel == models.ShareLevelEdit {
			// An editor of a file nobody can read is let write it back,
			// which is how it gets repaired.
			return object
		}
		return busyOnly(object, nil)
	}
	if shareLevel == models.ShareLevelEdit {
		if fileIsPrivate(decoded) {
			return busyOnly(object, decoded)
		}
		return object
	}
	if fileIsPrivate(decoded) || shareLevel.Rank() < models.ShareLevelTitles.Rank() {
		return busyOnly(object, decoded)
	}
	if shareLevel == models.ShareLevelTitles {
		return rewritten(object, decoded, titleProperties, false)
	}
	// details: the whole of it, less the alarms.
	return rewritten(object, decoded, nil, false)
}

// IsPrivateFile reports whether any event in an iCalendar file is marked
// private. The write path asks it of the stored file before a person who is
// not the owner changes or deletes the event: what they may see only as busy
// they may not change, since a change would be made blind and would write
// back the busy copy over the real event.
func IsPrivateFile(data string) bool {
	decoded, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	return err == nil && fileIsPrivate(decoded)
}

// fileIsPrivate reports whether any event in the file is marked private. One
// is enough: a series and the occurrences moved out of it are one file, and a
// reader who may not read one of them must not read the rest of it either.
func fileIsPrivate(decoded *ical.Calendar) bool {
	for _, child := range decoded.Children {
		if child == nil || child.Name != ical.CompEvent {
			continue
		}
		if class := child.Props.Get(ical.PropClass); class != nil && IsPrivateClass(class.Value) {
			return true
		}
	}
	return false
}

// busyOnly is the event reduced to when it is. decoded may be nil for a file
// that could not be read, in which case the times come from the columns.
func busyOnly(object models.CalendarObject, decoded *ical.Calendar) models.CalendarObject {
	var rebuilt []byte
	if decoded != nil {
		rebuilt = rewrite(decoded, busyProperties, true)
	}
	if rebuilt == nil {
		rebuilt = busyFromColumns(object)
	}
	object.Data = string(rebuilt)
	object.ETag = ETag(rebuilt)
	object.Summary = busyLabel
	object.Location = ""
	return object
}

// rewritten is the event with the properties that are not allowed taken out.
// A nil allowed keeps every property.
func rewritten(object models.CalendarObject, decoded *ical.Calendar, allowed map[string]bool, isBusy bool) models.CalendarObject {
	rebuilt := rewrite(decoded, allowed, isBusy)
	if rebuilt == nil {
		return busyOnly(object, nil)
	}
	object.Data = string(rebuilt)
	object.ETag = ETag(rebuilt)
	return object
}

// rewrite keeps the events of a file, each with the allowed properties, and
// the time zones they refer to. With isBusy every event is titled "Busy".
// It returns nil when the result cannot be written.
func rewrite(decoded *ical.Calendar, allowed map[string]bool, isBusy bool) []byte {
	kept := ical.NewCalendar()
	for name, properties := range decoded.Props {
		if allowed != nil && !calendarProperties[name] {
			continue
		}
		kept.Props[name] = properties
	}
	for _, child := range decoded.Children {
		if child == nil {
			continue
		}
		switch child.Name {
		case ical.CompTimezone:
			kept.Children = append(kept.Children, child)
		case ical.CompEvent:
			event := ical.NewComponent(ical.CompEvent)
			for name, properties := range child.Props {
				if allowed != nil && !allowed[name] {
					continue
				}
				event.Props[name] = properties
			}
			if allowed == nil {
				// details: everything but the alarms.
				for _, grandchild := range child.Children {
					if grandchild != nil && grandchild.Name != ical.CompAlarm {
						event.Children = append(event.Children, grandchild)
					}
				}
			}
			if isBusy {
				event.Props.SetText(ical.PropSummary, busyLabel)
				event.Props.Set(busyMark())
			}
			kept.Children = append(kept.Children, event)
		}
	}
	encoded, err := Encode(kept)
	if err != nil {
		return nil
	}
	return encoded
}

// busyFromColumns writes the smallest event that says when: from the columns
// beside a file that could not be read, which are all that is known of it.
func busyFromColumns(object models.CalendarObject) []byte {
	if object.StartsAt.IsZero() {
		return nil
	}
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, strings.NewReplacer("\r", "", "\n", "").Replace(object.UID))
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	if object.AllDay {
		event.Props.SetDate(ical.PropDateTimeStart, object.StartsAt.UTC())
		if object.EndsAt.After(object.StartsAt) {
			event.Props.SetDate(ical.PropDateTimeEnd, object.EndsAt.UTC())
		}
	} else {
		event.Props.SetDateTime(ical.PropDateTimeStart, object.StartsAt.UTC())
		if object.EndsAt.After(object.StartsAt) {
			event.Props.SetDateTime(ical.PropDateTimeEnd, object.EndsAt.UTC())
		}
	}
	event.Props.SetText(ical.PropSummary, busyLabel)
	event.Props.Set(busyMark())
	kept := ical.NewCalendar()
	kept.Props.SetText(ical.PropVersion, "2.0")
	kept.Props.SetText(ical.PropProductID, productID)
	kept.Children = append(kept.Children, event.Component)
	encoded, err := Encode(kept)
	if err != nil {
		return nil
	}
	return encoded
}

// busyMark is the property that marks an event reduced to when it is. Written
// bare, without the VALUE parameter the library adds to a text, since a
// reader that does not know the property has no use for the noise.
func busyMark() *ical.Prop {
	mark := ical.NewProp(BusyOnlyProperty)
	mark.Value = "TRUE"
	return mark
}
