package scheduling_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/scheduling"
	"github.com/ziyan/teanode/internal/storage"
)

// held is a message store that keeps what it is given, so a test can put a
// message where the worker will look for it without a disk.
type held struct {
	headers map[string][]string
	bodies  map[string][]byte
}

func newHeld() *held {
	return &held{headers: map[string][]string{}, bodies: map[string][]byte{}}
}

func (self *held) Put(ctx context.Context, id string, headers []string, body []byte) error {
	self.headers[id] = headers
	self.bodies[id] = body
	return nil
}

func (self *held) Get(ctx context.Context, id string) ([]string, []byte, error) {
	headers, ok := self.headers[id]
	if !ok {
		return nil, nil, storage.ErrNotFound
	}
	return headers, self.bodies[id], nil
}

func (self *held) Delete(ctx context.Context, id string) error { return nil }
func (self *held) Close() error                                { return nil }

func (self *held) PutFile(ctx context.Context, id string, content []byte) error { return nil }
func (self *held) GetFile(ctx context.Context, id string) ([]byte, error) {
	return nil, storage.ErrNotFound
}
func (self *held) DeleteFile(ctx context.Context, id string) error { return nil }

// invitationMessage is a message carrying a calendar part, in the shape a
// calendar program sends.
func invitationMessage(method, uid, summary string, extra ...string) ([]string, []byte) {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Example//EN", "METHOD:" + method,
		"BEGIN:VEVENT", "UID:" + uid, "DTSTAMP:20260912T120000Z",
		"DTSTART:20260914T100000Z", "DTEND:20260914T110000Z", "SUMMARY:" + summary,
	}
	lines = append(lines, extra...)
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")
	text := strings.Join(lines, "\r\n") + "\r\n"
	headers := []string{
		"Content-Type: multipart/alternative; boundary=\"edge\"",
		"MIME-Version: 1.0",
	}
	body := []byte("--edge\r\nContent-Type: text/plain\r\n\r\nPlease come\r\n" +
		"--edge\r\nContent-Type: text/calendar; method=" + method + "; charset=utf-8\r\n\r\n" +
		text + "--edge--\r\n")
	return headers, body
}

// stage is a server with one account, a calendar, and a worker.
type stage struct {
	database  db.Database
	store     *held
	scheduler *scheduling.Scheduler
	userID    string
	mailboxID string
	// The address delivery recorded, which is what an invitation is
	// checked against.
	recipient string
}

func newStage(t *testing.T) (*stage, func()) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	here := &stage{database: database, store: newHeld(), recipient: "alice@example.com"}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		here.userID = owner.ID
		if _, err := tx.CreateCalendar(&models.Calendar{UserID: owner.ID, Name: "Calendar"}); err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		// A mailbox with an address, because an invitation has to be
		// addressed to the person whose calendar it enters.
		domain, err := tx.CreateDomain(&models.Domain{ID: "example.com", Domain: "example.com"})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		here.mailboxID = mailbox.ID
		if _, err := tx.CreateAlias(&models.Alias{
			DomainID: domain.ID, Pattern: "^alice$", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID,
		}); err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
	})
	here.scheduler = scheduling.New(database, here.store, scheduling.Settings{Instance: "test"})
	return here, closeDatabase
}

// deliver records a message, puts it in storage under the identifier the
// database gave it, and notes it the way delivery would.
//
// The identifier comes back from the database rather than being chosen here,
// because that is where it is minted -- a test that picks its own stores the
// message somewhere the worker will never look.
func (self *stage) deliver(t *testing.T, name string, passedDMARC bool, headers []string, body []byte) {
	t.Helper()
	self.deliverFrom(t, name, "grace@example.com", passedDMARC, headers, body)
}

