package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// pagesTouched is the pages the conversation's own words touch, in full,
// so that a run does not file what a page already says.
func (self *Agent) pagesTouched(tx db.Transaction, agentId string, messages []*models.AgentMessage, limit int) ([]string, error) {
	var words strings.Builder
	for _, message := range messages {
		if message.Role == string(llm.RoleUser) {
			words.WriteString(message.Content)
			words.WriteByte('\n')
		}
	}
	nodes, _, err := tx.SearchAgentGraph(agentId, words.String(), limit)
	if err != nil {
		return nil, err
	}
	pages := make([]string, 0, len(nodes))
	for _, node := range nodes {
		facts, err := tx.ListAgentFacts(agentId, node.ID, false, 20)
		if err != nil {
			return nil, err
		}
		block := node.Path
		if node.Name != "" {
			block += " — " + node.Name
		}
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			block += "\n" + cutRunes(summary, 800)
		}
		for _, fact := range facts {
			block += "\n#" + fmt.Sprint(fact.Number) + " " + fact.Line()
		}
		pages = append(pages, block)
	}
	return pages, nil
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
		nodes, err := tx.ListAgentIndex(agentId, 300)
		if err != nil {
			return err
		}
		spent := 0
		for _, node := range nodes {
			if node.Kind == models.NodeFolder && node.Summary == "" && node.ParentID == "" {
				continue
			}
			line := node.IndexLine(140)
			cost := llm.EstimateTokens(line)
			if spent+cost > rememberIndexTokens {
				break
			}
			spent += cost
			material.IndexLines = append(material.IndexLines, line)
		}
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
