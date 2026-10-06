package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// A goal is background work with a conversation of its own, of the kind
// goal. Its turns run there, where the person does not read unless they
// open it; what happened is kept as its activity, and what it made as its
// artifacts. The main conversation hears from a goal once each time it
// comes to need the person, and from nowhere else. See
// docs/planning/background-goals-execplan.md.

// The bounds on starting goals.
const (
	// goalTitleCharacters is as long as a goal's title may be: what it is
	// called in a list, a few words.
	goalTitleCharacters = 80

	// goalDescriptionCharacters is as long as its description may be: what
	// it is for and what done looks like, a paragraph.
	goalDescriptionCharacters = 2000

	// goalsInProgressMost is as many goals as may be working or waiting at
	// once. Each costs turns of the person's budget with nobody watching;
	// past this, one has to finish or be dropped before another starts.
	goalsInProgressMost = 20

	// goalSurfaceBatch is how many waiting goals one sweep says in the
	// main conversation, across every agent.
	goalSurfaceBatch = 20

	// goalSurface is what a goal's call for the person is said on: the
	// note of the events the drawer hears it by, as an alert's is.
	goalSurface = "goal_needs_you"
)

// ErrTooManyGoals is starting a goal while goalsInProgressMost are going.
var ErrTooManyGoals = fmt.Errorf("%d goals are already in progress; finish or drop one first", goalsInProgressMost)

// StartGoal starts a goal in a conversation of its own and makes it due at
// once. The title is what it is called in a list, the description what it
// is for. The origin is the conversation it was asked for in, when there
// was one, kept in the first activity row.
func (self *Agent) StartGoal(tx db.Transaction, agent *models.Agent, goalTitle, goalDescription, originConversationId string) (*models.AgentConversation, error) {
	goalTitle = cutRunes(strings.TrimSpace(strings.ReplaceAll(goalTitle, "\n", " ")), goalTitleCharacters)
	goalDescription = cutRunes(strings.TrimSpace(goalDescription), goalDescriptionCharacters)
	if goalDescription == "" {
		return nil, fmt.Errorf("a goal needs a description: what it is for and what done looks like")
	}
	if goalTitle == "" {
		goalTitle = firstWords(goalDescription, goalTitleCharacters)
	}
	var started *models.AgentConversation
	if err := func() error {
		going, err := tx.ListAgentGoals(agent.ID, []models.AgentGoalState{models.GoalWorking, models.GoalWaiting}, goalsInProgressMost+1)
		if err != nil {
			return err
		}
		if len(going) >= goalsInProgressMost {
			return ErrTooManyGoals
		}
		now := time.Now()
		if started, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: agent.ID, Kind: models.AgentConversationGoal, Title: goalTitle, TitledBy: "program", Surface: "goal", LastAt: now,
			Goal: goalDescription, GoalTitle: goalTitle, GoalState: models.GoalWorking, GoalNextAt: &now, GoalSetAt: &now,
			GoalOriginConversationID: originOf(tx, agent.ID, originConversationId),
		}); err != nil {
			return err
		}
		_, err = tx.AddAgentGoalActivity(&models.AgentGoalActivity{
			AgentID: agent.ID, ConversationID: started.ID, GoalActivityKind: models.GoalActivityStarted,
			ActivityHeadline: "Started", ActivityDetail: goalDescription,
		})
		return err
	}(); err != nil {
		return nil, err
	}
	log.Noticef("agent %q started goal %q (%s)", agent.ID, goalTitle, started.ID)
	return started, nil
}

