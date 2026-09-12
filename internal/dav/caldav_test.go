package dav_test

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/dav"
)

// An event as a phone sends one: a zone described beside it, a repeat, a
// cancelled occurrence, people, and a property this server has never heard of.
const anEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Apple Inc.//iOS 26.0//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:9C1F2A3B-4D5E-6789-ABCD-EF0123456789\r\n" +
	"DTSTAMP:20260912T120000Z\r\nDTSTART:20260914T100000Z\r\nDTEND:20260914T110000Z\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=5\r\nEXDATE:20260921T100000Z\r\n" +
	"SUMMARY:Weekly sync\r\nLOCATION:The small room\r\n" +
	"ORGANIZER;CN=\"Hopper, Grace\":mailto:grace@example.com\r\n" +
	"ATTENDEE;CN=Ada Lovelace;PARTSTAT=ACCEPTED:mailto:ada@example.com\r\n" +
	"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

func (self *world) calendarPath() string {
	return dav.Prefix + "/" + self.userID + "/calendars/" + self.calendarID + "/"
}

func (self *world) eventPath(name string) string {
	return self.calendarPath() + name + ".ics"
}

// putEvent keeps one and returns the ETag the server gave it.
func (self *world) putEvent(t *testing.T, name, body string, headers ...string) string {
	t.Helper()
	answer := self.ask(t, http.MethodPut, self.eventPath(name), body,
		append([]string{"Content-Type", "text/calendar; charset=utf-8"}, headers...)...)
	defer func() { _ = text(t, answer) }()
	if answer.StatusCode != http.StatusCreated && answer.StatusCode != http.StatusNoContent &&
		answer.StatusCode != http.StatusOK {
		t.Fatalf("keeping an event: %d", answer.StatusCode)
	}
	return answer.Header.Get("ETag")
}

// A calendar client finds the calendar from the principal, and the well-known
// address sends it there.
func TestACalendarClientFindsTheCalendar(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	// Both home sets in one question, which is what a client that does
	// both protocols actually asks.
	answer := here.ask(t, "PROPFIND", dav.Prefix+"/", `<?xml version="1.0"?>
		<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"
		            xmlns:a="urn:ietf:params:xml:ns:carddav">
		  <d:prop><d:current-user-principal/><c:calendar-home-set/><a:addressbook-home-set/></d:prop>
		</d:propfind>`, "Depth", "0")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("asking who I am: %d %s", answer.StatusCode, body)
	}
	if !strings.Contains(body, "/calendars/") {
		t.Fatalf("the principal must say where the calendars are:\n%s", body)
	}
	// And the address books are still advertised beside them: adding one
	// home set must not have replaced the other, which is exactly what a
	// list built by assignment rather than by appending would have done.
	if !strings.Contains(body, "/contacts/") {
		t.Fatalf("the address books are still there too:\n%s", body)
	}

	// The well-known address a client tries when somebody types only a
	// mail address. It used to be answered "this server does not serve
	// calendars", which is no longer true.
	found := here.ask(t, "PROPFIND", "/.well-known/caldav", "", "Depth", "0")
	_ = text(t, found)
	if found.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("the well-known address points at the mount: %d", found.StatusCode)
	}
	if found.Header.Get("Location") != dav.Prefix+"/" {
		t.Fatalf("and points at it: %q", found.Header.Get("Location"))
	}
}

// An event kept by a device comes back exactly as it was stored, with the
// length that was advertised for it.
func TestAnEventComesBackAsItWasKept(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	etag := here.putEvent(t, "one", anEvent)
	if etag == "" {
		t.Fatal("keeping an event names the version")
	}

	answer := here.ask(t, http.MethodGet, here.eventPath("one"), "")
	body := text(t, answer)
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("fetching it: %d %s", answer.StatusCode, body)
	}
	if answer.Header.Get("ETag") != etag {
		t.Fatalf("the same version: %q %q", answer.Header.Get("ETag"), etag)
	}
	// The length said and the length sent agree. They disagreed on the
	// contacts side for a long time, because the answer was computed by
	// re-encoding rather than by measuring what is served.
	declared, err := strconv.Atoi(answer.Header.Get("Content-Length"))
	if err != nil || declared != len(body) {
		t.Fatalf("the length declared is the length sent: %q against %d",
			answer.Header.Get("Content-Length"), len(body))
	}
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	for _, want := range []string{
		"UID:9C1F2A3B-4D5E-6789-ABCD-EF0123456789",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=5",
		"EXDATE:20260921T100000Z",
		`ORGANIZER;CN="Hopper, Grace":mailto:grace@example.com`,
		"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC",
	} {
		if !strings.Contains(unfolded, want) {
			t.Fatalf("what came back lost %q:\n%s", want, body)
		}
	}
	// Every line is within what the format asks for, which the library's
	// own encoder does not do.
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("a line of %d octets was served: %q", len(line), line)
		}
	}
}

