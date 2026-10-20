package db_test

import (
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A visit from a place's circle that is short and has the person moving fast
// near it is a pass-by and is deleted; a short one with a stop in it, a long
// one, one nothing says the speed of, and a visit iOS noticed stay.
func TestPassBysAreDeleted(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-pass-by")
		otherId := locationUser(t, tx, "location-pass-by-other")
		school := locationPlace(t, tx, userId, "School", locationLatitude)
		otherSchool := locationPlace(t, tx, otherId, "School", locationLatitude)

		at := func(minutes, seconds int) time.Time {
			return locationDay.Add(time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second)
		}
		put := func(owner, placeId, key string, visitSource models.VisitSource, arrivedAt, departedAt time.Time) string {
			t.Helper()
			visit := &models.PlaceVisit{
				PlaceID: placeId, ClientVisitKey: key, VisitSource: visitSource, ArrivedAt: &arrivedAt, DepartedAt: &departedAt,
				LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude,
			}
			if _, err := tx.PutPlaceVisit(owner, visit); err != nil {
				t.Fatalf("PutPlaceVisit(%s): %s", key, err)
			}
			return visit.ID
		}
		position := func(owner string, measuredAt time.Time, speedMetersPerSecond float64) *models.LocationPoint {
			return &models.LocationPoint{
				UserID: owner, LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude, AccuracyMeters: 5,
				SpeedMetersPerSecond: &speedMetersPerSecond, MeasuredAt: measuredAt, LocationSource: models.LocationSourcePhone,
			}
		}

		// Fifteen seconds in the circle, at 19 m/s during them.
		throughId := put(userId, school.ID, "through", models.VisitSourceRegion, at(0, 0), at(0, 15))
		// Under two minutes, at 20 m/s a minute before, nothing during.
		slowedId := put(userId, school.ID, "slowed", models.VisitSourceRegion, at(30, 0), at(31, 42))
		// Two minutes with a stop: a drop-off.
		dropOffId := put(userId, school.ID, "drop-off", models.VisitSourceRegion, at(60, 0), at(62, 0))
		// Five minutes, though fast before.
		stayId := put(userId, school.ID, "stay", models.VisitSourceRegion, at(90, 0), at(95, 0))
		// Short, and no position says how fast.
		unknownId := put(userId, school.ID, "unknown", models.VisitSourceRegion, at(120, 0), at(121, 0))
		// iOS noticed it, so the person stayed.
		noticedId := put(userId, school.ID, "noticed", models.VisitSourceVisit, at(150, 0), at(151, 0))
		// Another person's pass-by is theirs.
		otherVisitId := put(otherId, otherSchool.ID, "other", models.VisitSourceRegion, at(0, 0), at(0, 15))
		for _, point := range []*models.LocationPoint{
			position(userId, at(0, 5), 19),
			position(userId, at(29, 6), 20),
			position(userId, at(59, 30), 10), position(userId, at(61, 0), 0),
			position(userId, at(89, 30), 20),
			position(userId, at(149, 30), 20),
		} {
			if _, err := tx.InsertLocationPoints(userId, []*models.LocationPoint{point}); err != nil {
				t.Fatalf("InsertLocationPoints: %s", err)
			}
		}
		if _, err := tx.InsertLocationPoints(otherId, []*models.LocationPoint{position(otherId, at(0, 5), 19)}); err != nil {
			t.Fatalf("InsertLocationPoints: %s", err)
		}

		// A range that ends before the first is over touches nothing.
		if deletedIds, err := tx.DeletePassByVisits(userId, at(-30, 0), at(-10, 0)); err != nil || len(deletedIds) != 0 {
			t.Fatalf("a range before them deleted %v, %v", deletedIds, err)
		}
		deletedIds, err := tx.DeletePassByVisits(userId, at(0, 0), at(160, 0))
		if err != nil {
			t.Fatalf("DeletePassByVisits: %s", err)
		}
		slices.Sort(deletedIds)
		expected := []string{throughId, slowedId}
		slices.Sort(expected)
		if !slices.Equal(deletedIds, expected) {
			t.Errorf("deleted %v, the pass-bys are %v", deletedIds, expected)
		}
		visits, _, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{})
		if err != nil {
			t.Fatalf("ListPlaceVisits: %s", err)
		}
		var keptIds []string
		for _, visit := range visits {
			keptIds = append(keptIds, visit.ID)
		}
		for _, keptId := range []string{dropOffId, stayId, unknownId, noticedId} {
			if !slices.Contains(keptIds, keptId) {
				t.Errorf("visit %s is not a pass-by and was deleted", keptId)
			}
		}
		if visit, err := tx.GetPlaceVisitByClientKey(otherId, "other"); err != nil || visit == nil || visit.ID != otherVisitId {
			t.Errorf("another person's visit was touched: %v, %v", visit, err)
		}
	})
}

// The departure of a visit iOS noticed finds the visit by the second it
// arrived in; a visit from a place's circle is not found that way.
func TestAVisitIsFoundByItsArrival(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-arrival")
		home := locationPlace(t, tx, userId, "Home", locationLatitude)
		arrivedAt := locationDay.Add(400 * time.Millisecond)
		noticed := &models.PlaceVisit{
			PlaceID: home.ID, ClientVisitKey: "noticed", VisitSource: models.VisitSourceVisit, ArrivedAt: &arrivedAt,
			LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude,
		}
		if _, err := tx.PutPlaceVisit(userId, noticed); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		regionAt := locationDay.Add(time.Hour)
		if _, err := tx.PutPlaceVisit(userId, &models.PlaceVisit{
			PlaceID: home.ID, ClientVisitKey: "region", VisitSource: models.VisitSourceRegion, ArrivedAt: &regionAt,
			LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude,
		}); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}

		if found, err := tx.GetPlaceVisitByArrival(userId, locationDay.Add(900*time.Millisecond)); err != nil || found == nil || found.ClientVisitKey != "noticed" {
			t.Errorf("the same second finds the visit: %v, %v", found, err)
		}
		if found, err := tx.GetPlaceVisitByArrival(userId, locationDay.Add(time.Second)); err != nil || found != nil {
			t.Errorf("the next second finds nothing: %v, %v", found, err)
		}
		if found, err := tx.GetPlaceVisitByArrival(userId, regionAt); err != nil || found != nil {
			t.Errorf("a visit from a circle is not found by its arrival: %v, %v", found, err)
		}
	})
}
