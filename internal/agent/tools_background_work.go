package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// backgroundWorkListCount is how many pieces of work the tool lists.
const backgroundWorkListCount = 20

// backgroundWorkTool reaches the surveys and subagents the agent started
// in the background, the way the shell tool reads and stops commands. Built
// here rather than registered, as the survey and the subagent are: a stop
// has to reach the cancel of work running on this instance, which only
// the agent holds.
func (self *Agent) backgroundWorkTool() *Tool {
	return &Tool{
		Name:   "background_work",
		Family: FamilyGeneral,
		Risk:   tools.RiskRead,
		// Stopping changes something, if only the agent's own work.
		RiskOf: func(arguments json.RawMessage) tools.Risk {
			var decoded struct {
				Action string `json:"action"`
			}
			_ = json.Unmarshal(arguments, &decoded)
			if decoded.Action == "stop" {
				return tools.RiskWrite
			}
			return tools.RiskRead
		},
		Description: "The surveys and subagents you started in the background: list them, read one with the whole of its result, or stop one still running. " +
			"You are woken when one finishes by itself, with the start of its result; read gives all of it. A stopped one wakes nothing.",
		Parameters: tools.Object(map[string]any{
			"action": tools.EnumProperty("list the newest, read one, or stop one", "list", "read", "stop"),
			"id":     tools.StringProperty("the backgroundWorkId, for read and stop"),
		}, "action"),
		Guidance: "background_work: reads or stops the surveys and subagents you started in the background.",
		Run:      self.runBackgroundWorkTool,
	}
}

func (self *Agent) runBackgroundWorkTool(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := tools.DecodeArguments[struct {
		Action string `json:"action"`
		ID     string `json:"id"`
	}](call)
	if err != nil {
		return nil, err
	}
	run := tools.MustRun(ctx)
	agentId := run.Agent().ID
	workId := strings.TrimSpace(arguments.ID)
	if arguments.Action != "list" && workId == "" {
		return nil, fmt.Errorf("say which: the backgroundWorkId the start answered with")
	}
	switch arguments.Action {
	case "list":
		var works []*models.AgentBackgroundWork
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			works, err = tx.ListAgentBackgroundWork(agentId, backgroundWorkListCount)
			return err
		}); err != nil {
			return nil, err
		}
		listed := make([]map[string]any, 0, len(works))
		for _, work := range works {
			listed = append(listed, backgroundWorkForTool(work, false))
		}
		result, err := tools.JSONResult(map[string]any{"backgroundWork": listed})
		if err != nil {
			return nil, err
		}
		result.Note = fmt.Sprintf("%d", len(listed))
		return result, nil
	case "read":
		var work *models.AgentBackgroundWork
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			work, err = tx.GetAgentBackgroundWork(agentId, workId)
			return err
		}); err != nil {
			return nil, err
		}
		if work == nil {
			return nil, fmt.Errorf("there is no background work %q", workId)
		}
		result, err := tools.JSONResult(backgroundWorkForTool(work, true))
		if err != nil {
			return nil, err
		}
		// What a survey or a subagent answered was made from what it
		// read, and is data, as it is when it wakes the conversation.
		result.Untrusted = true
		result.Note = backgroundWorkOutcome(work)
		return result, nil
	case "stop":
		var work *models.AgentBackgroundWork
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			work, err = self.StopBackgroundWork(tx, agentId, workId)
			return err
		}); err != nil {
			return nil, err
		}
		result, err := tools.JSONResult(backgroundWorkForTool(work, false))
		if err != nil {
			return nil, err
		}
		result.Note = backgroundWorkOutcome(work)
		return result, nil
	}
	return nil, fmt.Errorf("the action is list, read or stop")
}

// backgroundWorkForTool is a piece of work as the tool answers it, with
// its result when asked, under the keys the API uses.
func backgroundWorkForTool(work *models.AgentBackgroundWork, hasResult bool) map[string]any {
	answered := map[string]any{
		"id": work.ID, "workKind": work.WorkKind, "title": work.Title, "workStatus": work.WorkStatus,
		"createdAt": work.CreatedAt,
	}
	if work.FinishedAt != nil {
		answered["finishedAt"] = work.FinishedAt
	}
	if work.ErrorMessage != "" {
		answered["errorMessage"] = work.ErrorMessage
	}
	if hasResult {
		answered["what"] = backgroundWorkWhat(work)
		answered["resultText"] = work.ResultText
		answered["runIds"] = work.RunIDs
	}
	return answered
}