// TellGoal passes what the person said to one of their goals: written into
// the goal's conversation as their words, carried over from where they said
// them, and the goal back at work a minute later. A goal that was met or
// dropped is not started again by this; the person does that.
func (self *Agent) TellGoal(tx db.Transaction, agent *models.Agent, conversationId, text string) (*models.AgentConversation, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("say what to tell the goal")
	}
	var after *models.AgentConversation
	if err := func() error {
		goal, err := ownGoal(tx, agent.ID, conversationId)
		if err != nil {
			return err
		}
		if goal.GoalState == models.GoalMet || goal.GoalState == models.GoalDropped {
			return fmt.Errorf("that goal is %s; open it again first if there is more to do", goal.GoalState)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{
			ConversationID: goal.ID, Role: "user", Content: models.GoalRelayMarker + " " + text,
		}); err != nil {
			return err
		}
		next := time.Now().Add(goalAfterPerson)
		if after, err = tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState, conversation.GoalNextAt, conversation.LastAt = models.GoalWorking, &next, time.Now()
			return nil
		}); err != nil {
			return err
		}
		_, err = tx.AddAgentGoalActivity(&models.AgentGoalActivity{
			AgentID: agent.ID, ConversationID: goal.ID, GoalActivityKind: models.GoalActivityResumed,
			ActivityHeadline: "You answered", ActivityDetail: text,
		})
		return err
	}(); err != nil {
		return nil, err
	}
	return after, nil
}

// SetGoalState is the person marking a goal done, dropping it, or taking
// it up again: met, dropped, or working. Only the person does this; a turn
// of the agent's own says note, wait or met through the goal tool.
func (self *Agent) SetGoalState(tx db.Transaction, agent *models.Agent, conversationId string, goalState models.AgentGoalState) (*models.AgentConversation, error) {
	if goalState != models.GoalMet && goalState != models.GoalDropped && goalState != models.GoalWorking {
		return nil, fmt.Errorf("a goal can be marked met, dropped, or working again, not %q", goalState)
	}
	var after *models.AgentConversation
	if err := func() error {
		goal, err := ownGoal(tx, agent.ID, conversationId)
		if err != nil {
			return err
		}
		if goal.GoalState == goalState {
			after = goal
			return nil
		}
		if goalState == models.GoalWorking {
			going, err := tx.ListAgentGoals(agent.ID, []models.AgentGoalState{models.GoalWorking, models.GoalWaiting}, goalsInProgressMost+1)
			if err != nil {
				return err
			}
			if len(going) >= goalsInProgressMost && goal.GoalState != models.GoalWaiting {
				return ErrTooManyGoals
			}
		}
		if after, err = tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState = goalState
			conversation.GoalNextAt = nil
			if goalState == models.GoalWorking {
				now := time.Now()
				conversation.GoalNextAt = &now
			}
			return nil
		}); err != nil {
			return err
		}
		activityKind, headline := models.GoalActivityResumed, "Taken up again"
		switch goalState {
		case models.GoalMet:
			activityKind, headline = models.GoalActivityMet, "Marked done by you"
		case models.GoalDropped:
			activityKind, headline = models.GoalActivityDropped, "Dropped by you"
		}
		_, err = tx.AddAgentGoalActivity(&models.AgentGoalActivity{
			AgentID: agent.ID, ConversationID: goal.ID, GoalActivityKind: activityKind, ActivityHeadline: headline,
		})
		return err
	}(); err != nil {
		return nil, err
	}
	return after, nil
}

// ownGoal is one of the agent's goal conversations, or an error saying
// there is none by that id.
func ownGoal(tx db.Transaction, agentId, conversationId string) (*models.AgentConversation, error) {
	goal, err := tx.GetAgentConversation(strings.TrimSpace(conversationId))
	if err != nil {
		return nil, err
	}
	if goal == nil || goal.AgentID != agentId || !goal.IsGoal() || goal.Goal == "" {
		return nil, fmt.Errorf("there is no goal %q", conversationId)
	}
	return goal, nil
}

// addGoalActivity writes a row of a goal's log, for a goal conversation
// only.
func addGoalActivity(tx db.Transaction, goal *models.AgentConversation, activityKind models.AgentGoalActivityKind, headline, detail string) error {
	if !goal.IsGoal() {
		return nil
	}
	_, err := tx.AddAgentGoalActivity(&models.AgentGoalActivity{
		AgentID: goal.AgentID, ConversationID: goal.ID, GoalActivityKind: activityKind,
		ActivityHeadline: headline, ActivityDetail: detail,
	})
	return err
}

