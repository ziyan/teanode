package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A trip that starts when a stored one does keeps its id, and so does a leg
// that starts when a stored leg does, so that an id a person was shown still
// works after the day is built again. A trip that is the stored one again is
// not written.
func TestReplacingTripsKeepsTheIdsOfTripsAndLegsThatStartTogether(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-stable")
		at := func(minutes int) time.Time { return locationDay.Add(time.Duration(minutes) * time.Minute) }
		windowStart, windowEnd := at(-60), at(600)
		replace := func(trips ...*models.Trip) []*models.Trip {
			t.Helper()
			if err := tx.ReplaceTrips(userId, windowStart, windowEnd, trips); err != nil {
				t.Fatalf("ReplaceTrips: %s", err)
			}
			stored, _, err := tx.ListTrips(userId, windowStart, windowEnd, 0, 0)
			if err != nil {
				t.Fatalf("ListTrips: %s", err)
			}
			// Oldest first.
			for first, last := 0, len(stored)-1; first < last; first, last = first+1, last-1 {
				stored[first], stored[last] = stored[last], stored[first]
			}
			return stored
		}
		before := replace(
			travelTrip(at(0), travelLeg(at(0), 5, "walk", 400), travelLeg(at(5), 15, "car", 3000)),
			travelTrip(at(300), travelLeg(at(300), 20, "car", 5000)))
		if len(before) != 2 || len(before[0].Legs) != 2 {
			t.Fatalf("two trips: %+v", before)
		}

		// The same trips with distances a few centimeters apart: nothing is
		// written, so the stored distance stays.
		again := travelTrip(at(0), travelLeg(at(0), 5, "walk", 400.4), travelLeg(at(5), 15, "car", 3000.4))
		afterSame := replace(again, travelTrip(at(300), travelLeg(at(300), 20, "car", 5000.4)))
		if afterSame[0].ID != before[0].ID || afterSame[0].DistanceMeters != 3400 || afterSame[0].Legs[1].DistanceMeters != 3000 {
			t.Errorf("an equal trip is left as it was: %+v", afterSame[0])
		}

		// The first trip now has its legs cut elsewhere: the leg that starts
		// when one did keeps its id, a new leg gets one, and the old one that
		// starts nowhere now is gone. The second trip is not given and goes;
		// a third that starts elsewhere is new.
		changed := travelTrip(at(0), travelLeg(at(0), 8, "walk", 700), travelLeg(at(8), 12, "bike", 2700))
		afterChange := replace(changed, travelTrip(at(400), travelLeg(at(400), 10, "car", 2000)))
		if len(afterChange) != 2 || afterChange[0].ID != before[0].ID {
			t.Fatalf("the first trip keeps its id: %+v", afterChange)
		}
		if legs := afterChange[0].Legs; len(legs) != 2 || legs[0].ID != before[0].Legs[0].ID || !legs[0].EndedAt.Equal(at(8)) ||
			legs[1].ID == before[0].Legs[1].ID || legs[1].TravelMode != "bike" {
			t.Fatalf("the leg that starts together keeps its id, the other is new: %+v", legs)
		}
		if afterChange[1].ID == before[1].ID {
			t.Errorf("a trip that starts elsewhere has a new id")
		}
		if gone, err := tx.GetTripLeg(userId, before[0].Legs[1].ID); err != nil || gone != nil {
			t.Errorf("the leg that starts nowhere now is deleted: %+v %v", gone, err)
		}
		if gone, err := tx.GetTripLeg(userId, before[1].Legs[0].ID); err != nil || gone != nil {
			t.Errorf("the legs of the trip that was not given are deleted: %+v %v", gone, err)
		}
	})
}

