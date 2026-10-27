package rebuild

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// visitsForTest stores home, at the origin, and an office two kilometers
// north, and a visit to each: the person is at home from arrivedHome to
// leftHome and at the office from reachedOffice on. Motion activity says they
// walked from leftHome, so that the trip is built however long the phone was
// silent on the way.
func visitsForTest(t *testing.T, tx db.Transaction, userId string, arrivedHome, leftHome, reachedOffice time.Time) {
	t.Helper()
	home, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Home", IsNamedByPerson: true, LatitudeDegrees: 40, LongitudeDegrees: -75, RadiusMeters: 100})
	if err != nil {
		t.Fatal(err)
	}
	office, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Office", IsNamedByPerson: true, LatitudeDegrees: 40.02, LongitudeDegrees: -75, RadiusMeters: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, visit := range []*models.PlaceVisit{
		{PlaceID: home.ID, ClientVisitKey: "home", VisitSource: models.VisitSourceVisit, ArrivedAt: &arrivedHome, DepartedAt: &leftHome, LatitudeDegrees: 40, LongitudeDegrees: -75},
		{PlaceID: office.ID, ClientVisitKey: "office", VisitSource: models.VisitSourceVisit, ArrivedAt: &reachedOffice, LatitudeDegrees: 40.02, LongitudeDegrees: -75},
	} {
		if _, err := tx.PutPlaceVisit(userId, visit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.PutMotionActivities(userId, []*models.MotionActivity{{StartedAt: leftHome, ActivityKind: "walking", ActivityConfidence: "high"}}); err != nil {
		t.Fatal(err)
	}
}

func tripsOf(t *testing.T, tx db.Transaction, userId string, from, until time.Time) []*models.Trip {
	t.Helper()
	trips, _, err := tx.ListTrips(userId, from, until, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return trips
}

// The person left home and their phone said nothing for 30 hours: the report
// of the arrival still builds the trip, whose departure is more than a day
// before the arrival.
func TestAroundBuildsATripThatBeganMoreThanADayBefore(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userId := dbtest.CreateUser(t, database, "rebuild-long-gap")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		reached := time.Now().Add(-time.Hour).Truncate(time.Second)
		left := reached.Add(-30 * time.Hour)
		visitsForTest(t, tx, userId, left.Add(-10*time.Hour), left, reached)
		if err := Around(tx, userId, reached, reached); err != nil {
			t.Fatal(err)
		}
		if trips := tripsOf(t, tx, userId, left.Add(-time.Hour), reached.Add(time.Hour)); len(trips) != 1 || !trips[0].StartedAt.Equal(left) {
			t.Fatalf("a trip from the departure 30 hours before: %+v", trips)
		}
	})
}

// A trip that starts inside the window and arrives after the visits are
// usually read to is built, not deleted.
func TestWindowBuildsATripThatArrivesPastTheReadRange(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userId := dbtest.CreateUser(t, database, "rebuild-late-arrival")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		left := time.Now().Add(-10 * 24 * time.Hour).Truncate(time.Second)
		reached := left.Add(72 * time.Hour)
		visitsForTest(t, tx, userId, left.Add(-10*time.Hour), left, reached)
		if err := Window(tx, userId, left.Add(-time.Hour), left.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if trips := tripsOf(t, tx, userId, left.Add(-time.Hour), reached.Add(time.Hour)); len(trips) != 1 || !trips[0].EndedAt.Equal(reached) {
			t.Fatalf("a trip that arrives three days later: %+v", trips)
		}
	})
}

// Positions are read in pages, every one of them.
func TestPositionsAreReadInPagesWithoutLeavingAnyOut(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userId := dbtest.CreateUser(t, database, "rebuild-pages")
	previous := pointsPageSize
	pointsPageSize = 2
	defer func() { pointsPageSize = previous }()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		start := time.Now().Add(-time.Hour).Truncate(time.Second)
		var points []*models.LocationPoint
		for index := 0; index < 5; index++ {
			points = append(points, &models.LocationPoint{
				LatitudeDegrees: 40, LongitudeDegrees: -75, AccuracyMeters: 5, MeasuredAt: start.Add(time.Duration(index) * time.Minute),
				ReceivedAt: start, LocationSource: models.LocationSourcePhone,
			})
		}
		if _, err := tx.InsertLocationPoints(userId, points); err != nil {
			t.Fatal(err)
		}
		positions, err := positionsBetween(tx, userId, start.Add(-time.Hour), start.Add(time.Hour))
		if err != nil || len(positions) != 5 {
			t.Fatalf("all five positions, not a page: %d %v", len(positions), err)
		}
	})
}

