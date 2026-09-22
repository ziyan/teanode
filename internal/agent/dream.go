package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

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
			if agent.DreamBootstrap && caughtUp(record) {
				agent.DreamBootstrap = false
				log.Noticef("agent %s has finished bootstrapping; dreaming is back to its hours", run.Agent.ID)
			}
			return nil
		})
		return err
	})
}

// caughtUp says whether a finished night means there is nothing left to
// catch up on, which is when bootstrapping switches itself off.
//
// Nothing waiting, or nothing read, which is the same thing said by a
// night that had nothing to read. A night that says what went wrong is
// neither: it did not read because it could not, and the backlog it left
// is still there.
func caughtUp(record *models.AgentDream) bool {
	if record.LastError != "" {
		return false
	}
	return record.Backlog-record.Digested <= 0 || record.Digested == 0
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
