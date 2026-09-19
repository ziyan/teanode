package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The nightly run: working through what arrived, writing up the month,
// rewriting the pages it touched, and tidying.
//
// Every bound here is pacing, never truncation. A phase that cannot get
// through its work in one night does not drop the rest: it leaves the
// cursor where it stopped, reports how far behind it is, and comes back
// tomorrow. Where the backlog is larger than anybody would wait for it
// coarsens -- a channel-day as one unit instead of its threads one by one
// -- and says so, so a later night or the person can redo that stretch
// finely. Coverage is always total; only detail degrades.
//
// Nothing here ever deletes. It merges, rewrites, marks dormant and
// proposes. A run that could lose something is one nobody would let
// happen while they slept.

// The bounds.
const (
	// dreamQuiet is how long the person must have been away, so that a
	// nightly run never happens mid-conversation.
	dreamQuiet = 30 * time.Minute

	// dreamApart is the least time between two runs.
	dreamApart = 6 * time.Hour

	// dreamEvery is how often the sweep looks: often enough that a night
	// bootstrapping starts again within the minute of the last one.
	dreamEvery = time.Minute

	// Bootstrapping -- a first ingest read as fast as the machine allows
	// -- widens the night: the reading may take most of the night rather
	// than half, more of it, more months written up, more pages divided,
	// and a shorter wait after the person's last word.
	dreamDigestBootstrap = 5000
	// dreamSilences is how many batches in a row the model may leave
	// unanswered before the night's reading stops.
	dreamSilences       = 3
	dreamQuietBootstrap = 5 * time.Minute

	// dreamLongest is how long one night may run. The reading takes
	// half of it at most (see halfway), so the rest is never starved.
	dreamLongest = 45 * time.Minute

	// dreamDigest is how many things one night reads at full resolution,
	// and dreamConsolidate how many pages it rewrites.
	dreamDigest      = 2000
	dreamConsolidate = 100

	// dreamBatch is how many items go into one call of the digest phase.
	dreamBatch = 20

	// digestFacts is how many facts one batch may file.
	//
	// A quarter of the batch, while a fact was only ever about a subject.
	// The reading is now told that the author of every item earns a page
	// and the one line the item shows about them, so a batch of twenty
	// commits brings up to twenty people along with whatever they were
	// working on -- and five between them left the people and the
	// projects competing for the same five, which is a bar on the pages
	// by another name. Half the batch, still under rememberFacts, the
	// hard bound the filing itself enforces.
	digestFacts = dreamBatch / 2

	// digestSmallest is how much a document must hold to be worth a
	// share of a call: about two short lines. See markTinyRead for what
	// is measured against it.
	digestSmallest = 160

	// dreamShareDefault is how much of the day's budget a night may
	// spend when the operator has not said.
	dreamShareDefault = 0.3

	// dreamEmbed is how many vectors one night writes.
	dreamEmbed = 2000

	// dormantAfter is how long a fact goes unused before it leaves the
	// index. It stays searchable; a preference or a decision never goes
	// dormant at all, because those are asked for by name.
	dormantAfter = 180 * 24 * time.Hour
)

// Proposal is something the night wants the person to decide.
type Proposal struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// queueDreaming queues a nightly run for whoever is due one.
func (self *Agent) queueDreaming(ctx context.Context, now time.Time) {
	if now.Sub(self.lastDream) < dreamEvery || self.settings.Registry == nil {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "dreaming") {
		return
	}
	self.lastDream = now

	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to dream for: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Enabled || agent.OperatorDisabledAt != nil {
			continue
		}
		owner := self.ownerOf(ctx, agent)
		if owner == nil {
			continue
		}
		if !self.dreamDue(ctx, agent, owner, now) {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := self.Enqueue(tx, models.AgentJobDream, agent.ID, "", now.Format("2006-01-02"))
			return err
		}); err != nil {
			log.Warningf("cannot queue a dream for agent %q: %s", agent.ID, err)
		}
	}
}

// ownerOf is whose agent this is.
func (self *Agent) ownerOf(ctx context.Context, agent *models.Agent) *models.User {
	var owner *models.User
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		owner, err = tx.GetUser(agent.UserID)
		return err
	}); err != nil {
		log.Debugf("cannot read the owner of agent %q: %s", agent.ID, err)
		return nil
	}
	return owner
}

// dreamDue says whether this agent should work tonight.
func (self *Agent) dreamDue(ctx context.Context, agent *models.Agent, owner *models.User, now time.Time) bool {
	// Catching up, the six hours between nights do not apply: the night
	// runs again at the next tick until nothing waits. The hours and the
	// quiet rule still do.
	if !agent.DreamBootstrap && agent.DreamedAt != nil && now.Sub(*agent.DreamedAt) < dreamApart {
		return false
	}
	if !insideWindow(agent, owner, now) {
		return false
	}
	quiet := dreamQuiet
	if agent.DreamBootstrap {
		quiet = dreamQuietBootstrap
	}
	// Not while a dream is already queued or running. The job is queued
	// under the day's date, which kept one night from being queued twice
	// until a night crossed midnight: the next tick queued the new day's
	// dream beside the old day's, the two took every worker slot between
	// them and starved the ingest, and the second marked the first cut
	// short while it went on reading.
	var busy bool
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		open, err := tx.CountAgentJobs(&db.AgentJobFilter{
			AgentID:  agent.ID,
			Kinds:    []models.AgentJobKind{models.AgentJobDream},
			Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
		})
		if err != nil {
			return err
		}
		if open > 0 {
			busy = true
			return nil
		}
		// Not while they are talking. A run that rewrites a page the
		// person is reading is a run that looks broken. Their own last
		// word, not the conversation's last message: a goal takes turns
		// of the agent's own every few minutes, and counted as talk those
		// kept a night from ever starting while one ran.
		spoke, err := tx.LastAgentPersonWordAt(agent.ID)
		if err != nil {
			return err
		}
		if spoke != nil && now.Sub(*spoke) < quiet {
			busy = true
		}
		return nil
	}); err != nil {
		log.Debugf("cannot say whether %q is busy: %s", agent.ID, err)
		return false
	}
	return !busy
}

// insideWindow says whether it is the person's night.
func insideWindow(agent *models.Agent, owner *models.User, now time.Time) bool {
	from, until := agent.DreamWindow()
	if from == until {
		return true // any time
	}
	local := now.In(Location(owner))
	minutes := local.Hour()*60 + local.Minute()
	start, err := minutesOf(from)
	if err != nil {
		return true
	}
	end, err := minutesOf(until)
	if err != nil {
		return true
	}
	if start <= end {
		return minutes >= start && minutes < end
	}
	// A window that crosses midnight.
	return minutes >= start || minutes < end
}

