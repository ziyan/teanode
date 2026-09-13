package apigraph

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/models"
)

// The daily brief.
//
// "Every weekday at half past seven, tell me what the day holds and what is
// waiting for an answer, and send it to me" is three fields in a form that a
// person has to think up first. This is the same thing behind one switch.
//
// No new machinery: it writes an ordinary schedule row. That matters twice
// over -- it runs through everything schedules already do, and it appears in
// the Schedules card afterwards, where the prompt can be edited, the time
// changed, or the whole thing thrown away. The switch is a convenience that
// writes a row, and the card says so.

// BriefName is what the schedule the switch writes is called. Found by name,
// because a person who renames it has made it theirs and the switch should
// leave it alone.
const BriefName = "Daily brief"

// SetAgentBriefArguments say when the brief arrives, or that it should not.
type SetAgentBriefArguments struct {
	Enabled bool `json:"enabled"`

	// At is the time of day in the person's own zone, as "HH:MM". Empty
	// keeps the time the schedule already has, or 07:30 for a new one.
	At string `json:"at" graphapi:"nullable"`

	// Days are the days of the week it arrives on, 1 for Monday through 7
	// for Sunday. Empty keeps what it has, or weekdays for a new one.
	Days []int `json:"days" graphapi:"nullable"`
}

// SetAgentBrief turns the daily brief on or off, and says when it comes.
func (self *graph) SetAgentBrief(ctx context.Context, arguments SetAgentBriefArguments) (*models.AgentSchedule, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if !agent.FeatureAllowed(self.config.Current(), "schedules") {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	schedules, err := tx.ListAgentSchedules(found.ID)
	if err != nil {
		return nil, err
	}
	var brief *models.AgentSchedule
	for _, schedule := range schedules {
		if strings.EqualFold(schedule.Name, BriefName) {
			brief = schedule
			break
		}
	}
	cron, err := briefCron(arguments.At, arguments.Days, brief)
	if err != nil {
		return nil, err
	}
	if brief == nil {
		if !arguments.Enabled {
			return nil, nil // off, and there was nothing to turn off
		}
		schedule := &models.AgentSchedule{
			AgentID: found.ID, Name: BriefName, Cron: cron,
			Prompt: agent.BriefPrompt(agent.Language(found, principal.User)),
			// The person's own words, because the person pressed the
			// switch: a schedule the agent writes for itself arrives as a
			// note about what to do rather than as an instruction.
			WrittenBy: models.WrittenByPerson,
			Deliver:   models.AgentDeliverMail, Enabled: true,
		}
		next, err := agent.SettleSchedule(schedule, principal.User, time.Now())
		if err != nil {
			return nil, translateError(err)
		}
		schedule.NextRunAt = &next
		created, err := tx.CreateAgentSchedule(schedule)
		if err != nil {
			return nil, translateError(err)
		}
		log.Noticef("%s turned the daily brief on", operatorName(ctx))
		return created, nil
	}
	updated, err := tx.UpdateAgentSchedule(brief.ID, func(schedule *models.AgentSchedule) error {
		schedule.Cron = cron
		schedule.Enabled = arguments.Enabled
		if strings.TrimSpace(schedule.Prompt) == "" {
			schedule.Prompt = agent.BriefPrompt(agent.Language(found, principal.User))
		}
		next, err := agent.SettleSchedule(schedule, principal.User, time.Now())
		if err != nil {
			return err
		}
		schedule.NextRunAt = &next
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return updated, nil
}

// briefCron is the cron line for a time and a set of days, keeping what the
// schedule already has where the caller said nothing.
func briefCron(at string, days []int, existing *models.AgentSchedule) (string, error) {
	hour, minute := 7, 30
	weekdays := "1-5"
	if existing != nil {
		if parsedHour, parsedMinute, parsedDays, ok := readBriefCron(existing.Cron); ok {
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
func (self *graph) RunAgentBriefNow(ctx context.Context) (*models.AgentSchedule, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	schedules, err := tx.ListAgentSchedules(found.ID)
	if err != nil {
		return nil, err
	}
	for _, schedule := range schedules {
		if !strings.EqualFold(schedule.Name, BriefName) {
			continue
		}
		if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: found.ID, Kind: models.AgentJobSchedule, SubjectID: schedule.ID}); err != nil {
			return nil, err
		}
		return schedule, nil
	}
	return nil, fmt.Errorf("the daily brief is not set up yet")
}
