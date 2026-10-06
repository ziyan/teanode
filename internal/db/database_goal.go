package db

import (
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// GoalOperation keeps what a goal did and made: its activity, which the
// person reads as the goal's log, and the artifacts that carry no
// conversation of their own. The goal itself is a conversation of the kind
// goal, kept with the rest of them.
type GoalOperation interface {
	// ListAgentGoals is the agent's goal conversations in the states
	// given, every state when none is, the most recently active first.
	ListAgentGoals(agentId string, goalStates []models.AgentGoalState, limit int) ([]*models.AgentConversation, error)

	// AddAgentGoalActivity writes one row of a goal's log.
	AddAgentGoalActivity(activity *models.AgentGoalActivity) (*models.AgentGoalActivity, error)

	// ListAgentGoalActivity is a goal's log, newest first.
	ListAgentGoalActivity(agentId, conversationId string, limit int) ([]*models.AgentGoalActivity, error)

	// AddAgentGoalArtifact records something a goal made.
	AddAgentGoalArtifact(artifact *models.AgentGoalArtifact) (*models.AgentGoalArtifact, error)

	// ListAgentGoalArtifacts is what a goal made, oldest first.
	ListAgentGoalArtifacts(agentId, conversationId string) ([]*models.AgentGoalArtifact, error)

	// ListAgentGoalsWithoutNextTurn is every goal, across agents, that is
	// working with no next turn of its own: one its schedule keeps, or one
	// whose schedule has since gone and that needs a turn again.
	ListAgentGoalsWithoutNextTurn(limit int) ([]*models.AgentConversation, error)

	// ListAgentGoalsToSurface is every goal, across agents, that waits on
	// its person and has not yet been said in their main conversation.
	ListAgentGoalsToSurface(limit int) ([]*models.AgentConversation, error)
}

type agentGoalActivityModel struct {
	ID               string    `gorm:"column:id;primaryKey"`
	AgentID          string    `gorm:"column:agent_id"`
	ConversationID   string    `gorm:"column:conversation_id"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	GoalActivityKind string    `gorm:"column:goal_activity_kind"`
	ActivityHeadline string    `gorm:"column:activity_headline"`
	ActivityDetail   string    `gorm:"column:activity_detail"`
}

func (agentGoalActivityModel) TableName() string { return "agent_goal_activity" }

type agentGoalArtifactModel struct {
	ID                string    `gorm:"column:id;primaryKey"`
	AgentID           string    `gorm:"column:agent_id"`
	ConversationID    string    `gorm:"column:conversation_id"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	GoalArtifactKind  string    `gorm:"column:goal_artifact_kind"`
	ArtifactReference string    `gorm:"column:artifact_reference"`
	ArtifactTitle     string    `gorm:"column:artifact_title"`
}

func (agentGoalArtifactModel) TableName() string { return "agent_goal_artifact" }

func (self *transaction) ListAgentGoals(agentId string, goalStates []models.AgentGoalState, limit int) ([]*models.AgentConversation, error) {
	if limit <= 0 {
		limit = 200
	}
	query := self.tx.Where(`"agent_id" = ? AND "kind" = ? AND "goal" <> ''`, agentId, string(models.AgentConversationGoal))
	if len(goalStates) > 0 {
		states := make([]string, 0, len(goalStates))
		for _, goalState := range goalStates {
			states = append(states, string(goalState))
		}
		query = query.Where(`"goal_state" IN ?`, states)
	}
	var found []agentConversationModel
	if err := query.Order(`"last_at" DESC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) ListAgentGoalsWithoutNextTurn(limit int) ([]*models.AgentConversation, error) {
	if limit <= 0 {
		limit = 200
	}
	var found []agentConversationModel
	if err := self.tx.Where(`"kind" = ? AND "goal_state" = ? AND "goal_next_at" IS NULL AND "goal" <> ''`,
		string(models.AgentConversationGoal), string(models.GoalWorking)).
		Order(`"modified_at" ASC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) ListAgentGoalsToSurface(limit int) ([]*models.AgentConversation, error) {
	if limit <= 0 {
		limit = 50
	}
	var found []agentConversationModel
	if err := self.tx.Where(`"kind" = ? AND "goal_state" = ? AND "goal_surfaced_at" IS NULL AND "goal" <> ''`,
		string(models.AgentConversationGoal), string(models.GoalWaiting)).
		Order(`"modified_at" ASC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	conversations := make([]*models.AgentConversation, 0, len(found))
	for index := range found {
		conversations = append(conversations, conversationFromModel(&found[index]))
	}
	return conversations, nil
}

func (self *transaction) AddAgentGoalActivity(activity *models.AgentGoalActivity) (*models.AgentGoalActivity, error) {
	if activity.AgentID == "" || activity.ConversationID == "" || activity.GoalActivityKind == "" {
		return nil, fmt.Errorf("db: a goal's activity needs the agent, the goal and what happened")
	}
	model := &agentGoalActivityModel{
		ID: newID(), AgentID: activity.AgentID, ConversationID: activity.ConversationID, CreatedAt: time.Now(),
		GoalActivityKind: string(activity.GoalActivityKind),
		ActivityHeadline: truncateRunes(activity.ActivityHeadline, 300), ActivityDetail: truncateRunes(activity.ActivityDetail, 2000),
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return goalActivityFromModel(model), nil
}

func (self *transaction) ListAgentGoalActivity(agentId, conversationId string, limit int) ([]*models.AgentGoalActivity, error) {
	if limit <= 0 {
		limit = 100
	}
	var found []agentGoalActivityModel
	if err := self.tx.Where(`"agent_id" = ? AND "conversation_id" = ?`, agentId, conversationId).
		Order(`"created_at" DESC, "id" DESC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	activities := make([]*models.AgentGoalActivity, 0, len(found))
	for index := range found {
		activities = append(activities, goalActivityFromModel(&found[index]))
	}
	return activities, nil
}

func (self *transaction) AddAgentGoalArtifact(artifact *models.AgentGoalArtifact) (*models.AgentGoalArtifact, error) {
	if artifact.AgentID == "" || artifact.ConversationID == "" || artifact.GoalArtifactKind == "" {
		return nil, fmt.Errorf("db: a goal's artifact needs the agent, the goal and its kind")
	}
	model := &agentGoalArtifactModel{
		ID: newID(), AgentID: artifact.AgentID, ConversationID: artifact.ConversationID, CreatedAt: time.Now(),
		GoalArtifactKind:  string(artifact.GoalArtifactKind),
		ArtifactReference: truncateRunes(artifact.ArtifactReference, 300), ArtifactTitle: truncateRunes(artifact.ArtifactTitle, 300),
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return goalArtifactFromModel(model), nil
}

func (self *transaction) ListAgentGoalArtifacts(agentId, conversationId string) ([]*models.AgentGoalArtifact, error) {
	var found []agentGoalArtifactModel
	if err := self.tx.Where(`"agent_id" = ? AND "conversation_id" = ?`, agentId, conversationId).
		Order(`"created_at" ASC, "id" ASC`).Limit(200).Find(&found).Error; err != nil {
		return nil, err
	}
	artifacts := make([]*models.AgentGoalArtifact, 0, len(found))
	for index := range found {
		artifacts = append(artifacts, goalArtifactFromModel(&found[index]))
	}
	return artifacts, nil
}

func goalActivityFromModel(model *agentGoalActivityModel) *models.AgentGoalActivity {
	return &models.AgentGoalActivity{
		ID: model.ID, AgentID: model.AgentID, ConversationID: model.ConversationID, CreatedAt: model.CreatedAt.In(time.Local),
		GoalActivityKind: models.AgentGoalActivityKind(model.GoalActivityKind),
		ActivityHeadline: model.ActivityHeadline, ActivityDetail: model.ActivityDetail,
	}
}

func goalArtifactFromModel(model *agentGoalArtifactModel) *models.AgentGoalArtifact {
	return &models.AgentGoalArtifact{
		ID: model.ID, AgentID: model.AgentID, ConversationID: model.ConversationID, CreatedAt: model.CreatedAt.In(time.Local),
		GoalArtifactKind:  models.AgentGoalArtifactKind(model.GoalArtifactKind),
		ArtifactReference: model.ArtifactReference, ArtifactTitle: model.ArtifactTitle,
	}
}
