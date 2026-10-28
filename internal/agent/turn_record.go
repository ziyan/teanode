package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/periodic"
)

// A turn runs on one instance, and the others have to know it is running:
// to leave the conversation to it, to stop it when the conversation is
// deleted, to send it a person's answer, and to carry it on when the
// instance running it stops. Each turn is recorded in agent_turn while it
// runs, and each instance says every so often that it is still running its
// own. A turn its instance stopped with is marked cut; a turn whose instance
// has not been heard of for turnStaleAfter was lost with it.

const (
	// turnHeartbeatEvery is how often an instance says it is still running
	// its turns.
	turnHeartbeatEvery = 30 * time.Second

	// turnStaleAfter is how long a turn's instance may go unheard of before
	// the turn counts as lost with it.
	turnStaleAfter = 3 * turnHeartbeatEvery

	// turnRecordWait is how long recording a turn's end may take.
	turnRecordWait = 10 * time.Second

	// turnElsewherePoll is how often a turn waiting for one on another
	// instance looks again.
	turnElsewherePoll = time.Second
)

// startTurnRecords marks what this instance was running when it last
// stopped as cut, for the next look to carry on at once, and begins saying
// that the turns it runs now are still running. Nothing else can be running
// under this instance's name: the server holds the name while it runs.
func (self *Agent) startTurnRecords() {
	if self.settings.Database == nil {
		return
	}
	if err := self.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) error {
		cutCount, err := tx.CutAgentTurns(self.settings.Instance, time.Now())
		if cutCount > 0 {
			log.Noticef("%d turn(s) this instance was running before it restarted wait to be carried on", cutCount)
		}
		return err
	}); err != nil {
		log.Warningf("cannot mark the turns this instance was running before it restarted: %s", err)
	}
	periodic.New(self.ctx, &self.waitGroup, func(ctx context.Context) error {
		// Taken before the runs are read, so that a turn recorded after
		// is not mistaken for one that ended.
		now := time.Now()
		running := self.runningTurns()
		return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.HeartbeatAgentTurns(self.settings.Instance, running, now)
		})
	}, &periodic.Settings{Interval: turnHeartbeatEvery, Name: "agent:turn-heartbeat", SkipInitialRun: true}).Start()
}

// runningTurns is the record of each run this instance is running now. The
// heartbeat writes exactly these: a record whose end was not written (the
// database did not answer) is dropped, and one another instance took for cut
// while this one was too slow to say otherwise is written back.
func (self *Agent) runningTurns() []*models.AgentTurn {
	self.runsMutex.Lock()
	defer self.runsMutex.Unlock()
	var running []*models.AgentTurn
	for _, run := range self.runs {
		if run.recordedAt.IsZero() || run.isFinished() {
			continue
		}
		running = append(running, &models.AgentTurn{
			RunID: run.ID, ConversationID: run.settings.Conversation.ID, Instance: self.settings.Instance,
			StartedAt: run.recordedAt,
		})
	}
	return running
}

// recordTurn records a run as running here. A turn that is not recorded
// still runs; the other instances just cannot see it.
func (self *Agent) recordTurn(run *AskRun) {
	if self.settings.Database == nil || run.settings.Conversation == nil || run.settings.Conversation.ID == "" {
		return
	}
	// As the database keeps it, so that two turns compare their starts the
	// same way here and there.
	now := time.Now().Truncate(time.Microsecond)
	// Under the lock the heartbeat reads it with.
	self.runsMutex.Lock()
	run.recordedAt = now
	self.runsMutex.Unlock()
	if err := self.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) error {
		return tx.PutAgentTurn(&models.AgentTurn{
			RunID: run.ID, ConversationID: run.settings.Conversation.ID, Instance: self.settings.Instance,
			StartedAt: now, HeartbeatAt: now,
		})
	}); err != nil {
		log.Warningf("cannot record the turn %q as running: %s", run.ID, err)
	}
}

