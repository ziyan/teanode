// Package todo is a conversation's task list: the agent's own, laid out
// when a piece of work has several steps and kept as it goes, so the agent
// keeps its place and the person watches its progress.
package todo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "todo", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "This conversation's task list: yours, for work in several steps, to keep your place and show the person your progress; they can see it but not change it. `batch` makes every change in one call -- add the steps when you start, complete each as you finish it, update or delete one that changed; `list` shows it, `prune` clears the finished ones. Shown to you every round.",
				Parameters: tools.Object(map[string]any{
					"action": tools.EnumProperty("what to do", "batch", "list", "prune"),
					"items": map[string]any{
						"type": "array", "minItems": 1, "maxItems": todoBatchMostChangeCount,
						"description": "for batch: the changes, in order",
						"items": tools.Object(map[string]any{
							"op":   tools.EnumProperty("the change", "add", "update", "complete", "reopen", "delete"),
							"id":   tools.StringProperty("for update, complete, reopen and delete: the item"),
							"text": tools.StringProperty("for add and update: the step, short, in one line"),
						}, "op"),
					},
				}, "action"),
				Guidance: "todo: for work of three steps or more -- research, a change in several places, anything that takes more than a couple of tool calls -- put the steps on the list in one batch before you start, a short line each; complete each in the batch that follows its work, add or update steps when the plan changes, and delete the ones that no longer apply. The person watches the list as your progress, so keep it true: never leave a finished step open or a step marked done that was not. A single question or a one-call answer needs no list. Prune finished steps when a new piece of work begins.",
				Preview: tools.PreviewOf(func(call struct {
					Action string          `json:"action"`
					Items  []todoBatchItem `json:"items"`
				}) string {
					switch call.Action {
					case "batch":
						return describeTodoBatch(call.Items)
					case "prune":
						return "Clear the finished steps"
					}
					return "Look at the steps"
				}),
				Run:     runTodo,
				RiskOf:  riskOfTodo,
				Overlay: todoOverlay,
			},
		}
	})
}

// todoBatchMostChangeCount is the most changes one batch makes.
const todoBatchMostChangeCount = 50

// todoTextMostRuneCount is the longest a step may be: a line, not a paragraph.
const todoTextMostRuneCount = 200

