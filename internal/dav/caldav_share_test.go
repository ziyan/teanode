package dav_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// sharedWorld is the signed-in account (alice) and another (bertie) in a
// circle together. Bertie keeps a calendar with a dentist visit, with notes and
// guests, and a private event.
type sharedWorld struct {
	*world
	bertieCalendarID string
	circleID         string
	// A circle alice is not in, with a calendar of its own.
	strangersCalendarID string
}

const dentistVisit = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:dentist\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20300102T100000Z\r\nDTEND:20300102T110000Z\r\n" +
	"SUMMARY:Dentist\r\nLOCATION:Elm Street\r\nDESCRIPTION:Bring the form\r\n" +
	"ORGANIZER:mailto:bertie@example.com\r\nATTENDEE:mailto:guest@example.net\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Dentist\r\nEND:VALARM\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

const privateVisit = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:secret\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20300103T100000Z\r\nDTEND:20300103T110000Z\r\n" +
	"SUMMARY:Surprise party\r\nCLASS:PRIVATE\r\nLOCATION:The barn\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

const plainVisit = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:library\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20300104T100000Z\r\nDTEND:20300104T110000Z\r\n" +
	"SUMMARY:Library\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

const guestVisit = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:party\r\nDTSTAMP:20300101T000000Z\r\nDTSTART:20300105T100000Z\r\nDTEND:20300105T110000Z\r\n" +
	"SUMMARY:Party\r\nORGANIZER:mailto:alice@example.com\r\nATTENDEE:mailto:guest@example.net\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func putIn(t *testing.T, database db.Database, calendarId, data string) {
	t.Helper()
	parsed, err := calendar.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	occurrences, indexedUntil, err := calendar.Indexed(parsed)
	if err != nil {
		t.Fatalf("Indexed: %s", err)
	}
	rows := make([]models.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		rows = append(rows, models.Occurrence{StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt})
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: parsed.UID, CalendarID: calendarId, UID: parsed.UID, ETag: calendar.ETag(parsed.Data), Data: string(parsed.Data),
			Summary: parsed.Summary, Location: parsed.Location, StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt,
			Status: parsed.Status, IndexedUntil: &indexedUntil,
		}, rows); err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
	})
}

func newSharedWorld(t *testing.T) (*sharedWorld, func()) {
	t.Helper()
	here, done := newWorld(t)
	shared := &sharedWorld{world: here}
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		bertieCalendar, err := tx.CreateCalendar(&models.Calendar{UserID: here.other, Name: "Calendar"})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		shared.bertieCalendarID = bertieCalendar.ID
		circle, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, here.userID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		shared.circleID = circle.ID
		if _, err := tx.AddCircleMember(circle.ID, here.other, models.MemberRoleMember, here.userID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		// A circle alice is not in: bertie alone.
		elsewhere, err := tx.CreateCircle(&models.Circle{Name: "Elsewhere", CircleKind: models.CircleKindFriends}, here.other)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		strangers, err := tx.CreateCircleCalendar(elsewhere.ID, "Not for alice", "")
		if err != nil {
			t.Fatalf("CreateCircleCalendar: %s", err)
		}
		shared.strangersCalendarID = strangers.ID
	})
	putIn(t, here.database, shared.bertieCalendarID, dentistVisit)
	putIn(t, here.database, shared.bertieCalendarID, privateVisit)
	putIn(t, here.database, shared.bertieCalendarID, plainVisit)
	putIn(t, here.database, shared.strangersCalendarID, plainVisit)
	return shared, done
}

// share has bertie share his calendar with alice at a level.
func (self *sharedWorld) share(t *testing.T, shareLevel models.ShareLevel) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.ShareCalendar(self.other, &models.CalendarShare{
			CalendarID: self.bertieCalendarID, GranteeUserID: self.userID, ShareLevel: shareLevel,
		}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
	})
}

func (self *sharedWorld) path(calendarId string, name ...string) string {
	path := dav.Prefix + "/" + self.userID + "/calendars/" + calendarId + "/"
	if len(name) > 0 {
		path += name[0] + ".ics"
	}
	return path
}

