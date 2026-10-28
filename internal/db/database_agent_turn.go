package db

import (
	"errors"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// AgentTurnOperation is the turns running on some instance (agent_turn).
type AgentTurnOperation interface {
	// PutAgentTurn records a turn as running, or records it again under
	// whichever instance now runs it.
	PutAgentTurn(turn *models.AgentTurn) error

	// DeleteAgentTurn forgets a turn that ended.
	DeleteAgentTurn(runId string) error

	// GetAgentTurn is a recorded turn, nil when there is none.
	GetAgentTurn(runId string) (*models.AgentTurn, error)

	// HeartbeatAgentTurns says an instance is running exactly these turns
	// as of now: they are written as running, and the instance's other turns
	// that were not cut and were last heard of before now are forgotten.
	HeartbeatAgentTurns(instance string, running []*models.AgentTurn, now time.Time) error

	// CutAgentTurns marks the turns an instance is stopping with as cut, so
	// another instance may carry them on at once.
	CutAgentTurns(instance string, now time.Time) (int64, error)

	// ListLiveAgentTurns is the turns of a conversation that some instance
	// is running: not cut, and heard of since freshAfter.
	ListLiveAgentTurns(conversationId string, freshAfter time.Time) ([]*models.AgentTurn, error)

	// ClaimCutAgentTurns takes, and forgets, the turns that were cut or whose
	// instance has not been heard of since staleBefore, for the caller to
	// carry on. Each is taken by one caller only.
	ClaimCutAgentTurns(staleBefore time.Time, limit int) ([]*models.AgentTurn, error)
}

type agentTurnModel struct {
	RunID          string     `gorm:"column:run_id;primaryKey"`
	ConversationID string     `gorm:"column:conversation_id"`
	Instance       string     `gorm:"column:instance"`
	StartedAt      time.Time  `gorm:"column:started_at"`
	HeartbeatAt    time.Time  `gorm:"column:heartbeat_at"`
	CutAt          *time.Time `gorm:"column:cut_at"`
}

func (agentTurnModel) TableName() string { return "agent_turn" }

func (self *agentTurnModel) toModel() *models.AgentTurn {
	return &models.AgentTurn{
		RunID: self.RunID, ConversationID: self.ConversationID, Instance: self.Instance,
		StartedAt: self.StartedAt, HeartbeatAt: self.HeartbeatAt, CutAt: self.CutAt,
	}
}

func (self *transaction) PutAgentTurn(turn *models.AgentTurn) error {
	if turn == nil || turn.RunID == "" || turn.ConversationID == "" || turn.Instance == "" {
		return ErrInvalidArguments
	}
	row := &agentTurnModel{
		RunID: turn.RunID, ConversationID: turn.ConversationID, Instance: turn.Instance,
		StartedAt: turn.StartedAt, HeartbeatAt: turn.HeartbeatAt, CutAt: turn.CutAt,
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"instance", "started_at", "heartbeat_at", "cut_at"}),
	}).Create(row).Error
}

func (self *transaction) DeleteAgentTurn(runId string) error {
	return self.tx.Where(`"run_id" = ?`, runId).Delete(&agentTurnModel{}).Error
}

func (self *transaction) GetAgentTurn(runId string) (*models.AgentTurn, error) {
	var row agentTurnModel
	result := self.tx.Where(`"run_id" = ?`, runId).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return row.toModel(), nil
}

func (self *transaction) HeartbeatAgentTurns(instance string, running []*models.AgentTurn, now time.Time) error {
	runIds := make([]string, 0, len(running))
	for _, turn := range running {
		runIds = append(runIds, turn.RunID)
		if err := self.PutAgentTurn(&models.AgentTurn{
			RunID: turn.RunID, ConversationID: turn.ConversationID, Instance: instance,
			StartedAt: turn.StartedAt, HeartbeatAt: now,
		}); err != nil {
			return err
		}
	}
	forgotten := self.tx.Where(`"instance" = ? AND "cut_at" IS NULL AND "heartbeat_at" < ?`, instance, now)
	if len(runIds) > 0 {
		forgotten = forgotten.Where(`"run_id" NOT IN ?`, runIds)
	}
	return forgotten.Delete(&agentTurnModel{}).Error
}

func (self *transaction) CutAgentTurns(instance string, now time.Time) (int64, error) {
	result := self.tx.Model(&agentTurnModel{}).Where(`"instance" = ? AND "cut_at" IS NULL`, instance).
		Update("cut_at", now)
	return result.RowsAffected, result.Error
}

func (self *transaction) ListLiveAgentTurns(conversationId string, freshAfter time.Time) ([]*models.AgentTurn, error) {
	var rows []agentTurnModel
	if err := self.tx.Where(`"conversation_id" = ? AND "cut_at" IS NULL AND "heartbeat_at" >= ?`, conversationId, freshAfter).
		Order(`"started_at", "run_id"`).Find(&rows).Error; err != nil {
		return nil, err
	}
	turns := make([]*models.AgentTurn, 0, len(rows))
	for index := range rows {
		turns = append(turns, rows[index].toModel())
	}
	return turns, nil
}

func (self *transaction) ClaimCutAgentTurns(staleBefore time.Time, limit int) ([]*models.AgentTurn, error) {
	if limit <= 0 {
		return nil, errors.New("db: claim at least one turn")
	}
	var rows []agentTurnModel
	if err := self.tx.Raw(`DELETE FROM "agent_turn" WHERE "run_id" IN (
			SELECT "run_id" FROM "agent_turn"
			WHERE "cut_at" IS NOT NULL OR "heartbeat_at" < ?
			ORDER BY "started_at" LIMIT ? FOR UPDATE SKIP LOCKED)
		RETURNING *`, staleBefore, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	turns := make([]*models.AgentTurn, 0, len(rows))
	for index := range rows {
		turns = append(turns, rows[index].toModel())
	}
	return turns, nil
}
