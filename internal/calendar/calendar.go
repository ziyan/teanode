// Package calendar knows the iCalendar format, so that nothing else has to.
//
// An iCalendar file is how an appointment is written down: lines such as
// "SUMMARY:Weekly sync" between BEGIN:VEVENT and END:VEVENT, wrapped in a
// BEGIN:VCALENDAR. It is what a phone sends over CalDAV and what it is given
// back, and this server keeps it as the event itself rather than as one
// rendering of a set of columns. That way a property this server has never
// heard of -- and calendar programs invent them freely, from travel time to
// conferencing links -- survives a round trip through it untouched.
//
// Unlike the vCard side of this server, the library's own encoder is used as
// it is: it was measured against the cases that destroy a vCard, including
// parameters in quotes, and it returns them unchanged. The one thing it does
// not do is fold long lines, which is done here.
package calendar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// MaximumObject is how large one event may be. An event is text and a few
// hundred bytes in the ordinary case; the limit is here because a file may
// carry a long description, an attachment inline, or years of overridden
// occurrences, and a client that decides to send a ten megabyte one should be
// told no rather than have it kept.
const MaximumObject = 1 << 20

// ErrTooLarge is a file bigger than this server keeps. Named, because the
// answer a client is given for it is a different one -- a status that makes
// it stop resending rather than retry for ever -- and deciding that by
// looking at the words in a message means rewording the message changes the
// protocol.
var ErrTooLarge = errors.New("calendar: that event is larger than this server keeps")

