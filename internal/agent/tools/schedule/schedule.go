// Package schedule is the tool the agent keeps its own times with: a
// prompt run on a cron line, at one moment, or a distance from now, in the
// person's zone, with nobody present.
package schedule

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "schedule", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "Things you do on your own at set times, in the person's zone: add one (a name, a cron line or a single moment, what to do, and whether the answer goes by mail or into the conversation), list, change, remove, or run one now. A reminder for later is a schedule at one moment; \"check this in five minutes\" is @in 5m with the item's id in the prompt. A scheduled run can confirm nothing, so it does only what needs no confirmation.",
				Parameters: tools.Object(map[string]any{
					"action":  tools.EnumProperty("what to do", "add", "list", "update", "remove", "run"),
					"id":      tools.StringProperty("for update, remove and run: the schedule"),
					"name":    tools.StringProperty("what it is called"),
					"cron":    tools.StringProperty("five fields: minute hour day month weekday, such as 0 8 * * 1-5; or once, at a moment in the person's zone: @at 2026-09-12 09:00; or once, from now: @in 5m, @in 2h, @in 1 day"),
					"prompt":  tools.StringProperty("what to do, as the person would say it"),
					"deliver": tools.EnumProperty("where the answer goes", "drawer", "mail"),
					"enabled": tools.BooleanProperty("on"),
				}, "action"),
				Run: runScheduleTool,
			},
		}
	})
}

type scheduleArguments struct {
	Action     string `json:"action"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Cron       string `json:"cron"`
	Prompt     string `json:"prompt"`
	Deliver    string `json:"deliver"`
	Enabled    *bool  `json:"enabled"`
	ScheduleID string `json:"schedule_id"`
}

func runScheduleTool(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[scheduleArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	if !tools.FeatureAllowed(run.Configuration(), "schedules") {
		return nil, fmt.Errorf("schedules are off on this server")
	}
	database := run.Database()
	agentId := run.Agent().ID
	location := tools.Location(run.Owner())
	describe := func(schedule *models.AgentSchedule) map[string]any {
		row := map[string]any{"id": schedule.ID, "name": schedule.Name, "cron": schedule.Cron, "prompt": schedule.Prompt, "deliver": schedule.Deliver, "enabled": schedule.Enabled}
		if schedule.NextRunAt != nil {
			row["next_run"] = schedule.NextRunAt.In(location).Format("2006-01-02 15:04")
		}
		return row
	}
	id := arguments.ID
	if id == "" {
		id = arguments.ScheduleID
	}
	own := func(tx db.Transaction) (*models.AgentSchedule, error) {
		schedule, err := tx.GetAgentSchedule(id)
		if err != nil {
			return nil, err
		}
		if schedule == nil || schedule.AgentID != agentId {
			return nil, fmt.Errorf("there is no schedule %q", id)
		}
		return schedule, nil
	}
	switch arguments.Action {
	case "add":
		cron, err := tools.ResolveRelative(arguments.Cron, time.Now(), location)
		if err != nil {
			return nil, err
		}
		arguments.Cron = cron
		next, err := tools.NextCron(arguments.Cron, time.Now(), location)
		if err != nil {
			return nil, err
		}
		var created *models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: agentId, Name: arguments.Name, Cron: arguments.Cron, Prompt: arguments.Prompt, Deliver: arguments.Deliver, Enabled: arguments.Enabled == nil || *arguments.Enabled, NextRunAt: &next})
			return err
		}); err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(describe(created))
		if err != nil {
			return nil, err
		}
		result.Note = "scheduled " + created.Name
		return result, nil
	case "list":
		var schedules []*models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			schedules, err = tx.ListAgentSchedules(agentId)
			return err
		}); err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(schedules))
		for _, schedule := range schedules {
			rows = append(rows, describe(schedule))
		}
		return tools.JSONResult(map[string]any{"schedules": rows})
	case "update":
		var updated *models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			if _, err := own(tx); err != nil {
				return err
			}
			var err error
			updated, err = tx.UpdateAgentSchedule(id, func(schedule *models.AgentSchedule) error {
				if arguments.Name != "" {
					schedule.Name = arguments.Name
				}
				if arguments.Prompt != "" {
					schedule.Prompt = arguments.Prompt
				}
				if arguments.Deliver != "" {
					schedule.Deliver = arguments.Deliver
				}
				if arguments.Enabled != nil {
					schedule.Enabled = *arguments.Enabled
				}
				if arguments.Cron != "" {
					cron, err := tools.ResolveRelative(arguments.Cron, time.Now(), location)
					if err != nil {
						return err
					}
					schedule.Cron = cron
				}
				if arguments.Cron != "" || (arguments.Enabled != nil && *arguments.Enabled) {
					next, err := tools.NextCron(schedule.Cron, time.Now(), location)
					if err != nil {
						return err
					}
					schedule.NextRunAt = &next
				}
				return nil
			})
			return err
		}); err != nil {
			return nil, err
		}
		return tools.JSONResult(describe(updated))
	case "remove":
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			if _, err := own(tx); err != nil {
				return err
			}
			return tx.DeleteAgentSchedule(id)
		}); err != nil {
			return nil, err
		}
		return tools.TextResult("removed the schedule"), nil
	case "run":
		var schedule *models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			if schedule, err = own(tx); err != nil {
				return err
			}
			err = run.Enqueue(tx, models.AgentJobSchedule, "", schedule.ID)
			return err
		}); err != nil {
			return nil, err
		}
		return tools.TextResult("queued a run of %q; the answer arrives where the schedule says", schedule.Name), nil
	}
	return nil, fmt.Errorf("%q is not an action of schedule", arguments.Action)
}
