package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A reminder goes into the person's reminders list, made for it, is changed
// and ticked off in place, and removed; the calendar beside it takes no
// reminders, and the reminders list takes no events.
func TestAReminderIsKeptInTheRemindersListBesideTheCalendar(test *testing.T) {
	database, principal, events := calendarCommandFixture(test)
	commands := New(database)
	ctx := test.Context()

	title, due := "Buy stamps", "2026-09-29"
	kept, err := commands.SaveReminder(ctx, principal, "", &calendar.ReminderFields{Title: &title, DueOn: &due})
	if err != nil {
		test.Fatal(err)
	}
	if kept.CalendarID == events.ID || kept.Summary != title || kept.Status != "NEEDS-ACTION" || !kept.AllDay {
		test.Fatalf("kept in the reminders list, not the calendar: %+v", kept)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		list, err := transaction.GetCalendar(kept.CalendarID)
		if err != nil || list.CalendarKind != models.CalendarReminders || list.Name != "Reminders" {
			test.Fatalf("the reminders list: %+v %v", list, err)
		}
	})

	done, notes := true, "the ones with birds on"
	ticked, err := commands.SaveReminder(ctx, principal, kept.ID, &calendar.ReminderFields{IsDone: &done, Notes: &notes})
	if err != nil || ticked.ID != kept.ID || ticked.Status != "COMPLETED" || !strings.Contains(ticked.Data, "the ones with birds on") || ticked.UID != kept.UID {
		test.Fatalf("ticked off in place: %+v %v", ticked, err)
	}

	summary, starts := "Standup", time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	_, err = commands.SaveEvent(ctx, principal, EventRequest{CalendarID: kept.CalendarID}, func(_ context.Context, _ db.Transaction, _ *models.CalendarObject) (*calendar.Parsed, error) {
		return calendar.Build(nil, &calendar.Fields{Summary: &summary, StartsAt: &starts})
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "reminders list") {
		test.Fatalf("an event is not put into the reminders list: %v", err)
	}
	if err := commands.DeleteReminder(ctx, principal, kept.ID); err != nil {
		test.Fatal(err)
	}
	if err := commands.DeleteReminder(ctx, principal, kept.ID); !errors.Is(err, db.ErrNotFound) {
		test.Fatalf("removed once: %v", err)
	}
}