const propfindNames = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:current-user-privilege-set/></d:prop></d:propfind>`

// homeSet is the home set as a phone reads it: the display name and whether
// the privilege set says the calendar can be written, by calendar address.
func (self *sharedWorld) homeSet(t *testing.T) map[string]string {
	t.Helper()
	answer := self.ask(t, "PROPFIND", dav.Prefix+"/"+self.userID+"/calendars/", propfindNames, "Depth", "1")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("the home set: %d %s", answer.StatusCode, body)
	}
	found := map[string]string{}
	for _, block := range regexp.MustCompile(`(?s)<response\b[^>]*>.*?</response>`).FindAllString(body, -1) {
		href := regexp.MustCompile(`<href\b[^>]*>([^<]*)</href>`).FindStringSubmatch(block)
		name := regexp.MustCompile(`<displayname\b[^>]*>([^<]*)</displayname>`).FindStringSubmatch(block)
		if href == nil || name == nil || name[1] == "" {
			continue
		}
		privilege := "read only"
		if strings.Contains(block, "<write") {
			privilege = "read write"
		}
		found[href[1]] = html.UnescapeString(name[1]) + " (" + privilege + ")"
	}
	return found
}

func TestASharedCalendarAppearsInTheHomeSetWithItsOwnersName(t *testing.T) {
	here, done := newSharedWorld(t)
	defer done()

	listing := here.homeSet(t)
	if len(listing) != 2 {
		t.Fatalf("before anything is shared: only the calendar and the reminders list: %v", listing)
	}
	if got := listing[here.path(here.calendarID)]; got != "Calendar (read write)" {
		t.Fatalf("their own calendar: %q", got)
	}

	// Busy is not a calendar over CalDAV.
	here.share(t, models.ShareLevelBusy)
	if listing = here.homeSet(t); len(listing) != 2 {
		t.Fatalf("a calendar shared at busy is not listed: %v", listing)
	}

	here.share(t, models.ShareLevelTitles)
	listing = here.homeSet(t)
	if got := listing[here.path(here.bertieCalendarID)]; got != "bertie's calendar (read only)" {
		t.Fatalf("the shared calendar is read only and named for its owner: %v", listing)
	}
	if got := listing[here.path(here.calendarID)]; got != "Calendar (read write)" {
		t.Fatalf("their own calendar is still writable: %q", got)
	}
	// Neither the other circle's calendar nor anything unshared.
	if _, listed := listing[here.path(here.strangersCalendarID)]; listed {
		t.Fatalf("a circle alice is not in: %v", listing)
	}

	// A circle's own calendar is writable for every member, under its name.
	var circleCalendarID string
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		made, err := tx.CreateCircleCalendar(here.circleID, "The household", "")
		if err != nil {
			t.Fatalf("CreateCircleCalendar: %s", err)
		}
		circleCalendarID = made.ID
	})
	listing = here.homeSet(t)
	if got := listing[here.path(circleCalendarID)]; got != "The household (read write)" {
		t.Fatalf("the circle calendar: %v", listing)
	}

	here.share(t, models.ShareLevelEdit)
	if got := here.homeSet(t)[here.path(here.bertieCalendarID)]; got != "bertie's calendar (read write)" {
		t.Fatalf("at edit it is writable: %q", got)
	}
}

func TestASharedCalendarIsReadAtItsLevelOverCalDAV(t *testing.T) {
	here, done := newSharedWorld(t)
	defer done()
	here.share(t, models.ShareLevelTitles)

	fetch := func(name string) string {
		answer := here.ask(t, http.MethodGet, here.path(here.bertieCalendarID, name), "")
		body := text(t, answer)
		if answer.StatusCode != http.StatusOK {
			t.Fatalf("fetching %s: %d %s", name, answer.StatusCode, body)
		}
		return strings.ReplaceAll(body, "\r\n ", "")
	}
	visit := fetch("dentist")
	if !strings.Contains(visit, "SUMMARY:Dentist") || !strings.Contains(visit, "LOCATION:Elm Street") ||
		strings.Contains(visit, "Bring the form") || strings.Contains(visit, "guest@example.net") || strings.Contains(visit, "VALARM") {
		t.Fatalf("at titles a fetch is the title and the place:\n%s", visit)
	}
	if secret := fetch("secret"); strings.Contains(secret, "Surprise") || strings.Contains(secret, "barn") ||
		!strings.Contains(secret, "SUMMARY:Busy") || !strings.Contains(secret, "DTSTART:20300103T100000Z") {
		t.Fatalf("a private event is busy:\n%s", secret)
	}

	// A listing and a report are redacted the same way, and the version a
	// listing names is the version a fetch names.
	answer := here.ask(t, "REPORT", here.path(here.bertieCalendarID), `<?xml version="1.0"?>
		<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <d:prop><d:getetag/><c:calendar-data/></d:prop>
		  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT">
		    <c:time-range start="20300101T000000Z" end="20300110T000000Z"/>
		  </c:comp-filter></c:comp-filter></c:filter>
		</c:calendar-query>`, "Depth", "1")
	report := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("a query: %d %s", answer.StatusCode, report)
	}
	for _, leaked := range []string{"Bring the form", "guest@example.net", "Surprise", "The barn", "VALARM"} {
		if strings.Contains(report, leaked) {
			t.Errorf("a report leaks %q:\n%s", leaked, report)
		}
	}
	if strings.Count(report, "<response ") != 3 {
		t.Errorf("three events in the report: %s", report)
	}
	listing := here.ask(t, "PROPFIND", here.path(here.bertieCalendarID), propfindETags, "Depth", "1")
	listed := text(t, listing)
	etag := regexp.MustCompile(`<getetag\b[^>]*>[^<]+</getetag>`)
	headed := here.ask(t, http.MethodGet, here.path(here.bertieCalendarID, "dentist"), "")
	_ = text(t, headed)
	if !strings.Contains(listed, strings.Trim(headed.Header.Get("ETag"), `"`)) || len(etag.FindAllString(listed, -1)) != 3 {
		t.Errorf("the listing names the versions a fetch gives:\n%s\n%s", listed, headed.Header.Get("ETag"))
	}

	// The owner's own copy is whole.
	var stored string
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		object, err := tx.GetCalendarObject(here.bertieCalendarID, "dentist")
		if err != nil || object == nil {
			t.Fatalf("GetCalendarObject: %v %v", object, err)
		}
		stored = object.Data
	})
	if !strings.Contains(stored, "Bring the form") {
		t.Fatalf("what is stored is untouched by anybody's reading of it")
	}

	// Somebody's calendar that was not shared, and a circle alice is not in,
	// are not there.
	for _, calendarId := range []string{here.strangersCalendarID, "nothing"} {
		for _, method := range []string{http.MethodGet, "PROPFIND"} {
			answer := here.ask(t, method, here.path(calendarId, "library"), "", "Depth", "0")
			_ = text(t, answer)
			if method == http.MethodGet && answer.StatusCode != http.StatusNotFound {
				t.Errorf("GET in %s: %d", calendarId, answer.StatusCode)
			}
		}
		answer := here.ask(t, "PROPFIND", here.path(calendarId), propfindETags, "Depth", "1")
		_ = text(t, answer)
		if answer.StatusCode != http.StatusNotFound {
			t.Errorf("a calendar alice does not reach (%s): %d", calendarId, answer.StatusCode)
		}
	}

	// Details are the whole event, less the alarm.
	here.share(t, models.ShareLevelDetails)
	if details := fetch("dentist"); !strings.Contains(details, "SUMMARY:Dentist") || !strings.Contains(details, "Bring the form") || !strings.Contains(details, "guest@example.net") || strings.Contains(details, "VALARM") {
		t.Fatalf("at details:\n%s", details)
	}
}

