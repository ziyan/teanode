package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Each service starts on a model worth talking to, never on the first of a
// long list, and on an embedding model where it has one.
func TestAnAssistantStartsOnAModelWorthTalkingTo(t *testing.T) {
	cases := []struct {
		name           string
		kind           string
		isOwnServer    bool
		names          []string
		defaultModel   string
		embeddingModel string
	}{
		{"openai", config.AgentProviderKindOpenAI, false,
			[]string{"babbage-002", "gpt-4o-mini", "gpt-4o-mini-tts", "gpt-5-mini", "gpt-5-mini-2025-08-07", "text-embedding-3-large", "text-embedding-3-small"},
			"gpt-5-mini", "text-embedding-3-small"},
		{"openai without the first preference", config.AgentProviderKindOpenAI, false,
			[]string{"gpt-4.1-mini", "gpt-4o-mini"}, "gpt-4.1-mini", ""},
		{"openai with none of them", config.AgentProviderKindOpenAI, false,
			[]string{"babbage-002", "gpt-4o-mini-tts", "some-chat-model"}, "some-chat-model", ""},
		{"anthropic", config.AgentProviderKindAnthropic, false,
			[]string{"claude-haiku-4-5-20251001", "claude-sonnet-4-20250514", "claude-sonnet-4-5-20250929"},
			"claude-sonnet-4-5-20250929", ""},
		{"gemini", config.AgentProviderKindGemini, false,
			[]string{"gemini-2.0-flash", "gemini-2.5-flash", "gemini-2.5-flash-preview-05-20", "gemini-2.5-pro", "gemini-embedding-001"},
			"gemini-2.5-flash", "gemini-embedding-001"},
		{"a server of one's own", config.AgentProviderKindOpenAI, true,
			[]string{"nomic-embed-text", "llama3.2", "qwen2.5"}, "llama3.2", "nomic-embed-text"},
	}
	for _, each := range cases {
		defaultModel, embeddingModel := chooseAssistantModels(each.kind, each.isOwnServer, each.names)
		if defaultModel != each.defaultModel || embeddingModel != each.embeddingModel {
			t.Errorf("%s: chose %q and %q, want %q and %q", each.name, defaultModel, embeddingModel, each.defaultModel, each.embeddingModel)
		}
	}
}

// One call sets up a model server of one's own: the provider, the default
// and embedding models, agents offered, setup finished, and the caller's
// own agent on. A key the server refuses changes nothing.
func TestTheAssistantIsSetUpInOneStep(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") == "Bearer refused" {
			http.Error(writer, `{"error":{"message":"Incorrect API key provided"}}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"object": "list", "data": []map[string]any{
			{"id": "nomic-embed-text", "object": "model"}, {"id": "llama3.2", "object": "model"},
		}})
	}))
	defer modelServer.Close()

	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	userId := dbtest.CreateUser(t, database, "first-owner")
	configuration := config.Default()
	configuration.Server.Name = "assistant.example.com"
	configuration.Listen.HTTPS = ""
	configuration.TLS.ACME.Enabled = false
	configuration.Services = config.AssistantServices()
	store := config.NewMemoryStore(configuration)
	defer func() { _ = store.Close() }()
	resolver := &graph{database: database, config: store}

	address := modelServer.URL + "/v1"
	refused := "refused"
	dbtest.RunTransactionOn(t, database, func(transaction db.Transaction) {
		user, err := transaction.GetUser(userId)
		if err != nil {
			t.Fatal(err)
		}
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionServerManage}, {Permission: models.PermissionAgentUse}})}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(t.Context(), principal), transaction)

		if _, err := resolver.SetUpAssistant(ctx, SetUpAssistantArguments{Kind: config.AgentProviderKindOpenAI, BaseURL: &address, APIKey: &refused}); err == nil || !strings.Contains(err.Error(), AssistantKeyRefused) {
			t.Fatalf("a refused key was not reported as refused: %v", err)
		}
		if current := store.Current().Agent; current.Enabled || len(current.Providers) != 0 {
			t.Fatalf("a refused key changed the configuration: %+v", current)
		}

		setup, err := resolver.SetUpAssistant(ctx, SetUpAssistantArguments{Kind: config.AgentProviderKindOpenAI, BaseURL: &address})
		if err != nil {
			t.Fatal(err)
		}
		if setup.Provider != "local" || setup.DefaultModel != "local:llama3.2" || setup.EmbeddingModel != "local:nomic-embed-text" || !setup.IsAgentOn || !setup.NeedsRestart {
			t.Errorf("the setup is %+v", setup)
		}
		current := store.Current()
		if !current.Agent.Enabled || len(current.Agent.Providers) != 1 || current.Agent.Providers[0].BaseURL != address || current.Agent.Models.Default != "local:llama3.2" {
			t.Errorf("the configuration is %+v", current.Agent)
		}
		if !current.Services.HasCompletedSetup {
			t.Error("setting up the assistant did not finish setup")
		}
		agent, err := transaction.GetAgentByUser(user.ID)
		if err != nil || agent == nil || !agent.Enabled {
			t.Errorf("the caller's own agent is not on: %+v %v", agent, err)
		}

		// Again, with a model named: the same provider, not a second one.
		chosen := "qwen2.5"
		if _, err := resolver.SetUpAssistant(ctx, SetUpAssistantArguments{Kind: config.AgentProviderKindOpenAI, BaseURL: &address, Model: &chosen}); err != nil {
			t.Fatal(err)
		}
		if current := store.Current().Agent; len(current.Providers) != 1 || current.Models.Default != "local:qwen2.5" {
			t.Errorf("setting up again gave %+v", current)
		}
	})
}
