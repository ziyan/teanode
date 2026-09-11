package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// InsightOperation is what the agent worked out about messages, and the
// transcripts of it working.
type InsightOperation interface {
	// PutMailInsight writes or replaces the insight for a message in a
	// mailbox.
	PutMailInsight(insight *models.MailInsight) error

	// SetMailInsightNotes writes what research found onto an insight.
	SetMailInsightNotes(mailId, mailboxId, notes, runId string) error

	// GetMailInsights reads the insights a mailbox has for these messages,
	// by mail id.
	GetMailInsights(mailboxId string, mailIds []string) (map[string]*models.MailInsight, error)

	// ListMailWithoutInsight is the newest messages of a mailbox — in any
	// folder but Junk, Trash, Drafts and Sent, since a rule may have filed
	// a message before the agent saw it — that have no insight yet, for a
	// backfill.
	ListMailWithoutInsight(mailboxId string, limit int) ([]string, error)

	// DeleteMailInsights forgets everything a mailbox was told.
	DeleteMailInsights(mailboxId string) (int64, error)

	// PutThreadSummary writes or replaces a conversation's summary.
	PutThreadSummary(summary *models.ThreadSummary) error

	// GetThreadSummary reads a conversation's summary, nil when there is
	// none.
	GetThreadSummary(mailboxId, threadId string) (*models.ThreadSummary, error)

	// DeleteThreadSummaries forgets every summary a mailbox has.
	DeleteThreadSummaries(mailboxId string) (int64, error)

	CreateAgentConversation(conversation *models.AgentConversation) (*models.AgentConversation, error)
	GetAgentConversation(conversationId string) (*models.AgentConversation, error)
	UpdateAgentConversation(conversationId string, modify func(*models.AgentConversation) error) (*models.AgentConversation, error)
	ListAgentConversations(agentId string, kinds []models.AgentConversationKind, options *Options) ([]*models.AgentConversation, error)
	DeleteAgentConversation(conversationId string) error

	// SearchAgentConversations finds a person's conversations by words in
	// the title or in what was said, newest first, archived ones included.
	SearchAgentConversations(agentId, query string, limit int) ([]*models.AgentConversation, error)

	// ListAgentConversationsToDescribe is every conversation quiet since
	// the given time with something said since it was last described.
	ListAgentConversationsToDescribe(quietSince time.Time, limit int) ([]*models.AgentConversation, error)

	// ScavengeAgentConversations removes run transcripts older than the
	// given time.
	ScavengeAgentConversations(kind models.AgentConversationKind, before time.Time) (int64, error)

	AppendAgentMessage(message *models.AgentMessage) (*models.AgentMessage, error)
	ListAgentMessages(conversationId string, options *Options) ([]*models.AgentMessage, error)
}

type mailInsightModel struct {
	MailID        string    `gorm:"column:mail_id;primaryKey"`
	MailboxID     string    `gorm:"column:mailbox_id;primaryKey"`
	AgentID       string    `gorm:"column:agent_id"`
	Category      string    `gorm:"column:category"`
	Priority      string    `gorm:"column:priority"`
	NeedsReply    bool      `gorm:"column:needs_reply"`
	ResearchAsked bool      `gorm:"column:research_asked"`
	Summary       string    `gorm:"column:summary"`
	ActionItems   []byte    `gorm:"column:action_items;type:jsonb"`
	Notes         string    `gorm:"column:notes"`
	NotesRunID    string    `gorm:"column:notes_run_id"`
	Model         string    `gorm:"column:model"`
	RunID         string    `gorm:"column:run_id"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (mailInsightModel) TableName() string { return "mail_insight" }

type agentConversationModel struct {
	ID               string     `gorm:"column:id;primaryKey"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	ModifiedAt       time.Time  `gorm:"column:modified_at"`
	AgentID          string     `gorm:"column:agent_id"`
	MailboxID        string     `gorm:"column:mailbox_id"`
	Kind             string     `gorm:"column:kind"`
	Title            string     `gorm:"column:title"`
	Summary          string     `gorm:"column:summary"`
	TitledBy         string     `gorm:"column:titled_by"`
	DescribedAt      *time.Time `gorm:"column:described_at"`
	JobID            string     `gorm:"column:job_id"`
	JobKind          string     `gorm:"column:job_kind"`
	SubjectID        string     `gorm:"column:subject_id"`
	Surface          string     `gorm:"column:surface"`
	ArchivedAt       *time.Time `gorm:"column:archived_at"`
	LastAt           time.Time  `gorm:"column:last_at"`
	CompactedThrough string     `gorm:"column:compacted_through"`
}

