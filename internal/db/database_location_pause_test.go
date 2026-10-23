package db_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// Pausing starts a pause now, and pausing again while it is in effect changes
// its end and adds nothing, to a time or to none.
func TestPausingWhilePausedChangesThePauseInEffect(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-pause-twice")
		now := locationDay
		endedAt := now.Add(time.Hour)

		first, isStarted, err := tx.PauseLocation(userId, &endedAt, now)
		if err != nil || !isStarted || first.ID == "" || first.UserID != userId || !first.StartedAt.Equal(now) || first.EndedAt == nil || !first.EndedAt.Equal(endedAt) {
			t.Fatalf("the first pause: %+v started %v, %v", first, isStarted, err)
		}

		later := now.Add(10 * time.Minute)
		laterEnd := later.Add(3 * time.Hour)
		changed, isStarted, err := tx.PauseLocation(userId, &laterEnd, later)
		if err != nil || isStarted || changed.ID != first.ID || !changed.StartedAt.Equal(now) || !changed.EndedAt.Equal(laterEnd) {
			t.Fatalf("pausing while paused changes the end: %+v started %v, %v", changed, isStarted, err)
		}

		open, isStarted, err := tx.PauseLocation(userId, nil, later)
		if err != nil || isStarted || open.ID != first.ID || open.EndedAt != nil {
			t.Fatalf("pausing until resumed takes the end away: %+v started %v, %v", open, isStarted, err)
		}

		pauses, err := tx.ListLocationPauses(userId, now.Add(-time.Hour), now.Add(24*time.Hour), 0)
		if err != nil || len(pauses) != 1 {
			t.Fatalf("one pause, not three: %+v %v", pauses, err)
		}
	})
}

// A pause ends after now and at most thirty days from now, and a refusal
// stores nothing.
func TestAPauseEndsAfterNowAndWithinThirtyDays(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-pause-bounds")
		now := locationDay
		past, equal, tooFar := now.Add(-time.Minute), now, now.Add(db.LocationPauseLongest+time.Second)
		for name, endedAt := range map[string]*time.Time{"in the past": &past, "now": &equal, "past thirty days": &tooFar} {
			if _, _, err := tx.PauseLocation(userId, endedAt, now); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("a pause ending %s is refused: %v", name, err)
			}
		}
		if pause, err := tx.GetLocationPauseInEffect(userId, now.Add(time.Second)); err != nil || pause != nil {
			t.Errorf("a refused pause stores nothing: %+v %v", pause, err)
		}
		longest := now.Add(db.LocationPauseLongest)
		if _, _, err := tx.PauseLocation(userId, &longest, now); err != nil {
			t.Errorf("exactly thirty days is allowed: %s", err)
		}
	})
}

// A pause is in effect from its start up to, not including, its end, and an
// open one until it is resumed. A resumed pause ends at the resume.
func TestAPauseIsInEffectFromItsStartUntilItsEndOrTheResume(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		timedUserId := locationUser(t, tx, "location-pause-timed")
		openUserId := locationUser(t, tx, "location-pause-open")
		startedAt := locationDay
		endedAt := startedAt.Add(time.Hour)
		if _, _, err := tx.PauseLocation(timedUserId, &endedAt, startedAt); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}
		if _, _, err := tx.PauseLocation(openUserId, nil, startedAt); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}
		inEffectAt := func(userId string, at time.Time) bool {
			t.Helper()
			pause, err := tx.GetLocationPauseInEffect(userId, at)
			if err != nil {
				t.Fatalf("GetLocationPauseInEffect: %s", err)
			}
			return pause != nil
		}
		for name, test := range map[string]struct {
			userId string
			at     time.Time
			want   bool
		}{
			"timed, before the start": {timedUserId, startedAt.Add(-time.Second), false},
			"timed, at the start":     {timedUserId, startedAt, true},
			"timed, inside":           {timedUserId, startedAt.Add(30 * time.Minute), true},
			"timed, at the end":       {timedUserId, endedAt, false},
			"open, a week later":      {openUserId, startedAt.AddDate(0, 0, 7), true},
			"open, before the start":  {openUserId, startedAt.Add(-time.Second), false},
		} {
			if got := inEffectAt(test.userId, test.at); got != test.want {
				t.Errorf("%s: in effect is %v, want %v", name, got, test.want)
			}
		}

		resumedAt := startedAt.Add(20 * time.Minute)
		if isResumed, err := tx.ResumeLocation(timedUserId, resumedAt); err != nil || !isResumed {
			t.Fatalf("resuming a timed pause early: %v %v", isResumed, err)
		}
		if inEffectAt(timedUserId, resumedAt) || !inEffectAt(timedUserId, resumedAt.Add(-time.Second)) {
			t.Errorf("a resumed pause ends at the resume")
		}
		pauses, err := tx.ListLocationPauses(timedUserId, startedAt.Add(-time.Hour), startedAt.Add(time.Hour), 0)
		if err != nil || len(pauses) != 1 || pauses[0].EndedAt == nil || !pauses[0].EndedAt.Equal(resumedAt) {
			t.Errorf("the resumed pause is kept, ended at the resume: %+v %v", pauses, err)
		}
		if isResumed, err := tx.ResumeLocation(timedUserId, resumedAt.Add(time.Minute)); err != nil || isResumed {
			t.Errorf("resuming when not paused finds nothing: %v %v", isResumed, err)
		}

		// A pause ended the moment it began lasted no time and is not kept.
		if isResumed, err := tx.ResumeLocation(openUserId, startedAt); err != nil || !isResumed {
			t.Fatalf("resuming at the start: %v %v", isResumed, err)
		}
		if pauses, err := tx.ListLocationPauses(openUserId, startedAt.Add(-time.Hour), startedAt.AddDate(0, 0, 1), 0); err != nil || len(pauses) != 0 {
			t.Errorf("a pause that lasted no time is gone: %+v %v", pauses, err)
		}
	})
}

