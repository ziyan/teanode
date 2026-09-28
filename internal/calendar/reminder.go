package calendar

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// A reminder is one item of a person's reminders list: something to do,
// maybe by a time or a day, done or not. iCalendar calls it a to-do, a VTODO
// component, and that is the name used only where the format is written;
// everywhere else in this program it is a reminder, because the agent's own
// task list is called todo and is a different thing.

// Reminder is one reminder read out of its file, with the file itself.
//
// As with an event, Data is what a phone sent and is given back, and the
// fields beside it are read out of it; where they disagree Data is right.
type Reminder struct {
	UID   string
	Title string
	Notes string

	// DueAt is when it is due, in UTC; zero when it is not due at all.
	// IsDueDate marks one due on a day rather than at a time, which is
	// read at midnight UTC of that day and must not be shifted by a zone.
	DueAt     time.Time
	IsDueDate bool

	// IsDone is its STATUS being COMPLETED, and DoneAt its COMPLETED time
	// where the file gives one.
	IsDone bool
	DoneAt time.Time

	// Priority is the file's own: 0 for none, 1 the highest to 9 the lowest.
	Priority int

	Data []byte
}

// ReminderFields are what may be written into a reminder; what is nil is
// left as it is.
type ReminderFields struct {
	Title *string
	Notes *string

	// DueAt is a time it is due, and DueOn a day, as 2006-01-02. Setting
	// either replaces the other; ClearDue takes the due date away.
	DueAt    *time.Time
	DueOn    *string
	ClearDue bool

	IsDone   *bool
	Priority *int
}

// ParseReminder reads one reminder's file and returns what to keep beside
// it. A file with no to-do in it is refused: an event put into the reminders
// list would sit there where no program looks for it.
func ParseReminder(data []byte) (*Reminder, error) {
	if len(data) > MaximumObject {
		return nil, ErrTooLarge
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("calendar: there is nothing in that reminder")
	}
	decoded, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("calendar: that is not a reminder this server can read: %w", err)
	}
	settleZones(decoded)
	encoded, err := Encode(decoded)
	if err != nil {
		return nil, err
	}
	todo := firstToDo(decoded)
	if todo == nil {
		return nil, fmt.Errorf("calendar: there is no reminder in that file")
	}
	reminder := &Reminder{Data: encoded}
	reminder.UID, _ = todo.Props.Text(ical.PropUID)
	reminder.UID = strings.TrimSpace(reminder.UID)
	if reminder.UID == "" {
		return nil, fmt.Errorf("calendar: that reminder has no identifier")
	}
	reminder.Title, _ = todo.Props.Text(ical.PropSummary)
	reminder.Notes, _ = todo.Props.Text(ical.PropDescription)
	if due := todo.Props.Get(ical.PropDue); due != nil {
		reminder.IsDueDate = due.ValueType() == ical.ValueDate
		if at, err := due.DateTime(time.UTC); err == nil {
			reminder.DueAt = at.UTC()
		}
	}
	if status, err := todo.Props.Text(ical.PropStatus); err == nil {
		reminder.IsDone = strings.EqualFold(strings.TrimSpace(status), "COMPLETED")
	}
	if done := todo.Props.Get(ical.PropCompleted); done != nil {
		// Some programs mark a reminder done with the time alone.
		reminder.IsDone = true
		if at, err := done.DateTime(time.UTC); err == nil {
			reminder.DoneAt = at.UTC()
		}
	}
	if priority := todo.Props.Get(ical.PropPriority); priority != nil {
		if value, err := priority.Int(); err == nil && value >= 0 && value <= 9 {
			reminder.Priority = value
		}
	}
	return reminder, nil
}

