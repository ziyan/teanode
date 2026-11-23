package client

import (
	"context"
	"time"
)

// Goals: what the agent keeps at in the background, each in a conversation
// of its own, until it is met or dropped.

// AgentGoal is one goal; its activity and what it made are filled only
// when it was read one at a time.
type AgentGoal struct {
	ConversationID  string `json:"conversationId"`
	GoalTitle       string `json:"goalTitle"`
	GoalDescription string `json:"goalDescription"`

	// GoalState is working, waiting, paused, met or dropped, and
	// GoalStatus the agent's one line on where it stands.
	GoalState  string `json:"goalState"`
	GoalStatus string `json:"goalStatus"`

	// GoalTimes are when its turns are, in the person's zone, and
	// GoalMailConditions the mail it takes a turn for.
	GoalTimes                []string               `json:"goalTimes"`
	GoalMailConditions       []MailboxRuleCondition `json:"goalMailConditions"`
	GoalWrittenBy            string                 `json:"goalWrittenBy"`
	IsGoalNotifiedWhenPosted bool                   `json:"isGoalNotifiedWhenPosted"`

	GoalSetAt      *time.Time `json:"goalSetAt"`
	GoalNextAt     *time.Time `json:"goalNextAt"`
	GoalSurfacedAt *time.Time `json:"goalSurfacedAt"`
	LastAt         time.Time  `json:"lastAt"`

	Activity       []*AgentGoalActivity   `json:"activity,omitempty"`
	BackgroundWork []*AgentBackgroundWork `json:"backgroundWork,omitempty"`
	Artifacts      []*AgentGoalArtifact   `json:"artifacts,omitempty"`
}

// AgentGoalActivity is one row of a goal's log.
type AgentGoalActivity struct {
	ID               string    `json:"id"`
	CreatedAt        time.Time `json:"createdAt"`
	GoalActivityKind string    `json:"goalActivityKind"`
	ActivityHeadline string    `json:"activityHeadline"`
	ActivityDetail   string    `json:"activityDetail,omitempty"`
}

// AgentGoalArtifact is a mail rule, a reminder or an alert mute a goal
// made.
type AgentGoalArtifact struct {
	ID                string    `json:"id"`
	CreatedAt         time.Time `json:"createdAt"`
	GoalArtifactKind  string    `json:"goalArtifactKind"`
	ArtifactReference string    `json:"artifactReference"`
	ArtifactTitle     string    `json:"artifactTitle"`
}

const goalFields = `conversationId goalTitle goalDescription goalState goalStatus goalTimes goalMailConditions { field header operator value } goalWrittenBy isGoalNotifiedWhenPosted goalSetAt goalNextAt goalSurfacedAt lastAt`

// The documents the command line and the goal tool send, exported so a
// test can check them against the schema.
const (
	DocumentListAgentGoals = `query ($goalStates: [String!]) {
		ListAgentGoals(goalStates: $goalStates) { ` + goalFields + ` }
	}`
	DocumentGetAgentGoal = `query ($conversationId: String!) {
		GetAgentGoal(conversationId: $conversationId) {
			` + goalFields + `
			activity { id createdAt goalActivityKind activityHeadline activityDetail }
			backgroundWork { ` + backgroundWorkFields + ` }
			artifacts { id createdAt goalArtifactKind artifactReference artifactTitle }
		}
	}`
	DocumentStartAgentGoal = `mutation ($goalTitle: String!, $goalDescription: String!, $originConversationId: String, $goalTimes: [String!], $goalMailConditions: [MailboxRuleConditionInput!], $isGoalNotifiedWhenPosted: Boolean) {
		StartAgentGoal(goalTitle: $goalTitle, goalDescription: $goalDescription, originConversationId: $originConversationId, goalTimes: $goalTimes, goalMailConditions: $goalMailConditions, isGoalNotifiedWhenPosted: $isGoalNotifiedWhenPosted) { ` + goalFields + ` }
	}`
	DocumentUpdateAgentGoal = `mutation ($conversationId: String!, $goalTitle: String, $goalDescription: String, $goalTimes: [String!], $goalMailConditions: [MailboxRuleConditionInput!], $isGoalNotifiedWhenPosted: Boolean) {
		UpdateAgentGoal(conversationId: $conversationId, goalTitle: $goalTitle, goalDescription: $goalDescription, goalTimes: $goalTimes, goalMailConditions: $goalMailConditions, isGoalNotifiedWhenPosted: $isGoalNotifiedWhenPosted) { ` + goalFields + ` }
	}`
	DocumentRunAgentGoal = `mutation ($conversationId: String!) {
		RunAgentGoal(conversationId: $conversationId) { ` + goalFields + ` }
	}`
	DocumentTellAgentGoal = `mutation ($conversationId: String!, $text: String!) {
		TellAgentGoal(conversationId: $conversationId, text: $text) { ` + goalFields + ` }
	}`
	DocumentSetAgentGoalState = `mutation ($conversationId: String!, $goalState: String!) {
		SetAgentGoalState(conversationId: $conversationId, goalState: $goalState) { ` + goalFields + ` }
	}`
)

