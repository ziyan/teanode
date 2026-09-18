package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/version"
)

// The history of a page.
//
// Every write to the graph leaves a row here saying what changed, what it
// said before, and who did it. That is what makes a page answerable: a
// sentence that looks wrong can be traced to the change that wrote it,
// and a page the nightly run rewrote can be read as it was.
//
// Recorded inside the same transaction as the change, so the two cannot
// disagree: a history that can be missing an entry is a history nobody
// can reason from.

// RevisionOperation is the history as the rest of the server reaches it.
type RevisionOperation interface {
	// RecordAgentRevision files one change against a page.
	RecordAgentRevision(revision *models.AgentRevision) (*models.AgentRevision, error)

	// ListAgentRevisions is a page's history, newest first.
	ListAgentRevisions(agentId, nodeId string, limit int) ([]*models.AgentRevision, error)

	// ListAgentRevisionsSince is everything that changed lately, across
	// the graph: what a person reads to see what their agent has been
	// doing.
	ListAgentRevisionsSince(agentId string, since time.Time, limit int) ([]*models.AgentRevision, error)
}

type agentRevisionModel struct {
	ID        string    `gorm:"column:id;primaryKey"`
	AgentID   string    `gorm:"column:agent_id"`
	NodeID    string    `gorm:"column:node_id"`
	Revision  int       `gorm:"column:revision"`
	Kind      string    `gorm:"column:kind"`
	Actor     string    `gorm:"column:actor"`
	Before    []byte    `gorm:"column:before;type:jsonb"`
	After     []byte    `gorm:"column:after;type:jsonb"`
	Reason    string    `gorm:"column:reason"`
	Version   string    `gorm:"column:version"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (agentRevisionModel) TableName() string { return "agent_revision" }

func (self *transaction) RecordAgentRevision(revision *models.AgentRevision) (*models.AgentRevision, error) {
	if revision.AgentID == "" || revision.NodeID == "" || revision.Kind == "" {
		return nil, fmt.Errorf("db: a revision needs an agent, a page and what changed")
	}
	before, err := json.Marshal(orEmptyMap(revision.Before))
	if err != nil {
		return nil, err
	}
	after, err := json.Marshal(orEmptyMap(revision.After))
	if err != nil {
		return nil, err
	}
	// Take the page's next number and move the mark on in one statement,
	// so that two changes at once cannot be given the same one.
	var numbers []int
	if err := self.tx.Raw(
		`UPDATE "agent_node" SET "next_revision" = "next_revision" + 1
		 WHERE "id" = ? AND "agent_id" = ? RETURNING "next_revision" - 1`,
		revision.NodeID, revision.AgentID).Scan(&numbers).Error; err != nil {
		return nil, err
	}
	if len(numbers) == 0 {
		// The page is gone, which happens when a change and a deletion
		// race. Losing the history entry is the right answer: there is
		// nothing left for it to be the history of.
		return nil, nil
	}
	created := *revision
	created.ID = newID()
	created.Revision = numbers[0]
	created.Version = version.Version()
	created.CreatedAt = time.Now().Truncate(time.Microsecond)
	row := &agentRevisionModel{
		ID: created.ID, AgentID: created.AgentID, NodeID: created.NodeID,
		Revision: created.Revision, Kind: string(created.Kind), Actor: string(created.Actor),
		Before: before, After: after, Reason: truncateRunes(created.Reason, 500),
		Version: created.Version, CreatedAt: created.CreatedAt,
	}
	if err := self.tx.Create(row).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) revisionsFrom(rows []agentRevisionModel) ([]*models.AgentRevision, error) {
	revisions := make([]*models.AgentRevision, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		revision := &models.AgentRevision{
			ID: row.ID, AgentID: row.AgentID, NodeID: row.NodeID, Revision: row.Revision,
			Kind: models.RevisionKind(row.Kind), Actor: models.RevisionActor(row.Actor),
			Reason: row.Reason, Version: row.Version, CreatedAt: row.CreatedAt,
			Before: map[string]any{}, After: map[string]any{},
		}
		if len(row.Before) > 0 {
			if err := json.Unmarshal(row.Before, &revision.Before); err != nil {
				return nil, err
			}
		}
		if len(row.After) > 0 {
			if err := json.Unmarshal(row.After, &revision.After); err != nil {
				return nil, err
			}
		}
		revisions = append(revisions, revision)
	}
	return revisions, nil
}

func (self *transaction) ListAgentRevisions(agentId, nodeId string, limit int) ([]*models.AgentRevision, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []agentRevisionModel
	if err := self.tx.Where(`"agent_id" = ? AND "node_id" = ?`, agentId, nodeId).
		Order(`"revision" DESC`).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.revisionsFrom(rows)
}

func (self *transaction) ListAgentRevisionsSince(agentId string, since time.Time, limit int) ([]*models.AgentRevision, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []agentRevisionModel
	if err := self.tx.Where(`"agent_id" = ? AND "created_at" >= ?`, agentId, since).
		Order(`"created_at" DESC`).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.revisionsFrom(rows)
}
