package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// errNothingLeftToSpend is a call the night's allowance cannot pay for.
// Every caller already stops on an error from a call, which is what
// should happen here too; it is named so that the reading can tell it
// from a model that went quiet.
var errNothingLeftToSpend = errors.New("the night has spent its share of the day")

// dreamBudget is what a night may spend.
//
// The allowance is the day's share less what today's earlier nights have
// already spent of it, not a fresh fraction each time. The share exists
// to leave the rest of the day to the person, and a night runs as often
// as every six hours: taken fresh, four nights spent four shares and the
// conversation they were protecting paid for it.
type dreamBudget struct {
	// Read and written from several batches at once when the reading
	// runs concurrently.
	mutex   sync.Mutex
	allowed int64
	spent   int64

	// reserved is what the calls in flight are assumed to cost until they
	// come back and say what they really cost.
	reserved int64

	// exhausted is a night whose share of the day was gone before it
	// began. Its own field because an allowance of zero has always meant
	// "nothing caps this", and today's earlier nights leaving nothing is
	// the opposite of that.
	exhausted bool

	// digestUntil is when the reading has to stop so the rest of the
	// night gets its turn; zero means the night has no deadline.
	digestUntil time.Time

	// refused is a provider that will not answer anything, because the
	// account it bills cannot pay. Nothing the night does next changes
	// that, so it counts as having nothing left to spend.
	refused bool
}

// whyItStopped is what to write on the night's row when the allowance,
// rather than the model, is what ended the reading.
func (self *dreamBudget) whyItStopped() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.refused {
		return "the provider would not bill this account; the reading stops here"
	}
	return "the night has spent its share of the day; the reading stops here"
}

// refuse records that the provider will not answer again tonight.
func (self *dreamBudget) refuse() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.refused {
		log.Warningf("the night stops: the provider will not bill this account")
	}
	self.refused = true
}

// readingStopReason is what to write on the night's row when the reading
// gives up.
//
// A turn the day's allowance stopped ends the same way as a model that
// went quiet: the loop notes why and returns without an error, so the
// batch comes back unanswered either way. Blaming the model sends the
// reader to a provider status page for something that is a number in the
// settings, so say what the budget says when the budget is the reason.
func readingStopReason(budget *Budget) string {
	if budget != nil {
		if spent := budget.exhaustedBy(); spent != "" {
			return spent + "; the reading stops here"
		}
	}
	return "the model did not answer; the reading stops here"
}

// budgetNow is the day's allowance as it stands, or nil if it cannot be
// read. Nil is not a failure worth ending a night over: it only means the
// row falls back to what it used to say.
func (self *Agent) budgetNow(ctx context.Context, run *Run) *Budget {
	var budget *Budget
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		budget, err = CheckBudget(tx, run.Configuration(), run.Agent, run.Owner, time.Now())
		return err
	}); err != nil {
		log.Debugf("cannot read the budget for a night that stopped reading: %s", err)
		return nil
	}
	return budget
}

// dreamCallEstimate is what one call of a night is held against the
// allowance while it is in flight.
//
// It only has to be the right order of magnitude: the real cost replaces
// it the moment the call returns, and its job is to stop three batches
// reading at once from each being told the whole remainder is free. A
// batch of documents is the largest prompt a night sends and the answer
// is capped at four thousand tokens, so this is about one of those.
const dreamCallEstimate = 10000

// partway is the moment a share of the night's remaining time is gone,
// or zero when the night has no deadline.
func partway(ctx context.Context, now time.Time, share float64) time.Time {
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(now) {
		return time.Time{}
	}
	return now.Add(time.Duration(float64(deadline.Sub(now)) * share))
}

// halfway is partway at a half.
func halfway(ctx context.Context, now time.Time) time.Time {
	return partway(ctx, now, 0.5)
}

// readingTimeLeft says whether the reading may go on.
func (self *dreamBudget) readingTimeLeft() bool {
	return self.digestUntil.IsZero() || time.Now().Before(self.digestUntil)
}

// newDreamBudget is the share of the day this night may have, given what
// the day's earlier nights have already spent of it.
func newDreamBudget(configuration *config.Configuration, agent *models.Agent, share float64, spentDreaming int64) *dreamBudget {
	daily := agent.DailyTokens
	if daily <= 0 {
		daily = configuration.Agent.Limits.DailyTokensPerAgent
	}
	if daily <= 0 {
		return &dreamBudget{allowed: 0} // no cap
	}
	allowed := int64(float64(daily)*share) - spentDreaming
	if allowed <= 0 {
		return &dreamBudget{exhausted: true}
	}
	return &dreamBudget{allowed: allowed}
}

// left says whether there is budget for another call, counting what the
// calls in flight have claimed.
// spentSoFar is what the night has spent, for a record written while the
// night is still going. Under the lock, because the batches settle their
// own calls from several goroutines.
func (self *dreamBudget) spentSoFar() int64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.spent
}

func (self *dreamBudget) left() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.room()
}

// room is left without the lock, for a caller that already holds it.
func (self *dreamBudget) room() bool {
	if self.exhausted || self.refused {
		return false
	}
	return self.allowed == 0 || self.spent+self.reserved < self.allowed
}

// reserve claims the estimate for a call about to be made and says
// whether there was room for it. Asking and claiming happen under the one
// lock, which is the whole point of it.
func (self *dreamBudget) reserve() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.room() {
		return false
	}
	self.reserved += dreamCallEstimate
	return true
}

// settle gives back the estimate and books what the call really cost.
func (self *dreamBudget) settle(usage llm.Usage) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.reserved = max(self.reserved-dreamCallEstimate, 0)
	self.spent += int64(usage.PromptTokens + usage.CompletionTokens)
}
