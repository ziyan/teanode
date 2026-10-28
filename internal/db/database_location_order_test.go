package db_test

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

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
		arrival := "unknown"
		if visit.ArrivedAt != nil {
			arrival = fmt.Sprint(int(visit.ArrivedAt.Sub(locationDay).Minutes()))
		}
		spans = append(spans, fmt.Sprintf("%s %s-%s", visit.Place.PlaceName, arrival, departure))
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

// A departure from a watched place that the phone noticed long after the
// person had arrived somewhere else is the end of their visit before, which
// keeps its own departure; one stored inside a visit elsewhere before this was
// put in order, after a visit at a third place, moves to before the visit it
// was noticed in.
func TestADepartureNoticedLateIsNotAVisitInsideAnother(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-late-departure")
		home := locationPlace(t, tx, userId, "home", 10.5)
		office := locationPlace(t, tx, userId, "office", 10.6)
		gym := locationPlace(t, tx, userId, "gym", 10.7)
		regionDeparture := func(key string, departedMinute int) *models.PlaceVisit {
			visit := locationVisit(office.ID, key, -1, departedMinute)
			visit.VisitSource = models.VisitSourceRegion
			return visit
		}

		putAndNormalize(t, tx, userId, locationVisit(office.ID, "office", 0, 200))
		putAndNormalize(t, tx, userId, locationVisit(home.ID, "home", 225, -1))
		// The phone also tells of coming into home's circle, which joins the
		// visit home; a visit begun at the late departure would split it.
		homeCircle := locationVisit(home.ID, "home-circle", 320, -1)
		homeCircle.VisitSource = models.VisitSourceRegion
		putAndNormalize(t, tx, userId, homeCircle)
		putAndNormalize(t, tx, userId, regionDeparture("office-circle", 300))
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[office 0-200 home 225-open]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}
		found, err := tx.GetPlaceVisitByClientKey(userId, "office-circle")
		if err != nil || found == nil || found.ArrivedAt == nil || !found.ArrivedAt.Equal(*minutesAfterNine(0)) {
			t.Errorf("the late departure's key finds the visit to the office: %+v %v", found, err)
		}
		putAndNormalize(t, tx, userId, locationVisit(home.ID, "home", -1, 900))
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[office 0-200 home 225-900]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}

		putAndNormalize(t, tx, userId, locationVisit(gym.ID, "gym", 1000, 1100))
		putAndNormalize(t, tx, userId, locationVisit(home.ID, "home-again", 1130, 1500))
		if _, err := tx.PutPlaceVisit(userId, regionDeparture("office-circle-again", 1200)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		if ranges, err := tx.ListOverlappedVisitRanges(locationDay.Add(-time.Hour)); err != nil || len(ranges) != 1 || ranges[0].UserID != userId {
			t.Errorf("the departure inside the visit home is found to put in order: %+v %v", ranges, err)
		}
		if err := tx.NormalizePlaceVisits(userId, *minutesAfterNine(1130), *minutesAfterNine(1500)); err != nil {
			t.Fatalf("NormalizePlaceVisits: %s", err)
		}
		found, err = tx.GetPlaceVisitByClientKey(userId, "office-circle-again")
		if err != nil || found == nil || found.ArrivedAt != nil || found.DepartedAt == nil || !found.DepartedAt.Equal(*minutesAfterNine(1130)) {
			t.Errorf("the departure after a visit elsewhere moves to the arrival home: %+v %v", found, err)
		}
		if ranges, err := tx.ListOverlappedVisitRanges(locationDay.Add(-time.Hour)); err != nil || len(ranges) != 0 {
			t.Errorf("visits put in order still overlap: %+v %v", ranges, err)
		}
	})
}

