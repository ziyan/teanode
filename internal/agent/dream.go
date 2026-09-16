package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

	// dreamEvery is how often the sweep looks.
	dreamEvery = 5 * time.Minute

	// dreamLongest is how long one night may run. The reading takes
	// half of it at most (see halfway), so the rest is never starved.
	dreamLongest = 45 * time.Minute

	// dreamDigest is how many things one night reads at full resolution,
	// and dreamConsolidate how many pages it rewrites.
	dreamDigest      = 400
	dreamConsolidate = 100

	// dreamBatch is how many items go into one call of the digest phase.
	dreamBatch = 40

	// digestSmallest is the size below which a document is not worth a
	// share of a call: about two short lines.
	digestSmallest = 160

	// dreamCoarseAboveRetired was the backlog at which a night read titles
	// instead of contents. Retired: see dreamDigest.
	// dreamCoarseAbove is the backlog at which a night stops reading
	// things one by one and works a stretch at a time instead. Two
	// thousand is about five nights at full resolution: below that,
	// waiting is reasonable.
	dreamCoarseAbove = 2000

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
			log.Warningf("cannot queue a nightly run for agent %q: %s", agent.ID, err)
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
	if agent.DreamedAt != nil && now.Sub(*agent.DreamedAt) < dreamApart {
		return false
	}
	if !insideWindow(agent, owner, now) {
		return false
	}
	// Not while they are talking. A run that rewrites a page the person
	// is reading is a run that looks broken.
	var busy bool
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		conversations, err := tx.ListAgentConversations(agent.ID, []models.AgentConversationKind{
			models.AgentConversationMain, models.AgentConversationNamed,
		}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			if now.Sub(conversation.LastAt) < dreamQuiet {
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

	record := &models.AgentDream{AgentID: run.Agent.ID, StartedAt: time.Now()}
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
	budget.digestUntil = halfway(ctx, time.Now())
	self.dreamDigest(ctx, run, record, budget)
	self.dreamTimeline(ctx, run, record, budget)
	self.dreamConsolidate(ctx, run, record, budget)
	self.dreamOrganize(ctx, run, record, budget)
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
			return nil
		})
		return err
	})
}

// dreamBudget is what a night may spend.
type dreamBudget struct {
	allowed int64
	spent   int64

	// digestUntil is when the reading has to stop so the rest of the
	// night gets its turn; zero means the night has no deadline.
	digestUntil time.Time
}

// halfway is the moment half of the night's remaining time is gone, or
// zero when the night has no deadline.
func halfway(ctx context.Context, now time.Time) time.Time {
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(now) {
		return time.Time{}
	}
	return now.Add(deadline.Sub(now) / 2)
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
	return self.allowed == 0 || self.spent < self.allowed
}

func (self *dreamBudget) note(usage llm.Usage) {
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
		waiting, backlog, err = tx.ListAgentDocumentsToDigest(run.Agent.ID, chatNamesOf(run.Owner), dreamDigest)
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

	for start := 0; start < len(waiting); start += dreamBatch {
		if ctx.Err() != nil || !budget.left() || !budget.readingTimeLeft() {
			break
		}
		end := start + dreamBatch
		if end > len(waiting) {
			end = len(waiting)
		}
		filed := self.digestBatch(ctx, run, waiting[start:end], budget, record.Coarse)
		record.Digested += end - start
		record.Filed += filed
	}
	// However far it got, the ones it read are marked, so tomorrow starts
	// where tonight stopped rather than at the beginning.
	ids := make([]string, 0, record.Digested)
	for index := 0; index < record.Digested && index < len(waiting); index++ {
		ids = append(ids, waiting[index].ID)
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentDocumentsDigested(ids, time.Now())
	}); err != nil {
		log.Warningf("cannot mark what was read: %s", err)
	}
}

// digestBatch reads a handful of documents and files what they taught.
func (self *Agent) digestBatch(ctx context.Context, run *Run, documents []*models.AgentDocument, budget *dreamBudget, coarse bool) int {
	configuration := run.Configuration()
	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return 0
	}
	modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)

	var index []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		lines, err := memoryLines(tx, run.Agent.ID, models.AudienceAsk, 10, false)
		index = lines
		return err
	}); err != nil {
		log.Debugf("cannot read the index for a nightly run: %s", err)
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
		return 0
	}
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 1500,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		log.Debugf("a nightly digest could not ask the model: %s", err)
		return 0
	}
	extracted, err := llm.ExtractJSON(response.Message.Content)
	if err != nil {
		return 0
	}
	answer := &RememberAnswer{}
	if err := json.Unmarshal([]byte(extracted), answer); err != nil {
		return 0
	}
	// Evidence points at the document rather than at a conversation:
	// these facts came from something read, not something said.
	filed, err := self.fileWhatWasLearned(ctx, run, answer, nil)
	if err != nil {
		log.Debugf("cannot file what a nightly digest found: %s", err)
	}
	return filed
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
	digest, err := self.Digest(ctx, run.Agent, run.Owner, from, until)
	if err != nil {
		log.Warningf("cannot digest the month: %s", err)
		return
	}
	if strings.TrimSpace(digest) == "" {
		return
	}
	path := PeriodPath(now, true)

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

	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return
	}
	modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)
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
	configuration := run.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 2000,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		log.Debugf("cannot write up the month: %s", err)
		return
	}
	text := strings.TrimSpace(response.Message.Content)
	if text == "" {
		return
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: path, Kind: models.NodePeriod,
			Name: from.Format("January 2006"), Summary: cutRunes(text, models.SummaryLength),
		})
		return err
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
	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return false
	}
	modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)

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
	configuration := run.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		// Room for a page with many facts on it. At nine hundred a page
		// with thirteen of them came back empty -- the model had spent
		// its allowance before writing anything -- and the page that
		// most needed its duplicates merged was the one that never got
		// looked at.
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 3000,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		log.Warningf("cannot rewrite %q: %s", page.Path, err)
		return false
	}
	// Said rather than shrugged at. A phase that gives up in silence is
	// how a page with thirteen wordings of one sentence sat there for a
	// week while the log reported six pages rewritten.
	extracted, err := llm.ExtractJSON(response.Message.Content)
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
	summary := cutRunes(strings.TrimSpace(answer.Summary), models.SummaryLength)
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
	provider, model, err := run.Registry().ForWork(config.AgentWorkScan)
	if err != nil {
		return
	}
	modelName := run.Registry().Configuration().Models.ForWork(config.AgentWorkScan)

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
	configuration := run.Configuration()
	callContext, cancel := context.WithTimeout(ctx, configuration.Agent.Limits.RequestTimeout.Duration())
	defer cancel()
	response, err := provider.Chat(callContext, &llm.ChatRequest{
		Model: model, Messages: []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}}, MaxTokens: 1200,
	})
	if response != nil {
		RecordUsage(run.Database(), run.Agent.ID, "", modelName, string(models.AgentJobDream), response.Usage)
		budget.note(response.Usage)
	}
	if err != nil {
		return
	}
	extracted, err := llm.ExtractJSON(response.Message.Content)
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
