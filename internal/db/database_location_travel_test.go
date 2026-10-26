package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func travelLeg(startedAt time.Time, minutes int, travelMode string, distanceMeters float64) *models.TripLeg {
	return &models.TripLeg{
		StartedAt: startedAt, EndedAt: startedAt.Add(time.Duration(minutes) * time.Minute),
		DetectedTravelMode: travelMode, DetectionSource: "motion", DistanceMeters: distanceMeters,
	}
}

// travelTrip is a trip made of the legs, one after the other, from startedAt.
func travelTrip(startedAt time.Time, legs ...*models.TripLeg) *models.Trip {
	trip := &models.Trip{StartedAt: startedAt, Legs: legs}
	trip.EndedAt = startedAt
	for _, leg := range legs {
		trip.DistanceMeters += leg.DistanceMeters
		if leg.EndedAt.After(trip.EndedAt) {
			trip.EndedAt = leg.EndedAt
		}
	}
	return trip
}

func putTrips(t *testing.T, tx db.Transaction, userId string, trips ...*models.Trip) {
	t.Helper()
	if err := tx.ReplaceTrips(userId, locationDay.AddDate(-5, 0, 0), locationDay.AddDate(5, 0, 0), trips); err != nil {
		t.Fatalf("ReplaceTrips: %s", err)
	}
}

// A report sent twice adds no activity, and the listing starts with the
// activity in effect at the start of the range.
func TestMotionActivitiesAreStoredOnceAndListedWithTheOneInEffect(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-activity")
		otherUserId := locationUser(t, tx, "travel-activity-other")
		activity := func(minute int, kind string) *models.MotionActivity {
			return &models.MotionActivity{StartedAt: *minutesAfterNine(minute), ActivityKind: kind, ActivityConfidence: "high"}
		}
		batch := []*models.MotionActivity{activity(0, "stationary"), activity(10, "walking"), activity(20, "automotive")}
		if newCount, err := tx.PutMotionActivities(userId, batch); err != nil || newCount != 3 {
			t.Fatalf("the first report adds three: %d %v", newCount, err)
		}
		if newCount, err := tx.PutMotionActivities(userId, append(batch, activity(30, "stationary"))); err != nil || newCount != 1 {
			t.Fatalf("the same report again adds only the new one: %d %v", newCount, err)
		}
		if newCount, err := tx.PutMotionActivities(otherUserId, batch[:1]); err != nil || newCount != 1 {
			t.Fatalf("somebody else may have one at the same instant: %d %v", newCount, err)
		}

		listed, err := tx.ListMotionActivities(userId, *minutesAfterNine(15), *minutesAfterNine(30))
		if err != nil || len(listed) != 2 || listed[0].ActivityKind != "walking" || listed[1].ActivityKind != "automotive" {
			t.Fatalf("the one in effect at 15 minutes, then the one at 20: %+v %v", listed, err)
		}
		listed, err = tx.ListMotionActivities(userId, *minutesAfterNine(10), *minutesAfterNine(11))
		if err != nil || len(listed) != 2 || listed[0].ActivityKind != "stationary" || listed[1].ActivityKind != "walking" {
			t.Fatalf("one starting exactly at the range start comes after the one in effect before it: %+v %v", listed, err)
		}
		listed, err = tx.ListMotionActivities(userId, *minutesAfterNine(-60), *minutesAfterNine(-30))
		if err != nil || len(listed) != 0 {
			t.Fatalf("nothing before the first: %+v %v", listed, err)
		}
	})
}

