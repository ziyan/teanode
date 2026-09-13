package scheduling

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/periodic"
	"github.com/ziyan/teanode/internal/util/security"
)

// Settings are what the worker needs.
type Settings struct {
	// Tick is how often to look for messages waiting to be read. An
	// invitation is not urgent -- somebody reads their mail in minutes, not
	// milliseconds -- but it should be there by the time they open it.
	Tick time.Duration

	// PerTick is how many to read at once.
	PerTick int

	// Instance names this process in a claim, so that two of them can run
	// without reading the same message twice.
	Instance string
}

// Scheduler reads the invitations that arrive as mail.
type Scheduler struct {
	database db.Database
	store    storage.Storage
	settings Settings

	ctx       context.Context
	cancel    context.CancelFunc
	waitGroup sync.WaitGroup
	worker    periodic.Periodic
}

// New builds the scheduler.
func New(database db.Database, store storage.Storage, settings Settings) *Scheduler {
	if settings.Tick <= 0 {
		settings.Tick = 30 * time.Second
	}
	if settings.PerTick <= 0 {
		settings.PerTick = 20
	}
	if strings.TrimSpace(settings.Instance) == "" {
		settings.Instance = security.NewULID()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		database: database, store: store, settings: settings,
		ctx: ctx, cancel: cancel,
	}
}

// Start begins reading what delivery has noted.
func (self *Scheduler) Start() {
	self.worker = periodic.New(self.ctx, &self.waitGroup, self.Tick, &periodic.Settings{
		Interval: self.settings.Tick,
		Name:     "scheduling:worker",
	})
	self.worker.Start()
}

// Stop ends the worker and waits for what is in flight.
func (self *Scheduler) Stop() {
	self.cancel()
	if self.worker != nil {
		self.worker.Stop()
	}
	self.waitGroup.Wait()
}

// OnMailboxDelivery is the delivery hook: a message has just been placed in a
// mailbox, inside the delivery transaction.
//
// All it does is write a row. Whether the message actually carries an
// invitation is decided later, by a worker, because deciding it here means
// fetching the message from storage -- and a slow read inside the SMTP
// transaction is mail this server refuses after having accepted it.
//
// So a row is written for every delivered message, and most of them turn out
// to be nothing. That is the cheap end of the trade: one insert against a
// read that could block. Guessing from what is already known about the
// message was tried and thrown away -- a calendar part is not always counted
// as an attachment, so the guess would have had to say yes almost always to
// be safe, and a guard that always says yes is not a guard.
//
// The rows that turn out to be nothing are swept up afterwards; see sweep.
func (self *Scheduler) OnMailboxDelivery(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail) {
	if mailbox == nil || item == nil || mail == nil {
		return
	}
	if _, err := tx.NoteCalendarInvitation(&models.CalendarInvitation{
		UserID: mailbox.UserID, MailboxID: mailbox.ID, ItemID: item.ID, MailID: mail.ID,
	}); err != nil {
		// Logged and dropped. An invitation that is not noticed is a
		// person having to add an appointment by hand; a delivery that
		// fails because of it is a message that bounces.
		log.Warningf("a delivered message could not be noted as a possible invitation: %s", err)
	}
}

// Tick reads what is waiting. Exported so a test can run one turn of the
// worker without starting it and waiting.
func (self *Scheduler) Tick(ctx context.Context) error {
	var claimed []*models.CalendarInvitation
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		claimed, err = tx.ClaimCalendarInvitations(self.settings.Instance, self.settings.PerTick, time.Now())
		return err
	}); err != nil {
		return err
	}
	for _, invitation := range claimed {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		self.read(ctx, invitation)
	}
	self.sweep(ctx)
	self.reindex(ctx)
	return nil
}

// indexedAhead is how close to running out a repeat may get before its
// occurrences are worked out further.
//
// Comfortably inside the horizon the index is written to, so a standing
// meeting is extended long before anybody could scroll to the end of it.
const indexedAhead = 300 * 24 * time.Hour

