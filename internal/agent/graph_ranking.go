package agent

import (
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// fuseChunks ranks what two searches found by reciprocal rank.
func fuseChunks(limit int, lists ...[]*models.AgentChunk) []*models.AgentChunk {
	scores := map[string]float64{}
	byId := map[string]*models.AgentChunk{}
	var order []string
	for _, list := range lists {
		for position, chunk := range list {
			if _, seen := byId[chunk.ID]; !seen {
				order = append(order, chunk.ID)
			}
			scores[chunk.ID] += 1 / float64(reciprocalRankConstant+position+1)
			byId[chunk.ID] = chunk
		}
	}
	sort.SliceStable(order, func(left, right int) bool { return scores[order[left]] > scores[order[right]] })
	ranked := make([]*models.AgentChunk, 0, limit)
	for _, id := range order {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, byId[id])
	}
	return ranked
}

// fuseNodes and fuseFacts rank what several searches found by reciprocal
// rank: a row's score is the sum of 1/(k+position) over every list it
// appears in. A row that two searches both found beats one that only the
// best search found in first place, which is the property wanted: the
// words and the meaning agreeing is the strongest evidence there is.
func fuseNodes(limit int, lists ...[]*models.AgentNode) []*models.AgentNode {
	now := time.Now()
	scores := map[string]float64{}
	byId := map[string]*models.AgentNode{}
	for _, list := range lists {
		for position, node := range list {
			scores[node.ID] += 1 / float64(reciprocalRankConstant+position+1)
			byId[node.ID] = node
		}
	}
	for id, node := range byId {
		scores[id] *= ageWeight(decayOfNode(node, now))
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(left, right int) bool {
		if scores[ids[left]] != scores[ids[right]] {
			return scores[ids[left]] > scores[ids[right]]
		}
		return ids[left] < ids[right]
	})
	ranked := make([]*models.AgentNode, 0, limit)
	for _, id := range ids {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, byId[id])
	}
	return ranked
}

// ageFloor is the least an old fact or page keeps of its fused score.
//
// Age breaks ties; it does not decide. Multiplied in whole, a status fact
// with a ninety-day half-life fell to a seventh of its score, so a
// perfect match from years ago ranked under the twentieth match from this
// month, and most of what a person asks about their own life -- where
// they grew up, when a child was born, the job before this one -- is old.
// A statement that is no longer true is superseded and left out anyway;
// what age should settle is which of two equally good matches comes
// first.
//
// Fused scores are close together: one place down the list is about a
// sixtieth less. A floor of 0.7 still let age move an old match down
// twenty places; at 0.95 it moves it three or four.
const ageFloor = 0.95

// ageWeight is what age costs a fused score. Age alone: that a fact was
// inferred rather than told is applied whole, beside it.
func ageWeight(decay float64) float64 {
	return ageFloor + (1-ageFloor)*decay
}

// fuseFacts ranks by reciprocal rank and then by age.
//
// Age adjusts the fused score without removing older facts that may still
// answer the question. Equally relevant recent statements rank ahead of them.
func fuseFacts(limit int, lists ...[]*models.AgentFact) []*models.AgentFact {
	now := time.Now()
	scores := map[string]float64{}
	byId := map[string]*models.AgentFact{}
	for _, list := range lists {
		for position, fact := range list {
			scores[fact.ID] += 1 / float64(reciprocalRankConstant+position+1)
			byId[fact.ID] = fact
		}
	}
	for id, fact := range byId {
		scores[id] *= ageWeight(ageOfFact(fact, now)) * inferredWeight(fact)
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(left, right int) bool {
		if scores[ids[left]] != scores[ids[right]] {
			return scores[ids[left]] > scores[ids[right]]
		}
		return ids[left] < ids[right]
	})
	ranked := make([]*models.AgentFact, 0, limit)
	for _, id := range ids {
		if len(ranked) >= limit {
			break
		}
		ranked = append(ranked, byId[id])
	}
	return ranked
}

func idsOf(scores []db.Scored) []string {
	ids := make([]string, 0, len(scores))
	for _, score := range scores {
		ids = append(ids, score.ID)
	}
	return ids
}

func orderNodes(nodes []*models.AgentNode, ids []string) []*models.AgentNode {
	byId := make(map[string]*models.AgentNode, len(nodes))
	for _, node := range nodes {
		byId[node.ID] = node
	}
	ordered := make([]*models.AgentNode, 0, len(ids))
	for _, id := range ids {
		if node := byId[id]; node != nil {
			ordered = append(ordered, node)
		}
	}
	return ordered
}

func orderFacts(facts []*models.AgentFact, ids []string) []*models.AgentFact {
	byId := make(map[string]*models.AgentFact, len(facts))
	for _, fact := range facts {
		byId[fact.ID] = fact
	}
	ordered := make([]*models.AgentFact, 0, len(ids))
	for _, id := range ids {
		if fact := byId[id]; fact != nil {
			ordered = append(ordered, fact)
		}
	}
	return ordered
}

func orderChunks(chunks []*models.AgentChunk, ids []string) []*models.AgentChunk {
	byId := make(map[string]*models.AgentChunk, len(chunks))
	for _, chunk := range chunks {
		byId[chunk.ID] = chunk
	}
	ordered := make([]*models.AgentChunk, 0, len(ids))
	for _, id := range ids {
		if chunk := byId[id]; chunk != nil {
			ordered = append(ordered, chunk)
		}
	}
	return ordered
}

// byNumber puts facts back in the order they are numbered, for reading,
// after a query chose which of them to show.
func byNumber(facts []*models.AgentFact) {
	sort.Slice(facts, func(left, right int) bool { return facts[left].Number < facts[right].Number })
}
