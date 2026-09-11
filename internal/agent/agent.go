// Package agent is a person's agent: the worker that does its processing
// with nobody present, and — in later milestones — the conversation it
// holds with the person and the tools it reaches everything through.
//
// It knows mail and the database; it knows nothing about HTTP. Everything
// it does for a person is measured against the person and recorded with
// the source beside it. Nothing here is constructed while the agent is off:
// the server passes a nil registry and builds no worker.
package agent

import (
	"context"
	"fmt"
	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"sync"
	"sync/atomic"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/periodic"
)

var log = logging.MustGetLogger("agent")

// Settings is what the agent needs.
type Settings struct {
	Database db.Database
	Storage  storage.Storage
	Registry *llm.Registry

	// Exchange runs the rules that wait for an insight, and climbs the
	// out-of-office ladder for the agent. Nil in tests that have no mail
	// path.
	Exchange mx.Exchange

	// Mailer composes and sends what the agent writes. Set after the
	// mailer exists, which is after the worker does; nil until then.
	Mailer mailer.Mailer

	// Configuration is read at the start of every tick, so an operator's
	// change to a limit or a feature applies without a restart.
	Configuration func() *config.Configuration

	// Instance names this server instance on the jobs it claims.
	Instance string

	// Tick is how often the worker looks for work. Zero means five seconds.
	Tick time.Duration
}

// Agent is the worker and, for the rest of the server, the way to hand it
// work.
type Agent struct {
	settings *Settings

	ctx       context.Context
	cancel    context.CancelFunc
	waitGroup sync.WaitGroup
	worker    periodic.Periodic

	handlersMutex sync.RWMutex
	handlers      map[models.AgentJobKind]Handler

	// slots bounds how many runs execute at once on this instance.
	slots chan struct{}

	// catalog is every tool; runs are the Ask turns in flight, and latest
	// the newest turn of each conversation, which the next one waits for.
	catalog   *Catalog
	runsMutex sync.Mutex
	runs      map[string]*AskRun
	latest    map[string]*AskRun

	// operations makes the API as a person, for runs nobody started from
	// a request; lastScavenge is when old rows were last swept.
	operations   OperationsFactory
	lastScavenge time.Time
	lastDescribe time.Time
	describing   atomic.Bool

	// connections are the sessions with connected servers, per server and
	// person.
	connectionsMutex sync.Mutex
	connections      map[string]*connectedServer

	// tabs are the browser tabs people attached; contextsOpen counts the
	// headless browser contexts in use under the operator's cap.
	tabsMutex sync.Mutex
	tabs      map[string]*attachedTab
	// feeds are the subscribers to each conversation's events, by
	// conversation; the relay queue is what this instance's runs emitted
	// and the others have not heard yet.
	feedsMutex sync.Mutex
	feeds      map[string]map[int]chan Event
	nextFeed   int
	relayMutex sync.Mutex
	relaying   bool
	relayQueue []Event
	relayWake  chan struct{}
	// foreignRuns are the runs heard of through the feed that other
	// instances run, by run id.
	foreignMutex sync.Mutex
	foreignRuns  map[string]foreignRun

	// computers are the computers people attached with `teanode computer`.
	computersMutex sync.Mutex
	computers      map[string]map[string]*attachedComputer
	contextsMutex  sync.Mutex
	contextsOpen   int
}

// Catalog is every tool the server knows.
func (self *Agent) Catalog() *Catalog {
	return self.catalog
}

// Handler runs one kind of job. It returns nil when the job is done, a
// Deferral to wait for a budget, or an error to retry.
type Handler func(ctx context.Context, run *Run) error

// Run is one job with what every handler needs resolved: the agent, its
// owner, and the mailbox the job is about.
type Run struct {
	Job     *models.AgentJob
	Agent   *models.Agent
	Owner   *models.User
	Mailbox *models.Mailbox
	Source  *models.AgentMailbox

	// Now is the moment the tick that claimed the job ran as of, so that a
	// hold or a deferral is judged against the same clock that claimed it.
	Now time.Time

	settings *Settings
}

// Database is the run's database.
func (self *Run) Database() db.Database { return self.settings.Database }

