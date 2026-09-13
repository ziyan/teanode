package dav

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// maximumEventName is how long the last segment of an event's URL may be,
// which is the width of the column it becomes.
const maximumEventName = 255

// calendarBackend is the calendar as CalDAV sees it. Every method takes only
// a context, which is why whose request this is travels in one.
//
// It is a second type rather than more methods on the address book's backend
// because the two protocols each want a method called ListCalendars or
// CreateCalendar of their own shape, and one object cannot answer both.
type calendarBackend struct {
	component *component
	signedIn  *session
}

var _ caldav.Backend = (*calendarBackend)(nil)

func (self *calendarBackend) who(ctx context.Context) *session {
	if signedIn := signedInFrom(ctx); signedIn != nil {
		return signedIn
	}
	return self.signedIn
}

// CurrentUserPrincipal is the person, as a URL.
func (self *calendarBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return principalPath(self.who(ctx).userID), nil
}

// CalendarHomeSetPath is where their calendars live.
func (self *calendarBackend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	return calendarHomeSetPath(self.who(ctx).userID), nil
}

// ListCalendars is every calendar this person keeps. An account that has
// never had one is given one here, because a phone asked to synchronize an
// empty home set has nothing to point at and some clients then stop asking.
func (self *calendarBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	signedIn := self.who(ctx)
	var calendars []*models.Calendar
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListCalendars(signedIn.userID)
		if err != nil {
			return unexpectedCalendar(err)
		}
		if len(found) == 0 {
			made, err := tx.CreateCalendar(&models.Calendar{UserID: signedIn.userID, Name: "Calendar"})
			if err != nil {
				return unexpectedCalendar(err)
			}
			found = []*models.Calendar{made}
		}
		calendars = found
		return nil
	}); err != nil {
		return nil, err
	}
	listed := make([]caldav.Calendar, 0, len(calendars))
	for _, found := range calendars {
		listed = append(listed, self.describe(signedIn, found))
	}
	return listed, nil
}

func (self *calendarBackend) describe(signedIn *session, found *models.Calendar) caldav.Calendar {
	return caldav.Calendar{
		Path:        calendarPath(signedIn.userID, found.ID),
		Name:        found.Name,
		Description: found.Description,
		// So that a client knows not to try to send something enormous,
		// rather than finding out when it is refused.
		MaxResourceSize: calendar.MaximumObject,
		// Events only. A client told this server also keeps to-dos would
		// put one here and find it did not come back, which is worse than
		// being told at the start.
		SupportedComponentSet: []string{ical.CompEvent},
	}
}

func (self *calendarBackend) GetCalendar(ctx context.Context, address string) (*caldav.Calendar, error) {
	signedIn := self.who(ctx)
	found, err := self.calendarAt(ctx, signedIn, address)
	if err != nil {
		return nil, davError(err)
	}
	described := self.describe(signedIn, found)
	return &described, nil
}

// CreateCalendar is refused. A person has one calendar here, made for them; a
// client that offers to add one is offering something this server does not
// do, and saying so plainly is better than half-doing it.
func (self *calendarBackend) CreateCalendar(ctx context.Context, found *caldav.Calendar) error {
	return webdav.NewHTTPError(http.StatusForbidden,
		fmt.Errorf("this server keeps one calendar per person"))
}

// GetCalendarObject is one event.
func (self *calendarBackend) GetCalendarObject(ctx context.Context, address string, request *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	signedIn := self.who(ctx)
	found, objectId, err := self.objectAt(ctx, signedIn, address)
	if err != nil {
		return nil, davError(err)
	}
	var object *models.CalendarObject
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		object, err = tx.GetCalendarObject(found.ID, objectId)
		return unexpectedCalendar(err)
	}); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, davError(refuse(http.StatusNotFound, "no such event"))
	}
	return self.object(signedIn, object)
}

