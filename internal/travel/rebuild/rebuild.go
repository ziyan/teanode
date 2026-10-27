// Package rebuild stores a person's trips: it reads the visits, positions and
// motion activity of some days from the database, has package travel build
// the trips of those days from them, and replaces the trips stored for those
// days. Trips are derived and can be rebuilt at any time; what the person
// said about how they traveled is kept apart, as corrections, and is not
// touched here.
package rebuild

import (
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/travel"
)

const (
	// defaultLookBack is how far before the times a change touched the
	// window starts when the person has no visit that departed before them.
	defaultLookBack = 24 * time.Hour

	// lookAhead is how far after the times a change touched the window
	// ends. A position, a visit or a motion activity changes only the trips
	// that start at or before its time, except through the visits the
	// report itself carries, and those are among the times given.
	lookAhead = 6 * time.Hour

	// readMargin is how much further than the trips rebuilt the visits are
	// read, so that a trip that starts inside the window finds the visit it
	// ends at, and the one it starts from.
	readMargin = 24 * time.Hour

	// arrivalLookAhead is how far after the window the arrival that ends its
	// last trip is looked for. No trip lasts longer (db.TripLongestDuration).
	arrivalLookAhead = db.TripLongestDuration

	// RunGapMost is the longest gap between two times of one change that
	// still make them one window; across a longer one they are rebuilt
	// apart, so that a report holding two distant days does not rebuild all
	// the days between.
	RunGapMost = 48 * time.Hour

	// RunLengthMost is the longest a run of times rebuilt as one window
	// lasts. Longer runs are split, so that trips a day apart for months
	// (a place visited daily, then forgotten) are not read in one window.
	RunLengthMost = 7 * 24 * time.Hour
)

// pointsPageSize is how many positions are read at a time; a variable so that
// a test can make it small.
var pointsPageSize = 10000

// Around rebuilds the trips that the times from and until can have changed.
// A caller that stored positions, visits or motion activity between them
// calls it in the same transaction, holding the person's location lock. The
// window starts at the departure of the person's latest visit that departed
// before from (or a day before from when there is none): a trip that began
// before then and was not built, because the person's phone was off, ends in
// the visit this change brings.
//
// When from falls inside a visit that began before it, the person was staying
// put at from, so nothing the change brings belongs to a trip that ended at
// that visit's arrival: the window starts at that arrival, and a report
// during a long stay reads only the stay. No window starts more than the
// longest trip before from.
func Around(tx db.Transaction, userId string, from, until time.Time) error {
	windowStart := from.Add(-defaultLookBack)
	staying, _, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{From: &from, Until: &from, LimitCount: 1})
	if err != nil {
		return err
	}
	if len(staying) > 0 && staying[0].ArrivedAt != nil && staying[0].ArrivedAt.Before(from) {
		windowStart = *staying[0].ArrivedAt
	} else {
		departure, err := tx.GetLatestVisitDeparture(userId, from)
		if err != nil {
			return err
		}
		if departure != nil {
			windowStart = *departure
		}
	}
	if earliest := from.Add(-travel.MaximumTripDuration); windowStart.Before(earliest) {
		windowStart = earliest
	}
	return Window(tx, userId, windowStart, until.Add(lookAhead))
}

// AroundTimes rebuilds around each run of the times (see runsOf).
func AroundTimes(tx db.Transaction, userId string, times []time.Time) error {
	for _, run := range runsOf(times) {
		if err := Around(tx, userId, run.from, run.until); err != nil {
			return err
		}
	}
	return nil
}

// timeRun is the first and last of a run of times.
type timeRun struct {
	from, until time.Time
}

// runsOf groups the times into runs, in order: times with no gap of more than
// RunGapMost between them, a run lasting no more than RunLengthMost.
func runsOf(times []time.Time) []timeRun {
	if len(times) == 0 {
		return nil
	}
	times = append([]time.Time(nil), times...)
	sort.Slice(times, func(first, second int) bool { return times[first].Before(times[second]) })
	runs := []timeRun{{from: times[0], until: times[0]}}
	for _, at := range times[1:] {
		current := &runs[len(runs)-1]
		if at.Sub(current.until) > RunGapMost || at.Sub(current.from) > RunLengthMost {
			runs = append(runs, timeRun{from: at, until: at})
			continue
		}
		current.until = at
	}
	return runs
}