func minutesOf(value string) (int, error) {
	when, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return when.Hour()*60 + when.Minute(), nil
}

// runDream is the handler for a dream job.
func (self *Agent) runDream(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "dreaming") {
		return nil
	}
	registry := run.Registry()
	if registry == nil {
		return fmt.Errorf("no model registry")
	}

	// One dream at a time, at the moment of running as well as of
	// queueing: a restart hands every job back to the queue, and two
	// dream jobs queued before it -- one either side of midnight -- were
	// both claimed again and both ran, taking every worker slot between
	// them. The one that finds another running ends here; the queue
	// brings the next night when the running one is over.
	var another bool
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		open, err := tx.ListAgentJobs(&db.AgentJobFilter{
			AgentID:  run.Agent.ID,
			Kinds:    []models.AgentJobKind{models.AgentJobDream},
			Statuses: []models.AgentJobStatus{models.AgentJobRunning},
		}, &db.Options{Limit: 5})
		if err != nil {
			return err
		}
		for _, job := range open {
			if job.ID != run.Job.ID {
				another = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if another {
		log.Infof("agent %q is already dreaming under another job; this one ends", run.Agent.ID)
		return nil
	}

	record := &models.AgentDream{AgentID: run.Agent.ID, StartedAt: time.Now(), JobID: run.Job.ID}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		record, err = tx.StartAgentDream(record)
		return err
	}); err != nil {
		return err
	}

	// A night spends the day's share of the day, so it never eats the
	// day. What today's earlier nights spent comes off it: the share is
	// what is kept from the person's conversation, and a night that
	// starts from the whole share again keeps nothing when nights run
	// every six hours. Read from the usage rows, which the loop writes as
	// it goes, so a night that was killed still counts.
	share := configuration.Agent.Limits.DreamShare
	if share <= 0 {
		share = dreamShareDefault
	}
	var spentDreaming int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		spentDreaming, err = SumSpendOfKind(tx, run.Agent.ID, string(models.AgentJobDream), DayStart(run.Owner, time.Now()))
		return err
	}); err != nil {
		// The night goes ahead on its own share alone, which is what it
		// had before this was read at all.
		log.Warningf("cannot read what today's nights have spent for agent %q: %s", run.Agent.ID, err)
	}
	budget := newDreamBudget(configuration, run.Agent, share, spentDreaming)

	// A night in order. Read what arrived, write it up, tidy the pages it
	// touched, then the two halves proper: the arithmetic one that
	// reweights what was used together and lets the weak fall out of
	// reach, and the generative one that walks the graph looking for
	// relations nobody wrote down. Rehearsal last, because it is the only
	// phase that asks what the rest of the night left missing.
	//
	// Order matters three times. Reading comes before writing up because a
	// month's page should include tonight's facts. The quiet half comes
	// before the generative one because walking a graph whose weights are
	// a day stale wanders somewhere nothing has been wanted in months.
	// And the vectors come before rehearsal, which asks by meaning.
	// First, and before anything that asks a model: what an older build
	// of this program wrote, under the rules this one has. It costs no
	// calls and finishes in seconds, and put after the reading it never
	// ran -- a night has a deadline, four hundred chat days took all of
	// it, and the twenty-eight empty pages stood for another day.
	self.dreamRevise(ctx, run, record)
	// The reading gets half of what is left of the night, never all of
	// it: what comes after it is what makes the reading worth doing.
	reading := 0.5
	if run.Agent.DreamBootstrap {
		reading = 0.7
	}
	budget.digestUntil = partway(ctx, time.Now(), reading)
	self.dreamDigest(ctx, run, record, budget)
	// The pictures after the words, and outside the reading's share of
	// the night: what is decided here is not what to read but what is
	// worth opening at all, and the reading would otherwise take the
	// whole share every night on an archive with fifty thousand files in
	// it and never leave a minute for them. What it opens gets its
	// passages tonight and is read like anything else tomorrow.
	self.dreamAttachments(ctx, run, budget)
	self.dreamTimeline(ctx, run, record, budget)
	self.dreamConsolidate(ctx, run, record, budget)
	self.dreamOrganize(ctx, run, record, budget)
	self.dreamSplit(ctx, run, record, budget)
	self.dreamQuietHalf(ctx, run, record, time.Now())
	self.dreamAssociate(ctx, run, record, budget)
	// Vectors before rehearsal, because rehearsal asks the graph by
	// meaning and everything filed tonight has no vector until this runs.
	// The other way round, a night's own work was invisible to the phase
	// whose whole job is to notice what the graph cannot answer, and
	// every question about it came back as a gap.
	self.dreamEmbed(ctx, run, record)
	self.dreamRehearse(ctx, run, record, budget)

	finished := time.Now()
	record.FinishedAt = &finished
	record.Tokens = budget.spent
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.FinishAgentDream(record); err != nil {
			return err
		}
		_, err := tx.UpdateAgent(run.Agent.ID, func(agent *models.Agent) error {
			agent.DreamedAt = &finished
			// Catching up ends by itself when the reading has caught up:
			// nothing left waiting, or nothing read tonight, which is the
			// same thing said by a night that could not.
			// A dream whose reading stopped on an error has not caught
			// up; it says so, and bootstrapping stays on for the next.
			if agent.DreamBootstrap && record.LastError == "" && (record.Backlog-record.Digested <= 0 || record.Digested == 0) {
				agent.DreamBootstrap = false
				log.Noticef("agent %s has finished bootstrapping; dreaming is back to its hours", run.Agent.ID)
			}
			return nil
		})
		return err
	})
}

// lookupTools is the pair a call reaches when all it needs is to look
// something up: the graph, to find a page before filing to it, and the
// sources, to read a document whole when its first passage is not enough.
// Nothing here changes anything, which is why a call given this set can
// be read-only as well.
//
// This is what describing a checkout gets. The night itself gets
// everyTool; the two are named apart so that a call site says which it
// is asking for.
//
// It is the same pair twice over: what a lookup is given, and what the
// night -- which is given everything -- may only look with, as
// AskSettings.ReadOnlyTools. Naming it once is what keeps a tool added to
// the graph from arriving in one place and not the other.
var lookupTools = map[string]bool{"memory": true, "knowledge": true}