// Attendee is one person asked to an event, as the file records them.
type Attendee struct {
	// Address is the "mailto:" the ATTENDEE line names, without the scheme.
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`

	// Participation is their PARTSTAT: NEEDS-ACTION, ACCEPTED, DECLINED or
	// TENTATIVE. Role is CHAIR, REQ-PARTICIPANT or OPT-PARTICIPANT.
	Participation string `json:"participation,omitempty"`
	Role          string `json:"role,omitempty"`
}

// Parsed is what is kept beside the file, pulled out of it once when it is
// written so that nothing later has to read iCalendar to list or search.
//
// Data is the file itself, in the form this server stores: what the library
// returns, folded. Where Data and the fields beside it disagree, Data is
// right -- it is what a device is given back, and the fields exist only to
// answer questions about it quickly.
type Parsed struct {
	UID         string
	Summary     string
	Location    string
	Description string

	// Timezone is the IANA name the start is anchored to, when it is
	// anchored to one; empty means the file wrote a plain moment.
	// Recurrence is the repeat rule as its own text, without the property
	// name, for a form that shows it.
	Timezone   string
	Recurrence string

	// StartsAt and EndsAt are the first occurrence, in UTC. AllDay marks an
	// event written as a date rather than a date and a time: a birthday
	// belongs to the day everywhere, and must not be shifted into the day
	// before by a time zone.
	StartsAt time.Time
	EndsAt   time.Time
	AllDay   bool

	// Recurring marks a file that carries a recurrence rule, so a caller
	// knows to ask for occurrences rather than to trust StartsAt alone.
	Recurring bool

	// Status is the event's own STATUS, and Transparent marks an event its
	// owner has said does not make them busy.
	Status      string
	Transparent bool

	// Organizer and Attendees are who is arranging it and who is asked.
	// Method is the file's METHOD: REQUEST, REPLY or CANCEL when it arrived
	// as an invitation by mail, and empty for an ordinary event.
	Organizer string

	// SentBy is who put it in the post on the organizer's behalf, when the
	// file says somebody did.
	SentBy string

	Attendees []Attendee
	Method    string

	// Sequence is the event's own SEQUENCE, which an organizer increments
	// each time they change it. A REQUEST carrying a lower one than is
	// already held is stale and must be ignored.
	Sequence int

	Data []byte

	// calendar is kept so that occurrences can be expanded without decoding
	// the text a second time.
	calendar *ical.Calendar
}

// Parse reads one iCalendar file and returns what to keep beside it.
//
// The file is re-encoded before it is stored, because the library normalizes
// as it decodes: what goes in is not byte for byte what comes out, but what
// comes out is stable, so storing that and taking the ETag over it keeps the
// promise that the version a listing names is the bytes a fetch returns.
func Parse(data []byte) (*Parsed, error) {
	if len(data) > MaximumObject {
		return nil, ErrTooLarge
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("calendar: there is nothing in that event")
	}
	decoded, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("calendar: that is not an event this server can read: %w", err)
	}
	// Before anything reads a time out of it: a zone named the way Windows
	// names them becomes the name this machine knows, and one nobody can
	// name has its times turned into the instants they stand for. Done once,
	// here, so everything afterwards is the ordinary path.
	settleZones(decoded)
	encoded, err := Encode(decoded)
	if err != nil {
		return nil, err
	}
	parsed := &Parsed{Data: encoded, calendar: decoded}
	parsed.Method, _ = decoded.Props.Text(ical.PropMethod)
	parsed.Method = strings.ToUpper(strings.TrimSpace(parsed.Method))

	event := firstEvent(decoded)
	if event == nil {
		return nil, fmt.Errorf("calendar: there is no event in that file")
	}
	parsed.UID, _ = event.Props.Text(ical.PropUID)
	parsed.UID = strings.TrimSpace(parsed.UID)
	if parsed.UID == "" {
		return nil, fmt.Errorf("calendar: that event has no identifier")
	}
	parsed.Summary, _ = event.Props.Text(ical.PropSummary)
	parsed.Location, _ = event.Props.Text(ical.PropLocation)
	parsed.Description, _ = event.Props.Text(ical.PropDescription)
	if rule := event.Props.Get(ical.PropRecurrenceRule); rule != nil {
		parsed.Recurrence = strings.TrimSpace(rule.Value)
	}
	if status, err := event.Status(); err == nil {
		parsed.Status = strings.ToUpper(string(status))
	}
	if transparency, err := event.Props.Text(ical.PropTransparency); err == nil {
		parsed.Transparent = strings.EqualFold(strings.TrimSpace(transparency), "TRANSPARENT")
	}
	if sequence := event.Props.Get(ical.PropSequence); sequence != nil {
		parsed.Sequence, _ = sequence.Int()
	}
	parsed.Recurring = event.Props.Get(ical.PropRecurrenceRule) != nil ||
		event.Props.Get(ical.PropRecurrenceDates) != nil

	if start := event.Props.Get(ical.PropDateTimeStart); start != nil {
		parsed.AllDay = start.ValueType() == ical.ValueDate
		parsed.Timezone = strings.TrimSpace(start.Params.Get(ical.ParamTimezoneID))
	}
	// Read in the zone the file names, and in UTC when it names none --
	// which is what a "floating" time means, and treating it as UTC is the
	// only choice that does not invent a zone the file did not give.
	//
	// The library resolves a TZID with time.LoadLocation and ignores the
	// VTIMEZONE beside it, so a name this machine does not know -- every
	// name Microsoft writes, "W. Europe Standard Time" and the rest -- came
	// back as the zero time and was stored as an event in the year one,
	// appearing in no window anybody ever asks about. Where that happens the
	// description in the file is used instead.
	// Read the ordinary way. The zones were settled before anything looked
	// at the file, so a name this machine does not know has either been
	// renamed to one it does or had its times turned into instants.
	starts, err := event.DateTimeStart(time.UTC)
	if err != nil {
		return nil, fmt.Errorf("calendar: that event's start cannot be read: %w", err)
	}
	parsed.StartsAt = starts.UTC()
	if ends, err := event.DateTimeEnd(time.UTC); err == nil {
		parsed.EndsAt = ends.UTC()
	}
	if parsed.StartsAt.IsZero() {
		// An event with no time is one that exists and can never be found.
		return nil, fmt.Errorf("calendar: that event has no start")
	}
	if parsed.EndsAt.Before(parsed.StartsAt) {
		// A file whose end precedes its start describes nothing. Rather
		// than refuse it -- some clients do send this, and refusing loses
		// the event entirely -- it is treated as an instant.
		parsed.EndsAt = parsed.StartsAt
	}

	parsed.Organizer = addressOf(event.Props.Get(ical.PropOrganizer))
	// Who actually put it in the post, when that is somebody else. An
	// assistant, a room booking system, a service that sends for a person:
	// SENT-BY exists for exactly that, and refusing it turns every
	// delegated invitation into nothing.
	if organizer := event.Props.Get(ical.PropOrganizer); organizer != nil {
		parsed.SentBy = strings.TrimPrefix(
			strings.ToLower(strings.TrimSpace(organizer.Params.Get("SENT-BY"))), "mailto:")
		parsed.SentBy = strings.Trim(parsed.SentBy, "\"")
		if at := strings.Index(parsed.SentBy, ":"); at >= 0 {
			parsed.SentBy = strings.TrimPrefix(parsed.SentBy[at+1:], "//")
		}
	}
	for index := range event.Props[ical.PropAttendee] {
		property := &event.Props[ical.PropAttendee][index]
		address := addressOf(property)
		if address == "" {
			continue
		}
		parsed.Attendees = append(parsed.Attendees, Attendee{
			Address:       address,
			Name:          strings.TrimSpace(property.Params.Get(ical.ParamCommonName)),
			Participation: strings.ToUpper(strings.TrimSpace(property.Params.Get(ical.ParamParticipationStatus))),
			Role:          strings.ToUpper(strings.TrimSpace(property.Params.Get(ical.ParamRole))),
		})
	}
	return parsed, nil
}

// Occurrence is one time an event happens.
type Occurrence struct {
	StartsAt time.Time
	EndsAt   time.Time
	AllDay   bool
}

// Occurrences are when an event happens between two moments, with its
// recurrence rule applied.
//
// Three things about this are easy to get wrong, and each of them is a way to
// show somebody a meeting that is not happening:
//
// An event that does not recur happens once, at its own time, whether or not
// that is inside the window.
//
// A recurring event's occurrences are what the rule generates, which does not
// include its own start unless the start satisfies the rule. An event
// beginning on a Tuesday with "every Monday" first happens on the following
// Monday, and prepending the start would put a meeting in the calendar that
// nobody was invited to.
//
// An occurrence lasts as long as the first one did. It is the length that
// repeats, not the end: an hour-long meeting recurring weekly is an hour long
// every week, and using the original end would make every later occurrence
// finish in the past.
func Occurrences(parsed *Parsed, from, until time.Time) ([]Occurrence, error) {
	if parsed == nil || parsed.calendar == nil {
		return nil, fmt.Errorf("calendar: there is no event to expand")
	}
	event := firstEvent(parsed.calendar)
	if event == nil {
		return nil, fmt.Errorf("calendar: there is no event in that file")
	}
	length := parsed.EndsAt.Sub(parsed.StartsAt)
	if length < 0 {
		length = 0
	}
	set, err := event.RecurrenceSet(time.UTC)
	if err != nil {
		return nil, fmt.Errorf("calendar: that event's recurrence cannot be read: %w", err)
	}
	if set == nil {
		if parsed.StartsAt.Before(from) && !parsed.EndsAt.After(from) {
			return nil, nil
		}
		if !parsed.StartsAt.Before(until) {
			return nil, nil
		}
		return []Occurrence{{
			StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt, AllDay: parsed.AllDay,
		}}, nil
	}
	// Asked from earlier than the window, because an occurrence that began
	// before it and has not finished is still on: a person looking at
	// Tuesday wants to see the meeting that started on Monday night.
	starting := from.Add(-maximumLength)
	occurrences := make([]Occurrence, 0, 8)

	// Walked one at a time rather than asked for the window in one go.
	//
	// Between builds the whole list before returning it, so the bound below
	// would have limited the answer and not the work: "every second, for
	// ever" over the stretch this server indexes is ninety million moments
	// built in memory before a single one is thrown away. One email carrying
	// that rule was enough to take the server down. Pulling them one by one
	// means the bound is a bound on what is done, not on what is kept.
	next := set.Iterator()
	walked := 0
	for ; walked < maximumSteps; walked++ {
		when, ok := next()
		if !ok {
			break
		}
		if when.Before(starting) {
			continue
		}
		if !when.Before(until) {
			break
		}
		occurrence := Occurrence{
			StartsAt: when.UTC(), EndsAt: when.Add(length).UTC(), AllDay: parsed.AllDay,
		}
		if !occurrence.EndsAt.After(from) && occurrence.StartsAt.Before(from) {
			continue
		}
		occurrences = append(occurrences, occurrence)
		if len(occurrences) >= MaximumOccurrences {
			break
		}
	}
	// Running out of steps before reaching the window is not the same as
	// there being nothing in it, and returning an empty list for both said
	// the event simply was not happening. A rule fine enough and old enough
	// -- every hour, six years back -- spends the whole bound getting to
	// today, and the event then vanished from every view while a fetch of
	// it still worked.
	if walked >= maximumSteps && len(occurrences) == 0 {
		return nil, fmt.Errorf("calendar: that repeat is too fine to work out over this stretch of time")
	}
	sort.Slice(occurrences, func(first, second int) bool {
		return occurrences[first].StartsAt.Before(occurrences[second].StartsAt)
	})
	return occurrences, nil
}

// MaximumOccurrences is how many one file may contribute to one window.
//
// There has to be a number: a rule may repeat every minute for ever, and a
// window is chosen by whoever is asking. It is high enough that no calendar a
// person keeps reaches it -- a daily event fills eleven years -- and low
// enough that one pathological rule cannot fill the memory of the server.
const MaximumOccurrences = 4000

// maximumSteps is how many moments a rule may be walked through before this
// server stops, however few of them land in the window asked about.
//
// Separate from MaximumOccurrences, which bounds what is kept: a rule that
// repeats every second inside a window that wants none of them does no work
// worth keeping and unbounded work getting there. Ten times the cap leaves
// room for a rule that genuinely steps past a great many -- an anniversary
// among daily standups -- without letting one walk for ever.
const maximumSteps = 10 * MaximumOccurrences

// maximumLength is how far before a window an occurrence may have begun and
// still be counted as inside it. A fortnight covers a conference or a
// holiday, which are the longest things people actually put in a calendar,
// without making every query read the whole year.
const maximumLength = 14 * 24 * time.Hour

// ETag names a version of an event.
func ETag(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// firstEvent is the VEVENT a file is about.
//
// A file may hold several: an event that recurs, and beside it the individual
// occurrences somebody moved or renamed, each with the same UID and its own
// RECURRENCE-ID. The one without a RECURRENCE-ID is the event itself, and is
// what the fields beside the file describe.
func firstEvent(cal *ical.Calendar) *ical.Event {
	events := cal.Events()
	for index := range events {
		if events[index].Props.Get(ical.PropRecurrenceID) == nil {
			return &events[index]
		}
	}
	if len(events) > 0 {
		return &events[0]
	}
	return nil
}

// addressOf reads the mail address out of an ORGANIZER or ATTENDEE line,
// which carries it as a URI: "mailto:ada@example.com".
//
// Anything that is not a mailto is left alone rather than guessed at -- a
// room booking system may name a resource by some other scheme, and turning
// that into an address would be inventing one.
func addressOf(property *ical.Prop) string {
	if property == nil {
		return ""
	}
	value := strings.TrimSpace(property.Value)
	if value == "" {
		return ""
	}
	if scheme := strings.Index(value, ":"); scheme >= 0 {
		if !strings.EqualFold(value[:scheme], "mailto") {
			return ""
		}
		value = value[scheme+1:]
	}
	return strings.TrimSpace(value)
}

// Horizon is how far ahead and behind the times an event happens are worked
// out and written down.
//
// An event that repeats with no end cannot be indexed for ever, so the index
// reaches a horizon and no further. Two years ahead covers every view a
// calendar draws and any reasonable amount of paging; a year behind covers
// looking back at what happened. Measured from now rather than from the
// event, so that a repeat set up years ago is still indexed over the part of
// it anybody is going to look at.
const (
	HorizonAhead  = 2 * 366 * 24 * time.Hour
	HorizonBehind = 366 * 24 * time.Hour
)

// Indexed is when an event happens, over the stretch worth writing down.
//
// It is here rather than beside either caller because both doors into a
// calendar -- the dashboard's API and CalDAV -- index what they write, and
// two doors that disagreed about the horizon would give a calendar whose
// contents depended on which one last touched it.
func Indexed(parsed *Parsed) ([]Occurrence, time.Time, error) {
	if parsed == nil {
		return nil, time.Time{}, fmt.Errorf("calendar: there is no event to index")
	}
	now := time.Now().UTC()
	from, until := now.Add(-HorizonBehind), now.Add(HorizonAhead)
	if !parsed.Recurring {
		// An event that happens once is indexed wherever it is. Somebody
		// who puts a date in for a graduation years away should see it
		// when they get there, and it is one row.
		from, until = parsed.StartsAt.Add(-time.Second), parsed.EndsAt.Add(time.Second)
	} else if parsed.StartsAt.After(from) {
		from = parsed.StartsAt.Add(-time.Second)
	}
	occurrences, err := Occurrences(parsed, from, until)
	if err != nil {
		return nil, time.Time{}, err
	}
	// How far this was worked out to, which is what says when it needs
	// doing again. Not the furthest occurrence: a series that has finished
	// has none here, and would otherwise look like it needed extending for
	// ever.
	return occurrences, until, nil
}
