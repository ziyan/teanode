package agent

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// EmbedGraph gives vectors to pages and facts that have none, or whose
// vector an older model made, and says how many it wrote.
func (self *Agent) EmbedGraph(ctx context.Context, agent *models.Agent, limit int) (int, error) {
	_, _, modelName, _, ok := self.embedderFor()
	if !ok {
		return 0, nil
	}
	var nodes []*models.AgentNode
	var facts []*models.AgentFact
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if nodes, err = tx.ListAgentNodesWithoutVector(agent.ID, modelName, limit); err != nil {
			return err
		}
		facts, err = tx.ListAgentFactsWithoutVector(agent.ID, modelName, limit)
		return err
	}); err != nil {
		return 0, err
	}
	if len(nodes) == 0 && len(facts) == 0 {
		return 0, nil
	}

	pages := map[string]*models.AgentNode{}
	paths := map[string]string{}
	names := map[string]string{}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		ids := make([]string, 0, len(facts))
		for _, fact := range facts {
			ids = append(ids, fact.NodeID)
		}
		found, err := tx.GetAgentNodes(agent.ID, ids)
		if err != nil {
			return err
		}
		for _, node := range found {
			pages[node.ID] = node
			paths[node.ID] = node.Path
			names[node.ID] = node.Name
		}
		return nil
	}); err != nil {
		return 0, err
	}

	texts := make([]string, 0, len(nodes)+len(facts))
	for _, node := range nodes {
		texts = append(texts, nodeText(node))
	}
	for _, fact := range facts {
		texts = append(texts, factText(fact, paths[fact.NodeID], names[fact.NodeID]))
	}
	vectors, _, ok := self.embed(ctx, agent.ID, "embed", texts)
	if !ok {
		return 0, nil
	}
	writing := make([]db.AgentGraphVector, 0, len(nodes)+len(facts))
	for index, node := range nodes {
		if index >= len(vectors) || len(vectors[index]) == 0 {
			continue
		}
		writing = append(writing, db.AgentGraphVector{NodeID: node.ID, NodeModifiedAt: node.ModifiedAt, Model: modelName, Vector: vectors[index]})
	}
	for index, fact := range facts {
		position := len(nodes) + index
		page := pages[fact.NodeID]
		if page == nil || position >= len(vectors) || len(vectors[position]) == 0 {
			continue
		}
		writing = append(writing, db.AgentGraphVector{NodeID: page.ID, NodeModifiedAt: page.ModifiedAt, FactID: fact.ID, FactModifiedAt: fact.ModifiedAt, Model: modelName, Vector: vectors[position]})
	}
	written := 0
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		written, err = tx.PutAgentGraphVectors(agent.ID, writing)
		return err
	}); err != nil {
		return 0, err
	}
	return written, nil
}

// MigrateMemories moves an agent's flat memories onto the graph, once.
//
// Done here rather than in the migration because a page and a fact need
// identifiers this package makes, and because doing it lazily means the
// old table is untouched: a deployment that goes back a release finds its
// memories exactly as they were.
func (self *Agent) MigrateMemories(ctx context.Context, agent *models.Agent) error {
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			return err
		}
		memories, err := tx.ListAgentMemories(agent.ID, "", 1000)
		if err != nil || len(memories) == 0 {
			return err
		}
		notes, err := tx.GetAgentNode(agent.ID, models.PathNotes)
		if err != nil || notes == nil {
			return err
		}
		existing, err := tx.ListAgentFacts(agent.ID, notes.ID, true, 2000)
		if err != nil {
			return err
		}
		already := map[string]bool{}
		for _, fact := range existing {
			for _, evidence := range fact.Evidence {
				if evidence.Kind == models.EvidenceMemory {
					already[evidence.ID] = true
				}
			}
		}
		moved := 0
		for _, memory := range memories {
			if already[memory.ID] {
				continue
			}
			text := memory.Line()
			if len(memory.Tags) > 0 {
				text += " (" + strings.Join(memory.Tags, ", ") + ")"
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agent.ID, NodeID: notes.ID, Kind: models.FactPlain,
				Text: cutRunes(text, models.FactLength), Confidence: 1,
				Evidence:  []models.Evidence{{Kind: models.EvidenceMemory, ID: memory.ID, At: &memory.CreatedAt}},
				Audiences: memory.AppliesTo,
			}); err != nil {
				return err
			}
			moved++
		}
		if moved > 0 {
			log.Noticef("moved %d memories of agent %s onto the graph", moved, agent.ID)
		}
		return nil
	})
}

// EnsureVectorIndexes builds the vector index for every table and every
// model that has written a vector. Called at start, and again whenever a
// model writes its first vector, so that the index exists before a corpus
// arrives rather than having to be built over one.
func (self *Agent) EnsureVectorIndexes(ctx context.Context) error {
	database := self.settings.Database
	if !database.VectorIndexing() {
		return nil
	}
	var widths map[string]int
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		widths, err = tx.ListVectorModels()
		return err
	}); err != nil {
		return err
	}
	// The model this deployment is configured with, whether or not it has
	// written anything yet: building the index first is the whole point.
	if _, _, modelName, dimensions, ok := self.embedderFor(); ok && dimensions > 0 {
		if _, known := widths[modelName]; !known {
			if widths == nil {
				widths = map[string]int{}
			}
			widths[modelName] = dimensions
		}
	}
	tables := []db.VectorTable{db.AgentNodeTable, db.AgentFactTable, db.AgentChunkTable, db.MailEmbeddingTable}
	for model, dimension := range widths {
		for _, table := range tables {
			if err := database.EnsureVectorIndex(table, model, dimension); err != nil {
				// A table that does not exist yet is not a failure: the
				// chunk table arrives with knowledge sources.
				log.Debugf("no vector index for %s on %s: %s", model, table.Table, err)
			}
		}
	}
	return nil
}
