package db

import (
	"time"

	"gorm.io/gorm/clause"
)

// AgentComputerAttachmentOperation is which instance holds the websocket of
// a person's computer (agent_computer_attachment). Only the instance holding
// the socket can read the computer, so the others look here to see that a job
// for it is that instance's to run.
type AgentComputerAttachmentOperation interface {
	// PutAgentComputerAttachment says an instance holds the computer, or says
	// so again under whichever instance now does.
	PutAgentComputerAttachment(agentId, computerName, instance string, now time.Time) error

	// DeleteAgentComputerAttachment forgets that an instance holds the
	// computer. A row another instance has since written is left alone.
	DeleteAgentComputerAttachment(agentId, computerName, instance string) error

	// DeleteAgentComputerAttachmentsOfInstance forgets every computer an
	// instance said it held, for the instance starting: it holds none yet.
	DeleteAgentComputerAttachmentsOfInstance(instance string) error

	// HeartbeatAgentComputerAttachments says an instance is still holding the
	// computers it has not let go of.
	HeartbeatAgentComputerAttachments(instance string, now time.Time) error

	// FindAgentComputerAttachment is the name of the instance that holds the
	// computer and has been heard of since freshAfter, or empty when none
	// does. An empty computerName is any computer of the agent, the one
	// heard of most recently. A computer name is matched without regard to
	// case, as the person's choice of computer for a skill is.
	FindAgentComputerAttachment(agentId, computerName string, freshAfter time.Time) (string, error)
}

type agentComputerAttachmentModel struct {
	AgentID      string    `gorm:"column:agent_id;primaryKey"`
	ComputerName string    `gorm:"column:computer_name;primaryKey"`
	Instance     string    `gorm:"column:instance"`
	HeartbeatAt  time.Time `gorm:"column:heartbeat_at"`
}

func (agentComputerAttachmentModel) TableName() string { return "agent_computer_attachment" }

func (self *transaction) PutAgentComputerAttachment(agentId, computerName, instance string, now time.Time) error {
	if agentId == "" || instance == "" {
		return ErrInvalidArguments
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "computer_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"instance", "heartbeat_at"}),
	}).Create(&agentComputerAttachmentModel{AgentID: agentId, ComputerName: computerName, Instance: instance, HeartbeatAt: now}).Error
}

func (self *transaction) DeleteAgentComputerAttachment(agentId, computerName, instance string) error {
	return self.tx.Where(`"agent_id" = ? AND "computer_name" = ? AND "instance" = ?`, agentId, computerName, instance).
		Delete(&agentComputerAttachmentModel{}).Error
}

func (self *transaction) DeleteAgentComputerAttachmentsOfInstance(instance string) error {
	return self.tx.Where(`"instance" = ?`, instance).Delete(&agentComputerAttachmentModel{}).Error
}

func (self *transaction) HeartbeatAgentComputerAttachments(instance string, now time.Time) error {
	return self.tx.Model(&agentComputerAttachmentModel{}).Where(`"instance" = ?`, instance).
		Update("heartbeat_at", now).Error
}

func (self *transaction) FindAgentComputerAttachment(agentId, computerName string, freshAfter time.Time) (string, error) {
	query := self.tx.Model(&agentComputerAttachmentModel{}).
		Where(`"agent_id" = ? AND "heartbeat_at" >= ?`, agentId, freshAfter)
	if computerName != "" {
		query = query.Where(`LOWER("computer_name") = LOWER(?)`, computerName)
	}
	var instances []string
	if err := query.Order(`"heartbeat_at" DESC`).Limit(1).Pluck("instance", &instances).Error; err != nil {
		return "", err
	}
	if len(instances) == 0 {
		return "", nil
	}
	return instances[0], nil
}