// everyTool is what a call of the night reaches: all of them, including
// the person's own computer where they have attached one.
//
// Nil is how the loop is told not to filter by name (see AskSettings.Allow),
// so this is a nil map rather than a list that would have to be kept in
// step with the catalog.
//
// The night was refused `shell` and `filesystem` when the devices were
// first wired up, on the grounds that an unattended run which can execute
// programs on somebody's machine is a different risk from a conversation
// they are watching. That reasoning is in
// docs/planning/active/20260918-what-was-attached.md and it still holds;
// the owner read it and accepted the risk, so preparation no longer has to
// happen in a records script before the night can use what it prepared.
// What still stands between the night and the grave shapes is the
// confirmation card: a call that needs the person's word is refused
// outright when nobody is there to give it.
var everyTool map[string]bool

// dreamFrame is what every call of a dream is told first: it has the
// person's tools and may use them, and the one thing it does not do by
// hand is change the graph -- not because it lacks the permission, but
// because a change made through the object the call ends with is filed by
// code together with the evidence it came from, which is what keeps a
// fact attached to its source and a move a proposal the person can refuse.
//
// Said as a fact about the run rather than as a request, because it is
// one: the two tools are held to reading by AskSettings.ReadOnlyTools, so
// a call that would change something comes back refused whatever the
// frame says. Telling the model what will happen saves it the round it
// would spend finding out, and the memory tool's own description, which
// invites it to note what it learns, is right there arguing the other way.
const dreamFrame = "You are working through your own memory with nobody present. You have the person's own tools here, their computer among them where they have attached one: read a file, run something over it, look at what came back, the same as you would in a conversation with them. Nobody is there to be asked, so a call that would need their word comes back refused; say in your answer what you would have done rather than looking for another way to do it. The memory and knowledge tools are for looking in this run: `index`, `get`, `search`, `history`, `read`, `sources` and `shape` go through, and anything that would change something -- `note`, `page`, `link`, `unlink`, `move`, `merge`, `forget`, or adding or syncing a source -- comes back refused, inside a `batch` call as well as on its own. That is not a permission you are missing. Every change you want is said in the object you end with, and code files it with the evidence it came from, so that a fact keeps its source and a move stays a proposal the person can refuse; said any other way it is refused and nothing is filed. Use `get` and `search` freely to look a page up first, and when several need looking up, look them all up in one `batch` call of up to eight items rather than one call at a time. Then answer with the object."

// dreamThink is one call of a dream as a run of the loop, titled by what
// it is doing, on the scan model, with what it cost taken off the
// dream's budget. What comes back is the last thing the run said; the
// caller reads the object out of it as it read the response before.
func (self *Agent) dreamThink(ctx context.Context, run *Run, budget *dreamBudget, title, prompt string, lookups bool) (string, error) {
	thinking, err := self.dreamThought(ctx, run, budget, title, prompt, lookups)
	if err != nil {
		return "", err
	}
	return thinking.Text, nil
}

// dreamThought is dreamThink with the run itself handed back, for a
// caller that wants to say afterwards what the run turned out to be.
//
// lookups says whether the call may look pages up: the reading and the
// filing of orphans, which decide where things go, may; a month written
// from its record, an opening rewritten from its facts, a page divided,
// a walk judged and a question rehearsed have all they need in the prompt,
// and a model given tools for those spent ten rounds looking instead of
// answering.
func (self *Agent) dreamThought(ctx context.Context, run *Run, budget *dreamBudget, title, prompt string, lookups bool) (*thought, error) {
	return self.dreamThoughtAbout(ctx, run, budget, title, prompt, nil, lookups)
}

// dreamThoughtAbout is dreamThought with pictures in the turn, for the
// one phase that has something to look at. The allowance is claimed and
// settled here exactly as it is for a call in words, which is what makes
// a night that has spent its share stop looking at pictures too.
func (self *Agent) dreamThoughtAbout(ctx context.Context, run *Run, budget *dreamBudget, title, prompt string, pictures []llm.ContentPart, lookups bool) (*thought, error) {
	// A call with nothing to look up answers from its prompt, so it keeps
	// the read-only turn it always had; there is nothing for a tool to do
	// in it and no reason to offer one.
	tools, rounds, think := noTools, 1, self.thinkAbout
	if lookups {
		// Said before the prompt, because the memory tool's own
		// description invites the model to note what it learns, and a
		// dream that tried to note ran out of rounds refused and never
		// answered.
		tools, rounds, think = everyTool, roundsFor(run.Configuration(), models.AgentJobDream), self.thinkAboutFreely
		prompt = dreamFrame + "\n\n" + prompt
	}
	// Claimed before the call and settled after. A check that stands on
	// its own is a promise made to every batch in flight at once: three
	// of them asked whether there was room before any had spent
	// anything, and all three were told yes.
	if !budget.reserve() {
		return nil, errNothingLeftToSpend
	}
	thinking, err := think(ctx, run, title, prompt, pictures, tools, rounds, models.AgentJobDream, config.AgentWorkScan)
	usage := llm.Usage{}
	if thinking != nil {
		usage = thinking.Usage
	}
	budget.settle(usage)
	return thinking, err
}

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
func (self *dreamBudget) left() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.room()
}

