package apigraph

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// The calendar: a person's own events, which they edit here and their phone
// keeps in step with over CalDAV.

// CalendarQuery reads a person's calendar.
type CalendarQuery interface {
	// The caller's calendars. An account that has never had one is given
	// one here rather than being asked to make it, so that nothing has to
	// be set up before an appointment can be kept. Needs calendar:use.
	ListCalendars(ctx context.Context) ([]*CalendarView, error)

	// What is on between two moments, one entry per time something
	// happens: an event that repeats weekly appears once for each week it
	// falls in. Answered from the index of when things happen rather than
	// by expanding every repeat in the calendar. Needs calendar:use.
	ListCalendarEvents(ctx context.Context, arguments ListCalendarEventsArguments) ([]*CalendarEventView, error)

	// One event, with its whole file. Needs calendar:use.
	GetCalendarEvent(ctx context.Context, arguments CalendarEventArguments) (*CalendarEventView, error)
}

// CalendarMutation changes it.
type CalendarMutation interface {
	// Keep an event, or change one that is kept. Give either a whole
	// iCalendar file, which is what a program that speaks the format
	// sends, or the filled-in fields, which is what the dashboard sends;
	// with the fields, anything already on the event that they do not
	// cover is kept, so that correcting a title here does not throw away
	// the alarm a phone put there. Needs calendar:use.
	SaveCalendarEvent(ctx context.Context, arguments SaveCalendarEventArguments) (*CalendarEventView, error)

	// Take one away. Needs calendar:use.
	DeleteCalendarEvent(ctx context.Context, arguments CalendarEventArguments) (bool, error)

	// Rename a calendar, or change how it is shown. Needs calendar:use.
	SaveCalendar(ctx context.Context, arguments SaveCalendarArguments) (*CalendarView, error)
}

// CalendarView is one calendar and how much is in it.
type CalendarView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Colour      string `json:"colour,omitempty"`
	Timezone    string `json:"timezone,omitempty"`

	// WeekStart is the day this person's weeks are drawn from: "sunday" or
	// "monday".
	WeekStart string `json:"weekStart,omitempty"`
	Events    int    `json:"events"`
}

