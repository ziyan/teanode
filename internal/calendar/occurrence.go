package calendar

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// One occurrence of a series, changed on its own.
//
// A person who moves next Tuesday's standup out of the way has not touched
// the standup. The format says so by keeping both in one file: the series,
// and beside it a second VEVENT with the same UID and a RECURRENCE-ID naming
// the occurrence it replaces -- an override. A cancellation of one occurrence
// is the same idea from the other end: the date is excluded from the series.
//
// What arrives by mail is only the part that changed, so acting on it means
// putting that part back beside what is already held. Taking the message as
// the whole event is how somebody's weekly meeting became a single
// appointment and the other nineteen weeks disappeared -- from the calendar,
// the phone, free-busy, and everything that reads the index.

// Occurrence is the RECURRENCE-ID property a file carries, which names the
// occurrence it is about -- the property itself rather than the moment,
// because an exception date written from it has to keep the zone and the
// value type it was written with.
func (self *Parsed) Occurrence() *ical.Prop {
	if self == nil || self.calendar == nil {
		return nil
	}
	event := firstEvent(self.calendar)
	if event == nil {
		return nil
	}
	return event.Props.Get(ical.PropRecurrenceID)
}

// MergeOccurrence puts a changed occurrence beside the series it belongs to,
// replacing any earlier version of that same occurrence, and gives back the
// whole file as it should now be stored.
func MergeOccurrence(stored, arriving *Parsed) ([]byte, error) {
	if stored == nil || stored.calendar == nil || arriving == nil || arriving.calendar == nil {
		return nil, fmt.Errorf("calendar: there is nothing to merge")
	}
	changed := firstEvent(arriving.calendar)
	if changed == nil || changed.Props.Get(ical.PropRecurrenceID) == nil {
		return nil, fmt.Errorf("calendar: that file is not about one occurrence")
	}
	series := firstEvent(stored.calendar)
	if series == nil || series.Props.Get(ical.PropRecurrenceID) != nil {
		// Nothing to hang it on. An occurrence of a series this server
		// does not hold is not something to guess about.
		return nil, fmt.Errorf("calendar: there is no series here for that occurrence")
	}
	wanted := occurrenceKey(changed.Props.Get(ical.PropRecurrenceID))
	kept := make([]*ical.Component, 0, len(stored.calendar.Children)+1)
	for _, child := range stored.calendar.Children {
		if child == nil {
			continue
		}
		if child.Name == ical.CompEvent && occurrenceKey(child.Props.Get(ical.PropRecurrenceID)) == wanted && wanted != "" {
			// The earlier version of this same occurrence.
			continue
		}
		kept = append(kept, child)
	}
	// The zones the arriving occurrence names have to come with it, or the
	// times in it mean something else once it is stored beside a series
	// that never used them.
	for _, child := range arriving.calendar.Children {
		if child == nil || child.Name != ical.CompTimezone {
			continue
		}
		if !namesZone(kept, zoneName(child)) {
			kept = append(kept, child)
		}
	}
	kept = append(kept, changed.Component)
	merged := &ical.Calendar{Component: stored.calendar.Component}
	merged.Children = kept
	return Encode(merged)
}

