package models

import "time"

// Calendar is a person's own. It belongs to the account rather than to a
// mailbox: somebody with two mailboxes has one calendar, and reaches it
// through either of them.
//
// An account is given one named "Calendar" the first time it looks, so that
// nobody has to create one before they can keep an appointment.
type Calendar struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Colour is what a client paints this calendar's events, as "#rrggbb".
	// Timezone is the zone a new event is written in when nothing says
	// otherwise, as an IANA name such as "Europe/London".
	Colour   string `json:"colour,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// CalendarObject is one iCalendar file: usually one event, sometimes an event
// together with the occurrences of it that were changed one at a time.
//
// Data is the whole of it, and is what a phone sends and is given back. The
// fields beside it are pulled out of the text when it is written, so that
// listing and searching never parse iCalendar; where the two disagree the
// text is right.
type CalendarObject struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendarId"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	// UID is the file's own UID property, which is how the same event is
	// recognized across devices and how a reply is matched to what it
	// answers; ETag names a version, and is what a conditional write is
	// checked against.
	UID  string `json:"uid"`
	ETag string `json:"etag"`
	Data string `json:"data"`

	Summary  string `json:"summary,omitempty"`
	Location string `json:"location,omitempty"`

	// StartsAt and EndsAt are the first occurrence. A recurring event has
	// more, which are in the occurrence index rather than here.
	StartsAt  time.Time `json:"startsAt,omitempty"`
	EndsAt    time.Time `json:"endsAt,omitempty"`
	AllDay    bool      `json:"allDay,omitempty"`
	Recurring bool      `json:"recurring,omitempty"`

	// Status is the event's own STATUS: CONFIRMED, TENTATIVE or CANCELLED.
	Status string `json:"status,omitempty"`
}

// Occurrence is one time an event happens.
//
// These are derived from the iCalendar text and rewritten whenever an object
// is, so nothing here is authoritative. They exist so that "what is on this
// week" and "when is this person busy" are answered by reading a window
// rather than by expanding every recurrence rule in the calendar.
type Occurrence struct {
	CalendarID string    `json:"calendarId"`
	ObjectID   string    `json:"objectId"`
	StartsAt   time.Time `json:"startsAt"`
	EndsAt     time.Time `json:"endsAt"`
	AllDay     bool      `json:"allDay,omitempty"`
}
