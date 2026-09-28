package apigraph

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/calendar"
	calendarcommands "github.com/ziyan/teanode/internal/calendar/commands"
	"github.com/ziyan/teanode/internal/models"
)

// Reminders: the person's reminders list beside their calendar, which a
// phone's Reminders app syncs over CalDAV. The dashboard, the command line's
// teanode reminder and the agent's reminder tool all call these.

// ReminderQuery reads the caller's reminders.
type ReminderQuery interface {
	// The caller's reminders, the ones not done first by when they are
	// due, or only those done or not when isDone says. Needs calendar:use.
	ListReminders(ctx context.Context, arguments ListRemindersArguments) ([]*ReminderView, error)
}

// ReminderMutation changes them.
type ReminderMutation interface {
	// Make a reminder, or change one: what is left out is kept, including
	// whatever a phone put on it that this server does not show. Needs
	// calendar:use.
	SaveReminder(ctx context.Context, arguments SaveReminderArguments) (*ReminderView, error)

	// Say a reminder is done, or not done after all. Needs calendar:use.
	SetReminderDone(ctx context.Context, arguments SetReminderDoneArguments) (*ReminderView, error)

	// Remove a reminder. Needs calendar:use.
	DeleteReminder(ctx context.Context, arguments DeleteReminderArguments) (bool, error)
}

// ReminderView is one reminder.
type ReminderView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Notes string `json:"notes"`

	// DueAt is when it is due, and IsDueDate says it is due on that day
	// rather than at that time.
	DueAt     *time.Time `json:"dueAt" graphapi:"nullable"`
	IsDueDate bool       `json:"isDueDate"`

	IsDone bool       `json:"isDone"`
	DoneAt *time.Time `json:"doneAt" graphapi:"nullable"`

	// Priority is 0 for none, or 1 the highest to 9 the lowest.
	Priority int `json:"priority"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// ListRemindersArguments narrow the list to done or not done.
type ListRemindersArguments struct {
	IsDone *bool `json:"isDone" graphapi:"nullable"`
}

// SaveReminderArguments are a reminder's fields; what is left out is kept.
type SaveReminderArguments struct {
	// ReminderID names the one to change; left out, one is made.
	ReminderID string  `json:"reminderId" graphapi:"nullable"`
	Title      *string `json:"title" graphapi:"nullable"`
	Notes      *string `json:"notes" graphapi:"nullable"`
	// DueAt is a time, as RFC 3339; DueOn a day, as 2006-01-02. IsDueCleared
	// says it is due no longer.
	DueAt        *string `json:"dueAt" graphapi:"nullable"`
	DueOn        *string `json:"dueOn" graphapi:"nullable"`
	IsDueCleared bool    `json:"isDueCleared" graphapi:"nullable"`
	Priority     *int    `json:"priority" graphapi:"nullable"`
}

// SetReminderDoneArguments name the reminder and what became of it.
type SetReminderDoneArguments struct {
	ReminderID string `json:"reminderId"`
	IsDone     bool   `json:"isDone"`
}

// DeleteReminderArguments name the reminder.
type DeleteReminderArguments struct {
	ReminderID string `json:"reminderId"`
}

// reminderView is a kept reminder as the API shows it.
func reminderView(object *models.CalendarObject) (*ReminderView, error) {
	reminder, err := calendar.ParseReminder([]byte(object.Data))
	if err != nil {
		return nil, err
	}
	view := &ReminderView{
		ID: object.ID, Title: reminder.Title, Notes: reminder.Notes, IsDueDate: reminder.IsDueDate,
		IsDone: reminder.IsDone, Priority: reminder.Priority,
		CreatedAt: object.CreatedAt, ModifiedAt: object.ModifiedAt,
	}
	if !reminder.DueAt.IsZero() {
		view.DueAt = &reminder.DueAt
	}
	if !reminder.DoneAt.IsZero() {
		view.DoneAt = &reminder.DoneAt
	}
	return view, nil
}

func (self *graph) ListReminders(ctx context.Context, arguments ListRemindersArguments) ([]*ReminderView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	list, err := tx.EnsureCalendar(principal.User.ID, models.CalendarReminders, nil)
	if err != nil {
		return nil, err
	}
	objects, err := tx.ListCalendarObjects(list.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*ReminderView, 0, len(objects))
	for _, object := range objects {
		view, err := reminderView(object)
		if err != nil {
			// One file nobody can read is left out of the list rather than
			// failing it; the phone that wrote it still has it.
			log.Warningf("reminder %q of %q cannot be read: %s", object.ID, principal.User.ID, err)
			continue
		}
		if arguments.IsDone != nil && view.IsDone != *arguments.IsDone {
			continue
		}
		views = append(views, view)
	}
	// Not done before done; not done by when they are due, the ones due at
	// no time after the rest; done by when they were done, the latest first.
	sort.SliceStable(views, func(first, second int) bool {
		one, other := views[first], views[second]
		if one.IsDone != other.IsDone {
			return !one.IsDone
		}
		if one.IsDone {
			return doneTime(one).After(doneTime(other))
		}
		switch {
		case one.DueAt == nil && other.DueAt == nil:
			return one.CreatedAt.Before(other.CreatedAt)
		case one.DueAt == nil:
			return false
		case other.DueAt == nil:
			return true
		}
		return one.DueAt.Before(*other.DueAt)
	})
	return views, nil
}

// doneTime is when a reminder was done, or last changed when its file does
// not say.
func doneTime(view *ReminderView) time.Time {
	if view.DoneAt != nil {
		return *view.DoneAt
	}
	return view.ModifiedAt
}

func (self *graph) SaveReminder(ctx context.Context, arguments SaveReminderArguments) (*ReminderView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	fields := &calendar.ReminderFields{
		Title: arguments.Title, Notes: arguments.Notes, DueOn: arguments.DueOn,
		ClearDue: arguments.IsDueCleared, Priority: arguments.Priority,
	}
	if arguments.DueAt != nil && strings.TrimSpace(*arguments.DueAt) != "" {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(*arguments.DueAt))
		if err != nil {
			return nil, fmt.Errorf("%w: dueAt is a time, as RFC 3339: %s", api.ErrInvalidArguments, err)
		}
		fields.DueAt = &at
	}
	if strings.TrimSpace(arguments.ReminderID) == "" && (arguments.Title == nil || strings.TrimSpace(*arguments.Title) == "") {
		return nil, fmt.Errorf("%w: a reminder needs something to say", api.ErrInvalidArguments)
	}
	kept, err := calendarcommands.New(self.transaction(ctx)).SaveReminder(ctx, principal, arguments.ReminderID, fields)
	if err != nil {
		return nil, translateError(err)
	}
	return reminderView(kept)
}

func (self *graph) SetReminderDone(ctx context.Context, arguments SetReminderDoneArguments) (*ReminderView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.ReminderID) == "" {
		return nil, fmt.Errorf("%w: which reminder", api.ErrInvalidArguments)
	}
	kept, err := calendarcommands.New(self.transaction(ctx)).SaveReminder(ctx, principal, arguments.ReminderID, &calendar.ReminderFields{IsDone: &arguments.IsDone})
	if err != nil {
		return nil, translateError(err)
	}
	return reminderView(kept)
}

func (self *graph) DeleteReminder(ctx context.Context, arguments DeleteReminderArguments) (bool, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := calendarcommands.New(self.transaction(ctx)).DeleteReminder(ctx, principal, arguments.ReminderID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}
