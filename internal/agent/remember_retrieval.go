package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// rememberPageFacts is how many of a touched page's facts the filing run
// is shown, rememberSummaryCharacters how much of its opening, and
// rememberFactsSearched how many facts the conversation's words are
// matched against to choose them. rememberIndexReadPages is how many pages
// its index reads to fill rememberIndexTokens.
const (
	rememberPageFacts         = 20
	rememberSummaryCharacters = 800
	rememberFactsSearched     = 60
	rememberIndexReadPages    = 300
)

// pagesTouched is the pages the conversation's own words touch, so that a
// run does not file what a page already says.
//
// Each page shows the facts the conversation's words hit on it, then its
// newest, to rememberPageFacts, in number order, and says how many more
// it has. It showed its first twenty by number, the oldest, and on a long
// page the run filed again what a later fact already said.
func (self *Agent) pagesTouched(tx db.Transaction, agentId string, messages []*models.AgentMessage, limit int) ([]string, error) {
	var words strings.Builder
	for _, message := range messages {
		if message.Role == string(llm.RoleUser) {
			words.WriteString(message.Content)
			words.WriteByte('\n')
		}
	}
	// One search, asked for more rows than pages are shown: the pages
	// come back in the same order whatever the limit, and the facts it
	// finds are what the words hit.
	nodes, hitFacts, err := tx.SearchAgentGraph(agentId, words.String(), max(limit, rememberFactsSearched))
	if err != nil {
		return nil, err
	}
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}
	isHit := make(map[string]bool, len(hitFacts))
	for _, fact := range hitFacts {
		isHit[fact.ID] = true
	}
	pages := make([]string, 0, len(nodes))
	for _, node := range nodes {
		facts, err := tx.ListAgentFacts(agentId, node.ID, false, everyFactOnPage)
		if err != nil {
			return nil, err
		}
		shown := factsForFiling(facts, isHit, rememberPageFacts)
		block := node.Path
		if node.Name != "" {
			block += " — " + node.Name
		}
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			block += "\n" + cutMarked(summary, rememberSummaryCharacters)
		}
		for _, fact := range shown {
			block += "\n#" + fmt.Sprint(fact.Number) + " " + fact.Line()
		}
		if moreCount := len(facts) - len(shown); moreCount > 0 {
			block += fmt.Sprintf("\n(and %d more facts on this page, not shown here)", moreCount)
		}
		pages = append(pages, block)
	}
	return pages, nil
}

// factsForFiling is at most most of a page's facts, which are in number
// order: the ones hit first, then the newest, returned in number order.
func factsForFiling(facts []*models.AgentFact, isHit map[string]bool, most int) []*models.AgentFact {
	if len(facts) <= most {
		return facts
	}
	isChosen := make(map[string]bool, most)
	chosenCount := 0
	for _, fact := range facts {
		if chosenCount < most && isHit[fact.ID] {
			isChosen[fact.ID] = true
			chosenCount++
		}
	}
	for index := len(facts) - 1; index >= 0 && chosenCount < most; index-- {
		if !isChosen[facts[index].ID] {
			isChosen[facts[index].ID] = true
			chosenCount++
		}
	}
	chosen := make([]*models.AgentFact, 0, most)
	for _, fact := range facts {
		if isChosen[fact.ID] {
			chosen = append(chosen, fact)
		}
	}
	return chosen
}

// rememberMaterial is the stored context shown beside one conversation window.
type rememberMaterial struct {
	IndexLines          []string
	PageBlocks          []string
	UnlearnedStatements []string
}

func (self *Agent) retrieveRememberMaterial(ctx context.Context, database db.Database, agentId string, unread []*models.AgentMessage) (*rememberMaterial, error) {
	material := &rememberMaterial{}
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agentId); err != nil {
			return err
		}
		nodes, err := tx.ListAgentIndex(agentId, rememberIndexReadPages)
		if err != nil {
			return err
		}
		// The filing run has no tools, so the line about the pages left
		// out says how many and not how to list them.
		material.IndexLines, _ = indexLines(nodes, rememberIndexTokens, len(nodes) >= rememberIndexReadPages, false)
		// The pages this conversation already touched, so the run adds
		// to them rather than saying again what they say.
		material.PageBlocks, err = self.pagesTouched(tx, agentId, unread, rememberPages)
		if err != nil {
			return err
		}
		struck, err := tx.ListAgentFeedback(agentId, []models.AgentFeedbackKind{models.FeedbackUnlearned}, 20)
		if err != nil {
			return err
		}
		for _, entry := range struck {
			material.UnlearnedStatements = append(material.UnlearnedStatements, entry.Said)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return material, nil
}
