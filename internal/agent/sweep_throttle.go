package agent

import (
	"time"

	"github.com/ziyan/teanode/internal/db"
)

// takeSweep takes the turn of a sweep that every instance runs on its own
// clock, once every interval for the whole server, in the transaction that
// does the sweep's first step: a sweep whose step fails gives the turn back.
//
// The turn lasts a tenth less than the interval. Each instance runs its sweep
// every interval from its own start, so the instances are out of step with
// one another; with the full interval the turn taken by one a moment before
// another's would be refused to that one, which would then wait another whole
// interval for its next try.
// sweepRetry is how soon a sweep another instance had the turn of is tried
// again: an instance restarted more often than the sweep's interval would
// otherwise never be the one to run it.
const sweepRetry = 10 * time.Minute

func takeSweep(tx db.Transaction, sweepName string, interval time.Duration, now time.Time) (bool, error) {
	return tx.TakeThrottle("agent-sweep:"+sweepName, interval-interval/10, now)
}