func (agentConversationModel) TableName() string { return "agent_conversation" }

type agentMessageModel struct {
	ID             string    `gorm:"column:id;primaryKey"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	ConversationID string    `gorm:"column:conversation_id"`
	Role           string    `gorm:"column:role"`
	Content        string    `gorm:"column:content"`
	ToolCalls      []byte    `gorm:"column:tool_calls;type:jsonb"`
	ToolCallID     string    `gorm:"column:tool_call_id"`
	Name           string    `gorm:"column:name"`
	Usage          []byte    `gorm:"column:usage;type:jsonb"`
	Attachments    []byte    `gorm:"column:attachments;type:jsonb"`
	References     []byte    `gorm:"column:references;type:jsonb"`
}

func (agentMessageModel) TableName() string { return "agent_message" }

func insightFromModel(model *mailInsightModel) (*models.MailInsight, error) {
	insight := &models.MailInsight{
		MailID:        model.MailID,
		MailboxID:     model.MailboxID,
		AgentID:       model.AgentID,
		Category:      model.Category,
		Priority:      model.Priority,
		NeedsReply:    model.NeedsReply,
		ResearchAsked: model.ResearchAsked,
		Summary:       model.Summary,
		ActionItems:   []string{},
		Notes:         model.Notes,
		NotesRunID:    model.NotesRunID,
		Model:         model.Model,
		RunID:         model.RunID,
		CreatedAt:     model.CreatedAt.In(time.Local),
	}
	if err := decodeJSON(model.ActionItems, &insight.ActionItems); err != nil {
		return nil, fmt.Errorf("db: cannot read the action items of insight %q: %w", model.MailID, err)
	}
	if insight.ActionItems == nil {
		insight.ActionItems = []string{}
	}
	return insight, nil
}

func (self *transaction) PutMailInsight(insight *models.MailInsight) error {
	if insight.MailID == "" || insight.MailboxID == "" {
		return fmt.Errorf("db: an insight needs a message and a mailbox")
	}
	items := insight.ActionItems
	if items == nil {
		items = []string{}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return err
	}
	model := &mailInsightModel{
		MailID:        insight.MailID,
		MailboxID:     insight.MailboxID,
		AgentID:       insight.AgentID,
		Category:      insight.Category,
		Priority:      insight.Priority,
		NeedsReply:    insight.NeedsReply,
		ResearchAsked: insight.ResearchAsked,
		Summary:       insight.Summary,
		ActionItems:   encoded,
		Notes:         insight.Notes,
		NotesRunID:    insight.NotesRunID,
		Model:         insight.Model,
		RunID:         insight.RunID,
		CreatedAt:     time.Now(),
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "mail_id"}, {Name: "mailbox_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"agent_id", "category", "priority", "needs_reply", "research_asked", "summary", "action_items", "model", "run_id", "created_at"}),
	}).Create(model).Error
}

type threadSummaryModel struct {
	ThreadID      string    `gorm:"column:thread_id;primaryKey"`
	MailboxID     string    `gorm:"column:mailbox_id;primaryKey"`
	AgentID       string    `gorm:"column:agent_id"`
	Summary       string    `gorm:"column:summary"`
	ThroughMailID string    `gorm:"column:through_mail_id"`
	MessageCount  int       `gorm:"column:message_count"`
	Model         string    `gorm:"column:model"`
	RunID         string    `gorm:"column:run_id"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (threadSummaryModel) TableName() string { return "thread_summary" }

func (self *threadSummaryModel) toModel() *models.ThreadSummary {
	return &models.ThreadSummary{
		ThreadID:      self.ThreadID,
		MailboxID:     self.MailboxID,
		AgentID:       self.AgentID,
		Summary:       self.Summary,
		ThroughMailID: self.ThroughMailID,
		MessageCount:  self.MessageCount,
		Model:         self.Model,
		RunID:         self.RunID,
		CreatedAt:     self.CreatedAt,
	}
}

func (self *transaction) PutThreadSummary(summary *models.ThreadSummary) error {
	if summary.ThreadID == "" || summary.MailboxID == "" {
		return fmt.Errorf("db: a summary needs a conversation and a mailbox")
	}
	model := &threadSummaryModel{
		ThreadID:      summary.ThreadID,
		MailboxID:     summary.MailboxID,
		AgentID:       summary.AgentID,
		Summary:       summary.Summary,
		ThroughMailID: summary.ThroughMailID,
		MessageCount:  summary.MessageCount,
		Model:         summary.Model,
		RunID:         summary.RunID,
		CreatedAt:     time.Now(),
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "thread_id"}, {Name: "mailbox_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"agent_id", "summary", "through_mail_id", "message_count", "model", "run_id", "created_at"}),
	}).Create(model).Error; err != nil {
		return err
	}
	summary.CreatedAt = model.CreatedAt
	return nil
}