// BuildReminder writes fields into a reminder's file: the one already kept,
// or a new one. Applied to it, as Build is to an event: a phone puts an
// alarm, a list colour or a location on a reminder that no form here shows,
// and ticking it off in a browser must not throw those away.
func BuildReminder(previous []byte, fields *ReminderFields) (*Reminder, error) {
	if fields == nil {
		fields = &ReminderFields{}
	}
	var cal *ical.Calendar
	var todo *ical.Component
	if len(bytes.TrimSpace(previous)) > 0 {
		decoded, err := ical.NewDecoder(bytes.NewReader(previous)).Decode()
		if err != nil {
			return nil, fmt.Errorf("calendar: what is already kept cannot be read: %w", err)
		}
		cal, todo = decoded, firstToDo(decoded)
	}
	if cal == nil {
		cal = ical.NewCalendar()
		cal.Props.SetText(ical.PropProductID, productID)
		cal.Props.SetText(ical.PropVersion, "2.0")
	}
	if todo == nil {
		todo = ical.NewComponent(ical.CompToDo)
		todo.Props.SetText(ical.PropUID, newUID())
		todo.Props.SetDateTime(ical.PropCreated, time.Now().UTC())
		todo.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
		cal.Children = append(cal.Children, todo)
	}
	now := time.Now().UTC()
	todo.Props.SetDateTime(ical.PropDateTimeStamp, now)
	todo.Props.SetDateTime(ical.PropLastModified, now)

	setComponentText(todo, ical.PropSummary, fields.Title)
	setComponentText(todo, ical.PropDescription, fields.Notes)
	switch {
	case fields.ClearDue:
		todo.Props.Del(ical.PropDue)
	case fields.DueOn != nil && strings.TrimSpace(*fields.DueOn) != "":
		day, err := time.Parse("2006-01-02", strings.TrimSpace(*fields.DueOn))
		if err != nil {
			return nil, fmt.Errorf("calendar: a reminder is due on a day written as 2006-01-02: %w", err)
		}
		todo.Props.Del(ical.PropDue)
		todo.Props.SetDate(ical.PropDue, day)
	case fields.DueAt != nil && !fields.DueAt.IsZero():
		todo.Props.Del(ical.PropDue)
		todo.Props.SetDateTime(ical.PropDue, fields.DueAt.UTC())
	}
	if fields.Priority != nil {
		if *fields.Priority < 0 || *fields.Priority > 9 {
			return nil, fmt.Errorf("calendar: a reminder's priority is 0 for none, or 1 the highest to 9 the lowest")
		}
		todo.Props.Del(ical.PropPriority)
		if *fields.Priority > 0 {
			property := ical.NewProp(ical.PropPriority)
			property.Value = fmt.Sprint(*fields.Priority)
			todo.Props.Set(property)
		}
	}
	if fields.IsDone != nil {
		if *fields.IsDone {
			todo.Props.SetText(ical.PropStatus, "COMPLETED")
			if todo.Props.Get(ical.PropCompleted) == nil {
				todo.Props.SetDateTime(ical.PropCompleted, now)
			}
			percent := ical.NewProp(ical.PropPercentComplete)
			percent.Value = "100"
			todo.Props.Set(percent)
		} else {
			todo.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
			todo.Props.Del(ical.PropCompleted)
			todo.Props.Del(ical.PropPercentComplete)
		}
	}
	if title, _ := todo.Props.Text(ical.PropSummary); strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("calendar: a reminder needs something to say")
	}
	written, err := Encode(cal)
	if err != nil {
		return nil, err
	}
	return ParseReminder(written)
}

// firstToDo is the file's first to-do.
func firstToDo(cal *ical.Calendar) *ical.Component {
	for _, child := range cal.Children {
		if child != nil && child.Name == ical.CompToDo {
			return child
		}
	}
	return nil
}

// setComponentText writes a property, or takes it away when the instruction
// is to empty it.
func setComponentText(component *ical.Component, name string, value *string) {
	if value == nil {
		return
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		component.Props.Del(name)
		return
	}
	component.Props.SetText(name, text)
}
