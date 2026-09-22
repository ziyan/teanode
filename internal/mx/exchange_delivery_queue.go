package mx

import (
	"time"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

// wakeDeliveryQueue is safe inside an AfterCommit callback: the database keeps
// the work and this coalesced notification only shortens the polling delay.
func (self *exchange) wakeDeliveryQueue() {
	select {
	case self.deliveryWake <- struct{}{}:
	default:
	}
}

func (self *exchange) runDeliveryQueue() {
	defer deferutil.Recover()
	defer self.waitGroup.Done()
	for self.ctx.Err() == nil {
		deliveryCount, err := self.deliverBatch(self.ctx)
		if self.ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Errorf("cannot process queued deliveries: %s", err)
		} else if deliveryCount > 0 {
			// Drain a backlog without an idle interval, while keeping one batch active.
			continue
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-self.ctx.Done():
			timer.Stop()
			return
		case <-self.deliveryWake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