// Listing a calendar is how a device works out what changed, so it has to
// carry the version of every event in it.
func TestListingACalendarNamesEveryVersion(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	etag := here.putEvent(t, "one", anEvent)
	answer := here.ask(t, "PROPFIND", here.calendarPath(), propfindETags, "Depth", "1")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("listing: %d %s", answer.StatusCode, body)
	}
	if !strings.Contains(body, "one.ics") {
		t.Fatalf("the event is listed:\n%s", body)
	}
	if !strings.Contains(body, strings.Trim(etag, `"`)) {
		t.Fatalf("with the version a fetch would return:\n%s", body)
	}
}

// A calendar-query with a time range answers with the events in the window
// and no others -- including a repeating one, on a week its first occurrence
// is nowhere near.
func TestAQueryAnswersWithWhatIsInTheWindow(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "weekly", anEvent)
	here.putEvent(t, "later", strings.NewReplacer(
		"9C1F2A3B-4D5E-6789-ABCD-EF0123456789", "later-one",
		"DTSTART:20260914T100000Z", "DTSTART:20261210T100000Z",
		"DTEND:20260914T110000Z", "DTEND:20261210T110000Z",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=5\r\n", "",
		"EXDATE:20260921T100000Z\r\n", "",
		"Weekly sync", "In December",
	).Replace(anEvent))

	query := func(from, until string) string {
		t.Helper()
		answer := here.ask(t, "REPORT", here.calendarPath(), `<?xml version="1.0"?>
			<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
			  <d:prop><d:getetag/><c:calendar-data/></d:prop>
			  <c:filter><c:comp-filter name="VCALENDAR">
			    <c:comp-filter name="VEVENT">
			      <c:time-range start="`+from+`" end="`+until+`"/>
			    </c:comp-filter></c:comp-filter></c:filter>
			</c:calendar-query>`, "Depth", "1")
		body := text(t, answer)
		if answer.StatusCode != http.StatusMultiStatus {
			t.Fatalf("querying: %d %s", answer.StatusCode, body)
		}
		return body
	}

	// The week the repeat starts.
	september := query("20260913T000000Z", "20260920T000000Z")
	if !strings.Contains(september, "weekly.ics") {
		t.Fatalf("the weekly event is on that week:\n%s", september)
	}
	if strings.Contains(september, "later.ics") {
		t.Fatalf("the December one is not:\n%s", september)
	}

	// A week in October, which only the repeat reaches -- and it reaches it
	// through the index of when things happen rather than through its own
	// start date, which is three weeks earlier.
	october := query("20261004T000000Z", "20261011T000000Z")
	if !strings.Contains(october, "weekly.ics") {
		t.Fatalf("the repeat reaches October:\n%s", october)
	}

	// The week its cancelled occurrence would have been in.
	cancelled := query("20260920T000000Z", "20260927T000000Z")
	if strings.Contains(cancelled, "weekly.ics") {
		t.Fatalf("the cancelled occurrence does not happen:\n%s", cancelled)
	}

	// And December finds the other one.
	december := query("20261207T000000Z", "20261214T000000Z")
	if !strings.Contains(december, "later.ics") || strings.Contains(december, "weekly.ics") {
		t.Fatalf("December has the December one and nothing else:\n%s", december)
	}
}

// A calendar-multiget of an href exactly as it was listed returns the event,
// rather than a 404 -- which is what happened on the contacts side until the
// href was percent-decoded on the way in.
func TestAnEventHrefSurvivesBeingListedAndSentBack(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	// A name with a space in it, which a listing writes as %20.
	here.putEvent(t, "team meeting", anEvent)
	listed := text(t, here.ask(t, "PROPFIND", here.calendarPath(), propfindETags, "Depth", "1"))
	found := regexp.MustCompile(`<href[^>]*>([^<]*\.ics)</href>`).FindStringSubmatch(listed)
	if found == nil {
		t.Fatalf("the listing names the event:\n%s", listed)
	}
	href := found[1]
	if !strings.Contains(href, "%20") {
		t.Fatalf("the listing percent-encodes the name: %q", href)
	}

	answer := here.ask(t, "REPORT", here.calendarPath(), `<?xml version="1.0"?>
		<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <d:prop><d:getetag/><c:calendar-data/></d:prop>
		  <d:href>`+href+`</d:href>
		</c:calendar-multiget>`, "Depth", "1")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("multiget: %d %s", answer.StatusCode, body)
	}
	if !strings.Contains(body, "Weekly sync") {
		t.Fatalf("the event the client named came back:\n%s", body)
	}
	if strings.Contains(body, "404") {
		t.Fatalf("and was not reported missing:\n%s", body)
	}
}

