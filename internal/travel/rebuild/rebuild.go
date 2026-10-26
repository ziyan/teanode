// Package rebuild stores a person's trips: it reads the visits, positions and
// motion activity of some days from the database, has package travel build
// the trips of those days from them, and replaces the trips stored for those
// days. Trips are derived and can be rebuilt at any time; what the person
// said about how they traveled is kept apart, as corrections, and is not
// touched here.
package rebuild

import (
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/travel"
)

const (
	// paddingBefore and paddingAfter widen the times a change touched to
	// the trips that can depend on them: a position or a visit changes the
	// trip it lies in, which began at the departure before it, and a late
	// departure ends a visit that began a day or more earlier.
	paddingBefore = 24 * time.Hour
	paddingAfter  = 48 * time.Hour

	// readMargin is how much further than the trips rebuilt the visits are
	// read, so that a trip that starts inside the window finds the visit it
	// ends at, and the one it starts from.
	readMargin = 24 * time.Hour

	// pointsMost bounds the positions read for one rebuild. A phone in use
	// records at most one every few seconds, and the window is a few days.
	pointsMost = 100000
)

// Around rebuilds the trips that the times from and until can have changed.
// A caller that stored positions, visits or motion activity between them
// calls it in the same transaction, holding the person's location lock.
func Around(tx db.Transaction, userId string, from, until time.Time) error {
	return Window(tx, userId, from.Add(-paddingBefore), until.Add(paddingAfter))
}

// Window rebuilds the trips that start at or after windowStart and before
// windowEnd: they are deleted and built again from the visits whose
// departure lies in the window.
func Window(tx db.Transaction, userId string, windowStart, windowEnd time.Time) error {
	if !windowStart.Before(windowEnd) {
		return nil
	}
	readStart, readEnd := windowStart.Add(-readMargin), windowEnd.Add(readMargin)
	stays, err := staysBetween(tx, userId, readStart, readEnd)
	if err != nil {
		return err
	}
	points, err := tx.ListLocationPoints(userId, readStart, readEnd, pointsMost)
	if err != nil {
		return err
	}
	positions := make([]travel.Position, 0, len(points))
	for _, point := range points {
		positions = append(positions, travel.Position{
			MeasuredAt: point.MeasuredAt, LatitudeDegrees: point.LatitudeDegrees, LongitudeDegrees: point.LongitudeDegrees, AccuracyMeters: point.AccuracyMeters,
		})
	}
	motionActivities, err := tx.ListMotionActivities(userId, readStart, readEnd)
	if err != nil {
		return err
	}
	activities := make([]travel.Activity, 0, len(motionActivities))
	for _, motionActivity := range motionActivities {
		activities = append(activities, travel.Activity{
			StartedAt: motionActivity.StartedAt, ActivityKind: motionActivity.ActivityKind, ActivityConfidence: motionActivity.ActivityConfidence,
		})
	}
	var trips []*models.Trip
	for _, built := range travel.BuildTrips(stays, positions, activities) {
		if built.StartedAt.Before(windowStart) || !built.StartedAt.Before(windowEnd) {
			continue
		}
		trip := &models.Trip{
			UserID: userId, StartedAt: built.StartedAt, EndedAt: built.EndedAt,
			FromVisitID: built.FromVisitID, ToVisitID: built.ToVisitID, DistanceMeters: built.DistanceMeters,
		}
		for _, leg := range built.Legs {
			trip.Legs = append(trip.Legs, &models.TripLeg{
				UserID: userId, StartedAt: leg.StartedAt, EndedAt: leg.EndedAt,
				DetectedTravelMode: string(leg.DetectedMode), DetectionSource: leg.DetectionSource, DistanceMeters: leg.DistanceMeters,
			})
		}
		trips = append(trips, trip)
	}
	return tx.ReplaceTrips(userId, windowStart, windowEnd, trips)
}

// staysBetween reads the person's visits that overlap the range, every page
// of them.
func staysBetween(tx db.Transaction, userId string, from, until time.Time) ([]travel.Stay, error) {
	var stays []travel.Stay
	for offset := 0; ; {
		visits, nextOffset, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{From: &from, Until: &until, Offset: offset, LimitCount: db.PlacePageMost})
		if err != nil {
			return nil, err
		}
		for _, visit := range visits {
			stays = append(stays, travel.Stay{
				VisitID: visit.ID, ArrivedAt: visit.ArrivedAt, DepartedAt: visit.DepartedAt,
				LatitudeDegrees: visit.LatitudeDegrees, LongitudeDegrees: visit.LongitudeDegrees,
			})
		}
		if nextOffset <= 0 || len(visits) == 0 {
			return stays, nil
		}
		offset = nextOffset
	}
}