// ListCalendarObjects is every event in a calendar, which is what a client
// asks for with PROPFIND Depth: 1. Without sync-collection this is how a
// device works out what changed: it compares the ETags against what it holds,
// and anything it holds that has stopped being listed has been deleted.
func (self *calendarBackend) ListCalendarObjects(ctx context.Context, address string, request *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	signedIn := self.who(ctx)
	found, err := self.calendarAt(ctx, signedIn, address)
	if err != nil {
		return nil, davError(err)
	}
	var objects []*models.CalendarObject
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		objects, err = tx.ListCalendarObjects(found.ID)
		return unexpectedCalendar(err)
	}); err != nil {
		return nil, err
	}
	listed := make([]caldav.CalendarObject, 0, len(objects))
	for _, object := range objects {
		described, err := self.object(signedIn, object)
		if err != nil {
			return nil, err
		}
		listed = append(listed, *described)
	}
	return listed, nil
}

// QueryCalendarObjects answers a calendar-query REPORT, which is a client
// asking for the events in a window.
func (self *calendarBackend) QueryCalendarObjects(ctx context.Context, address string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	objects, err := self.ListCalendarObjects(ctx, address, nil)
	if err != nil {
		return nil, err
	}
	return caldav.Filter(query, objects)
}

// PutCalendarObject keeps an event a device sent.
func (self *calendarBackend) PutCalendarObject(ctx context.Context, address string, sent *ical.Calendar, options *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	signedIn := self.who(ctx)
	found, objectId, err := self.objectAt(ctx, signedIn, address)
	if err != nil {
		return nil, davError(err)
	}
	// Encoded and read back, rather than taken from the decoded form. What
	// is stored has to be what a fetch returns, and what is stored is the
	// folded, canonical text -- so it is written once here and everything
	// afterwards, the ETag included, is taken over those same bytes.
	written, err := calendar.Encode(sent)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	parsed, err := calendar.Parse(written)
	if err != nil {
		// Something larger than this server keeps is the one case worth
		// its own status: 507 tells a client the request was understood
		// and there is no room, which is what makes it stop resending.
		if errors.Is(err, calendar.ErrTooLarge) {
			return nil, webdav.NewHTTPError(http.StatusInsufficientStorage, err)
		}
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	if len(parsed.UID) > maximumEventName {
		return nil, davError(refuse(http.StatusBadRequest,
			"that event's identifier is longer than the %d characters this server keeps", maximumEventName))
	}

	// Indexed over the same stretch the dashboard indexes, because the
	// horizon is a property of the calendar rather than of the door.
	expanded, indexedUntil, err := calendar.Indexed(parsed)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	indexedAt := time.Now().UTC()
	occurrences := make([]models.Occurrence, 0, len(expanded))
	for _, occurrence := range expanded {
		occurrences = append(occurrences, models.Occurrence{
			StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay,
		})
	}
	kept := &models.CalendarObject{
		ID: objectId, CalendarID: found.ID, UID: parsed.UID,
		ETag: calendar.ETag(parsed.Data), Data: string(parsed.Data),
		Summary: parsed.Summary, Location: parsed.Location,
		StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt,
		AllDay: parsed.AllDay, Recurring: parsed.Recurring, Status: parsed.Status,
		IndexedUntil: &indexedUntil, IndexedAt: &indexedAt,
	}
	var stored *models.CalendarObject
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Held for the rest of the transaction, because the conditional
		// headers below are only as good as the row they were checked
		// against. Read without a lock, two devices holding the same
		// version both asked "is it still this version", were both told
		// yes, and both wrote -- so the check that exists to stop one
		// device overwriting another silently permitted it.
		existing, err := tx.LockCalendarObject(found.ID, objectId)
		if err != nil {
			return unexpectedCalendar(err)
		}
		// The conditional headers, which are how two devices editing the
		// same event at once are stopped from silently overwriting one
		// another.
		if options != nil {
			if options.IfMatch.IsSet() {
				// "Only if it is still the version I read." A client that
				// read an event, thought about it, and is now writing
				// back what it decided.
				var matched bool
				if existing != nil {
					matched, _ = options.IfMatch.MatchETag(existing.ETag)
				}
				if !matched {
					return webdav.NewHTTPError(http.StatusPreconditionFailed,
						fmt.Errorf("that event has changed since you read it"))
				}
			}
			if options.IfNoneMatch.IsSet() && existing != nil {
				// "Only if it is not there yet", or "only if it is not
				// this particular version". A client creating an event,
				// which must not quietly become an overwrite.
				if options.IfNoneMatch.IsWildcard() {
					return webdav.NewHTTPError(http.StatusPreconditionFailed,
						fmt.Errorf("there is already an event there"))
				}
				matched, err := options.IfNoneMatch.MatchETag(existing.ETag)
				if err == nil && matched {
					return webdav.NewHTTPError(http.StatusPreconditionFailed,
						fmt.Errorf("that event is already the version you have"))
				}
			}
		}
		// A calendar has a ceiling, enforced here rather than by cutting
		// the listing short: a client reads the listing as the whole
		// truth, so a calendar it cannot list completely is one whose
		// events it would decide had been deleted.
		if existing == nil {
			held, err := tx.CountCalendarObjects(found.ID)
			if err != nil {
				return unexpectedCalendar(err)
			}
			if held >= db.ObjectsPerCalendar {
				return webdav.NewHTTPError(http.StatusInsufficientStorage,
					fmt.Errorf("this calendar already holds %d events, which is as many as this server keeps", db.ObjectsPerCalendar))
			}
		}
		// A file names the event it is about, and one claiming a name
		// that belongs to an event kept under another file name is
		// refused rather than quietly landed on top of it. 409 is what
		// the protocol has for this, and it tells the client to go and
		// look rather than to try again.
		twin, err := tx.GetCalendarObjectByUID(found.ID, kept.UID)
		if err != nil {
			return unexpectedCalendar(err)
		}
		if twin != nil && twin.ID != objectId {
			return webdav.NewHTTPError(http.StatusConflict,
				fmt.Errorf("an event with that identifier is already kept here under another name"))
		}
		if existing != nil {
			kept.CreatedAt = existing.CreatedAt
		}
		stored, err = tx.PutCalendarObject(kept, occurrences)
		return unexpectedCalendar(err)
	}); err != nil {
		return nil, err
	}
	return self.object(signedIn, stored)
}