// deliverFrom is the same, from a named sender: what the file claims about
// who is speaking is checked against this, so it is the interesting variable.
func (self *stage) deliverFrom(t *testing.T, name, from string, passedDMARC bool, headers []string, body []byte) {
	t.Helper()
	var mailId string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		mail := &models.Mail{ReceivedAt: time.Now(), From: from}
		if passedDMARC {
			mail.AuthenticationResults.DMARC = &models.DMARCResult{Result: "pass"}
		} else {
			mail.AuthenticationResults.DMARC = &models.DMARCResult{Result: "fail"}
		}
		created, err := tx.CreateMail(mail, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		mailId = created.ID
		if _, err := tx.NoteCalendarInvitation(&models.CalendarInvitation{
			UserID: self.userID, MailboxID: self.mailboxID, ItemID: "item-" + name, MailID: mailId,
			Recipient: self.recipient,
		}); err != nil {
			t.Fatalf("NoteCalendarInvitation: %s", err)
		}
	})
	if err := self.store.Put(context.Background(), mailId, headers, body); err != nil {
		t.Fatalf("storing: %s", err)
	}
}

// work runs one tick.
func (self *stage) work(t *testing.T) {
	t.Helper()
	if err := self.scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("a tick: %s", err)
	}
}

func (self *stage) invitation(t *testing.T, mailId string) *models.CalendarInvitation {
	t.Helper()
	var found *models.CalendarInvitation
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		found, err = tx.GetCalendarInvitationForItem(self.mailboxID, "item-"+mailId)
		if err != nil {
			t.Fatalf("reading it back: %s", err)
		}
	})
	if found == nil {
		t.Fatalf("no row for %q", mailId)
	}
	return found
}

func (self *stage) events(t *testing.T) []*models.CalendarObject {
	t.Helper()
	var found []*models.CalendarObject
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		calendars, err := tx.ListCalendars(self.userID)
		if err != nil || len(calendars) == 0 {
			t.Fatalf("listing calendars: %v %v", calendars, err)
		}
		if found, err = tx.ListCalendarObjects(calendars[0].ID); err != nil {
			t.Fatalf("listing events: %s", err)
		}
	})
	return found
}

// An invitation that arrives as mail becomes an event, waiting on an answer.
func TestAnInvitationBecomesAnEvent(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "mail1", "grace@example.com", true, headers, body)
	here.work(t)

	invitation := here.invitation(t, "mail1")
	if invitation.Status != models.CalendarInvitationRead {
		t.Fatalf("it was read: %s %s", invitation.Status, invitation.Error)
	}
	if invitation.Method != models.CalendarMethodRequest || invitation.UID != "the-meeting" {
		t.Fatalf("what it was: %+v", invitation)
	}
	events := here.events(t)
	if len(events) != 1 || events[0].Summary != "Planning" {
		t.Fatalf("one event: %+v", events)
	}
	if invitation.ObjectID != events[0].ID {
		t.Fatalf("and the row points at it: %q %q", invitation.ObjectID, events[0].ID)
	}
}

// An invitation from a sender that did not prove where it came from is filed
// like any other message and is not an invitation.
//
// An invitation is an instruction to write something into somebody's
// calendar. A forged one should not be, and "somebody sent you a meeting" is
// an easy thing to fake convincingly.
func TestAnUnprovenInvitationIsNotOne(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "forged", "Not really",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliver(t, "mail1", false, headers, body)
	here.work(t)

	invitation := here.invitation(t, "mail1")
	if invitation.Status != models.CalendarInvitationIgnored {
		t.Fatalf("it should have been left alone: %s", invitation.Status)
	}
	if len(here.events(t)) != 0 {
		t.Fatal("and nothing should have been put in the calendar")
	}
}

