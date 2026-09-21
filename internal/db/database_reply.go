package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ReplyOperation is the replies the agent writes on a person's behalf.
type ReplyOperation interface {
	CreateAgentReply(reply *models.AgentReply) (*models.AgentReply, error)
	GetAgentReply(replyId string) (*models.AgentReply, error)
	UpdateAgentReply(replyId string, modify func(*models.AgentReply) error) (*models.AgentReply, error)
	ListAgentReplies(filter *AgentReplyFilter, options *Options) ([]*models.AgentReply, error)
	CountAgentReplies(filter *AgentReplyFilter) (int64, error)

	// ScavengeAgentReplies removes replies that are over — sent, cancelled,
	// refused or failed — before the given time.
	ScavengeAgentReplies(before time.Time) (int64, error)
}

// AgentReplyFilter narrows a listing; every field set must hold.
type AgentReplyFilter struct {
	AgentID     string
	MailboxID   string
	MailID      string
	ThreadID    string
	DraftItemID string
	Statuses    []models.AgentReplyStatus

	// Since keeps replies created at or after it; SentSince the ones sent
	// at or after it.
	Since     time.Time
	SentSince time.Time
}

type agentReplyModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	ModifiedAt  time.Time  `gorm:"column:modified_at"`
	AgentID     string     `gorm:"column:agent_id"`
	MailboxID   string     `gorm:"column:mailbox_id"`
	MailID      string     `gorm:"column:mail_id"`
	ThreadID    string     `gorm:"column:thread_id"`
	DraftItemID string     `gorm:"column:draft_item_id"`
	RunID       string     `gorm:"column:run_id"`
	Status      string     `gorm:"column:status"`
	Reason      string     `gorm:"column:reason"`
	Subject     string     `gorm:"column:subject"`
	FromAddress string     `gorm:"column:from_address"`
	ToAddress   string     `gorm:"column:to_address"`
	Text        string     `gorm:"column:text"`
	SendAfter   *time.Time `gorm:"column:send_after"`
	SentMailID  string     `gorm:"column:sent_mail_id"`
	SentAt      *time.Time `gorm:"column:sent_at"`
}

func (agentReplyModel) TableName() string { return "agent_reply" }

func agentReplyToModel(reply *models.AgentReply) *agentReplyModel {
	return &agentReplyModel{
		ID:          reply.ID,
		CreatedAt:   reply.CreatedAt,
		ModifiedAt:  reply.ModifiedAt,
		AgentID:     reply.AgentID,
		MailboxID:   reply.MailboxID,
		MailID:      reply.MailID,
		ThreadID:    reply.ThreadID,
		DraftItemID: reply.DraftItemID,
		RunID:       reply.RunID,
		Status:      string(reply.Status),
		Reason:      reply.Reason,
		Subject:     reply.Subject,
		FromAddress: reply.From,
		ToAddress:   reply.To,
		Text:        reply.Text,
		SendAfter:   reply.SendAfter,
		SentMailID:  reply.SentMailID,
		SentAt:      reply.SentAt,
	}
}

func (self *agentReplyModel) toModel() *models.AgentReply {
	return &models.AgentReply{
		ID:          self.ID,
		CreatedAt:   self.CreatedAt,
		ModifiedAt:  self.ModifiedAt,
		AgentID:     self.AgentID,
		MailboxID:   self.MailboxID,
		MailID:      self.MailID,
		ThreadID:    self.ThreadID,
		DraftItemID: self.DraftItemID,
		RunID:       self.RunID,
		Status:      models.AgentReplyStatus(self.Status),
		Reason:      self.Reason,
		Subject:     self.Subject,
		From:        self.FromAddress,
		To:          self.ToAddress,
		Text:        self.Text,
		SendAfter:   self.SendAfter,
		SentMailID:  self.SentMailID,
		SentAt:      self.SentAt,
	}
}

func (self *transaction) CreateAgentReply(reply *models.AgentReply) (*models.AgentReply, error) {
	if reply.AgentID == "" || reply.MailboxID == "" || reply.MailID == "" || reply.Status == "" {
		return nil, fmt.Errorf("db: a reply needs an agent, a mailbox, a message and a status")
	}
	created := *reply
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	if err := self.applyMutation(models.AuditResourceAgent, created.AgentID, models.AuditActionUpdate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(agentReplyToModel(&created)).Error
	}); err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) GetAgentReply(replyId string) (*models.AgentReply, error) {
	var found []agentReplyModel
	if err := self.tx.Where("\"id\" = ?", replyId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) UpdateAgentReply(replyId string, modify func(*models.AgentReply) error) (*models.AgentReply, error) {
	var found []agentReplyModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", replyId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	before := found[0].toModel()
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	after.ID = before.ID
	after.CreatedAt = before.CreatedAt
	after.ModifiedAt = time.Now()
	if err := self.applyMutation(models.AuditResourceAgent, after.AgentID, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Save(agentReplyToModel(&after)).Error
	}); err != nil {
		return nil, err
	}
	return &after, nil
}

func (self *transaction) agentReplyQuery(filter *AgentReplyFilter) *gorm.DB {
	query := self.tx.Model(&agentReplyModel{})
	if filter == nil {
		return query
	}
	if filter.AgentID != "" {
		query = query.Where("\"agent_id\" = ?", filter.AgentID)
	}
	if filter.MailboxID != "" {
		query = query.Where("\"mailbox_id\" = ?", filter.MailboxID)
	}
	if filter.MailID != "" {
		query = query.Where("\"mail_id\" = ?", filter.MailID)
	}
	if filter.ThreadID != "" {
		query = query.Where("\"thread_id\" = ?", filter.ThreadID)
	}
	if filter.DraftItemID != "" {
		query = query.Where("\"draft_item_id\" = ?", filter.DraftItemID)
	}
	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			statuses = append(statuses, string(status))
		}
		query = query.Where("\"status\" IN ?", statuses)
	}
	if !filter.Since.IsZero() {
		query = query.Where("\"created_at\" >= ?", filter.Since)
	}
	if !filter.SentSince.IsZero() {
		query = query.Where("\"sent_at\" >= ?", filter.SentSince)
	}
	return query
}

func (self *transaction) ListAgentReplies(filter *AgentReplyFilter, options *Options) ([]*models.AgentReply, error) {
	var found []agentReplyModel
	query := self.agentReplyQuery(filter).Order("\"created_at\" DESC, \"id\" DESC")
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	replies := make([]*models.AgentReply, 0, len(found))
	for index := range found {
		replies = append(replies, found[index].toModel())
	}
	return replies, nil
}

func (self *transaction) CountAgentReplies(filter *AgentReplyFilter) (int64, error) {
	var count int64
	err := self.agentReplyQuery(filter).Count(&count).Error
	return count, err
}

func (self *transaction) ScavengeAgentReplies(before time.Time) (int64, error) {
	over := []string{string(models.AgentReplySent), string(models.AgentReplyCancelled), string(models.AgentReplyRefused), string(models.AgentReplyFailed)}
	result := self.tx.Where("\"status\" IN ? AND \"modified_at\" < ?", over, before).Delete(&agentReplyModel{})
	return result.RowsAffected, result.Error
}
