package db

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ConnectionOperation is a person's connections to the servers the
// operator declared.
type ConnectionOperation interface {
	// PutAgentConnection writes or replaces the person's connection to a
	// server.
	PutAgentConnection(connection *models.AgentConnection) (*models.AgentConnection, error)
	GetAgentConnection(agentId, serverName string) (*models.AgentConnection, error)
	ListAgentConnections(agentId string) ([]*models.AgentConnection, error)
	DeleteAgentConnection(agentId, serverName string) error
	DeleteAgentConnections(agentId string) (int64, error)
}

type agentConnectionModel struct {
	ID              string     `gorm:"column:id;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	ModifiedAt      time.Time  `gorm:"column:modified_at"`
	AgentID         string     `gorm:"column:agent_id"`
	ServerName      string     `gorm:"column:server_name"`
	Status          string     `gorm:"column:status"`
	Credential      string     `gorm:"column:credential"`
	Tokens          string     `gorm:"column:tokens"`
	Pending         string     `gorm:"column:pending"`
	LastError       string     `gorm:"column:last_error"`
	LastConnectedAt *time.Time `gorm:"column:last_connected_at"`
}

func (agentConnectionModel) TableName() string { return "agent_mcp_connection" }

func (self *agentConnectionModel) toModel() *models.AgentConnection {
	return &models.AgentConnection{ID: self.ID, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, AgentID: self.AgentID, ServerName: self.ServerName, Status: models.AgentConnectionStatus(self.Status), Credential: self.Credential, Tokens: self.Tokens, Pending: self.Pending, LastError: self.LastError, LastConnectedAt: self.LastConnectedAt}
}

func (self *transaction) PutAgentConnection(connection *models.AgentConnection) (*models.AgentConnection, error) {
	if connection.AgentID == "" || connection.ServerName == "" || connection.Status == "" {
		return nil, fmt.Errorf("db: a connection needs an agent, a server and a status")
	}
	existing, err := self.GetAgentConnection(connection.AgentID, connection.ServerName)
	if err != nil {
		return nil, err
	}
	stored := *connection
	stored.ModifiedAt = time.Now()
	if existing != nil {
		stored.ID = existing.ID
		stored.CreatedAt = existing.CreatedAt
	} else {
		stored.ID = newID()
		stored.CreatedAt = stored.ModifiedAt
	}
	model := &agentConnectionModel{ID: stored.ID, CreatedAt: stored.CreatedAt, ModifiedAt: stored.ModifiedAt, AgentID: stored.AgentID, ServerName: stored.ServerName, Status: string(stored.Status), Credential: stored.Credential, Tokens: stored.Tokens, Pending: stored.Pending, LastError: stored.LastError, LastConnectedAt: stored.LastConnectedAt}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "server_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"modified_at", "status", "credential", "tokens", "pending", "last_error", "last_connected_at"}),
	}).Create(model).Error; err != nil {
		return nil, err
	}
	return &stored, nil
}

func (self *transaction) GetAgentConnection(agentId, serverName string) (*models.AgentConnection, error) {
	var found []agentConnectionModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"server_name\" = ?", agentId, serverName).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListAgentConnections(agentId string) ([]*models.AgentConnection, error) {
	var found []agentConnectionModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"server_name\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	connections := make([]*models.AgentConnection, 0, len(found))
	for index := range found {
		connections = append(connections, found[index].toModel())
	}
	return connections, nil
}

func (self *transaction) DeleteAgentConnection(agentId, serverName string) error {
	return self.tx.Where("\"agent_id\" = ? AND \"server_name\" = ?", agentId, serverName).Delete(&agentConnectionModel{}).Error
}

func (self *transaction) DeleteAgentConnections(agentId string) (int64, error) {
	result := self.tx.Where("\"agent_id\" = ?", agentId).Delete(&agentConnectionModel{})
	return result.RowsAffected, result.Error
}
