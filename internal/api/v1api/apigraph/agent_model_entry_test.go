package apigraph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/graphapi"
)

type modelEntryWorld struct {
	graph   *graph
	store   config.Store
	manager *api.Principal
	visitor *api.Principal
	schema  graphql.Schema
}

func newModelEntryWorld(test *testing.T, transaction db.Transaction, userId string) *modelEntryWorld {
	test.Helper()
	configuration := config.Default()
	configuration.Server.Name = "models.example.com"
	configuration.Listen.HTTPS = ""
	configuration.TLS.ACME.Enabled = false
	configuration.Services = config.AssistantServices()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{
		{Name: "alpha", Kind: config.AgentProviderKindOpenAI, APIKey: "key-one", ModelEntries: []config.AgentModelEntry{{Model: "alpha-large", Name: "Large"}, {Model: "alpha-small"}, {Model: "alpha-embed"}}},
		{Name: "judge", Kind: config.AgentProviderKindTypeSafe, APIKey: "key-two", ModelEntries: []config.AgentModelEntry{{Model: "judge-one"}}},
	}
	configuration.Agent.Models = config.AgentModels{Default: "alpha:alpha-large", Quick: "alpha:alpha-small", Fast: "alpha:alpha-small", Embedding: "alpha:alpha-embed", Decide: "judge:judge-one"}
	store := config.NewMemoryStore(configuration)
	test.Cleanup(func() { _ = store.Close() })
	user, err := transaction.GetUser(userId)
	if err != nil {
		test.Fatal(err)
	}
	world := &modelEntryWorld{
		graph:   &graph{config: store},
		store:   store,
		manager: &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionServerManage}})},
		visitor: &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})},
	}
	graphApi := graphapi.New()
	var query Query = world.graph
	var mutation Mutation = world.graph
	if err := graphApi.Register(&query, &mutation, nil); err != nil {
		test.Fatalf("cannot register the schema: %s", err)
	}
	if world.schema, err = graphApi.Build(); err != nil {
		test.Fatalf("cannot build the schema: %s", err)
	}
	world.graph.schema = world.schema
	return world
}

// run executes a document as the principal and returns the data and the
// first error message.
func (self *modelEntryWorld) run(test *testing.T, transaction db.Transaction, principal *api.Principal, document string, variables map[string]any) (map[string]any, string) {
	test.Helper()
	ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
	result := graphql.Do(graphql.Params{Schema: self.schema, RequestString: document, VariableValues: variables, Context: ctx})
	message := ""
	if len(result.Errors) > 0 {
		message = result.Errors[0].Message
	}
	encoded, _ := json.Marshal(result.Data)
	var data map[string]any
	_ = json.Unmarshal(encoded, &data)
	return data, message
}

const saveModelEntryDocument = `mutation ($providerName: String!, $entry: AgentModelEntryInput!) {
  SaveAgentModelEntry(providerName: $providerName, entry: $entry) { model name contextWindow reportedContextWindow isEnabled usedFor pricing { input output cacheRead cacheWrite audioInput } }
}`

const deleteModelEntryDocument = `mutation ($providerName: String!, $model: String!) { DeleteAgentModelEntry(providerName: $providerName, model: $model) }`