// reindexPerTick bounds the work: extending a repeat costs an expansion and a
// write, and there is no hurry -- what is being fixed is a year away.
const reindexPerTick = 20

// reindex works out further occurrences for the repeats that are running out.
//
// The index reaches a horizon set when an event was written, and nothing
// moved it: a weekly meeting saved today stopped appearing two years from
// now, everywhere the index is read -- the calendar page, the agent, free-busy
// and what a phone is told -- while the event itself was still there. A
// repeat whose start was past the old horizon was never indexed at all.
func (self *Scheduler) reindex(ctx context.Context) {
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		running, err := tx.ListCalendarObjectsRunningOut(time.Now().Add(indexedAhead), reindexPerTick)
		if err != nil {
			return err
		}
		for _, object := range running {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			parsed, err := calendar.Parse([]byte(object.Data))
			if err != nil {
				// Kept text that cannot be read is this server's problem,
				// and re-reading it every tick would say so every tick.
				log.Debugf("a kept event could not be read to extend it: %s", err)
				continue
			}
			occurrences, indexedUntil, err := indexed(parsed)
			if err != nil {
				log.Debugf("a repeat could not be worked out further: %s", err)
				continue
			}
			// Recorded, so this one is done once per advance of the
			// horizon rather than found again on the next tick and for
			// ever after -- which also kept it permanently in front of
			// the repeats that genuinely needed extending.
			object.IndexedUntil = &indexedUntil
			if _, err := tx.PutCalendarObject(object, occurrences); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Warningf("the repeats that are running out could not be extended: %s", err)
	}
}

// retryAfter is how long to wait before reading a message again after a
// failure, doubled on each attempt. The first wait is short because most
// failures are a moment of storage being busy; the last is long enough to
// outlive an outage rather than spending every attempt inside one.
const retryAfter = 30 * time.Second

// keptAfterNothing is how long a message that carried no invitation is
// remembered. Long enough that a worker restarting does not read the same
// messages again, short enough that the table does not become a second copy
// of every message ever delivered.
const keptAfterNothing = 48 * time.Hour

// sweep removes the rows that came to nothing.
func (self *Scheduler) sweep(ctx context.Context) {
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		removed, err := tx.SweepCalendarInvitations(time.Now().Add(-keptAfterNothing))
		if err == nil && removed > 0 {
			log.Debugf("forgot %d messages that carried no invitation", removed)
		}
		return err
	}); err != nil {
		log.Warningf("the invitations that came to nothing could not be swept up: %s", err)
	}
}

// read works out what one message carried, and does something about it.
func (self *Scheduler) read(ctx context.Context, invitation *models.CalendarInvitation) {
	invitation.ClaimedBy = self.settings.Instance
	outcome, err := self.consider(ctx, invitation)
	if err != nil {
		invitation.Status = models.CalendarInvitationWaiting
		invitation.Error = err.Error()
		// Waited on before it is tried again, and for longer each time.
		// Claiming set this in the database, but the row in hand still
		// carried what it had before -- nil -- and finishing wrote that
		// back, so a failure was claimable again on the next tick. Storage
		// being unreachable for three minutes burnt every attempt and lost
		// the invitation for good.
		wait := retryAfter << min(invitation.Attempts, 5)
		when := time.Now().Add(wait)
		invitation.NotBefore = &when
		if invitation.Attempts >= db.MaximumInvitationAttempts {
			// A message that cannot be read will not become readable.
			invitation.Status = models.CalendarInvitationIgnored
		}
		log.Warningf("an invitation in message %q could not be read: %s", invitation.MailID, err)
	} else {
		invitation.Status = outcome.status
		invitation.Error = outcome.because
		invitation.Method = outcome.method
		invitation.UID = outcome.uid
		invitation.Sequence = outcome.sequence
		invitation.Organizer = outcome.organizer
		invitation.CalendarID = outcome.calendarId
		invitation.ObjectID = outcome.objectId
		invitation.NotBefore = nil
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.FinishCalendarInvitation(invitation)
	}); err != nil {
		log.Warningf("an invitation could not be recorded: %s", err)
	}
}