// Correcting the second leg of an older correction that covers two legs
// leaves the first leg corrected as it was.
func TestCorrectingOneLegKeepsTheRestOfAnOlderCorrection(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-trim")
		minute := func(offset int) time.Time { return locationDay.Add(time.Duration(offset) * time.Minute) }
		putTrips(t, tx, userId, travelTrip(minute(0),
			travelLeg(minute(0), 10, "car", 3000), travelLeg(minute(10), 10, "car", 3000), travelLeg(minute(20), 10, "car", 3000)))
		modes := func() []string {
			t.Helper()
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
		bus, train, boat := "bus", "train", "boat"
		set(0, 30, &bus)
		equal(modes(), "bus", "bus", "bus")
		// The middle of a correction: both sides are kept.
		set(10, 20, &train)
		equal(modes(), "bus", "train", "bus")
		// The end of one, then the start of one.
		set(20, 30, &boat)
		equal(modes(), "bus", "train", "boat")
		set(0, 10, nil)
		equal(modes(), "car", "train", "boat")
		// A span over the start of an older correction takes only the overlap.
		set(12, 20, &bus)
		equal(modes(), "car", "bus", "boat")
	})
}

// A trip that began before an erased range and ended inside it is erased
// with it.
func TestErasingHistoryErasesATripThatEndedInsideTheRange(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-erase-overlap")
		minute := func(offset int) time.Time { return locationDay.Add(time.Duration(offset) * time.Minute) }
		putTrips(t, tx, userId,
			travelTrip(minute(0), travelLeg(minute(0), 60, "car", 9000)),
			travelTrip(minute(300), travelLeg(minute(300), 20, "car", 5000)),
			travelTrip(minute(600), travelLeg(minute(600), 20, "car", 5000)))
		from, until := minute(30), minute(400)
		if _, _, _, err := tx.EraseLocationHistory(userId, &from, &until); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
		trips, _, err := tx.ListTrips(userId, minute(-60), minute(1000), 0, 0)
		if err != nil || len(trips) != 1 || !trips[0].StartedAt.Equal(minute(600)) {
			t.Fatalf("only the trip after the range stays: %+v %v", trips, err)
		}
		// The range's edges: a trip that begins at its end, or ends at its
		// start, is not part of it.
		putTrips(t, tx, userId,
			travelTrip(minute(0), travelLeg(minute(0), 30, "car", 3000)),
			travelTrip(minute(400), travelLeg(minute(400), 20, "car", 3000)))
		if _, _, _, err := tx.EraseLocationHistory(userId, &from, &until); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
		if trips, _, err = tx.ListTrips(userId, minute(-60), minute(1000), 0, 0); err != nil || len(trips) != 2 {
			t.Fatalf("a trip that ended when the range began, and one that began when it ended, stay: %+v %v", trips, err)
		}
	})
}

// Trips that began more than TripLongestDuration before the range are not
// looked at: none lasts that long.
func TestListingTripsLeavesOutTheOnesThatBeganTooLongBefore(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "travel-bound")
		day := 24 * time.Hour
		rangeStart := locationDay
		recent := &models.Trip{StartedAt: rangeStart.Add(-29 * day), EndedAt: rangeStart.Add(time.Hour), DistanceMeters: 1000,
			Legs: []*models.TripLeg{{StartedAt: rangeStart.Add(-29 * day), EndedAt: rangeStart.Add(time.Hour), DetectedTravelMode: "car", DetectionSource: "speed", DistanceMeters: 1000}}}
		old := &models.Trip{StartedAt: rangeStart.Add(-31 * day), EndedAt: rangeStart.Add(time.Hour), DistanceMeters: 1000,
			Legs: []*models.TripLeg{{StartedAt: rangeStart.Add(-31 * day), EndedAt: rangeStart.Add(time.Hour), DetectedTravelMode: "car", DetectionSource: "speed", DistanceMeters: 1000}}}
		putTrips(t, tx, userId, recent, old)
		trips, _, err := tx.ListTrips(userId, rangeStart, rangeStart.Add(day), 0, 0)
		if err != nil || len(trips) != 1 || !trips[0].StartedAt.Equal(recent.StartedAt) {
			t.Fatalf("only the trip within the bound: %+v %v", trips, err)
		}
	})
}
