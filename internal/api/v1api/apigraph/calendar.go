package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
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
	Events      int    `json:"events"`
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
}

type SaveCalendarArguments struct {
	ID          string `json:"id"`
	Name        string `json:"name" graphapi:"nullable"`
	Description string `json:"description" graphapi:"nullable"`
	Colour      string `json:"colour" graphapi:"nullable"`
	Timezone    string `json:"timezone" graphapi:"nullable"`
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
			Colour: found.Colour, Timezone: found.Timezone, Events: int(count),
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
		return nil, err
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
	var kept *models.CalendarObject
	var refused error
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Read inside the transaction that writes. The form sends the
		// boxes it showed and the server merges them onto the file it
		// holds; reading that outside the write would let a phone's
		// change arriving in between be merged away without a word.
		var existing *models.CalendarObject
		if named := strings.TrimSpace(arguments.ID); named != "" {
			if existing, err = tx.GetCalendarObject(found.ID, named); err != nil {
				return err
			}
			if existing == nil {
				return api.ErrNotFound
			}
		}
		parsed, err := buildSaved(&arguments, existing)
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
		occurrences, err := occurrencesOf(parsed)
		if err != nil {
			refused = fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
			return refused
		}
		kept, err = tx.PutCalendarObject(object, occurrences)
		return err
	}); err != nil {
		if refused != nil {
			return nil, refused
		}
		return nil, translateError(err)
	}
	return eventView(kept, true)
}

// indexedFor is how far ahead the times an event happens are worked out and
// written down.
//
// An event that repeats with no end cannot be indexed for ever, so the index
// reaches a horizon and no further. Two years covers every view the dashboard
// draws and any reasonable amount of paging; past it a calendar answers from
// the file itself. It is measured from now rather than from the event so that
// a repeat set up years ago is still indexed over the part of it anybody is
// going to look at.
const indexedFor = 2 * 366 * 24 * time.Hour

// indexedFrom is how far back, for the same reason in the other direction.
const indexedFrom = 366 * 24 * time.Hour

func occurrencesOf(parsed *calendar.Parsed) ([]models.Occurrence, error) {
	now := time.Now().UTC()
	from, until := now.Add(-indexedFrom), now.Add(indexedFor)
	// An event that happens once, outside the horizon, is still indexed:
	// somebody who puts a date in for their child's graduation should see
	// it when they get there, and there is only one row to write.
	if !parsed.Recurring {
		from, until = parsed.StartsAt.Add(-time.Second), parsed.EndsAt.Add(time.Second)
	} else if parsed.StartsAt.After(from) {
		from = parsed.StartsAt.Add(-time.Second)
	}
	occurrences, err := calendar.Occurrences(parsed, from, until)
	if err != nil {
		return nil, err
	}
	rows := make([]models.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		rows = append(rows, models.Occurrence{
			StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay,
		})
	}
	return rows, nil
}

// buildSaved turns what was sent into a file: whole iCalendar text when a
// program sent one, otherwise the filled-in fields applied to whatever is
// already kept.
func buildSaved(arguments *SaveCalendarEventArguments, existing *models.CalendarObject) (*calendar.Parsed, error) {
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
		Status: arguments.Status,
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
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DeleteCalendarObject(found.ID, object.ID)
	}); err != nil {
		return false, err
	}
	return true, nil
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
	var kept *models.Calendar
	var count int64
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if kept, err = tx.UpdateCalendar(&models.Calendar{
			ID: found.ID, Name: arguments.Name, Description: arguments.Description,
			Colour: arguments.Colour, Timezone: arguments.Timezone,
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
		Colour: kept.Colour, Timezone: kept.Timezone, Events: int(count),
	}, nil
}
