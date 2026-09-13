package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const oneEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//TeaNode//EN\r\nBEGIN:VEVENT\r\n" +
	"UID:weekly\r\nDTSTAMP:20260912T120000Z\r\nDTSTART:20260914T100000Z\r\n" +
	"DTEND:20260914T110000Z\r\nSUMMARY:Weekly sync\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func at(day, hour int) time.Time {
	return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC)
}

// A calendar belongs to an account, holds events, and takes them and their
// occurrences with it when it goes.
func TestACalendarHoldsEventsAndTakesThemWithIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		calendar, err := tx.CreateCalendar(&models.Calendar{UserID: owner.ID})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		// A calendar with no name is called something anyway, because a
		// nameless collection is no use on a phone.
		if calendar.Name != "Calendar" {
			t.Fatalf("a calendar is named: %q", calendar.Name)
		}
		calendars, err := tx.ListCalendars(owner.ID)
		if err != nil || len(calendars) != 1 {
			t.Fatalf("one calendar for the account: %d %v", len(calendars), err)
		}

		kept, err := tx.PutCalendarObject(&models.CalendarObject{
			CalendarID: calendar.ID, UID: "weekly", ETag: "e1", Data: oneEvent,
			Summary: "Weekly sync", StartsAt: at(14, 10), EndsAt: at(14, 11),
		}, []models.Occurrence{
			{StartsAt: at(14, 10), EndsAt: at(14, 11)},
			{StartsAt: at(21, 10), EndsAt: at(21, 11)},
		})
		if err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
		if kept.ID == "" || kept.CreatedAt.IsZero() {
			t.Fatalf("a kept event has an id and a time: %+v", kept)
		}
		read, err := tx.GetCalendarObject(calendar.ID, kept.ID)
		if err != nil || read == nil {
			t.Fatalf("GetCalendarObject: %v %v", read, err)
		}
		if read.Data != oneEvent {
			t.Fatalf("the file is kept exactly:\n%q", read.Data)
		}
		if !read.StartsAt.Equal(at(14, 10)) {
			t.Fatalf("when it starts: %v", read.StartsAt)
		}
		byUID, err := tx.GetCalendarObjectByUID(calendar.ID, "weekly")
		if err != nil || byUID == nil || byUID.ID != kept.ID {
			t.Fatalf("found by the identifier the file carries: %v %v", byUID, err)
		}
		count, err := tx.CountCalendarObjects(calendar.ID)
		if err != nil || count != 1 {
			t.Fatalf("one event: %d %v", count, err)
		}

		// And not by somebody else asking for it by its identifier.
		if err := tx.DeleteCalendar("somebody-else", calendar.ID); err == nil {
			t.Fatal("a calendar is not deleted by whoever knows its identifier")
		}

		// Deleting the calendar takes the events and the occurrences with
		// it, by the foreign keys rather than by further statements.
		if err := tx.DeleteCalendar(calendar.UserID, calendar.ID); err != nil {
			t.Fatalf("DeleteCalendar: %s", err)
		}
		gone, err := tx.GetCalendarObject(calendar.ID, kept.ID)
		if err != nil || gone != nil {
			t.Fatalf("the events went with it: %v %v", gone, err)
		}
		left, err := tx.ListOccurrences(calendar.ID, at(1, 0), at(30, 0))
		if err != nil || len(left) != 0 {
			t.Fatalf("and so did the occurrences: %d %v", len(left), err)
		}
	})
}

// What is on between two moments is what overlaps the window, not only what
// is contained by it.
func TestWhatIsOnThisWeek(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		calendar, err := tx.CreateCalendar(&models.Calendar{UserID: owner.ID, Name: "Work"})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		put := func(id, uid string, occurrences ...models.Occurrence) {
			t.Helper()
			if _, err := tx.PutCalendarObject(&models.CalendarObject{
				ID: id, CalendarID: calendar.ID, UID: uid, ETag: "e", Data: oneEvent,
				Summary: uid, StartsAt: occurrences[0].StartsAt, EndsAt: occurrences[0].EndsAt,
			}, occurrences); err != nil {
				t.Fatalf("PutCalendarObject %s: %s", id, err)
			}
		}
		// Inside the window, before it, after it, and one that spans it
		// entirely without starting or finishing inside it.
		put("inside", "inside", models.Occurrence{StartsAt: at(16, 10), EndsAt: at(16, 11)})
		put("before", "before", models.Occurrence{StartsAt: at(10, 10), EndsAt: at(10, 11)})
		put("after", "after", models.Occurrence{StartsAt: at(25, 10), EndsAt: at(25, 11)})
		put("across", "across", models.Occurrence{StartsAt: at(14, 9), EndsAt: at(18, 17)})

		found, err := tx.ListOccurrences(calendar.ID, at(16, 0), at(17, 0))
		if err != nil {
			t.Fatalf("ListOccurrences: %s", err)
		}
		seen := map[string]bool{}
		for _, occurrence := range found {
			seen[occurrence.ObjectID] = true
		}
		if !seen["inside"] || !seen["across"] {
			t.Fatalf("what is on that day: %v", seen)
		}
		if seen["before"] || seen["after"] {
			t.Fatalf("and what is not: %v", seen)
		}

		// The window is half-open, so two adjacent windows show an event
		// once rather than twice. The event finishing exactly as the
		// second window opens belongs to the first.
		first, err := tx.ListOccurrences(calendar.ID, at(16, 0), at(16, 11))
		if err != nil || len(first) == 0 {
			t.Fatalf("the first window holds it: %d %v", len(first), err)
		}
		second, err := tx.ListOccurrences(calendar.ID, at(16, 11), at(16, 23))
		if err != nil {
			t.Fatalf("ListOccurrences: %s", err)
		}
		for _, occurrence := range second {
			if occurrence.ObjectID == "inside" {
				t.Fatal("and the second does not show it again")
			}
		}
	})
}