func (self *transaction) GetThreadSummary(mailboxId, threadId string) (*models.ThreadSummary, error) {
	var found []*threadSummaryModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"thread_id\" = ?", mailboxId, threadId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) DeleteThreadSummaries(mailboxId string) (int64, error) {
	result := self.tx.Where("\"mailbox_id\" = ?", mailboxId).Delete(&threadSummaryModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) SetMailInsightNotes(mailId, mailboxId, notes, runId string) error {
	return self.tx.Model(&mailInsightModel{}).Where("\"mail_id\" = ? AND \"mailbox_id\" = ?", mailId, mailboxId).Updates(map[string]any{
		"notes": notes, "notes_run_id": runId,
	}).Error
}

func (self *transaction) GetMailInsights(mailboxId string, mailIds []string) (map[string]*models.MailInsight, error) {
	insights := map[string]*models.MailInsight{}
	if len(mailIds) == 0 {
		return insights, nil
	}
	var found []mailInsightModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"mail_id\" IN ?", mailboxId, mailIds).Find(&found).Error; err != nil {
		return nil, err
	}
	for index := range found {
		insight, err := insightFromModel(&found[index])
		if err != nil {
			return nil, err
		}
		insights[insight.MailID] = insight
	}
	return insights, nil
}

func (self *transaction) ListMailWithoutInsight(mailboxId string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	var ids []string
	err := self.tx.Raw(`SELECT "mailbox_item"."mail_id" FROM "mailbox_item"
		JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
		WHERE "mailbox_folder"."mailbox_id" = ? AND "mailbox_folder"."kind" NOT IN ('junk', 'trash', 'drafts', 'sent') AND "mailbox_item"."deleted" = false
		  AND NOT EXISTS (SELECT 1 FROM "mail_insight" WHERE "mail_insight"."mail_id" = "mailbox_item"."mail_id" AND "mail_insight"."mailbox_id" = ?)
		ORDER BY "mailbox_item"."added_at" DESC LIMIT ?`, mailboxId, mailboxId, limit).Scan(&ids).Error
	if ids == nil {
		ids = []string{}
	}
	return ids, err
}

func (self *transaction) DeleteMailInsights(mailboxId string) (int64, error) {
	result := self.tx.Where("\"mailbox_id\" = ?", mailboxId).Delete(&mailInsightModel{})
	return result.RowsAffected, result.Error
}

