// Package askuser is the conversation's own two tools: a question to the
// person when the answer changes what happens, and the task list a long
// piece of work keeps its place with.
package askuser

import (
	"context"
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
				Description: "This conversation's task list, for work in several steps: add items, mark them done, reopen or remove them, list them. Shown to you every round, so you keep your place.",
				Parameters: tools.Object(map[string]any{
					"action": tools.EnumProperty("what to do", "add", "done", "reopen", "remove", "list"),
					"text":   tools.StringProperty("for add: the item"),
					"id":     tools.StringProperty("for done, reopen and remove: the item"),
				}, "action"),
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

type todoArguments struct {
	Action string `json:"action"`
	Text   string `json:"text"`
	ID     string `json:"id"`
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
	conversationId := run.Conversation().ID
	list := func() (*tools.Result, error) {
		var todos []*models.AgentTodo
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			todos, err = tx.ListAgentTodos(conversationId)
			return err
		}); err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(todos))
		for _, todo := range todos {
			rows = append(rows, map[string]any{"id": todo.ID, "text": todo.Text, "done": todo.DoneAt != nil})
		}
		return tools.JSONResult(map[string]any{"todo": rows})
	}
	switch arguments.Action {
	case "add":
		if strings.TrimSpace(arguments.Text) == "" {
			return nil, fmt.Errorf("an item needs text")
		}
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.CreateAgentTodo(&models.AgentTodo{ConversationID: conversationId, Text: strings.TrimSpace(arguments.Text)})
			return err
		}); err != nil {
			return nil, err
		}
		return list()
	case "done", "reopen":
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.UpdateAgentTodo(arguments.ID, func(todo *models.AgentTodo) error {
				if todo.ConversationID != conversationId {
					return fmt.Errorf("there is no item %q", arguments.ID)
				}
				if arguments.Action == "done" {
					now := time.Now()
					todo.DoneAt = &now
				} else {
					todo.DoneAt = nil
				}
				return nil
			})
			return err
		}); err != nil {
			return nil, err
		}
		return list()
	case "remove":
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.DeleteAgentTodo(conversationId, arguments.ID)
		}); err != nil {
			return nil, err
		}
		return list()
	case "", "list":
		return list()
	}
	return nil, fmt.Errorf("%q is not an action of todo", arguments.Action)
}

// todoOverlay is the open items, oldest first, capped, with counts.
func todoOverlay(ctx context.Context) string {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return ""
	}
	var todos []*models.AgentTodo
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		todos, err = tx.ListAgentTodos(run.Conversation().ID)
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