// CalendarEventView is one event, and when it happens.
//
// StartsAt and EndsAt are the occurrence being shown rather than the event's
// own first time, so that a weekly meeting listed for November says November.
// File is the whole of it, and is left out of a listing, because a page of
// whole files is a great deal of text nobody reads.
type CalendarEventView struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`
	UID        string `json:"uid"`
	ETag       string `json:"etag"`

	Summary     string `json:"summary,omitempty"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`

	StartsAt string `json:"startsAt"`
	EndsAt   string `json:"endsAt"`
	AllDay   bool   `json:"allDay"`

	// Recurring says the event repeats, and Recurrence is the rule itself
	// for a form that shows it. Occurrence marks an entry that is one time
	// a repeating event happens rather than the event itself, so that an
	// editor can say "this changes every one of them".
	Recurring  bool   `json:"recurring"`
	Recurrence string `json:"recurrence,omitempty"`
	Occurrence bool   `json:"occurrence"`

	Status   string `json:"status,omitempty"`
	Timezone string `json:"timezone,omitempty"`

	Organizer string              `json:"organizer,omitempty"`
	Attendees []*CalendarAttendee `json:"attendees"`

	File string `json:"file,omitempty"`
}

// CalendarAttendee is one person asked to an event.
type CalendarAttendee struct {
	Address       string `json:"address"`
	Name          string `json:"name,omitempty"`
	Participation string `json:"participation,omitempty"`
	Role          string `json:"role,omitempty"`
}

type ListCalendarEventsArguments struct {
	CalendarID string `json:"calendarId"`

	// The window, as RFC 3339 moments. A month view asks for a month.
	From  string `json:"from"`
	Until string `json:"until"`
}

type CalendarEventArguments struct {
	CalendarID string `json:"calendarId"`
	ID         string `json:"id"`
}

type SaveCalendarEventArguments struct {
	CalendarID string `json:"calendarId"`

	// ID names an event already kept; empty keeps a new one.
	ID string `json:"id" graphapi:"nullable"`

	// File is a whole iCalendar file, which is what a program that speaks
	// the format sends. When it is given the fields below are ignored.
	File string `json:"file" graphapi:"nullable"`

	// Pointers, because leaving a field out and emptying it are different
	// instructions: a caller that sends only a title must not thereby
	// delete the location, and a form whose box is empty must be able to
	// clear what was there.
	Summary     *string `json:"summary" graphapi:"nullable"`
	Location    *string `json:"location" graphapi:"nullable"`
	Description *string `json:"description" graphapi:"nullable"`

	// RFC 3339 moments. With AllDay, only the day part is read.
	StartsAt *string `json:"startsAt" graphapi:"nullable"`
	EndsAt   *string `json:"endsAt" graphapi:"nullable"`
	AllDay   *bool   `json:"allDay" graphapi:"nullable"`

	// The zone the times are wall-clock times in, as an IANA name. Empty
	// writes them in UTC.
	Timezone string `json:"timezone" graphapi:"nullable"`

	// The repeat, as the text of a rule without its property name:
	// "FREQ=WEEKLY;BYDAY=MO;COUNT=10". Empty stops it repeating.
	Recurrence *string `json:"recurrence" graphapi:"nullable"`

	Status *string `json:"status" graphapi:"nullable"`

	// Attendees are the people to ask, as addresses. Setting them makes
	// the event a meeting and sends each of them an invitation; a list
	// given empty takes everybody off it, which is not the same as calling
	// the meeting off.
	Attendees *[]string `json:"attendees" graphapi:"nullable"`
}

type SaveCalendarArguments struct {
	ID          string `json:"id"`
	Name        string `json:"name" graphapi:"nullable"`
	Description string `json:"description" graphapi:"nullable"`
	Colour      string `json:"colour" graphapi:"nullable"`
	Timezone    string `json:"timezone" graphapi:"nullable"`

	// WeekStart is "sunday" or "monday", and nothing else.
	WeekStart string `json:"weekStart" graphapi:"nullable"`
}

// weekStartOf is the day a calendar's weeks are drawn from, and Sunday for a
// calendar made before this was written down. Answered here rather than left
// empty so that nothing drawing a week has to invent a default of its own.
func weekStartOf(found *models.Calendar) string {
	if known := models.KnownWeekStart(found.WeekStart); known != "" {
		return known
	}
	return models.WeekStartsSunday
}

// longestWindow is how much of a calendar may be asked for at once.
//
// A window is chosen by whoever is asking, and every occurrence in it is
// written out, so without a ceiling one request can ask this server to
// describe a century. Five years covers a year view and any amount of paging
// about, and is far more than any page here draws.
const longestWindow = 5 * 366 * 24 * time.Hour

// requireCalendarPerson is the caller, if they may keep a calendar.
func (self *graph) requireCalendarPerson(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requirePermission(ctx, models.PermissionCalendarUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// requireOwnCalendar is one calendar, if it is the caller's. A calendar
// belonging to somebody else is not found, rather than refused, because whose
// it is is not the caller's business.
func (self *graph) requireOwnCalendar(ctx context.Context, calendarId string) (*models.Calendar, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	found, err := self.transaction(ctx).GetCalendar(strings.TrimSpace(calendarId))
	if err != nil {
		return nil, err
	}
	if found == nil || found.UserID != principal.User.ID {
		return nil, api.ErrNotFound
	}
	return found, nil
}

func (self *graph) ListCalendars(ctx context.Context) ([]*CalendarView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	var calendars []*models.Calendar
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListCalendars(principal.User.ID)
		if err != nil {
			return err
		}
		// Nobody should have to make a calendar before they can put
		// something in it, and the CalDAV layout needs one to exist
		// before a phone can be pointed at it.
		if len(found) == 0 {
			made, err := tx.CreateCalendar(&models.Calendar{
				UserID: principal.User.ID, Name: "Calendar", Timezone: self.zoneFor(principal),
			})
			if err != nil {
				return err
			}
			found = []*models.Calendar{made}
		}
		calendars = found
		return nil
	}); err != nil {
		return nil, err
	}
	views := make([]*CalendarView, 0, len(calendars))
	for _, found := range calendars {
		count, err := self.transaction(ctx).CountCalendarObjects(found.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, &CalendarView{
			ID: found.ID, Name: found.Name, Description: found.Description,
			Colour: found.Colour, Timezone: found.Timezone,
			WeekStart: weekStartOf(found), Events: int(count),
		})
	}
	return views, nil
}

// zoneFor is the zone a new calendar is written in: the person's own, if the
// account records one, and otherwise none, which means times are written in
// UTC. Not the server's zone, which is a fact about a machine rather than
// about anybody's day.
func (self *graph) zoneFor(principal *api.Principal) string {
	if principal == nil || principal.User == nil {
		return ""
	}
	return strings.TrimSpace(principal.User.Timezone)
}

func (self *graph) ListCalendarEvents(ctx context.Context, arguments ListCalendarEventsArguments) ([]*CalendarEventView, error) {
	found, err := self.requireOwnCalendar(ctx, arguments.CalendarID)
	if err != nil {
		return nil, err
	}
	from, until, err := window(arguments.From, arguments.Until)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	occurrences, err := tx.ListOccurrences(found.ID, from, until)
	if err != nil {
		return nil, translateError(err)
	}
	// Read each event once, however many times it happens in the window.
	objects := make(map[string]*models.CalendarObject, len(occurrences))
	views := make([]*CalendarEventView, 0, len(occurrences))
	for _, occurrence := range occurrences {
		object, already := objects[occurrence.ObjectID]
		if !already {
			if object, err = tx.GetCalendarObject(found.ID, occurrence.ObjectID); err != nil {
				return nil, err
			}
			objects[occurrence.ObjectID] = object
		}
		if object == nil {
			// The index is derived, so a row with no event behind it is
			// this server having got something wrong rather than
			// anything the caller did. Left out rather than served as a
			// gap nobody can explain.
			continue
		}
		view, err := eventView(object, false)
		if err != nil {
			return nil, err
		}
		view.StartsAt = occurrence.StartsAt.UTC().Format(time.RFC3339)
		view.EndsAt = occurrence.EndsAt.UTC().Format(time.RFC3339)
		view.AllDay = occurrence.AllDay
		// One time a repeating event happens is not the event, and an
		// editor has to say so before it changes every one of them.
		view.Occurrence = object.Recurring && !occurrence.StartsAt.Equal(object.StartsAt)
		views = append(views, view)
	}
	return views, nil
}

// window reads the two moments a caller asked about, and refuses a span this
// server will not write out in one answer.
func window(from, until string) (time.Time, time.Time, error) {
	opened, err := time.Parse(time.RFC3339, strings.TrimSpace(from))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: the start of the window is not a moment: %s",
			api.ErrInvalidArguments, err)
	}
	closed, err := time.Parse(time.RFC3339, strings.TrimSpace(until))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: the end of the window is not a moment: %s",
			api.ErrInvalidArguments, err)
	}
	if !closed.After(opened) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: the window ends before it begins",
			api.ErrInvalidArguments)
	}
	if closed.Sub(opened) > longestWindow {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: that is a longer stretch of calendar than this server answers for at once",
			api.ErrInvalidArguments)
	}
	return opened, closed, nil
}

func (self *graph) GetCalendarEvent(ctx context.Context, arguments CalendarEventArguments) (*CalendarEventView, error) {
	found, err := self.requireOwnCalendar(ctx, arguments.CalendarID)
	if err != nil {
		return nil, err
	}
	object, err := self.transaction(ctx).GetCalendarObject(found.ID, strings.TrimSpace(arguments.ID))
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, api.ErrNotFound
	}
	return eventView(object, true)
}

// eventView is one stored event as the dashboard reads it. Whole says whether
// to carry the file itself, which a listing leaves out.
func eventView(object *models.CalendarObject, whole bool) (*CalendarEventView, error) {
	view := &CalendarEventView{
		ID: object.ID, CalendarID: object.CalendarID, UID: object.UID, ETag: object.ETag,
		Summary: object.Summary, Location: object.Location,
		StartsAt: object.StartsAt.UTC().Format(time.RFC3339),
		EndsAt:   object.EndsAt.UTC().Format(time.RFC3339),
		AllDay:   object.AllDay, Recurring: object.Recurring, Status: object.Status,
		Attendees: []*CalendarAttendee{},
	}
	// The details a form needs come out of the file, which is the only
	// place they are: the columns beside it exist to answer a listing
	// quickly, not to be the event.
	parsed, err := calendar.Parse([]byte(object.Data))
	if err != nil {
		// Kept text that cannot be read is this server's problem, not the
		// caller's, and what is known about it is still worth showing.
		return view, nil
	}
	view.Description = parsed.Description
	view.Recurrence = parsed.Recurrence
	view.Timezone = parsed.Timezone
	view.Organizer = parsed.Organizer
	for _, attendee := range parsed.Attendees {
		view.Attendees = append(view.Attendees, &CalendarAttendee{
			Address: attendee.Address, Name: attendee.Name,
			Participation: attendee.Participation, Role: attendee.Role,
		})
	}
	if whole {
		view.File = string(parsed.Data)
	}
	return view, nil
}

func (self *graph) SaveCalendarEvent(ctx context.Context, arguments SaveCalendarEventArguments) (*CalendarEventView, error) {
	found, err := self.requireOwnCalendar(ctx, arguments.CalendarID)
	if err != nil {
		return nil, err
	}
	// Who would be asking, if anybody is asked. Looked up before the write
	// so that the event carries it from the first version: an invitation
	// with no organizer is one nobody can answer.
	organizer, err := self.organizerFor(ctx)
	if err != nil {
		return nil, err
	}

	// What the guest list was before, so that afterwards it is possible to
	// tell who is newly asked from who was already coming.
	var before *models.CalendarObject
	if named := strings.TrimSpace(arguments.ID); named != "" {
		if before, err = self.transaction(ctx).GetCalendarObject(found.ID, named); err != nil {
			return nil, err
		}
	}

	var kept *models.CalendarObject
	var refused error
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Read inside the transaction that writes, and held. The form
		// sends the boxes it showed and the server merges them onto the
		// file it holds; reading that outside the write -- or inside it
		// without the lock, which is the same thing under this database's
		// ordinary isolation -- would let a phone's change arriving in
		// between be merged away without a word.
		var existing *models.CalendarObject
		if named := strings.TrimSpace(arguments.ID); named != "" {
			if existing, err = tx.LockCalendarObject(found.ID, named); err != nil {
				return err
			}
			if existing == nil {
				return api.ErrNotFound
			}
		}
		parsed, err := buildSaved(&arguments, existing, organizer)
		if err != nil {
			refused = fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
			return refused
		}
		object := &models.CalendarObject{
			CalendarID: found.ID, UID: parsed.UID, ETag: calendar.ETag(parsed.Data),
			Data: string(parsed.Data), Summary: parsed.Summary, Location: parsed.Location,
			StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt,
			AllDay: parsed.AllDay, Recurring: parsed.Recurring, Status: parsed.Status,
		}
		if existing != nil {
			object.ID = existing.ID
			object.CreatedAt = existing.CreatedAt
		}
		// The same ceiling the CalDAV side enforces. A listing is the
		// whole calendar in one answer, and a client reads an event
		// missing from it as deleted, so a calendar that grew past what
		// can be listed would tell a phone to forget what it could not
		// see.
		if existing == nil {
			held, err := tx.CountCalendarObjects(found.ID)
			if err != nil {
				return err
			}
			if held >= db.ObjectsPerCalendar {
				refused = fmt.Errorf("%w: this calendar already holds %d events, which is as many as this server keeps",
					api.ErrInvalidArguments, db.ObjectsPerCalendar)
				return refused
			}
		}
		// The file's own identifier decides which event this is. Two
		// devices adding an appointment at the same time pick different
		// file names but agree on the identifier, and the second must
		// land on the first rather than making a second copy of it.
		//
		// When an event is already named, a file carrying somebody else's
		// identifier is refused instead. Leaving that to the unique index
		// gave whoever asked a 500 carrying the index's name.
		twin, err := tx.GetCalendarObjectByUID(found.ID, object.UID)
		if err != nil {
			return err
		}
		if twin != nil {
			if object.ID == "" {
				object.ID = twin.ID
				object.CreatedAt = twin.CreatedAt
			} else if twin.ID != object.ID {
				refused = fmt.Errorf("%w: another event in this calendar already has that identifier",
					api.ErrInvalidArguments)
				return refused
			}
		}
		occurrences, indexedUntil, err := occurrencesOf(parsed)
		if err != nil {
			refused = fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
			return refused
		}
		object.IndexedUntil = &indexedUntil
		indexedAt := time.Now().UTC()
		object.IndexedAt = &indexedAt
		kept, err = tx.PutCalendarObject(object, occurrences)
		return err
	}); err != nil {
		if refused != nil {
			return nil, refused
		}
		return nil, translateError(err)
	}

	// Sent after the write, never before. An invitation that goes out and
	// is then not saved is a meeting everybody has been asked to that the
	// person who called it cannot see.
	if err := self.inviteTo(ctx, kept, before, organizer); err != nil {
		return nil, fmt.Errorf("the event is saved, but the invitations could not be sent: %w", err)
	}
	return eventView(kept, true)
}

// organizerFor is the address this person would organize a meeting from.
//
// One of their own mailbox's addresses, because the answers come back as
// mail: an organizer at an address this server does not receive is a meeting
// whose replies go nowhere. Empty when they have no mailbox, which makes an
// event with guests refuse rather than send invitations nobody can answer.
func (self *graph) organizerFor(ctx context.Context) (string, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return "", err
	}
	mailboxes, err := self.transaction(ctx).ListMailboxes(principal.User.ID)
	if err != nil {
		return "", err
	}
	for _, mailbox := range mailboxes {
		for _, address := range mailbox.Addresses {
			if trimmed := strings.TrimSpace(address.Address); trimmed != "" {
				return trimmed, nil
			}
		}
	}
	return "", nil
}

// inviteTo asks the people an event names.
//
// Only the ones who are not already coming: everybody on the list when the
// event has really changed, and only the newly added ones when it has not.
// Sending to everybody on every save would mean correcting a typo in the
// notes putting an invitation in five people's mailboxes.
func (self *graph) inviteTo(ctx context.Context, kept, before *models.CalendarObject, organizer string) error {
	if kept == nil || organizer == "" {
		return nil
	}
	parsed, err := calendar.Parse([]byte(kept.Data))
	if err != nil || len(parsed.Attendees) == 0 {
		return nil
	}
	var held *calendar.Parsed
	if before != nil {
		if decoded, err := calendar.Parse([]byte(before.Data)); err == nil {
			held = decoded
		}
	}
	asked, err := guestsToInvite(parsed, held, organizer)
	if err != nil {
		return err
	}
	if len(asked) == 0 {
		return nil
	}
	written, err := calendar.Invite([]byte(kept.Data), organizer)
	if err != nil {
		return err
	}
	return self.sendCalendarMessage(ctx, organizer, asked, kept, written, "REQUEST")
}

// guestsToInvite is who an invitation goes to when this person saves this
// event, and nobody at all when it is not theirs to send.
//
// Only for an event this person called. The cancellation has always checked
// that and the invitation did not, which is the wrong way round: an invitation
// that arrived by mail brings its guest list with it, and saving somebody
// else's event -- correcting its title, moving it in one's own copy -- then
// sent an invitation to every one of them, from this person's address and
// signed by their domain, carrying whatever text the sender had written. An
// event naming no organizer is this person's own, made here, and inviting
// people to it is the whole point.
//
// And a guest list has an end. Nothing else bounded this one: the setting that
// limits recipients is about relayed mail and this path does not go through
// it, so a file carrying twenty thousand attendee lines was twenty thousand
// messages waiting for somebody to press save.
func guestsToInvite(parsed, before *calendar.Parsed, organizer string) ([]string, error) {
	if parsed == nil || organizer == "" {
		return nil, nil
	}
	if held := strings.TrimSpace(parsed.Organizer); held != "" && !strings.EqualFold(held, organizer) {
		return nil, nil
	}
	if len(parsed.Attendees) > calendar.MaximumGuests {
		return nil, fmt.Errorf("%w: this event asks more than %d people, which is more than this server invites at once",
			api.ErrInvalidArguments, calendar.MaximumGuests)
	}
	// Only the ones who are not already coming: everybody on the list when
	// the event has really changed, and only the newly added ones when it
	// has not. Sending to everybody on every save would mean correcting a
	// typo in the notes putting an invitation in five people's mailboxes.
	changed := true
	already := map[string]bool{}
	if before != nil {
		changed = before.Sequence != parsed.Sequence
		for _, attendee := range before.Attendees {
			already[strings.ToLower(attendee.Address)] = true
		}
	}
	asked := make([]string, 0, len(parsed.Attendees))
	for _, attendee := range parsed.Attendees {
		if strings.EqualFold(attendee.Address, organizer) {
			// Not to themselves. Their own calendar already has it, and
			// a mail program shown an invitation it is the organizer of
			// offers to accept on their behalf.
			continue
		}
		if !changed && already[strings.ToLower(attendee.Address)] {
			continue
		}
		asked = append(asked, attendee.Address)
	}
	return asked, nil
}

func occurrencesOf(parsed *calendar.Parsed) ([]models.Occurrence, time.Time, error) {
	// The horizon lives in the calendar package, because CalDAV indexes
	// what it writes too and two doors that disagreed about how far ahead
	// to look would give a calendar whose contents depended on which one
	// last touched it.
	occurrences, indexedUntil, err := calendar.Indexed(parsed)
	if err != nil {
		return nil, time.Time{}, err
	}
	rows := make([]models.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		rows = append(rows, models.Occurrence{
			StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay,
		})
	}
	return rows, indexedUntil, nil
}

// buildSaved turns what was sent into a file: whole iCalendar text when a
// program sent one, otherwise the filled-in fields applied to whatever is
// already kept.
func buildSaved(arguments *SaveCalendarEventArguments, existing *models.CalendarObject, organizer string) (*calendar.Parsed, error) {
	if strings.TrimSpace(arguments.File) != "" {
		return calendar.Parse([]byte(arguments.File))
	}
	var previous []byte
	if existing != nil {
		previous = []byte(existing.Data)
	}
	fields := &calendar.Fields{
		Summary: arguments.Summary, Location: arguments.Location,
		Description: arguments.Description, AllDay: arguments.AllDay,
		Timezone: strings.TrimSpace(arguments.Timezone), Recurrence: arguments.Recurrence,
		Status: arguments.Status, Organizer: organizer,
	}
	if arguments.Attendees != nil {
		asked := make([]calendar.Attendee, 0, len(*arguments.Attendees))
		for _, address := range *arguments.Attendees {
			trimmed := strings.TrimSpace(address)
			if trimmed == "" {
				continue
			}
			asked = append(asked, calendar.Attendee{Address: trimmed})
		}
		fields.Attendees = &asked
	}
	var err error
	if fields.StartsAt, err = moment(arguments.StartsAt, "start"); err != nil {
		return nil, err
	}
	if fields.EndsAt, err = moment(arguments.EndsAt, "end"); err != nil {
		return nil, err
	}
	return calendar.Build(previous, fields)
}

// moment reads one of the times a form sent.
func moment(value *string, which string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	at, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, fmt.Errorf("the %s is not a moment: %s", which, err)
	}
	return &at, nil
}

func (self *graph) DeleteCalendarEvent(ctx context.Context, arguments CalendarEventArguments) (bool, error) {
	found, err := self.requireOwnCalendar(ctx, arguments.CalendarID)
	if err != nil {
		return false, err
	}
	object, err := self.transaction(ctx).GetCalendarObject(found.ID, strings.TrimSpace(arguments.ID))
	if err != nil {
		return false, err
	}
	if object == nil {
		return false, api.ErrNotFound
	}
	// Told before it goes, not after. Once it is deleted there is nothing
	// left to write the cancellation from, and the people who were coming
	// would simply never hear.
	organizer, err := self.organizerFor(ctx)
	if err != nil {
		return false, err
	}
	if err := self.callOff(ctx, object, organizer); err != nil {
		return false, fmt.Errorf("the event is still here: the people coming could not be told: %w", err)
	}
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DeleteCalendarObject(found.ID, object.ID)
	}); err != nil {
		return false, err
	}
	return true, nil
}

// callOff tells the people coming that a meeting is off.
//
// Only when this person is the one who called it. Deleting an event somebody
// else organized is leaving their meeting, not cancelling it, and sending a
// cancellation would take it out of everybody else's calendar too.
func (self *graph) callOff(ctx context.Context, object *models.CalendarObject, organizer string) error {
	if object == nil || organizer == "" {
		return nil
	}
	parsed, err := calendar.Parse([]byte(object.Data))
	if err != nil || len(parsed.Attendees) == 0 {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(parsed.Organizer), organizer) {
		return nil
	}
	// The same ceiling as inviting: an event whose guest list came from
	// somewhere else is not a mail run waiting for somebody to press
	// delete either.
	if len(parsed.Attendees) > calendar.MaximumGuests {
		return fmt.Errorf("%w: this event asks more than %d people, which is more than this server will write to at once",
			api.ErrInvalidArguments, calendar.MaximumGuests)
	}
	asked := make([]string, 0, len(parsed.Attendees))
	for _, attendee := range parsed.Attendees {
		if strings.EqualFold(attendee.Address, organizer) {
			continue
		}
		asked = append(asked, attendee.Address)
	}
	if len(asked) == 0 {
		return nil
	}
	written, err := calendar.CallOff([]byte(object.Data), organizer)
	if err != nil {
		return err
	}
	return self.sendCalendarMessage(ctx, organizer, asked, object, written, "CANCEL")
}

func (self *graph) SaveCalendar(ctx context.Context, arguments SaveCalendarArguments) (*CalendarView, error) {
	found, err := self.requireOwnCalendar(ctx, arguments.ID)
	if err != nil {
		return nil, err
	}
	if zone := strings.TrimSpace(arguments.Timezone); zone != "" {
		if _, err := time.LoadLocation(zone); err != nil {
			return nil, fmt.Errorf("%w: this server does not know the time zone %q",
				api.ErrInvalidArguments, zone)
		}
	}
	// Refused rather than quietly turned into Sunday: a person who typed
	// something else meant something, and being told is how they find out
	// there are two answers here and not seven.
	if given := strings.TrimSpace(arguments.WeekStart); given != "" && models.KnownWeekStart(given) == "" {
		return nil, fmt.Errorf("%w: a week starts on sunday or on monday", api.ErrInvalidArguments)
	}
	var kept *models.Calendar
	var count int64
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if kept, err = tx.UpdateCalendar(&models.Calendar{
			ID: found.ID, Name: arguments.Name, Description: arguments.Description,
			Colour: arguments.Colour, Timezone: arguments.Timezone,
			WeekStart: arguments.WeekStart,
		}); err != nil {
			return err
		}
		if kept == nil {
			return api.ErrNotFound
		}
		count, err = tx.CountCalendarObjects(found.ID)
		return err
	}); err != nil {
		return nil, translateError(err)
	}
	return &CalendarView{
		ID: kept.ID, Name: kept.Name, Description: kept.Description,
		Colour: kept.Colour, Timezone: kept.Timezone, WeekStart: weekStartOf(kept),
		Events: int(count),
	}, nil
}

// sendCalendarMessage puts an invitation or a cancellation in the post.
//
// Sending on somebody's behalf is the riskiest thing this feature does, so it
// is narrow on purpose: only to the addresses the event itself names, only
// from an address of the person's own mailbox, and never to an address that
// says in so many words that it does not take mail.
func (self *graph) sendCalendarMessage(ctx context.Context, organizer string, asked []string,
	object *models.CalendarObject, written []byte, method string) error {
	if self.mailer == nil {
		return fmt.Errorf("this server cannot send mail")
	}
	to := make([]string, 0, len(asked))
	for _, address := range asked {
		if noReplyAddress(address) {
			// An address that says it does not take mail. Inviting it is
			// a message that bounces, or worse, does not.
			continue
		}
		to = append(to, address)
	}
	if len(to) == 0 {
		return nil
	}
	summary := strings.TrimSpace(object.Summary)
	if summary == "" {
		summary = "an appointment"
	}
	subject := "Invitation: " + summary
	body := "You have been invited to " + summary + ".\r\n"
	if method == "CANCEL" {
		subject = "Cancelled: " + summary
		body = summary + " has been called off.\r\n"
	}
	if !object.StartsAt.IsZero() {
		body += "\r\n" + object.StartsAt.UTC().Format("Monday, 2 January 2006 at 15:04 MST") + "\r\n"
	}
	if strings.TrimSpace(object.Location) != "" {
		body += object.Location + "\r\n"
	}
	message := &mailer.Message{
		From:    organizer,
		To:      to,
		Subject: subject,
		Text:    body,
		Attachments: []*mailparse.Attachment{{
			Filename: "invite.ics",
			// The method belongs in the content type: it is how a mail
			// program knows to show this as something to answer rather
			// than as a file to save.
			ContentType: "text/calendar; method=" + method + "; charset=utf-8",
			Content:     written,
		}},
	}
	envelope := &mailparse.Envelope{}
	if request := api.ContextRequest(ctx); request != nil {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			host = request.RemoteAddr
		}
		envelope.IP = net.ParseIP(host)
		envelope.Location = self.locator.Locate(envelope.IP)
		envelope.TLS = request.TLS
	}
	return self.mailer.Send(ctx, envelope, message)
}

// noReplyAddress is an address that says it does not take mail. The same
// shapes the out-of-office reply refuses, and for the same reason: writing to
// one is a message nobody reads and often one that bounces.
func noReplyAddress(address string) bool {
	local := strings.ToLower(strings.TrimSpace(address))
	if at := strings.Index(local, "@"); at >= 0 {
		local = local[:at]
	}
	local = strings.NewReplacer("-", "", "_", "", ".", "").Replace(local)
	switch local {
	case "noreply", "donotreply", "nobody", "mailerdaemon", "postmaster":
		return true
	}
	return false
}