// Storage is the run's message store.
func (self *Run) Storage() storage.Storage { return self.settings.Storage }

// Registry is the run's models.
func (self *Run) Registry() *llm.Registry { return self.settings.Registry }

// Configuration is the operator's configuration as of this tick.
func (self *Run) Configuration() *config.Configuration { return self.settings.Configuration() }

// New builds the agent. Register handlers, then Start.
func New(settings *Settings) *Agent {
	if settings.Tick <= 0 {
		settings.Tick = 5 * time.Second
	}
	self := &Agent{settings: settings, handlers: map[models.AgentJobKind]Handler{}}
	self.ctx, self.cancel = context.WithCancel(context.Background())
	concurrency := settings.Configuration().Agent.Limits.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	self.slots = make(chan struct{}, concurrency)
	self.Register(models.AgentJobNoop, func(ctx context.Context, run *Run) error {
		log.Debugf("noop job %s ran for agent %s", run.Job.ID, run.Agent.ID)
		return nil
	})
	self.Register(models.AgentJobTriage, self.runTriage)
	self.Register(models.AgentJobBackfill, self.runBackfill)
	self.Register(models.AgentJobSummarize, self.runSummarize)
	self.Register(models.AgentJobReply, self.runReply)
	self.Register(models.AgentJobSend, self.runSend)
	self.Register(models.AgentJobEmbed, self.runEmbed)
	self.Register(models.AgentJobSchedule, self.runSchedule)
	self.Register(models.AgentJobResearch, self.runResearch)
	self.catalog = FullCatalog()
	return self
}

// FullCatalog is every tool the agent can be given, before permissions and
// policy narrow it: what the worker runs with, and what the operator's
// tool policy lists.
func FullCatalog() *Catalog {
	return tools.Build()
}

// OperationsFor is what a run made for a person outside a request — a
// scheduled run, a turn from a chat app — may do: what the person may.
func (self *Agent) OperationsFor(ctx context.Context, owner *models.User) (Operations, error) {
	return self.operations(ctx, owner)
}

// SetMailer hands the worker the mailer, once there is one.
func (self *Agent) SetMailer(sender mailer.Mailer) {
	self.settings.Mailer = sender
}

// Register sets the handler for a kind of job.
func (self *Agent) Register(kind models.AgentJobKind, handler Handler) {
	self.handlersMutex.Lock()
	defer self.handlersMutex.Unlock()
	self.handlers[kind] = handler
}

// Start begins claiming work.
func (self *Agent) Start() {
	self.worker = periodic.New(self.ctx, &self.waitGroup, self.tick, &periodic.Settings{
		Interval: self.settings.Tick,
		Name:     "agent:worker",
	})
	self.worker.Start()
	self.startFeed()
}

// Stop ends the worker and waits for the runs in flight.
func (self *Agent) Stop() {
	self.cancel()
	if self.worker != nil {
		self.worker.Stop()
	}
	self.waitGroup.Wait()
}

// Enqueue adds a job inside the caller's transaction. A job for the same
// agent, kind and subject that is already queued or running is returned
// instead of a second one.
func (self *Agent) Enqueue(tx db.Transaction, kind models.AgentJobKind, agentId, mailboxId, subjectId string) (*models.AgentJob, error) {
	return tx.EnqueueAgentJob(&models.AgentJob{AgentID: agentId, MailboxID: mailboxId, Kind: kind, SubjectID: subjectId})
}

