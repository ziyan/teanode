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
	cleared, err := BuildReminder(made.Data, &ReminderFields{IsDueCleared: true})
	if err != nil || !cleared.DueAt.IsZero() {
		t.Fatalf("due no longer: %+v %v", cleared, err)
	}
	empty := " "
	if _, err := BuildReminder(nil, &ReminderFields{Title: &empty}); err == nil {
		t.Fatal("a reminder needs something to say")
	}
}

// A new due date takes away a start it would leave wrong: one written as a
// time where the due date is now a day, or one after the new due date. A
// start that still fits is kept, and clearing the due date keeps the start.
func TestANewDueDateTakesAwayAStartItWouldLeaveWrong(t *testing.T) {
	started := strings.Replace(phoneReminder, "DUE;VALUE=DATE:20260929\r\n",
		"DTSTART:20260928T090000Z\r\nDUE:20260929T170000Z\r\n", 1)
	day := "2026-10-01"
	onADay, err := BuildReminder([]byte(started), &ReminderFields{DueOn: &day})
	if err != nil {
		t.Fatal(err)
	}
	if !onADay.IsDueDate || !onADay.StartsAt.IsZero() || strings.Contains(string(onADay.Data), "DTSTART") {
		t.Fatalf("a start written as a time goes when the due date is a day:\n%s", onADay.Data)
	}
	later := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	kept, err := BuildReminder([]byte(started), &ReminderFields{DueAt: &later})
	if err != nil || !kept.StartsAt.Equal(time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("a start before the new due date stays: %+v %v", kept, err)
	}
	earlier := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	moved, err := BuildReminder([]byte(started), &ReminderFields{DueAt: &earlier})
	if err != nil || !moved.StartsAt.IsZero() {
		t.Fatalf("a start after the new due date goes: %+v %v", moved, err)
	}
	cleared, err := BuildReminder([]byte(started), &ReminderFields{IsDueCleared: true})
	if err != nil || !cleared.DueAt.IsZero() || cleared.StartsAt.IsZero() {
		t.Fatalf("a start alone is kept: %+v %v", cleared, err)
	}
}

// A weekly reminder ticked off is this week's done: it moves on a week, start
// and all, and is still open. The last of a counted repeat is done for good.
func TestAWeeklyReminderTickedOffMovesOnAWeek(t *testing.T) {
	weekly := strings.Replace(phoneReminder, "DUE;VALUE=DATE:20260929\r\n",
		"DTSTART:20260928T090000Z\r\nDUE:20260929T090000Z\r\nRRULE:FREQ=WEEKLY;COUNT=2\r\n", 1)
	parsed, err := ParseReminder([]byte(weekly))
	if err != nil || !parsed.IsRepeating {
		t.Fatalf("repeating: %+v %v", parsed, err)
	}
	done := true
	ticked, err := BuildReminder([]byte(weekly), &ReminderFields{IsDone: &done})
	if err != nil {
		t.Fatal(err)
	}
	if ticked.IsDone || !ticked.IsRepeating ||
		!ticked.DueAt.Equal(time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)) ||
		!ticked.StartsAt.Equal(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("moved on a week and still open: %+v\n%s", ticked, ticked.Data)
	}
	if strings.Contains(string(ticked.Data), "COMPLETED") || !strings.Contains(string(ticked.Data), "COUNT=1") {
		t.Fatalf("not completed, and one time left:\n%s", ticked.Data)
	}
	last, err := BuildReminder(ticked.Data, &ReminderFields{IsDone: &done})
	if err != nil || !last.IsDone || !last.DueAt.Equal(ticked.DueAt) {
		t.Fatalf("the last time is done for good: %+v %v", last, err)
	}
}

// A file holding one changed occurrence ahead of the series is still about
// the series: that is the one written into, and the occurrence is left as
// it was.
func TestTheSeriesIsEditedNotTheOccurrenceBeforeIt(t *testing.T) {
	file := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//Reminders//EN\r\n" +
		"BEGIN:VTODO\r\nUID:weekly@example.com\r\nDTSTAMP:20260928T120000Z\r\n" +
		"RECURRENCE-ID:20261005T090000Z\r\nDTSTART:20261005T100000Z\r\nSUMMARY:Water the plants later\r\nEND:VTODO\r\n" +
		"BEGIN:VTODO\r\nUID:weekly@example.com\r\nDTSTAMP:20260928T120000Z\r\n" +
		"DTSTART:20260928T090000Z\r\nRRULE:FREQ=WEEKLY\r\nSUMMARY:Water the plants\r\nEND:VTODO\r\n" +
		"END:VCALENDAR\r\n"
	read, err := ParseReminder([]byte(file))
	if err != nil || read.Title != "Water the plants" || !read.IsRepeating {
		t.Fatalf("the series is read: %+v %v", read, err)
	}
	title := "Water the garden"
	changed, err := BuildReminder([]byte(file), &ReminderFields{Title: &title})
	if err != nil || changed.Title != title {
		t.Fatalf("the series is written: %+v %v", changed, err)
	}
	if !strings.Contains(string(changed.Data), "SUMMARY:Water the plants later") {
		t.Fatalf("the occurrence is left as it was:\n%s", changed.Data)
	}
}
