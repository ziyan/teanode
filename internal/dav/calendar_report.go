package dav

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Answering a calendar REPORT here rather than letting the protocol library
// do it, for the same two reasons a contacts report is answered here.
//
// The library serializes an event by re-encoding the parsed form with its own
// encoder, and that encoder does not fold long lines while what this server
// stores is folded -- so the bytes it would write are not the bytes the
// length says, and not the bytes the ETag was taken over. A GET is already
// answered from what is stored; a REPORT is the request a phone actually uses
// on every synchronization, so it has to be too.
//
// And the filter is ours to carry out anyway. A calendar-query asks for the
// events in a window, and answering that by decoding every file in the
// calendar and expanding its repeat is work proportional to the calendar
// rather than to the window; the index of when things happen is there so it
// does not have to be.
//
// Three reports are answered: calendar-multiget, which asks for events by
// name; calendar-query, which asks for the ones in a window; and
// free-busy-query, which asks when somebody is busy without asking what they
// are doing. The library has no answer for that last one at all, so if it
// were not answered here it would not be answered.

const calendarNS = "urn:ietf:params:xml:ns:caldav"

// calendarProperties are the properties a calendar report may be asked for.
// Separate from the contacts ones so that neither carries the other's
// namespace into an answer.
type calendarProperties struct {
	ETag         string `xml:"DAV: getetag,omitempty"`
	ContentType  string `xml:"DAV: getcontenttype,omitempty"`
	Length       string `xml:"DAV: getcontentlength,omitempty"`
	CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data,omitempty"`
}

type calendarPropStat struct {
	Prop   calendarProperties `xml:"DAV: prop"`
	Status string             `xml:"DAV: status"`
}

type calendarResponse struct {
	Href     string             `xml:"DAV: href"`
	PropStat []calendarPropStat `xml:"DAV: propstat"`
	Status   string             `xml:"DAV: status,omitempty"`
}

type calendarMultistatus struct {
	XMLName   xml.Name           `xml:"DAV: multistatus"`
	Responses []calendarResponse `xml:"DAV: response"`
}

// calendarQuery is the filter of a calendar-query, as it arrives.
//
// Only the parts a client actually sends are read: a nested pair of component
// filters -- VCALENDAR, then VEVENT -- and a time range on the inner one.
type calendarQuery struct {
	Filter struct {
		CompFilter compFilter `xml:"urn:ietf:params:xml:ns:caldav comp-filter"`
	} `xml:"urn:ietf:params:xml:ns:caldav filter"`
}

type compFilter struct {
	Name         string               `xml:"name,attr"`
	IsNotDefined *struct{}            `xml:"urn:ietf:params:xml:ns:caldav is-not-defined"`
	TimeRange    *timeRange           `xml:"urn:ietf:params:xml:ns:caldav time-range"`
	Children     []compFilter         `xml:"urn:ietf:params:xml:ns:caldav comp-filter"`
	Test         string               `xml:"test,attr"`
	PropFilters  []calendarPropFilter `xml:"urn:ietf:params:xml:ns:caldav prop-filter"`
}

// calendarPropFilter is a condition on one property of an event: that it is
// there, that it is not, or that its value matches some text.
type calendarPropFilter struct {
	Name         string     `xml:"name,attr"`
	IsNotDefined *struct{}  `xml:"urn:ietf:params:xml:ns:caldav is-not-defined"`
	TimeRange    *timeRange `xml:"urn:ietf:params:xml:ns:caldav time-range"`
	TextMatches  []struct {
		Text      string `xml:",chardata"`
		Negate    string `xml:"negate-condition,attr"`
		MatchType string `xml:"match-type,attr"`
		Collation string `xml:"collation,attr"`
	} `xml:"urn:ietf:params:xml:ns:caldav text-match"`
}

type timeRange struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
}

