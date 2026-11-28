package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
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
	// and what it made: background work, mail rules, reminders and alert
	// mutes. Needs agent:use.
	GetAgentGoal(ctx context.Context, arguments GetAgentGoalArguments) (*AgentGoalView, error)
}

// AgentGoalMutation starts goals, answers them and closes them.
type AgentGoalMutation interface {
	// Start a goal in the background: a title of a few words and a
	// description of what it is for and what done looks like, and, for one
	// on set times, the times its turns are at and the mail it waits for.
	// The agent takes its first turn on it at once, or at the soonest
	// time, in the goal's own conversation; the person hears from it only
	// when it needs them, or in their feed. Needs agent:use.
	StartAgentGoal(ctx context.Context, arguments StartAgentGoalArguments) (*AgentGoalView, error)

	// Change a goal's title, description, times, mail conditions or
	// notification; only what is given changes. Needs agent:use.
	UpdateAgentGoal(ctx context.Context, arguments UpdateAgentGoalArguments) (*AgentGoalView, error)

	// Give a working goal a turn now, out of its times. Needs agent:use.
	RunAgentGoal(ctx context.Context, arguments RunAgentGoalArguments) (*AgentGoalView, error)

	// Pass the person's words to a goal that is working or waiting: an
	// answer to what it asked, or something it should know. It goes on a
	// minute later. Needs agent:use.
	TellAgentGoal(ctx context.Context, arguments TellAgentGoalArguments) (*AgentGoalView, error)

	// Mark a goal done (met), stop it (dropped), switch it off for now
	// (paused), or take it up again (working). Needs agent:use.
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
	// which the goal's page links to.
	OriginConversationID string `json:"originConversationId" graphapi:"nullable"`

	// GoalTimes are when its turns are, in the person's zone: cron lines,
	// or "@at" and a moment, or "@in" and a distance from now. None keeps
	// a cadence of the agent's own.
	GoalTimes []string `json:"goalTimes" graphapi:"nullable"`

	// GoalMailConditions make it take a turn when a message arrives that
	// passes every one of them.
	GoalMailConditions []models.MailboxRuleCondition `json:"goalMailConditions" graphapi:"nullable"`

	// IsGoalNotifiedWhenPosted makes the first card of each of its turns
	// send one quiet notification.
	IsGoalNotifiedWhenPosted *bool `json:"isGoalNotifiedWhenPosted" graphapi:"nullable"`
}

// UpdateAgentGoalArguments change a goal: what is left out stays as it is,
// and an empty list of times or mail conditions takes them away.
type UpdateAgentGoalArguments struct {
	ConversationID           string                        `json:"conversationId"`
	GoalTitle                *string                       `json:"goalTitle" graphapi:"nullable"`
	GoalDescription          *string                       `json:"goalDescription" graphapi:"nullable"`
	GoalTimes                []string                      `json:"goalTimes" graphapi:"nullable"`
	GoalMailConditions       []models.MailboxRuleCondition `json:"goalMailConditions" graphapi:"nullable"`
	IsGoalNotifiedWhenPosted *bool                         `json:"isGoalNotifiedWhenPosted" graphapi:"nullable"`
}

// RunAgentGoalArguments name the goal to give a turn now.
type RunAgentGoalArguments struct {
	ConversationID string `json:"conversationId"`
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

	// GoalState is working, waiting, paused, met or dropped, and
	// GoalStatus the agent's one line on where it stands; while it waits,
	// what it needs.
	GoalState  string `json:"goalState"`
	GoalStatus string `json:"goalStatus"`

	// GoalTimes are when its turns are, in the person's zone, and
	// GoalMailConditions the mail it takes a turn for; both empty for a
	// goal that keeps a cadence of its own.
	GoalTimes          []string                      `json:"goalTimes"`
	GoalMailConditions []models.MailboxRuleCondition `json:"goalMailConditions"`

	// GoalWrittenBy is who wrote it: person, agent or built_in.
	GoalWrittenBy            string `json:"goalWrittenBy"`
	IsGoalNotifiedWhenPosted bool   `json:"isGoalNotifiedWhenPosted"`

	// GoalOriginConversationID is the conversation it was asked for in,
	// and GoalOriginTitle what that conversation is called, empty for the
	// main one; filled only by GetAgentGoal.
	GoalOriginConversationID string `json:"goalOriginConversationId"`
	GoalOriginTitle          string `json:"goalOriginTitle"`
	IsGoalOriginMain         bool   `json:"isGoalOriginMain"`

	GoalSetAt      *time.Time `json:"goalSetAt" graphapi:"nullable"`
	GoalNextAt     *time.Time `json:"goalNextAt" graphapi:"nullable"`
	GoalSurfacedAt *time.Time `json:"goalSurfacedAt" graphapi:"nullable"`
	LastAt         time.Time  `json:"lastAt"`

	Activity       []*models.AgentGoalActivity `json:"activity"`
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
			return nil, fmt.Errorf("%w: %q is not working, waiting, paused, met or dropped", api.ErrInvalidArguments, goalState)
		}
		goalStates = append(goalStates, wanted)
	}
	goals, err := self.transaction(ctx).ListAgentGoals(found.ID, goalStates, 0)
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
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	started, err := worker.StartGoal(self.transaction(ctx), found, principal.User, &agent.GoalStart{
		GoalTitle: arguments.GoalTitle, GoalDescription: arguments.GoalDescription, OriginConversationID: arguments.OriginConversationID,
		GoalTimes: arguments.GoalTimes, GoalMailConditions: arguments.GoalMailConditions,
		GoalWrittenBy:            goalWriter(principal),
		IsGoalNotifiedWhenPosted: arguments.IsGoalNotifiedWhenPosted != nil && *arguments.IsGoalNotifiedWhenPosted,
	})
	if err != nil {
		return nil, translateGoalError(err)
	}
	return goalView(started), nil
}

