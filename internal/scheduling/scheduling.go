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
	"github.com/ziyan/teanode/internal/util/mailparse"
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
func (self *Scheduler) OnMailboxDelivery(tx db.Transaction, mailbox *models.Mailbox, recipient string, item *models.MailboxItem, mail *models.Mail) {
	if mailbox == nil || item == nil || mail == nil {
		return
	}
	if _, err := tx.NoteCalendarInvitation(&models.CalendarInvitation{
		UserID: mailbox.UserID, MailboxID: mailbox.ID, ItemID: item.ID, MailID: mail.ID,
		Recipient: recipient,
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

// reindexedWithin is how long an event is left alone after its occurrences
// have been worked out.
//
// For the repeat that cannot reach the horizon however hard it is worked at:
// its index ends at its last written occurrence, which is inside the stretch
// the worker asks about, so it is running out the moment it is done. Without
// a rest it came back on the next tick and for ever after, in front of every
// event that could actually be extended. An hour is far shorter than any
// horizon this moves and long enough that such an event costs one expansion
// an hour rather than a hundred and twenty.
const reindexedWithin = time.Hour

// putOff moves an event that cannot be extended to the back of the queue.
//
// The horizon is written as though it had been done, so it is asked about
// again when that horizon next advances rather than on the next tick. What
// cannot be worked out now will not become workable in thirty seconds, and a
// row that keeps its place at the head of the queue holds up every other
// account's.
func (self *Scheduler) putOff(tx db.Transaction, object *models.CalendarObject) {
	until := time.Now().Add(calendar.HorizonAhead)
	object.IndexedUntil = &until
	doneAt := time.Now().UTC()
	object.IndexedAt = &doneAt
	if _, err := tx.TouchCalendarObjectHorizon(object.CalendarID, object.ID, until); err != nil {
		log.Debugf("an event that cannot be extended could not be put off: %s", err)
	}
}

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
		running, err := tx.ListCalendarObjectsRunningOut(
			time.Now().Add(indexedAhead), time.Now().Add(-reindexedWithin), reindexPerTick)
		if err != nil {
			return err
		}
		for _, object := range running {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// A row that cannot be extended is still moved on. Skipping
			// it left it exactly where the query looks first, so it came
			// back every tick for ever -- and twenty of them stopped
			// re-indexing for every account on the server, since the
			// question is asked of the whole table. Moving it on means it
			// is tried again at the next advance of the horizon rather
			// than thirty seconds later.
			parsed, err := calendar.Parse([]byte(object.Data))
			if err != nil {
				log.Debugf("a kept event could not be read to extend it: %s", err)
				self.putOff(tx, object)
				continue
			}
			occurrences, indexedUntil, err := indexed(parsed)
			if err != nil {
				log.Debugf("a repeat could not be worked out further: %s", err)
				self.putOff(tx, object)
				continue
			}
			// Recorded, so this one is done once per advance of the
			// horizon rather than found again on the next tick and for
			// ever after -- which also kept it permanently in front of
			// the repeats that genuinely needed extending.
			object.IndexedUntil = &indexedUntil
			doneAt := time.Now().UTC()
			object.IndexedAt = &doneAt
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

	// Everything that can be decided about the message itself is decided
	// before the calendar part is read.
	//
	// Reading it is the expensive half -- an iCalendar file describing
	// hundreds of zones this machine cannot name costs about a second of a
	// core -- and it was being done for every message carrying such a part,
	// including the ones about to be refused for having proved nothing about
	// where they came from. So the work an attacker could ask for was not
	// bounded by whether this server would act on their message at all.
	if !mail.DMARCPassed() && !mail.SubmittedHere() {
		return ignored("the message did not prove where it came from")
	}
	if mail.LooksLikeSpam() {
		return ignored("the message looks like spam")
	}
	if header := strings.ToLower(strings.TrimSpace(
		mailparse.FindHeaderValue(headers, "Precedence"))); header == "bulk" || header == "junk" {
		return ignored("the message is bulk mail")
	}
	for _, header := range []string{"List-Id", "List-Post", "List-Unsubscribe"} {
		if mailparse.FindHeaderValue(headers, header) != "" {
			return ignored("the message came through a mailing list")
		}
	}
	// Auto-Submitted is deliberately not among these. An invitation is
	// written by a program and the format says so: every one carries
	// "auto-generated", so refusing on it would refuse nearly all of them.
	// It is in the automatic reply's ladder because answering a machine is
	// how loops start, which is a different question from this one.

	// Who actually sent it. DMARC proves the From domain is theirs to use,
	// so this is the one identity in the message worth anything -- and every
	// claim the file makes about who is speaking is checked against it.
	sender := strings.ToLower(strings.TrimSpace(mail.From))
	if !strings.Contains(sender, "@") {
		return ignored("the message does not say who it is from")
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

// recipientsRecordedFrom is when this server began recording the address a
// message was delivered to. A row noted before it has none through no fault of
// its own; one noted after it has none only if something went wrong, and is
// refused rather than waved through.
var recipientsRecordedFrom = time.Date(2026, time.September, 13, 0, 0, 0, 0, time.UTC)

// addressedTo is whether an invitation actually asks the person it was sent
// to -- as an attendee, or as the organizer of their own event coming back.
//
// Their own event means their own: the organizer is only believed to be them
// when the message came from them. Taken from the file alone it let a stranger
// past this check entirely, by naming the recipient as the organizer and
// listing no guests at all -- so an event nobody was asked to, apparently
// called by the person it was planted on, landed in their calendar.
//
// Against the address the message was delivered to, which is the one the
// sender wrote. Checking a mailbox's advertised addresses instead was wrong in
// a way that lost mail: a mailbox reached by a catch-all advertises none --
// that alias has no one address to send as, so it is deliberately left out --
// and every invitation to such a mailbox was refused.
//
// The mailbox's own addresses are still accepted beside it, because a message
// may reach a mailbox at one address while the invitation names another of
// theirs.
func (self *Scheduler) addressedTo(ctx context.Context, invitation *models.CalendarInvitation,
	parsed *calendar.Parsed, sender string) (bool, error) {
	theirs := map[string]bool{}
	if delivered := strings.ToLower(strings.TrimSpace(invitation.Recipient)); delivered != "" {
		theirs[delivered] = true
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		mailbox, err := tx.GetMailbox(invitation.MailboxID)
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
		// Nothing to check against: no delivered address recorded, and no
		// address on the mailbox. Only a row noted before this server
		// began recording the recipient looks like this, and those are
		// finite and old -- so they are let through, while anything noted
		// since is refused, because a check that turns itself off is not
		// one. The sweep clears the old rows within two days.
		if invitation.CreatedAt.IsZero() || invitation.CreatedAt.Before(recipientsRecordedFrom) {
			return true, nil
		}
		return false, fmt.Errorf("the address this was delivered to was not recorded")
	}
	// Their own event coming back to them: from themselves, or from
	// somebody at their own domain who says in the file that they posted it
	// for them -- which is the assistant sending their employer's own
	// invitation to the employer, and the same evidence speaksFor accepts
	// for a new event.
	if organizer := strings.TrimSpace(parsed.Organizer); theirs[strings.ToLower(organizer)] &&
		(strings.EqualFold(organizer, sender) || postedFor(organizer, parsed.SentBy, sender)) {
		return true, nil
	}
	for _, attendee := range parsed.Attendees {
		if theirs[strings.ToLower(strings.TrimSpace(attendee.Address))] {
			return true, nil
		}
	}
	return false, nil
}

// mayActFor is whether the sender may act on an event the held copy says
// somebody else called: only that organizer, and nobody else at all.
//
// Four rewrites got here, and each of the first three believed something the
// sender had written. "The sender is the organizer of the arriving file" asks
// whether the sender is who they say they are. Adding "and the file agrees
// with the held copy about whose event it is" asks nothing either: the held
// organizer's address is printed on the invitation every guest received.
// Anchoring SENT-BY to the organizer's own domain looked like the answer,
// because a domain is the one thing DMARC actually proved -- but a domain is
// only an organization where a domain *is* an organization. On the free mail
// providers, where most people have their address, "the same domain" is any
// of a billion strangers: the largest population of organizers anywhere would
// have been protected by nothing.
//
// So a delegate may not move somebody's meeting. An assistant who sends for
// their employer can ask this person to a meeting -- speaksFor still allows
// that, and an invitation is something the person can read and decline -- but
// changing or calling off a meeting already in the calendar takes the
// organizer themselves. The failure is visible: the invitation is kept with
// its reason, and the person's own copy stands rather than being silently
// rewritten by somebody who typed a name into a file.
//
// An event naming no organizer is nobody's to act on from outside: it is an
// appointment its owner made for themselves.
func mayActFor(organizer string, parsed *calendar.Parsed, sender string) bool {
	held := strings.TrimSpace(organizer)
	if held == "" {
		return false
	}
	return strings.EqualFold(held, sender)
}

// speaksFor is whether the sender may send an invitation as this file's
// organizer: they are that organizer, or they are at the organizer's domain
// and the file says they posted it for them.
//
// Only for a file this server holds nothing about yet, where the worst a
// stranger achieves is an invitation the person can decline. Where there is a
// held copy -- somebody's real meeting, which could be moved or called off --
// mayActFor is the question, and it does not take a delegate's word.
func speaksFor(parsed *calendar.Parsed, sender string) bool {
	organizer := strings.TrimSpace(parsed.Organizer)
	if strings.EqualFold(organizer, sender) {
		return true
	}
	return postedFor(organizer, parsed.SentBy, sender)
}

// postedFor is whether the sender may be believed when a file says they put
// it in the post for the organizer.
//
// Two things have to hold. The file has to say so -- SENT-BY naming the
// sender and nobody else -- and the sender has to be at the organizer's own
// domain, which is the only part a forger cannot simply type, because it is
// the domain their message was aligned to. It is weak evidence on a domain
// that belongs to no one organization, which is why nothing that changes a
// meeting already held rests on it.
func postedFor(organizer, sentBy, sender string) bool {
	if sentBy == "" || !strings.EqualFold(strings.TrimSpace(sentBy), sender) {
		return false
	}
	return sameDomain(organizer, sender)
}

// sameDomain is whether two addresses are at the same domain. An address with
// no domain, or either one missing, is never at anybody's.
func sameDomain(first, second string) bool {
	return domainOf(first) != "" && strings.EqualFold(domainOf(first), domainOf(second))
}

// domainOf is what follows the last at sign, which is the domain even when the
// local part contains one of its own.
func domainOf(address string) string {
	at := strings.LastIndex(strings.TrimSpace(address), "@")
	if at < 0 {
		return ""
	}
	return strings.TrimSpace(strings.TrimSpace(address)[at+1:])
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
	asked, err := self.addressedTo(ctx, invitation, parsed, sender)
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
		// Held for the rest of the transaction: everything below decides
		// what to write by looking at this copy, and deciding from a copy
		// somebody else is replacing is how one of the two changes is
		// lost without a word.
		existing, err := tx.LockCalendarObjectByUID(found.ID, parsed.UID)
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
		// A change to one occurrence is only about that occurrence. The
		// organizer who moves next Tuesday's standup sends a file with
		// nothing in it but that Tuesday, and storing it as the whole
		// event replaced the standup with a single appointment -- every
		// other week gone, from the calendar, the phone, free-busy and
		// everything that reads them. It goes beside what is held.
		keeping := parsed
		if parsed.RecurrenceID != "" {
			if existing == nil {
				result.status = models.CalendarInvitationIgnored
				result.because = "a change to one occurrence of a series this calendar does not have"
				return nil
			}
			held, err := calendar.Parse([]byte(existing.Data))
			if err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = "a change to an event this server can no longer read"
				result.objectId = existing.ID
				return nil
			}
			merged, err := calendar.MergeOccurrence(held, parsed)
			if err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = err.Error()
				result.objectId = existing.ID
				return nil
			}
			keeping, err = calendar.Parse(merged)
			if err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = err.Error()
				result.objectId = existing.ID
				return nil
			}
		}
		occurrences, indexedUntil, err := indexed(keeping)
		if err != nil {
			return err
		}
		object := &models.CalendarObject{
			CalendarID: found.ID, UID: keeping.UID, ETag: calendar.ETag(keeping.Data),
			Data: string(keeping.Data), Summary: keeping.Summary, Location: keeping.Location,
			StartsAt: keeping.StartsAt, EndsAt: keeping.EndsAt,
			AllDay: keeping.AllDay, Recurring: keeping.Recurring, Status: keeping.Status,
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
		// Held for the rest of the transaction: everything below decides
		// what to write by looking at this copy, and deciding from a copy
		// somebody else is replacing is how one of the two changes is
		// lost without a word.
		existing, err := tx.LockCalendarObjectByUID(found.ID, parsed.UID)
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
		// One occurrence called off is one occurrence. Struck through the
		// series instead, cancelling next Tuesday's standup struck out
		// every standup there will ever be -- shown as cancelled on every
		// device and counted as free for the rest of the year.
		var cancelled *calendar.Parsed
		if parsed.RecurrenceID != "" {
			held, err := calendar.Parse([]byte(existing.Data))
			if err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = "a cancellation of an event this server can no longer read"
				result.objectId = existing.ID
				return nil
			}
			written, err := calendar.ExcludeOccurrence(held, parsed.Occurrence())
			if err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = err.Error()
				result.objectId = existing.ID
				return nil
			}
			if cancelled, err = calendar.Parse(written); err != nil {
				result.status = models.CalendarInvitationIgnored
				result.because = err.Error()
				result.objectId = existing.ID
				return nil
			}
		} else {
			// Marked cancelled rather than deleted. The person is told
			// the meeting is off, which is the useful thing; making it
			// vanish leaves them wondering whether they imagined it.
			var err error
			if cancelled, err = calendar.Build([]byte(existing.Data), &calendar.Fields{
				Status: statusText("CANCELLED"),
			}); err != nil {
				return err
			}
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

// answeredAlready is whether this answer has been overtaken by one already
// recorded, and why.
//
// By the sequence the answer is about first -- an answer to the meeting as it
// was two changes ago says nothing about the meeting as it is -- and then by
// when the answering program wrote it, which is what tells two answers to the
// same version apart. Neither is the sender's to forge usefully: a later
// stamp is what a later answer has anyway, and the worst an attacker does
// with a replayed message is have it ignored.
func answeredAlready(existing *models.CalendarObject, parsed *calendar.Parsed, sender string) (bool, string) {
	held, err := calendar.Parse([]byte(existing.Data))
	if err != nil {
		return false, ""
	}
	if parsed.Sequence < held.Sequence {
		return true, "an answer to an older version of this event"
	}
	when := parsed.Stamp
	for _, attendee := range held.Attendees {
		if !strings.EqualFold(strings.TrimSpace(attendee.Address), sender) {
			continue
		}
		if attendee.AnsweredAt.IsZero() || when.IsZero() {
			return false, ""
		}
		if when.Before(attendee.AnsweredAt) {
			return true, "an answer older than the one already recorded"
		}
	}
	return false, ""
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
		// Held for the rest of the transaction: everything below decides
		// what to write by looking at this copy, and deciding from a copy
		// somebody else is replacing is how one of the two changes is
		// lost without a word.
		existing, err := tx.LockCalendarObjectByUID(found.ID, parsed.UID)
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
				// Stamped with when their program wrote the answer, so
				// the next one to arrive can be told whether it is
				// older than this.
				attendee.AnsweredAt = parsed.Stamp
				theirs = append(theirs, attendee)
			}
		}
		if len(theirs) == 0 {
			result.status = models.CalendarInvitationIgnored
			result.because = "an answer on behalf of somebody else"
			return nil
		}
		// An answer arriving after a later one is an older answer. Mail is
		// not ordered, and a copy of an earlier message replays perfectly
		// well -- its signature is still good -- so without this a
		// "declined" was undone by the "accepted" that came before it,
		// with nothing to show that it had been.
		if stale, reason := answeredAlready(existing, parsed, sender); stale {
			result.status = models.CalendarInvitationIgnored
			result.because = reason
			return nil
		}
		var updated *calendar.Parsed
		if parsed.RecurrenceID != "" {
			// An answer about one occurrence belongs to that occurrence.
			// Written onto the series it said the person had answered for
			// every week of it.
			updated, err = calendar.AnswerOccurrence([]byte(existing.Data), parsed.RecurrenceID, theirs)
		} else {
			updated, err = calendar.Answer([]byte(existing.Data), theirs)
		}
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