func conversationFromModel(model *agentConversationModel) *models.AgentConversation {
	conversation := &models.AgentConversation{
		ID:               model.ID,
		CreatedAt:        model.CreatedAt.In(time.Local),
		ModifiedAt:       model.ModifiedAt.In(time.Local),
		AgentID:          model.AgentID,
		MailboxID:        model.MailboxID,
		Kind:             models.AgentConversationKind(model.Kind),
		Title:            model.Title,
		Summary:          model.Summary,
		TitledBy:         model.TitledBy,
		JobID:            model.JobID,
		JobKind:          model.JobKind,
		SubjectID:        model.SubjectID,
		Surface:          model.Surface,
		LastAt:           model.LastAt.In(time.Local),
		CompactedThrough: model.CompactedThrough,
	}
	if model.ArchivedAt != nil {
		at := model.ArchivedAt.In(time.Local)
		conversation.ArchivedAt = &at
	}
	if model.DescribedAt != nil {
		at := model.DescribedAt.In(time.Local)
		conversation.DescribedAt = &at
	}
	return conversation
}

func (self *transaction) CreateAgentConversation(conversation *models.AgentConversation) (*models.AgentConversation, error) {
	if conversation.AgentID == "" || conversation.Kind == "" {
		return nil, fmt.Errorf("db: a conversation needs an agent and a kind")
	}
	now := time.Now()
	model := &agentConversationModel{
		ID:         newID(),
		CreatedAt:  now,
		ModifiedAt: now,
		AgentID:    conversation.AgentID,
		MailboxID:  conversation.MailboxID,
		Kind:       string(conversation.Kind),
		Title:      truncateRunes(conversation.Title, 200),
		JobID:      conversation.JobID,
		JobKind:    conversation.JobKind,
		SubjectID:  conversation.SubjectID,
		Surface:    conversation.Surface,
		LastAt:     now,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return conversationFromModel(model), nil
}

func (self *transaction) GetAgentConversation(conversationId string) (*models.AgentConversation, error) {
	var model agentConversationModel
	result := self.tx.Where("\"id\" = ?", conversationId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return conversationFromModel(&model), nil
}

func (self *transaction) UpdateAgentConversation(conversationId string, modify func(*models.AgentConversation) error) (*models.AgentConversation, error) {
	if err := lockRow(self.tx, &agentConversationModel{}, conversationId); err != nil {
		return nil, err
	}
	before, err := self.GetAgentConversation(conversationId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&agentConversationModel{}).Where("\"id\" = ?", conversationId).Updates(map[string]any{
		"modified_at": time.Now(), "title": truncateRunes(after.Title, 200), "summary": truncateRunes(after.Summary, 1000), "titled_by": after.TitledBy, "archived_at": after.ArchivedAt, "described_at": after.DescribedAt,
		"last_at": after.LastAt, "compacted_through": after.CompactedThrough, "surface": after.Surface,
	}).Error; err != nil {
		return nil, err
	}
	return self.GetAgentConversation(conversationId)
}

func (self *transaction) ListAgentConversations(agentId string, kinds []models.AgentConversationKind, options *Options) ([]*models.AgentConversation, error) {
	query := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"last_at\" DESC")
	if len(kinds) > 0 {
		names := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			names = append(names, string(kind))
		}
		query = query.Where("\"kind\" IN ?", names)
	}
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	var found []agentConversationModel
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) SearchAgentConversations(agentId, query string, limit int) ([]*models.AgentConversation, error) {
	pattern := "%" + strings.TrimSpace(query) + "%"
	if limit <= 0 {
		limit = 50
	}
	var found []agentConversationModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"kind\" IN ? AND (\"title\" ILIKE ? OR \"summary\" ILIKE ? OR \"id\" IN (?))",
		agentId, []string{string(models.AgentConversationMain), string(models.AgentConversationNamed)}, pattern, pattern,
		self.tx.Model(&agentMessageModel{}).Select("\"conversation_id\"").Where("\"role\" IN ? AND \"content\" ILIKE ?", []string{"user", "assistant"}, pattern),
	).Order("\"last_at\" DESC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) ListAgentConversationsToDescribe(quietSince time.Time, limit int) ([]*models.AgentConversation, error) {
	if limit <= 0 {
		limit = 20
	}
	var found []agentConversationModel
	if err := self.tx.Where("\"kind\" IN ? AND \"last_at\" < ? AND (\"described_at\" IS NULL OR \"described_at\" < \"last_at\")",
		[]string{string(models.AgentConversationMain), string(models.AgentConversationNamed)}, quietSince,
	).Order("\"last_at\" ASC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) DeleteAgentConversation(conversationId string) error {
	return self.tx.Where("\"id\" = ?", conversationId).Delete(&agentConversationModel{}).Error
}

func (self *transaction) ScavengeAgentConversations(kind models.AgentConversationKind, before time.Time) (int64, error) {
	result := self.tx.Where("\"kind\" = ? AND \"last_at\" < ?", string(kind), before).Delete(&agentConversationModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) AppendAgentMessage(message *models.AgentMessage) (*models.AgentMessage, error) {
	if message.ConversationID == "" || message.Role == "" {
		return nil, fmt.Errorf("db: a message needs a conversation and a role")
	}
	model := &agentMessageModel{
		ID:             newID(),
		CreatedAt:      time.Now(),
		ConversationID: message.ConversationID,
		Role:           message.Role,
		Content:        message.Content,
		ToolCallID:     message.ToolCallID,
		Name:           message.Name,
	}
	var err error
	if len(message.ToolCalls) > 0 {
		if model.ToolCalls, err = json.Marshal(message.ToolCalls); err != nil {
			return nil, err
		}
	}
	if model.Usage, err = encodeJSON(message.Usage); err != nil {
		return nil, err
	}
	if len(message.Attachments) > 0 {
		if model.Attachments, err = json.Marshal(message.Attachments); err != nil {
			return nil, err
		}
	}
	if len(message.References) > 0 {
		if model.References, err = json.Marshal(message.References); err != nil {
			return nil, err
		}
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&agentConversationModel{}).Where("\"id\" = ?", message.ConversationID).Updates(map[string]any{
		"last_at": model.CreatedAt, "modified_at": model.CreatedAt,
	}).Error; err != nil {
		return nil, err
	}
	return messageFromModel(model)
}

func messageFromModel(model *agentMessageModel) (*models.AgentMessage, error) {
	message := &models.AgentMessage{
		ID:             model.ID,
		CreatedAt:      model.CreatedAt.In(time.Local),
		ConversationID: model.ConversationID,
		Role:           model.Role,
		Content:        model.Content,
		ToolCallID:     model.ToolCallID,
		Name:           model.Name,
	}
	if err := decodeJSON(model.ToolCalls, &message.ToolCalls); err != nil {
		return nil, fmt.Errorf("db: cannot read the tool calls of message %q: %w", model.ID, err)
	}
	if err := decodeJSON(model.Usage, &message.Usage); err != nil {
		return nil, fmt.Errorf("db: cannot read the usage of message %q: %w", model.ID, err)
	}
	if err := decodeJSON(model.Attachments, &message.Attachments); err != nil {
		return nil, fmt.Errorf("db: cannot read the attachments of message %q: %w", model.ID, err)
	}
	if err := decodeJSON(model.References, &message.References); err != nil {
		return nil, fmt.Errorf("db: cannot read the references of message %q: %w", model.ID, err)
	}
	return message, nil
}

func (self *transaction) ListAgentMessages(conversationId string, options *Options) ([]*models.AgentMessage, error) {
	query := self.tx.Where("\"conversation_id\" = ?", conversationId).Order("\"created_at\" ASC, \"id\" ASC")
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	var found []agentMessageModel
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	messages := make([]*models.AgentMessage, 0, len(found))
	for index := range found {
		message, err := messageFromModel(&found[index])
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

var _ = gorm.ErrRecordNotFound
