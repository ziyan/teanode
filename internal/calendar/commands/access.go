package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// ErrRefused is a change the person's access to the calendar does not allow,
// said in words they can act on. It is only ever returned to somebody who can
// already see the calendar: a calendar they cannot reach at all is not found,
// and nothing here tells them it exists.
var ErrRefused = errors.New("calendar: refused")

func refused(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, arguments...))
}

// writableCalendar locks a calendar and checks that the person may change the
// events in it: their own, a circle's they are in, or another person's shared
// with them at edit. It reports whether the calendar is the person's own,
// because an event with guests is only ever the owner's to change: the
// invitations and the answers go out from, and come back to, the owner.
//
// This and the other two functions of this file are where a command asks "may
// this person do this to this calendar"; none of them compares a calendar's
// UserID, which a circle's calendar does not have.
func writableCalendar(transaction db.Transaction, principal *access.Principal, calendarId string) (*models.Calendar, bool, error) {
	locked, err := transaction.LockCalendar(strings.TrimSpace(calendarId))
	if err != nil {
		return nil, false, err
	}
	if locked == nil || principal == nil || principal.User == nil {
		return nil, false, db.ErrNotFound
	}
	shareLevel, isOwner, err := transaction.CalendarAccess(principal.User.ID, locked)
	if err != nil {
		return nil, false, err
	}
	if shareLevel == "" {
		return nil, false, db.ErrNotFound
	}
	if !shareLevel.CanWrite() {
		return nil, false, refusedLevel(shareLevel)
	}
	return locked, isOwner, nil
}

func refusedLevel(shareLevel models.ShareLevel) error {
	return refused("this calendar is shared with you at the level %q, and you can only change it when it is shared at edit", shareLevel)
}

// refuseGuests refuses a change by somebody who is not the owner to an event
// with guests, or one that would give it guests.
//
// The reply to an invitation goes to the organizer's address, and an
// invitation that arrived by mail belongs to the calendar of the person it
// was sent to. Changing it from somebody else's account would send mail that
// looks as though the owner wrote it, and a circle's calendar has no owner to
// write it at all.
func refuseGuests(found *models.Calendar, isOwner bool, objects ...*models.CalendarObject) error {
	if isOwner {
		return nil
	}
	for _, object := range objects {
		if object == nil {
			continue
		}
		parsed, err := calendar.Parse([]byte(object.Data))
		if err != nil || len(parsed.Attendees) == 0 {
			continue
		}
		return refusedGuests(found)
	}
	return nil
}

// refusePrivate refuses a change by somebody who is not the owner to an event
// its owner marked private. They are shown it only as busy, and a change made
// from that copy would write the busy copy over the real event. A calendar
// that belongs to a circle has no owner to keep an event private from.
func refusePrivate(found *models.Calendar, isOwner bool, object *models.CalendarObject) error {
	if isOwner || found.CircleID != "" || object == nil || !calendar.IsPrivateFile(object.Data) {
		return nil
	}
	return refused("this event is private to its owner")
}

// refuseParsedGuests is refuseGuests for an event about to be kept.
func refuseParsedGuests(found *models.Calendar, isOwner bool, parsed *calendar.Parsed) error {
	if isOwner || parsed == nil || len(parsed.Attendees) == 0 {
		return nil
	}
	return refusedGuests(found)
}

func refusedGuests(found *models.Calendar) error {
	if found.CircleID != "" {
		return refused("an event in a circle's calendar cannot have guests, because there is nobody to send the invitations or to answer them")
	}
	return refused("this event has guests, and changing it would send them mail in the owner's name; only the owner of the calendar can do that")
}

// requireManagedCalendar locks a calendar and checks that the person manages
// it: their own, or the calendar of a circle they are an admin of. What is
// about how a calendar is shown, or who it is shared with, is the manager's and
// nobody else's, whatever level it was shared at, and a member of a circle who
// is not an admin reads and writes its events and changes nothing about it.
func requireManagedCalendar(transaction db.Transaction, principal *access.Principal, calendarId string) (*models.Calendar, error) {
	locked, err := transaction.LockCalendar(strings.TrimSpace(calendarId))
	if err != nil {
		return nil, err
	}
	if locked == nil || principal == nil || principal.User == nil {
		return nil, db.ErrNotFound
	}
	shareLevel, isOwner, err := transaction.CalendarAccess(principal.User.ID, locked)
	if err != nil {
		return nil, err
	}
	if shareLevel == "" {
		return nil, db.ErrNotFound
	}
	if locked.CircleID != "" {
		circle, err := transaction.GetCircle(locked.CircleID)
		if err != nil {
			return nil, err
		}
		if circle == nil || !isCircleAdmin(circle, principal.User.ID) {
			return nil, refused("only an admin of this circle can change how its calendar is shown")
		}
		return locked, nil
	}
	if !isOwner {
		return nil, refused("only the owner of this calendar can change how it is shown")
	}
	return locked, nil
}

func isCircleAdmin(circle *models.Circle, userId string) bool {
	member := circle.Member(userId)
	return member != nil && member.MemberRole == models.MemberRoleAdmin
}