func TestAWriteBelowEditIsRefusedBeforeTheLibrarySeesIt(t *testing.T) {
	here, done := newSharedWorld(t)
	defer done()
	for _, level := range []models.ShareLevel{models.ShareLevelTitles, models.ShareLevelDetails} {
		here.share(t, level)
		for name, request := range map[string]struct{ method, path, body string }{
			"put a good event":    {http.MethodPut, here.path(here.bertieCalendarID, "new"), plainVisit},
			"put garbage":         {http.MethodPut, here.path(here.bertieCalendarID, "garbage"), "this is not a calendar"},
			"replace an event":    {http.MethodPut, here.path(here.bertieCalendarID, "library"), plainVisit},
			"delete":              {http.MethodDelete, here.path(here.bertieCalendarID, "library"), ""},
			"delete one absent":   {http.MethodDelete, here.path(here.bertieCalendarID, "absent"), ""},
			"make a collection":   {"MKCOL", here.path(here.bertieCalendarID, "inside"), ""},
			"change properties":   {"PROPPATCH", here.path(here.bertieCalendarID), `<?xml version="1.0"?><d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:displayname>Mine</d:displayname></d:prop></d:set></d:propertyupdate>`},
			"a method of its own": {"FROBNICATE", here.path(here.bertieCalendarID, "library"), ""},
		} {
			answer := here.ask(t, request.method, request.path, request.body, "Content-Type", "text/calendar; charset=utf-8")
			said := text(t, answer)
			if answer.StatusCode != http.StatusForbidden || !strings.Contains(said, "edit") {
				t.Errorf("%s at %s: %d %q; a 403 that says what to ask for", name, level, answer.StatusCode, said)
			}
		}
	}
	// Nothing was written, and the event is as it was.
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		count, err := tx.CountCalendarObjects(here.bertieCalendarID)
		if err != nil || count != 3 {
			t.Fatalf("still three events: %d %v", count, err)
		}
	})
	// Garbage to the person's own calendar is the library's answer, not ours.
	answer := here.ask(t, http.MethodPut, here.eventPath("garbage"), "this is not a calendar", "Content-Type", "text/calendar; charset=utf-8")
	_ = text(t, answer)
	if answer.StatusCode == http.StatusForbidden || answer.StatusCode < 400 {
		t.Errorf("their own calendar answers garbage the way it always did: %d", answer.StatusCode)
	}
}

