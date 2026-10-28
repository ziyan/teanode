package server

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Days recorded before trips were built get their trips at start, weeks
// apart included, and a second pass finds nobody.
func TestBackfillTripsBuildsThePastTripsOnce(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "trip-backfill")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		home, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Home", IsNamedByPerson: true, LatitudeDegrees: 40, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		park, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Park", IsNamedByPerson: true, LatitudeDegrees: 40.02, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		// Two mornings five weeks apart: home, then the park two kilometers
		// away half an hour later.
		for index, daysAgo := range []int{40, 5} {
			start := time.Now().AddDate(0, 0, -daysAgo).Truncate(time.Second)
			left, reached := start.Add(time.Hour), start.Add(90*time.Minute)
			for _, visit := range []*models.PlaceVisit{
				{PlaceID: home.ID, ClientVisitKey: "home-" + string(rune('a'+index)), VisitSource: models.VisitSourceVisit, ArrivedAt: &start, DepartedAt: &left, LatitudeDegrees: 40, LongitudeDegrees: -75},
				{PlaceID: park.ID, ClientVisitKey: "park-" + string(rune('a'+index)), VisitSource: models.VisitSourceVisit, ArrivedAt: &reached, LatitudeDegrees: 40.02, LongitudeDegrees: -75},
			} {
				if _, err := transaction.PutPlaceVisit(userId, visit); err != nil {
					test.Fatal(err)
				}
			}
		}
	})

	backfillTrips(context.Background(), database)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		trips, _, err := transaction.ListTrips(userId, time.Now().AddDate(0, 0, -60), time.Now(), 0, 10)
		if err != nil || len(trips) != 2 {
			test.Fatalf("both mornings have a trip: %d %v", len(trips), err)
		}
		if userIds, err := transaction.ListUsersNeedingTripBackfill(tripRulesChangedAt); err != nil || len(userIds) != 0 {
			test.Errorf("a second pass finds %v %v", userIds, err)
		}
	})
}

// After the rules change, a person whose trips were built under the old ones
// has them built again, and only once.
func TestBackfillTripsBuildsAgainAfterTheRulesChange(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "trip-backfill-rules")
	start := time.Now().AddDate(0, 0, -3).Truncate(time.Second)
	left, reached := start.Add(time.Hour), start.Add(90*time.Minute)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		home, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Home", IsNamedByPerson: true, LatitudeDegrees: 40, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		park, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Park", IsNamedByPerson: true, LatitudeDegrees: 40.02, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		for _, visit := range []*models.PlaceVisit{
			{PlaceID: home.ID, ClientVisitKey: "home", VisitSource: models.VisitSourceVisit, ArrivedAt: &start, DepartedAt: &left, LatitudeDegrees: 40, LongitudeDegrees: -75},
			{PlaceID: park.ID, ClientVisitKey: "park", VisitSource: models.VisitSourceVisit, ArrivedAt: &reached, LatitudeDegrees: 40.02, LongitudeDegrees: -75},
		} {
			if _, err := transaction.PutPlaceVisit(userId, visit); err != nil {
				test.Fatal(err)
			}
		}
		// The trip as older rules built it, and marked done before they changed.
		if err := transaction.ReplaceTrips(userId, left, reached, []*models.Trip{{
			StartedAt: left, EndedAt: reached, DistanceMeters: 2224,
			Legs: []*models.TripLeg{{StartedAt: left, EndedAt: reached, DetectedTravelMode: "plane", DetectionSource: "speed", DistanceMeters: 2224}},
		}}); err != nil {
			test.Fatal(err)
		}
		if err := transaction.MarkTripsBackfilled(userId, time.Now().Add(-time.Hour)); err != nil {
			test.Fatal(err)
		}
	})
	rulesChangedAt := tripRulesChangedAt
	tripRulesChangedAt = time.Now().Add(-time.Minute)
	test.Cleanup(func() { tripRulesChangedAt = rulesChangedAt })

	backfillTrips(context.Background(), database)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		trips, _, err := transaction.ListTrips(userId, start, reached.Add(time.Hour), 0, 10)
		if err != nil || len(trips) != 1 || len(trips[0].Legs) != 1 || trips[0].Legs[0].DetectedTravelMode != "walk" {
			test.Fatalf("the trip built again is a walk: %+v %v", trips, err)
		}
		if userIds, err := transaction.ListUsersNeedingTripBackfill(tripRulesChangedAt); err != nil || len(userIds) != 0 {
			test.Errorf("a second pass finds %v %v", userIds, err)
		}
	})
}

// A start while another instance is on the pass does nothing: it finds the
// lock held and leaves the work to that instance, which builds them once.
func TestBackfillTripsLeavesThePassToTheInstanceHoldingIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "trip-backfill-held")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		home, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Home", IsNamedByPerson: true, LatitudeDegrees: 40, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		start := time.Now().AddDate(0, 0, -2).Truncate(time.Second)
		if _, err := transaction.PutPlaceVisit(userId, &models.PlaceVisit{PlaceID: home.ID, ClientVisitKey: "home", VisitSource: models.VisitSourceVisit, ArrivedAt: &start, LatitudeDegrees: 40, LongitudeDegrees: -75}); err != nil {
			test.Fatal(err)
		}
	})

	release, isLocked, err := database.TryLockSession(context.Background(), db.LockKey("trip:backfill"))
	if err != nil || !isLocked {
		test.Fatalf("the lock was not free: %v %v", isLocked, err)
	}
	backfillTrips(context.Background(), database)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if userIds, err := transaction.ListUsersNeedingTripBackfill(tripRulesChangedAt); err != nil || len(userIds) != 1 {
			test.Errorf("the pass ran while another instance held the lock: %v %v", userIds, err)
		}
	})

	release()
	backfillTrips(context.Background(), database)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if userIds, err := transaction.ListUsersNeedingTripBackfill(tripRulesChangedAt); err != nil || len(userIds) != 0 {
			test.Errorf("the pass did not run once the lock was free: %v %v", userIds, err)
		}
	})
}