// A report carries the stored bytes, not a re-encoding of them: the length
// and the data have to agree with what a fetch returns, because that is the
// promise the version makes.
func TestAReportCarriesWhatAFetchWouldReturn(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "one", anEvent)
	fetched := text(t, here.ask(t, http.MethodGet, here.eventPath("one"), ""))

	answer := here.ask(t, "REPORT", here.calendarPath(), `<?xml version="1.0"?>
		<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <d:prop><d:getetag/><d:getcontentlength/><c:calendar-data/></d:prop>
		  <d:href>`+here.eventPath("one")+`</d:href>
		</c:calendar-multiget>`, "Depth", "1")
	body := text(t, answer)
	declared := regexp.MustCompile(`<getcontentlength[^>]*>(\d+)</getcontentlength>`).FindStringSubmatch(body)
	if declared == nil {
		t.Fatalf("the report says how long the event is:\n%s", body)
	}
	if declared[1] != strconv.Itoa(len(fetched)) {
		t.Fatalf("the report says %s and a fetch returns %d octets", declared[1], len(fetched))
	}
}

// Two devices editing the same event at once cannot silently overwrite one
// another, and a device holding a stale copy cannot delete an edit it has
// never seen.
func TestTwoDevicesCannotOverwriteOneAnother(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	etag := here.putEvent(t, "one", anEvent)

	// A write conditional on a version that is no longer there.
	stale := here.ask(t, http.MethodPut, here.eventPath("one"),
		strings.Replace(anEvent, "Weekly sync", "Moved", 1),
		"Content-Type", "text/calendar; charset=utf-8", "If-Match", `"not-the-version"`)
	_ = text(t, stale)
	if stale.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("a stale write is refused: %d", stale.StatusCode)
	}

	// Creating something that is already there.
	exists := here.ask(t, http.MethodPut, here.eventPath("one"), anEvent,
		"Content-Type", "text/calendar; charset=utf-8", "If-None-Match", "*")
	_ = text(t, exists)
	if exists.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("creating over something is refused: %d", exists.StatusCode)
	}

	// A delete conditional on a stale version.
	staleDelete := here.ask(t, http.MethodDelete, here.eventPath("one"), "",
		"If-Match", `"not-the-version"`)
	_ = text(t, staleDelete)
	if staleDelete.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("a stale delete is refused: %d", staleDelete.StatusCode)
	}

	// And with the version it actually holds, it works.
	good := here.ask(t, http.MethodDelete, here.eventPath("one"), "", "If-Match", etag)
	_ = text(t, good)
	if good.StatusCode != http.StatusNoContent && good.StatusCode != http.StatusOK {
		t.Fatalf("the right version deletes: %d", good.StatusCode)
	}
}

// A file claiming an identifier that belongs to an event kept under another
// name is a conflict, not a silent overwrite.
func TestAnIdentifierAlreadyHeldIsAConflict(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "one", anEvent)
	answer := here.ask(t, http.MethodPut, here.eventPath("two"), anEvent,
		"Content-Type", "text/calendar; charset=utf-8")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusConflict {
		t.Fatalf("the same event under a second name is a conflict: %d", answer.StatusCode)
	}
}

// Somebody else's calendar is not reachable with your password.
func TestAnotherPersonsCalendarIsRefused(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	answer := here.ask(t, "PROPFIND", dav.Prefix+"/"+here.other+"/calendars/", propfindETags, "Depth", "1")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusForbidden {
		t.Fatalf("somebody else's calendars: %d", answer.StatusCode)
	}
}

// A collection asked for without its trailing slash is answered, not
// redirected. A redirect turns a PROPFIND into a GET, and the GET is then
// refused as a method the collection does not take -- which is how a
// calendar client reports that the server does not work at all.
func TestACalendarWithoutItsSlashIsAnsweredNotRedirected(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	for _, path := range []string{
		dav.Prefix + "/" + here.userID + "/calendars",
		strings.TrimSuffix(here.calendarPath(), "/"),
	} {
		answer := here.ask(t, "PROPFIND", path, propfindETags, "Depth", "1")
		_ = text(t, answer)
		if answer.StatusCode == http.StatusMovedPermanently || answer.StatusCode == http.StatusFound ||
			answer.StatusCode == http.StatusTemporaryRedirect || answer.StatusCode == http.StatusPermanentRedirect {
			t.Fatalf("%s was redirected (%d), which turns a PROPFIND into a GET", path, answer.StatusCode)
		}
		if answer.StatusCode != http.StatusMultiStatus {
			t.Fatalf("%s: %d", path, answer.StatusCode)
		}
	}
}