// room is left without the lock, for a caller that already holds it.
func (self *dreamBudget) room() bool {
	if self.exhausted {
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

// --- digest -----------------------------------------------------------

// dreamDigest works through what arrived and files what it taught.
//
// By priority rather than by order, because six hundred thousand threads
// will never all be read: what the person pinned, then what they started,
// then what they answered, then the rest newest first. And when the
// backlog is larger than anybody would wait for, a stretch at a time
// instead of an item at a time -- coarser, complete, and marked so it can
// be done again finely later.
func (self *Agent) dreamDigest(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	var waiting []*models.AgentDocument
	var backlog int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		limit := dreamDigest
		if run.Agent.DreamBootstrap {
			limit = dreamDigestBootstrap
		}
		waiting, backlog, err = tx.ListAgentDocumentsToDigest(run.Agent.ID, chatNamesOf(run.Owner), limit)
		return err
	}); err != nil {
		log.Warningf("cannot list what is waiting to be read: %s", err)
		return
	}
	record.Backlog = int(backlog)
	if len(waiting) == 0 {
		return
	}
	// Never coarsely. Reading titles instead of contents made pages and
	// no facts -- twenty-eight of them with nothing on them -- and a
	// backlog is pacing: what is not read tonight is read on a later
	// night, in full, in the order that matters.
	record.Coarse = false

	waiting = self.markTinyRead(ctx, run, waiting)

	// Batches go to the model a few at a time where it has the slots: a
	// service metered by the call gets one, a model of the person's own
	// as many as it can take. Each batch marks its own documents read as
	// it finishes, so a night that ends between batches loses nothing.
	concurrency := run.Configuration().Agent.Limits.ScanConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	// A batch is one source's documents. The night's order puts chat
	// before notes before code, and where that order crossed from one
	// source into another inside a batch the model carried the page it
	// had just filed to across the line: eleven facts about a checkout,
	// then a chat's news filed to the checkout's page. Sorted by source
	// behind the night's order, and cut where the source changes, a
	// batch holds things that belong together.
	sort.SliceStable(waiting, func(left, right int) bool {
		return waiting[left].SourceID < waiting[right].SourceID
	})
	var mutex sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, concurrency)
	stopped := false
	// Batches the model has not answered, in a row. One is a slow answer
	// on a long batch -- a stream that ran past the request timeout
	// happened about once an hour on the local model -- and a night that
	// stopped reading at the first of those read sixty documents of the
	// two hundred and forty it had time for. Three in a row is a model
	// that is not answering tonight.
	silent := 0
	for start := 0; start < len(waiting); {
		mutex.Lock()
		halt := stopped
		mutex.Unlock()
		if halt || ctx.Err() != nil || !budget.left() || !budget.readingTimeLeft() {
			break
		}
		end := start + 1
		for end < len(waiting) && end < start+dreamBatch && waiting[end].SourceID == waiting[start].SourceID {
			end++
		}
		batch := waiting[start:end]
		start = end
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			filed, answered := self.digestBatch(ctx, run, batch, budget, record.Coarse)
			mutex.Lock()
			defer mutex.Unlock()
			if !answered {
				// What it did not read waits for a night when it
				// answers: the batch is not marked read. The reading
				// goes on past one silence and stops at the third.
				if !budget.left() {
					// Except that a batch the allowance could not pay
					// for is not a silence, and blaming the model for
					// the night running out of tokens reads as a fault
					// on the dream's row.
					return
				}
				silent++
				if silent >= dreamSilences {
					record.LastError = "the model did not answer; the reading stops here"
					stopped = true
				}
				return
			}
			silent = 0
			record.Digested += len(batch)
			record.Filed += filed
			markRead(ctx, run, batch)
		}()
	}
	group.Wait()
}

// markTinyRead marks read, without a call, the documents that say too
// little to be worth a share of one, and answers with the rest.
//
// A channel-day that is one person joining, a file of twenty bytes: four
// hundred of them a night, forty to a call, filed three facts, and the
// calls are better spent on the ones with words in them.
//
// How much a document says is the larger of the size its source recorded
// and the text its passages hold, because neither measure on its own is
// one every document has. A commit is not a file and nothing ever stated
// a size for one, so a rule that read the size alone took every commit
// in an archive for empty -- one thousand eight hundred and seventy-four
// of them stamped read in a single minute, the only documents that carry
// an author and so the only ones an authorship map can be built from --
// and read is the one state a document must not reach without having
// been read, because nothing goes back for it. It happens the other way
// round as well: a file whose text nothing could extract has a size and
// no passages. Only a document that both measures call empty is set
// aside.
func (self *Agent) markTinyRead(ctx context.Context, run *Run, waiting []*models.AgentDocument) []*models.AgentDocument {
	documentIds := make([]string, 0, len(waiting))
	for _, document := range waiting {
		documentIds = append(documentIds, document.ID)
	}
	var characters map[string]int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		characters, err = tx.MeasureAgentDocuments(run.Agent.ID, documentIds)
		return err
	}); err != nil {
		// A measurement that did not happen is not evidence that
		// anything is empty. Reading them all costs calls; marking them
		// read on a query that failed costs the documents themselves.
		log.Warningf("cannot measure what is waiting to be read: %s", err)
		return waiting
	}

	var tiny []string
	kept := waiting[:0]
	for _, document := range waiting {
		if max(document.Bytes, characters[document.ID]) < digestSmallest {
			tiny = append(tiny, document.ID)
			continue
		}
		kept = append(kept, document)
	}
	if len(tiny) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.MarkAgentDocumentsDigested(tiny, time.Now())
		}); err != nil {
			log.Warningf("cannot mark the small ones as read: %s", err)
		}
	}
	return kept
}

