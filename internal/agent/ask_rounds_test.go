package agent_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

const timeCallRound = `{"id":"t1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_t","function":{"name":"datetime","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}
{"id":"t1","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":5}}`

// A turn has no number of rounds to stop at. A small fake model that asks
// for the time a hundred and sixty times, more than the hundred and fifty
// rounds a turn used to be given, is answered every time, is never told that
// a round is the last, and finishes with its answer.
func TestATurnGoesOnPastOneHundredAndFiftyRounds(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const calls = 160
	script := make([]string, 0, calls+1)
	for range calls {
		script = append(script, timeCallRound)
	}
	script = append(script, answerRound)
	model, requests := fakeModel(t, script)
	defer model.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	defer worker.Stop()

	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}

	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "keep checking the time", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	answer := ""
	for _, event := range collect(run) {
		if event.Kind == agent.EventMessage {
			answer = event.Text
		}
	}
	if answer != "The invoice is [Invoice 42](mail:item1)." {
		t.Fatalf("the turn did not reach its answer: %q", answer)
	}
	if len(*requests) != calls+1 {
		t.Fatalf("the model was asked %d times, want %d", len(*requests), calls+1)
	}
	for index, request := range *requests {
		if strings.Contains(strings.ToLower(requestText(request)), "this is the last round") {
			t.Fatalf("round %d was told it was the last", index+1)
		}
	}
}

// requestText is every message of a request, for looking for a word in.
func requestText(request map[string]any) string {
	var text strings.Builder
	messages, _ := request["messages"].([]any)
	for _, message := range messages {
		if fields, ok := message.(map[string]any); ok {
			if content, ok := fields["content"].(string); ok {
				text.WriteString(content)
				text.WriteString("\n")
			}
		}
	}
	return text.String()
}
