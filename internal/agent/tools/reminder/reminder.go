// Package reminder is the person's reminders list, as the agent keeps it for
// them: the list beside their calendar that their phone's Reminders app
// syncs. Not the todo tool, which is the agent's own task list in a
// conversation. Every action calls the operation the calendar page and the
// command line's teanode reminder call, so the three agree.
package reminder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "reminder", Family: tools.FamilyGeneral, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "The person's reminders: the list beside their calendar that their phone's Reminders app syncs. " +
					"`list` shows the ones not done, by when they are due (done: true for the done ones). " +
					"`add` makes one: a title, and when it is due if they said. " +
					"`edit` changes one; `done` ticks one off (one that repeats moves on to its next day instead) and `reopen` puts it back; `remove` takes it away. " +
					"Not for your own steps in this conversation, which are the todo tool's.",
				Parameters: tools.Object(map[string]any{
					"action":      tools.EnumProperty("what to do", "list", "add", "edit", "done", "reopen", "remove"),
					"reminder_id": tools.StringProperty("for edit, done, reopen and remove: which reminder, by the id list gives"),
					"title":       tools.StringProperty("for add and edit: what to be reminded of, in a few words"),
					"notes":       tools.StringProperty("for add and edit: anything else worth writing down"),
					"due":         tools.StringProperty("for add and edit: when it is due, a day as 2026-09-29 or a time as 2026-09-29T15:00 in their zone; empty for none"),
					"no_due":      tools.BooleanProperty("for edit: due at no time any more"),
					"priority":    tools.IntegerProperty("for add and edit: 1 the highest to 9 the lowest, 0 for none"),
					"done":        tools.BooleanProperty("for list: the done ones instead"),
				}, "action"),
				Guidance: "reminder: when the person asks to be reminded of something, add it to their reminders list, so their phone has it; ask for the day only when it matters and they did not say. To tell them what is on the list, list it.",
				Preview: tools.PreviewOf(func(call arguments) string {
					switch strings.TrimSpace(call.Action) {
					case "add":
						return "Add a reminder: " + strings.TrimSpace(call.Title)
					case "edit":
						return "Change a reminder"
					case "done":
						return "Tick off a reminder"
					case "reopen":
						return "Put a reminder back"
					case "remove":
						return "Remove a reminder"
					}
					return "List the reminders"
				}),
				Run: run,
			},
		}
	})
}

type arguments struct {
	Action       string `json:"action"`
	ReminderID   string `json:"reminder_id"`
	Title        string `json:"title"`
	Notes        string `json:"notes"`
	Due          string `json:"due"`
	IsDueCleared bool   `json:"no_due"`
	Priority     *int   `json:"priority"`
	IsDone       bool   `json:"done"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireGranted(ctx); err != nil {
		return nil, err
	}
	reminderId := strings.TrimSpace(asked.ReminderID)
	switch action := strings.ToLower(strings.TrimSpace(asked.Action)); action {
	case "", "list":
		result, err := operator.Execute(ctx, client.DocumentListReminders, map[string]any{"isDone": asked.IsDone})
		if err != nil {
			return nil, err
		}
		return tools.JSONResult(map[string]any{"reminders": result["ListReminders"]})
	case "add", "edit":
		if action == "add" && strings.TrimSpace(asked.Title) == "" {
			return nil, fmt.Errorf("add needs a title")
		}
		if action == "edit" && reminderId == "" {
			return nil, fmt.Errorf("edit needs reminder_id")
		}
		variables := map[string]any{"isDueCleared": asked.IsDueCleared}
		if reminderId != "" {
			variables["reminderId"] = reminderId
		}
		if title := strings.TrimSpace(asked.Title); title != "" {
			variables["title"] = title
		}
		if notes := strings.TrimSpace(asked.Notes); notes != "" {
			variables["notes"] = notes
		}
		if asked.Priority != nil {
			variables["priority"] = *asked.Priority
		}
		if due := strings.TrimSpace(asked.Due); due != "" {
			if _, err := time.Parse("2006-01-02", due); err == nil {
				variables["dueOn"] = due
			} else {
				at, err := tools.ParseTime(due, tools.Location(current.Owner()), time.Now())
				if err != nil {
					return nil, fmt.Errorf("due %q is not a day or a time: %w", due, err)
				}
				variables["dueAt"] = at.Format(time.RFC3339)
			}
		}
		result, err := operator.Execute(ctx, client.DocumentSaveReminder, variables)
		if err != nil {
			return nil, err
		}
		answer, err := tools.JSONResult(result["SaveReminder"])
		if err != nil {
			return nil, err
		}
		answer.Note = "on their reminders list, and on their phone at its next sync"
		return answer, nil
	case "done", "reopen":
		if reminderId == "" {
			return nil, fmt.Errorf("%s needs reminder_id", action)
		}
		result, err := operator.Execute(ctx, client.DocumentSetReminderDone, map[string]any{"reminderId": reminderId, "isDone": action == "done"})
		if err != nil {
			return nil, err
		}
		return tools.JSONResult(result["SetReminderDone"])
	case "remove":
		if reminderId == "" {
			return nil, fmt.Errorf("remove needs reminder_id")
		}
		if _, err := operator.Execute(ctx, client.DocumentDeleteReminder, map[string]any{"reminderId": reminderId}); err != nil {
			return nil, err
		}
		return tools.TextResult("removed the reminder"), nil
	default:
		return nil, fmt.Errorf("%q is not list, add, edit, done, reopen or remove", action)
	}
}

// requireGranted refuses when the person has not given their agent the reminders
// list, as the calendar tool refuses without the calendar: nothing from a
// source the person has not granted is the agent's to read or change.
func requireGranted(ctx context.Context) error {
	result, err := operator.Execute(ctx, client.DocumentListCalendars, nil)
	if err != nil {
		return err
	}
	listed, _ := result["ListCalendars"].([]any)
	for _, each := range listed {
		calendar, _ := each.(map[string]any)
		if calendar["calendarKind"] == string(models.CalendarReminders) {
			if granted, _ := calendar["agentGranted"].(bool); granted {
				return nil
			}
		}
	}
	return fmt.Errorf("they have not given you their reminders; it is a switch on their agent's page, beside the calendar")
}