// Replacing trips deletes and stores only the trips that start in its window,
// legs included, and leaves another person's alone.
func TestReplacingTripsReplacesOnlyItsWindow(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-replace")
		otherUserId := locationUser(t, tx, "travel-replace-other")
		at := func(hours int) time.Time { return locationDay.Add(time.Duration(hours) * time.Hour) }
		everything := func(who string) []*models.Trip {
			trips, nextOffset, err := tx.ListTrips(who, at(-24), at(100), 0, 0)
			if err != nil || nextOffset != nil {
				t.Fatalf("ListTrips: %v %v", nextOffset, err)
			}
			return trips
		}
		for _, hours := range []int{0, 24, 48} {
			putTripAt := travelTrip(at(hours), travelLeg(at(hours), 20, "car", 5000))
			if err := tx.ReplaceTrips(userId, at(hours), at(hours+1), []*models.Trip{putTripAt}); err != nil {
				t.Fatalf("ReplaceTrips: %s", err)
			}
		}
		if err := tx.ReplaceTrips(otherUserId, at(-24), at(100), []*models.Trip{travelTrip(at(24), travelLeg(at(24), 20, "walk", 900))}); err != nil {
			t.Fatalf("ReplaceTrips: %s", err)
		}
		before := everything(userId)
		if len(before) != 3 {
			t.Fatalf("three trips: %+v", before)
		}

		// The middle day is built again as two legs; the others stay as they were.
		replacement := travelTrip(at(24), travelLeg(at(24), 5, "walk", 400), travelLeg(at(24).Add(5*time.Minute), 15, "bike", 3000))
		if err := tx.ReplaceTrips(userId, at(24), at(48), []*models.Trip{replacement}); err != nil {
			t.Fatalf("ReplaceTrips: %s", err)
		}
		after := everything(userId)
		if len(after) != 3 || after[0].ID != before[0].ID || after[2].ID != before[2].ID {
			t.Fatalf("the first and last trips are untouched: %+v", after)
		}
		if after[1].ID == before[1].ID || len(after[1].Legs) != 2 || after[1].Legs[0].TravelMode != "walk" || after[1].Legs[1].TravelMode != "bike" {
			t.Fatalf("the middle trip is the new one with its two legs: %+v", after[1])
		}
		if old, err := tx.GetTripLeg(userId, before[1].Legs[0].ID); err != nil || old != nil {
			t.Fatalf("the replaced leg is gone: %+v %v", old, err)
		}
		if others := everything(otherUserId); len(others) != 1 {
			t.Fatalf("somebody else's trip stays: %+v", others)
		}

		// An empty window, and a mode that is not one of the eight, are refused.
		if err := tx.ReplaceTrips(userId, at(1), at(1), nil); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an empty window is refused: %v", err)
		}
		bad := travelTrip(at(60), travelLeg(at(60), 10, "rocket", 10))
		if err := tx.ReplaceTrips(userId, at(60), at(61), []*models.Trip{bad}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown way is refused: %v", err)
		}
	})
}