// surfaceGoals says, in each person's main conversation, the goals that
// came to need them and have not been said there yet. A goal whose main
// conversation has a turn running is left for the next sweep, a few
// seconds later.
func (self *Agent) surfaceGoals(ctx context.Context) {
	var waiting []*models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		waiting, err = tx.ListAgentGoalsToSurface(goalSurfaceBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the goals that need their person: %s", err)
		return
	}
	for _, goal := range waiting {
		if ctx.Err() != nil {
			return
		}
		if err := self.surfaceGoal(ctx, goal); err != nil && !errors.Is(err, errTurnRunning) {
			log.Warningf("cannot say in the main conversation that goal %q needs its person: %s", goal.ID, err)
		}
	}
}

// surfaceGoal writes one goal's call for the person into their main
// conversation, as an alert is written: a marker line naming the goal,
// then the agent's sentence saying what it needs. Under the lock turns
// start with, and in the transaction that marks the goal said, so it is
// said once.
func (self *Agent) surfaceGoal(ctx context.Context, goal *models.AgentConversation) error {
	var main *models.AgentConversation
	var said *models.AgentMessage
	checkIn, need := goalNeedsYouCheckIn(goal), goalNeed(goal)
	var mainId string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		main, err = scheduleConversation(tx, goal.AgentID, "")
		return err
	}); err != nil {
		return err
	}
	mainId = main.ID
	isWritten, err := self.whileNoTurnRuns(mainId, func() error {
		return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			// Checked and marked under the row's lock, so a sweep on another
			// instance that found the same goal waits here and then finds
			// it said.
			isToSay := false
			now := time.Now()
			if _, err := tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
				if conversation.GoalState != models.GoalWaiting || conversation.GoalSurfacedAt != nil {
					return nil
				}
				isToSay, conversation.GoalSurfacedAt = true, &now
				return nil
			}); err != nil || !isToSay {
				return err
			}
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: mainId, Role: "user", Content: checkIn}); err != nil {
				return err
			}
			var err error
			said, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: mainId, Role: "assistant", Content: need})
			return err
		})
	})
	if err != nil {
		return err
	}
	if !isWritten {
		return errTurnRunning
	}
	if said == nil {
		return nil
	}
	log.Noticef("goal %q told its person it needs them", goal.ID)
	runId, at := security.NewULID(), time.Now()
	for sequence, event := range []Event{
		{Kind: EventAsked, Text: checkIn, Note: goalSurface},
		{Kind: EventMessage, Text: need},
		{Kind: EventDone},
	} {
		event.RunID, event.ConversationID, event.Sequence, event.At = runId, mainId, sequence, at
		self.publish(event, true)
	}
	return nil
}

// goalNeedsYouCheckIn is the marker line a goal's call is written under in
// the main conversation. The goal's id follows the marker, so the drawer
// can link to it and the next turn can pass the person's answer on.
func goalNeedsYouCheckIn(goal *models.AgentConversation) string {
	return models.GoalNeedsYouMarker + " " + goal.ID + " " + goalTitleOf(goal) + "\n" +
		"Nobody asked for this: a goal you keep at in the background came to need the person, and you told them, unasked. What you said follows. " +
		"If they answer, pass their words on with the goal tool's tell and goal_id " + goal.ID + "; if they say it is done or to stop, the tool's done or drop."
}

// goalNeed is what the agent says in the main conversation about a goal
// that needs the person: the goal by its title, and the status line the
// turn that waited wrote, which says what it needs.
func goalNeed(goal *models.AgentConversation) string {
	need := strings.TrimSpace(goal.GoalNote)
	if need == "" {
		need = "It needs you before it can go on."
	}
	return "**" + goalTitleOf(goal) + "**: " + need
}

