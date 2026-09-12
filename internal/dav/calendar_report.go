package dav

import (
	"context"
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	Name         string       `xml:"name,attr"`
	IsNotDefined *struct{}    `xml:"urn:ietf:params:xml:ns:caldav is-not-defined"`
	TimeRange    *timeRange   `xml:"urn:ietf:params:xml:ns:caldav time-range"`
	Children     []compFilter `xml:"urn:ietf:params:xml:ns:caldav comp-filter"`
	PropFilters  []struct {
		Name string `xml:"name,attr"`
	} `xml:"urn:ietf:params:xml:ns:caldav prop-filter"`
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

// eventFilter is the VEVENT filter inside a query, if there is one, and
// whether the query is one this server can carry out at all.
//
// A filter this server cannot carry out is refused rather than answered
// emptily. Skipping every event would tell a client whose query was slightly
// out of spec that the calendar is empty, which a client enumerating with a
// query reads as everything having been deleted.
func (self *calendarQuery) eventFilter() (*compFilter, bool) {
	outer := self.Filter.CompFilter
	if outer.Name != "" && !strings.EqualFold(outer.Name, "VCALENDAR") {
		return nil, false
	}
	for index := range outer.Children {
		child := &outer.Children[index]
		switch {
		case strings.EqualFold(child.Name, "VEVENT"):
			return child, true
		case child.Name == "":
			continue
		default:
			// A to-do, a journal or a free-busy component. This server
			// keeps events, so a filter asking for anything else matches
			// nothing -- which is the truth rather than a refusal.
			return nil, true
		}
	}
	// No inner filter: every event in the calendar.
	return nil, true
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
		self.serveFreeBusy(writer, request, backing, body)
		return true

	case "calendar-query":
		var query calendarQuery
		if err := xml.Unmarshal(body, &query); err != nil {
			http.Error(writer, "that filter could not be read", http.StatusBadRequest)
			return true
		}
		filter, usable := query.eventFilter()
		if !usable {
			http.Error(writer, "this server does not answer that filter", http.StatusBadRequest)
			return true
		}
		objects, err := self.eventsMatching(ctx, backing, request.URL.Path, filter)
		if err != nil {
			status, message := statusOf(err)
			http.Error(writer, message, status)
			return true
		}
		for _, object := range objects {
			answers = append(answers, foundEvent(signedIn, object, wantsETag, wantsLength, wantsData))
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

	// Whose calendar this is, by address, so that "which of these
	// attendees is me" can be answered and an event they declined does not
	// make them look busy.
	theirs := make(map[string]bool)
	if signedIn.mailbox != nil {
		for _, address := range signedIn.mailbox.Addresses {
			theirs[strings.ToLower(strings.TrimSpace(address.Address))] = true
		}
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