func TestAnEditorWritesEventsWithoutGuestsOverCalDAV(t *testing.T) {
	here, done := newSharedWorld(t)
	defer done()
	here.share(t, models.ShareLevelEdit)

	put := func(name, body string) *http.Response {
		return here.ask(t, http.MethodPut, here.path(here.bertieCalendarID, name), body, "Content-Type", "text/calendar; charset=utf-8")
	}
	answer := put("outing", strings.Replace(plainVisit, "UID:library", "UID:outing", 1))
	if _ = text(t, answer); answer.StatusCode != http.StatusCreated {
		t.Fatalf("an editor adds an event without guests: %d", answer.StatusCode)
	}
	changed := strings.Replace(plainVisit, "SUMMARY:Library", "SUMMARY:Library, moved", 1)
	if answer = put("library", changed); answer.StatusCode >= 300 {
		t.Fatalf("and changes one: %d %s", answer.StatusCode, text(t, answer))
	}
	_ = text(t, answer)

	for name, response := range map[string]*http.Response{
		"add an event with guests":    put("party", guestVisit),
		"change an event with guests": put("dentist", strings.Replace(dentistVisit, "Dentist", "Dentist, moved", 1)),
		"remove an event with guests": here.ask(t, http.MethodDelete, here.path(here.bertieCalendarID, "dentist"), ""),
		"strip an event's guests":     put("dentist", strings.Replace(plainVisit, "UID:library", "UID:dentist", 1)),
	} {
		said := text(t, response)
		if response.StatusCode != http.StatusForbidden || !strings.Contains(said, "guests") {
			t.Errorf("%s: %d %q", name, response.StatusCode, said)
		}
	}
	answer = here.ask(t, http.MethodDelete, here.path(here.bertieCalendarID, "outing"), "")
	if _ = text(t, answer); answer.StatusCode >= 300 {
		t.Errorf("an editor removes an event without guests: %d", answer.StatusCode)
	}
	// The event with guests is as bertie left it.
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		object, err := tx.GetCalendarObject(here.bertieCalendarID, "dentist")
		if err != nil || object == nil || !strings.Contains(object.Data, "guest@example.net") || strings.Contains(object.Data, "moved") {
			t.Fatalf("the meeting is untouched: %v %v", object, err)
		}
	})
}

