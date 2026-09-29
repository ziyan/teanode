package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Background work: a survey or a subagent the agent started and did not
// wait for.
//
// A background command is held by the program on the person's computer,
// which is why it survives a restart of the server. This work runs on the
// server, and a goroutine dies with a deploy; so it is a row, and a queued
// job whose subject is the row runs it. A job claimed again after a
// restart finds the row running and runs it again, which costs the work
// twice and wakes the conversation at most twice, as the background
// command decision already accepts.
//
// When it finishes, the conversation that started it is woken the way an
// ended command wakes it: the same gathering, the same count of woken
// turns before the person writes again, the same note instead when out of
// turns or budget. See background.go.

const (
	// backgroundWorkLongest is how long one piece of work may run: a
	// survey's quarter of an hour and some room. The job's own bound is a
	// little longer, so that the work reaching this one is recorded as
	// failed rather than put back in the queue to run again.
	backgroundWorkLongest = surveyLongest + 5*time.Minute

	// backgroundWorkStopCheck is how often running work looks at its row
	// for a stop made on another instance, which cannot reach this one's
	// cancel.
	backgroundWorkStopCheck = 5 * time.Second

	// backgroundWorkSweepEvery is how often lost work is looked for:
	// work whose job vanished, and finished work whose wake was lost.
	backgroundWorkSweepEvery = time.Minute

	// backgroundWorkStaleAfter is how old queued or running work is
	// before it is taken to have lost its job and is failed.
	backgroundWorkStaleAfter = 24 * time.Hour

	// Finished work whose conversation was not woken is woken by the
	// sweep once it has been finished backgroundWorkWakeAfter, which
	// leaves the wake the job itself hands over its time, and until it
	// has been finished backgroundWorkWakeWithin, after which it is too
	// late to be news.
	backgroundWorkWakeAfter  = time.Minute
	backgroundWorkWakeWithin = time.Hour

	// backgroundWorkWakeClaimExpiry is how long a claim on waking the
	// conversation for finished work holds. Longer than a woken turn
	// takes, so that the claimer is never raced by another instance while
	// its turn runs; a claim this old is taken to be a server's that went
	// down mid-wake, and the sweep takes it again.
	backgroundWorkWakeClaimExpiry = 30 * time.Minute

	// backgroundWorkResultCharacters is how much of a result the wake
	// carries. A survey's report is usually shorter; the whole of it is
	// read with the background_work tool.
	backgroundWorkResultCharacters = 12000
)

// errBackgroundWorkStopped is the cause a piece of work is cancelled with
// when somebody stopped it.
var errBackgroundWorkStopped = errors.New("stopped")

// QueueBackgroundWork records a piece of work and queues the job that
// runs it, in the caller's transaction.
func (self *Agent) QueueBackgroundWork(tx db.Transaction, work *models.AgentBackgroundWork) (*models.AgentBackgroundWork, error) {
	created, err := tx.CreateAgentBackgroundWork(work)
	if err != nil {
		return nil, err
	}
	if _, err := self.Enqueue(tx, models.AgentJobBackground, created.AgentID, "", created.ID); err != nil {
		return nil, err
	}
	return created, nil
}

// CanSurvey says whether a survey can be run now, for the API to refuse
// one at once rather than queue work that fails.
func (self *Agent) CanSurvey() bool {
	return self != nil && self.canThink(self.settings.Configuration())
}

// startBackgroundWork records work a turn started and queues it, to wake
// the turn's conversation when it finishes. A turn with nobody present
// starts none, as it starts no background command: it has ended by the
// time the work finishes, and nobody is reading it.
func (self *Agent) startBackgroundWork(ctx context.Context, parent *AskRun, work *models.AgentBackgroundWork) (*models.AgentBackgroundWork, error) {
	if parent.settings.Headless || parent.settings.Conversation == nil {
		return nil, fmt.Errorf("nobody is present to be told when it finishes; set background to false and wait for it")
	}
	work.ConversationID, work.IsPersonPresent = parent.settings.Conversation.ID, true
	var started *models.AgentBackgroundWork
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		started, err = self.QueueBackgroundWork(tx, work)
		return err
	}); err != nil {
		return nil, err
	}
	return started, nil
}

// StopBackgroundWork stops one of the agent's, in the caller's
// transaction: the row says stopped at once, and the work, when it runs
// here, is cancelled once that is committed. Work running on another
// instance sees the row within backgroundWorkStopCheck. Stopped work wakes
// nothing. Work that had already finished is returned as it is.
func (self *Agent) StopBackgroundWork(tx db.Transaction, agentId, workId string) (*models.AgentBackgroundWork, error) {
	work, err := tx.StopAgentBackgroundWork(agentId, workId, time.Now())
	if errors.Is(err, db.ErrNotFound) {
		return nil, fmt.Errorf("there is no background work %q", workId)
	}
	if err != nil {
		return nil, err
	}
	if work.WorkStatus == models.BackgroundWorkStopped {
		tx.AfterCommit(func() { self.cancelRunningWork(workId) })
	}
	return work, nil
}

