package server

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/travel/rebuild"
)

// Putting in order the visits that were stored overlapping.
//
// Each report puts the person's visits around its own times in order
// (NormalizePlaceVisits). Before that reached back to the arrival of a visit
// a late departure completed, a departure that came hours late could carry a
// visit over the stops made since, and the stored visits still overlap.
//
// Once, at start: this finds each person with a visit that began inside a
// visit of theirs at another place, or a departure from a watched place
// noticed inside one, and puts that stretch in order, the same way a report
// does, and builds its trips again. Visits put in order overlap none, so a start after the
// first finds nothing and costs one query.

// visitRepairDays is how far back the look reaches, by arrival.
const visitRepairDays = 90

// repairOverlappedVisits puts in order every person's visits that overlap,
// one person at a time, each holding that person's location lock so that a
// report arriving meanwhile waits for it.
func repairOverlappedVisits(ctx context.Context, database db.Database) {
	var ranges []db.VisitRange
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		ranges, err = tx.ListOverlappedVisitRanges(time.Now().AddDate(0, 0, -visitRepairDays))
		return err
	}); err != nil {
		if ctx.Err() == nil {
			log.Warningf("failed to look for visits stored overlapping: %s", err)
		}
		return
	}
	repairedCount := 0
	for _, visitRange := range ranges {
		if ctx.Err() != nil {
			return
		}
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			if err := tx.LockLocation(visitRange.UserID); err != nil {
				return err
			}
			if err := tx.NormalizePlaceVisits(visitRange.UserID, visitRange.From, visitRange.Until); err != nil {
				return err
			}
			// The trips between the visits put in order are built again.
			return rebuild.Around(tx, visitRange.UserID, visitRange.From, visitRange.Until)
		}); err != nil {
			log.Warningf("failed to put in order the visits of user %s from %s to %s: %s", visitRange.UserID, visitRange.From, visitRange.Until, err)
			continue
		}
		repairedCount++
	}
	if repairedCount > 0 {
		log.Noticef("put in order the overlapping visits of %d people", repairedCount)
	}
}
