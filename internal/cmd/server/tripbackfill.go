package server

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/travel/rebuild"
)

// Building the trips of days recorded before this server knew what a trip was.
//
// Each report builds the trips of the days it touches, so trips exist from
// the day this shipped. The days before have visits and positions but no
// trips, and a person's distance per week or year would start empty. Once, at
// start: for each person with visits not marked as done (trip_backfill) since
// tripRulesChangedAt, this builds their trips back from now, a week at a
// time, skipping weeks with no visit, and marks them. A start after the first
// finds nobody. The same pass builds every past trip again after the rules
// change, when tripRulesChangedAt moves; a rebuild keeps the ids of trips
// that come out the same, and corrections are kept apart, so it loses
// nothing.

// tripBackfillWindow is how much is built in one transaction.
const tripBackfillWindow = 7 * 24 * time.Hour

// tripRulesChangedAt is when the rules in package travel last changed in a
// way that changes trips already built. Move it to a moment before the
// change is deployed whenever they do.
var tripRulesChangedAt = time.Date(2026, 10, 27, 10, 40, 0, 0, time.UTC)

// backfillTrips builds the trips of past days for every person who needs it.
func backfillTrips(ctx context.Context, database db.Database) {
	var userIds []string
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		userIds, err = tx.ListUsersNeedingTripBackfill(tripRulesChangedAt)
		return err
	}); err != nil {
		if ctx.Err() == nil {
			log.Warningf("failed to look for people whose trips were never built: %s", err)
		}
		return
	}
	builtCount := 0
	for _, userId := range userIds {
		if ctx.Err() != nil {
			return
		}
		if err := backfillTripsOf(ctx, database, userId); err != nil {
			if ctx.Err() == nil {
				log.Warningf("failed to build the past trips of user %s: %s", userId, err)
			}
			continue
		}
		builtCount++
	}
	if builtCount > 0 {
		log.Noticef("built the past trips of %d people", builtCount)
	}
}

// backfillTripsOf builds one person's trips from now back to their earliest
// visit, a week at a time, each week in its own transaction holding their
// location lock.
func backfillTripsOf(ctx context.Context, database db.Database, userId string) error {
	windowEnd := time.Now()
	for ctx.Err() == nil {
		isDone := false
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			// The latest visit before the window's end says where the next
			// week to build is, so a year with a month of no visits is not
			// walked a week at a time.
			visits, _, err := tx.ListPlaceVisits(userId, db.PlaceVisitFilter{Until: &windowEnd, LimitCount: 1})
			if err != nil {
				return err
			}
			if len(visits) == 0 {
				isDone = true
				return nil
			}
			if latest := visitTime(visits[0]); latest != nil && latest.Add(time.Second).Before(windowEnd) {
				windowEnd = latest.Add(time.Second)
			}
			if err := tx.LockLocation(userId); err != nil {
				return err
			}
			return rebuild.Window(tx, userId, windowEnd.Add(-tripBackfillWindow), windowEnd)
		}); err != nil {
			return err
		}
		if isDone {
			return database.TransactionContext(ctx, func(tx db.Transaction) error {
				return tx.MarkTripsBackfilled(userId, time.Now())
			})
		}
		windowEnd = windowEnd.Add(-tripBackfillWindow)
	}
	return ctx.Err()
}

// visitTime is the latest moment a visit says: its departure, which is where
// the trip after it starts, or its arrival while it has none.
func visitTime(visit *models.PlaceVisit) *time.Time {
	if visit.DepartedAt != nil {
		return visit.DepartedAt
	}
	return visit.ArrivedAt
}