// An older copy of an event arriving late does not undo a change the person
// has already seen. Mail is not ordered, so this happens.
func TestAnOlderVersionArrivingLateIsIgnored(t *testing.T) {
	here, done := newStage(t)
	defer done()

	newer, newerBody := invitationMessage("REQUEST", "the-meeting", "Moved to the big room",
		"SEQUENCE:5", "ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "mail1", "grace@example.com", true, newer, newerBody)
	here.work(t)

	older, olderBody := invitationMessage("REQUEST", "the-meeting", "The small room",
		"SEQUENCE:2", "ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "mail2", "grace@example.com", true, older, olderBody)
	here.work(t)

	events := here.events(t)
	if len(events) != 1 || events[0].Summary != "Moved to the big room" {
		t.Fatalf("the newer version stands: %+v", events)
	}
	if got := here.invitation(t, "mail2"); got.Status != models.CalendarInvitationIgnored {
		t.Fatalf("and the older one was left alone: %s %s", got.Status, got.Error)
	}
}

// The organizer calling their own meeting off marks it rather than deleting
// it: being told a meeting is off is the useful part.
//
// Whether a stranger can do it is decided by who sent the message, which
// TestACancellationIsCheckedAgainstTheSenderNotTheFile covers; here the
// organizer is the sender throughout.
func TestTheOrganizerCanCallItOff(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "asked", "grace@example.com", true, headers, body)
	here.work(t)

	headers, body = invitationMessage("CANCEL", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com")
	here.deliverFrom(t, "off", "grace@example.com", true, headers, body)
	here.work(t)

	events := here.events(t)
	if len(events) != 1 || events[0].Status != "CANCELLED" {
		t.Fatalf("marked off rather than removed: %+v", events)
	}
}

// An answer from somebody who was invited updates what the organizer's copy
// says about them, and changes nothing else.
func TestAnAnswerUpdatesTheOrganizersCopy(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:grace@example.com")
	here.deliverFrom(t, "mail1", "alice@example.com", true, headers, body)
	here.work(t)

	// Her own answer, from her.
	headers, body = invitationMessage("REPLY", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:grace@example.com")
	here.deliverFrom(t, "mail2", "grace@example.com", true, headers, body)
	here.work(t)

	events := here.events(t)
	if len(events) != 1 {
		t.Fatalf("still one event: %+v", events)
	}
	parsed, err := calendar.Parse([]byte(events[0].Data))
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	if len(parsed.Attendees) != 1 || parsed.Attendees[0].Participation != "ACCEPTED" {
		t.Fatalf("she is coming: %+v", parsed.Attendees)
	}
	if parsed.Summary != "Planning" {
		t.Fatalf("and the event is otherwise untouched: %q", parsed.Summary)
	}
}

// An answer from somebody who was never invited does not add them to the
// guest list.
func TestAnAnswerFromSomebodyNotInvitedChangesNothing(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:grace@example.com")
	here.deliverFrom(t, "mail1", "alice@example.com", true, headers, body)
	here.work(t)

	// Mallory answers for herself, and was never asked.
	headers, body = invitationMessage("REPLY", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:mallory@example.com")
	here.deliverFrom(t, "mail2", "mallory@example.com", true, headers, body)
	here.work(t)

	parsed, err := calendar.Parse([]byte(here.events(t)[0].Data))
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	if len(parsed.Attendees) != 1 || parsed.Attendees[0].Address != "grace@example.com" {
		t.Fatalf("the guest list is unchanged: %+v", parsed.Attendees)
	}
	if got := here.invitation(t, "mail2"); got.Status != models.CalendarInvitationIgnored {
		t.Fatalf("and it was left alone: %s %s", got.Status, got.Error)
	}
}

// An ordinary message carries nothing, and the row saying so is swept up
// afterwards rather than kept for ever: a row per message ever delivered
// would be a second copy of the mail table that nobody reads.
func TestAnOrdinaryMessageIsForgotten(t *testing.T) {
	here, done := newStage(t)
	defer done()

	here.deliver(t, "mail1", true,
		[]string{"Content-Type: text/plain", "MIME-Version: 1.0"}, []byte("hello\r\n"))
	here.work(t)

	if got := here.invitation(t, "mail1"); got.Status != models.CalendarInvitationIgnored || got.UID != "" {
		t.Fatalf("nothing to act on: %+v", got)
	}
	// Old enough to forget.
	var removed int64
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		var err error
		removed, err = tx.SweepCalendarInvitations(time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("sweeping: %s", err)
		}
	})
	if removed != 1 {
		t.Fatalf("the row was swept up: %d", removed)
	}
}

// What was actually an invitation is kept, because it is what the reader
// looks up to draw the card and what an answer is recorded against.
func TestARealInvitationIsNotSweptUp(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "mail1", "grace@example.com", true, headers, body)
	here.work(t)

	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		removed, err := tx.SweepCalendarInvitations(time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("sweeping: %s", err)
		}
		if removed != 0 {
			t.Fatalf("a real invitation is kept: %d removed", removed)
		}
	})
}

