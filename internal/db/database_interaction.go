package db

import (
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/models"
)

// InteractionOperation keeps the questions and approvals the agent put to
// the person until they are answered.
type InteractionOperation interface {
	// CreateAgentInteraction records a card as it is raised.
	CreateAgentInteraction(interaction *models.AgentInteraction) (*models.AgentInteraction, error)

	// GetAgentInteractionByCall is the card a tool call raised, or nil.
	GetAgentInteractionByCall(agentId, callId string) (*models.AgentInteraction, error)

	// ListOpenAgentInteractions is a conversation's unanswered cards,
	// oldest first.
	ListOpenAgentInteractions(conversationId string) ([]*models.AgentInteraction, error)

	// ClaimAgentInteraction resolves a card with an answer if it is still
	// open, and says whether it was: of the turn waiting on it and a late
	// answer, exactly one acts on it.
	ClaimAgentInteraction(interactionId, interactionAnswer string) (bool, error)
}

type agentInteractionModel struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	AgentID            string         `gorm:"column:agent_id"`
	ConversationID     string         `gorm:"column:conversation_id"`
	RunID              string         `gorm:"column:run_id"`
	CallID             string         `gorm:"column:call_id"`
	InteractionKind    string         `gorm:"column:interaction_kind"`
	ToolName           string         `gorm:"column:tool_name"`
	ToolArguments      string         `gorm:"column:tool_arguments"`
	InteractionText    string         `gorm:"column:interaction_text"`
	InteractionChoices pq.StringArray `gorm:"column:interaction_choices;type:text[]"`
	ToolRisk           string         `gorm:"column:tool_risk"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	ResolvedAt         *time.Time     `gorm:"column:resolved_at"`
	InteractionAnswer  string         `gorm:"column:interaction_answer"`
}

func (agentInteractionModel) TableName() string { return "agent_interaction" }

func (self *agentInteractionModel) toModel() *models.AgentInteraction {
	choices := []string(self.InteractionChoices)
	if choices == nil {
		choices = []string{}
	}
	return &models.AgentInteraction{
		ID: self.ID, AgentID: self.AgentID, ConversationID: self.ConversationID, RunID: self.RunID, CallID: self.CallID,
		InteractionKind: models.InteractionKind(self.InteractionKind), ToolName: self.ToolName, ToolArguments: self.ToolArguments,
		InteractionText: self.InteractionText, InteractionChoices: choices, ToolRisk: self.ToolRisk,
		CreatedAt: self.CreatedAt.In(time.Local), ResolvedAt: localTime(self.ResolvedAt), InteractionAnswer: self.InteractionAnswer,
	}
}

func (self *transaction) CreateAgentInteraction(interaction *models.AgentInteraction) (*models.AgentInteraction, error) {
	choices := interaction.InteractionChoices
	if choices == nil {
		choices = []string{}
	}
	model := &agentInteractionModel{
		ID: newID(), AgentID: interaction.AgentID, ConversationID: interaction.ConversationID, RunID: interaction.RunID,
		CallID: interaction.CallID, InteractionKind: string(interaction.InteractionKind), ToolName: interaction.ToolName,
		ToolArguments: interaction.ToolArguments, InteractionText: interaction.InteractionText,
		InteractionChoices: pq.StringArray(choices), ToolRisk: interaction.ToolRisk, CreatedAt: time.Now(),
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel(), nil
}

func (self *transaction) GetAgentInteractionByCall(agentId, callId string) (*models.AgentInteraction, error) {
	var found []agentInteractionModel
	if err := self.tx.Where(`"agent_id" = ? AND "call_id" = ?`, agentId, callId).Order(`"created_at" DESC`).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListOpenAgentInteractions(conversationId string) ([]*models.AgentInteraction, error) {
	var found []agentInteractionModel
	if err := self.tx.Where(`"conversation_id" = ? AND "resolved_at" IS NULL`, conversationId).Order(`"created_at" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	interactions := make([]*models.AgentInteraction, 0, len(found))
	for index := range found {
		interactions = append(interactions, found[index].toModel())
	}
	return interactions, nil
}

func (self *transaction) ClaimAgentInteraction(interactionId, interactionAnswer string) (bool, error) {
	result := self.tx.Model(&agentInteractionModel{}).Where(`"id" = ? AND "resolved_at" IS NULL`, interactionId).
		Updates(map[string]any{"resolved_at": time.Now(), "interaction_answer": interactionAnswer})
	return result.RowsAffected == 1, result.Error
}