func TestSavingAndDeletingModelEntries(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "model-manager")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newModelEntryWorld(test, transaction, userId)

		// Adding a model: the answer is the saved entry, with its prices
		// as an object and what uses it.
		data, message := world.run(test, transaction, world.manager, saveModelEntryDocument, map[string]any{
			"providerName": "alpha",
			"entry": map[string]any{
				"model": "alpha-medium", "name": "Medium", "contextWindow": 64000,
				"pricing": map[string]any{"input": 1.5, "output": 6, "audioInput": 3},
			},
		})
		if message != "" {
			test.Fatalf("saving a model: %s", message)
		}
		saved, _ := data["SaveAgentModelEntry"].(map[string]any)
		pricing, _ := saved["pricing"].(map[string]any)
		if saved["model"] != "alpha-medium" || saved["name"] != "Medium" || saved["contextWindow"] != float64(64000) || saved["isEnabled"] != true ||
			pricing["input"] != 1.5 || pricing["output"] != float64(6) || pricing["audioInput"] != float64(3) || pricing["cacheRead"] != float64(0) {
			test.Fatalf("the saved entry reads back as %+v", saved)
		}
		if usedFor, _ := saved["usedFor"].([]any); len(usedFor) != 0 {
			test.Fatalf("nothing uses a new model: %+v", saved)
		}
		entries := world.store.Current().Agent.Provider("alpha").ModelEntries
		if len(entries) != 4 || entries[3].Model != "alpha-medium" || entries[3].Pricing.Input != 1.5 || !entries[3].IsEnabled() {
			test.Fatalf("the configuration holds %+v", entries)
		}

		// The same id changes the entry rather than adding another, and an
		// entry sent without isEnabled is on.
		_, message = world.run(test, transaction, world.manager, saveModelEntryDocument, map[string]any{
			"providerName": "alpha", "entry": map[string]any{"model": "alpha-medium", "name": "Middle"},
		})
		if message != "" {
			test.Fatalf("changing a model: %s", message)
		}
		entries = world.store.Current().Agent.Provider("alpha").ModelEntries
		if len(entries) != 4 || entries[3].Name != "Middle" || entries[3].Pricing.Input != 0 {
			test.Fatalf("changing replaced nothing or added a second: %+v", entries)
		}

		// Only somebody who may run the server changes them.
		if _, message = world.run(test, transaction, world.visitor, saveModelEntryDocument, map[string]any{
			"providerName": "alpha", "entry": map[string]any{"model": "alpha-x"},
		}); message == "" {
			test.Fatal("somebody without server:manage saved a model")
		}
		if _, message = world.run(test, transaction, world.visitor, deleteModelEntryDocument, map[string]any{"providerName": "alpha", "model": "alpha-medium"}); message == "" {
			test.Fatal("somebody without server:manage deleted a model")
		}

		// Refused: a window under a thousand tokens, a negative price, an
		// unknown provider, an empty id. Nothing is stored for any of them.
		for name, entry := range map[string]map[string]any{
			"window": {"model": "alpha-bad", "contextWindow": 500},
			"price":  {"model": "alpha-bad", "pricing": map[string]any{"output": -1}},
			"id":     {"model": "  "},
		} {
			if _, message = world.run(test, transaction, world.manager, saveModelEntryDocument, map[string]any{"providerName": "alpha", "entry": entry}); message == "" {
				test.Errorf("a bad %s was saved", name)
			}
		}
		if _, message = world.run(test, transaction, world.manager, saveModelEntryDocument, map[string]any{"providerName": "nowhere", "entry": map[string]any{"model": "x"}}); !strings.Contains(message, "nowhere") {
			test.Errorf("an unknown provider is named: %q", message)
		}
		if count := len(world.store.Current().Agent.Provider("alpha").ModelEntries); count != 4 {
			test.Fatalf("a refused save changed the entries: %d", count)
		}

		// Switching off a model a role uses is refused, naming the role.
		_, message = world.run(test, transaction, world.manager, saveModelEntryDocument, map[string]any{
			"providerName": "alpha", "entry": map[string]any{"model": "alpha-large", "isEnabled": false},
		})
		if !strings.Contains(message, "agent.models.default") || !strings.Contains(message, "switched off") {
			test.Fatalf("switching off the default model: %q", message)
		}

		// A delete is refused while a role uses the model, and the refusal
		// names every role.
		_, message = world.run(test, transaction, world.manager, deleteModelEntryDocument, map[string]any{"providerName": "alpha", "model": "alpha-small"})
		if !strings.Contains(message, "quick") || !strings.Contains(message, "fast") || !strings.Contains(message, "alpha:alpha-small") {
			test.Fatalf("the refusal names the roles and the model: %q", message)
		}
		if !world.store.Current().Agent.Provider("alpha").Offers("alpha-small") {
			test.Fatal("a refused delete removed the model")
		}

		// Unused, it goes; twice is not found.
		data, message = world.run(test, transaction, world.manager, deleteModelEntryDocument, map[string]any{"providerName": "alpha", "model": "alpha-medium"})
		if message != "" || data["DeleteAgentModelEntry"] != true {
			test.Fatalf("deleting an unused model: %v %q", data, message)
		}
		if world.store.Current().Agent.Provider("alpha").ModelEntryFor("alpha-medium") != nil {
			test.Fatal("the model is still there")
		}
		if _, message = world.run(test, transaction, world.manager, deleteModelEntryDocument, map[string]any{"providerName": "alpha", "model": "alpha-medium"}); message == "" {
			test.Fatal("deleting a model twice was not refused")
		}
	})
}

