package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The reminders a day looks for are the ones due in it, from the time kept
// beside each, and the ones marked done, from the status kept beside each.
func TestCalendarObjectsDoneSinceListsOnlyFinishedReminders(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	zone := time.FixedZone("test", 0)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, zone) }
	var listId string
	ids := map[string]string{}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner := newCircleTestUser(t, tx, "reminders-ada")
		list, err := tx.EnsureCalendar(owner.ID, models.CalendarReminders, nil)
		if err != nil {
			t.Fatal(err)
		}
		listId = list.ID
		put := func(uid, status string, due time.Time) {
			object, err := tx.PutCalendarObject(&models.CalendarObject{
				CalendarID: list.ID, UID: uid, ETag: uid, Data: "BEGIN:VCALENDAR\nEND:VCALENDAR\n",
				Summary: uid, StartsAt: due, EndsAt: due, Status: status,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			ids[uid] = object.ID
		}
		put("water-plants", "COMPLETED", at(22, 8))
		put("call-the-bank", "NEEDS-ACTION", at(22, 10))
		put("renew-pass", "COMPLETED", at(10, 8))
	})
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "calendar_object" SET "modified_at" = '%s' WHERE "id" = '%s'`, at(22, 9).Format(time.RFC3339), ids["water-plants"]))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "calendar_object" SET "modified_at" = '%s' WHERE "id" = '%s'`, at(10, 9).Format(time.RFC3339), ids["renew-pass"]))

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		done, err := tx.ListCalendarObjectsDoneSince(listId, at(22, 0), 100)
		if err != nil || len(done) != 1 || done[0].Summary != "water-plants" {
			t.Fatalf("one reminder was finished since the morning of the 22nd: %+v %v", done, err)
		}
		everyone, err := tx.ListCalendarObjectsDoneSince(listId, at(1, 0), 100)
		if err != nil || len(everyone) != 2 || everyone[0].Summary != "renew-pass" {
			t.Fatalf("both finished reminders, earliest written first: %+v %v", everyone, err)
		}
		due, err := tx.ListCalendarObjectsStartingBetween(listId, at(22, 0), at(23, 0).Add(-time.Nanosecond), 100)
		if err != nil || len(due) != 2 {
			t.Fatalf("two reminders were due that day: %+v %v", due, err)
		}
	})
}