// forgetTurn forgets a run that ended. One the server stopping cut short is
// kept and marked cut instead, for this or another instance to carry on.
func (self *Agent) forgetTurn(run *AskRun) {
	if self.settings.Database == nil || run.recordedAt.IsZero() {
		return
	}
	isCut := self.ctx.Err() != nil
	// Bounded, because the turn's end waits for it: a database that does not
	// answer leaves the record for the heartbeat to put right.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), turnRecordWait)
	defer cancel()
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if !isCut {
			return tx.DeleteAgentTurn(run.ID)
		}
		cutAt := time.Now()
		return tx.PutAgentTurn(&models.AgentTurn{
			RunID: run.ID, ConversationID: run.settings.Conversation.ID, Instance: self.settings.Instance,
			StartedAt: run.recordedAt, HeartbeatAt: cutAt, CutAt: &cutAt,
		})
	}); err != nil {
		log.Warningf("cannot forget the turn %q: %s", run.ID, err)
	}
}

// turnsElsewhere is the turns of a conversation running on other instances.
func (self *Agent) turnsElsewhere(ctx context.Context, conversationId string) []*models.AgentTurn {
	if self.settings.Database == nil || conversationId == "" {
		return nil
	}
	var live []*models.AgentTurn
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		live, err = tx.ListLiveAgentTurns(conversationId, time.Now().Add(-turnStaleAfter))
		return err
	}); err != nil {
		log.Warningf("cannot read the turns running elsewhere in conversation %q: %s", conversationId, err)
		return nil
	}
	elsewhere := live[:0]
	for _, turn := range live {
		if turn.Instance != self.settings.Instance {
			elsewhere = append(elsewhere, turn)
		}
	}
	return elsewhere
}

// isTurnRunningElsewhere says whether another instance runs a turn in the
// conversation.
func (self *Agent) isTurnRunningElsewhere(conversationId string) bool {
	return len(self.turnsElsewhere(self.ctx, conversationId)) > 0
}

// waitForTurnsElsewhere waits, as a turn waits for the one before it here,
// until no turn that began before this one runs in the conversation on
// another instance. It says false when the run was stopped meanwhile. What
// the person writes while their turn runs on another instance is not handed
// to that turn, as it would be here: it is answered after it.
func (self *AskRun) waitForTurnsElsewhere() bool {
	if self.recordedAt.IsZero() {
		return true
	}
	hasSaidQueued := false
	for {
		isBehind := false
		elsewhere := self.agent.turnsElsewhere(self.ctx, self.settings.Conversation.ID)
		// Stopped while asking: the answer is no answer.
		if self.ctx.Err() != nil {
			return false
		}
		for _, turn := range elsewhere {
			if turn.StartedAt.Before(self.recordedAt) || turn.StartedAt.Equal(self.recordedAt) && turn.RunID < self.ID {
				isBehind = true
				break
			}
		}
		if !isBehind {
			return true
		}
		if !hasSaidQueued {
			self.sayNote(models.NoteQueued, "")
			hasSaidQueued = true
		}
		select {
		case <-self.ctx.Done():
			return false
		case <-time.After(turnElsewherePoll):
		}
	}
}

// stopTurnsElsewhere asks the instances running turns of a conversation to
// stop them.
func (self *Agent) stopTurnsElsewhere(conversationId string) {
	for _, turn := range self.turnsElsewhere(self.ctx, conversationId) {
		if err := self.Forward(RunCommand{RunID: turn.RunID, Action: CommandStop}); err != nil {
			log.Warningf("cannot ask another instance to stop the turn %q: %s", turn.RunID, err)
		}
	}
}

// recordedRunElsewhere is the conversation of a run recorded as running on
// another instance, and whether there is one.
func (self *Agent) recordedRunElsewhere(runId string) (string, bool) {
	if self.settings.Database == nil || runId == "" {
		return "", false
	}
	var turn *models.AgentTurn
	if err := self.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) (err error) {
		turn, err = tx.GetAgentTurn(runId)
		return err
	}); err != nil || turn == nil || turn.Instance == self.settings.Instance || turn.CutAt != nil {
		return "", false
	}
	return turn.ConversationID, true
}
