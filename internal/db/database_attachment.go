package db

import (
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// AttachmentOperation is the files a person hands their agent in a
// conversation: the rows here, the bytes in the spool under the same id.
type AttachmentOperation interface {
	CreateAgentAttachment(attachment *models.AgentAttachment) (*models.AgentAttachment, error)
	GetAgentAttachment(attachmentId string) (*models.AgentAttachment, error)

	// GetAgentAttachments reads several by id, in the order given; one
	// that does not exist is left out.
	GetAgentAttachments(attachmentIds []string) ([]*models.AgentAttachment, error)

	// ClaimAgentAttachments binds uploaded files to the message they came
	// with, so that they are the conversation's from then on.
	ClaimAgentAttachments(attachmentIds []string, conversationId, messageId string) error

	// ListAgentAttachments is every file of a conversation, oldest first,
	// or every file of an agent when the conversation is empty.
	ListAgentAttachments(agentId, conversationId string) ([]*models.AgentAttachment, error)

	// ListOrphanAgentAttachments is what was uploaded and never sent with
	// a turn, older than the given time, for the sweep.
	ListOrphanAgentAttachments(before time.Time) ([]*models.AgentAttachment, error)

	DeleteAgentAttachment(attachmentId string) error
}

type agentAttachmentModel struct {
	ID             string    `gorm:"column:id;primaryKey"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	AgentID        string    `gorm:"column:agent_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	MessageID      string    `gorm:"column:message_id"`
	Name           string    `gorm:"column:name"`
	ContentType    string    `gorm:"column:content_type"`
	Size           int64     `gorm:"column:size"`
	Text           string    `gorm:"column:text"`
}

func (agentAttachmentModel) TableName() string { return "agent_attachment" }

func attachmentFromModel(model *agentAttachmentModel) *models.AgentAttachment {
	return &models.AgentAttachment{
		ID: model.ID, CreatedAt: model.CreatedAt.In(time.Local), AgentID: model.AgentID,
		ConversationID: model.ConversationID, MessageID: model.MessageID,
		Name: model.Name, ContentType: model.ContentType, Size: model.Size, Text: model.Text,
	}
}

func (self *transaction) CreateAgentAttachment(attachment *models.AgentAttachment) (*models.AgentAttachment, error) {
	if attachment.AgentID == "" || attachment.Name == "" {
		return nil, fmt.Errorf("db: an attachment needs an agent and a name")
	}
	model := &agentAttachmentModel{
		ID: newID(), CreatedAt: time.Now(), AgentID: attachment.AgentID,
		ConversationID: attachment.ConversationID, MessageID: attachment.MessageID,
		Name: truncateRunes(attachment.Name, 255), ContentType: truncateRunes(attachment.ContentType, 128),
		Size: attachment.Size, Text: attachment.Text,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return attachmentFromModel(model), nil
}

func (self *transaction) GetAgentAttachment(attachmentId string) (*models.AgentAttachment, error) {
	var model agentAttachmentModel
	result := self.tx.Where("\"id\" = ?", attachmentId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return attachmentFromModel(&model), nil
}

func (self *transaction) GetAgentAttachments(attachmentIds []string) ([]*models.AgentAttachment, error) {
	if len(attachmentIds) == 0 {
		return nil, nil
	}
	var found []agentAttachmentModel
	if err := self.tx.Where("\"id\" IN ?", attachmentIds).Find(&found).Error; err != nil {
		return nil, err
	}
	byId := map[string]*models.AgentAttachment{}
	for index := range found {
		byId[found[index].ID] = attachmentFromModel(&found[index])
	}
	attachments := make([]*models.AgentAttachment, 0, len(attachmentIds))
	for _, id := range attachmentIds {
		if attachment := byId[id]; attachment != nil {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (self *transaction) ClaimAgentAttachments(attachmentIds []string, conversationId, messageId string) error {
	if len(attachmentIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentAttachmentModel{}).Where("\"id\" IN ?", attachmentIds).Updates(map[string]any{
		"conversation_id": conversationId, "message_id": messageId,
	}).Error
}

func (self *transaction) ListAgentAttachments(agentId, conversationId string) ([]*models.AgentAttachment, error) {
	query := self.tx.Where("\"agent_id\" = ?", agentId)
	if conversationId != "" {
		query = query.Where("\"conversation_id\" = ?", conversationId)
	}
	var found []agentAttachmentModel
	if err := query.Order("\"created_at\" ASC, \"id\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	attachments := make([]*models.AgentAttachment, 0, len(found))
	for index := range found {
		attachments = append(attachments, attachmentFromModel(&found[index]))
	}
	return attachments, nil
}

func (self *transaction) ListOrphanAgentAttachments(before time.Time) ([]*models.AgentAttachment, error) {
	var found []agentAttachmentModel
	if err := self.tx.Where("\"message_id\" = '' AND \"created_at\" < ?", before).Limit(500).Find(&found).Error; err != nil {
		return nil, err
	}
	attachments := make([]*models.AgentAttachment, 0, len(found))
	for index := range found {
		attachments = append(attachments, attachmentFromModel(&found[index]))
	}
	return attachments, nil
}

func (self *transaction) DeleteAgentAttachment(attachmentId string) error {
	return self.tx.Where("\"id\" = ?", attachmentId).Delete(&agentAttachmentModel{}).Error
}