func (self *calendarBackend) DeleteCalendarObject(ctx context.Context, address string) error {
	signedIn := self.who(ctx)
	found, objectId, err := self.objectAt(ctx, signedIn, address)
	if err != nil {
		return davError(err)
	}
	wanted := webdav.ConditionalMatch(ifMatchFrom(ctx))
	return self.component.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Held, so that "remove it only if it is still the version I
		// read" cannot be answered about a version somebody else is in
		// the middle of replacing.
		existing, err := tx.LockCalendarObject(found.ID, objectId)
		if err != nil {
			return unexpectedCalendar(err)
		}
		if existing == nil {
			return davError(refuse(http.StatusNotFound, "no such event"))
		}
		// "Remove it only if it is still the version I read." A device
		// holding a stale copy would otherwise delete an edit made
		// somewhere else that it has never seen, and with no tombstone
		// there would be nothing left to recover from.
		if wanted.IsSet() {
			matched, err := wanted.MatchETag(existing.ETag)
			if err != nil || !matched {
				return webdav.NewHTTPError(http.StatusPreconditionFailed,
					fmt.Errorf("that event has changed since you read it"))
			}
		}
		return unexpectedCalendar(tx.DeleteCalendarObject(found.ID, objectId))
	})
}

// object is one stored event as the protocol describes it.
func (self *calendarBackend) object(signedIn *session, object *models.CalendarObject) (*caldav.CalendarObject, error) {
	decoded, err := ical.NewDecoder(strings.NewReader(object.Data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("dav: a stored event cannot be read back: %w", err)
	}
	// The length of what is stored, because that is what is served: a
	// fetch and a report both write object.Data byte for byte. Computing
	// it by encoding the decoded form instead would be a second answer to
	// a question with one right answer, and a wrong one -- the library
	// does not fold, and what is stored is folded, so it would read short
	// by two octets for every line this server broke.
	return &caldav.CalendarObject{
		Path:          eventPath(signedIn.userID, object.CalendarID, object.ID),
		ModTime:       object.ModifiedAt,
		ContentLength: int64(len(object.Data)),
		ETag:          object.ETag,
		Data:          decoded,
	}, nil
}

// calendarAt is the calendar a URL names, refused unless it is this person's.
func (self *calendarBackend) calendarAt(ctx context.Context, signedIn *session, address string) (*models.Calendar, error) {
	segments := segmentsOf(address)
	if len(segments) < 3 {
		return nil, refuse(http.StatusNotFound, "no such calendar")
	}
	var found *models.Calendar
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		found, err = tx.GetCalendar(segments[2])
		return unexpectedCalendar(err)
	}); err != nil {
		return nil, err
	}
	if found == nil || found.UserID != signedIn.userID {
		return nil, refuse(http.StatusNotFound, "no such calendar")
	}
	return found, nil
}

