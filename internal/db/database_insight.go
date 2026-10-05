package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// InsightOperation is what the agent worked out about messages, and the
// transcripts of it working.
type InsightOperation interface {
	// PutMailInsight writes sorting fields, preserving existing proposals and
	// research notes. New insights may include initial proposals and notes.
	PutMailInsight(insight *models.MailInsight) error

	// SetMailInsightNotes writes what research found onto an insight.
	SetMailInsightNotes(mailId, mailboxId, notes, runId string) error

	// GetMailInsights reads the insights a mailbox has for these messages,
	// by mail id.
	GetMailInsights(mailboxId string, mailIds []string) (map[string]*models.MailInsight, error)
	LockMailInsight(mailboxId, mailId string) (*models.MailInsight, error)
	SetMailProposalStatus(mailboxId, mailId string, index int, proposalStatus string) error
	ReplaceMailProposals(mailboxId, mailId string, proposals []models.MailProposal) error

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

	// FindAgentConversationBySubject is the newest conversation of a kind
	// whose subject is the one given, not archived, or nil.
	FindAgentConversationBySubject(agentId string, kind models.AgentConversationKind, subjectId string) (*models.AgentConversation, error)
	// ListAgentGoalsInProgress is the agent's conversations whose goal is
	// still worked toward or waits on the person, archived or not, the
	// latest first.
	ListAgentGoalsInProgress(agentId string) ([]*models.AgentConversation, error)

	// UpdateAgentConversation changes a conversation. When its goal becomes
	// met, the ideas it was carrying out are done, and started again when
	// it is taken back up.
	UpdateAgentConversation(conversationId string, modify func(*models.AgentConversation) error) (*models.AgentConversation, error)

	// AddAgentConversationBackgroundWake counts one more turn woken by
	// background commands or work in a conversation, and
	// ResetAgentConversationBackgroundWakes starts the count again, for
	// the person having written.
	AddAgentConversationBackgroundWake(conversationId string) error
	ResetAgentConversationBackgroundWakes(conversationId string) error
	ListAgentConversations(agentId string, kinds []models.AgentConversationKind, options *Options) ([]*models.AgentConversation, error)

	// ListAgentRuns is the runs, newest first, narrowed by the filter and
	// paged by the options; CountAgentRuns is how many the filter leaves.
	// An empty agent is every agent's, for an operator.
	ListAgentRuns(agentId string, filter *AgentRunFilter, options *Options) ([]*models.AgentConversation, error)
	CountAgentRuns(agentId string, filter *AgentRunFilter) (int64, error)

	// SumAgentRunUsage is what each of these conversations cost: every
	// message's usage added up, keyed by conversation.
	SumAgentRunUsage(conversationIds []string) (map[string]models.AgentUsageNote, error)

	// SumAgentMessageCost is what every run of an agent has cost, from
	// the usage on its messages: what a scenario run has spent so far.
	SumAgentMessageCost(agentId string) (float64, error)

	// SumAgentDreamCost is what each of these dreams cost, keyed by
	// dream: its job's model calls made while it ran. By its own hours
	// rather than its whole job, because a dream a restart cut short and
	// the dream that took the job up again share one job.
	SumAgentDreamCost(dreams []*models.AgentDream) (map[string]float64, error)
	DeleteAgentConversation(conversationId string) error

	// SearchAgentConversations finds a person's conversations by words in
	// the title or in what was said, newest first, archived ones included.
	SearchAgentConversations(agentId, query string, limit int) ([]*models.AgentConversation, error)

	// ListAgentConversationsToRemember is every conversation with
	// something said in it that no remember run has read yet, quiet since
	// the moment given so that one still being typed into is left alone.
	ListAgentConversationsToRemember(quietSince time.Time, limit int) ([]*models.AgentConversation, error)

	// MarkAgentConversationRemembered records how far a remember run got.
	// Called in the same transaction as the facts it wrote.
	MarkAgentConversationRemembered(conversationId, messageId string, at time.Time) error

	// ListAgentConversationsToDescribe is every conversation quiet since
	// the given time with something said since it was last described.
	ListAgentConversationsToDescribe(quietSince time.Time, limit int) ([]*models.AgentConversation, error)

	// ListDueAgentGoals is every conversation whose goal is working and
	// whose next turn is due.
	ListDueAgentGoals(now time.Time, limit int) ([]*models.AgentConversation, error)

	// ScavengeAgentConversations removes run transcripts older than the
	// given time.
	ScavengeAgentConversations(kind models.AgentConversationKind, before time.Time) (int64, error)

	AppendAgentMessage(message *models.AgentMessage) (*models.AgentMessage, error)
	ListAgentMessages(conversationId string, options *Options) ([]*models.AgentMessage, error)
	// ListAgentOwnTurnAnswers is what the agent answered in a conversation
	// in turns of its own -- its last word of each round, never a round
	// that went on to call a tool -- after the message given and before
	// the moment given, oldest first. A turn of its own is one whose
	// opening message carries one of models.OwnTurnMarkers: nobody wrote
	// to it, so nobody's chat followed it.
	ListAgentOwnTurnAnswers(conversationId, afterMessageId string, before time.Time, limit int) ([]*models.AgentMessage, error)
	// LastAgentMessageID is the newest message of a conversation written
	// before the moment given, or empty.
	LastAgentMessageID(conversationId string, before time.Time) (string, error)
	// HasAgentToolAnswerContaining says whether a tool's answer in the
	// conversation holds any of the texts: what a page, a search or a
	// message showed the agent, as opposed to what the agent wrote.
	HasAgentToolAnswerContaining(conversationId string, texts ...string) (bool, error)
	// LastAgentPersonMessageAt is when the person last wrote in the
	// conversation: their own words, not a goal check-in the agent was
	// handed as a user turn. Nil when they never have.
	LastAgentPersonMessageAt(conversationId string) (*time.Time, error)
	// LastAgentPersonWordAt is when the person last wrote to this agent in
	// any of their own conversations: their words, not a goal check-in.
	// Nil when they never have.
	LastAgentPersonWordAt(agentId string) (*time.Time, error)
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
	ExtractAsked  bool      `gorm:"column:extract_asked"`
	Proposals     []byte    `gorm:"column:proposals;type:jsonb"`
	Notes         string    `gorm:"column:notes"`
	NotesRunID    string    `gorm:"column:notes_run_id"`
	AlertSignal   string    `gorm:"column:alert_signal"`
	AlertReason   string    `gorm:"column:alert_reason"`
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

	// How far a remember run has read. See migration 0067.
	RememberedThrough string     `gorm:"column:remembered_through"`
	RememberedAt      *time.Time `gorm:"column:remembered_at"`

	// The goal the agent keeps working toward in this conversation. See
	// migration 0083.
	Goal       string     `gorm:"column:goal"`
	GoalTitle  string     `gorm:"column:goal_title"`
	GoalState  string     `gorm:"column:goal_state"`
	GoalNote   string     `gorm:"column:goal_note"`
	GoalNextAt *time.Time `gorm:"column:goal_next_at"`
	GoalSetAt  *time.Time `gorm:"column:goal_set_at"`

	GoalSurfacedAt *time.Time `gorm:"column:goal_surfaced_at"`

	// How many turns background commands and work have woken since the
	// person last wrote. See migration 0122.
	BackgroundWakeCount int `gorm:"column:background_wake_count"`
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
		ExtractAsked:  model.ExtractAsked,
		Summary:       model.Summary,
		ActionItems:   []string{},
		Proposals:     []models.MailProposal{},
		Notes:         model.Notes,
		NotesRunID:    model.NotesRunID,
		AlertSignal:   model.AlertSignal,
		AlertReason:   model.AlertReason,
		Model:         model.Model,
		RunID:         model.RunID,
		CreatedAt:     model.CreatedAt.In(time.Local),
	}
	if err := decodeJSON(model.Proposals, &insight.Proposals); err != nil {
		return nil, err
	}
	if insight.Proposals == nil {
		insight.Proposals = []models.MailProposal{}
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
	proposals := insight.Proposals
	if proposals == nil {
		proposals = []models.MailProposal{}
	}
	encodedProposals, err := json.Marshal(proposals)
	if err != nil {
		return err
	}
	alertSignal := insight.AlertSignal
	if alertSignal == "" {
		alertSignal = models.AlertSignalNone
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
		ExtractAsked:  insight.ExtractAsked,
		Proposals:     encodedProposals,
		Notes:         insight.Notes,
		NotesRunID:    insight.NotesRunID,
		AlertSignal:   alertSignal,
		AlertReason:   insight.AlertReason,
		Model:         insight.Model,
		RunID:         insight.RunID,
		CreatedAt:     time.Now(),
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "mail_id"}, {Name: "mailbox_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"agent_id", "category", "priority", "needs_reply", "research_asked", "extract_asked", "summary", "action_items", "alert_signal", "alert_reason", "model", "run_id", "created_at"}),
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
	// A phone's notes are kept in the mailbox and are not mail to be sorted.
	err := self.tx.Raw(`SELECT "mailbox_item"."mail_id" FROM "mailbox_item"
		JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
		JOIN "mail" ON "mail"."id" = "mailbox_item"."mail_id"
		WHERE "mailbox_folder"."mailbox_id" = ? AND "mailbox_folder"."kind" NOT IN ('junk', 'trash', 'drafts', 'sent') AND "mailbox_item"."deleted" = false
		  AND "mail"."kind" <> 'note'
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
		ID:                model.ID,
		CreatedAt:         model.CreatedAt.In(time.Local),
		ModifiedAt:        model.ModifiedAt.In(time.Local),
		AgentID:           model.AgentID,
		MailboxID:         model.MailboxID,
		Kind:              models.AgentConversationKind(model.Kind),
		Title:             model.Title,
		Summary:           model.Summary,
		TitledBy:          model.TitledBy,
		JobID:             model.JobID,
		JobKind:           model.JobKind,
		SubjectID:         model.SubjectID,
		Surface:           model.Surface,
		LastAt:            model.LastAt.In(time.Local),
		CompactedThrough:  model.CompactedThrough,
		RememberedThrough: model.RememberedThrough,
		Goal:              model.Goal,
		GoalTitle:         model.GoalTitle,
		GoalState:         models.AgentGoalState(model.GoalState),
		GoalNote:          model.GoalNote,

		BackgroundWakeCount: model.BackgroundWakeCount,
	}
	if model.GoalNextAt != nil {
		at := model.GoalNextAt.In(time.Local)
		conversation.GoalNextAt = &at
	}
	if model.GoalSetAt != nil {
		at := model.GoalSetAt.In(time.Local)
		conversation.GoalSetAt = &at
	}
	if model.GoalSurfacedAt != nil {
		at := model.GoalSurfacedAt.In(time.Local)
		conversation.GoalSurfacedAt = &at
	}
	if model.RememberedAt != nil {
		at := model.RememberedAt.In(time.Local)
		conversation.RememberedAt = &at
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
		TitledBy:   conversation.TitledBy,
		JobID:      conversation.JobID,
		JobKind:    conversation.JobKind,
		SubjectID:  conversation.SubjectID,
		Surface:    conversation.Surface,
		LastAt:     now,
		Goal:       conversation.Goal,
		GoalTitle:  truncateRunes(conversation.GoalTitle, 200),
		GoalState:  string(conversation.GoalState),
		GoalNote:   conversation.GoalNote,
		GoalNextAt: conversation.GoalNextAt,
		GoalSetAt:  conversation.GoalSetAt,

		GoalSurfacedAt: conversation.GoalSurfacedAt,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return conversationFromModel(model), nil
}

func (self *transaction) FindAgentConversationBySubject(agentId string, kind models.AgentConversationKind, subjectId string) (*models.AgentConversation, error) {
	var found []agentConversationModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"kind\" = ? AND \"subject_id\" = ? AND \"archived_at\" IS NULL",
		agentId, string(kind), subjectId).Order("\"last_at\" DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return conversationFromModel(&found[0]), nil
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

func (self *transaction) AddAgentConversationBackgroundWake(conversationId string) error {
	return self.tx.Model(&agentConversationModel{}).Where(`"id" = ?`, conversationId).
		Update("background_wake_count", gorm.Expr(`"background_wake_count" + 1`)).Error
}

func (self *transaction) ResetAgentConversationBackgroundWakes(conversationId string) error {
	// Only when there is a count: most of what a person writes follows no
	// woken turn, and an update that changes nothing still takes the row.
	return self.tx.Model(&agentConversationModel{}).Where(`"id" = ? AND "background_wake_count" <> 0`, conversationId).
		Update("background_wake_count", 0).Error
}

// ListAgentGoalsInProgress: see the interface.
func (self *transaction) ListAgentGoalsInProgress(agentId string) ([]*models.AgentConversation, error) {
	var ids []string
	if err := self.tx.Model(&agentConversationModel{}).
		Where(`"agent_id" = ? AND "goal" <> '' AND "goal_state" NOT IN ?`, agentId, []string{string(models.GoalMet), string(models.GoalDropped)}).
		Order(`"last_at" DESC`).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(ids))
	for _, id := range ids {
		conversation, err := self.GetAgentConversation(id)
		if err != nil {
			return nil, err
		}
		if conversation != nil {
			conversations = append(conversations, conversation)
		}
	}
	return conversations, nil
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
		"modified_at": time.Now(), "kind": string(after.Kind), "title": truncateRunes(after.Title, 200), "summary": truncateRunes(after.Summary, 1000), "titled_by": after.TitledBy, "archived_at": after.ArchivedAt, "described_at": after.DescribedAt,
		"last_at": after.LastAt, "compacted_through": after.CompactedThrough, "surface": after.Surface,
		"goal": after.Goal, "goal_title": truncateRunes(after.GoalTitle, 200), "goal_state": string(after.GoalState), "goal_note": truncateRunes(after.GoalNote, 1000), "goal_next_at": after.GoalNextAt, "goal_set_at": after.GoalSetAt, "goal_surfaced_at": after.GoalSurfacedAt,
	}).Error; err != nil {
		return nil, err
	}
	// An idea carried out in a conversation is done when the conversation's
	// goal is met, whoever said so: the agent's goal tool or the person.
	// And started again when the goal is taken back up, which is what an
	// undo of "met" is. Here rather than at each of them, so that none can
	// forget.
	now := time.Now()
	switch isMet, wasMet := after.GoalState == models.GoalMet, before.GoalState == models.GoalMet; {
	case isMet && !wasMet:
		if err := self.tx.Model(&agentIdeaModel{}).
			Where(`"started_conversation_id" = ? AND "idea_status" = ?`, conversationId, string(models.IdeaStarted)).
			Updates(map[string]any{"idea_status": string(models.IdeaDone), "closed_at": now, "modified_at": now}).Error; err != nil {
			return nil, err
		}
	case wasMet && !isMet && after.Goal != "":
		if err := self.tx.Model(&agentIdeaModel{}).
			Where(`"started_conversation_id" = ? AND "idea_status" = ?`, conversationId, string(models.IdeaDone)).
			Updates(map[string]any{"idea_status": string(models.IdeaStarted), "closed_at": nil, "modified_at": now}).Error; err != nil {
			return nil, err
		}
	}
	return self.GetAgentConversation(conversationId)
}

// AgentRunFilter narrows a listing of runs: to one job's (a dream makes
// many, one per call), to some kinds, to titles carrying some words.
type AgentRunFilter struct {
	JobID string
	Kinds []string
	Query string
}

func (self *transaction) agentRunQuery(agentId string, filter *AgentRunFilter) *gorm.DB {
	query := self.tx.Model(&agentConversationModel{}).Where("\"kind\" = ?", string(models.AgentConversationRun))
	// No agent means every agent's: the operator's view.
	if agentId != "" {
		query = query.Where("\"agent_id\" = ?", agentId)
	}
	if filter == nil {
		return query
	}
	if filter.JobID != "" {
		query = query.Where("\"job_id\" = ?", filter.JobID)
	}
	if len(filter.Kinds) > 0 {
		query = query.Where("\"job_kind\" IN ?", filter.Kinds)
	}
	if words := strings.TrimSpace(filter.Query); words != "" {
		query = query.Where("\"title\" ILIKE ?", "%"+escapeLike(words)+"%")
	}
	return query
}

// ListAgentRuns is the runs, newest first, as the activity table shows
// them: a page at a time, because a bootstrapping dream makes hundreds a
// night and the table is meant to page through all of them.
func (self *transaction) ListAgentRuns(agentId string, filter *AgentRunFilter, options *Options) ([]*models.AgentConversation, error) {
	query := self.agentRunQuery(agentId, filter).Order("\"last_at\" DESC")
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	var rows []agentConversationModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(rows))
	for index := range rows {
		conversations = append(conversations, conversationFromModel(&rows[index]))
	}
	return conversations, nil
}

// SumAgentRunUsage adds up the usage on every message of each conversation,
// so a list of runs can say what each cost without reading its transcript.
func (self *transaction) SumAgentRunUsage(conversationIds []string) (map[string]models.AgentUsageNote, error) {
	totals := map[string]models.AgentUsageNote{}
	if len(conversationIds) == 0 {
		return totals, nil
	}
	var rows []struct {
		ConversationID   string
		PromptTokens     int
		CompletionTokens int
		CacheReadTokens  int
		CacheWriteTokens int
		Cost             float64
	}
	if err := self.tx.Raw(`
		SELECT "conversation_id",
		       coalesce(sum(("usage"->>'promptTokens')::bigint), 0) AS prompt_tokens,
		       coalesce(sum(("usage"->>'completionTokens')::bigint), 0) AS completion_tokens,
		       coalesce(sum(("usage"->>'cacheReadTokens')::bigint), 0) AS cache_read_tokens,
		       coalesce(sum(("usage"->>'cacheWriteTokens')::bigint), 0) AS cache_write_tokens,
		       coalesce(sum(("usage"->>'cost')::double precision), 0) AS cost
		FROM "agent_message"
		WHERE "conversation_id" = ANY(?) AND "usage" IS NOT NULL
		GROUP BY "conversation_id"`, pq.Array(conversationIds)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		totals[row.ConversationID] = models.AgentUsageNote{
			PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens,
			CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens, Cost: row.Cost,
		}
	}
	return totals, nil
}

func (self *transaction) SumAgentMessageCost(agentId string) (float64, error) {
	var cost float64
	err := self.tx.Raw(`
		SELECT coalesce(sum((m."usage"->>'cost')::double precision), 0)
		FROM "agent_message" m
		JOIN "agent_conversation" c ON c."id" = m."conversation_id"
		WHERE c."agent_id" = ? AND m."usage" IS NOT NULL`, agentId).Scan(&cost).Error
	return cost, err
}

func (self *transaction) SumAgentDreamCost(dreams []*models.AgentDream) (map[string]float64, error) {
	costs := map[string]float64{}
	var ids, jobIds, starts, ends []string
	for _, dream := range dreams {
		if dream.JobID == "" {
			continue
		}
		end := "infinity"
		if dream.FinishedAt != nil {
			end = dream.FinishedAt.Format(time.RFC3339Nano)
		}
		ids = append(ids, dream.ID)
		jobIds = append(jobIds, dream.JobID)
		starts = append(starts, dream.StartedAt.Format(time.RFC3339Nano))
		ends = append(ends, end)
	}
	if len(ids) == 0 {
		return costs, nil
	}
	var rows []struct {
		DreamID string
		Cost    float64
	}
	if err := self.tx.Raw(`
		SELECT d."dream_id", coalesce(sum((m."usage"->>'cost')::double precision), 0) AS cost
		FROM unnest(?::text[], ?::text[], ?::timestamptz[], ?::timestamptz[]) AS d("dream_id", "job_id", "started_at", "finished_at")
		JOIN "agent_conversation" c ON c."job_id" = d."job_id"
		JOIN "agent_message" m ON m."conversation_id" = c."id"
			AND m."created_at" >= d."started_at" AND m."created_at" <= d."finished_at"
		WHERE m."usage" IS NOT NULL
		GROUP BY d."dream_id"`, pq.Array(ids), pq.Array(jobIds), pq.Array(starts), pq.Array(ends)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		costs[row.DreamID] = row.Cost
	}
	return costs, nil
}

// CountAgentRuns is how many runs the filter leaves, for the pager.
func (self *transaction) CountAgentRuns(agentId string, filter *AgentRunFilter) (int64, error) {
	var total int64
	err := self.agentRunQuery(agentId, filter).Count(&total).Error
	return total, err
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
	kinds := []string{string(models.AgentConversationMain), string(models.AgentConversationNamed)}
	// The messages read are this agent's own chats only. Unbounded, the
	// pattern was matched against every message on the server, nearly all
	// of them from runs nobody searches, and a search took half a minute.
	chats := self.tx.Model(&agentConversationModel{}).Select("\"id\"").Where("\"agent_id\" = ? AND \"kind\" IN ?", agentId, kinds)
	var found []agentConversationModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"kind\" IN ? AND (\"title\" ILIKE ? OR \"summary\" ILIKE ? OR \"id\" IN (?))",
		agentId, kinds, pattern, pattern,
		self.tx.Model(&agentMessageModel{}).Select("\"conversation_id\"").Where("\"conversation_id\" IN (?) AND \"role\" IN ? AND \"content\" ILIKE ?", chats, []string{"user", "assistant"}, pattern),
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

// ListDueAgentGoals is the conversations the agent owes a turn of its own:
// a goal that is working, with its next time passed.
//
// A goal that is waiting for the person or already met has no next time,
// so this is the whole of the sweep's question; the partial index of
// migration 0083 answers it out of the few rows that have a goal running.
func (self *transaction) ListDueAgentGoals(now time.Time, limit int) ([]*models.AgentConversation, error) {
	if limit <= 0 {
		limit = 50
	}
	var found []agentConversationModel
	if err := self.tx.Where("\"goal_state\" = ? AND \"goal_next_at\" IS NOT NULL AND \"goal_next_at\" <= ?",
		string(models.GoalWorking), now,
	).Order("\"goal_next_at\" ASC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

// ListAgentConversationsToRemember is the conversations with something in
// them the agent has not filed.
//
// "Not filed" is a message newer than the one the last run stopped at,
// which is a different question from "changed since we last looked": a run
// that failed leaves the mark where it was, so the work comes back round
// rather than being lost.
func (self *transaction) ListAgentConversationsToRemember(quietSince time.Time, limit int) ([]*models.AgentConversation, error) {
	if limit <= 0 {
		limit = 20
	}
	var found []agentConversationModel
	if err := self.tx.Raw(`
		SELECT c.* FROM "agent_conversation" c
		WHERE c."kind" IN ('main', 'named') AND c."last_at" < ?
		  AND EXISTS (
			SELECT 1 FROM "agent_message" m
			WHERE m."conversation_id" = c."id"
			  AND m."role" IN ('user', 'assistant')
			  AND (c."remembered_through" = '' OR m."id" > c."remembered_through")
		  )
		ORDER BY c."last_at" ASC LIMIT ?`, quietSince, limit).Scan(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) MarkAgentConversationRemembered(conversationId, messageId string, at time.Time) error {
	if conversationId == "" {
		return fmt.Errorf("db: marking a conversation filed needs the conversation")
	}
	return self.tx.Model(&agentConversationModel{}).Where(`"id" = ?`, conversationId).
		Updates(map[string]any{"remembered_through": messageId, "remembered_at": at}).Error
}

func (self *transaction) DeleteAgentConversation(conversationId string) error {
	// An idea started in the conversation would otherwise stay started,
	// pointing at nothing, and never be offered again: it goes back on
	// offer. One done or dismissed keeps what became of it and loses only
	// the link. Here rather than at each caller, so that none can forget.
	if conversationId == "" {
		return fmt.Errorf("db: deleting a conversation needs the conversation")
	}
	now := time.Now()
	if err := self.tx.Model(&agentIdeaModel{}).
		Where(`"started_conversation_id" = ? AND "idea_status" = ?`, conversationId, string(models.IdeaStarted)).
		Updates(map[string]any{"idea_status": string(models.IdeaOpen), "started_conversation_id": "", "started_at": nil, "closed_at": nil, "modified_at": now}).Error; err != nil {
		return err
	}
	if err := self.tx.Model(&agentIdeaModel{}).
		Where(`"started_conversation_id" = ?`, conversationId).
		Updates(map[string]any{"started_conversation_id": "", "modified_at": now}).Error; err != nil {
		return err
	}
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
	// A message is text. A prompt that quotes a file read from disk may
	// carry bytes that are not, and PostgreSQL refuses the row for one of
	// them; the replacement character keeps the row and marks the spot.
	model := &agentMessageModel{
		ID:             newID(),
		CreatedAt:      time.Now(),
		ConversationID: message.ConversationID,
		Role:           message.Role,
		Content:        strings.ToValidUTF8(message.Content, "\uFFFD"),
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

func (self *transaction) ListAgentOwnTurnAnswers(conversationId, afterMessageId string, before time.Time, limit int) ([]*models.AgentMessage, error) {
	if limit <= 0 {
		limit = 20
	}
	markers := make([]string, 0, len(models.OwnTurnMarkers))
	for _, marker := range models.OwnTurnMarkers {
		markers = append(markers, escapeLike(marker)+"%")
	}
	var found []agentMessageModel
	if err := self.tx.Raw(`SELECT "answer".* FROM "agent_message" AS "answer"
		WHERE "answer"."conversation_id" = ? AND "answer"."id" > ? AND "answer"."created_at" < ?
		  AND "answer"."role" = 'assistant' AND "answer"."content" <> ''
		  AND ("answer"."tool_calls" IS NULL OR jsonb_typeof("answer"."tool_calls") <> 'array' OR jsonb_array_length("answer"."tool_calls") = 0)
		  AND (SELECT "opening"."content" FROM "agent_message" AS "opening"
		       WHERE "opening"."conversation_id" = "answer"."conversation_id" AND "opening"."role" = 'user'
		         AND ("opening"."created_at", "opening"."id") < ("answer"."created_at", "answer"."id")
		       ORDER BY "opening"."created_at" DESC, "opening"."id" DESC LIMIT 1) LIKE ANY (?)
		ORDER BY "answer"."created_at" ASC, "answer"."id" ASC LIMIT ?`,
		conversationId, afterMessageId, before, pq.Array(markers), limit).Scan(&found).Error; err != nil {
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

func (self *transaction) LastAgentMessageID(conversationId string, before time.Time) (string, error) {
	var ids []string
	if err := self.tx.Model(&agentMessageModel{}).Where("\"conversation_id\" = ? AND \"created_at\" < ?", conversationId, before).
		Order("\"created_at\" DESC, \"id\" DESC").Limit(1).Pluck("id", &ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

func (self *transaction) HasAgentToolAnswerContaining(conversationId string, texts ...string) (bool, error) {
	for _, text := range texts {
		if text == "" {
			continue
		}
		var found bool
		if err := self.tx.Raw(`SELECT EXISTS (SELECT 1 FROM "agent_message" WHERE "conversation_id" = ? AND "role" = 'tool' AND strpos("content", ?) > 0)`,
			conversationId, text).Scan(&found).Error; err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func (self *transaction) LastAgentPersonMessageAt(conversationId string) (*time.Time, error) {
	var last []time.Time
	query := self.tx.Model(&agentMessageModel{}).
		Where("\"conversation_id\" = ? AND \"role\" = ?", conversationId, "user")
	if err := withoutOwnTurns(query, "\"content\"").
		Order("\"created_at\" DESC").Limit(1).Pluck("created_at", &last).Error; err != nil {
		return nil, err
	}
	if len(last) == 0 {
		return nil, nil
	}
	return &last[0], nil
}

func (self *transaction) LastAgentPersonWordAt(agentId string) (*time.Time, error) {
	var last []time.Time
	query := self.tx.Model(&agentMessageModel{}).
		Joins("JOIN \"agent_conversation\" ON \"agent_conversation\".\"id\" = \"agent_message\".\"conversation_id\"").
		Where("\"agent_conversation\".\"agent_id\" = ? AND \"agent_conversation\".\"kind\" IN ? AND \"agent_message\".\"role\" = ?",
			agentId, []string{string(models.AgentConversationMain), string(models.AgentConversationNamed)}, "user")
	if err := withoutOwnTurns(query, "\"agent_message\".\"content\"").
		Order("\"agent_message\".\"created_at\" DESC").Limit(1).Pluck("\"agent_message\".\"created_at\"", &last).Error; err != nil {
		return nil, err
	}
	if len(last) == 0 {
		return nil, nil
	}
	return &last[0], nil
}

// withoutOwnTurns leaves out the messages that open a turn the agent took
// on its own: they are in the person's shape and are not the person.
func withoutOwnTurns(query *gorm.DB, column string) *gorm.DB {
	for _, marker := range models.OwnTurnMarkers {
		query = query.Where(column+" NOT LIKE ?", escapeLike(marker)+"%")
	}
	return query
}

var _ = gorm.ErrRecordNotFound

// LockMailInsight serializes proposal acceptance with other insight writers.
func (self *transaction) LockMailInsight(mailboxId, mailId string) (*models.MailInsight, error) {
	var found []mailInsightModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("mailbox_id = ? AND mail_id = ?", mailboxId, mailId).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return insightFromModel(&found[0])
}

// SetMailProposalStatus changes only one proposal status, preserving unrelated
// insight fields. Callers lock the insight and validate the proposal first.
func (self *transaction) SetMailProposalStatus(mailboxId, mailId string, index int, proposalStatus string) error {
	if index < 0 || (proposalStatus != models.MailProposalAccepted && proposalStatus != models.MailProposalDismissed) {
		return ErrInvalidArguments
	}
	update := self.tx.Exec(`UPDATE mail_insight SET proposals = jsonb_set(proposals, ARRAY[?::text, 'status'], to_jsonb(?::text)) WHERE mailbox_id = ? AND mail_id = ? AND jsonb_array_length(proposals) > ?`, fmt.Sprint(index), proposalStatus, mailboxId, mailId, index)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

// ReplaceMailProposals replaces outstanding offers while preserving the person's
// decisions. The row lock prevents acceptance from being lost to a reading run.
func (self *transaction) ReplaceMailProposals(mailboxId, mailId string, proposals []models.MailProposal) error {
	insight, err := self.LockMailInsight(mailboxId, mailId)
	if err != nil || insight == nil {
		return err
	}
	kept := make([]models.MailProposal, 0, len(insight.Proposals)+len(proposals))
	for _, proposal := range insight.Proposals {
		if proposal.Status != models.MailProposalOffered {
			kept = append(kept, proposal)
		}
	}
	encoded, err := json.Marshal(append(kept, proposals...))
	if err != nil {
		return err
	}
	return self.tx.Model(&mailInsightModel{}).Where("mailbox_id = ? AND mail_id = ?", mailboxId, mailId).Update("proposals", encoded).Error
}