// holdRunningWork keeps how to cancel work running here, until release is
// called.
func (self *Agent) holdRunningWork(workId string, cancel context.CancelCauseFunc) (release func()) {
	self.backgroundMutex.Lock()
	if self.runningWork == nil {
		self.runningWork = map[string]context.CancelCauseFunc{}
	}
	self.runningWork[workId] = cancel
	self.backgroundMutex.Unlock()
	return func() {
		self.backgroundMutex.Lock()
		delete(self.runningWork, workId)
		self.backgroundMutex.Unlock()
	}
}

func (self *Agent) cancelRunningWork(workId string) {
	self.backgroundMutex.Lock()
	cancel := self.runningWork[workId]
	self.backgroundMutex.Unlock()
	if cancel != nil {
		cancel(errBackgroundWorkStopped)
	}
}

// runBackgroundWork is the job: it runs the work its subject names and
// keeps what came of it, then wakes the conversation that started it.
func (self *Agent) runBackgroundWork(ctx context.Context, run *Run) error {
	var work *models.AgentBackgroundWork
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		work, err = tx.GetAgentBackgroundWork(run.Agent.ID, run.Job.SubjectID)
		return err
	}); err != nil {
		return err
	}
	// Gone with its agent, stopped before it started, or finished by a
	// run of this job that was claimed twice: nothing to do.
	if work == nil || work.WorkStatus.IsFinished() {
		return nil
	}
	// Out of budget: the job waits for it, and the row stays queued.
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return RequireBudget(tx, run.Configuration(), run.Agent, run.Owner, time.Now())
	}); err != nil {
		return err
	}
	isStarted := false
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		isStarted, err = tx.StartAgentBackgroundWork(work.ID, time.Now())
		return err
	}); err != nil || !isStarted {
		return err
	}
	startedAt := time.Now()
	work.StartedAt = &startedAt

	stopContext, cancelWork := context.WithCancelCause(ctx)
	defer cancelWork(nil)
	workContext, cancelDeadline := context.WithTimeout(stopContext, backgroundWorkLongest)
	defer cancelDeadline()
	release := self.holdRunningWork(work.ID, cancelWork)
	defer release()
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		self.watchForStop(workContext, run.Database(), work, cancelWork)
	}()

	resultText, runIds, err := self.carryOutBackgroundWork(workContext, run, work)
	cancelDeadline()
	<-watched

	isStopped := errors.Is(context.Cause(stopContext), errBackgroundWorkStopped)
	// The server is going down: the job goes back in the queue and runs
	// again after the restart, and the row stays running for it.
	if ctx.Err() != nil && !isStopped {
		return ctx.Err()
	}
	finishedAt := time.Now()
	work.FinishedAt, work.ResultText, work.RunIDs = &finishedAt, resultText, runIds
	switch {
	case isStopped:
		work.WorkStatus = models.BackgroundWorkStopped
	case err != nil && errors.Is(err, context.DeadlineExceeded):
		work.WorkStatus = models.BackgroundWorkFailed
		work.ErrorMessage = "it ran out of time and was stopped"
	case err != nil:
		work.WorkStatus, work.ErrorMessage = models.BackgroundWorkFailed, err.Error()
	default:
		work.WorkStatus = models.BackgroundWorkDone
	}
	// Written on a context of its own, since the work's may be done.
	finishContext, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), jobCompletionTimeout)
	defer cancelFinish()
	isFinished := false
	if err := run.Database().TransactionContext(finishContext, func(tx db.Transaction) (err error) {
		isFinished, err = tx.FinishAgentBackgroundWork(work)
		return err
	}); err != nil {
		return err
	}
	// Stopped while it ran: the row already says so, and stopped work
	// wakes nothing.
	if !isFinished || work.WorkStatus == models.BackgroundWorkStopped {
		return nil
	}
	self.wakeForBackgroundWork(work)
	return nil
}

// watchForStop cancels work whose row says it was stopped, for a stop
// made on another instance.
func (self *Agent) watchForStop(ctx context.Context, database db.Database, work *models.AgentBackgroundWork, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(backgroundWorkStopCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var current *models.AgentBackgroundWork
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			current, err = tx.GetAgentBackgroundWork(work.AgentID, work.ID)
			return err
		}); err != nil {
			continue
		}
		if current == nil || current.WorkStatus == models.BackgroundWorkStopped {
			cancel(errBackgroundWorkStopped)
			return
		}
	}
}

// carryOutBackgroundWork runs the survey or the subagent, with the same
// code the tools run when they wait.
func (self *Agent) carryOutBackgroundWork(ctx context.Context, run *Run, work *models.AgentBackgroundWork) (string, []string, error) {
	request := work.WorkRequest
	switch work.WorkKind {
	case models.BackgroundWorkSurvey:
		surveyed, err := self.Survey(ctx, run.Agent, run.Owner, request.Question, request.ScopePath)
		var runIds []string
		if surveyed != nil {
			runIds = surveyed.RunIDs
		}
		if err != nil {
			return "", runIds, err
		}
		return surveyed.Report, runIds, nil
	case models.BackgroundWorkSubagent:
		return self.backgroundSubagent(ctx, run, work)
	}
	return "", nil, fmt.Errorf("%q is not a kind of background work", work.WorkKind)
}