// An invitation is acted on only when the person sending it is the organizer
// it names.
//
// The organizer is a line in the sender's own attachment, so comparing it
// against the organizer in another copy of the file only proved the sender
// could copy a name out of something they had been forwarded. What ties the
// claim to a person is the address the message came from, which DMARC has
// already proven.
func TestAnInvitationFromSomebodyWhoIsNotTheOrganizerIsIgnored(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	// Mallory has a domain of her own and passes DMARC for it.
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	if got := here.invitation(t, "forged"); got.Status != models.CalendarInvitationIgnored {
		t.Fatalf("an invitation from somebody who is not its organizer: %s %s", got.Status, got.Error)
	}
	if len(here.events(t)) != 0 {
		t.Fatal("and nothing goes in the calendar")
	}
}

// Only the organizer may call a meeting off, and that is decided by who sent
// the message rather than by what the message says about itself.
func TestACancellationIsCheckedAgainstTheSenderNotTheFile(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "asked", "grace@example.com", true, headers, body)
	here.work(t)

	// Mallory copies the organizer line out of the invitation she was
	// forwarded and sends a cancellation from her own domain.
	headers, body = invitationMessage("CANCEL", "the-meeting", "Planning",
		"ORGANIZER:mailto:grace@example.com")
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	events := here.events(t)
	if len(events) != 1 || events[0].Status == "CANCELLED" {
		t.Fatalf("a stranger cannot call off somebody else's meeting: %+v", events)
	}
	if got := here.invitation(t, "forged"); got.Status != models.CalendarInvitationIgnored {
		t.Fatalf("and it is left alone: %s %s", got.Status, got.Error)
	}
}

// Somebody may answer for themselves and nobody else.
//
// A reply carries attendee lines, and applying all of them meant anybody who
// could send mail could mark anybody else as not coming -- several people at
// once, in a single message.
func TestAnAnswerOnSomebodyElsesBehalfIsIgnored(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alan@example.com")
	here.deliverFrom(t, "mine", "alice@example.com", true, headers, body)
	here.work(t)

	// Mallory says, on her own authority, that both of them have declined.
	headers, body = invitationMessage("REPLY", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=DECLINED:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=DECLINED:mailto:alan@example.com")
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	parsed, err := calendar.Parse([]byte(here.events(t)[0].Data))
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	for _, attendee := range parsed.Attendees {
		if attendee.Participation == "DECLINED" {
			t.Fatalf("%s did not decline; a stranger said so: %+v", attendee.Address, parsed.Attendees)
		}
	}

	// And the person themselves is believed.
	headers, body = invitationMessage("REPLY", "my-meeting", "Planning",
		"ORGANIZER:mailto:alice@example.com",
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:grace@example.com")
	here.deliverFrom(t, "hers", "grace@example.com", true, headers, body)
	here.work(t)

	parsed, err = calendar.Parse([]byte(here.events(t)[0].Data))
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	var said string
	for _, attendee := range parsed.Attendees {
		if attendee.Address == "grace@example.com" {
			said = attendee.Participation
		}
	}
	if said != "ACCEPTED" {
		t.Fatalf("she answered for herself: %q", said)
	}
}

// An event already here may only be changed by the organizer it names.
func TestOnlyTheOrganizerMayChangeAnEventAlreadyHere(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "The small room",
		"SEQUENCE:1", "ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "asked", "grace@example.com", true, headers, body)
	here.work(t)

	// Somebody the file was forwarded to rewrites the meeting.
	headers, body = invitationMessage("REQUEST", "the-meeting", "Somewhere else entirely",
		"SEQUENCE:99", "ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	events := here.events(t)
	if len(events) != 1 || events[0].Summary != "The small room" {
		t.Fatalf("the organizer's own words stand: %+v", events)
	}
}

// An event this server can no longer read cannot be replaced by a stranger.
//
// The organizer check was written as "if it parses, and it names an organizer,
// and that is not the sender" -- which let two whole populations through. This
// is the first: tightening what the parser accepts is exactly what creates
// events the parser now refuses, and every one of them became replaceable by
// anyone who knew the identifier, which is every other guest on the original
// invitation.
func TestAnEventThatCannotBeReadIsNotReplaceableByAStranger(t *testing.T) {
	here, done := newStage(t)
	defer done()

	// Something stored that this server will not parse back.
	var calendarId string
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		calendars, err := tx.ListCalendars(here.userID)
		if err != nil || len(calendars) == 0 {
			t.Fatalf("listing calendars: %v %v", calendars, err)
		}
		calendarId = calendars[0].ID
		if _, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: "unreadable", CalendarID: calendarId, UID: "the-board-meeting",
			ETag: "e", Data: "this is not a calendar at all", Summary: "The board meeting",
			StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
		}, nil); err != nil {
			t.Fatalf("storing: %s", err)
		}
	})

	headers, body := invitationMessage("REQUEST", "the-board-meeting", "Pay me instead",
		"SEQUENCE:99", "ORGANIZER:mailto:mallory@evil.example",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	var after *models.CalendarObject
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		found, err := tx.GetCalendarObject(calendarId, "unreadable")
		if err != nil {
			t.Fatalf("reading back: %s", err)
		}
		after = found
	})
	if after == nil || after.Summary != "The board meeting" {
		t.Fatalf("the stored event stands: %+v", after)
	}
}

