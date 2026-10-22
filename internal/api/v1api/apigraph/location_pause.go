package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// Pausing where a person is recorded. docs/subsystems/location.md says what a
// pause does. A pause is the person's, across their phone, the dashboard, the
// command line and their agent, so it lives on the server and every surface
// reads and sets the same thing: while one is in effect the server stores no
// position and no visit measured inside it, whatever sends them, and shows
// the people the location is shared with that it is paused instead of where
// the person is.

// LocationPauseView is a time during which a person's location is not
// recorded.
type LocationPauseView struct {
	ID string `json:"id"`

	// StartedAt is when it began. EndedAt is when it ends by itself, null for
	// a pause that lasts until the person resumes.
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
}

type PauseLocationArguments struct {
	// EndedAt is when the pause ends by itself, after now and at most 30 days
	// ahead; leave out to pause until the caller resumes.
	EndedAt *time.Time `json:"endedAt" graphapi:"nullable"`
}

type ListLocationPausesArguments struct {
	// UserID is whose pauses; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`

	// From and To keep the pauses that overlap the range.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// locationPausePageMost bounds the pauses one listing answers. A person
// pauses a few times a day at the most, so a range that holds more is a
// mistake in the range.
const locationPausePageMost = 500

// locationPauseReadMargin is added to the end of the range a report is read
// against, so that a position measured at the very instant a pause began is
// found inside it: a pause is in effect from its start.
const locationPauseReadMargin = time.Millisecond

func describeLocationPause(pause *models.LocationPause) *LocationPauseView {
	if pause == nil {
		return nil
	}
	return &LocationPauseView{ID: pause.ID, StartedAt: pause.StartedAt, EndedAt: pause.EndedAt}
}

func (self *graph) PauseLocation(ctx context.Context, arguments PauseLocationArguments) (*LocationPauseView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	userId := principal.User.ID

	// One change of a person's location at a time, as in ReportLocation: a
	// report that is being stored must see this pause or not, never half.
	if err := tx.LockLocation(userId); err != nil {
		return nil, err
	}
	pause, isStarted, err := tx.PauseLocation(userId, arguments.EndedAt, time.Now())
	if err != nil {
		return nil, locationFailure(err)
	}
	if isStarted {
		if err := endOpenVisitForPause(tx, userId, pause.StartedAt); err != nil {
			return nil, err
		}
	}
	// The people following this person on a map are told, so that the last
	// position they were shown is replaced by the pause. Nobody is told that
	// the person left the place: a pause says nothing about where they went.
	self.afterLocationChange(ctx, tx, userId, nil, nil)
	return describeLocationPause(pause), nil
}

// endOpenVisitForPause ends the visit in progress at the start of a pause, so
// that a viewer is never left seeing the person at a place for the length of
// it. A visit the positions say the person had left already ends where they
// say, as a check-in ends it.
func endOpenVisitForPause(tx db.Transaction, userId string, startedAt time.Time) error {
	open, err := tx.GetOpenPlaceVisit(userId)
	if err != nil || open == nil {
		return err
	}
	latest, err := tx.GetLatestLocationPoint(userId)
	if err != nil {
		return err
	}
	endedAt := startedAt
	if latest != nil && !place.IsStillThere(open, latest) && latest.MeasuredAt.Before(endedAt) {
		endedAt = latest.MeasuredAt
	}
	_, err = tx.EndOpenPlaceVisit(userId, endedAt)
	return err
}

// resumeIsThePersons is the refusal for a command the agent ran on the
// person's computer. Resuming records more than pausing did, so the agent
// resumes only through its own tool, whose card the person taps; the same
// command the person types is theirs and resumes.
const resumeIsThePersons = "resuming location recording is the person's own: they resume on the phone, the dashboard or the command line, or say yes to the agent's card"

func (self *graph) ResumeLocation(ctx context.Context) (bool, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return false, err
	}
	if request := api.ContextRequest(ctx); request != nil && request.Header.Get(api.AgentCommandHeader) != "" {
		return false, fmt.Errorf("%w: %s", api.ErrPermissionDenied, resumeIsThePersons)
	}
	tx := self.transaction(ctx)
	if err := tx.LockLocation(principal.User.ID); err != nil {
		return false, err
	}
	isResumed, err := tx.ResumeLocation(principal.User.ID, time.Now())
	if err != nil {
		return false, locationFailure(err)
	}
	if isResumed {
		self.afterLocationChange(ctx, tx, principal.User.ID, nil, nil)
	}
	return isResumed, nil
}

func (self *graph) ListLocationPauses(ctx context.Context, arguments ListLocationPausesArguments) ([]*LocationPauseView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	if !arguments.From.Before(arguments.To) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	pauses, err := self.transaction(ctx).ListLocationPauses(reach.User.ID, arguments.From, arguments.To, locationPausePageMost+1)
	if err != nil {
		return nil, locationFailure(err)
	}
	if len(pauses) > locationPausePageMost {
		return nil, fmt.Errorf("%w: that range holds more than %d pauses; ask for a shorter one", api.ErrInvalidArguments, locationPausePageMost)
	}
	views := make([]*LocationPauseView, 0, len(pauses))
	for _, pause := range pauses {
		views = append(views, describeLocationPause(pause))
	}
	return views, nil
}

// requireNotPaused refuses what would record where the person is while they
// have paused: a check-in is a visit measured now.
func requireNotPaused(tx db.Transaction, userId string, now time.Time) error {
	pause, err := tx.GetLocationPauseInEffect(userId, now)
	if err != nil || pause == nil {
		return err
	}
	if pause.EndedAt == nil {
		return fmt.Errorf("%w: your location is paused until you resume it", api.ErrInvalidArguments)
	}
	return fmt.Errorf("%w: your location is paused until %s", api.ErrInvalidArguments, pause.EndedAt.UTC().Format(time.RFC3339))
}

