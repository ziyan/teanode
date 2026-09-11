package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Two small tools of the conversation itself: a question to the person
// when the answer changes what happens, and the conversation's own task
// list so a long piece of work keeps its place.

func registerConversationTools(catalog *Catalog) {
	catalog.Register(&Tool{
		Name: "ask_user", Family: FamilyGeneral, Core: true, Risk: RiskRead,
		Description: "Ask the person a question and wait for the answer. Only when the answer changes what you do; otherwise state your assumption and go on. Offer choices when there are a few.",
		Parameters: object(map[string]any{
			"question": stringProperty("the question, in a line"),
			"choices":  arrayProperty("a few answers to pick from", stringProperty("a choice")),
		}, "question"),
		Run: runAskUser,
	})
	catalog.Register(&Tool{
		Name: "todo", Family: FamilyGeneral, Core: true, Risk: RiskWrite,
		Description: "This conversation's task list, for work in several steps: add items, mark them done, reopen or remove them, list them. Shown to you every round, so you keep your place.",
		Parameters: object(map[string]any{
			"action": enumProperty("what to do", "add", "done", "reopen", "remove", "list"),
			"text":   stringProperty("for add: the item"),
			"id":     stringProperty("for done, reopen and remove: the item"),
		}, "action"),
		Run:     runTodo,
		Overlay: todoOverlay,
	})
}

type askUserArguments struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
}

func runAskUser(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := decodeArguments[askUserArguments](call)
	if err != nil {
		return nil, err
	}
	run := runOf(ctx)
	if strings.TrimSpace(arguments.Question) == "" {
		return nil, fmt.Errorf("ask something")
	}
	if run.settings.Headless || run.settings.Surface == "mail" || run.settings.Surface == "schedule" || run.settings.Surface == "research" {
		return nil, fmt.Errorf("nobody is present to answer; state your assumption and go on, or say in the answer what you would have asked")
	}
	answer, err := run.question(ctx, call.ID, arguments.Question, arguments.Choices)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"answer": answer})
}

// question shows the card and waits for the person's words.
func (self *AskRun) question(ctx context.Context, callId, question string, choices []string) (string, error) {
	channel := make(chan string, 1)
	self.mutex.Lock()
	if self.questions == nil {
		self.questions = map[string]chan string{}
	}
	self.questions[callId] = channel
	self.mutex.Unlock()
	self.emit(Event{Kind: EventQuestion, CallID: callId, Tool: "ask_user", Note: question, Text: strings.Join(choices, "\n")})
	timer := time.NewTimer(confirmationWait)
	defer timer.Stop()
	select {
	case answer, ok := <-channel:
		if !ok {
			return "", ctx.Err()
		}
		return answer, nil
	case <-timer.C:
		self.mutex.Lock()
		delete(self.questions, callId)
		self.mutex.Unlock()
		return "", fmt.Errorf("the person did not answer within %s", confirmationWait)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Answer answers a question card. It says whether there was one.
func (self *AskRun) Answer(callId, answer string) bool {
	self.mutex.Lock()
	channel, ok := self.questions[callId]
	if ok {
		delete(self.questions, callId)
	}
	self.mutex.Unlock()
	if !ok {
		return false
	}
	channel <- answer
	return true
}

type todoArguments struct {
	Action string `json:"action"`
	Text   string `json:"text"`
	ID     string `json:"id"`
}

func runTodo(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := decodeArguments[todoArguments](call)
	if err != nil {
		return nil, err
	}
	run := runOf(ctx)
	database := run.agent.settings.Database
	conversationId := run.settings.Conversation.ID
	list := func() (*Result, error) {
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
		return jsonResult(map[string]any{"todo": rows})
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
			return tx.DeleteAgentTodo(arguments.ID)
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
	run := runOf(ctx)
	var todos []*models.AgentTodo
	if err := run.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		todos, err = tx.ListAgentTodos(run.settings.Conversation.ID)
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