// goalTitleOf is what a goal is called: its title, or the start of its
// description for one from before goals had titles.
func goalTitleOf(goal *models.AgentConversation) string {
	if title := strings.TrimSpace(goal.GoalTitle); title != "" {
		return title
	}
	return firstWords(goal.Goal, goalTitleCharacters)
}

// goalSchedules is the enabled schedules that belong to a goal: those made
// in its conversation or moved there. While it has one, the schedule is the
// goal's clock and the goal takes no turns of its own.
func goalSchedules(tx db.Transaction, goal *models.AgentConversation) ([]*models.AgentSchedule, error) {
	schedules, err := tx.ListAgentSchedules(goal.AgentID)
	if err != nil {
		return nil, err
	}
	var owned []*models.AgentSchedule
	for _, schedule := range schedules {
		// Only one that answers in the goal's conversation: a mailed one
		// runs in a conversation of its own and never says where the
		// goal stands.
		if schedule.ConversationID == goal.ID && schedule.Enabled && schedule.Deliver != models.AgentDeliverMail {
			owned = append(owned, schedule)
		}
	}
	return owned, nil
}

// MoveScheduleToGoal makes an existing schedule one of a goal's: its runs
// take place in the goal's conversation, as the goal's turns, and the goal
// stops taking turns of its own.
func (self *Agent) MoveScheduleToGoal(tx db.Transaction, agent *models.Agent, scheduleId, conversationId string) (*models.AgentSchedule, error) {
	var moved *models.AgentSchedule
	if err := func() error {
		goal, err := ownGoal(tx, agent.ID, conversationId)
		if err != nil {
			return err
		}
		schedule, err := tx.GetAgentSchedule(strings.TrimSpace(scheduleId))
		if err != nil {
			return err
		}
		if schedule == nil || schedule.AgentID != agent.ID {
			return fmt.Errorf("there is no schedule %q", scheduleId)
		}
		if moved, err = tx.UpdateAgentSchedule(schedule.ID, func(changing *models.AgentSchedule) error {
			// Its answer is read where the goal's turns run; a mailed
			// schedule would mail the person every run instead.
			changing.ConversationID, changing.Deliver = goal.ID, models.AgentDeliverDrawer
			return nil
		}); err != nil {
			return err
		}
		if _, err := tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
			if conversation.GoalState == models.GoalWorking {
				conversation.GoalNextAt = nil
			}
			return nil
		}); err != nil {
			return err
		}
		return addGoalActivity(tx, goal, models.GoalActivityProgress, "Runs on the schedule "+strconv.Quote(schedule.Name), schedule.Cron)
	}(); err != nil {
		return nil, err
	}
	return moved, nil
}

// wakeGoalsWithoutSchedule gives a turn to every working goal that has no
// next turn of its own and no schedule left to run it: its schedule was
// switched off or removed, and without this it would wait for ever.
func (self *Agent) wakeGoalsWithoutSchedule(ctx context.Context, now time.Time) error {
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		idle, err := tx.ListAgentGoalsWithoutNextTurn(goalSurfaceBatch)
		if err != nil {
			return err
		}
		for _, goal := range idle {
			schedules, err := goalSchedules(tx, goal)
			if err != nil {
				return err
			}
			if len(schedules) > 0 {
				continue
			}
			if _, err := tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
				if conversation.GoalState == models.GoalWorking && conversation.GoalNextAt == nil {
					conversation.GoalNextAt = &now
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// originOf is the conversation a goal was asked for in, when it is one of
// the agent's that the person chats in; empty otherwise.
func originOf(tx db.Transaction, agentId, conversationId string) string {
	conversationId = strings.TrimSpace(conversationId)
	if conversationId == "" {
		return ""
	}
	found, err := tx.GetAgentConversation(conversationId)
	if err != nil || found == nil || found.AgentID != agentId ||
		(found.Kind != models.AgentConversationMain && found.Kind != models.AgentConversationNamed) {
		return ""
	}
	return found.ID
}