// Window rebuilds the trips that start at or after windowStart and before
// windowEnd: they are deleted and built again from the visits whose
// departure lies in the window.
func Window(tx db.Transaction, userId string, windowStart, windowEnd time.Time) error {
	if !windowStart.Before(windowEnd) {
		return nil
	}
	readStart, readEnd := windowStart.Add(-readMargin), windowEnd.Add(readMargin)
	// A trip that starts near the window's end arrives later than it: read up
	// to the first arrival after the window, or its last trip is deleted
	// without being built again.
	arrival, err := tx.GetFirstVisitArrival(userId, windowEnd, windowEnd.Add(arrivalLookAhead))
	if err != nil {
		return err
	}
	if arrival != nil && arrival.Add(time.Second).After(readEnd) {
		readEnd = arrival.Add(time.Second)
	}
	visits, err := visitsBetween(tx, userId, readStart, readEnd)
	if err != nil {
		return err
	}
	positions, err := positionsBetween(tx, userId, readStart, readEnd)
	if err != nil {
		return err
	}
	motionActivities, err := tx.ListMotionActivities(userId, readStart, readEnd)
	if err != nil {
		return err
	}
	activities := make([]travel.MotionActivity, 0, len(motionActivities))
	for _, motionActivity := range motionActivities {
		activities = append(activities, travel.MotionActivity{
			StartedAt: motionActivity.StartedAt, ActivityKind: motionActivity.ActivityKind, ActivityConfidence: motionActivity.ActivityConfidence,
		})
	}
	var trips []*models.Trip
	for _, built := range travel.BuildTrips(visits, positions, activities) {
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
				DetectedTravelMode: string(leg.DetectedTravelMode), DetectionSource: leg.DetectionSource, DistanceMeters: leg.DistanceMeters,
			})
		}
		trips = append(trips, trip)
	}
	return tx.ReplaceTrips(userId, windowStart, windowEnd, trips)
}

// positionsBetween reads every position measured in the range, a page at a
// time, oldest first: none is left out for there being many. A page starts at
// the instant the one before ended at, since positions from two sources can
// share an instant, and the ones already read there are skipped.
func positionsBetween(tx db.Transaction, userId string, from, until time.Time) ([]travel.Position, error) {
	var positions []travel.Position
	readAtBoundary := map[string]bool{}
	for pageFrom := from; pageFrom.Before(until); {
		points, err := tx.ListLocationPoints(userId, pageFrom, until, pointsPageSize)
		if err != nil {
			return nil, err
		}
		for _, point := range points {
			if readAtBoundary[point.ID] {
				continue
			}
			positions = append(positions, travel.Position{
				MeasuredAt: point.MeasuredAt, LatitudeDegrees: point.LatitudeDegrees, LongitudeDegrees: point.LongitudeDegrees, AccuracyMeters: point.AccuracyMeters,
			})
		}
		if len(points) < pointsPageSize {
			break
		}
		last := points[len(points)-1].MeasuredAt
		if last.Equal(pageFrom) {
			// A whole page at one instant: there is no telling the rest
			// apart, so the next page starts after it.
			pageFrom = last.Add(time.Microsecond)
			continue
		}
		pageFrom = last
		readAtBoundary = map[string]bool{}
		for _, point := range points {
			if point.MeasuredAt.Equal(last) {
				readAtBoundary[point.ID] = true
			}
		}
	}
	return positions, nil
}

// visitsBetween reads the person's visits that overlap the range, every page
// of them.
func visitsBetween(tx db.Transaction, userId string, from, until time.Time) ([]travel.Visit, error) {
	var visits []travel.Visit
	for offset := 0; ; {
		placeVisits, nextOffset, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{From: &from, Until: &until, Offset: offset, LimitCount: db.PlacePageMost})
		if err != nil {
			return nil, err
		}
		for _, placeVisit := range placeVisits {
			visits = append(visits, travel.Visit{
				VisitID: placeVisit.ID, ArrivedAt: placeVisit.ArrivedAt, DepartedAt: placeVisit.DepartedAt,
				LatitudeDegrees: placeVisit.LatitudeDegrees, LongitudeDegrees: placeVisit.LongitudeDegrees,
			})
		}
		if nextOffset <= 0 || len(placeVisits) == 0 {
			return visits, nil
		}
		offset = nextOffset
	}
}