// And an event with no organizer -- every appointment somebody made for
// themselves -- cannot be replaced by a stranger either.
//
// This is the second population the old check let through: Build writes no
// ORGANIZER unless there are guests, so every personal appointment had none,
// and an empty organizer read as "nobody to check against".
func TestAnAppointmentWithNoOrganizerIsNotReplaceableByAStranger(t *testing.T) {
	here, done := newStage(t)
	defer done()

	var calendarId string
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		calendars, err := tx.ListCalendars(here.userID)
		if err != nil || len(calendars) == 0 {
			t.Fatalf("listing calendars: %v %v", calendars, err)
		}
		calendarId = calendars[0].ID
	})
	// Made the way the dashboard makes one: no guests, so no organizer.
	mine, err := calendar.Build(nil, &calendar.Fields{
		Summary: stringOf("Dentist"), StartsAt: momentOf(2026, 9, 14, 10),
		EndsAt: momentOf(2026, 9, 14, 11),
	})
	if err != nil {
		t.Fatalf("building: %s", err)
	}
	if mine.Organizer != "" {
		t.Fatalf("an appointment alone has no organizer: %q", mine.Organizer)
	}
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		if _, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: "mine", CalendarID: calendarId, UID: mine.UID, ETag: calendar.ETag(mine.Data),
			Data: string(mine.Data), Summary: mine.Summary,
			StartsAt: mine.StartsAt, EndsAt: mine.EndsAt,
		}, nil); err != nil {
			t.Fatalf("storing: %s", err)
		}
	})

	headers, body := invitationMessage("REQUEST", mine.UID, "Pay me bitcoin",
		"SEQUENCE:99", "ORGANIZER:mailto:mallory@evil.example",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		found, err := tx.GetCalendarObject(calendarId, "mine")
		if err != nil {
			t.Fatalf("reading back: %s", err)
		}
		if found == nil || found.Summary != "Dentist" {
			t.Fatalf("their own appointment stands: %+v", found)
		}
	})
}

// An invitation that does not ask this person is not put in their calendar.
//
// Otherwise any sender who passes DMARC can write an event with a title of
// their choosing into anybody's calendar -- which makes a calendar somewhere
// to put text in front of a person.
func TestAnInvitationThatAsksSomebodyElseIsIgnored(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "not-for-you", "Nothing to do with you",
		"ORGANIZER:mailto:mallory@evil.example",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:nobody@elsewhere.example")
	here.deliverFrom(t, "stranger", "mallory@evil.example", true, headers, body)
	here.work(t)

	if got := here.invitation(t, "stranger"); got.Status != models.CalendarInvitationIgnored {
		t.Fatalf("it asks somebody else: %s %s", got.Status, got.Error)
	}
	if len(here.events(t)) != 0 {
		t.Fatal("and nothing goes in the calendar")
	}
}

func stringOf(value string) *string { return &value }

