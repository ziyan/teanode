package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/travel"
)

// How a person got from one visit to the next. docs/subsystems/location.md
// says how trips are built. They are derived from the visits, positions and
// motion activity a phone reports and are built again whenever those change
// (package travel/rebuild); what the person says about how they traveled is
// kept apart, as corrections over spans of time, so that a rebuild keeps it.
// Trips are seen at the history level, like the day's path; only the person
// corrects their own.

// TripView is the travel between two visits.
type TripView struct {
	ID              string    `json:"id"`
	StartedAt       time.Time `json:"startedAt"`
	EndedAt         time.Time `json:"endedAt"`
	DurationSeconds int64     `json:"durationSeconds"`
	DistanceMeters  float64   `json:"distanceMeters"`

	// FromPlace and ToPlace are the places of the visits the trip left and
	// reached, null when a visit is gone.
	FromPlace *PlaceView `json:"fromPlace"`
	ToPlace   *PlaceView `json:"toPlace"`

	// Legs are the parts of the trip, each with one way of travel, in order.
	Legs []*TripLegView `json:"legs"`
}

// TripLegView is a part of a trip with one way of travel.
type TripLegView struct {
	ID              string    `json:"id"`
	StartedAt       time.Time `json:"startedAt"`
	EndedAt         time.Time `json:"endedAt"`
	DurationSeconds int64     `json:"durationSeconds"`
	DistanceMeters  float64   `json:"distanceMeters"`

	// TravelMode is the way of travel: the person's correction when one
	// covers the leg, otherwise DetectedTravelMode. One of walk, run, bike,
	// car, bus, train, boat, plane.
	TravelMode            string `json:"travelMode"`
	DetectedTravelMode    string `json:"detectedTravelMode"`
	IsTravelModeCorrected bool   `json:"isTravelModeCorrected"`

	// DetectionSource is what the detected way came from: motion (the
	// phone's motion activity) or speed (distance over time alone).
	DetectionSource string `json:"detectionSource"`
}

// TripPageView is a page of trips, newest first.
type TripPageView struct {
	Trips      []*TripView `json:"trips"`
	NextOffset *int        `json:"nextOffset"`
}

// TravelTotalView is how far a person traveled one way in one period.
type TravelTotalView struct {
	// PeriodStartDay is the first day of the period, as YYYY-MM-DD, in the
	// traveler's time zone.
	PeriodStartDay  string  `json:"periodStartDay"`
	TravelMode      string  `json:"travelMode"`
	DistanceMeters  float64 `json:"distanceMeters"`
	DurationSeconds int64   `json:"durationSeconds"`
	LegCount        int     `json:"legCount"`
}

type ListTripsArguments struct {
	// UserID is whose trips; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`

	// From and Until keep the trips that overlap the range.
	From  time.Time `json:"from"`
	Until time.Time `json:"until"`

	Offset     *int `json:"offset" graphapi:"nullable"`
	LimitCount *int `json:"limitCount" graphapi:"nullable"`
}

type SetTripLegTravelModeArguments struct {
	TripLegID string `json:"tripLegId"`

	// TravelMode is one of walk, run, bike, car, bus, train, boat, plane;
	// leave out to take back the correction, so the detected way counts.
	TravelMode *string `json:"travelMode" graphapi:"nullable"`
}

type ListTravelTotalsArguments struct {
	// UserID is whose travel; empty is the caller's own.
	UserID string `json:"userId" graphapi:"nullable"`

	// Period is day, week (from Monday), month or year, in the traveler's
	// time zone. A leg counts in the period it started in.
	Period string `json:"period"`

	From  time.Time `json:"from"`
	Until time.Time `json:"until"`
}

func describeTripLeg(leg *models.TripLeg) *TripLegView {
	return &TripLegView{
		ID: leg.ID, StartedAt: leg.StartedAt, EndedAt: leg.EndedAt,
		DurationSeconds: int64(leg.EndedAt.Sub(leg.StartedAt).Seconds()), DistanceMeters: leg.DistanceMeters,
		TravelMode: leg.TravelMode, DetectedTravelMode: leg.DetectedTravelMode, IsTravelModeCorrected: leg.IsTravelModeCorrected,
		DetectionSource: leg.DetectionSource,
	}
}