// outcome is what a message turned out to be.
type outcome struct {
	status     models.CalendarInvitationStatus
	because    string
	method     string
	uid        string
	sequence   int
	organizer  string
	calendarId string
	objectId   string
}

// ignored is a message this server does nothing with, and why.
func ignored(because string, arguments ...any) (*outcome, error) {
	return &outcome{
		status: models.CalendarInvitationIgnored, because: fmt.Sprintf(because, arguments...),
	}, nil
}

// consider reads the message and acts on what it finds.
func (self *Scheduler) consider(ctx context.Context, invitation *models.CalendarInvitation) (*outcome, error) {
	var mail *models.Mail
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		mail, err = tx.GetMail(invitation.MailID, nil)
		return err
	}); err != nil {
		return nil, err
	}
	if mail == nil {
		return ignored("the message is no longer here")
	}
	headers, body, err := self.store.Get(ctx, mail.ID)
	if err != nil {
		return nil, fmt.Errorf("the message could not be read from storage: %w", err)
	}
	text := CalendarPart(headers, body)
	if text == nil {
		return ignored("no calendar part")
	}
	parsed, err := calendar.Parse(text)
	if err != nil {
		return ignored("the calendar part could not be read: %s", err)
	}
	if parsed.Method == "" {
		// A calendar with no method is somebody sending an event to look
		// at rather than asking anything of anybody. Not acted on: adding
		// it to a calendar unasked is putting things in somebody's diary
		// because they were sent a file.
		return ignored("the calendar part asks for nothing")
	}

	// An invitation is an instruction to write into somebody's calendar,
	// and a forged one should not be. A message that did not prove it came
	// from where it says is filed like any other message and shown like
	// any other message, but it is not an invitation.
	if !mail.DMARCPassed() {
		return ignored("the message did not prove where it came from")
	}

	// Who actually sent it. DMARC proves the From domain is theirs to use,
	// so this is the one identity in the message worth anything -- and every
	// claim the file makes about who is speaking is checked against it.
	// Without that the checks below are the sender marking their own
	// homework: an attacker writes whatever ORGANIZER makes it work.
	sender := strings.ToLower(strings.TrimSpace(mail.From))
	if !strings.Contains(sender, "@") {
		return ignored("the message does not say who it is from")
	}

	switch parsed.Method {
	case models.CalendarMethodRequest:
		return self.request(ctx, invitation, parsed, sender)
	case models.CalendarMethodCancel:
		return self.callOff(ctx, invitation, parsed, sender)
	case models.CalendarMethodReply:
		return self.reply(ctx, invitation, parsed, sender)
	}
	return ignored("this server does not act on %s", parsed.Method)
}

// addressedTo is whether an invitation actually asks the person whose mailbox
// it arrived in -- as an attendee, or as the organizer of their own event
// coming back to them.
//
// Checked against every address of the mailbox it was delivered to, since
// that is the one the sender wrote to.
func (self *Scheduler) addressedTo(ctx context.Context, mailboxId string, parsed *calendar.Parsed) (bool, error) {
	theirs := map[string]bool{}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		mailbox, err := tx.GetMailbox(mailboxId)
		if err != nil || mailbox == nil {
			return err
		}
		for _, address := range mailbox.Addresses {
			theirs[strings.ToLower(strings.TrimSpace(address.Address))] = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	if len(theirs) == 0 {
		// Nothing to check against, which should not happen: the message
		// was delivered to this mailbox, so an address routed it there.
		// Treated as a failure rather than as permission -- the invitation
		// waits and is read again -- because the alternative is a check
		// that turns itself off exactly when it cannot see.
		return false, fmt.Errorf("the mailbox this arrived at has no address to check against")
	}
	if theirs[strings.ToLower(strings.TrimSpace(parsed.Organizer))] {
		return true, nil
	}
	for _, attendee := range parsed.Attendees {
		if theirs[strings.ToLower(strings.TrimSpace(attendee.Address))] {
			return true, nil
		}
	}
	return false, nil
}

// mayActFor is whether the sender may act on an event this organizer called:
// they are that organizer, or the arriving file says they sent it on that
// organizer's behalf.
//
// An event naming no organizer is nobody's to act on from outside -- it is an
// appointment its owner made for themselves, and treating an absent organizer
// as nobody-to-check-against made every one of them replaceable by a stranger.
func mayActFor(organizer string, parsed *calendar.Parsed, sender string) bool {
	if strings.TrimSpace(organizer) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(organizer), sender) {
		return true
	}
	return speaksFor(parsed, sender)
}