// Two positions measured at the same instant by two sources, where a page
// ends between them, are both read, and none twice.
func TestPositionsSharingAnInstantAtAPageEndAreAllRead(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userId := dbtest.CreateUser(t, database, "rebuild-page-instant")
	previous := pointsPageSize
	pointsPageSize = 2
	defer func() { pointsPageSize = previous }()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		start := time.Now().Add(-time.Hour).Truncate(time.Second)
		point := func(minute int, source models.LocationSource) *models.LocationPoint {
			return &models.LocationPoint{
				LatitudeDegrees: 40, LongitudeDegrees: -75, AccuracyMeters: 5, MeasuredAt: start.Add(time.Duration(minute) * time.Minute),
				ReceivedAt: start, LocationSource: source,
			}
		}
		points := []*models.LocationPoint{
			point(0, models.LocationSourcePhone), point(1, models.LocationSourcePhone), point(1, models.LocationSourceBrowser), point(2, models.LocationSourcePhone),
		}
		if _, err := tx.InsertLocationPoints(userId, points); err != nil {
			t.Fatal(err)
		}
		positions, err := positionsBetween(tx, userId, start.Add(-time.Hour), start.Add(time.Hour))
		if err != nil || len(positions) != 4 {
			t.Fatalf("all four positions once each: %d %v", len(positions), err)
		}
	})
}

// Times a long way apart are rebuilt apart: the days between are not.
func TestAroundTimesRebuildsEachRunAndNotTheDaysBetween(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userId := dbtest.CreateUser(t, database, "rebuild-runs")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		day := 24 * time.Hour
		now := time.Now().Truncate(time.Second)
		// A stored trip ten days ago, with no visits behind it: a rebuild
		// that reaches it deletes it.
		between := now.Add(-10 * day)
		if err := tx.ReplaceTrips(userId, between.Add(-time.Hour), between.Add(time.Hour), []*models.Trip{{
			StartedAt: between, EndedAt: between.Add(10 * time.Minute), DistanceMeters: 1000,
			Legs: []*models.TripLeg{{StartedAt: between, EndedAt: between.Add(10 * time.Minute), DetectedTravelMode: "car", DetectionSource: "speed", DistanceMeters: 1000}},
		}}); err != nil {
			t.Fatal(err)
		}
		times := []time.Time{now.Add(-20 * day), now.Add(-20*day + time.Hour), now.Add(-time.Hour)}
		if err := AroundTimes(tx, userId, times); err != nil {
			t.Fatal(err)
		}
		if trips := tripsOf(t, tx, userId, between.Add(-time.Hour), between.Add(time.Hour)); len(trips) != 1 {
			t.Fatalf("the trip between the runs stays: %+v", trips)
		}
		// Times close together are one run, which reaches it.
		if err := AroundTimes(tx, userId, []time.Time{between.Add(-time.Hour), between.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if trips := tripsOf(t, tx, userId, between.Add(-time.Hour), between.Add(time.Hour)); len(trips) != 0 {
			t.Fatalf("a run around it rebuilds it, and there are no visits: %+v", trips)
		}
	})
}

// Trips a day apart for a month, as a forgotten place visited daily leaves,
// are rebuilt a week at a time, not in one window of a month.
func TestRunsOfDailyTimesLastAWeekAtMost(t *testing.T) {
	day := 24 * time.Hour
	start := time.Date(2030, 3, 4, 8, 0, 0, 0, time.UTC)
	var times []time.Time
	for index := 29; index >= 0; index-- {
		times = append(times, start.Add(time.Duration(index)*day))
	}
	runs := runsOf(times)
	if len(runs) != 4 {
		t.Fatalf("got %d runs, want 4: %+v", len(runs), runs)
	}
	for index, run := range runs {
		if run.until.Sub(run.from) > RunLengthMost {
			t.Fatalf("run %d lasts %s", index, run.until.Sub(run.from))
		}
		if index > 0 && !run.from.Equal(runs[index-1].until.Add(day)) {
			t.Fatalf("run %d starts at %s, after %s", index, run.from, runs[index-1].until)
		}
	}
	if !runs[0].from.Equal(start) || !runs[3].until.Equal(start.Add(29*day)) {
		t.Fatalf("runs %+v do not cover the times", runs)
	}
	if got := runsOf([]time.Time{start, start.Add(3 * day)}); len(got) != 2 {
		t.Fatalf("a gap of three days splits the run: %+v", got)
	}
}
