package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing a calendar: with one person or one circle, at a level, and the
// calendar a circle keeps for itself. docs/subsystems/calendar.md ("Sharing")
// says what each level shows, and docs/decisions/
// 20261016-a-calendar-is-shared-at-a-level.md why.
//
// Who may read or write which calendar is decided in one place,
// graph.reachCalendar (calendar.go), from the database's CalendarAccess. The
// operations here are about who a calendar is shared with, which is the
// owner's alone.

// CalendarShareView is one share of the caller's calendar.
type CalendarShareView struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId"`

	// Who it is shared with: a person or a circle, never both.
	GranteeUserID     *string `json:"granteeUserId"`
	GranteeUsername   string  `json:"granteeUsername"`
	GranteeName       string  `json:"granteeName"`
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	// busy, titles, details or edit
	ShareLevel string `json:"shareLevel"`

	CreatedAt time.Time `json:"createdAt"`
}

func describeCalendarShare(share *models.CalendarShare) *CalendarShareView {
	view := &CalendarShareView{
		ID: share.ID, CalendarID: share.CalendarID, ShareLevel: string(share.ShareLevel), CreatedAt: share.CreatedAt,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

type ListCalendarSharesArguments struct {
	CalendarID string `json:"calendarId"`
}

func (self *graph) ListCalendarShares(ctx context.Context, arguments ListCalendarSharesArguments) ([]*CalendarShareView, error) {
	principal, err := self.requireCalendarShareEnder(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachCalendar(ctx, principal, arguments.CalendarID, models.ShareLevelOwner)
	if err != nil {
		return nil, err
	}
	shares, err := self.transaction(ctx).ListCalendarShares(reach.Calendar.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*CalendarShareView, 0, len(shares))
	for _, share := range shares {
		views = append(views, describeCalendarShare(share))
	}
	return views, nil
}

type ShareCalendarArguments struct {
	// The caller's own events calendar
	CalendarID string `json:"calendarId"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// busy, titles, details or edit
	ShareLevel string `json:"shareLevel"`
}

func (self *graph) ShareCalendar(ctx context.Context, arguments ShareCalendarArguments) (*CalendarShareView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachCalendar(ctx, principal, arguments.CalendarID, models.ShareLevelOwner)
	if err != nil {
		return nil, err
	}
	shareLevel := models.ShareLevel(strings.ToLower(strings.TrimSpace(arguments.ShareLevel)))
	if !shareLevel.IsGrantable() {
		return nil, fmt.Errorf("%w: a calendar is shared at busy, titles, details or edit", api.ErrInvalidArguments)
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	if reach.Calendar.CalendarKind != models.CalendarEvents {
		return nil, fmt.Errorf("%w: only the calendar of events is shared", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	// What the grantee was given before, to tell whether this is more.
	earlier, err := tx.ListCalendarShares(reach.Calendar.ID)
	if err != nil {
		return nil, err
	}
	saved, err := tx.ShareCalendar(principal.User.ID, &models.CalendarShare{
		CalendarID: reach.Calendar.ID, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, ShareLevel: shareLevel,
	})
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in: all of them answer as nothing found, because
		// which of them it was is not the caller's to learn.
		return nil, translateError(err)
	}
	shares, err := tx.ListCalendarShares(reach.Calendar.ID)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		if share.ID == saved.ID {
			saved = share
		}
	}
	if isCalendarShareWider(earlier, saved) {
		if err := self.tellAboutShare(ctx, principal.User, models.ShareReceivedNote{
			SharerName: principal.User.DisplayName(), ShareKind: models.ShareKindCalendar, ShareLevel: string(saved.ShareLevel),
		}, granteeUserId, granteeCircleId); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s shared their calendar at %s", principal.User.Username, shareLevel)
	return describeCalendarShare(saved), nil
}

// isCalendarShareWider says whether the share gives its grantee more than the
// calendar's shares did before it: there was none for them, or a lower level.
func isCalendarShareWider(earlier []*models.CalendarShare, saved *models.CalendarShare) bool {
	for _, share := range earlier {
		if share.GranteeUserID == saved.GranteeUserID && share.GranteeCircleID == saved.GranteeCircleID {
			return saved.ShareLevel.Rank() > share.ShareLevel.Rank()
		}
	}
	return true
}

type UnshareCalendarArguments struct {
	ShareID string `json:"shareId"`
}

func (self *graph) UnshareCalendar(ctx context.Context, arguments UnshareCalendarArguments) (bool, error) {
	principal, err := self.requireCalendarShareEnder(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	share, err := tx.GetCalendarShare(strings.TrimSpace(arguments.ShareID))
	if err != nil {
		return false, err
	}
	if share == nil {
		return false, api.ErrNotFound
	}
	// The share is the calendar's owner's to take back; anyone else is told
	// there is no such share.
	if _, err := self.reachCalendar(ctx, principal, share.CalendarID, models.ShareLevelOwner); err != nil {
		return false, err
	}
	if err := tx.DeleteCalendarShare(share.ID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

type CreateCircleCalendarArguments struct {
	CircleID string `json:"circleId"`

	// What to call it; the circle's name when left out
	Name string `json:"name" graphapi:"nullable"`
}

func (self *graph) CreateCircleCalendar(ctx context.Context, arguments CreateCircleCalendarArguments) (*CalendarView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	if !self.isServiceEnabled(models.ServiceCalendar) {
		return nil, &models.ServiceUnavailableError{Service: models.ServiceCalendar}
	}
	circle, _, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	made, err := tx.CreateCircleCalendar(circle.ID, strings.TrimSpace(arguments.Name), self.zoneFor(principal))
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s made a calendar for the circle %q", principal.User.Username, circle.Name)
	return self.calendarView(principal, &models.ReachableCalendar{
		Calendar: made, ShareLevel: models.ShareLevelEdit, CircleName: circle.Name,
	}, 0), nil
}

type DeleteCircleCalendarArguments struct {
	CircleID string `json:"circleId"`
}

func (self *graph) DeleteCircleCalendar(ctx context.Context, arguments DeleteCircleCalendarArguments) (bool, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return false, err
	}
	if !self.isServiceEnabled(models.ServiceCalendar) {
		return false, &models.ServiceUnavailableError{Service: models.ServiceCalendar}
	}
	circle, _, err := self.requireCircleAdmin(ctx, arguments.CircleID)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).DeleteCircleCalendar(circle.ID); err != nil {
		return false, translateError(err)
	}
	log.Noticef("%s deleted the calendar of the circle %q", principal.User.Username, circle.Name)
	return true, nil
}

// The most people one free-busy question is about. Each is a window of one
// person's calendar read, so the number is a cost; far past the people a
// meeting is arranged among.
const maximumFreeBusyPeople = 20

type ListCalendarFreeBusyArguments struct {
	// The people to ask about, by account identifier; the caller's own is
	// allowed and answered the same way
	UserIDs []string `json:"userIds"`

	// The window, as RFC 3339 moments
	From  string `json:"from"`
	Until string `json:"until"`
}

// CalendarFreeBusyView is when one person is busy, for a caller they share
// their calendar with at busy or above.
type CalendarFreeBusyView struct {
	UserID string `json:"userId"`

	// Who they are, told only when IsShared is true
	Username string `json:"username"`
	Name     string `json:"name"`

	// IsShared is false for a person who has shared nothing with the caller,
	// and for an identifier that is nobody's: the two are not told apart.
	IsShared bool `json:"isShared"`

	// Their calendar's level for the caller: owner for the caller's own
	ShareLevel string `json:"shareLevel"`

	// The stretches they are busy, merged, in the window and clipped to it
	Periods []*CalendarBusyPeriod `json:"periods"`
}

// CalendarBusyPeriod is a stretch of time somebody is busy.
type CalendarBusyPeriod struct {
	StartsAt string `json:"startsAt"`
	EndsAt   string `json:"endsAt"`
}

func (self *graph) ListCalendarFreeBusy(ctx context.Context, arguments ListCalendarFreeBusyArguments) ([]*CalendarFreeBusyView, error) {
	principal, err := self.requireCalendarPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.requireService(models.ServiceCalendar); err != nil {
		return nil, err
	}
	from, until, err := window(arguments.From, arguments.Until)
	if err != nil {
		return nil, err
	}
	if len(arguments.UserIDs) == 0 || len(arguments.UserIDs) > maximumFreeBusyPeople {
		return nil, fmt.Errorf("%w: ask about between one and %d people", api.ErrInvalidArguments, maximumFreeBusyPeople)
	}
	tx := self.transaction(ctx)
	views := make([]*CalendarFreeBusyView, 0, len(arguments.UserIDs))
	seen := make(map[string]bool, len(arguments.UserIDs))
	for _, userId := range arguments.UserIDs {
		userId = strings.TrimSpace(userId)
		if userId == "" || seen[userId] {
			continue
		}
		seen[userId] = true
		view := &CalendarFreeBusyView{UserID: userId, Periods: []*CalendarBusyPeriod{}}
		views = append(views, view)

		found, err := tx.FindCalendar(userId, models.CalendarEvents)
		if err != nil {
			return nil, err
		}
		if found == nil {
			continue
		}
		shareLevel, _, err := tx.CalendarAccess(principal.User.ID, found)
		if err != nil {
			return nil, err
		}
		if !shareLevel.AtLeast(models.ShareLevelBusy) {
			continue
		}
		periods, err := self.busyPeriods(ctx, found, from, until)
		if err != nil {
			return nil, err
		}
		view.IsShared, view.ShareLevel = true, string(shareLevel)
		owner := principal.User
		if userId != principal.User.ID {
			if owner, err = tx.GetUser(userId); err != nil {
				return nil, err
			}
		}
		if owner != nil {
			view.Username, view.Name = owner.Username, owner.DisplayName()
		}
		for _, period := range periods {
			view.Periods = append(view.Periods, &CalendarBusyPeriod{
				StartsAt: period.StartsAt.UTC().Format(time.RFC3339), EndsAt: period.EndsAt.UTC().Format(time.RFC3339),
			})
		}
	}
	return views, nil
}
