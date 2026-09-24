package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

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
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"Month":             from.Format("January 2006"),
		"Digest":            digest,
		"Existing":          existing,
		"Style":             describeVoice(run.Agent.Voice),
		"Instructions":      strings.TrimSpace(run.Agent.Instructions),
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