// The pauses of a range are the ones that overlap it, newest first, and an
// open pause reaches every range after its start.
func TestPausesOverlappingARangeAreTheOnesThatTouchIt(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-pause-range")
		otherUserId := locationUser(t, tx, "location-pause-range-other")
		hour := func(offset int) time.Time { return locationDay.Add(time.Duration(offset) * time.Hour) }
		// Three pauses: 0 to 1 h, 3 to 4 h, and one from 10 h that lasts until resumed.
		for _, span := range [][2]int{{0, 1}, {3, 4}, {10, -1}} {
			var endedAt *time.Time
			if span[1] >= 0 {
				ended := hour(span[1])
				endedAt = &ended
			}
			if _, _, err := tx.PauseLocation(userId, endedAt, hour(span[0])); err != nil {
				t.Fatalf("PauseLocation: %s", err)
			}
			if span[1] >= 0 {
				// Ending in the past of the next call, so the next is a new pause.
				if _, err := tx.ResumeLocation(userId, hour(span[1])); err != nil {
					t.Fatalf("ResumeLocation: %s", err)
				}
			}
		}
		if _, _, err := tx.PauseLocation(otherUserId, nil, hour(2)); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}

		startsOf := func(from, until time.Time, limitCount int) []int {
			t.Helper()
			pauses, err := tx.ListLocationPauses(userId, from, until, limitCount)
			if err != nil {
				t.Fatalf("ListLocationPauses: %s", err)
			}
			starts := []int{}
			for _, pause := range pauses {
				starts = append(starts, int(pause.StartedAt.Sub(locationDay)/time.Hour))
			}
			return starts
		}
		for name, test := range map[string]struct {
			from, until time.Time
			limitCount  int
			want        []int
		}{
			"everything":                {hour(-1), hour(100), 0, []int{10, 3, 0}},
			"between the first two":     {hour(1), hour(3), 0, []int{}},
			"touching the end of one":   {hour(1), hour(3).Add(time.Second), 0, []int{3}},
			"inside the second":         {hour(3).Add(10 * time.Minute), hour(3).Add(20 * time.Minute), 0, []int{3}},
			"after the open one began":  {hour(50), hour(60), 0, []int{10}},
			"before everything":         {hour(-5), hour(0), 0, []int{}},
			"limited to the newest two": {hour(-1), hour(100), 2, []int{10, 3}},
		} {
			got := startsOf(test.from, test.until, test.limitCount)
			if len(got) != len(test.want) {
				t.Errorf("%s: pauses starting at hours %v, want %v", name, got, test.want)
				continue
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("%s: pauses starting at hours %v, want %v", name, got, test.want)
					break
				}
			}
		}
	})
}