// speaksFor is whether the sender is entitled to speak as the organizer of
// this file: they are the organizer, or the file says they sent it on the
// organizer's behalf.
//
// SENT-BY is how an assistant, a room booking system or a sending service
// says so, and it is the sender's own claim -- but it is a claim about an
// address the message has proven, which is the part that matters. It lets
// somebody send as an organizer they name; it does not let them touch an
// event they were not already able to.
func speaksFor(parsed *calendar.Parsed, sender string) bool {
	if strings.EqualFold(strings.TrimSpace(parsed.Organizer), sender) {
		return true
	}
	return parsed.SentBy != "" && strings.EqualFold(parsed.SentBy, sender)
}

// calendarFor is the calendar an invitation goes into: the person's own,
// made if they have never had one.
func (self *Scheduler) calendarFor(tx db.Transaction, userId string) (*models.Calendar, error) {
	found, err := tx.ListCalendars(userId)
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		return found[0], nil
	}
	return tx.CreateCalendar(&models.Calendar{UserID: userId, Name: "Calendar"})
}

// request is somebody asking this person to a meeting.
func (self *Scheduler) request(ctx context.Context, invitation *models.CalendarInvitation,
	parsed *calendar.Parsed, sender string) (*outcome, error) {
	// An invitation is from the person calling the meeting. One that names
	// somebody else as the organizer is either forwarded -- in which case it
	// is not an invitation to this person -- or forged.
	if !speaksFor(parsed, sender) {
		return ignored("an invitation from somebody who is not its organizer")
	}
	// And it has to be addressed to them. Without this, any sender could
	// write an event with a title of their choosing into anybody's
	// calendar -- an invitation naming only strangers still landed, which
	// makes a calendar somewhere to put text in front of a person.
	asked, err := self.addressedTo(ctx, invitation.MailboxID, parsed)
	if err != nil {
		return nil, err
	}
	if !asked {
		return ignored("an invitation that does not ask this person")
	}
	result := &outcome{
		status: models.CalendarInvitationRead, method: parsed.Method,
		uid: parsed.UID, sequence: parsed.Sequence, organizer: parsed.Organizer,
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := self.calendarFor(tx, invitation.UserID)
		if err != nil {
			return err
		}
		result.calendarId = found.ID
		existing, err := tx.GetCalendarObjectByUID(found.ID, parsed.UID)
		if err != nil {
			return err
		}
		// A calendar holds only so much, and this door went straight to
		// the storage while the other two counted first -- so anybody who
		// could send mail could write into somebody's calendar without
		// limit. A ceiling enforced at two doors of three is not one.
		if existing == nil {
			held, err := tx.CountCalendarObjects(found.ID)
			if err != nil {
				return err
			}
			if held >= db.ObjectsPerCalendar {
				result.status = models.CalendarInvitationIgnored
				result.because = "this calendar already holds as many events as this server keeps"
				return nil
			}
		}
		if existing != nil {
			// Only the organizer the event already names may change it.
			// Without this, anybody the file was ever forwarded to could
			// rewrite the time and the title of a meeting somebody else
			// called, with nothing kept to recover from.
			//
			// Closed on every doubt, which took two goes. Written as "if
			// it parses, and it names an organizer, and that is not the
			// sender", it let two whole populations through: an event
			// this server can no longer read -- and tightening what it
			// will read is exactly what creates those -- and an event
			// with no organizer at all, which is every appointment
			// somebody made for themselves. Both were then replaceable by
			// any stranger who knew the identifier, which every other
			// guest on the original invitation does.
			held, err := calendar.Parse([]byte(existing.Data))
			if err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = "a change to an event this server can no longer read"
				result.objectId = existing.ID
				return nil
			}
			if !mayActFor(held.Organizer, parsed, sender) {
				result.status = models.CalendarInvitationIgnored
				result.because = "a change to an event from somebody who is not its organizer"
				result.objectId = existing.ID
				return nil
			}
			// An organizer increments the sequence each time they change
			// an event. One carrying a lower number than what is already
			// held is an older copy arriving late -- mail is not ordered
			// -- and acting on it would undo a change the person has
			// already seen.
			if parsed.Sequence < held.Sequence {
				result.status = models.CalendarInvitationIgnored
				result.because = "an older version of an event already here"
				result.objectId = existing.ID
				return nil
			}
		}
		occurrences, indexedUntil, err := indexed(parsed)
		if err != nil {
			return err
		}
		object := &models.CalendarObject{
			CalendarID: found.ID, UID: parsed.UID, ETag: calendar.ETag(parsed.Data),
			Data: string(parsed.Data), Summary: parsed.Summary, Location: parsed.Location,
			StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt,
			AllDay: parsed.AllDay, Recurring: parsed.Recurring, Status: parsed.Status,
			IndexedUntil: &indexedUntil,
		}
		if existing != nil {
			object.ID = existing.ID
			object.CreatedAt = existing.CreatedAt
		}
		kept, err := tx.PutCalendarObject(object, occurrences)
		if err != nil {
			return err
		}
		result.objectId = kept.ID
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// callOff is the organizer calling a meeting off. Not named cancel: that
// is the context's, and a field and a method cannot share a name.
func (self *Scheduler) callOff(ctx context.Context, invitation *models.CalendarInvitation,
	parsed *calendar.Parsed, sender string) (*outcome, error) {
	result := &outcome{
		status: models.CalendarInvitationRead, method: parsed.Method,
		uid: parsed.UID, sequence: parsed.Sequence, organizer: parsed.Organizer,
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := self.calendarFor(tx, invitation.UserID)
		if err != nil {
			return err
		}
		result.calendarId = found.ID
		existing, err := tx.GetCalendarObjectByUID(found.ID, parsed.UID)
		if err != nil {
			return err
		}
		// A calendar holds only so much, and this door went straight to
		// the storage while the other two counted first -- so anybody who
		// could send mail could write into somebody's calendar without
		// limit. A ceiling enforced at two doors of three is not one.
		if existing == nil {
			held, err := tx.CountCalendarObjects(found.ID)
			if err != nil {
				return err
			}
			if held >= db.ObjectsPerCalendar {
				result.status = models.CalendarInvitationIgnored
				result.because = "this calendar already holds as many events as this server keeps"
				return nil
			}
		}
		if existing == nil {
			result.status = models.CalendarInvitationIgnored
			result.because = "an event this calendar does not have"
			return nil
		}
		result.objectId = existing.ID
		// Only from the organizer the event already names. Anybody can
		// send a message saying a meeting is off; only the person who
		// called it can call it off, and taking anyone's word for it is a
		// way to delete somebody's appointments by writing to them.
		held, err := calendar.Parse([]byte(existing.Data))
		if err != nil {
			return fmt.Errorf("a kept event could not be read back: %w", err)
		}
		// Against the sender, not against the ORGANIZER line in the message
		// -- that line is written by whoever sent it, so comparing the two
		// only proved the attacker could copy a name out of an invitation
		// they had been forwarded.
		if !mayActFor(held.Organizer, parsed, sender) {
			result.status = models.CalendarInvitationIgnored
			result.because = "a cancellation from somebody who is not the organizer"
			return nil
		}
		// Marked cancelled rather than deleted. The person is told the
		// meeting is off, which is the useful thing; making it vanish
		// leaves them wondering whether they imagined it.
		cancelled, err := calendar.Build([]byte(existing.Data), &calendar.Fields{
			Status: statusText("CANCELLED"),
		})
		if err != nil {
			return err
		}
		occurrences, indexedUntil, err := indexed(cancelled)
		if err != nil {
			return err
		}
		_, err = tx.PutCalendarObject(&models.CalendarObject{
			ID: existing.ID, CalendarID: found.ID, UID: cancelled.UID,
			CreatedAt: existing.CreatedAt, ETag: calendar.ETag(cancelled.Data),
			Data: string(cancelled.Data), Summary: cancelled.Summary, Location: cancelled.Location,
			StartsAt: cancelled.StartsAt, EndsAt: cancelled.EndsAt,
			AllDay: cancelled.AllDay, Recurring: cancelled.Recurring, Status: cancelled.Status,
			IndexedUntil: &indexedUntil,
		}, occurrences)
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// reply is somebody this person invited saying whether they are coming.
func (self *Scheduler) reply(ctx context.Context, invitation *models.CalendarInvitation,
	parsed *calendar.Parsed, sender string) (*outcome, error) {
	result := &outcome{
		status: models.CalendarInvitationRead, method: parsed.Method,
		uid: parsed.UID, sequence: parsed.Sequence, organizer: parsed.Organizer,
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := self.calendarFor(tx, invitation.UserID)
		if err != nil {
			return err
		}
		result.calendarId = found.ID
		existing, err := tx.GetCalendarObjectByUID(found.ID, parsed.UID)
		if err != nil {
			return err
		}
		// A calendar holds only so much, and this door went straight to
		// the storage while the other two counted first -- so anybody who
		// could send mail could write into somebody's calendar without
		// limit. A ceiling enforced at two doors of three is not one.
		if existing == nil {
			held, err := tx.CountCalendarObjects(found.ID)
			if err != nil {
				return err
			}
			if held >= db.ObjectsPerCalendar {
				result.status = models.CalendarInvitationIgnored
				result.because = "this calendar already holds as many events as this server keeps"
				return nil
			}
		}
		if existing == nil {
			result.status = models.CalendarInvitationIgnored
			result.because = "an answer to an event this calendar does not have"
			return nil
		}
		result.objectId = existing.ID
		// A reply carries the answering attendee's own line and nothing
		// else worth keeping, so what is applied is exactly that: their
		// participation, onto the event already held.
		// Only for themselves. A reply carries attendee lines, and taking
		// all of them meant anybody who could send mail could mark anybody
		// else as not coming -- several at once, in one message.
		var theirs []calendar.Attendee
		for _, attendee := range parsed.Attendees {
			if strings.EqualFold(strings.TrimSpace(attendee.Address), sender) {
				theirs = append(theirs, attendee)
			}
		}
		if len(theirs) == 0 {
			result.status = models.CalendarInvitationIgnored
			result.because = "an answer on behalf of somebody else"
			return nil
		}
		updated, err := calendar.Answer([]byte(existing.Data), theirs)
		if err != nil {
			result.status = models.CalendarInvitationIgnored
			result.because = err.Error()
			return nil
		}
		occurrences, indexedUntil, err := indexed(updated)
		if err != nil {
			return err
		}
		_, err = tx.PutCalendarObject(&models.CalendarObject{
			ID: existing.ID, CalendarID: found.ID, UID: updated.UID,
			CreatedAt: existing.CreatedAt, ETag: calendar.ETag(updated.Data),
			Data: string(updated.Data), Summary: updated.Summary, Location: updated.Location,
			StartsAt: updated.StartsAt, EndsAt: updated.EndsAt,
			AllDay: updated.AllDay, Recurring: updated.Recurring, Status: updated.Status,
			IndexedUntil: &indexedUntil,
		}, occurrences)
		return err
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// indexed is when an event happens, as rows.
func indexed(parsed *calendar.Parsed) ([]models.Occurrence, time.Time, error) {
	expanded, indexedUntil, err := calendar.Indexed(parsed)
	if err != nil {
		return nil, time.Time{}, err
	}
	rows := make([]models.Occurrence, 0, len(expanded))
	for _, occurrence := range expanded {
		rows = append(rows, models.Occurrence{
			StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay,
		})
	}
	return rows, indexedUntil, nil
}

func statusText(value string) *string { return &value }
