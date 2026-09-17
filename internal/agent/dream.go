package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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
	dreamQuietBootstrap  = 5 * time.Minute

	// dreamLongest is how long one night may run. The reading takes
	// half of it at most (see halfway), so the rest is never starved.
	dreamLongest = 45 * time.Minute

	// dreamDigest is how many things one night reads at full resolution,
	// and dreamConsolidate how many pages it rewrites.
	dreamDigest      = 2000
	dreamConsolidate = 100

	// dreamBatch is how many items go into one call of the digest phase.
	dreamBatch = 20

	// digestSmallest is the size below which a document is not worth a
	// share of a call: about two short lines.
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
		// person is reading is a run that looks broken.
		conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{
			models.AgentConversationMain, models.AgentConversationNamed,
		}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			if now.Sub(conversation.LastAt) < quiet {
				busy = true
			}
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

	record := &models.AgentDream{AgentID: run.Agent.ID, StartedAt: time.Now(), JobID: run.Job.ID}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		record, err = tx.StartAgentDream(record)
		return err
	}); err != nil {
		return err
	}

	// A night spends its own share of the day, so it never eats the day.
	share := configuration.Agent.Limits.DreamShare
	if share <= 0 {
		share = dreamShareDefault
	}
	budget := newDreamBudget(configuration, run.Agent, share)

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
	self.dreamTimeline(ctx, run, record, budget)
	self.dreamConsolidate(ctx, run, record, budget)
	self.dreamOrganize(ctx, run, record, budget)
	self.dreamSplit(ctx, run, record, budget)
	self.dreamQuietHalf(ctx, run, record)
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

// dreamTools is what one call of a dream may reach: the graph, to look a
// page up before filing to it, and the sources, to read a document whole
// when its first passage is not enough. Read-only: what a dream changes
// it changes in code, from the object the call ends with, so that every
// fact keeps its evidence and every move stays a proposal.
var dreamTools = map[string]bool{"memory": true, "knowledge": true}

// dreamFrame is what every call of a dream is told first: the tools are
// for looking, and what should change goes in the object at the end.
const dreamFrame = "You are working through your own memory with nobody present. The memory and knowledge tools are read-only in this run: use `get` and `search` to look a page up, never `note`, `page`, `link`, `move`, `merge` or `forget` -- every change you want is said in the object you end with, and code makes it with its evidence. When several pages need looking up, look them all up in one `batch` call of up to eight `get` and `search` items rather than one call at a time; one such call, at most two, then answer with the object."

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
	tools, rounds := noTools, 1
	if lookups {
		// Said before the prompt, because the memory tool's own
		// description invites the model to note what it learns, and a
		// dream that tried to note ran out of rounds refused and never
		// answered.
		tools, rounds = dreamTools, roundsFor(run.Configuration(), models.AgentJobDream)
		prompt = dreamFrame + "\n\n" + prompt
	}
	thinking, err := self.think(ctx, run, title, prompt, tools, rounds, models.AgentJobDream, config.AgentWorkScan)
	if thinking != nil {
		budget.note(thinking.Usage)
	}
	return thinking, err
}

// dreamBudget is what a night may spend.
type dreamBudget struct {
	// Read and written from several batches at once when the reading
	// runs concurrently.
	mutex   sync.Mutex
	allowed int64
	spent   int64

	// digestUntil is when the reading has to stop so the rest of the
	// night gets its turn; zero means the night has no deadline.
	digestUntil time.Time
}

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

func newDreamBudget(configuration *config.Configuration, agent *models.Agent, share float64) *dreamBudget {
	daily := agent.DailyTokens
	if daily <= 0 {
		daily = configuration.Agent.Limits.DailyTokensPerAgent
	}
	if daily <= 0 {
		return &dreamBudget{allowed: 0} // no cap
	}
	return &dreamBudget{allowed: int64(float64(daily) * share)}
}

// left says whether there is budget for another call.
func (self *dreamBudget) left() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.allowed == 0 || self.spent < self.allowed
}

