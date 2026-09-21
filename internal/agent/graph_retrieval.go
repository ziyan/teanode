package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// errNotAskedByMeaning is a search by meaning that did not happen: no
// embedding model is configured, or the one there is did not answer.
//
// It matters that this is not an empty result. Recall can treat the two
// the same -- a turn with nothing to add carries nothing either way --
// but rehearsal cannot, because there "the graph was asked and had
// nothing" is a gap it writes down and "the graph was never asked" is
// not. Reported as an error so that a caller has to decide which it is.
var errNotAskedByMeaning = errors.New("the graph could not be asked by meaning")

// nearestInGraph is the pages and facts nearest in meaning to some words,
// or why it could not look.
func (self *Agent) nearestInGraph(ctx context.Context, agent *models.Agent, words string, limit int) ([]*models.AgentNode, []*models.AgentFact, error) {
	return self.nearestInGraphTo(ctx, agent.ID,
		self.meaningOf(ctx, agent.ID, "recall", cutRunes(words, graphEmbedCharacters)), limit)
}

// nearestInGraphTo is nearestInGraph once the words are already a vector,
// for a turn that embedded its question once and puts the one answer to
// both stores. A nil question is a search that did not happen.
func (self *Agent) nearestInGraphTo(ctx context.Context, agentId string, question *meaning, limit int) ([]*models.AgentNode, []*models.AgentFact, error) {
	if question == nil {
		return nil, nil, errNotAskedByMeaning
	}
	modelName, query := question.ModelName, question.Vector
	var nodes []*models.AgentNode
	var facts []*models.AgentFact
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		nodeScores, err := tx.Nearest(db.AgentNodeTable, agentId, modelName, query, limit, db.VectorQuery{Floor: meaningFloorGraph})
		if err != nil {
			return err
		}
		// Only what the pages still say.
		//
		// A struck line and a line standing behind a newer one keep their
		// vectors, and they were as eligible for the nearest few as
		// anything else -- so a retired statement could take one of the
		// five slots a question gets, and the fact that would have
		// answered it never came back. Recall then dropped the retired one
		// and reported the slot as nothing, which is a gap made by the
		// search rather than by the graph.
		//
		// Narrowed in the query rather than after it for that reason: a
		// filter applied to the answer shrinks the answer, and a filter
		// applied to the search changes what is in it.
		factScores, err := tx.Nearest(db.AgentFactTable, agentId, modelName, query, limit, db.VectorQuery{
			Floor: meaningFloorGraph,
			Where: []string{`EXISTS (SELECT 1 FROM "agent_fact" WHERE "agent_fact"."id" = "agent_fact_vector"."fact_id"` +
				` AND NOT "agent_fact"."dormant" AND COALESCE("agent_fact"."superseded_by", '') = '')`},
		})
		if err != nil {
			return err
		}
		if nodes, err = tx.GetAgentNodes(agentId, idsOf(nodeScores)); err != nil {
			return err
		}
		facts, err = tx.GetAgentFacts(agentId, idsOf(factScores))
		if err != nil {
			return err
		}
		// The reads come back in whatever order the table gave them; the
		// scores are the ranking.
		nodes = orderNodes(nodes, idsOf(nodeScores))
		facts = orderFacts(facts, idsOf(factScores))
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("ranking the graph by meaning: %w", err)
	}
	return nodes, facts, nil
}

// searchChunksByMeaning is the vector search itself, given a question
// already embedded, so that a turn (which embeds once and keeps it) and a
// search from outside one (which does not) run the same query.
func (self *Agent) searchChunksByMeaning(ctx context.Context, agentId string, question *meaning, sourceIds []string, limit int) ([]*models.AgentChunk, bool) {
	if question == nil || !self.settings.Database.VectorIndexing() {
		return nil, false
	}
	narrow := db.VectorQuery{Floor: meaningFloorGraph}
	if len(sourceIds) > 0 {
		narrow.Where = append(narrow.Where, `"source_id" = ANY(?)`)
		narrow.Arguments = append(narrow.Arguments, pq.Array(sourceIds))
	}
	var chunks []*models.AgentChunk
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentChunkTable, agentId, question.ModelName, question.Vector, limit, narrow)
		if err != nil {
			return err
		}
		found, err := tx.GetAgentChunks(agentId, idsOf(scores))
		if err != nil {
			return err
		}
		chunks = orderChunks(found, idsOf(scores))
		return nil
	}); err != nil {
		log.Warningf("cannot rank what agent %s indexed by meaning: %s", agentId, err)
		return nil, false
	}
	return chunks, true
}

// rankChunksByMeaning puts what the words found into the order an
// embedded question wants.
func (self *Agent) rankChunksByMeaning(ctx context.Context, agentId string, question *meaning, chunks []*models.AgentChunk, limit int) []*models.AgentChunk {
	if question == nil {
		return chunks
	}
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
	}
	var ordered []*models.AgentChunk
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentChunkTable, agentId, question.ModelName, question.Vector, limit, db.VectorQuery{
			Where:     []string{`"chunk_id" = ANY(?)`},
			Arguments: []any{pq.Array(ids)},
			Floor:     meaningFloorGraph,
		})
		if err != nil {
			return err
		}
		ordered = orderChunks(chunks, idsOf(scores))
		return nil
	}); err != nil {
		log.Debugf("cannot re-rank by meaning: %s", err)
		return chunks
	}
	// Anything the meaning had nothing to say about keeps its place at
	// the end rather than being dropped: the words did find it.
	seen := map[string]bool{}
	for _, chunk := range ordered {
		seen[chunk.ID] = true
	}
	for _, chunk := range chunks {
		if len(ordered) >= limit {
			break
		}
		if !seen[chunk.ID] {
			ordered = append(ordered, chunk)
		}
	}
	return ordered
}