// objectAt is the calendar and the event identifier a URL names. The file
// name is the client's to choose and clients do not agree on what it should
// be, so whatever it is becomes the identifier, with the .ics taken off.
func (self *calendarBackend) objectAt(ctx context.Context, signedIn *session, address string) (*models.Calendar, string, error) {
	found, err := self.calendarAt(ctx, signedIn, address)
	if err != nil {
		return nil, "", err
	}
	segments := segmentsOf(address)
	if len(segments) < 4 || segments[3] == "" {
		return nil, "", refuse(http.StatusNotFound, "no such event")
	}
	name := strings.TrimSuffix(segments[3], eventSuffix)
	// The client chose this, so check it rather than trust it. The length
	// is the column's: a phone names a file after its UID, which is a
	// thirty-six character UUID, and anything much longer is a client
	// doing something strange.
	if name == "" || len(name) > maximumEventName || strings.ContainsAny(name, "/\\") ||
		strings.ContainsFunc(name, func(letter rune) bool { return letter < 0x20 || letter == 0x7f }) {
		return nil, "", refuse(http.StatusBadRequest, "that is not a name this server can keep an event under")
	}
	return found, name, nil
}

// storedEvent is one event exactly as it is kept, for serving a fetch without
// going back through the protocol library's encoder.
func (self *calendarBackend) storedEvent(ctx context.Context, address string) (*models.CalendarObject, error) {
	signedIn := self.who(ctx)
	found, objectId, err := self.objectAt(ctx, signedIn, address)
	if err != nil {
		return nil, davError(err)
	}
	var object *models.CalendarObject
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		object, err = tx.GetCalendarObject(found.ID, objectId)
		return unexpectedCalendar(err)
	}); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, refuse(http.StatusNotFound, "no such event")
	}
	return object, nil
}

// storedEvents is every event in a calendar, exactly as stored, for answering
// a report without going back through the protocol library's encoder.
func (self *calendarBackend) storedEvents(ctx context.Context, address string) ([]*models.CalendarObject, error) {
	signedIn := self.who(ctx)
	found, err := self.calendarAt(ctx, signedIn, address)
	if err != nil {
		return nil, err
	}
	var objects []*models.CalendarObject
	if err := self.component.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		objects, err = tx.ListCalendarObjects(found.ID)
		return unexpectedCalendar(err)
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

// unexpectedCalendar hides a failure that is nobody's business but this
// server's. The same reasoning as unexpected, and a separate function only so
// that what is logged says which of the two collections was being served.
func unexpectedCalendar(err error) error {
	if err == nil {
		return nil
	}
	// Except for the one thing a client can do something about: a window so
	// wide that describing it would mean building an answer this server
	// does not build. That is the client's to narrow, and telling it so is
	// the difference between a request it can fix and a server that looks
	// broken.
	//
	// As an answer rather than as the library's own error, because both
	// paths that raise this one serve themselves and read the status back
	// off it -- written the library's way it was built, thrown away, and
	// sent as the blank 500 it was written to avoid.
	if errors.Is(err, db.ErrTooMuchAsked) {
		return refuse(http.StatusForbidden,
			"that stretch of time is too long to answer at once; ask about a shorter one")
	}
	log.Errorf("a calendar request could not be served: %s", err)
	return webdav.NewHTTPError(http.StatusInternalServerError,
		fmt.Errorf("this server could not do that just now"))
}
