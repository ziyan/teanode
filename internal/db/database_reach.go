package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type agentReachModel struct {
	AgentID      string    `gorm:"column:agent_id;primaryKey"`
	Kind         string    `gorm:"column:kind;primaryKey"`
	Name         string    `gorm:"column:name;primaryKey"`
	ComputerName string    `gorm:"column:computer_name"`
	ModifiedAt   time.Time `gorm:"column:modified_at"`
}

func (agentReachModel) TableName() string { return "agent_reach" }

// ListAgentReaches is every reach this person set: each skill and connected
// server whose requests go through one of their computers. Anything not
// listed goes through this server.
func (self *transaction) ListAgentReaches(agentId string) ([]*models.AgentReach, error) {
	var found []agentReachModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"kind\" ASC, \"name\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	reaches := make([]*models.AgentReach, 0, len(found))
	for _, row := range found {
		reaches = append(reaches, &models.AgentReach{
			AgentID: row.AgentID, Kind: row.Kind, Name: row.Name,
			ComputerName: row.ComputerName, ModifiedAt: row.ModifiedAt,
		})
	}
	return reaches, nil
}

// noteAboutReach is what an audit row says: which service now goes through
// which computer, or back through this server when the computer is empty.
// Worth a row, because it decides which network a request leaves from.
type noteAboutReach struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Computer string `json:"computer,omitempty"`
}

// PutAgentReach sets the reach of one skill or server: the computer its
// requests go through. An empty computer name puts it back through this
// server, which is no row at all.
func (self *transaction) PutAgentReach(reach *models.AgentReach) error {
	if reach == nil || reach.AgentID == "" || strings.TrimSpace(reach.Name) == "" {
		return fmt.Errorf("db: a reach needs an agent and a name")
	}
	if reach.Kind != models.AgentReachSkill && reach.Kind != models.AgentReachServer {
		return fmt.Errorf("db: %q is not something reached through a computer", reach.Kind)
	}
	name := strings.TrimSpace(reach.Name)
	computerName := strings.TrimSpace(reach.ComputerName)
	note := &noteAboutReach{Kind: reach.Kind, Name: name, Computer: computerName}
	if computerName == "" {
		return self.applyMutation(models.AuditResourceAgent, reach.AgentID, models.AuditActionUpdate, nil, note,
			func(tx *gorm.DB) error {
				return tx.Where("\"agent_id\" = ? AND \"kind\" = ? AND \"name\" = ?", reach.AgentID, reach.Kind, name).
					Delete(&agentReachModel{}).Error
			})
	}
	row := &agentReachModel{AgentID: reach.AgentID, Kind: reach.Kind, Name: name, ComputerName: computerName, ModifiedAt: time.Now()}
	return self.applyMutation(models.AuditResourceAgent, reach.AgentID, models.AuditActionUpdate, nil, note,
		func(tx *gorm.DB) error {
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "agent_id"}, {Name: "kind"}, {Name: "name"}},
				DoUpdates: clause.AssignmentColumns([]string{"computer_name", "modified_at"}),
			}).Create(row).Error
		})
}
