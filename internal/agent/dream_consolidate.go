package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

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
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"Path":              page.Path,
		"Name":              page.Name,
		"Kind":              string(page.Kind),
		"Existing":          page.Summary,
		"Facts":             lines,
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
	//
	// And an answer that could not be read leaves the page as it was.
	// `{}` and an error object used to read as an empty opening, and the
	// page's summary was blanked.
	read := readModelAnswer[consolidateAnswer](said, "summary")
	if !read.IsValid {
		log.Warningf("cannot rewrite %q: %s", page.Path, read.Problem)
		return false
	}
	answer := read.Value
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

// consolidateAnswer is what the rewrite of a page answers with.
type consolidateAnswer struct {
	Summary string  `json:"summary"`
	Same    [][]int `json:"same"`
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
		// The model reads the words; it does not get to call an event on
		// one day and the same event on another one statement, or a state
		// and an event. See couldBeOneStatement.
		if !couldBeOneStatement(bestNow, otherNow) {
			continue
		}
		// Keep the lower number so existing citations still resolve.
		// The better wording moves onto that row.
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
