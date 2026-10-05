package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Goals: the things the agent keeps at in the background for the person,
// each in a conversation of its own, across turns of its own, until it is
// met or dropped. The goal tool, the dashboard's Goals tab and `teanode
// agent goal` all go through these, so they list, start and close the same.

// AgentGoalQuery reads goals.
type AgentGoalQuery interface {
	// The caller's agent's goals, the most recently active first: what
	// each is called, where it stands and its one-line status. Every
	// state when goalStates is left out; working, waiting, met and
	// dropped otherwise. Needs agent:use.
	ListAgentGoals(ctx context.Context, arguments ListAgentGoalsArguments) ([]*AgentGoalView, error)

	// One goal with what it is for, what happened on it, newest first,
	// and what it made: schedules, background work, mail rules,
	// reminders and alert mutes. Needs agent:use.
	GetAgentGoal(ctx context.Context, arguments GetAgentGoalArguments) (*AgentGoalView, error)
}

// AgentGoalMutation starts goals, answers them and closes them.
type AgentGoalMutation interface {
	// Start a goal in the background: a title of a few words and a
	// description of what it is for and what done looks like. The agent
	// takes its first turn on it at once, in the goal's own
	// conversation; the person hears from it only when it needs them.
	// Needs agent:use.
	StartAgentGoal(ctx context.Context, arguments StartAgentGoalArguments) (*AgentGoalView, error)

	// Pass the person's words to a goal that is working or waiting: an
	// answer to what it asked, or something it should know. It goes on a
	// minute later. Needs agent:use.
	TellAgentGoal(ctx context.Context, arguments TellAgentGoalArguments) (*AgentGoalView, error)

	// Mark a goal done (met), stop it (dropped), or take it up again
	// (working). Needs agent:use.
	SetAgentGoalState(ctx context.Context, arguments SetAgentGoalStateArguments) (*AgentGoalView, error)
}

type ListAgentGoalsArguments struct {
	GoalStates []string `json:"goalStates" graphapi:"nullable"`
}

// GetAgentGoalArguments name one goal, by its conversation.
type GetAgentGoalArguments struct {
	ConversationID string `json:"conversationId"`
}

type StartAgentGoalArguments struct {
	GoalTitle       string `json:"goalTitle"`
	GoalDescription string `json:"goalDescription"`

	// OriginConversationID is the conversation the goal was asked for in,
	// kept in its first activity row.
	OriginConversationID string `json:"originConversationId" graphapi:"nullable"`
}

type TellAgentGoalArguments struct {
	ConversationID string `json:"conversationId"`
	Text           string `json:"text"`
}

type SetAgentGoalStateArguments struct {
	ConversationID string `json:"conversationId"`
	GoalState      string `json:"goalState"`
}

// AgentGoalView is one goal. Activity and the artifacts are filled only
// by GetAgentGoal.
type AgentGoalView struct {
	// ConversationID is the goal's own conversation, which is also how
	// it is named everywhere.
	ConversationID string `json:"conversationId"`

	GoalTitle       string `json:"goalTitle"`
	GoalDescription string `json:"goalDescription"`

	// GoalState is working, waiting, met or dropped, and GoalStatus the
	// agent's one line on where it stands; while it waits, what it needs.
	GoalState  string `json:"goalState"`
	GoalStatus string `json:"goalStatus"`

	GoalSetAt      *time.Time `json:"goalSetAt" graphapi:"nullable"`
	GoalNextAt     *time.Time `json:"goalNextAt" graphapi:"nullable"`
	GoalSurfacedAt *time.Time `json:"goalSurfacedAt" graphapi:"nullable"`
	LastAt         time.Time  `json:"lastAt"`

	Activity       []*models.AgentGoalActivity `json:"activity"`
	Schedules      []*models.AgentSchedule     `json:"schedules"`
	BackgroundWork []*AgentBackgroundWorkView  `json:"backgroundWork"`
	Artifacts      []*models.AgentGoalArtifact `json:"artifacts"`
}