// A departure from a watched place with no arrival that came moments after
// the person's visit before it ended was no stop: one at the same place is
// that visit's own, one at another place is a place they drove past. One long
// after the visit before stays, as a visit whose arrival was missed.
func TestADepartureMomentsAfterAVisitIsNoStop(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-pass-by-departure")
		office := locationPlace(t, tx, userId, "office", 10.5)
		cafe := locationPlace(t, tx, userId, "cafe", 10.6)
		pharmacy := locationPlace(t, tx, userId, "pharmacy", 10.7)
		regionDeparture := func(placeId, key string, departedMinute int) *models.PlaceVisit {
			visit := locationVisit(placeId, key, -1, departedMinute)
			visit.VisitSource = models.VisitSourceRegion
			return visit
		}

		putAndNormalize(t, tx, userId, locationVisit(office.ID, "office", 0, 180))
		putAndNormalize(t, tx, userId, regionDeparture(office.ID, "office-circle", 181))
		putAndNormalize(t, tx, userId, locationVisit(cafe.ID, "cafe", 200, 236))
		putAndNormalize(t, tx, userId, regionDeparture(pharmacy.ID, "pharmacy-circle", 238))
		putAndNormalize(t, tx, userId, locationVisit(office.ID, "office-again", 240, 400))
		putAndNormalize(t, tx, userId, regionDeparture(pharmacy.ID, "pharmacy-circle-later", 450))
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[office 0-180 cafe 200-236 office 240-400 pharmacy unknown-450]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}
		found, err := tx.GetPlaceVisitByClientKey(userId, "office-circle")
		if err != nil || found == nil || found.ArrivedAt == nil || !found.ArrivedAt.Equal(*minutesAfterNine(0)) {
			t.Errorf("the departure's key finds the visit to the office it ended: %+v %v", found, err)
		}

		// One stored before this was put in order is found at start.
		if _, err := tx.PutPlaceVisit(userId, regionDeparture(cafe.ID, "cafe-circle", 401)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		if ranges, err := tx.ListOverlappedVisitRanges(locationDay.Add(-time.Hour)); err != nil || len(ranges) != 1 || ranges[0].UserID != userId {
			t.Errorf("the departure moments after a visit is found to put in order: %+v %v", ranges, err)
		}
		if err := tx.NormalizePlaceVisits(userId, *minutesAfterNine(240), *minutesAfterNine(401)); err != nil {
			t.Fatalf("NormalizePlaceVisits: %s", err)
		}
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[office 0-180 cafe 200-236 office 240-400 pharmacy unknown-450]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}
		if ranges, err := tx.ListOverlappedVisitRanges(locationDay.Add(-time.Hour)); err != nil || len(ranges) != 0 {
			t.Errorf("visits put in order still need it: %+v %v", ranges, err)
		}
	})
}

// A visit of no length the phone tells of a short way off, in the middle of
// a visit at another place, is no stop and does not end that visit; the place
// it made is taken out with it, and a check-in there is kept.
func TestAMomentsVisitInsideAnotherIsNoStop(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-visit-inside")
		school := locationPlace(t, tx, userId, "school", 10.5)
		corner := locationPlace(t, tx, userId, "corner", 10.6)
		office := locationPlace(t, tx, userId, "office", 10.7)
		label := models.PlaceLabelSchool
		if _, err := tx.SavePlace(userId, school.ID, db.PlaceChanges{PlaceLabel: &label}); err != nil {
			t.Fatalf("SavePlace: %s", err)
		}
		schoolVisit := locationVisit(school.ID, "school", 0, -1)
		schoolVisit.VisitSource = models.VisitSourceRegion
		putAndNormalize(t, tx, userId, schoolVisit)
		putAndNormalize(t, tx, userId, locationVisit(corner.ID, "corner", 1, 1))
		schoolDeparture := locationVisit(school.ID, "school", -1, 8)
		schoolDeparture.VisitSource = models.VisitSourceRegion
		putAndNormalize(t, tx, userId, schoolDeparture)
		putAndNormalize(t, tx, userId, locationVisit(office.ID, "office", 30, -1))
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[school 0-8 office 30-open]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}
		if found, err := tx.GetPlace(userId, corner.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("the place the moment's visit made is taken out: %+v %v", found, err)
		}

		// One stored before this was put in order is found at start, and a
		// check-in inside another visit is the person's own word.
		if _, err := tx.PutPlaceVisit(userId, locationVisit(office.ID, "office-moment", 2, 3)); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
		if ranges, err := tx.ListOverlappedVisitRanges(locationDay.Add(-time.Hour)); err != nil || len(ranges) != 1 || ranges[0].UserID != userId {
			t.Errorf("the visit inside another is found to put in order: %+v %v", ranges, err)
		}
		if err := tx.NormalizePlaceVisits(userId, *minutesAfterNine(0), *minutesAfterNine(8)); err != nil {
			t.Fatalf("NormalizePlaceVisits: %s", err)
		}
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[school 0-8 office 30-open]"; got != want {
			t.Errorf("the visits are %s, want %s", got, want)
		}
		checkIn := locationVisit(office.ID, "office-check-in", 4, 5)
		checkIn.VisitSource = models.VisitSourceCheckIn
		putAndNormalize(t, tx, userId, checkIn)
		if got, want := fmt.Sprint(visitSpansOf(t, tx, userId)), "[school 0-4 office 4-5 office 30-open]"; got != want {
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