func (self *graph) UpdateAgentGoal(ctx context.Context, arguments UpdateAgentGoalArguments) (*AgentGoalView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	change := &agent.GoalChange{
		GoalTitle: arguments.GoalTitle, GoalDescription: arguments.GoalDescription,
		GoalWrittenBy: goalWriter(principal), IsGoalNotifiedWhenPosted: arguments.IsGoalNotifiedWhenPosted,
	}
	if arguments.GoalTimes != nil {
		change.GoalTimes = &arguments.GoalTimes
	}
	if arguments.GoalMailConditions != nil {
		change.GoalMailConditions = &arguments.GoalMailConditions
	}
	changed, err := worker.UpdateGoal(self.transaction(ctx), found, principal.User, arguments.ConversationID, change)
	if err != nil {
		return nil, translateGoalError(err)
	}
	return goalView(changed), nil
}

func (self *graph) RunAgentGoal(ctx context.Context, arguments RunAgentGoalArguments) (*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	changed, err := worker.RunGoalNow(self.transaction(ctx), found, arguments.ConversationID)
	if err != nil {
		return nil, translateGoalError(err)
	}
	return goalView(changed), nil
}

// goalWriter is who wrote what a goal's operation was given. The goal is the
// person's, and its turns may use Passwords, only when the person tapped
// save on a screen. The command line holds a token the agent's own shell can
// use too, so a goal written from there is recorded as the agent's.
func goalWriter(principal *api.Principal) string {
	if principal.IsPersonsTap() {
		return models.WrittenByPerson
	}
	return models.WrittenByAgent
}

// translateGoalError says a refused goal is the caller's arguments, and a
// missing one is not found.
func translateGoalError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("%w: %s", api.ErrNotFound, err)
	}
	return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
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
	told, err := worker.TellGoal(self.transaction(ctx), found, arguments.ConversationID, arguments.Text)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return goalView(told), nil
}

func (self *graph) SetAgentGoalState(ctx context.Context, arguments SetAgentGoalStateArguments) (*AgentGoalView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	changed, err := worker.SetGoalState(self.transaction(ctx), found, principal.User, arguments.ConversationID, models.AgentGoalState(strings.ToLower(strings.TrimSpace(arguments.GoalState))))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	// Whether it runs, changed by a caller that may be the agent, makes it
	// the agent's, as a change to what it says does.
	if goalWriter(principal) == models.WrittenByAgent && (changed.GoalState == models.GoalWorking || changed.GoalState == models.GoalPaused) &&
		models.KnownWriter(changed.GoalWrittenBy) != models.WrittenByAgent {
		if changed, err = self.transaction(ctx).UpdateAgentConversation(changed.ID, func(goal *models.AgentConversation) error {
			goal.GoalWrittenBy = models.WrittenByAgent
			return nil
		}); err != nil {
			return nil, err
		}
	}
	// Done, dropped or paused stops the turn it may be taking: left
	// running, it would go on with what the person has just closed.
	if changed.GoalState == models.GoalMet || changed.GoalState == models.GoalDropped || changed.GoalState == models.GoalPaused {
		worker.StopConversation(changed.ID)
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
	if goal.GoalOriginConversationID != "" {
		if origin, err := tx.GetAgentConversation(goal.GoalOriginConversationID); err != nil {
			return nil, err
		} else if origin != nil && origin.AgentID == found.ID {
			view.GoalOriginConversationID, view.GoalOriginTitle = origin.ID, origin.Title
			view.IsGoalOriginMain = origin.Kind == models.AgentConversationMain
		}
	}
	if view.Activity, err = tx.ListAgentGoalActivity(found.ID, goal.ID, 100); err != nil {
		return nil, err
	}
	if view.Artifacts, err = tx.ListAgentGoalArtifacts(found.ID, goal.ID); err != nil {
		return nil, err
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
	goalTimes, goalMailConditions := goal.GoalTimes, goal.GoalMailConditions
	if goalTimes == nil {
		goalTimes = []string{}
	}
	if goalMailConditions == nil {
		goalMailConditions = []models.MailboxRuleCondition{}
	}
	return &AgentGoalView{
		ConversationID: goal.ID, GoalTitle: title, GoalDescription: goal.Goal,
		GoalState: string(goal.GoalState), GoalStatus: goal.GoalNote,
		GoalTimes: goalTimes, GoalMailConditions: goalMailConditions,
		GoalWrittenBy: models.KnownWriter(goal.GoalWrittenBy), IsGoalNotifiedWhenPosted: goal.IsGoalNotifiedWhenPosted,
		GoalSetAt: goal.GoalSetAt, GoalNextAt: goal.GoalNextAt, GoalSurfacedAt: goal.GoalSurfacedAt, LastAt: goal.LastAt,
		Activity: []*models.AgentGoalActivity{}, BackgroundWork: []*AgentBackgroundWorkView{}, Artifacts: []*models.AgentGoalArtifact{},
	}
}