// backgroundSubagent runs a subagent with the tools fixed when it was
// started. Nobody is at the other end of it -- the turn that started it
// has ended -- so it can put no card to the person: a call that needs
// their word is refused, and it says what it would have done, for the turn
// it wakes to take to them.
func (self *Agent) backgroundSubagent(ctx context.Context, run *Run, work *models.AgentBackgroundWork) (string, []string, error) {
	operations, err := self.backgroundSubagentOperations(ctx, run.Owner, &work.WorkRequest)
	if err != nil {
		return "", nil, err
	}
	request := work.WorkRequest
	conversation, err := self.createSubagentRun(ctx, run.Agent.ID, work.Title)
	if err != nil {
		return "", nil, err
	}
	runIds := []string{conversation.ID}
	allowed := map[string]bool{}
	for _, name := range request.AllowedToolNames {
		allowed[name] = true
	}
	var readOnlyTools map[string]bool
	if len(request.ReadOnlyToolNames) > 0 {
		readOnlyTools = map[string]bool{}
		for _, name := range request.ReadOnlyToolNames {
			readOnlyTools[name] = true
		}
	}
	turn, err := self.Ask(&AskSettings{
		Agent:         run.Agent,
		Owner:         run.Owner,
		Operations:    operations,
		Conversation:  conversation,
		Message:       request.Prompt,
		Surface:       "subagent",
		ReadOnly:      request.IsReadOnly,
		ReadOnlyTools: readOnlyTools,
		Allow:         allowed,
		MaxRounds:     subagentRounds,
		UsageKind:     "subagent",
		isUnattended:  true,
		subagentDepth: 1,
	})
	if err != nil {
		return "", runIds, err
	}
	answer, _, err := followSubagent(ctx, turn, conversation.ID, backgroundWorkLongest)
	return answer, runIds, err
}

// backgroundSubagentOperations is the API as the person for a subagent in
// the background, held to what the turn that started it could do. Made
// afresh for the person, since a run after a restart has no turn to take
// them from, and narrowed, so that neither a permission granted since nor
// a turn of wider reach than the one that started it widens what it may
// do. A permission taken away since is gone from both.
func (self *Agent) backgroundSubagentOperations(ctx context.Context, owner *models.User, request *models.AgentBackgroundWorkRequest) (Operations, error) {
	if self.operations == nil {
		return nil, ErrUnavailable
	}
	operations, err := self.operations(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("cannot act as %q: %w", owner.Username, err)
	}
	return narrowOperations(operations, request.StartingTurnPermissions), nil
}

// sweepBackgroundWork fails work whose job was lost, and wakes the
// conversations of finished work whose wake was lost to a restart. Once a
// minute.
func (self *Agent) sweepBackgroundWork(ctx context.Context, now time.Time) {
	if now.Sub(self.lastBackgroundSweep) < backgroundWorkSweepEvery {
		return
	}
	self.lastBackgroundSweep = now
	var lost []*models.AgentBackgroundWork
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if failed, err := tx.FailStaleAgentBackgroundWork(now.Add(-backgroundWorkStaleAfter), "its job was lost before it finished"); err != nil {
			return err
		} else if failed > 0 {
			log.Noticef("failed %d piece(s) of background work whose job was lost", failed)
		}
		lost, err = tx.ListAgentBackgroundWorkToWake(now.Add(-backgroundWorkWakeWithin), now.Add(-backgroundWorkWakeAfter), now.Add(-backgroundWorkWakeClaimExpiry), 0)
		return err
	}); err != nil {
		log.Warningf("cannot sweep the background work: %s", err)
		return
	}
	for _, work := range lost {
		self.wakeForBackgroundWork(work)
	}
}

// backgroundWorkWhat says what a piece of work is, in a line.
func backgroundWorkWhat(work *models.AgentBackgroundWork) string {
	switch work.WorkKind {
	case models.BackgroundWorkSurvey:
		scope := "everything"
		if strings.TrimSpace(work.WorkRequest.ScopePath) != "" {
			scope = work.WorkRequest.ScopePath
		}
		return fmt.Sprintf("a survey of %s: %q", scope, work.WorkRequest.Question)
	case models.BackgroundWorkSubagent:
		return fmt.Sprintf("a subagent: %q", work.Title)
	}
	return string(work.WorkKind)
}

// backgroundWorkOutcome is how a piece of work ended, in a word or two.
func backgroundWorkOutcome(work *models.AgentBackgroundWork) string {
	switch work.WorkStatus {
	case models.BackgroundWorkDone:
		return "finished"
	case models.BackgroundWorkFailed:
		return "failed"
	case models.BackgroundWorkStopped:
		return "stopped before it finished"
	}
	return string(work.WorkStatus)
}
