// Package askuser is the conversation's own two tools: a question to the
// person when the answer changes what happens, and the task list a long
// piece of work keeps its place with.
package askuser

import (
	"context"
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
				Name: "ask_user", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskRead,
				Description: "Ask the person a question and wait for the answer. Only when the answer changes what you do; otherwise state your assumption and go on. Offer choices when there are a few.",
				Parameters: tools.Object(map[string]any{
					"question": tools.StringProperty("the question, in a line"),
					"choices":  tools.ArrayProperty("a few answers to pick from", tools.StringProperty("a choice")),
				}, "question"),
				Run: runAskUser,
			},
			{
				Name: "todo", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "This conversation's task list: yours, for work in several steps, to keep your place and show the person your progress; they can see it but not change it. `batch` makes every change in one call -- add the steps when you start, complete each as you finish it, update or delete one that changed; `list` shows it, `prune` clears the finished ones. Shown to you every round.",
				Parameters: tools.Object(map[string]any{
					"action": tools.EnumProperty("what to do", "batch", "list", "prune"),
					"items": map[string]any{
						"type": "array", "minItems": 1, "maxItems": todoBatchMost,
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
				Overlay: todoOverlay,
			},
		}
	})
}

type askUserArguments struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
}

func runAskUser(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[askUserArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Question) == "" {
		return nil, fmt.Errorf("ask something")
	}
	if run.Headless() || run.Surface() == "mail" || run.Surface() == "schedule" || run.Surface() == "research" {
		return nil, fmt.Errorf("nobody is present to answer; state your assumption and go on, or say in the answer what you would have asked")
	}
	answer, err := run.Ask(ctx, call.ID, arguments.Question, arguments.Choices)
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"answer": answer})
}

// todoBatchMost is the most changes one batch makes.
const todoBatchMost = 50

// todoTextMost is the longest a step may be: a line, not a paragraph.
const todoTextMost = 200

type todoBatchItem struct {
	Op   string `json:"op"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

type todoArguments struct {
	Action string          `json:"action"`
	Items  []todoBatchItem `json:"items"`
}

// describeTodoBatch says what a batch does, for the line a person sees.
func describeTodoBatch(items []todoBatchItem) string {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Op]++
	}
	var parts []string
	for _, op := range []string{"add", "complete", "update", "reopen", "delete"} {
		if counts[op] == 0 {
			continue
		}
		word := map[string]string{"add": "add", "complete": "finish", "update": "change", "reopen": "reopen", "delete": "drop"}[op]
		parts = append(parts, fmt.Sprintf("%s %d", word, counts[op]))
	}
	if len(parts) == 0 {
		return "Update the steps"
	}
	sentence := strings.Join(parts, ", ")
	return strings.ToUpper(sentence[:1]) + sentence[1:] + " step" + map[bool]string{true: "", false: "s"}[len(items) == 1]
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
			rows = append(rows, map[string]any{"id": todo.ID, "text": todo.Text, "done": todo.DoneAt != nil})
		}
		answer := map[string]any{"todo": rows, "open": open, "done": len(todos) - open}
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
		return listed(map[string]any{"pruned": pruned})
	case "batch":
		if len(arguments.Items) == 0 {
			return nil, fmt.Errorf("a batch needs at least one change")
		}
		if len(arguments.Items) > todoBatchMost {
			return nil, fmt.Errorf("a batch makes at most %d changes", todoBatchMost)
		}
		// Each change is its own: one that is refused says why and the
		// rest still happen, as the result shows item by item.
		results := make([]map[string]any, 0, len(arguments.Items))
		failed := 0
		for index, item := range arguments.Items {
			id, err := applyTodo(ctx, database, conversationId, item)
			result := map[string]any{"index": index, "op": item.Op}
			if id != "" {
				result["id"] = id
			}
			if err != nil {
				result["error"] = err.Error()
				failed++
			}
			results = append(results, result)
		}
		return listed(map[string]any{"results": results, "succeeded": len(results) - failed, "failed": failed})
	}
	return nil, fmt.Errorf("%q is not an action of todo: batch, list or prune", arguments.Action)
}

// applyTodo makes one change of a batch, and answers the item it touched.
func applyTodo(ctx context.Context, database db.Database, conversationId string, item todoBatchItem) (string, error) {
	text := strings.Join(strings.Fields(item.Text), " ")
	if len([]rune(text)) > todoTextMost {
		return "", fmt.Errorf("a step is a line, at most %d characters", todoTextMost)
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
			touched = item.ID
			return tx.DeleteAgentTodo(conversationId, item.ID)
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
			open = append(open, fmt.Sprintf("- [ ] %s (%s)", todo.Text, todo.ID))
		}
	}
	if len(open) == 0 && done == 0 {
		return ""
	}
	return fmt.Sprintf("<todo>\n%d open, %d done\n%s\n</todo>", len(todos)-done, done, strings.Join(open, "\n"))
}
