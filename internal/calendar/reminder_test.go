package calendar

import (
	"strings"
	"testing"
	"time"
)

// A reminder as a phone writes one: due on a day, not done, with an alarm
// no form here shows.
const phoneReminder = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//Reminders//EN\r\n" +
	"BEGIN:VTODO\r\nUID:reminder-1@example.com\r\nDTSTAMP:20260928T120000Z\r\n" +
	"SUMMARY:Buy stamps\r\nDUE;VALUE=DATE:20260929\r\nSTATUS:NEEDS-ACTION\r\nPRIORITY:1\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Buy stamps\r\nEND:VALARM\r\n" +
	"END:VTODO\r\nEND:VCALENDAR\r\n"

func TestAReminderIsReadAsAPhoneWritesIt(t *testing.T) {
	reminder, err := ParseReminder([]byte(phoneReminder))
	if err != nil {
		t.Fatal(err)
	}
	if reminder.UID != "reminder-1@example.com" || reminder.Title != "Buy stamps" || reminder.IsDone || reminder.Priority != 1 {
		t.Fatalf("read: %+v", reminder)
	}
	if !reminder.IsDueDate || !reminder.DueAt.Equal(time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("due on a day: %v %v", reminder.DueAt, reminder.IsDueDate)
	}
	event := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//x//EN\r\nBEGIN:VEVENT\r\nUID:e@x\r\nDTSTAMP:20260928T120000Z\r\n" +
		"DTSTART:20260929T090000Z\r\nSUMMARY:Standup\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if _, err := ParseReminder([]byte(event)); err == nil || !strings.Contains(err.Error(), "no reminder") {
		t.Fatalf("an event is not a reminder: %v", err)
	}
	if _, err := Parse([]byte(phoneReminder)); err == nil {
		t.Fatal("and a reminder is not an event")
	}
}

// Ticking a reminder off here keeps what the phone put on it, and taking it
// back puts it as it was; a new one is written from nothing.
func TestAReminderIsWrittenIntoWhatIsKept(t *testing.T) {
	done := true
	ticked, err := BuildReminder([]byte(phoneReminder), &ReminderFields{IsDone: &done})
	if err != nil {
		t.Fatal(err)
	}
	if !ticked.IsDone || ticked.DoneAt.IsZero() || ticked.UID != "reminder-1@example.com" {
		t.Fatalf("done: %+v", ticked)
	}
	if !strings.Contains(string(ticked.Data), "BEGIN:VALARM") || !strings.Contains(string(ticked.Data), "STATUS:COMPLETED") {
		t.Fatalf("the alarm is kept and the status written:\n%s", ticked.Data)
	}
	undone := false
	reopened, err := BuildReminder(ticked.Data, &ReminderFields{IsDone: &undone})
	if err != nil || reopened.IsDone || strings.Contains(string(reopened.Data), "COMPLETED:") {
		t.Fatalf("open again: %+v %v", reopened, err)
	}

	title, at := "Call the dentist", time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)
	made, err := BuildReminder(nil, &ReminderFields{Title: &title, DueAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if made.UID == "" || made.Title != title || made.IsDueDate || !made.DueAt.Equal(at) || made.IsDone {
		t.Fatalf("made: %+v", made)
	}
	cleared, err := BuildReminder(made.Data, &ReminderFields{ClearDue: true})
	if err != nil || !cleared.DueAt.IsZero() {
		t.Fatalf("due no longer: %+v %v", cleared, err)
	}
	empty := " "
	if _, err := BuildReminder(nil, &ReminderFields{Title: &empty}); err == nil {
		t.Fatal("a reminder needs something to say")
	}
}