// digestBatch reads a handful of documents and files what they taught.
func (self *Agent) digestBatch(ctx context.Context, run *Run, documents []*models.AgentDocument, budget *dreamBudget, coarse bool) (int, bool) {
	var index []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		lines, err := memoryLines(tx, run.Agent.ID, models.AudienceAsk, 10, false)
		index = lines
		return err
	}); err != nil {
		log.Debugf("cannot read the index for a dream: %s", err)
	}

	var builder strings.Builder
	// What each document was shown as, kept by id so that a fact filed
	// from this batch can be held against the words the model actually
	// had. A coarse night shows a title and no body, and a quote from one
	// of those came from nowhere.
	shown := make(map[string]string, len(documents))
	for _, document := range documents {
		heading := document.Cite()
		// "by" rather than another dash. A heading is a title, a name and
		// a date joined by the same mark, and the reading is now told
		// that the author of an item earns a page under people/ -- a rule
		// it cannot apply if it has to guess which of the three parts is
		// the person.
		if author := document.Author(); author != "" {
			heading += " — by " + author
		}
		if document.HappenedAt != nil {
			heading += " — " + document.HappenedAt.Format("2 Jan 2006")
		}
		opening := self.openingOf(ctx, run, document, coarse)
		builder.WriteString("[" + document.ID + "] " + heading + "\n")
		if opening != "" {
			builder.WriteString(unclosable(opening) + "\n")
		}
		builder.WriteString("\n")
		shown[document.ID] = heading + "\n" + opening
	}

	// Where this source's pages live. A batch is one source's documents,
	// and a source has a folder of its own -- the work checkouts and the
	// work chat under work/, a personal checkout under projects/ -- but
	// the model, shown only projects/portal as an example, filed a
	// hundred and seventy of an employer's customer projects under
	// projects/ beside the person's own.
	sourceName, sourceRoot := "", ""
	if len(documents) > 0 {
		_ = run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			source, err := tx.GetAgentSource(run.Agent.ID, documents[0].SourceID)
			if err == nil && source != nil {
				sourceName, sourceRoot = source.Name, strings.Trim(source.RootPath, "/")
			}
			return nil
		})
	}
	prompt, err := render("digest.txt", map[string]any{
		"SourceName": sourceName,
		"SourceRoot": sourceRoot,
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Items":      builder.String(),
		"Most":       digestFacts,
		"Coarse":     coarse,
	})
	if err != nil {
		return 0, false
	}
	thinking, err := self.dreamThought(ctx, run, budget, fmt.Sprintf("Read %d documents", len(documents)), prompt, true)
	if err != nil {
		// A batch the model's context cannot hold is cut in two and each
		// half read on its own. Twenty openings of twelve hundred runes
		// each are a few hundred tokens over a thirty-two thousand window
		// when the documents are written in Chinese, where a rune costs
		// far more tokens than an English one does, and ten halves of
		// such a batch all fit. Marking the whole batch read instead lost
		// two hundred and twenty documents in two nights, none of which
		// would ever have been read again.
		if llm.IsContextLengthError(err) && len(documents) > 1 {
			if thinking != nil {
				self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents: too long for the model, split in two", len(documents)))
			}
			return self.digestHalves(ctx, run, documents, budget, coarse)
		}
		// One document that on its own does not fit is not going to fit
		// next time either: it is marked read, and its run says why, so
		// the reading moves on rather than stopping at it every dream.
		if llm.IsContextLengthError(err) && thinking != nil {
			self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents: too long for the model, skipped", len(documents)))
			return 0, true
		}
		// Not read: a batch the model never answered is not marked as
		// read. Four thousand documents were, in ten minutes, while the
		// model was unreachable, and would never have been read again.
		log.Warningf("a dream's digest could not ask the model: %s", err)
		return 0, false
	}
	// Answered in words rather than with the object: the words hold the
	// reading's conclusions, so they are handed to one more call with no
	// tools that has only to write the object. A batch that still comes
	// back with none counts as read, so the dream moves on, and its run
	// says so where the person will see it rather than passing for a
	// batch that taught nothing.
	extracted, err := llm.ExtractJSON(thinking.Text)
	if err != nil && len(thinking.Text) > 200 && !textualToolCall(thinking.Text) {
		extracted, err = self.digestObjectFromWords(ctx, run, budget, thinking.Text)
	}
	if err != nil {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents, and answered with no object", len(documents)))
		return 0, true
	}
	answer := &RememberAnswer{}
	if err := json.Unmarshal([]byte(extracted), answer); err != nil {
		self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Read %d documents, and answered with no object", len(documents)))
		return 0, true
	}
	// Evidence points at the document rather than at a conversation:
	// these facts came from something read, not something said.
	filed, err := self.fileWhatWasLearned(ctx, run, answer, nil, models.EvidenceDocument, shown, nil)
	if err != nil {
		// Not read. A window is written whole or not at all, so a failure
		// here means nothing of this batch reached a page -- and marking
		// it read would put what the model found behind a mark that says
		// it was filed. The reading comes back to it in the next dream.
		log.Warningf("cannot file what a dream's digest found: %s", err)
		return 0, false
	}
	// A reading that quoted words nobody wrote says so on its own row, so
	// the night's runs show how often the model invents rather than only
	// how much it filed.
	if checked := filed.Describe(); checked != "" {
		self.retitle(ctx, run, thinking.Conversation,
			fmt.Sprintf("Read %d documents, filed %d, %s", len(documents), filed.Filed, checked))
	}
	return filed.Filed, true
}

// markRead records that a batch's documents were read, so the reading
// does not come back to them.
func markRead(ctx context.Context, run *Run, documents []*models.AgentDocument) {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.ID)
	}
	if err := run.Database().TransactionContext(context.WithoutCancel(ctx), func(tx db.Transaction) error {
		return tx.MarkAgentDocumentsDigested(ids, time.Now())
	}); err != nil {
		log.Warningf("cannot mark what was read: %s", err)
	}
}

// digestHalves reads a batch the model's context could not hold as two
// halves, and answers for the batch as a whole: what both halves filed,
// and answered only where both of them were.
//
// A half that was answered is marked read here, as it finishes, so that
// a half the model never answered leaves only itself for a night that
// answers: the batch as a whole is not marked read, and what was filed
// from the first half is not filed again.
func (self *Agent) digestHalves(ctx context.Context, run *Run, documents []*models.AgentDocument, budget *dreamBudget, coarse bool) (int, bool) {
	middle := len(documents) / 2
	total := 0
	for _, half := range [][]*models.AgentDocument{documents[:middle], documents[middle:]} {
		// The night's deadline and its allowance are checked before each
		// half, as the reading checks them before each batch: a half the
		// night has no time or no tokens for is not read and not marked
		// read.
		if ctx.Err() != nil || !budget.left() {
			return total, false
		}
		filed, answered := self.digestBatch(ctx, run, half, budget, coarse)
		total += filed
		if !answered {
			return total, false
		}
		markRead(ctx, run, half)
	}
	return total, true
}

// digestObjectFromWords asks once more, with no tools, for the object a
// reading in words should have ended with, and hands back the JSON in it.
func (self *Agent) digestObjectFromWords(ctx context.Context, run *Run, budget *dreamBudget, words string) (string, error) {
	prompt, err := render("digest_object.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Reading":    cutRunes(words, 12000),
	})
	if err != nil {
		return "", err
	}
	thinking, err := self.dreamThought(ctx, run, budget, "Wrote the object for a reading given in words", prompt, false)
	if err != nil {
		return "", err
	}
	return llm.ExtractJSON(thinking.Text)
}

// openingOf is as much of a document as the digest reads: its first
// passage, or its title alone where the night is working coarsely.
func (self *Agent) openingOf(ctx context.Context, run *Run, document *models.AgentDocument, coarse bool) string {
	if coarse {
		return ""
	}
	var chunks []*models.AgentChunk
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		chunks, err = tx.ListAgentChunks(run.Agent.ID, document.ID)
		return err
	}); err != nil || len(chunks) == 0 {
		return ""
	}
	return cutRunes(chunks[0].Text, 1200)
}

// --- timeline ---------------------------------------------------------