// ListAgentGoals is the person's goals in the states given, every state
// when none is.
func ListAgentGoals(ctx context.Context, connection *Client, goalStates []string) ([]*AgentGoal, error) {
	var result struct {
		ListAgentGoals []*AgentGoal `json:"ListAgentGoals"`
	}
	variables := map[string]any{}
	if len(goalStates) > 0 {
		variables["goalStates"] = goalStates
	}
	if err := connection.Execute(ctx, DocumentListAgentGoals, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentGoals, nil
}

// GetAgentGoal is one goal with its activity and what it made.
func GetAgentGoal(ctx context.Context, connection *Client, conversationId string) (*AgentGoal, error) {
	var result struct {
		GetAgentGoal *AgentGoal `json:"GetAgentGoal"`
	}
	if err := connection.Execute(ctx, DocumentGetAgentGoal, map[string]any{"conversationId": conversationId}, &result); err != nil {
		return nil, err
	}
	return result.GetAgentGoal, nil
}

// AgentGoalChange is what a goal is started with or changed by: only what is
// not nil is sent, and an empty list of times or mail conditions takes them
// away.
type AgentGoalChange struct {
	GoalTitle                *string
	GoalDescription          *string
	GoalTimes                []string
	GoalMailConditions       []MailboxRuleCondition
	IsGoalNotifiedWhenPosted *bool
}

func (self *AgentGoalChange) variables() map[string]any {
	variables := map[string]any{}
	if self.GoalTitle != nil {
		variables["goalTitle"] = *self.GoalTitle
	}
	if self.GoalDescription != nil {
		variables["goalDescription"] = *self.GoalDescription
	}
	if self.GoalTimes != nil {
		variables["goalTimes"] = self.GoalTimes
	}
	if self.GoalMailConditions != nil {
		variables["goalMailConditions"] = self.GoalMailConditions
	}
	if self.IsGoalNotifiedWhenPosted != nil {
		variables["isGoalNotifiedWhenPosted"] = *self.IsGoalNotifiedWhenPosted
	}
	return variables
}

// StartAgentGoal starts a goal in the background.
func StartAgentGoal(ctx context.Context, connection *Client, change *AgentGoalChange) (*AgentGoal, error) {
	var result struct {
		StartAgentGoal *AgentGoal `json:"StartAgentGoal"`
	}
	variables := change.variables()
	if _, ok := variables["goalTitle"]; !ok {
		variables["goalTitle"] = ""
	}
	if err := connection.Execute(ctx, DocumentStartAgentGoal, variables, &result); err != nil {
		return nil, err
	}
	return result.StartAgentGoal, nil
}

// UpdateAgentGoal changes a goal.
func UpdateAgentGoal(ctx context.Context, connection *Client, conversationId string, change *AgentGoalChange) (*AgentGoal, error) {
	var result struct {
		UpdateAgentGoal *AgentGoal `json:"UpdateAgentGoal"`
	}
	variables := change.variables()
	variables["conversationId"] = conversationId
	if err := connection.Execute(ctx, DocumentUpdateAgentGoal, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateAgentGoal, nil
}

// RunAgentGoal gives a working goal a turn now.
func RunAgentGoal(ctx context.Context, connection *Client, conversationId string) (*AgentGoal, error) {
	var result struct {
		RunAgentGoal *AgentGoal `json:"RunAgentGoal"`
	}
	if err := connection.Execute(ctx, DocumentRunAgentGoal, map[string]any{"conversationId": conversationId}, &result); err != nil {
		return nil, err
	}
	return result.RunAgentGoal, nil
}

// TellAgentGoal passes the person's words to a goal.
func TellAgentGoal(ctx context.Context, connection *Client, conversationId, text string) (*AgentGoal, error) {
	var result struct {
		TellAgentGoal *AgentGoal `json:"TellAgentGoal"`
	}
	if err := connection.Execute(ctx, DocumentTellAgentGoal, map[string]any{"conversationId": conversationId, "text": text}, &result); err != nil {
		return nil, err
	}
	return result.TellAgentGoal, nil
}

// SetAgentGoalState marks a goal met, dropped or paused, or takes it up
// again.
func SetAgentGoalState(ctx context.Context, connection *Client, conversationId, goalState string) (*AgentGoal, error) {
	var result struct {
		SetAgentGoalState *AgentGoal `json:"SetAgentGoalState"`
	}
	if err := connection.Execute(ctx, DocumentSetAgentGoalState, map[string]any{"conversationId": conversationId, "goalState": goalState}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentGoalState, nil
}
