package agent_test

import (
	"encoding/json"
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

// A round the provider stopped at its output limit with a tool call half
// written runs nothing: the call is dropped, the model is told its answer was
// cut at the provider's limit, and it goes on in the next round. No round asks
// the provider for an output limit of its own.
func TestARoundCutAtTheOutputLimitIsContinuedNotRun(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	cutRound := `{"id":"c1","model":"m","choices":[{"delta":{"content":"Checking the time. "}}]}
{"id":"c1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_cut","function":{"name":"datetime","arguments":"{\"zone"}}]},"finish_reason":"length"}]}
{"id":"c1","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":5}}`
	model, requests := fakeModel(t, []string{cutRound, answerRound})
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

	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "what time is it", Surface: "cli"})
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
		t.Fatalf("the turn did not go on to its answer: %q", answer)
	}
	if len(*requests) != 2 {
		t.Fatalf("the model was asked %d times, want 2", len(*requests))
	}
	for index, request := range *requests {
		if _, isSet := request["max_tokens"]; isSet {
			t.Errorf("round %d sent max_tokens", index+1)
		}
		if _, isSet := request["max_completion_tokens"]; isSet {
			t.Errorf("round %d sent max_completion_tokens", index+1)
		}
	}
	second, _ := json.Marshal((*requests)[1]["messages"])
	if !strings.Contains(string(second), "cut off at the provider's limit") || !strings.Contains(string(second), "were not run") {
		t.Errorf("the model was not told its answer was cut: %s", second)
	}
	if strings.Contains(string(second), `"role":"tool"`) || strings.Contains(string(second), "call_cut") {
		t.Errorf("the cut call was kept or run: %s", second)
	}
}