// dreamTimeline writes up the open month from its digest.
//
// One call a night for the month in hand, and one more when a month
// closes for the year it was in. The whole timeline costs less than one
// conversation, because the collecting is arithmetic (digest.go) and only
// the writing up is a model.
func (self *Agent) dreamTimeline(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	now := time.Now()
	from, until := MonthBounds(now, run.Owner)
	self.writeMonth(ctx, run, record, budget, from, until)

	// The months before: a first ingest brings years of record at once,
	// and a timeline with one page on it is not a timeline. A few a
	// night, most recent first, where the person's own record in the
	// month amounts to something. A month whose page reads like a guess
	// is owed again, after the ones with no page at all: a page that
	// says what a count of threads "suggests" is not a diary.
	var owed []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		months := timelineBackfill
		if run.Agent.DreamBootstrap {
			months = timelineBackfill * 2
		}
		owed, err = tx.ListAgentMonthsToWriteUp(run.Agent.ID, chatNamesOf(run.Owner), timelineLeast, months, guessedPattern)
		return err
	}); err != nil {
		log.Warningf("cannot list the months owed a page: %s", err)
		return
	}
	for _, month := range owed {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		start, err := time.ParseInLocation("2006/01", month, Location(run.Owner))
		if err != nil {
			continue
		}
		self.writeMonth(ctx, run, record, budget, start, start.AddDate(0, 1, 0))
	}
}

const (
	// timelineBackfill is how many past months one night writes up, and
	// timelineLeast how much of the person's own record a month needs
	// before it is worth a page.
	timelineBackfill = 6
	timelineLeast    = 5

	// guessedPattern is a PostgreSQL regular expression for a page that
	// guesses: the words a model reaches for when the record is a count
	// and the page is meant to be a story. A month page that matches is
	// written again from its record, which costs a model call and loses
	// nothing -- the record it was written from is still there.
	guessedPattern = `\m(suggests?|suggesting|likely|indicates?|indicating|probably|presumably|must have|seems? to)\M`
)

// writeMonth writes or rewrites one month's page from its record, and
// links the page to what the month was about.
func (self *Agent) writeMonth(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget, from, until time.Time) {
	digest, err := self.Digest(ctx, run.Agent, run.Owner, from, until)
	if err != nil {
		log.Warningf("cannot digest %s: %s", from.Format("January 2006"), err)
		return
	}
	if strings.TrimSpace(digest) == "" {
		return
	}
	path := PeriodPath(from, true)

	var existing string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(run.Agent.ID, path)
		if err != nil || node == nil {
			return err
		}
		existing = node.Summary
		return nil
	}); err != nil {
		log.Debugf("cannot read the month's page: %s", err)
	}

	prompt, err := render("timeline.txt", map[string]any{
		"PersonName":   personName(run.Owner),
		"Month":        from.Format("January 2006"),
		"Digest":       digest,
		"Existing":     existing,
		"Style":        describeVoice(run.Agent.Voice),
		"Instructions": strings.TrimSpace(run.Agent.Instructions),
	})
	if err != nil {
		return
	}
	said, err := self.dreamThink(ctx, run, budget, "Wrote up "+from.Format("January 2006"), prompt, false)
	if err != nil {
		log.Debugf("cannot write up the month: %s", err)
		return
	}
	// A list of hedging words stood here, and every sentence of the page
	// that contained one was cut out before the page was stored. It could
	// not name every way a model hedges, and each time it was wrong the
	// person lost a sentence nobody ever showed them. The page is kept as
	// it was written; a month that reads like a guess is owed its page
	// again, and written from the record.
	text := strings.TrimSpace(said)
	if text == "" {
		return
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		page, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: path, Kind: models.NodePeriod,
			Name: from.Format("January 2006"), Summary: cutRunes(text, models.SummaryLength),
		})
		if err != nil {
			return err
		}
		// What the month was about: the pages whose facts fall in it,
		// linked from the month, so a walk from the timeline reaches
		// the projects and a walk from a project reaches its months.
		facts, err := tx.ListAgentFactsBetween(run.Agent.ID, from, until, 500)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, fact := range facts {
			counts[fact.NodeID]++
		}
		for nodeId, howMany := range counts {
			if nodeId == page.ID || howMany < 2 {
				continue
			}
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: run.Agent.ID, FromID: page.ID, ToID: nodeId, Relation: models.EdgeAboutPlace,
				Note: fmt.Sprintf("%d facts from %s", howMany, from.Format("January 2006")),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot keep the month's page: %s", err)
		return
	}
	record.Rewritten++
}

// --- consolidate ------------------------------------------------------

// dreamConsolidate rewrites the pages that changed, from their facts.
//
// A page's summary is the digest and its facts are the record, so a page
// that gained four facts today is a page whose opening no longer says
// what it is about.
func (self *Agent) dreamConsolidate(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	var pages []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		pages, err = tx.ListAgentNodesToConsolidate(run.Agent.ID, dreamConsolidate)
		return err
	}); err != nil {
		log.Warningf("cannot list the pages to rewrite: %s", err)
		return
	}
	for _, page := range pages {
		if ctx.Err() != nil || !budget.left() {
			break
		}
		if self.consolidatePage(ctx, run, record, page, budget) {
			record.Rewritten++
		}
	}
}