func TestACalendarSharedAtBusyAnswersFreeBusyAndNothingElse(t *testing.T) {
	here, done := newSharedWorld(t)
	defer done()
	here.share(t, models.ShareLevelBusy)

	// Not a calendar to read.
	for method, wanted := range map[string]int{http.MethodGet: http.StatusNotFound, "PROPFIND": http.StatusNotFound} {
		path := here.path(here.bertieCalendarID, "dentist")
		answer := here.ask(t, method, path, propfindETags, "Depth", "0")
		if _ = text(t, answer); answer.StatusCode != wanted {
			t.Errorf("%s: %d", method, answer.StatusCode)
		}
	}
	answer := here.ask(t, "REPORT", here.path(here.bertieCalendarID), `<?xml version="1.0"?>
		<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-data/></d:prop>
		<c:filter><c:comp-filter name="VCALENDAR"/></c:filter></c:calendar-query>`)
	if _ = text(t, answer); answer.StatusCode != http.StatusNotFound {
		t.Errorf("a query for events at busy: %d", answer.StatusCode)
	}

	// But when is he busy, yes: the dentist and the private event, and the
	// library visit, in the window, and nothing about what any of it is.
	answer = here.ask(t, "REPORT", here.path(here.bertieCalendarID), `<?xml version="1.0"?>
		<c:free-busy-query xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <c:time-range start="20300101T000000Z" end="20300110T000000Z"/>
		</c:free-busy-query>`)
	body := text(t, answer)
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("free-busy: %d %s", answer.StatusCode, body)
	}
	for _, line := range []string{
		"FREEBUSY;FBTYPE=BUSY:20300102T100000Z/20300102T110000Z",
		"FREEBUSY;FBTYPE=BUSY:20300103T100000Z/20300103T110000Z",
		"FREEBUSY;FBTYPE=BUSY:20300104T100000Z/20300104T110000Z",
	} {
		if !strings.Contains(body, line) {
			t.Errorf("free-busy lacks %q:\n%s", line, body)
		}
	}
	for _, leaked := range []string{"Dentist", "Surprise", "Library", "Elm Street", "barn"} {
		if strings.Contains(body, leaked) {
			t.Errorf("free-busy leaks %q", leaked)
		}
	}
	// And not for a calendar that was never shared with her.
	answer = here.ask(t, "REPORT", here.path(here.strangersCalendarID), `<?xml version="1.0"?>
		<c:free-busy-query xmlns:c="urn:ietf:params:xml:ns:caldav"><c:time-range start="20300101T000000Z" end="20300110T000000Z"/></c:free-busy-query>`)
	if _ = text(t, answer); answer.StatusCode != http.StatusNotFound {
		t.Errorf("free-busy of a calendar nobody shared: %d", answer.StatusCode)
	}
}

func TestACircleCalendarIsWrittenByEveryMemberOverCalDAV(t *testing.T) {
	here, done := newSharedWorld(t)
	defer done()
	var circleCalendarID string
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		made, err := tx.CreateCircleCalendar(here.circleID, "The household", "")
		if err != nil {
			t.Fatalf("CreateCircleCalendar: %s", err)
		}
		circleCalendarID = made.ID
	})
	answer := here.ask(t, http.MethodPut, here.path(circleCalendarID, "play"), strings.Replace(plainVisit, "UID:library", "UID:play", 1),
		"Content-Type", "text/calendar; charset=utf-8")
	if _ = text(t, answer); answer.StatusCode != http.StatusCreated {
		t.Fatalf("a member adds to the circle's calendar: %d", answer.StatusCode)
	}
	answer = here.ask(t, http.MethodGet, here.path(circleCalendarID, "play"), "")
	if body := text(t, answer); answer.StatusCode != http.StatusOK || !strings.Contains(body, "SUMMARY:Library") {
		t.Fatalf("and reads it back whole: %d %s", answer.StatusCode, body)
	}
	if answer = here.ask(t, http.MethodPut, here.path(circleCalendarID, "party"), guestVisit, "Content-Type", "text/calendar; charset=utf-8"); answer.StatusCode != http.StatusForbidden {
		t.Errorf("no guests in a circle's calendar: %d %s", answer.StatusCode, text(t, answer))
	} else {
		_ = text(t, answer)
	}
	if answer = here.ask(t, http.MethodDelete, here.path(circleCalendarID, "play"), ""); answer.StatusCode >= 300 {
		t.Errorf("a member removes an event: %d", answer.StatusCode)
	}
	_ = text(t, answer)
	// Another circle's calendar is not found, for reading or writing.
	for _, request := range []struct{ method, name string }{{http.MethodGet, "library"}, {http.MethodPut, "mine"}, {http.MethodDelete, "library"}} {
		answer := here.ask(t, request.method, here.path(here.strangersCalendarID, request.name), plainVisit, "Content-Type", "text/calendar; charset=utf-8")
		if _ = text(t, answer); answer.StatusCode != http.StatusNotFound {
			t.Errorf("%s in a circle alice is not in: %d", request.method, answer.StatusCode)
		}
	}

	// Alice leaves the circle (bertie takes over as admin), and the calendar
	// goes with her.
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		if _, err := tx.SetCircleMemberRole(here.circleID, here.other, models.MemberRoleAdmin); err != nil {
			t.Fatalf("SetCircleMemberRole: %s", err)
		}
		if _, err := tx.RemoveCircleMember(here.circleID, here.userID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
	})
	if _, listed := here.homeSet(t)[here.path(circleCalendarID)]; listed {
		t.Errorf("a calendar of a circle alice left is still listed")
	}
	answer = here.ask(t, http.MethodGet, here.path(circleCalendarID, "play"), "")
	if _ = text(t, answer); answer.StatusCode != http.StatusNotFound {
		t.Errorf("and cannot be read: %d", answer.StatusCode)
	}
}