func (self *graph) ListAgentGoals(ctx context.Context, arguments ListAgentGoalsArguments) ([]*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	var goalStates []models.AgentGoalState
	for _, goalState := range arguments.GoalStates {
		wanted := models.AgentGoalState(strings.ToLower(strings.TrimSpace(goalState)))
		if !models.IsAgentGoalState(wanted) {
			return nil, fmt.Errorf("%w: %q is not working, waiting, met or dropped", api.ErrInvalidArguments, goalState)
		}
		goalStates = append(goalStates, wanted)
	}
	goals, err := self.transaction(ctx).ListAgentGoals(found.ID, goalStates, 200)
	if err != nil {
		return nil, err
	}
	views := make([]*AgentGoalView, 0, len(goals))
	for _, goal := range goals {
		views = append(views, goalView(goal))
	}
	return views, nil
}

func (self *graph) GetAgentGoal(ctx context.Context, arguments GetAgentGoalArguments) (*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.goalWithDetails(ctx, found, arguments.ConversationID)
}

func (self *graph) StartAgentGoal(ctx context.Context, arguments StartAgentGoalArguments) (*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	started, err := worker.StartGoal(ctx, found, arguments.GoalTitle, arguments.GoalDescription, strings.TrimSpace(arguments.OriginConversationID))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return goalView(started), nil
}

func (self *graph) TellAgentGoal(ctx context.Context, arguments TellAgentGoalArguments) (*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	told, err := worker.TellGoal(ctx, found, arguments.ConversationID, arguments.Text)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return goalView(told), nil
}

func (self *graph) SetAgentGoalState(ctx context.Context, arguments SetAgentGoalStateArguments) (*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	changed, err := worker.SetGoalState(ctx, found, arguments.ConversationID, models.AgentGoalState(strings.ToLower(strings.TrimSpace(arguments.GoalState))))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return goalView(changed), nil
}

// goalWithDetails is one of the agent's goals with its activity and what
// it made.
func (self *graph) goalWithDetails(ctx context.Context, found *models.Agent, conversationId string) (*AgentGoalView, error) {
	tx := self.transaction(ctx)
	goal, err := tx.GetAgentConversation(strings.TrimSpace(conversationId))
	if err != nil {
		return nil, err
	}
	if goal == nil || goal.AgentID != found.ID || !goal.IsGoal() || goal.Goal == "" {
		return nil, fmt.Errorf("%w: there is no goal %q", api.ErrNotFound, conversationId)
	}
	view := goalView(goal)
	if view.Activity, err = tx.ListAgentGoalActivity(found.ID, goal.ID, 100); err != nil {
		return nil, err
	}
	if view.Artifacts, err = tx.ListAgentGoalArtifacts(found.ID, goal.ID); err != nil {
		return nil, err
	}
	schedules, err := tx.ListAgentSchedules(found.ID)
	if err != nil {
		return nil, err
	}
	for _, schedule := range schedules {
		if schedule.ConversationID == goal.ID {
			view.Schedules = append(view.Schedules, schedule)
		}
	}
	works, err := tx.ListAgentBackgroundWork(found.ID, backgroundWorkListMost)
	if err != nil {
		return nil, err
	}
	for _, work := range works {
		if work.ConversationID == goal.ID {
			view.BackgroundWork = append(view.BackgroundWork, backgroundWorkView(work))
		}
	}
	return view, nil
}

// goalView is a goal conversation as the API shows it, without details.
func goalView(goal *models.AgentConversation) *AgentGoalView {
	title := strings.TrimSpace(goal.GoalTitle)
	if title == "" {
		title = goal.Title
	}
	return &AgentGoalView{
		ConversationID: goal.ID, GoalTitle: title, GoalDescription: goal.Goal,
		GoalState: string(goal.GoalState), GoalStatus: goal.GoalNote,
		GoalSetAt: goal.GoalSetAt, GoalNextAt: goal.GoalNextAt, GoalSurfacedAt: goal.GoalSurfacedAt, LastAt: goal.LastAt,
		Activity: []*models.AgentGoalActivity{}, Schedules: []*models.AgentSchedule{},
		BackgroundWork: []*AgentBackgroundWorkView{}, Artifacts: []*models.AgentGoalArtifact{},
	}
}
