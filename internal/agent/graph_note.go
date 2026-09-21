package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// NoteFact gives a fact its vector as it is written and answers whatever
// on the same page is near enough to be the same thing said twice.
//
// Within one page, and with a second test beside the cosine: the two must
// share a proper noun or a number where either has one. Two short
// sentences about two different people sit above nine tenths of each
// other, and a merge on that evidence alone loses one of them.
func (self *AskRun) NoteFact(ctx context.Context, fact *models.AgentFact) []*models.AgentFact {
	if fact == nil {
		return nil
	}
	agentId := self.settings.Agent.ID
	var path, name string
	var aliases []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNodeByID(agentId, fact.NodeID)
		if err != nil || node == nil {
			return err
		}
		path, name = node.Path, node.Name
		aliases = node.Aliases
		return nil
	}); err != nil {
		log.Warningf("cannot read the page a fact is on: %s", err)
	}
	vectors, modelName, ok := self.agent.embed(ctx, agentId, "ask", []string{factText(fact, path, name)})
	if !ok {
		return nil
	}
	var twins []*models.AgentFact
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.PutAgentFactVector(agentId, fact.ID, modelName, vectors[0]); err != nil {
			return err
		}
		scores, err := tx.Nearest(db.AgentFactTable, agentId, modelName, vectors[0], 6, db.VectorQuery{
			Where:     []string{`"fact_id" IN (SELECT "id" FROM "agent_fact" WHERE "node_id" = ? AND "id" <> ? AND NOT "dormant" AND "superseded_by" IS NULL)`},
			Arguments: []any{fact.NodeID, fact.ID},
			Floor:     twinFloor,
		})
		if err != nil {
			return err
		}
		candidates, err := tx.GetAgentFacts(agentId, idsOf(scores))
		if err != nil {
			return err
		}
		// The page's own name is not evidence either way; see sharesAName.
		itsOwn := append([]string{name}, aliases...)
		for _, candidate := range orderFacts(candidates, idsOf(scores)) {
			if sharesAName(fact.Text, candidate.Text, itsOwn...) {
				twins = append(twins, candidate)
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot keep a fact's vector: %s", err)
		return nil
	}
	return twins
}

// PreparePage obtains a page-name embedding before the caller opens its write
// transaction. The returned resolver keeps alias matching and page creation
// in that transaction, using the prepared vector or the existing word fallback.
func (self *AskRun) PreparePage(ctx context.Context, path string, kind models.AgentNodeKind, name string) tools.PreparedPage {
	agentId := self.settings.Agent.ID
	path, kind, name = pageIdentity(path, kind, name)
	if path == "" {
		return func(db.Transaction) (*models.AgentNode, error) { return nil, nil }
	}
	sense := self.agent.meaningOf(ctx, agentId, "remember", name)
	return func(tx db.Transaction) (*models.AgentNode, error) {
		return self.agent.resolvePage(tx, agentId, path, kind, name, sense)
	}
}

// NoteNode gives a page its vector.
func (self *AskRun) NoteNode(ctx context.Context, node *models.AgentNode) {
	if node == nil {
		return
	}
	agentId := self.settings.Agent.ID
	vectors, modelName, ok := self.agent.embed(ctx, agentId, "ask", []string{nodeText(node)})
	if !ok {
		return
	}
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.PutAgentNodeVector(agentId, node.ID, modelName, vectors[0])
	}); err != nil {
		log.Warningf("cannot keep a page's vector: %s", err)
	}
}