// A correction covering half of a leg or more decides its way of travel, the
// one covering most wins, and one covering less is ignored.
func TestACorrectionCoveringHalfALegDecidesItsTravelMode(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-resolve")
		otherUserId := locationUser(t, tx, "travel-resolve-other")
		start := locationDay
		minute := func(offset int) time.Time { return start.Add(time.Duration(offset) * time.Minute) }
		putTrips(t, tx, userId, travelTrip(minute(0), travelLeg(minute(0), 20, "car", 8000), travelLeg(minute(100), 20, "walk", 1500)))
		legs := func() []*models.TripLeg {
			trips, _, err := tx.ListTrips(userId, minute(-60), minute(300), 0, 0)
			if err != nil || len(trips) != 1 || len(trips[0].Legs) != 2 {
				t.Fatalf("ListTrips: %+v %v", trips, err)
			}
			return trips[0].Legs
		}
		correct := func(who string, from, until int, travelMode string) {
			if err := tx.SetTravelModeCorrection(who, minute(from), minute(until), &travelMode); err != nil {
				t.Fatalf("SetTravelModeCorrection: %s", err)
			}
		}

		// Nine of twenty minutes is less than half.
		correct(userId, 11, 40, "bus")
		if leg := legs()[0]; leg.TravelMode != "car" || leg.DetectedTravelMode != "car" || leg.IsTravelModeCorrected {
			t.Fatalf("a correction covering under half changes nothing: %+v", leg)
		}
		// Ten of twenty minutes is half.
		correct(userId, 10, 40, "bus")
		if leg := legs()[0]; leg.TravelMode != "bus" || leg.DetectedTravelMode != "car" || !leg.IsTravelModeCorrected {
			t.Fatalf("a correction covering half wins: %+v", leg)
		}
		if leg := legs()[1]; leg.TravelMode != "walk" || leg.IsTravelModeCorrected {
			t.Fatalf("the other leg is not touched: %+v", leg)
		}
		// Somebody else's correction means nothing here.
		correct(otherUserId, 0, 20, "boat")
		if leg := legs()[0]; leg.TravelMode != "bus" {
			t.Fatalf("somebody else's correction is not applied: %+v", leg)
		}

		// GetTripLeg resolves the same way and is for the owner only.
		leg, err := tx.GetTripLeg(userId, legs()[0].ID)
		if err != nil || leg == nil || leg.TravelMode != "bus" || !leg.IsTravelModeCorrected {
			t.Fatalf("GetTripLeg: %+v %v", leg, err)
		}
		if leg, err := tx.GetTripLeg(otherUserId, legs()[0].ID); err != nil || leg != nil {
			t.Fatalf("GetTripLeg of somebody else's leg: %+v %v", leg, err)
		}
		if leg, err := tx.GetTripLeg(userId, "nothing"); err != nil || leg != nil {
			t.Fatalf("GetTripLeg of no leg: %+v %v", leg, err)
		}

		// Two corrections can each cover half of a leg only when they meet at
		// the middle; the one that covers more wins. Corrections that overlap
		// replace each other, so build the two by hand at the edge: a leg of
		// 20 minutes, 12 minutes train then 8 minutes boat.
		correct(userId, 100, 112, "train")
		correct(userId, 112, 120, "boat")
		if leg := legs()[1]; leg.TravelMode != "train" || !leg.IsTravelModeCorrected {
			t.Fatalf("the correction covering most wins: %+v", leg)
		}
	})
}