// OnMailboxDelivery is the delivery hook: a message has just been placed in
// a mailbox, inside the delivery transaction. If the mailbox is a granted
// source of an active agent, the processing jobs are queued in the same
// transaction — and nothing else happens here, because a model call inside
// the SMTP transaction would be a model outage bouncing mail.
func (self *Agent) OnMailboxDelivery(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail) {
	if mailbox == nil || mailbox.Agent == nil || !mailbox.Agent.Granted {
		return
	}
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled {
		return
	}
	agent, err := tx.GetAgentByUser(mailbox.UserID)
	if err != nil {
		log.Warningf("cannot find the agent for mailbox %q: %s", mailbox.ID, err)
		return
	}
	if !agent.Active() {
		return
	}
	source := mailbox.Agent
	if source.Triage != nil && source.Triage.Enabled && configuration.Agent.FeatureOn("triage") {
		if _, err := self.Enqueue(tx, models.AgentJobTriage, agent.ID, mailbox.ID, mail.ID); err != nil {
			log.Warningf("cannot queue triage for message %q: %s", mail.ID, err)
		}
	}
	if source.Summaries != nil && source.Summaries.Enabled && configuration.Agent.FeatureOn("summaries") {
		// A conversation is summarized unasked once it is long enough to
		// need it; a shorter one waits to be opened.
		threadId := mail.ThreadID
		if threadId == "" {
			threadId = mail.ID
		}
		minimum := MinimumMessages(source)
		items, err := tx.ListItems("", &db.ItemOptions{MailboxID: mailbox.ID, ThreadID: threadId, Limit: summaryThreadLimit})
		if err != nil {
			log.Warningf("cannot count the conversation of message %q: %s", mail.ID, err)
		} else if countMails(items) >= minimum {
			if _, err := self.Enqueue(tx, models.AgentJobSummarize, agent.ID, mailbox.ID, threadId); err != nil {
				log.Warningf("cannot queue a summary for conversation %q: %s", threadId, err)
			}
		}
	}
	if source.Search && configuration.Agent.FeatureOn("search") && configuration.Agent.Models.Embedding != "" {
		if _, err := self.Enqueue(tx, models.AgentJobEmbed, agent.ID, mailbox.ID, mail.ID); err != nil {
			log.Warningf("cannot queue embedding for message %q: %s", mail.ID, err)
		}
	}
}

// staleClaim is how long a claimed job may sit without finishing before it
// is assumed to belong to an instance that died and is put back.
const staleClaim = 15 * time.Minute

// retryLadder is how long a failed job waits before each retry; after the
// last rung it is dead.
var retryLadder = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour}

// Tick releases stale claims, claims what is due, and runs it. The worker
// calls it on its interval; a test calls it by hand.
func (self *Agent) Tick(ctx context.Context) error {
	return self.tickAt(ctx, time.Now())
}

// TickAt is Tick as of a moment, for a test that cannot wait for a hold to
// end.
func (self *Agent) TickAt(ctx context.Context, now time.Time) error {
	return self.tickAt(ctx, now)
}

// Wait blocks until every run in flight has finished. For tests and for
// Stop.
func (self *Agent) Wait() {
	self.waitGroup.Wait()
}

// tick releases stale claims, claims what is due, and runs it.
func (self *Agent) tick(ctx context.Context) error {
	return self.tickAt(ctx, time.Now())
}

func (self *Agent) tickAt(ctx context.Context, now time.Time) error {
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled {
		return nil
	}
	free := cap(self.slots) - len(self.slots)
	if free <= 0 {
		return nil
	}
	if err := self.dueSchedules(ctx, now); err != nil {
		log.Warningf("cannot queue the schedules that are due: %s", err)
	}
	self.scavenge(ctx, now)
	self.describeInBackground(ctx, now)
	self.sweepBrowsers()
	var jobs []*models.AgentJob
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if released, err := tx.ReleaseStaleAgentJobs(now.Add(-staleClaim)); err != nil {
			return err
		} else if released > 0 {
			log.Warningf("put back %d job(s) whose instance never finished them", released)
		}
		claimed, err := tx.ClaimAgentJobs(self.settings.Instance, free, now)
		jobs = claimed
		return err
	}); err != nil {
		return fmt.Errorf("claiming agent jobs: %w", err)
	}
	for _, job := range jobs {
		self.slots <- struct{}{}
		self.waitGroup.Add(1)
		go func(job *models.AgentJob) {
			defer self.waitGroup.Done()
			defer func() { <-self.slots }()
			self.execute(job, now)
		}(job)
	}
	return nil
}

// Deferral is what a handler returns when a run must wait — for a budget,
// for a hold window — rather than fail.
type Deferral struct {
	Until  time.Time
	Reason string
}

func (self *Deferral) Error() string {
	return fmt.Sprintf("deferred until %s: %s", self.Until.Format(time.RFC3339), self.Reason)
}

