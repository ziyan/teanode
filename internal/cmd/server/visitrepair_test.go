package server

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A visit stored over the stops made since it began, as a departure sent
// hours late left it, is ended where the first of them began; a second pass
// finds nothing, and a person whose visits are in order is left alone.
func TestRepairOverlappedVisitsEndsAVisitAtTheFirstStopElsewhere(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "visit-repair")
	orderlyUserId := dbtest.CreateUser(test, database, "visit-repair-orderly")
	base := time.Now().Add(-8 * time.Hour).Truncate(time.Second)
	minutes := func(count int) *time.Time {
		at := base.Add(time.Duration(count) * time.Minute)
		return &at
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		for _, owner := range []string{userId, orderlyUserId} {
			home, err := transaction.CreatePlace(owner, &models.Place{PlaceName: "Home", IsNamedByPerson: true, LatitudeDegrees: 40, LongitudeDegrees: -75, RadiusMeters: 100})
			if err != nil {
				test.Fatal(err)
			}
			away, err := transaction.CreatePlace(owner, &models.Place{PlaceName: "Away", IsNamedByPerson: true, LatitudeDegrees: 40.16, LongitudeDegrees: -75, RadiusMeters: 100})
			if err != nil {
				test.Fatal(err)
			}
			homeDepartedAt := minutes(217)
			if owner == orderlyUserId {
				homeDepartedAt = minutes(48)
			}
			for _, visit := range []*models.PlaceVisit{
				{PlaceID: home.ID, ClientVisitKey: "home", VisitSource: models.VisitSourceRegion, ArrivedAt: minutes(0), DepartedAt: homeDepartedAt, LatitudeDegrees: 40, LongitudeDegrees: -75},
				{PlaceID: away.ID, ClientVisitKey: "away", VisitSource: models.VisitSourceVisit, ArrivedAt: minutes(48), DepartedAt: minutes(136), LatitudeDegrees: 40.16, LongitudeDegrees: -75, AccuracyMeters: 20},
			} {
				if _, err := transaction.PutPlaceVisit(owner, visit); err != nil {
					test.Fatal(err)
				}
			}
		}
		ranges, err := transaction.ListOverlappedVisitRanges(base.Add(-time.Hour))
		if err != nil || len(ranges) != 1 || ranges[0].UserID != userId {
			test.Fatalf("one person's visits overlap: %+v %v", ranges, err)
		}
	})

	repairOverlappedVisits(context.Background(), database)

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		home, err := transaction.GetPlaceVisitByClientKey(userId, "home")
		if err != nil || home == nil || home.DepartedAt == nil || !home.DepartedAt.Equal(*minutes(48)) {
			test.Errorf("home ends where the stop elsewhere began: %+v %v", home, err)
		}
		if ranges, err := transaction.ListOverlappedVisitRanges(base.Add(-time.Hour)); err != nil || len(ranges) != 0 {
			test.Errorf("a second pass finds %+v %v", ranges, err)
		}
	})
}

// A start while another instance is putting visits in order does nothing.
func TestRepairOverlappedVisitsLeavesThePassToTheInstanceHoldingIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "visit-repair-held")
	base := time.Now().Add(-8 * time.Hour).Truncate(time.Second)
	minutes := func(count int) *time.Time {
		at := base.Add(time.Duration(count) * time.Minute)
		return &at
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		home, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Home", IsNamedByPerson: true, LatitudeDegrees: 40, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		away, err := transaction.CreatePlace(userId, &models.Place{PlaceName: "Away", IsNamedByPerson: true, LatitudeDegrees: 40.16, LongitudeDegrees: -75, RadiusMeters: 100})
		if err != nil {
			test.Fatal(err)
		}
		for _, visit := range []*models.PlaceVisit{
			{PlaceID: home.ID, ClientVisitKey: "home", VisitSource: models.VisitSourceRegion, ArrivedAt: minutes(0), DepartedAt: minutes(217), LatitudeDegrees: 40, LongitudeDegrees: -75},
			{PlaceID: away.ID, ClientVisitKey: "away", VisitSource: models.VisitSourceVisit, ArrivedAt: minutes(48), DepartedAt: minutes(136), LatitudeDegrees: 40.16, LongitudeDegrees: -75, AccuracyMeters: 20},
		} {
			if _, err := transaction.PutPlaceVisit(userId, visit); err != nil {
				test.Fatal(err)
			}
		}
	})

	release, isLocked, err := database.TryLockSession(context.Background(), db.LockKey("visit:repair"))
	if err != nil || !isLocked {
		test.Fatalf("the lock was not free: %v %v", isLocked, err)
	}
	repairOverlappedVisits(context.Background(), database)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if ranges, err := transaction.ListOverlappedVisitRanges(base.Add(-time.Hour)); err != nil || len(ranges) != 1 {
			test.Errorf("the pass ran while another instance held the lock: %+v %v", ranges, err)
		}
	})

	release()
	repairOverlappedVisits(context.Background(), database)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if ranges, err := transaction.ListOverlappedVisitRanges(base.Add(-time.Hour)); err != nil || len(ranges) != 0 {
			test.Errorf("the pass did not run once the lock was free: %+v %v", ranges, err)
		}
	})
}
