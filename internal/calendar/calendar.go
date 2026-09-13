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

// MaximumGuests is how many people one event will have invitations sent to.
//
// Not a limit on what may be kept: an invitation that arrives naming five
// hundred people is stored as it came, because it is a record of something
// somebody else did. It is a limit on what this server will send, and it
// exists because the guest list of an event is not always its owner's work --
// an invitation arriving by mail brings somebody else's list, and saving that
// event must not turn into a mail run.
const MaximumGuests = 100

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

	// AnsweredAt is when the program that sent this person's answer wrote
	// it, kept beside the answer so that a later one cannot be undone by an
	// earlier one arriving afterwards -- mail is not ordered, and a copy of
	// an old message replays with its signature intact.
	//
	// This server's own, written as a parameter of its own name: the format
	// has nowhere else to put it, and anything that does not know the
	// parameter ignores it.
	AnsweredAt time.Time `json:"answeredAt,omitempty"`
}

// AnsweredParam is where the moment an answer was written is kept.
const AnsweredParam = "X-TEANODE-ANSWERED"

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

	// Stamp is DTSTAMP: when the program that wrote this file wrote it. For
	// an answer that is when the person pressed the button, which is how
	// two answers to the same version of an event are told apart.
	Stamp time.Time

	// RecurrenceID names the one occurrence of a series this file is about,
	// written the one way as an instant. Empty for a file about the series
	// itself, which is nearly every file.
	//
	// A person who moves next Tuesday's standup has not touched the
	// standup: the format says so with a second event carrying the same
	// UID and this property. What arrives by mail is only the part that
	// changed, so a file with this set must be put beside what is held
	// rather than treated as the whole event.
	RecurrenceID string

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
	if stamp := event.Props.Get(ical.PropDateTimeStamp); stamp != nil {
		if at, err := stamp.DateTime(time.UTC); err == nil {
			parsed.Stamp = at.UTC()
		}
	}
	parsed.Recurring = event.Props.Get(ical.PropRecurrenceRule) != nil ||
		event.Props.Get(ical.PropRecurrenceDates) != nil
	parsed.RecurrenceID = occurrenceKey(event.Props.Get(ical.PropRecurrenceID))

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
		attendee := Attendee{
			Address:       address,
			Name:          strings.TrimSpace(property.Params.Get(ical.ParamCommonName)),
			Participation: strings.ToUpper(strings.TrimSpace(property.Params.Get(ical.ParamParticipationStatus))),
			Role:          strings.ToUpper(strings.TrimSpace(property.Params.Get(ical.ParamRole))),
		}
		if written := strings.TrimSpace(property.Params.Get(AnsweredParam)); written != "" {
			if at, err := time.Parse("20060102T150405Z", written); err == nil {
				attendee.AnsweredAt = at.UTC()
			}
		}
		parsed.Attendees = append(parsed.Attendees, attendee)
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
	occurrences, _, err := occurrencesWithin(parsed, from, until)
	return occurrences, err
}