// A filter this server cannot carry out is refused rather than answered
// emptily, because a client enumerating with a query reads an empty answer as
// everything having been deleted.
func TestACalendarFilterThisServerCannotCarryOutIsRefused(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "one", anEvent)
	answer := here.ask(t, "REPORT", here.calendarPath(), `<?xml version="1.0"?>
		<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <d:prop><d:getetag/></d:prop>
		  <c:filter><c:comp-filter name="VEVENT">
		    <c:time-range start="nonsense" end="alsononsense"/>
		  </c:comp-filter></c:filter>
		</c:calendar-query>`, "Depth", "1")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusBadRequest {
		t.Fatalf("a filter this server cannot read: %d", answer.StatusCode)
	}
}

// A file that is not a calendar is refused rather than kept.
func TestSomethingThatIsNotAnEventIsRefused(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	answer := here.ask(t, http.MethodPut, here.eventPath("rubbish"), "this is not a calendar\r\n",
		"Content-Type", "text/calendar; charset=utf-8")
	_ = text(t, answer)
	if answer.StatusCode < 400 || answer.StatusCode >= 500 {
		t.Fatalf("rubbish should be refused as the client's fault: %d", answer.StatusCode)
	}
}

// event builds one, so a test can say what it is about rather than repeat a
// file each time.
func event(uid, summary, starts, ends string, extra ...string) string {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN", "BEGIN:VEVENT",
		"UID:" + uid, "DTSTAMP:20260912T120000Z",
		"DTSTART:" + starts, "DTEND:" + ends, "SUMMARY:" + summary,
	}
	lines = append(lines, extra...)
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	return strings.Join(lines, "\r\n") + "\r\n"
}

func (self *world) freeBusy(t *testing.T, from, until string) (int, string) {
	t.Helper()
	answer := self.ask(t, "REPORT", self.calendarPath(), `<?xml version="1.0"?>
		<c:free-busy-query xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <c:time-range start="`+from+`" end="`+until+`"/>
		</c:free-busy-query>`, "Depth", "1")
	return answer.StatusCode, text(t, answer)
}

// Asking when somebody is busy is answered, and answered with a calendar
// rather than with XML -- which is the one place in this protocol where a
// REPORT is not a multistatus.
func TestFreeBusySaysWhenSomebodyIsBusy(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "morning", event("morning", "Standup", "20260914T090000Z", "20260914T093000Z"))
	here.putEvent(t, "afternoon", event("afternoon", "Review", "20260914T140000Z", "20260914T150000Z"))

	status, body := here.freeBusy(t, "20260914T000000Z", "20260915T000000Z")
	if status != http.StatusOK {
		t.Fatalf("asking when somebody is busy: %d %s", status, body)
	}
	if !strings.Contains(body, "BEGIN:VFREEBUSY") {
		t.Fatalf("answered with a calendar:\n%s", body)
	}
	for _, want := range []string{
		"FREEBUSY;FBTYPE=BUSY:20260914T090000Z/20260914T093000Z",
		"FREEBUSY;FBTYPE=BUSY:20260914T140000Z/20260914T150000Z",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q:\n%s", want, body)
		}
	}
	// And it says nothing about what any of it is. This is the whole
	// point: somebody arranging a meeting is told the time is taken, not
	// what it is taken by.
	for _, leaked := range []string{"Standup", "Review", "SUMMARY", "LOCATION", "ATTENDEE"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("free-busy leaked %q:\n%s", leaked, body)
		}
	}
}

// Two meetings that touch are one stretch of unavailability, not two: a
// client drawing them separately would show a gap that is not there.
func TestBusyStretchesThatTouchAreOne(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "first", event("first", "One", "20260914T090000Z", "20260914T100000Z"))
	here.putEvent(t, "second", event("second", "Two", "20260914T100000Z", "20260914T110000Z"))
	// And one inside the first, which must not add a third stretch.
	here.putEvent(t, "inside", event("inside", "Three", "20260914T091500Z", "20260914T094500Z"))

	status, body := here.freeBusy(t, "20260914T000000Z", "20260915T000000Z")
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, body)
	}
	if count := strings.Count(body, "FREEBUSY;FBTYPE"); count != 1 {
		t.Fatalf("one stretch from nine to eleven, not %d:\n%s", count, body)
	}
	if !strings.Contains(body, "20260914T090000Z/20260914T110000Z") {
		t.Fatalf("nine to eleven:\n%s", body)
	}
}