// pauseRangeStart is where to start reading the pauses a report is held
// against: its first time, or the arrival of a visit it completes when that
// is earlier, since a departure reported alone ends a visit that began before
// the report's times and a pause may lie between.
func (self *graph) pauseRangeStart(tx db.Transaction, userId string, from time.Time, visits []ReportedVisit) (time.Time, error) {
	for _, reported := range visits {
		if reported.ArrivedAt != nil {
			continue
		}
		known, err := tx.GetPlaceVisitByClientKey(userId, strings.TrimSpace(reported.ClientVisitKey))
		if err != nil {
			return from, err
		}
		if known != nil && known.ArrivedAt != nil && known.ArrivedAt.Before(from) {
			from = *known.ArrivedAt
		}
	}
	return from, nil
}

// locationPauseSet is the pauses of one person that overlap the times of a
// report, for dropping what was measured inside them.
type locationPauseSet []*models.LocationPause

// readLocationPauseSet reads the person's pauses that overlap [from, until],
// and the time since until up to now, since a visit with no departure runs on
// to now. One query.
func readLocationPauseSet(tx db.Transaction, userId string, from, until, now time.Time) (locationPauseSet, error) {
	if now.After(until) {
		until = now
	}
	pauses, err := tx.ListLocationPauses(userId, from, until.Add(locationPauseReadMargin), 0)
	return locationPauseSet(pauses), err
}

// covering is the pause in effect at the instant, or nil.
func (self locationPauseSet) covering(instant time.Time) *models.LocationPause {
	for _, pause := range self {
		if pause.IsInEffectAt(instant) {
			return pause
		}
	}
	return nil
}

// firstStartAfter is the pause that began soonest after the instant and, when
// notAfter is given, no later than it; nil when there is none.
func (self locationPauseSet) firstStartAfter(instant time.Time, notAfter *time.Time) *models.LocationPause {
	var first *models.LocationPause
	for _, pause := range self {
		if !pause.StartedAt.After(instant) || (notAfter != nil && pause.StartedAt.After(*notAfter)) {
			continue
		}
		if first == nil || pause.StartedAt.Before(first.StartedAt) {
			first = pause
		}
	}
	return first
}

// keptPoints are the positions of a report that were not measured inside a
// pause, and how many were.
func (self locationPauseSet) keptPoints(points []*models.LocationPoint) ([]*models.LocationPoint, int) {
	if len(self) == 0 {
		return points, 0
	}
	kept := make([]*models.LocationPoint, 0, len(points))
	for _, point := range points {
		if self.covering(point.MeasuredAt) == nil {
			kept = append(kept, point)
		}
	}
	return kept, len(points) - len(kept)
}

// clippedVisit is a reported visit with what falls inside a pause taken away.
type clippedVisit struct {
	ArrivedAt  *time.Time
	DepartedAt *time.Time

	// IsDropped says nothing of the visit was measured outside a pause.
	IsDropped bool

	// IsDepartureCut says the departure was moved to the start of a pause, so
	// that it is not told to anybody: the person did not say they left then.
	IsDepartureCut bool
}

// clipVisit takes from a reported visit what a pause hides. known is the visit
// stored under the same key, which the report completes, or nil.
//
//   - An arrival inside a pause is not kept. With no departure after the pause
//     either, nothing is left of the visit.
//   - A departure inside a pause, of a visit that arrived before it, becomes the
//     start of the pause: the visit keeps its arrival and ends there. A
//     departure inside a pause with no arrival known is not kept, since the
//     place it was reported at is where the person was during the pause.
//   - A visit that arrived before a pause began and was not over by then ends at
//     the start of the pause, so that no visit runs across a gap whose
//     whereabouts are unknown. One that was already over is left alone.
func (self locationPauseSet) clipVisit(reportedArrivedAt, reportedDepartedAt *time.Time, known *models.PlaceVisit) clippedVisit {
	clipped := clippedVisit{ArrivedAt: reportedArrivedAt, DepartedAt: reportedDepartedAt}
	if len(self) == 0 {
		return clipped
	}
	if reportedArrivedAt != nil && self.covering(*reportedArrivedAt) != nil {
		clipped.ArrivedAt = nil
		if reportedDepartedAt == nil || self.covering(*reportedDepartedAt) != nil {
			clipped.IsDropped = true
		}
		return clipped
	}
	startedAt := reportedArrivedAt
	if startedAt == nil && known != nil {
		startedAt = known.ArrivedAt
	}
	if reportedDepartedAt != nil {
		if pause := self.covering(*reportedDepartedAt); pause != nil {
			if startedAt == nil {
				clipped.IsDropped = true
				return clipped
			}
			cutAt := pause.StartedAt
			clipped.DepartedAt, clipped.IsDepartureCut = &cutAt, true
			return clipped
		}
	}
	if startedAt == nil {
		return clipped
	}
	// A report that adds no departure to a visit that has one stored changes
	// nothing about when it ended.
	if reportedDepartedAt == nil && known != nil && known.DepartedAt != nil {
		return clipped
	}
	if pause := self.firstStartAfter(*startedAt, reportedDepartedAt); pause != nil {
		cutAt := pause.StartedAt
		clipped.DepartedAt, clipped.IsDepartureCut = &cutAt, true
	}
	return clipped
}