// window is the two moments a time-range names, read the way the format
// writes them: "20260914T100000Z".
func (self *timeRange) window() (time.Time, time.Time, bool) {
	from, opened := readMoment(self.Start)
	until, closed := readMoment(self.End)
	if !opened && !closed {
		return time.Time{}, time.Time{}, false
	}
	if !opened {
		// Open at the start: everything up to the end. Far enough back to
		// cover any calendar somebody keeps.
		from = until.AddDate(-200, 0, 0)
	}
	if !closed {
		until = from.AddDate(200, 0, 0)
	}
	if !until.After(from) {
		return time.Time{}, time.Time{}, false
	}
	return from, until, true
}

func readMoment(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, shape := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if at, err := time.ParseInLocation(shape, trimmed, time.UTC); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

// eventFilter is the VEVENT filter inside a query: the filter itself, whether
// the query asks for something this calendar can never hold, and whether it is
// a query this server can carry out at all.
//
// A filter this server cannot carry out is refused rather than answered
// emptily. Skipping every event would tell a client whose query was slightly
// out of spec that the calendar is empty, which a client enumerating with a
// query reads as everything having been deleted.
//
// The three answers have to be three. Written as two, "matches nothing" and
// "no filter at all" were both a nil filter, and the caller read nil the
// second way: a query for the to-dos this server does not keep came back
// carrying every event in the calendar, and a client that asked for to-dos
// and events together had its time range thrown away with the to-do filter.
func (self *calendarQuery) eventFilter() (filter *compFilter, nothing bool, usable bool) {
	outer := self.Filter.CompFilter
	if outer.Name != "" && !strings.EqualFold(outer.Name, "VCALENDAR") {
		return nil, false, false
	}
	other := false
	for index := range outer.Children {
		child := &outer.Children[index]
		switch {
		case strings.EqualFold(child.Name, "VEVENT"):
			// The events are what this server keeps, so this is the part
			// of the question it can answer, whatever else was asked
			// beside it.
			return child, false, true
		case child.Name == "":
			continue
		default:
			// A to-do, a journal or a free-busy component.
			other = true
		}
	}
	if other {
		// Asked only for kinds of thing this server does not keep, which
		// matches nothing -- the truth rather than a refusal.
		return nil, true, true
	}
	// No inner filter: every event in the calendar.
	return nil, false, true
}

// matches says whether one event satisfies the property conditions in a
// filter.
//
// Applied here rather than left to the client, for the reason the address
// book gives: a client using the query as its only filter would otherwise be
// told that every event in the calendar matches, and would carry the whole
// calendar across the network to find out otherwise.
//
// An event whose file cannot be read matches, on the principle that something
// nobody can read is better shown than silently withheld.
func (self *compFilter) matches(data string) bool {
	if self == nil || len(self.PropFilters) == 0 {
		return true
	}
	decoded, err := ical.NewDecoder(strings.NewReader(data)).Decode()
	if err != nil {
		return true
	}
	// Any event in the file. Reading the first one was wrong in an ordinary
	// shape: a client that moves one occurrence of a series writes that
	// occurrence into the file, often first, so a search for the series by
	// its own title did not find the file the series is in.
	matched := false
	empty := true
	for _, child := range decoded.Children {
		if child == nil || child.Name != ical.CompEvent {
			continue
		}
		empty = false
		if self.matchesEvent(child) {
			matched = true
			break
		}
	}
	return empty || matched
}

// matchesEvent is whether one event satisfies every property condition.
//
// Every one of them, not any: a calendar query has no test to choose with --
// the format that does is the address book's -- and a component matches when
// its time range and all of its property conditions do. Taken as "any", two
// conditions returned the union of what each asked for, which is the opposite
// of narrowing a search.
func (self *compFilter) matchesEvent(event *ical.Component) bool {
	for index := range self.PropFilters {
		if !self.PropFilters[index].matchesProp(event) {
			return false
		}
	}
	return true
}

// matchesProp is one property condition against the values an event holds.
//
// A property the event does not have matches only through is-not-defined.
// Written as "whatever the text match says, negated if asked", a negated
// match on an absent property came out true -- so "everything whose notes do
// not mention lunch" answered with every event that has no notes at all,
// which the format explicitly does not mean.
func (self *calendarPropFilter) matchesProp(event *ical.Component) bool {
	held := event.Props[strings.ToUpper(strings.TrimSpace(self.Name))]
	if self.IsNotDefined != nil {
		return len(held) == 0
	}
	if len(held) == 0 {
		return false
	}
	if len(self.TextMatches) == 0 {
		// The property is there, which is all that was asked.
		return true
	}
	return self.matchesText(held)
}

// matchesText is one property filter's text matches against the values an
// event holds for it, all of them, case-insensitively -- which is the
// protocol's default collation and the only one this server offers.
func (self *calendarPropFilter) matchesText(held []ical.Prop) bool {
	for _, match := range self.TextMatches {
		wanted := strings.ToLower(strings.TrimSpace(match.Text))
		found := false
		for _, property := range held {
			value := strings.ToLower(property.Value)
			switch match.MatchType {
			case "equals":
				found = value == wanted
			case "starts-with":
				found = strings.HasPrefix(value, wanted)
			case "ends-with":
				found = strings.HasSuffix(value, wanted)
			default:
				found = strings.Contains(value, wanted)
			}
			if found {
				break
			}
		}
		if match.Negate == "yes" {
			found = !found
		}
		if !found {
			return false
		}
	}
	return true
}

// carriedOut is whether every condition in a filter is one this server
// actually applies, so that a query asking for something else is refused
// rather than answered as though the condition had been met.
//
// Two of them were read off the wire and quietly dropped. A condition on when
// a property falls -- a time range inside a property filter -- was taken as
// "the property is there", so a query for events whose start is in 2020
// answered with events in 2026. And a collation names how text is compared:
// asked to compare exactly, this server compared case-insensitively anyway
// and said nothing, which is a different search from the one that was asked
// for.
func (self *compFilter) carriedOut() error {
	if self == nil {
		return nil
	}
	for _, filter := range self.PropFilters {
		if filter.TimeRange != nil {
			return fmt.Errorf("a condition on when %s falls is not one this server answers", filter.Name)
		}
		for _, match := range filter.TextMatches {
			switch strings.ToLower(strings.TrimSpace(match.Collation)) {
			case "", "default", "i;unicode-casemap", "i;ascii-casemap":
			default:
				return fmt.Errorf("a collation of %q is not one this server offers", match.Collation)
			}
		}
	}
	return nil
}

// addressesACalendar is whether a path names a calendar itself rather than
// something inside it. A query and a free-busy request are about a collection;
// a multiget may name one file, which is what its hrefs are for.
func addressesACalendar(path string) bool {
	return len(segmentsOf(path)) == 3
}

// serveCalendarReport answers a REPORT if it is one this package handles, and
// says so.
func (self *component) serveCalendarReport(writer http.ResponseWriter, request *http.Request,
	backing *calendarBackend, body []byte) bool {
	var asked reportRequest
	if err := xml.Unmarshal(body, &asked); err != nil {
		return false
	}
	if asked.XMLName.Space != calendarNS {
		return false
	}
	wantsData, wantsETag, wantsLength := false, false, false
	for _, name := range asked.Props.Names {
		switch {
		case name.Space == calendarNS && name.Local == "calendar-data":
			wantsData = true
		case name.Local == "getetag":
			wantsETag = true
		case name.Local == "getcontentlength":
			wantsLength = true
		}
	}

	ctx := request.Context()
	signedIn := backing.who(ctx)
	var answers []calendarResponse

	switch asked.XMLName.Local {
	case "calendar-multiget":
		// Named events. Each href is the client's, so each is resolved
		// the long way round -- through the calendar, which is checked to
		// belong to whoever is signed in -- rather than trusted.
		if len(asked.Hrefs) > maximumHrefs {
			http.Error(writer, "that report asks for too many events at once", http.StatusForbidden)
			return true
		}
		for _, href := range asked.Hrefs {
			// Percent-decoded before it is used as a path, and given back
			// percent-encoded. A client sends back what the listing gave
			// it, and the listing is a URL.
			path := decodeHref(strings.TrimSpace(href))
			object, err := backing.storedEvent(ctx, path)
			if err != nil {
				status, _ := statusOf(err)
				answers = append(answers, calendarResponse{
					Href: encodeHref(path), Status: statusLine(status)})
				continue
			}
			answers = append(answers, foundEvent(signedIn, object, wantsETag, wantsLength, wantsData))
		}

	case "free-busy-query":
		if !addressesACalendar(request.URL.Path) {
			http.Error(writer, "that report is about a calendar, not one event in it",
				http.StatusForbidden)
			return true
		}
		self.serveFreeBusy(writer, request, backing, body)
		return true

	case "calendar-query":
		// Addressed to the calendar. Asked about one event's own address
		// the path still resolved to the calendar it is in, so a report
		// about a single file answered with every file in the calendar.
		if !addressesACalendar(request.URL.Path) {
			http.Error(writer, "that report is about a calendar, not one event in it",
				http.StatusForbidden)
			return true
		}
		var query calendarQuery
		if err := xml.Unmarshal(body, &query); err != nil {
			http.Error(writer, "that filter could not be read", http.StatusBadRequest)
			return true
		}
		filter, nothing, usable := query.eventFilter()
		if !usable {
			http.Error(writer, "this server does not answer that filter", http.StatusBadRequest)
			return true
		}
		if err := filter.carriedOut(); err != nil {
			// Said rather than silently answered as though the condition
			// had been met, which is how a client gets back events it
			// carefully asked not to see.
			http.Error(writer, err.Error(), http.StatusForbidden)
			return true
		}
		if !nothing {
			objects, err := self.eventsMatching(ctx, backing, request.URL.Path, filter)
			if err != nil {
				status, message := statusOf(err)
				http.Error(writer, message, status)
				return true
			}
			for _, object := range objects {
				if !filter.matches(object.Data) {
					continue
				}
				answers = append(answers, foundEvent(signedIn, object, wantsETag, wantsLength, wantsData))
			}
		}

	default:
		return false
	}

	encoded, err := xml.Marshal(&calendarMultistatus{Responses: answers})
	if err != nil {
		log.Errorf("a calendar report could not be written: %s", err)
		http.Error(writer, "this server could not do that just now", http.StatusInternalServerError)
		return true
	}
	writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	writer.WriteHeader(http.StatusMultiStatus)
	if _, err := writer.Write([]byte(xml.Header)); err != nil {
		return true
	}
	_, _ = writer.Write(encoded)
	return true
}

// serveFreeBusy answers "when is this person busy", and says nothing else.
//
// Not a multistatus: this report answers with a calendar, which is the one
// place in WebDAV where a REPORT is not XML.
func (self *component) serveFreeBusy(writer http.ResponseWriter, request *http.Request,
	backing *calendarBackend, body []byte) {
	var asked struct {
		TimeRange timeRange `xml:"urn:ietf:params:xml:ns:caldav time-range"`
	}
	if err := xml.Unmarshal(body, &asked); err != nil {
		http.Error(writer, "that request could not be read", http.StatusBadRequest)
		return
	}
	from, until, ok := asked.TimeRange.window()
	if !ok {
		http.Error(writer, "a free-busy request has to say which stretch of time it is about",
			http.StatusBadRequest)
		return
	}

	ctx := request.Context()
	signedIn := backing.who(ctx)
	found, err := backing.calendarAt(ctx, signedIn, request.URL.Path)
	if err != nil {
		status, message := statusOf(err)
		http.Error(writer, message, status)
		return
	}

	// Whose calendar this is, by address, so that "which of these attendees
	// is me" can be answered and an event they declined does not make them
	// look busy.
	//
	// Every address of theirs, not just the mailbox this client signed in
	// with: a calendar belongs to the account, and somebody with two
	// mailboxes who declined a meeting under the other one was reported
	// busy for a meeting they had said no to.
	theirs := make(map[string]bool)
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		mailboxes, err := tx.ListMailboxes(signedIn.userID)
		if err != nil {
			return err
		}
		for _, mailbox := range mailboxes {
			for _, address := range mailbox.Addresses {
				theirs[strings.ToLower(strings.TrimSpace(address.Address))] = true
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the addresses of a calendar's owner: %s", err)
	}

	var busy []calendar.Occurrence
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		occurrences, err := tx.ListOccurrences(found.ID, from, until)
		if err != nil {
			return unexpectedCalendar(err)
		}
		// Whether an event makes its owner busy is a property of the
		// event, not of each time it happens, so it is decided once per
		// file however many occurrences that file has in the window.
		counts := make(map[string]bool, len(occurrences))
		for _, occurrence := range occurrences {
			decided, already := counts[occurrence.ObjectID]
			if !already {
				object, err := tx.GetCalendarObject(found.ID, occurrence.ObjectID)
				if err != nil {
					return unexpectedCalendar(err)
				}
				if object == nil {
					counts[occurrence.ObjectID] = false
					continue
				}
				parsed, err := calendar.Parse([]byte(object.Data))
				if err != nil {
					// Kept text that cannot be read is this server's
					// problem. Counted as busy, because the safe
					// mistake here is to say somebody is unavailable
					// when they are free rather than the other way
					// round: the first wastes a slot, the second
					// books over something real.
					decided = true
				} else {
					decided = calendar.MakesBusy(parsed, theirs)
				}
				counts[occurrence.ObjectID] = decided
			}
			if !decided {
				continue
			}
			busy = append(busy, calendar.Occurrence{
				StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay,
			})
		}
		return nil
	}); err != nil {
		status, message := statusOf(err)
		http.Error(writer, message, status)
		return
	}

	answer := calendar.WriteFreeBusy(calendar.FreeBusy(busy, from, until), from, until)
	writer.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(answer)))
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write(answer); err != nil {
		log.Debugf("a free-busy answer could not be written to the client: %s", err)
	}
}

