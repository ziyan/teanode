package agent_test

import (
	"context"
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

const openPageRound = `{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"open_page","arguments":"{\"path\":\"memory:people/some-person\",\"reason\":\"the page they asked for\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

const shownRound = `{"id":"s2","model":"m","choices":[{"delta":{"content":"There it is."},"finish_reason":"stop"}]}
{"id":"s2","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":4}}`

// requestedTools is the names of the tools a request to the model carried.
func requestedTools(request map[string]any) map[string]bool {
	names := map[string]bool{}
	list, _ := request["tools"].([]any)
	for _, entry := range list {
		function, _ := entry.(map[string]any)["function"].(map[string]any)
		if name, ok := function["name"].(string); ok {
			names[name] = true
		}
	}
	return names
}

// Asked in the drawer to be shown a page, the agent moves the dashboard
// there: the turn carries a navigate event with the page's path, which the
// drawer follows. Anywhere the turn is not read in the dashboard's own
// drawer -- a chat app, a terminal, the drawer framed into another site, a
// run with nobody present, a program over MCP -- the tool is not offered,
// and the agent gives a link instead.
func TestOpenPageMovesTheDrawerAndIsOfferedNowhereElse(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{openPageRound, shownRound})
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

	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "take me to my dad's page", Surface: "drawer"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	navigated := ""
	for _, event := range collect(run) {
		if event.Kind == agent.EventNavigate {
			navigated = event.Text
		}
		if event.Kind == agent.EventToolResult && event.Tool == "open_page" && event.Note != "Opened /knowledge/people/some-person: the page they asked for" {
			t.Errorf("the drawer's line for the call: %q (%s)", event.Note, event.Text)
		}
	}
	if navigated != "/knowledge/people/some-person" {
		t.Fatalf("the drawer was sent to %q", navigated)
	}
	if !requestedTools((*requests)[0])["open_page"] {
		t.Fatal("open_page is not offered in the drawer")
	}

	for _, surface := range []string{"phone"} {
		before := len(*requests)
		run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "and on the phone", Surface: surface})
		if err != nil {
			t.Fatalf("Ask on %s: %s", surface, err)
		}
		collect(run)
		if !requestedTools((*requests)[before])["open_page"] {
			t.Errorf("open_page is not offered on the %s", surface)
		}
	}
	for _, settings := range []*agent.AskSettings{
		{Surface: "telegram"}, {Surface: "discord"}, {Surface: "cli"}, {Surface: "extension"}, {Surface: "mail"},
		{Surface: "schedule", Headless: true},
	} {
		settings.Agent, settings.Owner, settings.Operations, settings.Conversation, settings.Message = found, owner, operations, conversation, "show me the page"
		before := len(*requests)
		run, err := worker.Ask(settings)
		if err != nil {
			t.Fatalf("Ask on %s: %s", settings.Surface, err)
		}
		collect(run)
		if len(*requests) == before {
			t.Fatalf("the turn on %s asked the model nothing", settings.Surface)
		}
		if requestedTools((*requests)[before])["open_page"] {
			t.Errorf("open_page is offered on %s, where there is no dashboard to move", settings.Surface)
		}
	}

	for _, tool := range worker.DirectTools(context.Background(), found, operations) {
		if tool.Name == "open_page" {
			t.Error("open_page is offered over MCP, where there is no dashboard to move")
		}
	}
}
