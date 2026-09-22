package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// noteFact gives a fact its vector as it is written and answers whatever
// on the same page is near enough to be the same thing said twice.
//
// Within one page, and with a second test beside the cosine: the two must
// share a proper noun or a number where either has one. Two short
// sentences about two different people sit above nine tenths of each
// other, and a merge on that evidence alone loses one of them.
func (self *Agent) noteFact(ctx context.Context, agentId string, fact *models.AgentFact) []*models.AgentFact {
	if fact == nil {
		return nil
	}
	var page *models.AgentNode
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNodeByID(agentId, fact.NodeID)
		if err != nil || node == nil {
			return err
		}
		page = node
		return nil
	}); err != nil {
		log.Warningf("cannot read the page a fact is on: %s", err)
		return nil
	}
	if page == nil {
		return nil
	}
	vectors, modelName, ok := self.embed(ctx, agentId, "ask", []string{factText(fact, page.Path, page.Name)})
	if !ok {
		return nil
	}
	var twins []*models.AgentFact
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		writtenCount, err := tx.PutAgentGraphVectors(agentId, []db.AgentGraphVector{{
			NodeID: page.ID, NodeModifiedAt: page.ModifiedAt, FactID: fact.ID,
			FactModifiedAt: fact.ModifiedAt, Model: modelName, Vector: vectors[0],
		}})
		if err != nil || writtenCount == 0 {
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
		itsOwn := append([]string{page.Name}, page.Aliases...)
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

// preparePage obtains a page-name embedding before the caller opens its write
// transaction. The returned resolver keeps alias matching and page creation
// in that transaction, using the prepared vector or the existing word fallback.
func (self *Agent) preparePage(ctx context.Context, agentId, path string, kind models.AgentNodeKind, name string) tools.PreparedPage {
	path, kind, name = pageIdentity(path, kind, name)
	if path == "" {
		return func(db.Transaction) (*models.AgentNode, error) { return nil, nil }
	}
	sense := self.meaningOf(ctx, agentId, "remember", name)
	return func(tx db.Transaction) (*models.AgentNode, error) {
		return self.resolvePage(tx, agentId, path, kind, name, sense)
	}
}

// noteNode gives a page its vector.
func (self *Agent) noteNode(ctx context.Context, agentId string, node *models.AgentNode) {
	if node == nil {
		return
	}
	vectors, modelName, ok := self.embed(ctx, agentId, "ask", []string{nodeText(node)})
	if !ok {
		return
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := tx.PutAgentGraphVectors(agentId, []db.AgentGraphVector{{
			NodeID: node.ID, NodeModifiedAt: node.ModifiedAt, Model: modelName, Vector: vectors[0],
		}})
		return err
	}); err != nil {
		log.Warningf("cannot keep a page's vector: %s", err)
	}
}

// The run's side of remembering (tools.Remembering), the same for a
// conversation and for a call from outside one: what a page or a fact is
// filed under is the agent's, not the run's.

func (self *AskRun) NoteFact(ctx context.Context, fact *models.AgentFact) []*models.AgentFact {
	return self.agent.noteFact(ctx, self.settings.Agent.ID, fact)
}

func (self *AskRun) PreparePage(ctx context.Context, path string, kind models.AgentNodeKind, name string) tools.PreparedPage {
	return self.agent.preparePage(ctx, self.settings.Agent.ID, path, kind, name)
}

func (self *AskRun) NoteNode(ctx context.Context, node *models.AgentNode) {
	self.agent.noteNode(ctx, self.settings.Agent.ID, node)
}