// eventsMatching is the events a query asks for.
//
// With a time range, from the index of when things happen, so the work is
// proportional to the window rather than to the calendar; without one, the
// whole calendar, which is what a client enumerating asks for.
func (self *component) eventsMatching(ctx context.Context, backing *calendarBackend,
	address string, filter *compFilter) ([]*models.CalendarObject, error) {
	if filter == nil {
		return backing.storedEvents(ctx, address)
	}
	if filter.IsNotDefined != nil {
		// "Only if there are no events", which for a calendar that has
		// any is nothing at all.
		return nil, nil
	}
	if filter.TimeRange == nil {
		return backing.storedEvents(ctx, address)
	}
	from, until, ok := filter.TimeRange.window()
	if !ok {
		return nil, refuse(http.StatusBadRequest, "that time range is not one this server can read")
	}
	signedIn := backing.who(ctx)
	found, err := backing.calendarAt(ctx, signedIn, address)
	if err != nil {
		return nil, err
	}
	var objects []*models.CalendarObject
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		occurrences, err := tx.ListOccurrences(found.ID, from, until)
		if err != nil {
			return unexpectedCalendar(err)
		}
		// One entry per event, however many times it happens in the
		// window: a report answers with files, and a file that happens
		// weekly is still one file.
		seen := make(map[string]bool, len(occurrences))
		for _, occurrence := range occurrences {
			if seen[occurrence.ObjectID] {
				continue
			}
			seen[occurrence.ObjectID] = true
			object, err := tx.GetCalendarObject(found.ID, occurrence.ObjectID)
			if err != nil {
				return unexpectedCalendar(err)
			}
			if object == nil {
				// The index is derived, so a row with no event behind it
				// is this server having got something wrong rather than
				// anything the client did.
				continue
			}
			objects = append(objects, object)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

// foundEvent is one event in an answer, with the bytes exactly as stored.
func foundEvent(signedIn *session, object *models.CalendarObject, etag, length, data bool) calendarResponse {
	held := calendarProperties{}
	if etag {
		held.ETag = strconv.Quote(object.ETag)
	}
	if length {
		held.Length = strconv.Itoa(len(object.Data))
	}
	if data {
		held.ContentType = "text/calendar; charset=utf-8"
		held.CalendarData = object.Data
	}
	return calendarResponse{
		Href:     encodeHref(eventPath(signedIn.userID, object.CalendarID, object.ID)),
		PropStat: []calendarPropStat{{Prop: held, Status: statusLine(http.StatusOK)}},
	}
}
