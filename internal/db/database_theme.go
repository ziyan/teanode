package db

import (
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// ThemeOperation is what the night's theme and reflection phases read
// and write beyond an ordinary page.
type ThemeOperation interface {
	// ListAgentGraphShape is every live page, as little of each as
	// clustering needs, and every stated link between two live pages.
	// Proposed links are left out: a guess the nightly walk made is not
	// evidence that two pages belong together.
	ListAgentGraphShape(agentId string) ([]*AgentGraphShapePage, []*AgentGraphShapeLink, error)

	// ListAgentThemesToReflect is the live themes with an overview written
	// since the night last reflected on them, most important first.
	ListAgentThemesToReflect(agentId string, limit int) ([]*models.AgentNode, error)

	// AgentNodeReflectedAt is when the night last reflected on a page, or
	// nil if it never has. MarkAgentNodeReflected writes it.
	AgentNodeReflectedAt(agentId, nodeId string) (*time.Time, error)
	MarkAgentNodeReflected(agentId, nodeId string, at time.Time) error

	// ListAgentFactsOnNodesSince is the live facts on any of the given
	// pages filed since a time, newest first.
	ListAgentFactsOnNodesSince(agentId string, nodeIds []string, since time.Time, limit int) ([]*models.AgentFact, error)
}

// AgentGraphShapePage is a page as clustering reads it: where it is,
// what kind it is and how much it matters, and nothing it says.
type AgentGraphShapePage struct {
	ID         string               `gorm:"column:id"`
	Path       string               `gorm:"column:path"`
	Kind       models.AgentNodeKind `gorm:"column:kind"`
	Importance float32              `gorm:"column:importance"`
}

// AgentGraphShapeLink is a stated link as clustering reads it.
type AgentGraphShapeLink struct {
	FromID   string                   `gorm:"column:from_id"`
	ToID     string                   `gorm:"column:to_id"`
	Relation models.AgentEdgeRelation `gorm:"column:relation"`
}

func (self *transaction) ListAgentGraphShape(agentId string) ([]*AgentGraphShapePage, []*AgentGraphShapeLink, error) {
	var pages []*AgentGraphShapePage
	if err := self.tx.Raw(`SELECT "id", "path", "kind", "importance" FROM "agent_node"
		WHERE "agent_id" = ? AND NOT "dormant" ORDER BY "id"`, agentId).Scan(&pages).Error; err != nil {
		return nil, nil, err
	}
	var links []*AgentGraphShapeLink
	if err := self.tx.Raw(`SELECT e."from_id", e."to_id", e."relation" FROM "agent_edge" e
		JOIN "agent_node" origin ON origin."id" = e."from_id" AND NOT origin."dormant"
		JOIN "agent_node" target ON target."id" = e."to_id" AND NOT target."dormant"
		WHERE e."agent_id" = ? AND COALESCE(e."status", '') IN ('', ?)
		ORDER BY e."from_id", e."to_id", e."relation"`, agentId, string(models.EdgeStated)).Scan(&links).Error; err != nil {
		return nil, nil, err
	}
	return pages, links, nil
}

func (self *transaction) ListAgentThemesToReflect(agentId string, limit int) ([]*models.AgentNode, error) {
	if limit <= 0 {
		limit = 3
	}
	return self.nodesFrom(self.tx.Raw(`
		SELECT n.* FROM "agent_node" n
		WHERE n."agent_id" = ? AND NOT n."dormant" AND n."path" LIKE 'themes/%'
		  AND n."overview" <> '' AND n."overview_written_at" IS NOT NULL
		  AND (n."reflected_at" IS NULL OR n."overview_written_at" > n."reflected_at")
		ORDER BY n."importance" DESC, n."path" ASC
		LIMIT ?`, agentId, limit))
}

func (self *transaction) AgentNodeReflectedAt(agentId, nodeId string) (*time.Time, error) {
	var reflectedAt []*time.Time
	if err := self.tx.Raw(`SELECT "reflected_at" FROM "agent_node" WHERE "agent_id" = ? AND "id" = ?`,
		agentId, nodeId).Scan(&reflectedAt).Error; err != nil {
		return nil, err
	}
	if len(reflectedAt) == 0 {
		return nil, nil
	}
	return reflectedAt[0], nil
}

func (self *transaction) MarkAgentNodeReflected(agentId, nodeId string, at time.Time) error {
	update := self.tx.Exec(`UPDATE "agent_node" SET "reflected_at" = ? WHERE "id" = ? AND "agent_id" = ?`,
		at.Truncate(time.Microsecond), nodeId, agentId)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("db: no page %q to mark as reflected on", nodeId)
	}
	return nil
}

func (self *transaction) ListAgentFactsOnNodesSince(agentId string, nodeIds []string, since time.Time, limit int) ([]*models.AgentFact, error) {
	if len(nodeIds) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 60
	}
	return self.factsFrom(self.tx.Where(`"agent_id" = ? AND "node_id" IN ? AND NOT "dormant" AND "superseded_by" IS NULL AND "created_at" >= ?`,
		agentId, nodeIds, since).Order(`"created_at" DESC, "number" DESC`).Limit(limit))
}