// An event that says it does not make its owner busy does not, and neither
// does one they declined or one that was cancelled. Each of these is a way to
// tell somebody a time is taken when it is free.
func TestWhatDoesNotMakeSomebodyBusy(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "transparent",
		event("transparent", "Reminder", "20260914T090000Z", "20260914T100000Z", "TRANSP:TRANSPARENT"))
	here.putEvent(t, "declined",
		event("declined", "Not going", "20260914T110000Z", "20260914T120000Z",
			"ORGANIZER:mailto:grace@example.com",
			"ATTENDEE;PARTSTAT=DECLINED:mailto:alice@example.com"))
	here.putEvent(t, "cancelled",
		event("cancelled", "Called off", "20260914T130000Z", "20260914T140000Z", "STATUS:CANCELLED"))
	here.putEvent(t, "birthday",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n"+
			"UID:birthday\r\nDTSTAMP:20260912T120000Z\r\nDTSTART;VALUE=DATE:20260914\r\n"+
			"DTEND;VALUE=DATE:20260915\r\nSUMMARY:Ada's birthday\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	// And one that does, so the test is not passing because nothing was
	// stored or because the query is broken.
	here.putEvent(t, "real", event("real", "A real meeting", "20260914T150000Z", "20260914T160000Z"))

	status, body := here.freeBusy(t, "20260914T000000Z", "20260915T000000Z")
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, body)
	}
	if !strings.Contains(body, "20260914T150000Z/20260914T160000Z") {
		t.Fatalf("the real meeting makes them busy:\n%s", body)
	}
	if count := strings.Count(body, "FREEBUSY;FBTYPE"); count != 1 {
		t.Fatalf("only the real meeting, not %d stretches:\n%s", count, body)
	}
	// And an attendee who is somebody else declining says nothing about
	// whether the owner is going.
	here.putEvent(t, "someoneElseDeclined",
		event("someoneElseDeclined", "Still on", "20260914T170000Z", "20260914T180000Z",
			"ORGANIZER:mailto:alice@example.com",
			"ATTENDEE;PARTSTAT=DECLINED:mailto:grace@example.com"))
	status, body = here.freeBusy(t, "20260914T000000Z", "20260915T000000Z")
	if status != http.StatusOK || !strings.Contains(body, "20260914T170000Z/20260914T180000Z") {
		t.Fatalf("somebody else declining does not free the owner: %d\n%s", status, body)
	}
}

// A stretch reaching outside the window is cut to it: a client asked about a
// window, and a period reaching past it says more about somebody's day than
// they agreed to tell.
func TestBusyStretchesAreCutToTheWindow(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "conference",
		event("conference", "Conference", "20260914T090000Z", "20260918T170000Z"))
	status, body := here.freeBusy(t, "20260916T000000Z", "20260917T000000Z")
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, body)
	}
	if !strings.Contains(body, "20260916T000000Z/20260917T000000Z") {
		t.Fatalf("cut to the day asked about:\n%s", body)
	}
	if strings.Contains(body, "20260914") || strings.Contains(body, "20260918") {
		t.Fatalf("and says nothing about the days either side:\n%s", body)
	}
}

// A repeat makes its owner busy every time it happens, not only the first.
func TestARepeatMakesSomebodyBusyEveryTime(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	here.putEvent(t, "weekly",
		event("weekly", "Weekly", "20260914T100000Z", "20260914T110000Z",
			"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=4"))
	// A week three repeats in, which the event's own start date is nowhere
	// near.
	status, body := here.freeBusy(t, "20261005T000000Z", "20261006T000000Z")
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, body)
	}
	if !strings.Contains(body, "20261005T100000Z/20261005T110000Z") {
		t.Fatalf("the fourth Monday:\n%s", body)
	}
}

// A free-busy request with no window is refused rather than answered for all
// of time.
func TestFreeBusyNeedsAWindow(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	answer := here.ask(t, "REPORT", here.calendarPath(), `<?xml version="1.0"?>
		<c:free-busy-query xmlns:c="urn:ietf:params:xml:ns:caldav"/>`, "Depth", "1")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusBadRequest {
		t.Fatalf("a free-busy with no window: %d", answer.StatusCode)
	}
}
