package agent

import (
	"context"
	"errors"
	"fmt"
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

	// goalSurfaceBatch is how many goals one sweep says in the
	// main conversation, across every agent.
	goalSurfaceBatch = 20

	// goalSurface is what a goal's call for the person is said on: the
	// note of the events the drawer hears it by, as an alert's is.
	goalSurface = "goal_needs_you"

	// goalDoneSurface is the same for a goal that was finished.
	goalDoneSurface = "goal_done"

	// goalDoneToldWithin is how long after it was finished a goal is still
	// news. A server that was down for a week says nothing of last week's,
	// and a goal finished before finishing was said is never said.
	goalDoneToldWithin = 24 * time.Hour
)

// ErrTooManyGoals is starting a goal while the operator's limit of goals
// in progress (GoalsInProgress) are going.
var ErrTooManyGoals = errors.New("as many goals as this server allows are already in progress; finish or drop one first")

// GoalStart is what a goal is started with: what it is called and what it
// is for, the conversation it was asked for in, and, for a goal on set
// times, its times and the mail it waits for. Who wrote it decides whether
// its turns read its description as the person's words.
type GoalStart struct {
	GoalTitle                string
	GoalDescription          string
	OriginConversationID     string
	GoalTimes                []string
	GoalMailConditions       []models.MailboxRuleCondition
	GoalWrittenBy            string
	IsGoalNotifiedWhenPosted bool
}