type todoBatchItem struct {
	Op   string `json:"op"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

type todoArguments struct {
	Action string          `json:"action"`
	Items  []todoBatchItem `json:"items"`
}

// todoOpWords is how each change is said in a line a person reads.
var todoOpWords = map[string]string{"add": "add", "complete": "finish", "update": "change", "reopen": "reopen", "delete": "drop"}

// describeTodoBatch says what a batch does, for the line a person sees.
func describeTodoBatch(items []todoBatchItem) string {
	counts := map[string]int{}
	knownCount := 0
	for _, item := range items {
		if _, known := todoOpWords[item.Op]; known {
			counts[item.Op]++
			knownCount++
		}
	}
	var parts []string
	for _, op := range []string{"add", "complete", "update", "reopen", "delete"} {
		if counts[op] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", todoOpWords[op], counts[op]))
		}
	}
	if len(parts) == 0 {
		return "Update the steps"
	}
	sentence := strings.Join(parts, ", ")
	noun := " steps"
	if knownCount == 1 {
		noun = " step"
	}
	return strings.ToUpper(sentence[:1]) + sentence[1:] + noun
}

// riskOfTodo is a read for list, a write for the rest.
func riskOfTodo(arguments json.RawMessage) tools.Risk {
	var call todoArguments
	if json.Unmarshal(arguments, &call) == nil && (call.Action == "list" || call.Action == "") {
		return tools.RiskRead
	}
	return tools.RiskWrite
}

func runTodo(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[todoArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	database := run.Database()
	// A to-do list is a conversation's, kept for the turns that follow. A
	// call from outside one has no list to keep, and inventing one would
	// file items nobody would ever see again.
	conversationId := tools.ConversationIDOf(run)
	if conversationId == "" {
		return nil, fmt.Errorf("a to-do list belongs to a conversation, and this call is not part of one")
	}
	listed := func(extra map[string]any) (*tools.Result, error) {
		var todos []*models.AgentTodo
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			todos, err = tx.ListAgentTodos(conversationId)
			return err
		}); err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(todos))
		open := 0
		for _, todo := range todos {
			if todo.DoneAt == nil {
				open++
			}
			rows = append(rows, map[string]any{"id": todo.ID, "text": todo.Text, "isDone": todo.DoneAt != nil})
		}
		answer := map[string]any{"todo": rows, "openCount": open, "doneCount": len(todos) - open}
		for key, value := range extra {
			answer[key] = value
		}
		return tools.JSONResult(answer)
	}
	switch arguments.Action {
	case "", "list":
		return listed(nil)
	case "prune":
		pruned := 0
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			todos, err := tx.ListAgentTodos(conversationId)
			if err != nil {
				return err
			}
			for _, todo := range todos {
				if todo.DoneAt == nil {
					continue
				}
				if err := tx.DeleteAgentTodo(conversationId, todo.ID); err != nil {
					return err
				}
				pruned++
			}
			return nil
		}); err != nil {
			return nil, err
		}
		return listed(map[string]any{"prunedCount": pruned})
	case "batch":
		if len(arguments.Items) == 0 {
			return nil, fmt.Errorf("a batch needs at least one change")
		}
		if len(arguments.Items) > todoBatchMostChangeCount {
			return nil, fmt.Errorf("a batch makes at most %d changes", todoBatchMostChangeCount)
		}
		// Each change is its own: one that is refused says why and the
		// rest still happen, as the result shows item by item.
		results := make([]map[string]any, 0, len(arguments.Items))
		failed := 0
		for index, item := range arguments.Items {
			id, err := applyTodo(ctx, database, conversationId, item)
			result := map[string]any{"changeIndex": index, "op": item.Op}
			if id != "" {
				result["id"] = id
			}
			if err != nil {
				result["errorMessage"] = err.Error()
				failed++
			}
			results = append(results, result)
		}
		return listed(map[string]any{"results": results, "succeededCount": len(results) - failed, "failedCount": failed})
	}
	return nil, fmt.Errorf("%q is not an action of todo: batch, list or prune", arguments.Action)
}

// applyTodo makes one change of a batch, and answers the item it touched.
func applyTodo(ctx context.Context, database db.Database, conversationId string, item todoBatchItem) (string, error) {
	text := strings.Join(strings.Fields(item.Text), " ")
	// Only where the text is used: a leftover on a completion is ignored.
	if (item.Op == "add" || item.Op == "update") && len([]rune(text)) > todoTextMostRuneCount {
		return "", fmt.Errorf("a step is a line, at most %d characters", todoTextMostRuneCount)
	}
	var touched string
	err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		switch item.Op {
		case "add":
			if text == "" {
				return fmt.Errorf("a step needs text")
			}
			created, err := tx.CreateAgentTodo(&models.AgentTodo{ConversationID: conversationId, Text: text})
			if err != nil {
				return err
			}
			touched = created.ID
			return nil
		case "update", "complete", "reopen":
			if item.Op == "update" && text == "" {
				return fmt.Errorf("an update needs the new text")
			}
			updated, err := tx.UpdateAgentTodo(item.ID, func(todo *models.AgentTodo) error {
				if todo.ConversationID != conversationId {
					return fmt.Errorf("there is no step %q", item.ID)
				}
				switch item.Op {
				case "update":
					todo.Text = text
				case "complete":
					now := time.Now()
					todo.DoneAt = &now
				case "reopen":
					todo.DoneAt = nil
				}
				return nil
			})
			if err != nil {
				return err
			}
			if updated == nil {
				return fmt.Errorf("there is no step %q", item.ID)
			}
			touched = updated.ID
			return nil
		case "delete":
			// A step that is not on this list is said to be missing, not
			// reported gone: the agent would believe a step it mistyped
			// had been removed.
			todos, err := tx.ListAgentTodos(conversationId)
			if err != nil {
				return err
			}
			for _, todo := range todos {
				if todo.ID == item.ID {
					touched = item.ID
					return tx.DeleteAgentTodo(conversationId, item.ID)
				}
			}
			return fmt.Errorf("there is no step %q", item.ID)
		}
		return fmt.Errorf("%q is not a change: add, update, complete, reopen or delete", item.Op)
	})
	if errors.Is(err, db.ErrNotFound) {
		return "", fmt.Errorf("there is no step %q", item.ID)
	}
	return touched, err
}

// todoOverlay is the open items, oldest first, capped, with counts.
func todoOverlay(ctx context.Context) string {
	run, err := tools.RunFrom(ctx)
	if err != nil || tools.ConversationIDOf(run) == "" {
		return ""
	}
	var todos []*models.AgentTodo
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		todos, err = tx.ListAgentTodos(tools.ConversationIDOf(run))
		return err
	}); err != nil || len(todos) == 0 {
		return ""
	}
	open := make([]string, 0, len(todos))
	done := 0
	for _, todo := range todos {
		if todo.DoneAt != nil {
			done++
			continue
		}
		if len(open) < 10 {
			// One line each, whatever an older item was written with.
			open = append(open, fmt.Sprintf("- [ ] %s (%s)", strings.Join(strings.Fields(todo.Text), " "), todo.ID))
		}
	}
	if len(open) == 0 && done == 0 {
		return ""
	}
	// A reminder, as the list itself is: the person watches it as progress,
	// so a finished step left open misleads them.
	reminder := "Complete each step in a batch as soon as its work is done; add or update steps when the plan changes."
	if len(open) == 0 {
		reminder = "Every step is done: prune them before starting something new."
	} else if done > 10 {
		reminder += " Prune the finished steps; they have piled up."
	}
	return fmt.Sprintf("<todo>\n%d open, %d done\n%s\n%s\n</todo>", len(todos)-done, done, strings.Join(open, "\n"), reminder)
}
