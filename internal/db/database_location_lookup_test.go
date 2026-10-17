package db_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The lookups the API's listings and check-ins use: positions in a range,
// places near a point, places by their OpenStreetMap object, how often each
// place was visited, and a stay by the key the phone gave it.
func TestLocationLookups(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "lookup-person")
		otherId := locationUser(t, tx, "lookup-other")

		// Positions: a range, oldest first, and a limit that can be told apart.
		points := []*models.LocationPoint{}
		for minute := 0; minute < 5; minute++ {
			points = append(points, &models.LocationPoint{LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude,
				MeasuredAt: *minutesAfterNine(minute), LocationSource: models.LocationSourcePhone})
		}
		if _, err := tx.InsertLocationPoints(userId, points); err != nil {
			t.Fatal(err)
		}
		found, err := tx.ListLocationPoints(userId, *minutesAfterNine(1), *minutesAfterNine(4), 10)
		if err != nil || len(found) != 3 || !found[0].MeasuredAt.Equal(*minutesAfterNine(1)) {
			t.Errorf("the range, oldest first: %d %v", len(found), err)
		}
		if limited, _ := tx.ListLocationPoints(userId, *minutesAfterNine(0), *minutesAfterNine(10), 2); len(limited) != 2 {
			t.Errorf("a limit: %d", len(limited))
		}
		if other, _ := tx.ListLocationPoints(otherId, *minutesAfterNine(0), *minutesAfterNine(10), 10); len(other) != 0 {
			t.Errorf("another person's: %d", len(other))
		}
		if _, err := tx.ListLocationPoints(userId, *minutesAfterNine(5), *minutesAfterNine(1), 10); err == nil {
			t.Error("a range that ends before it begins")
		}

		// Places near a point: by distance, the person's own, and one whose
		// own radius holds the point though its centre is further than asked.
		near := locationPlace(t, tx, userId, "Near", locationLatitude+0.0003)
		far := locationPlace(t, tx, userId, "Far", locationLatitude+0.0050)
		big, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Big", LatitudeDegrees: locationLatitude + 0.0060, LongitudeDegrees: locationLongitude, RadiusMeters: 1000})
		if err != nil {
			t.Fatal(err)
		}
		locationPlace(t, tx, otherId, "Theirs", locationLatitude)
		around, err := tx.ListPlacesNear(userId, locationLatitude, locationLongitude, 150, 10)
		if err != nil || len(around) != 2 || around[0].Place.ID != near.ID || around[1].Place.ID != big.ID {
			names := []string{}
			for _, each := range around {
				names = append(names, each.Place.PlaceName)
			}
			t.Errorf("Near, then Big (its radius holds the point), not Far and not theirs: %v %v", names, err)
		}
		if around[0].DistanceMeters < 30 || around[0].DistanceMeters > 40 {
			t.Errorf("the distance in meters: %v", around[0].DistanceMeters)
		}
		_ = far

		// Places by OpenStreetMap object.
		withObject, err := tx.CreatePlace(userId, &models.Place{PlaceName: "Cafe", LatitudeDegrees: locationLatitude, LongitudeDegrees: locationLongitude, OSMType: models.OSMTypeNode, OSMID: 42})
		if err != nil {
			t.Fatal(err)
		}
		ids, err := tx.FindPlaceIDsByOSM(userId, []db.OSMReference{{OSMType: "node", OSMID: 42}, {OSMType: "way", OSMID: 42}, {}})
		if err != nil || len(ids) != 1 || ids[db.OSMReference{OSMType: "node", OSMID: 42}] != withObject.ID {
			t.Errorf("by object: %v %v", ids, err)
		}
		if ids, _ := tx.FindPlaceIDsByOSM(otherId, []db.OSMReference{{OSMType: "node", OSMID: 42}}); len(ids) != 0 {
			t.Errorf("not another person's: %v", ids)
		}

		// Visits: counted per place, and found by key with their place.
		for index, key := range []string{"a", "b"} {
			if _, err := tx.PutPlaceVisit(userId, locationVisit(near.ID, key, index*60, index*60+10)); err != nil {
				t.Fatal(err)
			}
		}
		summaries, err := tx.SummarizePlaceVisits(userId, []string{near.ID, far.ID})
		if err != nil || summaries[near.ID] == nil || summaries[near.ID].VisitCount != 2 || summaries[near.ID].LastVisitAt == nil || summaries[far.ID] != nil {
			t.Errorf("counted: %v %v", summaries, err)
		}
		visit, err := tx.GetPlaceVisitByClientKey(userId, "b")
		if err != nil || visit == nil || visit.Place == nil || visit.Place.ID != near.ID {
			t.Errorf("by key: %v %v", visit, err)
		}
		if missing, err := tx.GetPlaceVisitByClientKey(otherId, "b"); err != nil || missing != nil {
			t.Errorf("another person's key: %v %v", missing, err)
		}
	})
}