func momentOf(year, month, day, hour int) *time.Time {
	at := time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
	return &at
}

// An invitation reaches a mailbox that advertises no address of its own.
//
// A mailbox reached by a catch-all has none: that alias has no one address to
// send as, so it is deliberately left out of the list a mailbox advertises.
// Checking an invitation against that list therefore refused every invitation
// such a mailbox ever received -- a fail-closed check that lost mail. What the
// question actually wants is the address the sender wrote to.
func TestAnInvitationToACatchAllStillArrives(t *testing.T) {
	here, done := newStage(t)
	defer done()

	// A mailbox whose only way in is a catch-all, so it advertises nothing.
	var mailboxId string
	dbtest.RunTransactionOn(t, here.database, func(tx db.Transaction) {
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: here.userID, Name: "Everything"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		mailboxId = mailbox.ID
		if _, err := tx.CreateAlias(&models.Alias{
			DomainID: "example.com", Pattern: ".*", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID,
		}); err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
		found, err := tx.GetMailbox(mailbox.ID)
		if err != nil {
			t.Fatalf("GetMailbox: %s", err)
		}
		if len(found.Addresses) != 0 {
			t.Fatalf("a catch-all advertises no address, so this test is about nothing: %+v", found.Addresses)
		}
	})
	here.mailboxID = mailboxId
	here.recipient = "anything@example.com"

	headers, body := invitationMessage("REQUEST", "caught", "Planning",
		"ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:anything@example.com")
	here.deliverFrom(t, "caught", "grace@example.com", true, headers, body)
	here.work(t)

	if got := here.invitation(t, "caught"); got.Status != models.CalendarInvitationRead {
		t.Fatalf("it asks the address it was sent to: %s %s", got.Status, got.Error)
	}
	if len(here.events(t)) != 1 {
		t.Fatal("and it goes in the calendar")
	}
}

// Naming yourself as the organizer does not make you one.
//
// This is the hole the second round opened while adding SENT-BY: the check
// fell through to "is the sender the organizer of the arriving file", and the
// arriving file is the attacker's. So it read "is the sender the person the
// sender says they are", which is always true. Anyone who knows an event's
// identifier -- every other guest on the original invitation -- could rewrite
// it or call it off.
func TestNamingYourselfAsOrganizerDoesNotMakeYouOne(t *testing.T) {
	here, done := newStage(t)
	defer done()

	headers, body := invitationMessage("REQUEST", "the-meeting", "The small room",
		"SEQUENCE:1", "ORGANIZER:mailto:grace@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "asked", "grace@example.com", true, headers, body)
	here.work(t)

	// Mallory names herself as the organizer of somebody else's event.
	headers, body = invitationMessage("REQUEST", "the-meeting", "PWNED",
		"SEQUENCE:99", "ORGANIZER:mailto:mallory@evil.example",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "forged", "mallory@evil.example", true, headers, body)
	here.work(t)

	events := here.events(t)
	if len(events) != 1 || events[0].Summary != "The small room" {
		t.Fatalf("the organizer's own words stand: %+v", events)
	}

	// And the same trick to call it off.
	headers, body = invitationMessage("CANCEL", "the-meeting", "The small room",
		"ORGANIZER:mailto:mallory@evil.example")
	here.deliverFrom(t, "forgedOff", "mallory@evil.example", true, headers, body)
	here.work(t)

	events = here.events(t)
	if len(events) != 1 || events[0].Status == "CANCELLED" {
		t.Fatalf("and she cannot call it off either: %+v", events)
	}

	// An assistant genuinely sending for the organizer still works: the file
	// names the organizer the event already has, and says who posted it.
	headers, body = invitationMessage("REQUEST", "the-meeting", "The big room",
		"SEQUENCE:2", `ORGANIZER;SENT-BY="mailto:assistant@example.com":mailto:grace@example.com`,
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:alice@example.com")
	here.deliverFrom(t, "assistant", "assistant@example.com", true, headers, body)
	here.work(t)

	events = here.events(t)
	if len(events) != 1 || events[0].Summary != "The big room" {
		t.Fatalf("an assistant may send for the organizer: %+v", events)
	}
}
