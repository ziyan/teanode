package dav_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// reminder builds one as a phone's Reminders app writes it, so a test can say
// what it is about.
func reminder(uid, title string, extra ...string) string {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//Reminders//EN", "BEGIN:VTODO",
		"UID:" + uid, "DTSTAMP:20260928T120000Z", "SUMMARY:" + title,
	}
	lines = append(lines, extra...)
	lines = append(lines, "END:VTODO", "END:VCALENDAR")
	return strings.Join(lines, "\r\n") + "\r\n"
}

// remindersPath is the person's reminders list, as a client finds it: by
// listing the calendar home, which is what makes it.
func (self *world) remindersPath(t *testing.T) string {
	t.Helper()
	answer := self.ask(t, "PROPFIND", dav.Prefix+"/"+self.userID+"/calendars/", `<?xml version="1.0"?>
		<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <d:prop><d:displayname/><c:supported-calendar-component-set/></d:prop>
		</d:propfind>`, "Depth", "1")
	_ = text(t, answer)
	var found *models.Calendar
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		calendars, err := tx.ListCalendars(self.userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, each := range calendars {
			if each.CalendarKind == models.CalendarReminders {
				found = each
			}
		}
	})
	if found == nil {
		t.Fatal("listing the calendar home made no reminders list")
	}
	return dav.Prefix + "/" + self.userID + "/calendars/" + found.ID + "/"
}

// The Reminders app finds a list that takes reminders beside the calendar,
// and the calendar still takes events only.
func TestAPhoneFindsARemindersListBesideTheCalendar(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	answer := here.ask(t, "PROPFIND", dav.Prefix+"/"+here.userID+"/calendars/", `<?xml version="1.0"?>
		<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
		  <d:prop><d:displayname/><c:supported-calendar-component-set/></d:prop>
		</d:propfind>`, "Depth", "1")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("listing the home: %d", answer.StatusCode)
	}
	if !strings.Contains(body, "Reminders") || !strings.Contains(body, `name="VTODO"`) || !strings.Contains(body, `name="VEVENT"`) {
		t.Fatalf("a list offering reminders and a calendar offering events:\n%s", body)
	}
	// Asked again, still one list.
	_ = here.remindersPath(t)
	_ = here.remindersPath(t)
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		calendars, _ := tx.ListCalendars(here.userID)
		if len(calendars) != 2 {
			t.Fatalf("one calendar and one reminders list: %d", len(calendars))
		}
	})
}

// A reminder goes in, comes back as it went, is found by the query the app
// makes for what is not done, is ticked off, and is removed.
func TestAReminderIsKeptTickedOffAndRemoved(t *testing.T) {
	here, done := newWorld(t)
	defer done()
	list := here.remindersPath(t)
	address := list + "buy-stamps.ics"

	answer := here.ask(t, http.MethodPut, address, reminder("stamps@example.com", "Buy stamps", "DUE;VALUE=DATE:20260929", "STATUS:NEEDS-ACTION"),
		"Content-Type", "text/calendar; charset=utf-8", "If-None-Match", "*")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusCreated {
		t.Fatalf("keeping a reminder: %d", answer.StatusCode)
	}
	fetched := here.ask(t, http.MethodGet, address, "")
	if body := text(t, fetched); fetched.StatusCode != http.StatusOK || !strings.Contains(body, "SUMMARY:Buy stamps") || !strings.Contains(body, "BEGIN:VTODO") {
		t.Fatalf("fetched: %d\n%s", fetched.StatusCode, body)
	}

	undone := func() string {
		answer := here.ask(t, "REPORT", list, `<?xml version="1.0"?>
			<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
			  <d:prop><d:getetag/></d:prop>
			  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VTODO">
			    <c:prop-filter name="COMPLETED"><c:is-not-defined/></c:prop-filter>
			  </c:comp-filter></c:comp-filter></c:filter>
			</c:calendar-query>`, "Depth", "1")
		body := text(t, answer)
		if answer.StatusCode != http.StatusMultiStatus {
			t.Fatalf("the query for what is not done: %d", answer.StatusCode)
		}
		return body
	}
	if !strings.Contains(undone(), "buy-stamps.ics") {
		t.Fatal("an open reminder is found by the query for what is not done")
	}

	answer = here.ask(t, http.MethodPut, address, reminder("stamps@example.com", "Buy stamps", "DUE;VALUE=DATE:20260929", "STATUS:COMPLETED", "COMPLETED:20260929T101500Z"),
		"Content-Type", "text/calendar; charset=utf-8")
	_ = text(t, answer)
	if answer.StatusCode != http.StatusNoContent && answer.StatusCode != http.StatusOK && answer.StatusCode != http.StatusCreated {
		t.Fatalf("ticking it off: %d", answer.StatusCode)
	}
	if strings.Contains(undone(), "buy-stamps.ics") {
		t.Fatal("a reminder ticked off is no longer among what is not done")
	}

	// An event in the reminders list, and a reminder in the calendar, are
	// each refused with the status for a component the collection does not
	// take.
	wrong := here.ask(t, http.MethodPut, list+"standup.ics", event("standup@example.com", "Standup", "20260929T090000Z", "20260929T091500Z"),
		"Content-Type", "text/calendar; charset=utf-8")
	_ = text(t, wrong)
	if wrong.StatusCode != http.StatusForbidden {
		t.Fatalf("an event in the reminders list: %d", wrong.StatusCode)
	}
	wrong = here.ask(t, http.MethodPut, here.eventPath("stamps"), reminder("other@example.com", "Buy stamps"),
		"Content-Type", "text/calendar; charset=utf-8")
	_ = text(t, wrong)
	if wrong.StatusCode != http.StatusForbidden {
		t.Fatalf("a reminder in the calendar: %d", wrong.StatusCode)
	}

	removed := here.ask(t, http.MethodDelete, address, "")
	_ = text(t, removed)
	if removed.StatusCode != http.StatusNoContent && removed.StatusCode != http.StatusOK {
		t.Fatalf("removing it: %d", removed.StatusCode)
	}
}
