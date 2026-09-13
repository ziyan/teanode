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

	// IndexedUntil is how far ahead the times this event happens have been
	// worked out. A repeat with no end cannot be worked out for ever, so
	// the index reaches a horizon and something extends it as that horizon
	// approaches; this is what says which events still need it.
	IndexedUntil *time.Time `json:"indexedUntil,omitempty"`

	// IndexedAt is when that work was last done. An event whose repeat is
	// too fine to reach the horizon is worked out only as far as its last
	// written occurrence, so it is always running out; this is what keeps
	// it from being done again on every tick, ahead of everybody else's.
	IndexedAt *time.Time `json:"indexedAt,omitempty"`
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

// CalendarInvitationStatus is how far an invitation that arrived as mail has
// got.
type CalendarInvitationStatus string

const (
	// Waiting: not read yet. Delivery writes this and does no more.
	CalendarInvitationWaiting CalendarInvitationStatus = "waiting"
	// Read: it was an invitation, and the calendar knows about it.
	CalendarInvitationRead CalendarInvitationStatus = "read"
	// Ignored: it carried nothing this server acts on, or it was refused.
	// Error says which, and it is kept rather than deleted so that the
	// same message is not picked up and refused again every time it is
	// looked at.
	CalendarInvitationIgnored CalendarInvitationStatus = "ignored"
)

// CalendarInvitationMethod is what a calendar part asks for.
const (
	CalendarMethodRequest = "REQUEST"
	CalendarMethodReply   = "REPLY"
	CalendarMethodCancel  = "CANCEL"
)

// CalendarInvitation is a message that carried a calendar part.
//
// It exists because reading one means fetching the message from storage and
// decoding it, and that must not happen in the SMTP transaction: a slow read
// or a failure there would bounce mail. Delivery writes the row; a worker
// reads the message afterwards.
type CalendarInvitation struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	MailboxID string `json:"mailboxId"`
	ItemID    string `json:"itemId"`
	MailID    string `json:"mailId"`

	// Recipient is the address this was delivered to, which is the one the
	// sender wrote to and so the one an invitation has to name. A mailbox's
	// advertised addresses are not the same thing: one reached by a
	// catch-all advertises none.
	Recipient  string    `json:"recipient,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Status    CalendarInvitationStatus `json:"status"`
	Attempts  int                      `json:"attempts"`
	NotBefore *time.Time               `json:"notBefore,omitempty"`
	ClaimedAt *time.Time               `json:"claimedAt,omitempty"`
	ClaimedBy string                   `json:"claimedBy,omitempty"`
	Error     string                   `json:"error,omitempty"`

	Method     string `json:"method,omitempty"`
	UID        string `json:"uid,omitempty"`
	Sequence   int    `json:"sequence,omitempty"`
	Organizer  string `json:"organizer,omitempty"`
	CalendarID string `json:"calendarId,omitempty"`
	ObjectID   string `json:"objectId,omitempty"`
}