// consolidatePage rewrites one page and merges what it says twice.
func (self *Agent) consolidatePage(ctx context.Context, run *Run, record *models.AgentDream, page *models.AgentNode, budget *dreamBudget) bool {
	var facts []*models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		facts, err = tx.ListAgentFacts(run.Agent.ID, page.ID, false, 200)
		return err
	}); err != nil {
		return false
	}
	// A page with no facts left and an opening still on it: the opening
	// was written from facts that are gone, so it goes, and no model is
	// asked anything.
	if len(facts) == 0 {
		if strings.TrimSpace(page.Summary) == "" {
			return false
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			empty := *page
			empty.Summary = ""
			if _, err := tx.PutAgentNode(&empty); err != nil {
				return err
			}
			return tx.MarkAgentNodeConsolidated(page.ID, time.Now())
		}); err != nil {
			log.Warningf("cannot clear the opening of %q: %s", page.Path, err)
			return false
		}
		return true
	}

	var lines []string
	for _, fact := range facts {
		lines = append(lines, fmt.Sprintf("#%d %s", fact.Number, fact.Line()))
	}
	prompt, err := render("consolidate.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Path":       page.Path,
		"Name":       page.Name,
		"Kind":       string(page.Kind),
		"Existing":   page.Summary,
		"Facts":      lines,
	})
	if err != nil {
		return false
	}
	said, err := self.dreamThink(ctx, run, budget, "Rewrote the opening of "+page.Path, prompt, false)
	if err != nil {
		log.Warningf("cannot rewrite %q: %s", page.Path, err)
		return false
	}
	// Said rather than shrugged at. A phase that gives up in silence is
	// how a page with thirteen wordings of one sentence sat there for a
	// week while the log reported six pages rewritten.
	extracted, err := llm.ExtractJSON(said)
	if err != nil {
		log.Warningf("cannot rewrite %q: the answer is not an object: %s", page.Path, err)
		return false
	}
	var answer struct {
		Summary string  `json:"summary"`
		Same    [][]int `json:"same"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		log.Warningf("cannot rewrite %q: %s", page.Path, err)
		return false
	}
	// An empty opening is an answer, not a failure: a page whose facts
	// say no more than its own name is better with nothing at the top
	// than with a paragraph saying so at length. The merges below are
	// still worth making, so the pass carries on.
	// The prompt says to write "" for no opening, and a model that writes
	// the two characters is answering as asked: an opening of two quote
	// marks is no opening.
	summary := cutRunes(strings.Trim(strings.TrimSpace(answer.Summary), "\"'\u201c\u201d"), models.SummaryLength)
	if strings.EqualFold(summary, "null") {
		summary = ""
	}
	// Counted, because nothing counted it: the number was in the model,
	// the migration and the dashboard, and every night reported none.
	merged := 0
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: page.AgentID, Path: page.Path, Kind: page.Kind, Name: page.Name,
			Aliases: page.Aliases, ContactID: page.ContactID, Pinned: page.Pinned,
			Importance: page.Importance, Summary: summary,
		}); err != nil {
			return err
		}
		if merged, err = mergeSaidTwice(tx, page.AgentID, facts, answer.Same); err != nil {
			return err
		}
		return tx.MarkAgentNodeConsolidated(page.ID, time.Now())
	}); err != nil {
		log.Warningf("cannot rewrite %q: %s", page.Path, err)
		return false
	}
	record.Merged += merged
	return true
}

// mergeSaidTwice folds the pairs a rewrite called one statement, and says
// how many it folded.
//
// A pair the model called the same thing: the older keeps its number, the
// newer goes dormant behind it. Never deleted -- what it said is still
// readable, and a merge the person disagrees with can be undone.
//
// Every pair is resolved against the page as the pairs before it left it,
// not against the numbering the model was shown. The model answers with
// overlapping pairs -- [[5,3],[5,7]] -- and with chains -- [[1,2],[2,3]]
// -- and both are reasonable answers to "which of these say the same
// thing". Applied from the snapshot, the second pair of each folded a
// live fact behind a row the first pair had already retired, so the page
// stated neither of them and the citation trail led to a dormant row.
func mergeSaidTwice(tx db.Transaction, agentId string, facts []*models.AgentFact, same [][]int) (int, error) {
	byNumber := map[int]*models.AgentFact{}
	for _, fact := range facts {
		byNumber[fact.Number] = fact
	}
	merged := 0
	for _, pair := range same {
		if len(pair) != 2 {
			continue
		}
		// The pair is given best first, because one fact can say
		// everything another says and more.
		best, other := byNumber[pair[0]], byNumber[pair[1]]
		if best == nil || other == nil || best.ID == other.ID {
			continue
		}
		bestNow, err := survivingFact(tx, agentId, best.ID)
		if err != nil {
			return merged, err
		}
		otherNow, err := survivingFact(tx, agentId, other.ID)
		if err != nil {
			return merged, err
		}
		// Both rows already gone, or already folded into one another:
		// there is nothing left of this pair to merge.
		if bestNow == nil || otherNow == nil || bestNow.ID == otherNow.ID {
			continue
		}
		// The number that survives is the lower one, whichever wording
		// wins: a conversation last month cited "things/kittiwake#3", and
		// a citation that stops pointing at anything is worse than a
		// clumsier sentence. So the better words move onto the older
		// number and the newer row goes.
		keep, gone := bestNow, otherNow
		if otherNow.Number < bestNow.Number {
			keep, gone = otherNow, bestNow
		}
		wording := bestNow.Text
		if _, err := tx.UpdateAgentFact(agentId, gone.ID, func(fact *models.AgentFact) error {
			fact.SupersededBy = keep.ID
			return nil
		}); err != nil {
			return merged, err
		}
		if _, err := tx.UpdateAgentFact(agentId, keep.ID, func(fact *models.AgentFact) error {
			fact.Text = wording
			fact.Evidence = append(fact.Evidence, gone.Evidence...)
			if len(fact.Evidence) > models.EvidenceCount {
				fact.Evidence = fact.Evidence[:models.EvidenceCount]
			}
			return nil
		}); err != nil {
			return merged, err
		}
		merged++
	}
	return merged, nil
}

// survivingFact is the row a fact has become: itself, or whatever it was
// folded into, following the chain. Nil where nothing of it is left --
// struck, or deleted under us.
//
// Read back rather than taken from the caller's list, because the pair
// before this one may have moved the wording onto the row this one is
// about.
func survivingFact(tx db.Transaction, agentId, factId string) (*models.AgentFact, error) {
	// A page holds a few hundred facts and a fold chain is a few rows
	// long; this is only here so that a cycle written by an older build
	// cannot spin.
	for hop := 0; hop < 32; hop++ {
		found, err := tx.GetAgentFacts(agentId, []string{factId})
		if err != nil || len(found) == 0 {
			return nil, err
		}
		if found[0].SupersededBy == "" {
			if found[0].Dormant {
				return nil, nil
			}
			return found[0], nil
		}
		factId = found[0].SupersededBy
	}
	return nil, nil
}

// --- organize ---------------------------------------------------------

// dreamOrganize proposes where the orphans belong.
//
// A move to a path that exists is made; anything else is a proposal in
// the log with a button. The night does not invent a hierarchy while
// nobody is watching.
func (self *Agent) dreamOrganize(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var orphans []*models.AgentNode
	var index []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListAgentNodesUnder(run.Agent.ID, models.PathNotes, 60)
		if err != nil {
			return err
		}
		for _, node := range found {
			if node.Path != models.PathNotes {
				orphans = append(orphans, node)
			}
		}
		nodes, err := tx.ListAgentIndex(run.Agent.ID, 200)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			index = append(index, node.IndexLine(120))
		}
		return nil
	}); err != nil || len(orphans) == 0 {
		return
	}

	var lines []string
	for _, orphan := range orphans {
		lines = append(lines, orphan.IndexLine(140))
	}
	prompt, err := render("organize.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Orphans":    lines,
	})
	if err != nil {
		return
	}
	said, err := self.dreamThink(ctx, run, budget, fmt.Sprintf("Offered homes to %d orphan pages", len(orphans)), prompt, true)
	if err != nil {
		return
	}
	extracted, err := llm.ExtractJSON(said)
	if err != nil {
		return
	}
	var answer struct {
		Moves []struct {
			Path   string `json:"path"`
			Under  string `json:"under"`
			Reason string `json:"reason"`
		} `json:"moves"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return
	}
	for _, move := range answer.Moves {
		path := models.NormalizePath(move.Path)
		under := models.NormalizePath(move.Under)
		if path == "" || under == "" {
			continue
		}
		var exists bool
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			node, err := tx.GetAgentNode(run.Agent.ID, under)
			exists = node != nil
			return err
		}); err != nil {
			continue
		}
		if !exists {
			// A page that does not exist yet is a hierarchy being
			// invented. That is the person's to approve.
			record.Proposals = append(record.Proposals, models.DreamProposal{
				Kind: "move", Path: path, To: under, Reason: move.Reason,
			})
			continue
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.MoveAgentNode(run.Agent.ID, path, under)
			return err
		}); err != nil {
			log.Debugf("cannot move %q under %q: %s", path, under, err)
			continue
		}
		record.Moved++
	}
}

