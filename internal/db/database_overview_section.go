package db

import (
	"fmt"
	"time"

	"github.com/lib/pq"
)

// OverviewSectionOperation keeps the vectors of the sections of pages'
// overviews.
type OverviewSectionOperation interface {
	// ListAgentOverviewSectionVectorIds is every section id holding a
	// vector of the model, so the embedding pass can tell what is missing
	// and what is gone.
	ListAgentOverviewSectionVectorIds(agentId, model string) ([]string, error)

	// PutAgentOverviewSectionVector writes one section's vector.
	PutAgentOverviewSectionVector(agentId, nodeId, sectionId, model string, vector []float32) error

	// DeleteAgentOverviewSectionVectors removes the vectors of sections
	// that are no longer in any overview.
	DeleteAgentOverviewSectionVectors(agentId, model string, sectionIds []string) error
}

func (self *transaction) ListAgentOverviewSectionVectorIds(agentId, model string) ([]string, error) {
	var ids []string
	err := self.tx.Raw(`SELECT "section_id" FROM "agent_overview_section_vector" WHERE "agent_id" = ? AND "model" = ?`,
		agentId, model).Scan(&ids).Error
	return ids, err
}

func (self *transaction) PutAgentOverviewSectionVector(agentId, nodeId, sectionId, model string, vector []float32) error {
	if agentId == "" || nodeId == "" || sectionId == "" || model == "" || len(vector) == 0 {
		return fmt.Errorf("db: a section vector needs the agent, the page, the section, the model and the vector")
	}
	if err := self.NoteVectorModel(model, len(vector)); err != nil {
		return err
	}
	return self.tx.Exec(
		`INSERT INTO "agent_overview_section_vector" ("section_id", "node_id", "agent_id", "model", "vector", "created_at")
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT ("section_id", "model") DO UPDATE SET "vector" = EXCLUDED."vector", "created_at" = EXCLUDED."created_at"`,
		sectionId, nodeId, agentId, model, pq.Float32Array(vector), time.Now()).Error
}

func (self *transaction) DeleteAgentOverviewSectionVectors(agentId, model string, sectionIds []string) error {
	if len(sectionIds) == 0 {
		return nil
	}
	return self.tx.Exec(`DELETE FROM "agent_overview_section_vector" WHERE "agent_id" = ? AND "model" = ? AND "section_id" = ANY(?)`,
		agentId, model, pq.Array(sectionIds)).Error
}