// ExcludeOccurrence takes one occurrence out of a series: any version of it
// kept separately goes, and the date is written into the series as an
// exception, which is what the format has for "this one is not happening".
func ExcludeOccurrence(stored *Parsed, recurrenceID *ical.Prop) ([]byte, error) {
	if stored == nil || stored.calendar == nil {
		return nil, fmt.Errorf("calendar: there is nothing to change")
	}
	if recurrenceID == nil {
		return nil, fmt.Errorf("calendar: that file does not say which occurrence")
	}
	series := firstEvent(stored.calendar)
	if series == nil || series.Props.Get(ical.PropRecurrenceID) != nil {
		return nil, fmt.Errorf("calendar: there is no series here for that occurrence")
	}
	wanted := occurrenceKey(recurrenceID)
	kept := make([]*ical.Component, 0, len(stored.calendar.Children))
	for _, child := range stored.calendar.Children {
		if child == nil {
			continue
		}
		if child.Name == ical.CompEvent && wanted != "" &&
			occurrenceKey(child.Props.Get(ical.PropRecurrenceID)) == wanted {
			continue
		}
		kept = append(kept, child)
	}
	excluded := *recurrenceID
	excluded.Name = ical.PropExceptionDates
	// RANGE says "this one and every one after it", which is a different
	// request and not one carried by an exception date.
	excluded.Params = cloneParams(recurrenceID.Params)
	excluded.Params.Del("RANGE")
	for _, already := range series.Props[ical.PropExceptionDates] {
		if occurrenceKey(&already) == wanted { //nolint:gosec,exportloopref
			// Already excluded: a cancellation arriving twice is one
			// cancellation.
			changed := &ical.Calendar{Component: stored.calendar.Component}
			changed.Children = kept
			return Encode(changed)
		}
	}
	series.Props.Add(&excluded)
	changed := &ical.Calendar{Component: stored.calendar.Component}
	changed.Children = kept
	return Encode(changed)
}

// AnswerOccurrence records an attendee's answer against one occurrence of a
// series rather than against the series itself.
//
// Only where that occurrence is already kept apart. An answer about an
// occurrence nobody has moved has nowhere of its own to go, and writing it on
// the series would say the person had answered for every week.
func AnswerOccurrence(previous []byte, recurrenceID string, answers []Attendee) (*Parsed, error) {
	decoded, err := ical.NewDecoder(strings.NewReader(string(previous))).Decode()
	if err != nil {
		return nil, fmt.Errorf("calendar: what is already kept cannot be read: %w", err)
	}
	wanted := strings.TrimSpace(recurrenceID)
	var found *ical.Component
	for _, child := range decoded.Children {
		if child == nil || child.Name != ical.CompEvent {
			continue
		}
		if occurrenceKey(child.Props.Get(ical.PropRecurrenceID)) == wanted && wanted != "" {
			found = child
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("calendar: that answer is about an occurrence this server does not keep apart")
	}
	if !answerInto(found, answers) {
		return nil, fmt.Errorf("calendar: that answer is from somebody this event does not invite")
	}
	written, err := Encode(decoded)
	if err != nil {
		return nil, err
	}
	return Parse(written)
}

// overrides are the occurrences kept apart from the series, by the moment
// each one replaces.
func overrides(cal *ical.Calendar) map[string]*ical.Component {
	found := map[string]*ical.Component{}
	if cal == nil {
		return found
	}
	for _, child := range cal.Children {
		if child == nil || child.Name != ical.CompEvent {
			continue
		}
		if key := occurrenceKey(child.Props.Get(ical.PropRecurrenceID)); key != "" {
			found[key] = child
		}
	}
	return found
}

// occurrenceKey is a RECURRENCE-ID or an EXDATE as a moment, written the one
// way, so that two spellings of the same instant are the same occurrence.
func occurrenceKey(property *ical.Prop) string {
	if property == nil {
		return ""
	}
	at, err := property.DateTime(time.UTC)
	if err != nil {
		return strings.TrimSpace(property.Value)
	}
	return at.UTC().Format(time.RFC3339)
}

func zoneName(component *ical.Component) string {
	if component == nil {
		return ""
	}
	if property := component.Props.Get(ical.PropTimezoneID); property != nil {
		return strings.TrimSpace(property.Value)
	}
	return ""
}

func namesZone(components []*ical.Component, name string) bool {
	if name == "" {
		return true
	}
	for _, child := range components {
		if child != nil && child.Name == ical.CompTimezone && zoneName(child) == name {
			return true
		}
	}
	return false
}

func cloneParams(params ical.Params) ical.Params {
	copied := ical.Params{}
	for name, values := range params {
		copied[name] = append([]string(nil), values...)
	}
	return copied
}
