package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/calendar"
	calendarcommands "github.com/ziyan/teanode/internal/calendar/commands"
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

	// Completion of a retained request, including when its event no longer exists.
	GetCalendarRequest(ctx context.Context, arguments CalendarRequestArguments) (*CalendarRequestView, error)

	// Who a calendar is shared with, and at what level. Only the calendar's
	// owner is told. Needs calendar:use.
	ListCalendarShares(ctx context.Context, arguments ListCalendarSharesArguments) ([]*CalendarShareView, error)

	// When each of these people is busy between two moments, for the ones
	// whose calendar is shared with the caller at busy or above, and the
	// caller's own. A person who has shared nothing with the caller is
	// answered as not shared, the same as one who does not exist. Merged, so
	// that the caller can take the union of several people's and ask when they
	// are all free. Needs calendar:use.
	ListCalendarFreeBusy(ctx context.Context, arguments ListCalendarFreeBusyArguments) ([]*CalendarFreeBusyView, error)
}

// CalendarMutation changes it.
type CalendarMutation interface {
	// Resolve an uncertain request before abandoning its retained parameters.
	CancelCalendarRequest(ctx context.Context, arguments CalendarRequestArguments) (*CalendarRequestView, error)
	// Keep an event, or change one that is kept. Give either a whole
	// iCalendar file, which is what a program that speaks the format
	// sends, or the filled-in fields, which is what the dashboard sends;
	// with the fields, anything already on the event that they do not
	// cover is kept, so that correcting a title here does not throw away
	// the alarm a phone put there. Needs calendar:use.
	SaveCalendarEvent(ctx context.Context, arguments SaveCalendarEventArguments) (*CalendarEventView, error)

	// Take one away. Needs calendar:use.
	DeleteCalendarEvent(ctx context.Context, arguments DeleteCalendarEventArguments) (bool, error)

	// Rename a calendar, or change how it is shown. Needs calendar:use.
	SaveCalendar(ctx context.Context, arguments SaveCalendarArguments) (*CalendarView, error)

	// Share the caller's own calendar with one person or one circle at a
	// level, or change the level of a share already made. The person must
	// share a circle with the caller, and the caller must be in the circle.
	// Needs calendar:use.
	ShareCalendar(ctx context.Context, arguments ShareCalendarArguments) (*CalendarShareView, error)

	// Take a share back. Only the calendar's owner can. Needs calendar:use.
	UnshareCalendar(ctx context.Context, arguments UnshareCalendarArguments) (bool, error)

	// Make a circle's calendar, which every member reads and writes. A circle
	// has one. Needs the caller to be an admin of the circle.
	CreateCircleCalendar(ctx context.Context, arguments CreateCircleCalendarArguments) (*CalendarView, error)
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

	// AgentGranted says the person has given their agent this calendar. The
	// agent's own tools read it: what is not granted is not theirs to see.
	AgentGranted bool `json:"agentGranted"`

	// CalendarKind is what it holds: events, or reminders. A person has one
	// of each, and whatever works on "the calendar" means the events one.
	CalendarKind string `json:"calendarKind"`

	// ShareLevel is what the caller may do with it: owner for their own,
	// busy, titles, details or edit for one shared with them, and edit for a
	// circle's. Below edit it is read only, and at busy it shows no events,
	// only when the owner is busy.
	ShareLevel string `json:"shareLevel"`

	// Whose it is: their account, and what to call them. Null and empty for a
	// circle's calendar, which belongs to the circle.
	OwnerUserID           *string `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	// The circle a circle calendar belongs to; null for any other.
	CircleID         *string `json:"circleId"`
	CircleName       string  `json:"circleName"`
	IsCircleCalendar bool    `json:"isCircleCalendar"`

	// Which of eight colors to paint it in when it has no colour of its own,
	// the same on every screen for the same calendar.
	ColorIndex int `json:"colorIndex"`
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

	// IsBusyOnly says this is all the caller is told of it: that the owner is
	// busy. Its title is empty and screens say "Busy" in the caller's
	// language.
	IsBusyOnly bool `json:"isBusyOnly"`

	// IsPrivate says the owner marked this event private (CLASS:PRIVATE or
	// CLASS:CONFIDENTIAL): everybody the calendar is shared with sees it as
	// busy. Only the owner and editors ever read an event for which it is true.
	IsPrivate bool `json:"isPrivate"`

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

type DeleteCalendarEventArguments struct {
	CalendarID string `json:"calendarId"`
	ID         string `json:"id"`
	RequestID  string `json:"requestId" graphapi:"nullable"`
}

type SaveCalendarEventArguments struct {
	// These optional fields bind a corrected event to the offer being accepted.
	// Omit them from ordinary request digests for compatibility with old receipts.
	ProposalItemID   string `json:"proposalItemId,omitempty" graphapi:"nullable"`
	ProposalIndex    *int   `json:"proposalIndex,omitempty" graphapi:"nullable"`
	ExpectedProposal string `json:"expectedProposal,omitempty" graphapi:"nullable"`
	// RequestID is retained by the caller across retries of the exact same input.
	RequestID  string `json:"requestId" graphapi:"nullable"`
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

	// IsPrivate marks the event private, so that those the calendar is shared
	// with see only that the owner is busy; false takes the mark away.
	// Omitted when empty, like the proposal fields, so that the digest of a
	// request that does not mention it is what it was before this field.
	IsPrivate *bool `json:"isPrivate,omitempty" graphapi:"nullable"`

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
	if !self.isServiceEnabled(models.ServiceCalendar) && !self.isServiceEnabled(models.ServiceReminders) {
		return nil, &models.ServiceUnavailableError{Service: models.ServiceCalendar}
	}
	principal, err := self.requirePermission(ctx, models.PermissionCalendarUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// requireCalendarShareEnder is the caller, if they may end what they shared.
// It asks for no calendar:use, so that somebody whose role lost it can still
// take back a share they made while they had it, and see what they shared.
func (self *graph) requireCalendarShareEnder(ctx context.Context) (*api.Principal, error) {
	if !self.isServiceEnabled(models.ServiceCalendar) {
		return nil, &models.ServiceUnavailableError{Service: models.ServiceCalendar}
	}
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// calendarReach is a calendar and what the caller may do with it.
type calendarReach struct {
	Calendar   *models.Calendar
	ShareLevel models.ShareLevel
	IsOwner    bool
}

// reachCalendar is one calendar, if the caller reaches it at the level
// asked for or better: their own, a circle's they are in, or another person's
// shared with them. Anything else, and a level that falls short, is not found,
// rather than refused, because whether a calendar exists and whose it is is not
// the caller's business.
//
// This is the one question "may this person read or write this calendar, and
// at what level", and the answer is the database's CalendarAccess. Nothing
// here compares a calendar's UserID with the caller, which a circle's calendar
// does not have.
func (self *graph) reachCalendar(ctx context.Context, principal *api.Principal, calendarId string, atLeast models.ShareLevel) (*calendarReach, error) {
	tx := self.transaction(ctx)
	found, err := tx.GetCalendar(strings.TrimSpace(calendarId))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, api.ErrNotFound
	}
	shareLevel, isOwner, err := tx.CalendarAccess(principal.User.ID, found)
	if err != nil {
		return nil, err
	}
	if !shareLevel.AtLeast(atLeast) {
		return nil, api.ErrNotFound
	}
	if err := self.requireService(calendarService(found.CalendarKind)); err != nil {
		return nil, err
	}
	return &calendarReach{Calendar: found, ShareLevel: shareLevel, IsOwner: isOwner}, nil
}

// translateCalendarError is translateError, and keeps the words of a refusal
// the caller can act on: this calendar is read only to them, or an event with
// guests is the owner's to change.
func translateCalendarError(err error) error {
	if errors.Is(err, calendarcommands.ErrRefused) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, strings.TrimPrefix(err.Error(), calendarcommands.ErrRefused.Error()+": "))
	}
	return translateError(err)
}

func (self *graph) ListCalendars(ctx context.Context) ([]*CalendarView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	var reachable []*models.ReachableCalendar
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Nobody should have to make a calendar before they can put
		// something in it, and the CalDAV layout needs one to exist
		// before a phone can be pointed at it.
		if self.isServiceEnabled(models.ServiceCalendar) {
			if _, err := tx.EnsureCalendar(principal.User.ID, models.CalendarEvents, &models.Calendar{Timezone: self.zoneFor(principal)}); err != nil {
				return err
			}
		}
		// And the reminders list beside it, so that the switch that gives
		// it to the agent is there before anything has been put in it.
		if self.isServiceEnabled(models.ServiceReminders) {
			if _, err := tx.EnsureCalendar(principal.User.ID, models.CalendarReminders, &models.Calendar{Timezone: self.zoneFor(principal)}); err != nil {
				return err
			}
		}
		// The caller's own, then their circles', then the ones shared with
		// them, each with the level the caller reaches it at.
		found, err := tx.ListReachableCalendars(principal.User.ID)
		reachable = found
		return err
	}); err != nil {
		return nil, err
	}
	views := make([]*CalendarView, 0, len(reachable))
	for _, each := range reachable {
		if !self.isServiceEnabled(calendarService(each.Calendar.CalendarKind)) {
			continue
		}
		// How much is in it is told to whoever may read the events, and not
		// to somebody who is only told when the owner is busy.
		var count int64
		if each.ShareLevel.CanRead() {
			if count, err = self.transaction(ctx).CountCalendarObjects(each.Calendar.ID); err != nil {
				return nil, err
			}
		}
		views = append(views, self.calendarView(principal, each, count))
	}
	spreadCalendarColors(views)
	return views, nil
}

// spreadCalendarColors gives each calendar in one person's list a color none
// of the others has, for as long as there are colors left, so that three
// calendars on a screen are three colors rather than whatever the hash of
// their identifiers collided to. A calendar keeps the color its identifier
// hashes to when that is free; otherwise it takes the next free one after it.
// The person's own calendars choose first, then the rest by identifier, so
// the same list is the same colors on the dashboard and the iPhone, which
// both draw what this says. With more calendars than colors, the ones past
// the eighth keep the hashed color.
func spreadCalendarColors(views []*CalendarView) {
	ordered := make([]*CalendarView, len(views))
	copy(ordered, views)
	sort.SliceStable(ordered, func(first, second int) bool {
		isFirstOwn, isSecondOwn := ordered[first].ShareLevel == string(models.ShareLevelOwner), ordered[second].ShareLevel == string(models.ShareLevelOwner)
		if isFirstOwn != isSecondOwn {
			return isFirstOwn
		}
		return ordered[first].ID < ordered[second].ID
	})
	isTaken := make([]bool, calendarColorCount)
	takenCount := 0
	for _, view := range ordered {
		if takenCount == calendarColorCount {
			break
		}
		colorIndex := view.ColorIndex
		for isTaken[colorIndex] {
			colorIndex = (colorIndex + 1) % calendarColorCount
		}
		isTaken[colorIndex] = true
		takenCount++
		view.ColorIndex = colorIndex
	}
}

// calendarView is a calendar as the caller reaches it. What belongs to the
// owner alone (the agent's switch) is false for anyone else, and the zone the
// owner keeps their day in, the description and the title of the calendar are
// not told to someone who sees only when they are busy: the title is replaced
// by the owner's name.
func (self *graph) calendarView(principal *api.Principal, each *models.ReachableCalendar, count int64) *CalendarView {
	found := each.Calendar
	view := &CalendarView{
		ID: found.ID, Name: found.Name, Description: found.Description,
		Colour: found.Colour, Timezone: found.Timezone,
		WeekStart: weekStartOf(found), Events: int(count),
		AgentGranted: found.AgentGranted && each.ShareLevel == models.ShareLevelOwner,
		CalendarKind: string(found.CalendarKind),
		ShareLevel:   string(each.ShareLevel),
		ColorIndex:   calendarColorIndex(found.ID),
	}
	owner := each.Owner
	if each.ShareLevel == models.ShareLevelOwner {
		owner = principal.User
	}
	if each.ShareLevel != models.ShareLevelOwner && !each.ShareLevel.CanRead() {
		view.Timezone, view.Description = "", ""
		// The title of a calendar can say what is in it, so somebody who is
		// told only when the owner is busy is told whose it is instead.
		view.Name = ""
		if owner != nil {
			view.Name = owner.DisplayName()
		}
	}
	if found.CircleID != "" {
		view.IsCircleCalendar = true
		view.CircleID = &found.CircleID
		view.CircleName = each.CircleName
		return view
	}
	view.OwnerUserID = &found.UserID
	if owner != nil {
		view.OwnerUsername = owner.Username
		view.OwnerName = owner.DisplayName()
		view.OwnerAvatarURL = avatarUrl(owner)
	}
	view.OwnerAvatarColorIndex = avatar.InitialColorIndex(found.UserID)
	return view
}

// calendarColorIndex is which of eight colors a calendar is painted in when it
// has none of its own. The dashboard and the iPhone app compute the same
// number from the same identifier, so a calendar is the same color on every
// screen; the hash is the one avatars use, spelled out in internal/avatar.
func calendarColorIndex(calendarId string) int {
	return avatar.InitialColorIndex(calendarId) % calendarColorCount
}

// calendarColorCount is how many colors the screens paint calendars in.
const calendarColorCount = 8

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
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachCalendar(ctx, principal, arguments.CalendarID, models.ShareLevelBusy)
	if err != nil {
		return nil, err
	}
	found := reach.Calendar
	from, until, err := window(arguments.From, arguments.Until)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if !reach.ShareLevel.CanRead() {
		// Shared at busy: when the owner is busy, by the same judgments and
		// the same merging as their own free-busy, and not one word of what
		// any of it is.
		periods, err := self.busyPeriods(ctx, found, from, until)
		if err != nil {
			return nil, err
		}
		views := make([]*CalendarEventView, 0, len(periods))
		for _, period := range periods {
			views = append(views, &CalendarEventView{
				CalendarID: found.ID, IsBusyOnly: true, Attendees: []*CalendarAttendee{},
				StartsAt: period.StartsAt.UTC().Format(time.RFC3339), EndsAt: period.EndsAt.UTC().Format(time.RFC3339),
			})
		}
		return views, nil
	}
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
			// As the caller's level lets them see it.
			if object, err = tx.ReadCalendarObject(found.ID, occurrence.ObjectID, reach.ShareLevel); err != nil {
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

// busyPeriods is when the owner of a calendar is busy, merged: the events that
// make them busy by calendar.MakesBusy, as calendar.FreeBusy merges them. The
// owner's own free-busy over CalDAV is the same computation.
func (self *graph) busyPeriods(ctx context.Context, found *models.Calendar, from, until time.Time) ([]calendar.Period, error) {
	tx := self.transaction(ctx)
	theirs, err := calendarcommands.OwnerAddresses(tx, found.UserID)
	if err != nil {
		log.Warningf("cannot read the addresses of a calendar's owner: %s", err)
	}
	busy, err := calendarcommands.BusyOccurrences(tx, found.ID, theirs, from, until)
	if err != nil {
		return nil, translateError(err)
	}
	return calendar.FreeBusy(busy, from, until), nil
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
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	// At titles or above: shared at busy, a calendar has no events to fetch,
	// only the owner's busy time.
	reach, err := self.reachCalendar(ctx, principal, arguments.CalendarID, models.ShareLevelTitles)
	if err != nil {
		return nil, err
	}
	object, err := self.transaction(ctx).ReadCalendarObject(reach.Calendar.ID, strings.TrimSpace(arguments.ID), reach.ShareLevel)
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
	if parsed.IsBusyOnly {
		// All the caller is told of it. The label is the screens' to
		// translate, so the title is empty rather than the word "Busy".
		view.IsBusyOnly = true
		view.Summary, view.Location = "", ""
	}
	view.IsPrivate = parsed.IsPrivate
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
	if arguments.RequestID == "" {
		if arguments.ProposalItemID != "" || arguments.ProposalIndex != nil || arguments.ExpectedProposal != "" {
			return nil, api.ErrInvalidArguments
		}
		view, _, err := self.saveCalendarEvent(ctx, arguments)
		return view, err
	}
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	requestContent, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	var saved *CalendarEventView
	outcome, err := calendarcommands.New(self.transaction(ctx)).ExecuteRequest(ctx, principal, calendarcommands.RequestIdentity{RequestID: arguments.RequestID, CalendarID: arguments.CalendarID, Operation: "save", Content: requestContent}, func(ctx context.Context, transaction db.Transaction) (calendarcommands.RequestResult, error) {
		var isMailSendRequired bool
		var err error
		saved, isMailSendRequired, err = self.saveCalendarEvent(api.ContextWithTransaction(ctx, transaction), arguments)
		if err != nil {
			return calendarcommands.RequestResult{}, err
		}
		return calendarcommands.RequestResult{ObjectID: saved.ID, IsMailSendRequired: isMailSendRequired, IsMailWriteRequired: arguments.ProposalItemID != ""}, nil
	})
	if err != nil {
		return nil, translateCalendarError(err)
	}
	if !outcome.IsReplay {
		return saved, nil
	}
	// Return current content, never an old snapshot or a recreated deleted
	// event, and as the caller's level now lets them see it.
	reach, err := self.reachCalendar(ctx, principal, outcome.Receipt.CalendarID, models.ShareLevelTitles)
	if errors.Is(err, api.ErrNotFound) {
		// The calendar is gone, or the caller can no longer see it: the
		// event cannot be handed back, and the receipt says what happened.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	object, err := self.transaction(ctx).ReadCalendarObject(reach.Calendar.ID, outcome.Receipt.ObjectID, reach.ShareLevel)
	if err != nil {
		return nil, translateError(err)
	}
	if object == nil {
		return nil, nil
	}
	return eventView(object, true)
}

func (self *graph) saveCalendarEvent(ctx context.Context, arguments SaveCalendarEventArguments) (*CalendarEventView, bool, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, false, err
	}
	organizer, err := self.organizerFor(ctx)
	if err != nil {
		return nil, false, err
	}
	var proposalInsight *models.MailInsight
	if arguments.ProposalItemID != "" {
		proposalInsight, err = self.calendarProposal(ctx, arguments)
		if err != nil {
			return nil, false, err
		}
	} else if arguments.ProposalIndex != nil || arguments.ExpectedProposal != "" {
		return nil, false, api.ErrInvalidArguments
	}
	isMailSendRequired := false
	kept, err := calendarcommands.New(self.transaction(ctx)).SaveEvent(ctx, principal, calendarcommands.EventRequest{CalendarID: arguments.CalendarID, ID: arguments.ID}, func(ctx context.Context, transaction db.Transaction, existing *models.CalendarObject) (*calendar.Parsed, error) {
		parsed, err := buildSaved(&arguments, existing, organizer)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", db.ErrInvalidArguments, err)
		}
		if len(parsed.Attendees) > 0 && !self.isServiceEnabled(models.ServiceMailboxes) {
			return nil, &models.ServiceUnavailableError{Service: models.ServiceMailboxes}
		}
		return parsed, nil
	}, func(ctx context.Context, transaction db.Transaction, kept, before *models.CalendarObject) error {
		var err error
		isMailSendRequired, err = self.inviteTo(api.ContextWithTransaction(ctx, transaction), kept, before, organizer)
		return err
	})
	if errors.Is(err, db.ErrInvalidArguments) {
		return nil, false, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	if err != nil {
		return nil, false, translateCalendarError(err)
	}
	if proposalInsight != nil {
		if err := self.transaction(ctx).SetMailProposalStatus(proposalInsight.MailboxID, proposalInsight.MailID, *arguments.ProposalIndex, models.MailProposalAccepted); err != nil {
			return nil, false, translateError(err)
		}
	}
	view, err := eventView(kept, true)
	return view, isMailSendRequired, err
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
func (self *graph) inviteTo(ctx context.Context, kept, before *models.CalendarObject, organizer string) (bool, error) {
	if kept == nil || organizer == "" {
		return false, nil
	}
	parsed, err := calendar.Parse([]byte(kept.Data))
	if err != nil || len(parsed.Attendees) == 0 {
		return false, nil
	}
	var held *calendar.Parsed
	if before != nil {
		if decoded, err := calendar.Parse([]byte(before.Data)); err == nil {
			held = decoded
		}
	}
	asked, err := guestsToInvite(parsed, held, organizer)
	if err != nil {
		return false, err
	}
	if len(asked) == 0 {
		return false, nil
	}
	written, err := calendar.Invite([]byte(kept.Data), organizer)
	if err != nil {
		return false, err
	}
	return true, self.sendCalendarMessage(ctx, organizer, asked, kept, written, "REQUEST")
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
		Status: arguments.Status, IsPrivate: arguments.IsPrivate, Organizer: organizer,
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

func (self *graph) DeleteCalendarEvent(ctx context.Context, arguments DeleteCalendarEventArguments) (bool, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return false, err
	}
	removeEvent := func(ctx context.Context, transaction db.Transaction) (calendarcommands.RequestResult, error) {
		ctx = api.ContextWithTransaction(ctx, transaction)
		organizer, err := self.organizerFor(ctx)
		if err != nil {
			return calendarcommands.RequestResult{}, err
		}
		isMailSendRequired := false
		err = calendarcommands.New(transaction).DeleteEvent(ctx, principal, calendarcommands.EventRequest{CalendarID: arguments.CalendarID, ID: arguments.ID}, func(ctx context.Context, transaction db.Transaction, _ *models.CalendarObject, object *models.CalendarObject) error {
			var err error
			isMailSendRequired, err = self.callOff(api.ContextWithTransaction(ctx, transaction), object, organizer)
			return err
		})
		return calendarcommands.RequestResult{ObjectID: arguments.ID, IsMailSendRequired: isMailSendRequired}, err
	}
	if arguments.RequestID == "" {
		_, err = removeEvent(ctx, self.transaction(ctx))
	} else {
		var requestContent []byte
		requestContent, err = json.Marshal(arguments)
		if err != nil {
			return false, err
		}
		_, err = calendarcommands.New(self.transaction(ctx)).ExecuteRequest(ctx, principal, calendarcommands.RequestIdentity{RequestID: arguments.RequestID, CalendarID: arguments.CalendarID, Operation: "delete", Content: requestContent}, removeEvent)
	}
	if err != nil {
		return false, translateCalendarError(err)
	}
	return true, nil
}

// callOff tells the people coming that a meeting is off.
//
// Only when this person is the one who called it. Deleting an event somebody
// else organized is leaving their meeting, not cancelling it, and sending a
// cancellation would take it out of everybody else's calendar too.
func (self *graph) callOff(ctx context.Context, object *models.CalendarObject, organizer string) (bool, error) {
	if object == nil || organizer == "" {
		return false, nil
	}
	parsed, err := calendar.Parse([]byte(object.Data))
	if err != nil || len(parsed.Attendees) == 0 {
		return false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(parsed.Organizer), organizer) {
		return false, nil
	}
	// The same ceiling as inviting: an event whose guest list came from
	// somewhere else is not a mail run waiting for somebody to press
	// delete either.
	if len(parsed.Attendees) > calendar.MaximumGuests {
		return false, fmt.Errorf("%w: this event asks more than %d people, which is more than this server will write to at once",
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
		return false, nil
	}
	written, err := calendar.CallOff([]byte(object.Data), organizer)
	if err != nil {
		return false, err
	}
	return true, self.sendCalendarMessage(ctx, organizer, asked, object, written, "CANCEL")
}

func (self *graph) SaveCalendar(ctx context.Context, arguments SaveCalendarArguments) (*CalendarView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := calendarcommands.New(self.transaction(ctx)).Update(ctx, principal, calendarcommands.UpdateRequest{ID: arguments.ID, Name: arguments.Name, Description: arguments.Description, Colour: arguments.Colour, Timezone: arguments.Timezone, WeekStart: arguments.WeekStart})
	if errors.Is(err, db.ErrInvalidArguments) {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	if err != nil {
		return nil, translateCalendarError(err)
	}
	kept := outcome.Calendar
	return self.calendarView(principal, &models.ReachableCalendar{
		Calendar: kept, ShareLevel: models.ShareLevelOwner, OwnerUserID: principal.User.ID, Owner: principal.User,
	}, outcome.EventCount), nil
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
	// Sending is what this does, so it asks for the permission to send.
	//
	// Everything else that puts mail on the wire checks it -- composing,
	// leaving a mailing list, a rule that forwards, the out-of-office reply
	// -- and this did not: keeping a calendar was enough. An account with
	// calendar:use and no mail:send could put text of its own choosing in
	// front of a hundred addresses per call, from this server's domains,
	// signed and aligned.
	if _, err := self.requirePermission(ctx, models.PermissionMailSend); err != nil {
		return err
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
	envelope, err := self.calendarEnvelope(ctx, organizer)
	if err != nil {
		return err
	}

	_, err = self.mailer.AcceptSubmission(ctx, self.transaction(ctx), envelope, message)
	return err
}

// calendarEnvelope resolves a currently owned sender for transactional calendar mail.
func (self *graph) calendarEnvelope(ctx context.Context, organizer string) (*mailparse.Envelope, error) {
	principal := api.ContextPrincipal(ctx)
	if principal == nil || principal.User == nil {
		return nil, api.ErrNotFound
	}
	mailboxes, err := self.transaction(ctx).ListMailboxes(principal.User.ID)
	if err != nil {
		return nil, err
	}
	mailboxId := ""
	for _, mailbox := range mailboxes {
		for _, address := range mailbox.Addresses {
			if strings.EqualFold(strings.TrimSpace(address.Address), organizer) {
				mailboxId = mailbox.ID
				break
			}
		}
		if mailboxId != "" {
			break
		}
	}
	if mailboxId == "" {
		return nil, api.ErrNotFound
	}
	envelope := &mailparse.Envelope{MailboxID: mailboxId}
	if request := api.ContextRequest(ctx); request != nil {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			host = request.RemoteAddr
		}
		envelope.IP = net.ParseIP(host)
		envelope.Location = self.locator.Locate(envelope.IP)
		envelope.TLS = request.TLS
	}
	return envelope, nil
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

func calendarService(kind models.CalendarKind) models.Service {
	if kind == models.CalendarReminders {
		return models.ServiceReminders
	}
	return models.ServiceCalendar
}