// Writing an event again replaces its occurrences rather than adding to them.
// Leaving the old ones behind would show a meeting at the time it used to be
// at, which is the whole reason the two are written together.
func TestRewritingAnEventRewritesWhenItHappens(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		calendar, err := tx.CreateCalendar(&models.Calendar{UserID: owner.ID})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		object := &models.CalendarObject{
			ID: "moved", CalendarID: calendar.ID, UID: "moved", ETag: "e1", Data: oneEvent,
			Summary: "Weekly sync", StartsAt: at(14, 10), EndsAt: at(14, 11),
		}
		if _, err := tx.PutCalendarObject(object, []models.Occurrence{
			{StartsAt: at(14, 10), EndsAt: at(14, 11)},
			{StartsAt: at(21, 10), EndsAt: at(21, 11)},
		}); err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
		// Moved to the afternoon.
		object.ETag = "e2"
		object.StartsAt, object.EndsAt = at(14, 14), at(14, 15)
		if _, err := tx.PutCalendarObject(object, []models.Occurrence{
			{StartsAt: at(14, 14), EndsAt: at(14, 15)},
			{StartsAt: at(21, 14), EndsAt: at(21, 15)},
		}); err != nil {
			t.Fatalf("PutCalendarObject again: %s", err)
		}
		morning, err := tx.ListOccurrences(calendar.ID, at(14, 9), at(14, 12))
		if err != nil || len(morning) != 0 {
			t.Fatalf("it is not in the morning any more: %d %v", len(morning), err)
		}
		afternoon, err := tx.ListOccurrences(calendar.ID, at(14, 13), at(14, 16))
		if err != nil || len(afternoon) != 1 {
			t.Fatalf("it is in the afternoon: %d %v", len(afternoon), err)
		}
		all, err := tx.ListOccurrences(calendar.ID, at(1, 0), at(30, 0))
		if err != nil || len(all) != 2 {
			t.Fatalf("two occurrences, not four: %d %v", len(all), err)
		}
	})
}

// Two calendars may hold an event under the same file name: the name is the
// one a client chose, and a client that numbers its files from one must not
// stop every other person's from doing the same.
func TestTwoCalendarsMayNameAFileTheSame(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var first *models.Calendar
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if first, err = tx.CreateCalendar(&models.Calendar{UserID: owner.ID, Name: "Work"}); err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		second, err := tx.CreateCalendar(&models.Calendar{UserID: owner.ID, Name: "Home"})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		for _, calendar := range []*models.Calendar{first, second} {
			if _, err := tx.PutCalendarObject(&models.CalendarObject{
				ID: "1.ics", CalendarID: calendar.ID, UID: "uid-" + calendar.ID,
				ETag: "e", Data: oneEvent, Summary: "One", StartsAt: at(14, 10), EndsAt: at(14, 11),
			}, []models.Occurrence{{StartsAt: at(14, 10), EndsAt: at(14, 11)}}); err != nil {
				t.Fatalf("a file named 1.ics in %s: %s", calendar.Name, err)
			}
		}
	})

	// The same event twice in one calendar is refused, which is what stops
	// two devices each adding it. In a transaction of its own, because a
	// refused write aborts the one it happened in: PostgreSQL will not take
	// anything more from a transaction that has hit a constraint.
	var refused error
	_ = database.Transaction(func(tx db.Transaction) error {
		_, refused = tx.PutCalendarObject(&models.CalendarObject{
			ID: "2.ics", CalendarID: first.ID, UID: "uid-" + first.ID,
			ETag: "e", Data: oneEvent, Summary: "Again", StartsAt: at(14, 10), EndsAt: at(14, 11),
		}, nil)
		return refused
	})
	if refused == nil {
		t.Fatal("a second event under the same identifier must be refused")
	}

	// And the first is untouched.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := tx.ListCalendarObjects(first.ID)
		if err != nil || len(found) != 1 || found[0].Summary != "One" {
			t.Fatalf("the one that was there is still there: %d %v", len(found), err)
		}
	})
}

// A file name longer than the column is refused rather than cut short, since
// cutting it short would make two events one. An iPhone names a file after
// its UID, which is thirty-six characters, so the ordinary case is well
// inside the limit and only the absurd one is refused.
func TestAnIdentifierTooLongIsRefusedNotShortened(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		calendar, err := tx.CreateCalendar(&models.Calendar{UserID: owner.ID})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		// What a phone actually sends: a UUID with the extension on it.
		phone := "9C1F2A3B-4D5E-6789-ABCD-EF0123456789.ics"
		if _, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: phone, CalendarID: calendar.ID, UID: "9C1F2A3B-4D5E-6789-ABCD-EF0123456789",
			ETag: "e", Data: oneEvent, Summary: "From a phone",
			StartsAt: at(14, 10), EndsAt: at(14, 11),
		}, nil); err != nil {
			t.Fatalf("what a phone sends must be kept: %s", err)
		}
		long := make([]byte, 300)
		for index := range long {
			long[index] = 'a'
		}
		if _, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: string(long), CalendarID: calendar.ID, UID: "absurd",
			ETag: "e", Data: oneEvent, Summary: "Absurd",
		}, nil); err == nil {
			t.Fatal("an identifier of three hundred characters should be refused")
		}
	})
}
