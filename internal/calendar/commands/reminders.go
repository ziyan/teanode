package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Reminders are kept in the person's reminders list, a calendar of kind
// reminders beside their calendar, and each is an iCalendar file holding a
// to-do, as a phone writes one.

// ReminderObject is how a reminder is kept: its file, with its due time
// where an event keeps its start, and its status, so that a listing never
// reads iCalendar. The one place this is said, for the dashboard's door and
// for CalDAV's.
func ReminderObject(calendarId, objectId string, reminder *calendar.Reminder) *models.CalendarObject {
	status := "NEEDS-ACTION"
	if reminder.IsDone {
		status = "COMPLETED"
	}
	return &models.CalendarObject{
		ID: objectId, CalendarID: calendarId, UID: reminder.UID,
		ETag: calendar.ETag(reminder.Data), Data: string(reminder.Data),
		Summary: reminder.Title, StartsAt: reminder.DueAt, EndsAt: reminder.DueAt,
		AllDay: reminder.IsDueDate, Status: status,
	}
}

// SaveReminder writes fields into one of the person's reminders, or into a
// new one when reminderId is empty, in their reminders list, which is made
// when they have none.
func (self *Commands) SaveReminder(ctx context.Context, principal *access.Principal, reminderId string, fields *calendar.ReminderFields) (*models.CalendarObject, error) {
	if !canUseCalendar(principal) {
		return nil, db.ErrNotFound
	}
	var saved *models.CalendarObject
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		list, err := transaction.EnsureCalendar(principal.User.ID, models.CalendarReminders, nil)
		if err != nil {
			return err
		}
		if list, err = transaction.LockCalendar(list.ID); err != nil {
			return err
		}
		var existing *models.CalendarObject
		if id := strings.TrimSpace(reminderId); id != "" {
			if existing, err = transaction.LockCalendarObject(list.ID, id); err != nil {
				return err
			}
			if existing == nil {
				return db.ErrNotFound
			}
		} else {
			held, err := transaction.CountCalendarObjects(list.ID)
			if err != nil {
				return err
			}
			if held >= db.ObjectsPerCalendar {
				return fmt.Errorf("%w: the reminders list already holds %d reminders, which is as many as this server keeps", db.ErrInvalidArguments, db.ObjectsPerCalendar)
			}
		}
		previous := []byte(nil)
		if existing != nil {
			previous = []byte(existing.Data)
		}
		reminder, err := calendar.BuildReminder(previous, fields)
		if err != nil {
			return fmt.Errorf("%w: %s", db.ErrInvalidArguments, err)
		}
		object := ReminderObject(list.ID, "", reminder)
		if existing != nil {
			object.ID, object.CreatedAt = existing.ID, existing.CreatedAt
		}
		saved, err = transaction.PutCalendarObject(object, nil)
		return err
	})
	return saved, err
}

// DeleteReminder removes one of the person's reminders.
func (self *Commands) DeleteReminder(ctx context.Context, principal *access.Principal, reminderId string) error {
	if !canUseCalendar(principal) {
		return db.ErrNotFound
	}
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		list, err := transaction.EnsureCalendar(principal.User.ID, models.CalendarReminders, nil)
		if err != nil {
			return err
		}
		object, err := transaction.LockCalendarObject(list.ID, strings.TrimSpace(reminderId))
		if err != nil {
			return err
		}
		if object == nil {
			return db.ErrNotFound
		}
		return transaction.DeleteCalendarObject(list.ID, object.ID)
	})
}