func describeTrip(trip *models.Trip) *TripView {
	view := &TripView{
		ID: trip.ID, StartedAt: trip.StartedAt, EndedAt: trip.EndedAt,
		DurationSeconds: int64(trip.EndedAt.Sub(trip.StartedAt).Seconds()), DistanceMeters: trip.DistanceMeters,
		Legs: make([]*TripLegView, 0, len(trip.Legs)),
	}
	for _, leg := range trip.Legs {
		view.Legs = append(view.Legs, describeTripLeg(leg))
	}
	return view
}

func (self *graph) ListTrips(ctx context.Context, arguments ListTripsArguments) (*TripPageView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	if !arguments.From.Before(arguments.Until) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	offset, limitCount, err := pageOf(arguments.Offset, arguments.LimitCount, db.PlacePageDefault, db.PlacePageMost)
	if err != nil {
		return nil, err
	}
	trips, nextOffset, err := self.transaction(ctx).ListTrips(reach.User.ID, arguments.From, arguments.Until, offset, limitCount)
	if err != nil {
		return nil, locationFailure(err)
	}
	// The places at each end, read for the whole page at once.
	visitIds := make([]string, 0, 2*len(trips))
	for _, trip := range trips {
		visitIds = append(visitIds, trip.FromVisitID, trip.ToVisitID)
	}
	places, err := self.transaction(ctx).ListPlacesOfVisits(reach.User.ID, visitIds)
	if err != nil {
		return nil, err
	}
	page := &TripPageView{Trips: make([]*TripView, 0, len(trips)), NextOffset: nextOffset}
	for _, trip := range trips {
		view := describeTrip(trip)
		if place, ok := places[trip.FromVisitID]; ok {
			view.FromPlace = describePlace(place)
		}
		if place, ok := places[trip.ToVisitID]; ok {
			view.ToPlace = describePlace(place)
		}
		page.Trips = append(page.Trips, view)
	}
	return page, nil
}

func (self *graph) SetTripLegTravelMode(ctx context.Context, arguments SetTripLegTravelModeArguments) (*TripLegView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	var travelMode *string
	if arguments.TravelMode != nil && strings.TrimSpace(*arguments.TravelMode) != "" {
		mode := strings.ToLower(strings.TrimSpace(*arguments.TravelMode))
		if !travel.IsMode(mode) {
			return nil, fmt.Errorf("%w: a way of travel is one of walk, run, bike, car, bus, train, boat, plane", api.ErrInvalidArguments)
		}
		travelMode = &mode
	}
	tx := self.transaction(ctx)
	userId := principal.User.ID
	// Only the person's own legs: somebody else's is not found, as if there
	// were none.
	leg, err := tx.GetTripLeg(userId, strings.TrimSpace(arguments.TripLegID))
	if err != nil {
		return nil, err
	}
	if leg == nil {
		return nil, api.ErrNotFound
	}
	if err := tx.SetTravelModeCorrection(userId, leg.StartedAt, leg.EndedAt, travelMode); err != nil {
		return nil, locationFailure(err)
	}
	if leg, err = tx.GetTripLeg(userId, leg.ID); err != nil || leg == nil {
		return nil, err
	}
	self.afterLocationChange(ctx, tx, userId, nil, nil)
	return describeTripLeg(leg), nil
}

// travelTotalPeriods are the periods ListTravelTotals groups by.
var travelTotalPeriods = []string{"day", "week", "month", "year"}

func (self *graph) ListTravelTotals(ctx context.Context, arguments ListTravelTotalsArguments) ([]*TravelTotalView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelHistory)
	if err != nil {
		return nil, err
	}
	period := strings.ToLower(strings.TrimSpace(arguments.Period))
	isPeriod := false
	for _, each := range travelTotalPeriods {
		isPeriod = isPeriod || each == period
	}
	if !isPeriod {
		return nil, fmt.Errorf("%w: the period is day, week, month or year", api.ErrInvalidArguments)
	}
	if !arguments.From.Before(arguments.Until) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	totals, err := self.transaction(ctx).ListTravelTotals(reach.User.ID, agent.Location(reach.User), period, arguments.From, arguments.Until)
	if err != nil {
		return nil, locationFailure(err)
	}
	views := make([]*TravelTotalView, 0, len(totals))
	for _, total := range totals {
		views = append(views, &TravelTotalView{
			PeriodStartDay: total.PeriodStartDay, TravelMode: total.TravelMode, DistanceMeters: total.DistanceMeters,
			DurationSeconds: total.DurationSeconds, LegCount: total.LegCount,
		})
	}
	return views, nil
}
