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

	// StartsAt is its DTSTART, read as DueAt is, and zero when it has none;
	// IsStartDate marks one written as a day.
	StartsAt    time.Time
	IsStartDate bool

	// IsDone is its STATUS being COMPLETED, and DoneAt its COMPLETED time
	// where the file gives one.
	IsDone bool
	DoneAt time.Time

	// Priority is the file's own: 0 for none, 1 the highest to 9 the lowest.
	Priority int

	// IsRepeating is the reminder having a repeat rule. Ticking one off
	// moves it on to its next time rather than finishing it.
	IsRepeating bool

	Data []byte
}

// ReminderFields are what may be written into a reminder; what is nil is
// left as it is.
type ReminderFields struct {
	Title *string
	Notes *string

	// DueAt is a time it is due, and DueOn a day, as 2006-01-02. Setting
	// either replaces the other; IsDueCleared takes the due date away.
	DueAt        *time.Time
	DueOn        *string
	IsDueCleared bool

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
	if start := todo.Props.Get(ical.PropDateTimeStart); start != nil {
		reminder.IsStartDate = isDateValue(start)
		if at, err := start.DateTime(time.UTC); err == nil {
			reminder.StartsAt = at.UTC()
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
	reminder.IsRepeating = todo.Props.Get(ical.PropRecurrenceRule) != nil
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
	case fields.IsDueCleared:
		// A start with no due date is a reminder on its own, so it stays.
		todo.Props.Del(ical.PropDue)
	case fields.DueOn != nil && strings.TrimSpace(*fields.DueOn) != "":
		day, err := time.Parse("2006-01-02", strings.TrimSpace(*fields.DueOn))
		if err != nil {
			return nil, fmt.Errorf("calendar: a reminder is due on a day written as 2006-01-02: %w", err)
		}
		todo.Props.Del(ical.PropDue)
		todo.Props.SetDate(ical.PropDue, day)
		keepStartBeforeDue(todo)
	case fields.DueAt != nil && !fields.DueAt.IsZero():
		todo.Props.Del(ical.PropDue)
		todo.Props.SetDateTime(ical.PropDue, fields.DueAt.UTC())
		keepStartBeforeDue(todo)
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
	isDoneForGood := true
	if fields.IsDone != nil && *fields.IsDone && todo.Props.Get(ical.PropRecurrenceRule) != nil {
		// Ticking off a repeating reminder is doing this time of it, and
		// the reminder moves on to the next. Only when there is no next
		// time left is it done for good.
		isDoneForGood = !advanceToNext(todo)
	}
	if fields.IsDone != nil {
		if *fields.IsDone && isDoneForGood {
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

// firstToDo is the to-do a file is about: the one without a RECURRENCE-ID,
// as firstEvent is for an event, or the first when every one has one.
func firstToDo(cal *ical.Calendar) *ical.Component {
	var first *ical.Component
	for _, child := range cal.Children {
		if child == nil || child.Name != ical.CompToDo {
			continue
		}
		if child.Props.Get(ical.PropRecurrenceID) == nil {
			return child
		}
		if first == nil {
			first = child
		}
	}
	return first
}

// keepStartBeforeDue takes DTSTART away when a new due date leaves it
// wrong: written as a day where the due date is a time, or the other way
// round, or after the due date. The format allows neither, and a reminder
// may have no start at all.
func keepStartBeforeDue(todo *ical.Component) {
	due, start := todo.Props.Get(ical.PropDue), todo.Props.Get(ical.PropDateTimeStart)
	if due == nil || start == nil {
		return
	}
	if isDateValue(start) != isDateValue(due) {
		todo.Props.Del(ical.PropDateTimeStart)
		return
	}
	dueAt, dueErr := due.DateTime(time.UTC)
	startAt, startErr := start.DateTime(time.UTC)
	if dueErr != nil || startErr != nil || startAt.After(dueAt) {
		todo.Props.Del(ical.PropDateTimeStart)
	}
}

// isDateValue is a property holding a day rather than a time, whether or
// not it says VALUE=DATE.
func isDateValue(property *ical.Prop) bool {
	switch property.ValueType() {
	case ical.ValueDate:
		return true
	case ical.ValueDefault:
		return len(strings.TrimSpace(property.Value)) == len("20060102")
	}
	return false
}

// advanceToNext moves a repeating reminder on to its next time after the
// one it is at now: DUE, and DTSTART with it by the same amount, and it is
// open again. It says false, and changes nothing, when the repeat has no
// next time or cannot be read, and the reminder is then simply done.
//
// The repeat is counted from DTSTART, as the format has it, or from DUE for
// a reminder with no start. Moving the start moves where the count begins,
// so a COUNT is lowered by the times stepped past, or a reminder meant to
// repeat three times would repeat for ever.
func advanceToNext(todo *ical.Component) bool {
	due, start := todo.Props.Get(ical.PropDue), todo.Props.Get(ical.PropDateTimeStart)
	anchor := start
	if anchor == nil {
		anchor = due
	}
	if anchor == nil {
		return false
	}
	current, err := anchor.DateTime(time.UTC)
	if err != nil {
		return false
	}
	// The repeat worked out as though the anchor were the start, with the
	// dates the file adds and takes away, and again with the rule alone,
	// which is what a COUNT counts.
	withDates := &ical.Component{Name: ical.CompToDo, Props: ical.Props{}}
	ruleAlone := &ical.Component{Name: ical.CompToDo, Props: ical.Props{}}
	startProperty := *anchor
	startProperty.Name = ical.PropDateTimeStart
	for _, component := range []*ical.Component{withDates, ruleAlone} {
		component.Props.Set(&startProperty)
		component.Props.Set(todo.Props.Get(ical.PropRecurrenceRule))
	}
	withDates.Props[ical.PropExceptionDates] = todo.Props[ical.PropExceptionDates]
	withDates.Props[ical.PropRecurrenceDates] = todo.Props[ical.PropRecurrenceDates]
	set, err := withDates.RecurrenceSet(time.UTC)
	if err != nil || set == nil {
		return false
	}
	var next time.Time
	iterator := set.Iterator()
	for step := 0; step < maximumSteps; step++ {
		when, ok := iterator()
		if !ok {
			return false
		}
		if when.After(current) {
			next = when
			break
		}
	}
	if next.IsZero() {
		return false
	}
	rule := todo.Props.Get(ical.PropRecurrenceRule)
	if options, err := todo.Props.RecurrenceRule(); err == nil && options != nil && options.Count > 0 {
		ruleSet, err := ruleAlone.RecurrenceSet(time.UTC)
		if err != nil || ruleSet == nil {
			return false
		}
		steppedPast := 0
		ruleIterator := ruleSet.Iterator()
		for step := 0; step < maximumSteps; step++ {
			when, ok := ruleIterator()
			if !ok || !when.Before(next) {
				break
			}
			steppedPast++
		}
		remainingCount := options.Count - steppedPast
		if remainingCount < 1 {
			remainingCount = 1
		}
		rule.Value = replaceRulePart(rule.Value, "COUNT", fmt.Sprint(remainingCount))
	}
	offset := next.Sub(current)
	if start != nil {
		moveProperty(start, next)
	}
	if due != nil {
		if start == nil {
			moveProperty(due, next)
		} else if dueAt, err := due.DateTime(time.UTC); err == nil {
			moveProperty(due, dueAt.Add(offset))
		}
	}
	todo.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
	todo.Props.Del(ical.PropCompleted)
	todo.Props.Del(ical.PropPercentComplete)
	return true
}

// moveProperty writes a new moment into a date or time property the way it
// was already written: a day, a UTC time, a time in its TZID, or a floating
// time.
func moveProperty(property *ical.Prop, at time.Time) {
	value := strings.TrimSpace(property.Value)
	switch {
	case isDateValue(property):
		property.Value = at.Format("20060102")
	case strings.HasSuffix(value, "Z"):
		property.Value = at.UTC().Format("20060102T150405Z")
	case property.Params.Get(ical.PropTimezoneID) != "":
		if location, err := time.LoadLocation(property.Params.Get(ical.PropTimezoneID)); err == nil {
			at = at.In(location)
		}
		property.Value = at.Format("20060102T150405")
	default:
		property.Value = at.Format("20060102T150405")
	}
}

// replaceRulePart sets one NAME=value part of a repeat rule's text, leaving
// the others as they were written.
func replaceRulePart(rule, name, value string) string {
	parts := strings.Split(rule, ";")
	for index, part := range parts {
		if key, _, found := strings.Cut(part, "="); found && strings.EqualFold(strings.TrimSpace(key), name) {
			parts[index] = name + "=" + value
		}
	}
	return strings.Join(parts, ";")
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