// execute runs one claimed job and records how it ended.
func (self *Agent) execute(job *models.AgentJob, now time.Time) {
	ctx, cancel := context.WithTimeout(self.ctx, 10*time.Minute)
	defer cancel()

	run, err := self.resolve(ctx, job)
	if run != nil {
		run.Now = now
	}
	if err == nil {
		self.handlersMutex.RLock()
		handler := self.handlers[job.Kind]
		self.handlersMutex.RUnlock()
		if handler == nil {
			err = fmt.Errorf("no handler for a %s job", job.Kind)
		} else {
			err = handler(ctx, run)
		}
	}

	status, message, notBefore := models.AgentJobDone, "", (*time.Time)(nil)
	switch typed := err.(type) {
	case nil:
	case *Deferral:
		status, message, notBefore = models.AgentJobQueued, typed.Reason, &typed.Until
		log.Noticef("%s job %s for agent %s waits until %s: %s", job.Kind, job.ID, job.AgentID, typed.Until.Format(time.RFC3339), typed.Reason)
	default:
		message = err.Error()
		if job.Attempts > len(retryLadder) {
			status = models.AgentJobDead
			log.Errorf("%s job %s for agent %s gave up after %d attempts: %s", job.Kind, job.ID, job.AgentID, job.Attempts, err)
		} else {
			delay := retryLadder[job.Attempts-1]
			retryAt := time.Now().Add(delay)
			status, notBefore = models.AgentJobQueued, &retryAt
			log.Warningf("%s job %s for agent %s failed (attempt %d), retrying in %s: %s", job.Kind, job.ID, job.AgentID, job.Attempts, delay, err)
		}
	}
	if err := self.settings.Database.Transaction(func(tx db.Transaction) error {
		return tx.FinishAgentJob(job.ID, job.ClaimedBy, status, message, notBefore)
	}); err != nil {
		log.Errorf("cannot record how job %s ended: %s", job.ID, err)
	}
}

// resolve loads what every handler needs. A job whose agent has been turned
// off or whose source has been revoked since it was queued is done with
// nothing to do, not an error.
func (self *Agent) resolve(ctx context.Context, job *models.AgentJob) (*Run, error) {
	run := &Run{Job: job, settings: self.settings}
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		agent, err := tx.GetAgent(job.AgentID)
		if err != nil {
			return err
		}
		if agent == nil {
			return fmt.Errorf("agent %s no longer exists", job.AgentID)
		}
		run.Agent = agent
		if run.Owner, err = tx.GetUser(agent.UserID); err != nil {
			return err
		}
		if job.MailboxID != "" {
			if run.Mailbox, err = tx.GetMailbox(job.MailboxID); err != nil {
				return err
			}
			if run.Mailbox != nil {
				run.Source = run.Mailbox.Agent
			}
		}
		return nil
	})
	return run, err
}

// countMails is how many distinct messages a list of items holds; a message
// filed in two folders is one message.
func countMails(items []*models.MailboxItem) int {
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.MailID] = true
	}
	return len(seen)
}

// scavenge sweeps what retention says is old, once an hour: run
// transcripts, corrections, finished jobs, and replies that are over.
func (self *Agent) scavenge(ctx context.Context, now time.Time) {
	if now.Sub(self.lastScavenge) < time.Hour {
		return
	}
	self.lastScavenge = now
	retention := self.settings.Configuration().Agent.Retention
	runs := retention.Runs.Duration()
	if runs <= 0 {
		runs = 30 * 24 * time.Hour
	}
	corrections := retention.Corrections.Duration()
	if corrections <= 0 {
		corrections = 90 * 24 * time.Hour
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if _, err := tx.ScavengeAgentConversations(models.AgentConversationRun, now.Add(-runs)); err != nil {
			return err
		}
		if _, err := tx.ScavengeAgentFeedback(now.Add(-corrections)); err != nil {
			return err
		}
		if _, err := tx.ScavengeAgentJobs(now.Add(-runs)); err != nil {
			return err
		}
		if _, err := tx.ScavengeAgentReplies(now.Add(-runs)); err != nil {
			return err
		}
		return self.scavengeAttachments(ctx, tx, now)
	}); err != nil {
		log.Warningf("cannot sweep the agent's old rows: %s", err)
	}
}