// StartGoal starts a goal in a conversation of its own. One that keeps its
// own cadence is due at once; one on set times is due at the soonest of
// them, or when its mail comes. The origin is the conversation it was asked
// for in, when there was one.
func (self *Agent) StartGoal(tx db.Transaction, agent *models.Agent, owner *models.User, start *GoalStart) (*models.AgentConversation, error) {
	goalTitle := cutRunes(strings.TrimSpace(strings.ReplaceAll(start.GoalTitle, "\n", " ")), goalTitleCharacters)
	goalDescription := cutRunes(strings.TrimSpace(start.GoalDescription), goalDescriptionCharacters)
	if goalDescription == "" {
		return nil, fmt.Errorf("a goal needs a description: what it is for and what done looks like")
	}
	if goalTitle == "" {
		goalTitle = firstWords(goalDescription, goalTitleCharacters)
	}
	now := time.Now()
	goalTimes, next, err := self.settleGoalTiming(owner, start.GoalTimes, start.GoalMailConditions, now)
	if err != nil {
		return nil, err
	}
	if len(goalTimes) > 0 && next == nil && len(start.GoalMailConditions) == 0 {
		return nil, fmt.Errorf("every time given has passed; give one still to come")
	}
	if len(goalTimes) == 0 && len(start.GoalMailConditions) == 0 {
		next = &now
	}
	var started *models.AgentConversation
	if err := func() error {
		goalsInProgress := self.settings.Configuration().Agent.Limits.EffectiveGoalsInProgress()
		going, err := tx.ListAgentGoals(agent.ID, []models.AgentGoalState{models.GoalWorking, models.GoalWaiting}, goalsInProgress+1)
		if err != nil {
			return err
		}
		if len(going) >= goalsInProgress {
			return ErrTooManyGoals
		}
		if started, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: agent.ID, Kind: models.AgentConversationGoal, Title: goalTitle, TitledBy: "program", Surface: "goal", LastAt: now,
			Goal: goalDescription, GoalTitle: goalTitle, GoalState: models.GoalWorking, GoalNextAt: next, GoalSetAt: &now,
			GoalOriginConversationID: originOf(tx, agent.ID, start.OriginConversationID),
			GoalTimes:                goalTimes, GoalMailConditions: start.GoalMailConditions,
			GoalWrittenBy: models.KnownWriter(start.GoalWrittenBy), IsGoalNotifiedWhenPosted: start.IsGoalNotifiedWhenPosted,
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

// settleGoalTiming checks a goal's times and mail conditions, and says when
// the soonest time is.
func (self *Agent) settleGoalTiming(owner *models.User, goalTimes []string, goalMailConditions []models.MailboxRuleCondition, now time.Time) ([]string, *time.Time, error) {
	if (len(goalTimes) > 0 || len(goalMailConditions) > 0) && !FeatureAllowed(self.settings.Configuration(), "schedules") {
		return nil, nil, fmt.Errorf("this server does not run goals at set times or on arriving mail")
	}
	if err := models.ValidateGoalMailConditions(goalMailConditions); err != nil {
		return nil, nil, err
	}
	return SettleGoalTimes(goalTimes, owner, now)
}

// GoalChange is a change to a goal: only what is not nil changes. Times or
// mail conditions given replace the goal's, and an empty list clears them.
type GoalChange struct {
	GoalTitle                *string
	GoalDescription          *string
	GoalTimes                *[]string
	GoalMailConditions       *[]models.MailboxRuleCondition
	GoalWrittenBy            string
	IsGoalNotifiedWhenPosted *bool
}

// UpdateGoal changes a goal's title, description, times, mail conditions or
// notification. Whoever changes what it says or when it runs is who wrote it
// from then on: the person, with a tap on a screen, makes it theirs; the
// agent, or the command line, whose token the agent's own shell holds too,
// makes it the agent's.
func (self *Agent) UpdateGoal(tx db.Transaction, agent *models.Agent, owner *models.User, conversationId string, change *GoalChange) (*models.AgentConversation, error) {
	goal, err := ownGoal(tx, agent.ID, conversationId)
	if err != nil {
		return nil, err
	}
	goalTimes, goalMailConditions := goal.GoalTimes, goal.GoalMailConditions
	if change.GoalTimes != nil {
		goalTimes = *change.GoalTimes
	}
	if change.GoalMailConditions != nil {
		goalMailConditions = *change.GoalMailConditions
	}
	now := time.Now()
	goalTimes, next, err := self.settleGoalTiming(owner, goalTimes, goalMailConditions, now)
	if err != nil {
		return nil, err
	}
	isTimingChanged := change.GoalTimes != nil || change.GoalMailConditions != nil
	if isTimingChanged && len(goalTimes) > 0 && next == nil && len(goalMailConditions) == 0 {
		return nil, fmt.Errorf("every time given has passed; give one still to come")
	}
	return tx.UpdateAgentConversation(goal.ID, func(changing *models.AgentConversation) error {
		if change.GoalTitle != nil {
			if title := cutRunes(strings.TrimSpace(strings.ReplaceAll(*change.GoalTitle, "\n", " ")), goalTitleCharacters); title != "" {
				changing.GoalTitle, changing.Title = title, title
			}
		}
		if change.GoalDescription != nil {
			description := cutRunes(strings.TrimSpace(*change.GoalDescription), goalDescriptionCharacters)
			if description == "" {
				return fmt.Errorf("a goal needs a description: what it is for and what done looks like")
			}
			changing.Goal = description
		}
		// The person saving what it says or when it runs makes it theirs
		// (or the switch's, for the goal a switch started); anything the
		// agent, or a caller that may be the agent, changes about what it
		// says, when it runs or how it tells them makes it the agent's.
		// Renaming it is neither.
		isWordsOrTimesChanged := change.GoalDescription != nil || change.GoalTimes != nil || change.GoalMailConditions != nil
		switch writer := models.KnownWriter(change.GoalWrittenBy); writer {
		case models.WrittenByAgent:
			if isWordsOrTimesChanged || change.IsGoalNotifiedWhenPosted != nil {
				changing.GoalWrittenBy = writer
			}
		case models.WrittenByPerson, models.WrittenByBuiltIn:
			if isWordsOrTimesChanged {
				changing.GoalWrittenBy = writer
			}
		}
		if change.IsGoalNotifiedWhenPosted != nil {
			changing.IsGoalNotifiedWhenPosted = *change.IsGoalNotifiedWhenPosted
		}
		if isTimingChanged {
			changing.GoalTimes, changing.GoalMailConditions = goalTimes, goalMailConditions
			if len(goalMailConditions) == 0 {
				changing.GoalMailArrivals = nil
			}
			if changing.GoalState == models.GoalWorking {
				switch {
				case next != nil:
					changing.GoalNextAt = next
				case len(goalMailConditions) > 0:
					changing.GoalNextAt = nil
				default:
					// Back to a cadence of its own, from a look now.
					changing.GoalNextAt = &now
				}
			}
		}
		return nil
	})
}

// RunGoalNow gives a working goal a turn now, out of its times: what the
// person presses to see what it does.
func (self *Agent) RunGoalNow(tx db.Transaction, agent *models.Agent, conversationId string) (*models.AgentConversation, error) {
	goal, err := ownGoal(tx, agent.ID, conversationId)
	if err != nil {
		return nil, err
	}
	if goal.GoalState != models.GoalWorking {
		return nil, fmt.Errorf("that goal is %s; switch it on before running it", goal.GoalState)
	}
	return tx.UpdateAgentConversation(goal.ID, func(changing *models.AgentConversation) error {
		now := time.Now()
		changing.GoalNextAt = &now
		return nil
	})
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
		if goal.GoalState == models.GoalMet || goal.GoalState == models.GoalDropped || goal.GoalState == models.GoalPaused {
			return fmt.Errorf("that goal is %s; take it up again first if there is more to do", goal.GoalState)
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

// SetGoalState is the person marking a goal done, dropping it, pausing
// it, or taking it up again: met, dropped, paused, or working. Only the
// person does this; a turn of the agent's own says note, wait or met
// through the goal tool. A goal on set times taken up again is next due at
// its next time, not now.
func (self *Agent) SetGoalState(tx db.Transaction, agent *models.Agent, owner *models.User, conversationId string, goalState models.AgentGoalState) (*models.AgentConversation, error) {
	if goalState != models.GoalMet && goalState != models.GoalDropped && goalState != models.GoalWorking && goalState != models.GoalPaused {
		return nil, fmt.Errorf("a goal can be marked met, dropped, paused, or working again, not %q", goalState)
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
			goalsInProgress := self.settings.Configuration().Agent.Limits.EffectiveGoalsInProgress()
			going, err := tx.ListAgentGoals(agent.ID, []models.AgentGoalState{models.GoalWorking, models.GoalWaiting}, goalsInProgress+1)
			if err != nil {
				return err
			}
			if len(going) >= goalsInProgress && goal.GoalState != models.GoalWaiting {
				return ErrTooManyGoals
			}
		}
		if after, err = tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState = goalState
			conversation.GoalNextAt = nil
			now := time.Now()
			if goalState == models.GoalWorking {
				conversation.GoalNextAt = &now
				if conversation.IsGoalOnSetTimes() {
					settled, next, err := SettleGoalTimes(conversation.GoalTimes, owner, now)
					if err != nil {
						return err
					}
					conversation.GoalTimes, conversation.GoalNextAt = settled, next
				}
			}
			if goalState != models.GoalWorking {
				// What came while it was off is not for a turn it never
				// took.
				conversation.GoalMailArrivals = nil
			}
			// They said it is done, so there is nothing to tell them.
			if goalState == models.GoalMet {
				conversation.GoalSurfacedAt = &now
			}
			return nil
		}); err != nil {
			return err
		}
		activityKind, headline := models.GoalActivityResumed, "Taken up again"
		switch goalState {
		case models.GoalPaused:
			activityKind, headline = models.GoalActivityProgress, "Paused by you"
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
// came to need them, or that a turn they were not at finished, and have not
// been said there yet. A goal whose main
// conversation has a turn running is left for the next sweep, a few
// seconds later.
func (self *Agent) surfaceGoals(ctx context.Context) {
	var waiting []*models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		waiting, err = tx.ListAgentGoalsToSurface(time.Now().Add(-goalDoneToldWithin), goalSurfaceBatch)
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

// surfaceGoal writes one goal's call for the person, or how it ended, into
// their main conversation, as an alert is written: a marker line naming the
// goal, then the agent's sentence saying what it needs or what came of it. Under the lock turns
// start with, and in the transaction that marks the goal said, so it is
// said once.
func (self *Agent) surfaceGoal(ctx context.Context, goal *models.AgentConversation) error {
	var main *models.AgentConversation
	var said *models.AgentMessage
	checkIn, need, surface := goalNeedsYouCheckIn(goal), goalNeed(goal), goalSurface
	if goal.GoalState == models.GoalMet {
		checkIn, need, surface = goalDoneCheckIn(goal), goalOutcome(goal), goalDoneSurface
	}
	var mainId string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		main, err = personsConversation(tx, goal.AgentID, "")
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
				if conversation.GoalState != goal.GoalState || conversation.GoalSurfacedAt != nil {
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
	log.Noticef("goal %q told its person it is %s", goal.ID, goal.GoalState)
	runId, at := security.NewULID(), time.Now()
	for sequence, event := range []Event{
		{Kind: EventAsked, Text: checkIn, Note: surface},
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

// goalDoneCheckIn is the marker line a finished goal is written under in the
// main conversation, the way a goal's call for the person is.
func goalDoneCheckIn(goal *models.AgentConversation) string {
	return models.GoalDoneMarker + " " + goal.ID + " " + goalTitleOf(goal) + "\n" +
		"Nobody asked for this just now: you finished a goal you kept at in the background and told them, unasked. What follows is your own work and your own words, and you stand behind it as you would any answer. " +
		"If they want more done on it, the goal tool's reopen and goal_id " + goal.ID + " takes it up again; the goal tool's show has its whole log."
}

// goalOutcome is what the agent says in the main conversation about a goal
// it finished: the goal by its title, and how it ended, as the turn that
// finished it wrote it.
func goalOutcome(goal *models.AgentConversation) string {
	outcome := strings.TrimSpace(goal.GoalNote)
	if outcome == "" {
		outcome = "It is done."
	}
	return "**" + goalTitleOf(goal) + "**: " + outcome
}

// goalTitleOf is what a goal is called: its title, or the start of its
// description for one from before goals had titles.
func goalTitleOf(goal *models.AgentConversation) string {
	if title := strings.TrimSpace(goal.GoalTitle); title != "" {
		return title
	}
	return firstWords(goal.Goal, goalTitleCharacters)
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