// occurrencesWithin is Occurrences, and whether it stopped at the cap rather
// than at the end of the window.
//
// The difference matters to whoever writes down how far an event has been
// worked out. Stopping at the cap means the index reaches the last occurrence
// kept and no further, and recording the window's end instead says the event
// was indexed over a stretch where nothing was written -- which is how a
// standing meeting disappears while a fetch of it still works.
func occurrencesWithin(parsed *Parsed, from, until time.Time) ([]Occurrence, bool, error) {
	if parsed == nil || parsed.calendar == nil {
		return nil, false, fmt.Errorf("calendar: there is no event to expand")
	}
	event := firstEvent(parsed.calendar)
	if event == nil {
		return nil, false, fmt.Errorf("calendar: there is no event in that file")
	}
	length := parsed.EndsAt.Sub(parsed.StartsAt)
	if length < 0 {
		length = 0
	}
	set, err := event.RecurrenceSet(time.UTC)
	if err != nil {
		return nil, false, fmt.Errorf("calendar: that event's recurrence cannot be read: %w", err)
	}
	if set == nil {
		if parsed.StartsAt.Before(from) && !parsed.EndsAt.After(from) {
			return nil, false, nil
		}
		if !parsed.StartsAt.Before(until) {
			return nil, false, nil
		}
		return []Occurrence{{
			StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt, AllDay: parsed.AllDay,
		}}, false, nil
	}
	// The occurrences somebody moved, or that were called off on their own.
	// Each replaces the one the rule would have generated at the moment it
	// names -- so the rule's own answer for that moment is dropped, and the
	// override's times are used instead. Without this a meeting moved to
	// Thursday went on showing up on Tuesday, and the phone that moved it
	// was told it had not happened.
	changed := overrides(parsed.calendar)
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
	// Three ways to stop, and only two of them mean the answer is short.
	// Read off the counters afterwards instead, a series that happened to
	// end on the last allowed step looked cut off -- and a series that has
	// finished being told it was cut off is one whose whole past is thrown
	// away and which is then re-expanded, unchanged, for ever.
	capped, spent := false, true
	walked := 0
	for ; walked < maximumSteps; walked++ {
		when, ok := next()
		if !ok {
			// The series itself ended.
			spent = false
			break
		}
		if when.Before(starting) {
			continue
		}
		if !when.Before(until) {
			// Past the end of the window, which is as far as anybody
			// asked.
			spent = false
			break
		}
		if _, moved := changed[when.UTC().Format(time.RFC3339)]; moved {
			// This one is written out on its own below, at whatever time
			// it was moved to -- or not at all, if it was called off.
			continue
		}
		occurrence := Occurrence{
			StartsAt: when.UTC(), EndsAt: when.Add(length).UTC(), AllDay: parsed.AllDay,
		}
		if !occurrence.EndsAt.After(from) && occurrence.StartsAt.Before(from) {
			continue
		}
		occurrences = append(occurrences, occurrence)
		if len(occurrences) >= MaximumOccurrences {
			// Full. Whether that cut anything off is a question with an
			// answer: ask for one more. A series of exactly this many
			// that has finished is not cut off, and treating it as
			// though it were costs it its whole past.
			if further, ok := next(); ok && further.Before(until) {
				capped = true
			}
			spent = false
			break
		}
	}
	// Running out of steps before reaching the window is not the same as
	// there being nothing in it, and returning an empty list for both said
	// the event simply was not happening. A rule fine enough and old enough
	// -- every hour, six years back -- spends the whole bound getting to
	// today, and the event then vanished from every view while a fetch of
	// it still worked.
	if spent && len(occurrences) == 0 {
		return nil, false, fmt.Errorf("calendar: that repeat is too fine to work out over this stretch of time")
	}
	// Walking out of steps is stopping short just as surely as filling the
	// list is.
	if spent {
		capped = true
	}
	occurrences = append(occurrences, movedOccurrences(changed, from, until)...)
	sort.Slice(occurrences, func(first, second int) bool {
		return occurrences[first].StartsAt.Before(occurrences[second].StartsAt)
	})
	return occurrences, capped, nil
}

// movedOccurrences are the ones kept apart from the series, at the times they
// were moved to. One that was called off is not on at all.
func movedOccurrences(changed map[string]*ical.Component, from, until time.Time) []Occurrence {
	if len(changed) == 0 {
		return nil
	}
	moved := make([]Occurrence, 0, len(changed))
	for _, component := range changed {
		event := &ical.Event{Component: component}
		if status, err := event.Status(); err == nil &&
			strings.EqualFold(string(status), "CANCELLED") {
			continue
		}
		starts, err := event.DateTimeStart(time.UTC)
		if err != nil || starts.IsZero() {
			continue
		}
		ends, err := event.DateTimeEnd(time.UTC)
		if err != nil || ends.Before(starts) {
			ends = starts.Add(time.Hour)
		}
		if !starts.Before(until) || !ends.After(from) {
			continue
		}
		allDay := false
		if start := component.Props.Get(ical.PropDateTimeStart); start != nil {
			allDay = start.ValueType() == ical.ValueDate
		}
		moved = append(moved, Occurrence{
			StartsAt: starts.UTC(), EndsAt: ends.UTC(), AllDay: allDay,
		})
	}
	return moved
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
	occurrences, capped, err := occurrencesWithin(parsed, from, until)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !capped {
		// How far this was worked out to, which is what says when it
		// needs doing again. Not the furthest occurrence: a series that
		// has finished has none here, and would otherwise look like it
		// needed extending for ever.
		return occurrences, until, nil
	}
	// A repeat fine enough to fill the cap is a different matter, and
	// saying "indexed to the horizon" about it is simply untrue. Two things
	// follow.
	//
	// The cap is spent on what is ahead rather than on what has already
	// happened. Worked out from a year back, an hourly repeat filled the
	// whole list with last spring and had nothing in it for today, so the
	// event was nowhere in the calendar while every fetch of it worked.
	// Looking back at a repeat that fine is the thing given up, and it is
	// the right thing to give up.
	if from.Before(now) {
		ahead, _, err := occurrencesWithin(parsed, now, until)
		if err != nil {
			// A repeat that cannot even be walked to today is one this
			// server cannot index at all, and saying so puts it out of
			// the queue. Swallowed, the answer kept was the one full of
			// last spring -- nothing in it for today, a horizon already
			// in the past, and therefore the same two long walks redone
			// on every tick for ever.
			return nil, time.Time{}, err
		}
		if len(ahead) == 0 {
			return nil, time.Time{}, fmt.Errorf("calendar: that repeat has nothing left in the stretch this server writes down")
		}
		occurrences = ahead
	}
	// And the horizon is the last moment actually written down. Recording
	// the window's end instead left the index reaching five months out
	// while the row claimed two years, so nothing ever extended it and the
	// event stopped appearing with nothing to show for it.
	if len(occurrences) == 0 {
		return occurrences, now, nil
	}
	return occurrences, occurrences[len(occurrences)-1].StartsAt, nil
}
