package db_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// visitSpansOf is the person's visits as "place arrival-departure" text in
// the order they began, with minutes after nine.
func visitSpansOf(t *testing.T, tx db.Transaction, userId string) []string {
	t.Helper()
	visits, _, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{LimitCount: 200})
	if err != nil {
		t.Fatalf("ListPlaceVisits: %s", err)
	}
	spans := make([]string, 0, len(visits))
	for index := len(visits) - 1; index >= 0; index-- {
		visit := visits[index]
		departure := "open"
		if visit.DepartedAt != nil {
			departure = fmt.Sprint(int(visit.DepartedAt.Sub(locationDay).Minutes()))
		}
		spans = append(spans, fmt.Sprintf("%s %d-%s", visit.Place.PlaceName, int(visit.ArrivedAt.Sub(locationDay).Minutes()), departure))
	}
	return spans
}

func putAndNormalize(t *testing.T, tx db.Transaction, userId string, visit *models.PlaceVisit) {
	t.Helper()
	if _, err := tx.PutPlaceVisit(userId, visit); err != nil {
		t.Fatalf("PutPlaceVisit(%s): %s", visit.ClientVisitKey, err)
	}
	from, until := locationDay, locationDay
	if visit.ArrivedAt != nil {
		from, until = *visit.ArrivedAt, *visit.ArrivedAt
	}
	if visit.DepartedAt != nil {
		if visit.ArrivedAt == nil {
			from = *visit.DepartedAt
		}
		until = *visit.DepartedAt
	}
	if err := tx.NormalizePlaceVisits(userId, from, until); err != nil {
		t.Fatalf("NormalizePlaceVisits: %s", err)
	}
}

// An arrival at a place delivered after the person's later visit to the same
// place, and the visit after that, does not take the later visit's departure
// from it.
func TestAnArrivalDeliveredLateKeepsALaterVisitToTheSamePlaceWhole(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-late-arrival")
		home := locationPlace(t, tx, userId, "home", 10.5)
		gym := locationPlace(t, tx, userId, "gym", 10.6)

		putAndNormalize(t, tx, userId, locationVisit(home.ID, "home-second", 630, 900))
		putAndNormalize(t, tx, userId, locationVisit(gym.ID, "gym", 930, -1))
		putAndNormalize(t, tx, userId, locationVisit(home.ID, "home-first", 0, -1))
		putAndNormalize(t, tx, userId, locationVisit(home.ID, "home-first", -1, 300))
		got := fmt.Sprint(visitSpansOf(t, tx, userId))
		if want := "[home 0-300 home 630-900 gym 930-open]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}
	})
}

// Whatever order the reports of four stays arrive in, one at a time, the
// visits are the ones the person had.
func TestVisitsDeliveredInAnyOrderEndUpAsTheyWereInOrder(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	stays := []struct {
		placeName         string
		arrived, departed int
	}{{"home", 0, 300}, {"work", 330, 600}, {"home", 630, 900}, {"gym", 930, 1000}}
	random := rand.New(rand.NewSource(7))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for run := 0; run < 180; run++ {
			userId := locationUser(t, tx, fmt.Sprintf("location-order-%d", run))
			places := map[string]*models.Place{}
			for index, name := range []string{"home", "work", "gym"} {
				places[name] = locationPlace(t, tx, userId, name, 10.5+float64(index)/10)
			}
			var reports []*models.PlaceVisit
			for index, stay := range stays {
				key := fmt.Sprintf("stay-%d", index)
				reports = append(reports, locationVisit(places[stay.placeName].ID, key, stay.arrived, -1), locationVisit(places[stay.placeName].ID, key, -1, stay.departed))
			}
			random.Shuffle(len(reports), func(left, right int) { reports[left], reports[right] = reports[right], reports[left] })
			for _, report := range reports {
				putAndNormalize(t, tx, userId, report)
			}
			got := fmt.Sprint(visitSpansOf(t, tx, userId))
			if want := "[home 0-300 work 330-600 home 630-900 gym 930-1000]"; got != want {
				t.Fatalf("run %d: the visits are %s, want %s", run, got, want)
			}
		}
	})
}

// The latest report is the latest of a position and the arrival or departure
// of a visit.
func TestTheLatestLocationReportIsTheLatestOfPositionsAndVisits(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-latest-report")
		if reportedAt, err := tx.GetLatestLocationReportAt(userId); err != nil || !reportedAt.IsZero() {
			t.Fatalf("nothing reported: %v, %v", reportedAt, err)
		}
		cafe := locationPlace(t, tx, userId, "Birch Cafe", 10.5)
		if _, err := tx.InsertLocationPoints(userId, []*models.LocationPoint{{LatitudeDegrees: 1, LongitudeDegrees: 2, AccuracyMeters: 1, MeasuredAt: *minutesAfterNine(50), LocationSource: models.LocationSourcePhone}}); err != nil {
			t.Fatalf("InsertLocationPoints: %s", err)
		}
		if reportedAt, err := tx.GetLatestLocationReportAt(userId); err != nil || !reportedAt.Equal(*minutesAfterNine(50)) {
			t.Fatalf("a position: %v, %v", reportedAt, err)
		}
		if _, err := tx.PutPlaceVisit(userId, locationVisit(cafe.ID, "cafe", 60, 80)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		if reportedAt, err := tx.GetLatestLocationReportAt(userId); err != nil || !reportedAt.Equal(*minutesAfterNine(80)) {
			t.Fatalf("a visit's departure: %v, %v", reportedAt, err)
		}
	})
}