// The pauses in effect for many people come in one answer, and a person who is
// not paused is not in it.
func TestThePausesInEffectOfManyPeopleComeTogether(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		timedUserId := locationUser(t, tx, "location-pause-many-timed")
		openUserId := locationUser(t, tx, "location-pause-many-open")
		overUserId := locationUser(t, tx, "location-pause-many-over")
		freeUserId := locationUser(t, tx, "location-pause-many-free")
		now := locationDay
		endedAt := now.Add(time.Hour)
		overEndedAt := now.Add(2 * time.Minute)
		for userId, ended := range map[string]*time.Time{timedUserId: &endedAt, openUserId: nil, overUserId: &overEndedAt} {
			if _, _, err := tx.PauseLocation(userId, ended, now); err != nil {
				t.Fatalf("PauseLocation: %s", err)
			}
		}

		inEffect, err := tx.GetLocationPausesInEffect([]string{timedUserId, openUserId, overUserId, freeUserId, timedUserId, ""}, now.Add(time.Minute))
		if err != nil {
			t.Fatalf("GetLocationPausesInEffect: %s", err)
		}
		if len(inEffect) != 3 || inEffect[timedUserId] == nil || inEffect[openUserId] == nil || inEffect[overUserId] == nil || inEffect[freeUserId] != nil {
			t.Errorf("three are paused: %+v", inEffect)
		}
		// At the end of the shortest one it is over, and the others are not.
		inEffect, err = tx.GetLocationPausesInEffect([]string{timedUserId, openUserId, overUserId}, overEndedAt)
		if err != nil || len(inEffect) != 2 || inEffect[overUserId] != nil || inEffect[timedUserId] == nil || inEffect[openUserId] == nil {
			t.Errorf("a timed pause is over at its end: %+v %v", inEffect, err)
		}
		if empty, err := tx.GetLocationPausesInEffect(nil, now); err != nil || len(empty) != 0 {
			t.Errorf("nobody asked about: %+v %v", empty, err)
		}
	})
}

// Erasing history erases the pauses that began in the range and are over, and
// leaves the pause in effect alone, so erasing does not resume recording.
func TestErasingHistoryErasesPastPausesAndLeavesThePauseInEffect(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		userId := locationUser(t, tx, "location-pause-erase")
		otherUserId := locationUser(t, tx, "location-pause-erase-other")
		hour := func(offset int) time.Time { return locationDay.Add(time.Duration(offset) * time.Hour) }
		for _, span := range [][2]int{{0, 1}, {24, 25}} {
			ended := hour(span[1])
			if _, _, err := tx.PauseLocation(userId, &ended, hour(span[0])); err != nil {
				t.Fatalf("PauseLocation: %s", err)
			}
		}
		if _, _, err := tx.PauseLocation(otherUserId, nil, hour(0)); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}
		// The pause in effect began before the erased range and ends after it.
		now := time.Now()
		inEffectEnd := now.Add(time.Hour)
		if _, _, err := tx.PauseLocation(userId, &inEffectEnd, now); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}

		until := hour(12)
		if _, _, _, err := tx.EraseLocationHistory(userId, nil, &until); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
		pauses, err := tx.ListLocationPauses(userId, hour(-1), now.Add(24*time.Hour), 0)
		if err != nil || len(pauses) != 2 {
			t.Fatalf("the first past pause is erased, the second and the one in effect stay: %+v %v", pauses, err)
		}

		if _, _, _, err := tx.EraseLocationHistory(userId, nil, nil); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
		pauses, err = tx.ListLocationPauses(userId, hour(-1), now.Add(24*time.Hour), 0)
		if err != nil || len(pauses) != 1 || !pauses[0].IsInEffectAt(now.Add(time.Minute)) {
			t.Fatalf("erasing everything leaves the pause in effect: %+v %v", pauses, err)
		}
		// Somebody else's pause is not touched, and one that lasts until resumed
		// is never over.
		if _, _, _, err := tx.EraseLocationHistory(otherUserId, nil, nil); err != nil {
			t.Fatalf("EraseLocationHistory: %s", err)
		}
		if pause, err := tx.GetLocationPauseInEffect(otherUserId, now); err != nil || pause == nil {
			t.Errorf("a pause until resumed stays: %+v %v", pause, err)
		}
	})
}

// Two visits to one place less than ten minutes apart are one visit, unless
// a pause began between them: nobody knows where the person was in it, however
// short it was.
func TestVisitsToOnePlaceAreNotJoinedAcrossAPause(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		pausedUserId := locationUser(t, tx, "location-pause-join-paused")
		freeUserId := locationUser(t, tx, "location-pause-join-free")
		for _, userId := range []string{pausedUserId, freeUserId} {
			home := locationPlace(t, tx, userId, "home", 10.5)
			putAndNormalize(t, tx, userId, locationVisit(home.ID, "before", 0, 60))
			if userId == pausedUserId {
				endedAt := *minutesAfterNine(63)
				if _, _, err := tx.PauseLocation(userId, &endedAt, *minutesAfterNine(60)); err != nil {
					t.Fatalf("PauseLocation: %s", err)
				}
			}
			putAndNormalize(t, tx, userId, locationVisit(home.ID, "after", 67, 120))
		}
		if got, want := fmt.Sprint(visitSpansOf(t, tx, pausedUserId)), "[home 0-60 home 67-120]"; got != want {
			t.Errorf("across a pause the visits stay two: %s, want %s", got, want)
		}
		if got, want := fmt.Sprint(visitSpansOf(t, tx, freeUserId)), "[home 0-120]"; got != want {
			t.Errorf("without a pause the visits are one: %s, want %s", got, want)
		}
	})
}