// Correcting a span replaces every correction it overlaps, and nil clears
// them and adds nothing.
func TestCorrectingASpanReplacesOverlapsAndNilClears(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	userIdForCount := ""
	defer func() {
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "travel_mode_correction" WHERE "user_id" = '`+userIdForCount+`'`); got != "0" {
			t.Errorf("clearing leaves %s corrections", got)
		}
	}()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-correct")
		start := locationDay
		minute := func(offset int) time.Time { return start.Add(time.Duration(offset) * time.Minute) }
		putTrips(t, tx, userId, travelTrip(minute(0),
			travelLeg(minute(0), 10, "car", 3000), travelLeg(minute(10), 10, "car", 3000), travelLeg(minute(20), 10, "car", 3000)))
		modes := func() []string {
			trips, _, err := tx.ListTrips(userId, minute(-60), minute(300), 0, 0)
			if err != nil || len(trips) != 1 {
				t.Fatalf("ListTrips: %+v %v", trips, err)
			}
			var found []string
			for _, leg := range trips[0].Legs {
				found = append(found, leg.TravelMode)
			}
			return found
		}
		equal := func(got []string, want ...string) {
			t.Helper()
			if len(got) != len(want) {
				t.Fatalf("modes %v, want %v", got, want)
			}
			for index := range want {
				if got[index] != want[index] {
					t.Fatalf("modes %v, want %v", got, want)
				}
			}
		}
		set := func(from, until int, travelMode *string) {
			t.Helper()
			if err := tx.SetTravelModeCorrection(userId, minute(from), minute(until), travelMode); err != nil {
				t.Fatalf("SetTravelModeCorrection: %s", err)
			}
		}
		train, boat := "train", "boat"
		set(0, 10, &train)
		set(10, 20, &boat)
		equal(modes(), "train", "boat", "car")
		// A span over both replaces both, and covers half of each leg.
		bus := "bus"
		set(5, 15, &bus)
		equal(modes(), "bus", "bus", "car")
		set(0, 30, &bus)
		equal(modes(), "bus", "bus", "bus")
		// Clearing a span takes away every correction it overlaps.
		set(0, 30, nil)
		equal(modes(), "car", "car", "car")

		userIdForCount = userId

		rocket := "rocket"
		if err := tx.SetTravelModeCorrection(userId, minute(0), minute(10), &rocket); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown way is refused: %v", err)
		}
		if err := tx.SetTravelModeCorrection(userId, minute(10), minute(10), &bus); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an empty span is refused: %v", err)
		}
	})
}

// Totals are grouped by the period a leg started in, in the traveler's zone.
func TestTravelTotalsGroupByPeriodInTheTravelersZone(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-totals")
		zone, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			t.Skipf("no time zone data: %s", err)
		}
		local := func(year int, month time.Month, day, hour, minute int) time.Time {
			return time.Date(year, month, day, hour, minute, 0, 0, zone)
		}
		legs := []*models.TripLeg{
			// Just before and just after local midnight, which is 08:00 UTC.
			travelLeg(local(2026, time.March, 4, 23, 50), 5, "car", 1000),
			travelLeg(local(2026, time.March, 5, 0, 10), 10, "car", 2000),
			travelLeg(local(2026, time.March, 5, 9, 0), 10, "walk", 500),
			// Sunday night, then Monday just after midnight.
			travelLeg(local(2026, time.March, 1, 22, 0), 10, "bike", 4000),
			travelLeg(local(2026, time.March, 2, 0, 10), 10, "bike", 8000),
			// The last evening of February and of 2025.
			travelLeg(local(2026, time.February, 28, 23, 30), 10, "car", 16000),
			travelLeg(local(2025, time.December, 31, 23, 30), 10, "plane", 32000),
		}
		var trips []*models.Trip
		for _, leg := range legs {
			trips = append(trips, travelTrip(leg.StartedAt, leg))
		}
		putTrips(t, tx, userId, trips...)
		// The walk is really a bus.
		bus := "bus"
		if err := tx.SetTravelModeCorrection(userId, local(2026, time.March, 5, 9, 0), local(2026, time.March, 5, 9, 10), &bus); err != nil {
			t.Fatalf("SetTravelModeCorrection: %s", err)
		}

		from, until := local(2025, time.December, 1, 0, 0), local(2026, time.March, 10, 0, 0)
		totalsFor := func(period string) map[string]*models.TravelTotal {
			found, err := tx.ListTravelTotals(userId, zone, period, from, until)
			if err != nil {
				t.Fatalf("ListTravelTotals(%s): %s", period, err)
			}
			byKey := map[string]*models.TravelTotal{}
			for _, total := range found {
				byKey[total.PeriodStartDay+" "+total.TravelMode] = total
			}
			return byKey
		}
		check := func(period string, totals map[string]*models.TravelTotal, key string, distanceMeters float64, legCount int) {
			t.Helper()
			total := totals[key]
			if total == nil || total.DistanceMeters != distanceMeters || total.LegCount != legCount {
				t.Errorf("%s %s: %+v, want %v meters in %d legs", period, key, total, distanceMeters, legCount)
			}
		}

		day := totalsFor("day")
		check("day", day, "2026-03-04 car", 1000, 1)
		check("day", day, "2026-03-05 car", 2000, 1)
		check("day", day, "2026-03-05 bus", 500, 1)
		check("day", day, "2026-03-01 bike", 4000, 1)
		check("day", day, "2026-03-02 bike", 8000, 1)
		if day["2026-03-05 bus"].DurationSeconds != 600 {
			t.Errorf("ten minutes is 600 seconds: %+v", day["2026-03-05 bus"])
		}

		week := totalsFor("week")
		check("week", week, "2026-02-23 bike", 4000, 1)
		check("week", week, "2026-03-02 bike", 8000, 1)
		check("week", week, "2026-03-02 car", 3000, 2)
		check("week", week, "2026-02-23 car", 16000, 1)

		month := totalsFor("month")
		check("month", month, "2026-03-01 car", 3000, 2)
		check("month", month, "2026-03-01 bike", 12000, 2)
		check("month", month, "2026-02-01 car", 16000, 1)

		year := totalsFor("year")
		check("year", year, "2026-01-01 car", 19000, 3)
		check("year", year, "2025-01-01 plane", 32000, 1)

		// Newest period first, then the ways in order.
		found, err := tx.ListTravelTotals(userId, zone, "day", from, until)
		if err != nil || len(found) < 3 || found[0].PeriodStartDay != "2026-03-05" || found[0].TravelMode != "car" || found[1].TravelMode != "bus" {
			t.Fatalf("ordered newest first, then car before bus: %+v %v", found, err)
		}

		// Only legs that started inside the range count.
		narrow, err := tx.ListTravelTotals(userId, zone, "day", local(2026, time.March, 5, 0, 0), local(2026, time.March, 6, 0, 0))
		if err != nil || len(narrow) != 2 {
			t.Fatalf("one day holds two totals: %+v %v", narrow, err)
		}

		// Too many periods, and a period that does not exist, are refused.
		if _, err := tx.ListTravelTotals(userId, zone, "day", from, from.AddDate(2, 0, 0)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("more than 400 days is refused: %v", err)
		}
		if _, err := tx.ListTravelTotals(userId, zone, "week", from, from.AddDate(2, 0, 0)); err != nil {
			t.Errorf("two years of weeks is allowed: %s", err)
		}
		if _, err := tx.ListTravelTotals(userId, zone, "fortnight", from, until); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an unknown period is refused: %v", err)
		}
	})
}

// The people whose past days need trips are those with a visit before their
// earliest trip or with visits and no trip.
func TestPeopleWithVisitsBeforeTheirTripsAreListed(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		noTripId := locationUser(t, tx, "travel-backfill-none")
		oldVisitId := locationUser(t, tx, "travel-backfill-old")
		builtId := locationUser(t, tx, "travel-backfill-built")
		for _, userId := range []string{noTripId, oldVisitId, builtId} {
			place := locationPlace(t, tx, userId, "Maple Street", 10.1)
			if _, err := tx.PutPlaceVisit(userId, locationVisit(place.ID, "early", 0, 30)); err != nil {
				t.Fatalf("PutPlaceVisit: %s", err)
			}
		}
		putTrips(t, tx, oldVisitId, travelTrip(*minutesAfterNine(60), travelLeg(*minutesAfterNine(60), 10, "car", 3000)))
		putTrips(t, tx, builtId, travelTrip(*minutesAfterNine(-60), travelLeg(*minutesAfterNine(-60), 10, "car", 3000)))

		if start, err := tx.GetEarliestTripStart(noTripId); err != nil || start != nil {
			t.Errorf("no trips, no start: %v %v", start, err)
		}
		if start, err := tx.GetEarliestTripStart(oldVisitId); err != nil || start == nil || !start.Equal(*minutesAfterNine(60)) {
			t.Errorf("the earliest start: %v %v", start, err)
		}
		if err := tx.MarkTripsBackfilled(builtId, time.Now()); err != nil {
			t.Fatalf("MarkTripsBackfilled: %s", err)
		}
		listed, err := tx.ListUsersNeedingTripBackfill()
		if err != nil {
			t.Fatalf("ListUsersNeedingTripBackfill: %s", err)
		}
		has := map[string]bool{}
		for _, userId := range listed {
			has[userId] = true
		}
		if !has[noTripId] || !has[oldVisitId] || has[builtId] {
			t.Errorf("listed %v: want the people not yet backfilled, not the one marked", listed)
		}
	})
}

// Erasing a range of history erases the trips, legs, motion activity and
// corrections that started in it, and nothing else.
func TestErasingHistoryErasesTravelInTheRange(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	at := func(hours int) time.Time { return locationDay.Add(time.Duration(hours) * time.Hour) }
	var userId, otherUserId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId = locationUser(t, tx, "travel-erase")
		otherUserId = locationUser(t, tx, "travel-erase-other")
		bus := "bus"
		for _, who := range []string{userId, otherUserId} {
			putTrips(t, tx, who,
				travelTrip(at(0), travelLeg(at(0), 20, "car", 5000)),
				travelTrip(at(24), travelLeg(at(24), 20, "walk", 900)))
			if _, err := tx.PutMotionActivities(who, []*models.MotionActivity{
				{StartedAt: at(0), ActivityKind: "walking", ActivityConfidence: "high"},
				{StartedAt: at(24), ActivityKind: "walking", ActivityConfidence: "high"},
			}); err != nil {
				t.Fatalf("PutMotionActivities: %s", err)
			}
			for _, hours := range []int{0, 24} {
				if err := tx.SetTravelModeCorrection(who, at(hours), at(hours+1), &bus); err != nil {
					t.Fatalf("SetTravelModeCorrection: %s", err)
				}
			}
		}
	})
	count := func(table, who string) string {
		return dbtest.QueryString(t, database, `SELECT count(*) FROM "`+table+`" WHERE "user_id" = '`+who+`'`)
	}
	tables := []string{"trip", "trip_leg", "motion_activity", "travel_mode_correction"}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		until := at(12)
		if _, _, _, err := tx.EraseLocationHistory(userId, nil, &until); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
	})
	for _, table := range tables {
		if got := count(table, userId); got != "1" {
			t.Errorf("erasing the first day leaves %s rows in %s, want the second day's one", got, table)
		}
		if got := count(table, otherUserId); got != "2" {
			t.Errorf("somebody else's %s has %s rows, want 2", table, got)
		}
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, _, _, err := tx.EraseLocationHistory(userId, nil, nil); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
	})
	for _, table := range tables {
		if got := count(table, userId); got != "0" {
			t.Errorf("erasing everything leaves %s rows in %s", got, table)
		}
	}
}

// A page of trips says where the next one starts, and a trip is listed only
// when it overlaps the range.
func TestListingTripsPagesAndKeepsToTheRange(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-page")
		var trips []*models.Trip
		for index := 0; index < 5; index++ {
			startedAt := locationDay.Add(time.Duration(index) * time.Hour)
			trips = append(trips, travelTrip(startedAt, travelLeg(startedAt, 30, "car", 1000)))
		}
		putTrips(t, tx, userId, trips...)

		first, nextOffset, err := tx.ListTrips(userId, locationDay.Add(-time.Hour), locationDay.Add(24*time.Hour), 0, 2)
		if err != nil || len(first) != 2 || nextOffset == nil || *nextOffset != 2 || !first[0].StartedAt.After(first[1].StartedAt) {
			t.Fatalf("the first page, newest first: %+v %v %v", first, nextOffset, err)
		}
		last, nextOffset, err := tx.ListTrips(userId, locationDay.Add(-time.Hour), locationDay.Add(24*time.Hour), 4, 2)
		if err != nil || len(last) != 1 || nextOffset != nil {
			t.Fatalf("the last page: %+v %v %v", last, nextOffset, err)
		}
		// The trip at 09:00 ends 09:30: a range starting at 09:30 misses it, and
		// one starting at 09:29 holds it.
		missed, _, err := tx.ListTrips(userId, locationDay.Add(30*time.Minute), locationDay.Add(time.Hour), 0, 0)
		if err != nil || len(missed) != 0 {
			t.Fatalf("a trip that ended when the range begins is not in it: %+v %v", missed, err)
		}
		held, _, err := tx.ListTrips(userId, locationDay.Add(29*time.Minute), locationDay.Add(time.Hour), 0, 0)
		if err != nil || len(held) != 1 {
			t.Fatalf("a trip still under way when the range begins is in it: %+v %v", held, err)
		}
	})
}