func (self *dreamBudget) note(usage llm.Usage) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
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

	// A document too small to say anything -- a channel-day that is one
	// person joining, a file of twenty bytes -- is marked read without a
	// call. Four hundred of them a night, forty to a call, filed three
	// facts; the calls are better spent on the ones with words in them.
	var tiny []string
	kept := waiting[:0]
	for _, document := range waiting {
		if document.Bytes < digestSmallest {
			tiny = append(tiny, document.ID)
			continue
		}
		kept = append(kept, document)
	}
	waiting = kept
	if len(tiny) > 0 {
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.MarkAgentDocumentsDigested(tiny, time.Now())
		}); err != nil {
			log.Warningf("cannot mark the small ones as read: %s", err)
		}
	}

	// Batches go to the model a few at a time where it has the slots: a
	// service metered by the call gets one, a model of the person's own
	// as many as it can take. Each batch marks its own documents read as
	// it finishes, so a night that ends between batches loses nothing.
	concurrency := run.Configuration().Agent.Limits.ScanConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	var mutex sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, concurrency)
	stopped := false
	for start := 0; start < len(waiting); start += dreamBatch {
		mutex.Lock()
		halt := stopped
		mutex.Unlock()
		if halt || ctx.Err() != nil || !budget.left() || !budget.readingTimeLeft() {
			break
		}
		end := start + dreamBatch
		if end > len(waiting) {
			end = len(waiting)
		}
		batch := waiting[start:end]
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			filed, answered := self.digestBatch(ctx, run, batch, budget, record.Coarse)
			mutex.Lock()
			defer mutex.Unlock()
			if !answered {
				// The model is not answering tonight; what it did not
				// read waits for a night when it does.
				record.LastError = "the model did not answer; the reading stops here"
				stopped = true
				return
			}
			record.Digested += len(batch)
			record.Filed += filed
			ids := make([]string, 0, len(batch))
			for _, document := range batch {
				ids = append(ids, document.ID)
			}
			if err := run.Database().TransactionContext(context.WithoutCancel(ctx), func(tx db.Transaction) error {
				return tx.MarkAgentDocumentsDigested(ids, time.Now())
			}); err != nil {
				log.Warningf("cannot mark what was read: %s", err)
			}
		}()
	}
	group.Wait()
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
	for _, document := range documents {
		builder.WriteString("[" + document.ID + "] " + document.Cite())
		if author := document.Author(); author != "" {
			builder.WriteString(" — " + author)
		}
		if document.HappenedAt != nil {
			builder.WriteString(" — " + document.HappenedAt.Format("2 Jan 2006"))
		}
		builder.WriteString("\n")
		if text := self.openingOf(ctx, run, document, coarse); text != "" {
			builder.WriteString(unclosable(text) + "\n")
		}
		builder.WriteString("\n")
	}

	prompt, err := render("digest.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Items":      builder.String(),
		"Most":       dreamBatch / 4,
		"Coarse":     coarse,
	})
	if err != nil {
		return 0, false
	}
	thinking, err := self.dreamThought(ctx, run, budget, fmt.Sprintf("Read %d documents", len(documents)), prompt, true)
	if err != nil {
		// A batch the model's context cannot hold is not going to fit
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
	filed, err := self.fileWhatWasLearned(ctx, run, answer, nil, models.EvidenceDocument)
	if err != nil {
		log.Debugf("cannot file what a dream's digest found: %s", err)
	}
	return filed, true
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
	// What the model guessed comes out here, not in the prompt: told not
	// to, it guesses anyway, and a page that guesses is owed again.
	text := dropGuesses(strings.TrimSpace(said))
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
	byNumber := map[int]*models.AgentFact{}
	for _, fact := range facts {
		byNumber[fact.Number] = fact
	}
	// Counted, because nothing counted it: the number was in the model,
	// the migration and the dashboard, and every night reported none.
	merged := 0
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: page.AgentID, Path: page.Path, Kind: page.Kind, Name: page.Name,
			Aliases: page.Aliases, ContactID: page.ContactID, Pinned: page.Pinned,
			Importance: page.Importance, Summary: summary,
		}); err != nil {
			return err
		}
		// A pair the model called the same thing: the older keeps its
		// number, the newer goes dormant. Never deleted -- what it said
		// is still readable, and a merge the person disagrees with can
		// be undone.
		for _, pair := range answer.Same {
			if len(pair) != 2 {
				continue
			}
			// The pair is given best first, because one fact can say
			// everything another says and more.
			best, other := byNumber[pair[0]], byNumber[pair[1]]
			if best == nil || other == nil || best.ID == other.ID {
				continue
			}
			// But the number that survives is the lower one, whichever
			// wording wins: a conversation last month cited
			// "things/kittiwake#3", and a citation that stops pointing at
			// anything is worse than a clumsier sentence. So the better
			// words move onto the older number and the newer row goes.
			keep, gone := best, other
			if other.Number < best.Number {
				keep, gone = other, best
			}
			wording := best.Text
			if _, err := tx.UpdateAgentFact(page.AgentID, gone.ID, func(fact *models.AgentFact) error {
				fact.SupersededBy = keep.ID
				return nil
			}); err != nil {
				return err
			}
			if _, err := tx.UpdateAgentFact(page.AgentID, keep.ID, func(fact *models.AgentFact) error {
				fact.Text = wording
				fact.Evidence = append(fact.Evidence, gone.Evidence...)
				if len(fact.Evidence) > models.EvidenceCount {
					fact.Evidence = fact.Evidence[:models.EvidenceCount]
				}
				return nil
			}); err != nil {
				return err
			}
			merged++
		}
		return tx.MarkAgentNodeConsolidated(page.ID, time.Now())
	}); err != nil {
		log.Warningf("cannot rewrite %q: %s", page.Path, err)
		return false
	}
	record.Merged += merged
	return true
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
	if owner == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if len(name) < 3 || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(owner.Username)
	for _, word := range strings.Fields(owner.Name) {
		add(word)
	}
	return names
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