// --- vectors ----------------------------------------------------------

// dreamEmbed gives vectors to what has none.
func (self *Agent) dreamEmbed(ctx context.Context, run *Run, record *models.AgentDream) {
	written, err := self.EmbedGraph(ctx, run.Agent, 200)
	if err != nil {
		log.Debugf("cannot embed the graph: %s", err)
	}
	record.Embedded += written
	chunks, _, err := self.embedChunks(ctx, run.Agent, dreamEmbed)
	if err != nil {
		log.Debugf("cannot embed what was indexed: %s", err)
	}
	record.Embedded += chunks
}

// chatNamesOf is what the person may be called in a chat archive: their
// username, and each word of their name, in lower case. A thread whose
// participants include one of these is one they took part in.
func chatNamesOf(owner *models.User) []string {
	return reading.ChatNamesOf(owner)
}

// --- split ------------------------------------------------------------

const (
	// splitAbove is how many facts a page holds before the night divides
	// it, and splitPages how many pages one night divides. A turn reads
	// twenty facts of a page and the tool shows sixty; a page of two
	// hundred is an inventory again, whatever its opening says.
	splitAbove = 40
	splitPages = 5
	// splitLeast is the fewest facts that make a page of their own.
	splitLeast = 3
)

// dreamSplit divides a page that has outgrown itself into children by
// theme, one call per page, moving the facts and leaving the parent with
// its opening and its links.
func (self *Agent) dreamSplit(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var crowded []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		pages := splitPages
		if run.Agent.DreamBootstrap {
			pages = splitPages * 2
		}
		crowded, err = tx.ListAgentNodesCrowded(run.Agent.ID, splitAbove, pages)
		return err
	}); err != nil {
		log.Warningf("cannot list the pages that have outgrown themselves: %s", err)
		return
	}
	for _, page := range crowded {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		moved, err := self.splitPage(ctx, run, budget, page)
		if err != nil {
			log.Warningf("cannot divide %q: %s", page.Path, err)
			continue
		}
		record.Moved += moved
	}
}

func (self *Agent) splitPage(ctx context.Context, run *Run, budget *dreamBudget, page *models.AgentNode) (int, error) {
	var facts []*models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		facts, err = tx.ListAgentFacts(run.Agent.ID, page.ID, false, 400)
		return err
	}); err != nil {
		return 0, err
	}
	byNumber := map[int]*models.AgentFact{}
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		byNumber[fact.Number] = fact
		lines = append(lines, fmt.Sprintf("#%d %s", fact.Number, cutRunes(fact.Text, 300)))
	}
	prompt, err := render("split.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Path":       page.Path,
		"Name":       page.Name,
		"Kind":       string(page.Kind),
		"Opening":    cutRunes(page.Summary, 600),
		"Facts":      lines,
		"Self":       page.Path == models.PathSelf,
	})
	if err != nil {
		return 0, err
	}
	said, err := self.dreamThink(ctx, run, budget, "Divided "+page.Path, prompt, false)
	if err != nil {
		return 0, err
	}
	extracted, err := llm.ExtractJSON(said)
	if err != nil {
		return 0, err
	}
	var answer struct {
		Groups []struct {
			Name  string `json:"name"`
			Slug  string `json:"slug"`
			Facts []int  `json:"facts"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return 0, err
	}

	moved := 0
	taken := map[int]bool{}
	for _, group := range answer.Groups {
		slug := models.NormalizePath(strings.ToLower(strings.TrimSpace(group.Slug)))
		name := strings.TrimSpace(group.Name)
		if slug == "" || strings.Contains(slug, "/") || name == "" {
			continue
		}
		var chosen []*models.AgentFact
		for _, number := range group.Facts {
			if fact := byNumber[number]; fact != nil && !taken[number] {
				chosen = append(chosen, fact)
			}
		}
		if len(chosen) < splitLeast {
			continue
		}
		childPath := models.JoinPath(page.Path, slug)
		kind := page.Kind
		if page.Path == models.PathSelf {
			kind = models.NodeTopic
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			child, err := tx.GetAgentNode(run.Agent.ID, childPath)
			if err != nil {
				return err
			}
			if child == nil {
				child, err = tx.PutAgentNode(&models.AgentNode{
					AgentID: run.Agent.ID, Path: childPath, ParentID: page.ID, Kind: kind, Name: name,
				})
				if err != nil {
					return err
				}
			}
			for _, fact := range chosen {
				if _, err := tx.MoveAgentFact(run.Agent.ID, fact.ID, child.ID); err != nil {
					// A fact struck or merged away while the model was
					// thinking -- a reading pass refiles the person's own
					// page every run -- is not a reason to leave the rest.
					if errors.Is(err, db.ErrNoSuchFact) {
						continue
					}
					return err
				}
				taken[fact.Number] = true
				moved++
			}
			// Both pages are due a fresh opening.
			if err := tx.MarkAgentNodeConsolidated(child.ID, time.Time{}); err != nil {
				return err
			}
			return tx.MarkAgentNodeConsolidated(page.ID, time.Time{})
		}); err != nil {
			return moved, err
		}
		log.Noticef("divided %s: %d facts now under %s", page.Path, len(chosen), childPath)
	}
	return moved, nil
}
