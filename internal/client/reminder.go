package client

import (
	"context"
	"time"
)

// The reminders list beside the calendar, which a phone's Reminders app
// syncs. The agent's reminder tool sends these same documents.

// Reminder is one reminder.
type Reminder struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	DueAt       *time.Time `json:"dueAt"`
	IsDueDate   bool       `json:"isDueDate"`
	IsDone      bool       `json:"isDone"`
	DoneAt      *time.Time `json:"doneAt"`
	Priority    int        `json:"priority"`
	IsRepeating bool       `json:"isRepeating"`
	CreatedAt   time.Time  `json:"createdAt"`
	ModifiedAt  time.Time  `json:"modifiedAt"`
}

// ReminderChange is what to write into a reminder; what is nil is kept.
type ReminderChange struct {
	Title        *string `json:"title,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	DueAt        *string `json:"dueAt,omitempty"`
	DueOn        *string `json:"dueOn,omitempty"`
	IsDueCleared bool    `json:"isDueCleared,omitempty"`
	Priority     *int    `json:"priority,omitempty"`
}

const reminderFields = `{ id title notes dueAt isDueDate isDone doneAt priority isRepeating createdAt modifiedAt }`

const (
	DocumentListReminders = `query ($isDone: Boolean) { ListReminders(isDone: $isDone) ` + reminderFields + ` }`

	DocumentSaveReminder = `mutation ($reminderId: String, $title: String, $notes: String, $dueAt: String, $dueOn: String, $isDueCleared: Boolean, $priority: Int) {
  SaveReminder(reminderId: $reminderId, title: $title, notes: $notes, dueAt: $dueAt, dueOn: $dueOn, isDueCleared: $isDueCleared, priority: $priority) ` + reminderFields + `
}`

	DocumentSetReminderDone = `mutation ($reminderId: String!, $isDone: Boolean!) {
  SetReminderDone(reminderId: $reminderId, isDone: $isDone) ` + reminderFields + `
}`

	DocumentDeleteReminder = `mutation ($reminderId: String!) { DeleteReminder(reminderId: $reminderId) }`
)

// ListReminders is the reminders, done or not when isDone says, or all.
func ListReminders(ctx context.Context, connection *Client, isDone *bool) ([]*Reminder, error) {
	var result struct {
		ListReminders []*Reminder `json:"ListReminders"`
	}
	variables := map[string]any{}
	if isDone != nil {
		variables["isDone"] = *isDone
	}
	if err := connection.Execute(ctx, DocumentListReminders, variables, &result); err != nil {
		return nil, err
	}
	return result.ListReminders, nil
}

// SaveReminder makes a reminder, when reminderId is empty, or changes one.
func SaveReminder(ctx context.Context, connection *Client, reminderId string, change *ReminderChange) (*Reminder, error) {
	var result struct {
		SaveReminder *Reminder `json:"SaveReminder"`
	}
	variables := map[string]any{"isDueCleared": change.IsDueCleared}
	if reminderId != "" {
		variables["reminderId"] = reminderId
	}
	for name, value := range map[string]*string{"title": change.Title, "notes": change.Notes, "dueAt": change.DueAt, "dueOn": change.DueOn} {
		if value != nil {
			variables[name] = *value
		}
	}
	if change.Priority != nil {
		variables["priority"] = *change.Priority
	}
	if err := connection.Execute(ctx, DocumentSaveReminder, variables, &result); err != nil {
		return nil, err
	}
	return result.SaveReminder, nil
}

// SetReminderDone says a reminder is done, or not.
func SetReminderDone(ctx context.Context, connection *Client, reminderId string, isDone bool) (*Reminder, error) {
	var result struct {
		SetReminderDone *Reminder `json:"SetReminderDone"`
	}
	if err := connection.Execute(ctx, DocumentSetReminderDone, map[string]any{"reminderId": reminderId, "isDone": isDone}, &result); err != nil {
		return nil, err
	}
	return result.SetReminderDone, nil
}

// DeleteReminder removes a reminder.
func DeleteReminder(ctx context.Context, connection *Client, reminderId string) error {
	return connection.Execute(ctx, DocumentDeleteReminder, map[string]any{"reminderId": reminderId}, nil)
}
