package db

import (
	"time"

	"gorm.io/gorm/clause"
)

// AgentGraphVector carries the versions whose words were sent to the embedder.
// A fact also depends on its page's name and therefore its page version.
type AgentGraphVector struct {
	NodeID         string
	NodeModifiedAt time.Time
	FactID         string
	FactModifiedAt time.Time
	Model          string
	Vector         []float32
}

// PutAgentGraphVectors skips results whose input changed during the model call.
// Locks cover the version check and vector write, so a later edit either wins
// before the check or waits and invalidates the committed vector normally.
func (self *transaction) PutAgentGraphVectors(agentId string, vectors []AgentGraphVector) (int, error) {
	if len(vectors) == 0 {
		return 0, nil
	}
	nodeIds := make([]string, 0, len(vectors))
	factIds := make([]string, 0, len(vectors))
	for _, vector := range vectors {
		nodeIds = append(nodeIds, vector.NodeID)
		if vector.FactID != "" {
			factIds = append(factIds, vector.FactID)
		}
	}
	// Fact edits also lock the fact before recording a revision on its page.
	var facts []agentFactModel
	if len(factIds) > 0 {
		if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" IN ?`, agentId, factIds).Order(`"id"`).Find(&facts).Error; err != nil {
			return 0, err
		}
	}
	var nodes []agentNodeModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"agent_id" = ? AND "id" IN ?`, agentId, nodeIds).Order(`"id"`).Find(&nodes).Error; err != nil {
		return 0, err
	}
	nodeVersions := make(map[string]time.Time, len(nodes))
	for _, node := range nodes {
		nodeVersions[node.ID] = node.ModifiedAt
	}
	factVersions := make(map[string]agentFactModel, len(facts))
	for _, fact := range facts {
		factVersions[fact.ID] = fact
	}
	writtenCount := 0
	for _, vector := range vectors {
		modifiedAt, exists := nodeVersions[vector.NodeID]
		if !exists || !modifiedAt.Equal(vector.NodeModifiedAt) {
			continue
		}
		if vector.FactID == "" {
			if err := self.PutAgentNodeVector(agentId, vector.NodeID, vector.Model, vector.Vector); err != nil {
				return 0, err
			}
		} else {
			fact, exists := factVersions[vector.FactID]
			if !exists || fact.NodeID != vector.NodeID || !fact.ModifiedAt.Equal(vector.FactModifiedAt) {
				continue
			}
			if err := self.PutAgentFactVector(agentId, vector.FactID, vector.Model, vector.Vector); err != nil {
				return 0, err
			}
		}
		writtenCount++
	}
	return writtenCount, nil
}
