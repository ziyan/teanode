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
		if userIds, err := transaction.ListUsersNeedingTripBackfill(); err != nil || len(userIds) != 0 {
			test.Errorf("a second pass finds %v %v", userIds, err)
		}
	})
}