// The settings list each provider's models with what uses them, and the old
// allow, deny and modelPricing fields answer empty.
func TestSettingsListModelEntriesWithTheirRoles(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "model-reader")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newModelEntryWorld(test, transaction, userId)
		settings := describeAgentSettings(world.store.Current())
		alpha := settings.Providers[0]
		if len(alpha.Models) != 3 || alpha.Models[0].Model != "alpha-large" || alpha.Models[0].Name != "Large" {
			test.Fatalf("the models are %+v", alpha.Models)
		}
		if got := strings.Join(alpha.Models[1].UsedFor, ","); got != "fast,quick" {
			test.Fatalf("what uses the small model: %q", got)
		}
		if got := strings.Join(settings.Providers[1].Models[0].UsedFor, ","); got != "decide" {
			test.Fatalf("what uses the judge: %q", got)
		}
		if len(alpha.Allow) != 0 || len(alpha.Deny) != 0 || len(alpha.ModelPricing) != 0 || alpha.Allow == nil || alpha.ModelPricing == nil {
			test.Fatalf("the deprecated fields answer empty, not null: %+v", alpha)
		}
		// Through the schema, with the fields the clients ask for.
		data, message := world.run(test, transaction, world.manager, `{ __type(name: "AgentModelEntryInput") { inputFields { name } } }`, nil)
		if message != "" {
			test.Fatal(message)
		}
		fields := map[string]bool{}
		for _, field := range data["__type"].(map[string]any)["inputFields"].([]any) {
			fields[field.(map[string]any)["name"].(string)] = true
		}
		for _, want := range []string{"model", "name", "contextWindow", "pricing", "isEnabled"} {
			if !fields[want] {
				test.Errorf("AgentModelEntryInput has no %s: %v", want, fields)
			}
		}
		if fields["usedFor"] || fields["reportedContextWindow"] {
			test.Errorf("the read-only fields are in the input: %v", fields)
		}
	})
}

// Every document the command line sends for a provider's models, and the
// settings document that reads them, is valid against the schema the server
// builds.
func TestModelEntryClientDocumentsMatchTheSchema(test *testing.T) {
	test.Parallel()

	component, err := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, &api.Settings{})
	if err != nil {
		test.Fatalf("the schema does not build: %s", err)
	}
	schema := component.(*graph).schema
	for name, document := range map[string]string{
		"GetSettings":           client.DocumentGetSettings,
		"AgentProviderModels":   client.DocumentAgentProviderModels,
		"SaveAgentModelEntry":   client.DocumentSaveAgentModelEntry,
		"DeleteAgentModelEntry": client.DocumentDeleteAgentModelEntry,
	} {
		parsed, err := parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{Body: []byte(document), Name: name})})
		if err != nil {
			test.Errorf("%s does not parse: %s", name, err)
			continue
		}
		for _, failure := range graphql.ValidateDocument(&schema, parsed, nil).Errors {
			test.Errorf("%s is not valid against the schema: %s", name, failure.Message)
		}
	}
}
