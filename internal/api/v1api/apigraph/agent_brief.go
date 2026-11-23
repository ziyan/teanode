package apigraph

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The daily brief.
//
// "Every weekday at half past seven, tell me what the day holds and what is
// waiting for an answer" is a goal with a time that a person has to think up
// first. This is the same thing behind one switch.
//
// No new machinery: it starts an ordinary goal. That matters twice over --
// it runs through everything goals on set times already do, and it appears
// on the Goals screens afterwards, where its words can be edited, the time
// changed, or the whole thing dropped. The switch is a convenience that
// starts a goal, and the screens say so.

// BriefName is what the goal the switch starts is called. Found by title,
// because a person who renames it has made it theirs and the switch should
// leave it alone.
const BriefName = agent.BriefGoalTitle

// SetAgentBriefArguments say when the brief arrives, or that it should not.
type SetAgentBriefArguments struct {
	Enabled bool `json:"enabled"`

	// At is the time of day in the person's own zone, as "HH:MM". Empty
	// keeps the time the goal already has, or 07:30 for a new one.
	At string `json:"at" graphapi:"nullable"`

	// Days are the days of the week it arrives on, 1 for Monday through 7
	// for Sunday. Empty keeps what it has, or weekdays for a new one.
	Days []int `json:"days" graphapi:"nullable"`
}

// SetAgentBrief turns the daily brief on or off, and says when it comes.
func (self *graph) SetAgentBrief(ctx context.Context, arguments SetAgentBriefArguments) (*AgentGoalView, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if !agent.FeatureAllowed(self.config.Current(), "schedules") {
		return nil, agent.ErrUnavailable
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	brief, err := findGoalByTitle(tx, found.ID, BriefName)
	if err != nil {
		return nil, err
	}
	cron, err := briefCron(arguments.At, arguments.Days, brief)
	if err != nil {
		return nil, translateError(err)
	}
	if brief == nil {
		if !arguments.Enabled {
			return nil, nil // off, and there was nothing to turn off
		}
		started, err := worker.StartGoal(tx, found, principal.User, &agent.GoalStart{
			GoalTitle: BriefName, GoalDescription: agent.BriefPrompt(found, principal.User), GoalTimes: []string{cron},
			// The person's own words, because the person pressed the switch:
			// a goal the agent writes for itself arrives as a note about what
			// to do rather than as an instruction. Recorded as built in, not
			// as the person's: the brief reads the inbox, so a turn it wakes
			// does not count as the person asking for Passwords.
			GoalWrittenBy:            models.WrittenByBuiltIn,
			IsGoalNotifiedWhenPosted: true,
		})
		if err != nil {
			return nil, translateGoalError(err)
		}
		log.Noticef("%s turned the daily brief on", operatorName(ctx))
		return goalView(started), nil
	}
	times := []string{cron}
	// The brief runs at its hour, whatever it was changed to before.
	noMail := []models.MailboxRuleCondition{}
	if _, err := worker.UpdateGoal(tx, found, principal.User, brief.ID, &agent.GoalChange{GoalTimes: &times, GoalMailConditions: &noMail, GoalWrittenBy: models.WrittenByBuiltIn}); err != nil {
		return nil, translateGoalError(err)
	}
	goalState := models.GoalPaused
	if arguments.Enabled {
		goalState = models.GoalWorking
	}
	changed, err := worker.SetGoalState(tx, found, principal.User, brief.ID, goalState)
	if err != nil {
		return nil, translateGoalError(err)
	}
	return goalView(changed), nil
}

// findGoalByTitle is the agent's goal of that title that is still kept at,
// working, waiting or paused, or nil. Only a goal its switch or the person
// wrote counts: a goal the agent gave the same title stays the agent's,
// since a switch adopting it would make the agent's words read as the
// person's.
func findGoalByTitle(tx db.Transaction, agentId, goalTitle string) (*models.AgentConversation, error) {
	goals, err := tx.ListAgentGoals(agentId, []models.AgentGoalState{models.GoalWorking, models.GoalWaiting, models.GoalPaused}, 0)
	if err != nil {
		return nil, err
	}
	for _, goal := range goals {
		if goal.GoalWrittenBy != models.WrittenByBuiltIn && goal.GoalWrittenBy != models.WrittenByPerson {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(goal.GoalTitle), goalTitle) {
			return goal, nil
		}
	}
	return nil, nil
}

// briefCron is the cron line for a time and a set of days, keeping what the
// goal already has where the caller said nothing.
func briefCron(at string, days []int, existing *models.AgentConversation) (string, error) {
	hour, minute := 7, 30
	weekdays := "1-5"
	if existing != nil && len(existing.GoalTimes) > 0 {
		if parsedHour, parsedMinute, parsedDays, ok := readBriefCron(existing.GoalTimes[0]); ok {
			hour, minute, weekdays = parsedHour, parsedMinute, parsedDays
		}
	}
	if trimmed := strings.TrimSpace(at); trimmed != "" {
		parts := strings.Split(trimmed, ":")
		if len(parts) != 2 {
			return "", fmt.Errorf("a time is HH:MM, not %q", at)
		}
		parsedHour, err := strconv.Atoi(parts[0])
		if err != nil || parsedHour < 0 || parsedHour > 23 {
			return "", fmt.Errorf("%q is not an hour", at)
		}
		parsedMinute, err := strconv.Atoi(parts[1])
		if err != nil || parsedMinute < 0 || parsedMinute > 59 {
			return "", fmt.Errorf("%q is not a time", at)
		}
		hour, minute = parsedHour, parsedMinute
	}
	if len(days) > 0 {
		written := make([]string, 0, len(days))
		for _, day := range days {
			if day < 1 || day > 7 {
				return "", fmt.Errorf("a day is 1 (Monday) to 7 (Sunday), not %d", day)
			}
			// Cron counts Sunday as 0; a person counts it as the seventh.
			written = append(written, strconv.Itoa(day%7))
		}
		weekdays = strings.Join(written, ",")
	}
	return fmt.Sprintf("%d %d * * %s", minute, hour, weekdays), nil
}

// readBriefCron reads back what briefCron wrote, so that changing the time
// keeps the days and changing the days keeps the time.
func readBriefCron(cron string) (hour, minute int, days string, ok bool) {
	fields := strings.Fields(cron)
	if len(fields) != 5 {
		return 0, 0, "", false
	}
	minute, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, "", false
	}
	hour, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, "", false
	}
	return hour, minute, fields[4], true
}

// RunAgentBriefNow sends the brief immediately, for somebody who has just set
// it up and would like to see what arrives.
func (self *graph) RunAgentBriefNow(ctx context.Context) (*AgentGoalView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	brief, err := findGoalByTitle(tx, found.ID, BriefName)
	if err != nil {
		return nil, err
	}
	if brief == nil {
		return nil, fmt.Errorf("%w: the daily brief is not set up yet", api.ErrInvalidArguments)
	}
	ran, err := worker.RunGoalNow(tx, found, brief.ID)
	if err != nil {
		return nil, translateGoalError(err)
	}
	return goalView(ran), nil
}
