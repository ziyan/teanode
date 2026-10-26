package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// dreamTimeline writes up the open month from its digest.
//
// One call a night for the month in hand, and one more when a month
// closes for the year it was in. The whole timeline costs less than one
// conversation, because the collecting is arithmetic (digest.go) and only
// the writing up is a model.
//
// A month the journal has written a narrative for is not written up here:
// the journal is the one writer of a month's story, and the page is made from
// its narrative with no model call, before the budget is looked at.
func (self *Agent) dreamTimeline(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	journalMonths := self.writeMonthsFromJournal(ctx, run, record)
	if !budget.left() {
		return
	}
	now := time.Now()
	from, until := MonthBounds(now, run.Owner)
	if !journalMonths[from.Format("2006/01")] {
		self.writeMonth(ctx, run, record, budget, from, until)
	}

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
		owed, err = tx.ListAgentMonthsToWriteUp(run.Agent.ID, chatNamesOf(run.Owner), timelineLeast, months, guessedPattern, timelineRulesSince)
		return err
	}); err != nil {
		log.Warningf("cannot list the months owed a page: %s", err)
		return
	}
	for _, month := range owed {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		if journalMonths[month] {
			continue
		}
		start, err := time.ParseInLocation("2006/01", month, Location(run.Owner))
		if err != nil {
			continue
		}
		self.writeMonth(ctx, run, record, budget, start, start.AddDate(0, 1, 0))
	}
}

// timelineRulesSince is when the rules a month is written by last changed.
// A page written before it is owed again, and written from nothing rather
// than revised: revising keeps what the page already says, and what was
// wrong with the pages before it was what they said. They wrote everything
// the record held about a month as the person's own doing, so a library
// copied into a repository, carrying its copyright year, came out as a
// month they spent working on it.
var timelineRulesSince = time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)

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
		if !node.ModifiedAt.Before(timelineRulesSince) {
			existing = node.Summary
		}
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
	self.keepMonthPage(ctx, run, record, from, until, path, text)
}

// keepMonthPage stores a month's page and links it to what the month was
// about.
func (self *Agent) keepMonthPage(ctx context.Context, run *Run, record *models.AgentDream, from, until time.Time, path, text string) {
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		page, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: path, Kind: models.NodePeriod,
			Name: from.Format("January 2006"), Summary: text,
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

// writeMonthsFromJournal writes the month pages from the narratives of the
// journal's months, with no model call, and answers which months it wrote
// ("2006/01"). A page that already says the same is left alone. A memory page
// cannot link to a journal period, so the page says where the month is in the
// journal, and says whom it is written to: the journal's narrative speaks to
// the person as "you".
func (self *Agent) writeMonthsFromJournal(ctx context.Context, run *Run, record *models.AgentDream) map[string]bool {
	months := map[string]bool{}
	var periods []*models.JournalPeriod
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		permissions, err := tx.EffectivePermissions(run.Owner.ID)
		if err != nil || !permissions.Has(models.PermissionJournalUse) {
			return err
		}
		periods, err = tx.ListJournalPeriods(run.Owner.ID, models.JournalPeriodMonth, "", "")
		return err
	}); err != nil {
		log.Warningf("cannot list the journal's months: %s", err)
		return months
	}
	for _, period := range periods {
		if ctx.Err() != nil {
			return months
		}
		if strings.TrimSpace(period.Narrative) == "" {
			continue
		}
		from, err := time.ParseInLocation(journal.DayLayout, period.PeriodStart, Location(run.Owner))
		if err != nil {
			continue
		}
		until := from.AddDate(0, 1, 0)
		path := PeriodPath(from, true)
		months[from.Format("2006/01")] = true

		var text, existing string
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			text = self.monthPageFromJournal(tx, run, period, from)
			node, err := tx.GetAgentNode(run.Agent.ID, path)
			if err != nil || node == nil {
				return err
			}
			existing = node.Summary
			return nil
		}); err != nil {
			log.Debugf("cannot make the page of %s from the journal: %s", from.Format("January 2006"), err)
			continue
		}
		if text != "" && text != existing {
			self.keepMonthPage(ctx, run, record, from, until, path, text)
		}
	}
	return months
}

// monthPageFromJournal is the text of a month page made from the journal's
// narrative of the month. A mention of a record is replaced by the record's
// title, or taken out when the agent may not read the record.
func (self *Agent) monthPageFromJournal(tx db.Transaction, run *Run, period *models.JournalPeriod, from time.Time) string {
	reader := recordlink.Reader{UserID: run.Owner.ID, AgentID: run.Agent.ID, IsAgent: true}
	narrative := journalReferencePattern.ReplaceAllStringFunc(period.Narrative, func(mention string) string {
		parts := journalReferencePattern.FindStringSubmatch(mention)
		resolved, err := recordlink.ResolveRecord(tx, reader, models.AgentRecordKind(parts[1]), parts[2], models.AgentRecordDetail{}, 0, 0)
		if err != nil || resolved == nil || !resolved.IsReadable() || resolved.Title == "" {
			return ""
		}
		return resolved.Title
	})
	var builder strings.Builder
	if headline := strings.TrimSpace(period.Headline); headline != "" {
		builder.WriteString("**" + headline + "**\n\n")
	}
	builder.WriteString(strings.TrimSpace(narrative))
	builder.WriteString(fmt.Sprintf("\n\nThis is the journal's account of the month, written to %s as \"you\". The journal page is /journal/month/%s.", personName(run.Owner), from.Format("2006-01")))
	return builder.String()
}
