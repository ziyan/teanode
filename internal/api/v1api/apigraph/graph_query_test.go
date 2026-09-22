package apigraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

type queryTransactionDatabase struct {
	db.Database
	activeCount          atomic.Int64
	beginCount           atomic.Int64
	hasNestedTransaction atomic.Bool
}

func (self *queryTransactionDatabase) Transaction(function func(db.Transaction) error) error {
	return self.TransactionContext(context.Background(), function)
}

func (self *queryTransactionDatabase) TransactionContext(ctx context.Context, function func(db.Transaction) error) error {
	self.beginCount.Add(1)
	return self.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if self.activeCount.Add(1) > 1 {
			self.hasNestedTransaction.Store(true)
		}
		defer self.activeCount.Add(-1)
		return function(transaction)
	})
}

func TestRecallHTTPReleasesTransactionsAndRechecksAuthorization(test *testing.T) {
	for _, scenario := range []string{"recall", "denied", "anonymous", "skipped", "revoke", "disable-user", "disable-agent"} {
		test.Run(scenario, func(test *testing.T) {
			database, release := dbtest.AcquireDatabase(test)
			defer release()
			tracked := &queryTransactionDatabase{Database: database}
			var owner *models.User
			var person *models.Agent
			var role *models.Role
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				owner, err = transaction.CreateUser(&models.User{Username: "fixture-owner"})
				if err != nil {
					test.Fatal(err)
				}
				person, err = transaction.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
				if err != nil {
					test.Fatal(err)
				}
				permissions := []models.Permission{models.PermissionAgentUse}
				if scenario == "denied" {
					permissions = nil
				}
				role, err = transaction.CreateRole(&models.Role{Name: "Fixture role", Permissions: permissions})
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.CreateGroup(&models.Group{Name: "Fixture group", UserIDs: []string{owner.ID}, RoleIDs: []string{role.ID}}); err != nil {
					test.Fatal(err)
				}
				if err := transaction.EnsureAgentRoots(person.ID); err != nil {
					test.Fatal(err)
				}
				node, err := transaction.PutAgentNode(&models.AgentNode{AgentID: person.ID, Path: "projects/fixture", Kind: models.NodeProject, Name: "Fixture"})
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.AddAgentFact(&models.AgentFact{AgentID: person.ID, NodeID: node.ID, Kind: models.FactPlain, Text: "Fixture uses the blue workspace.", Audiences: []models.AgentAudience{models.AudienceAsk}}); err != nil {
					test.Fatal(err)
				}
			})
			var requestCount atomic.Int64
			var hasOpenTransaction atomic.Bool
			providerErrors := make(chan error, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requestCount.Add(1)
				if tracked.activeCount.Load() != 0 {
					hasOpenTransaction.Store(true)
				}
				err := database.TransactionContext(request.Context(), func(transaction db.Transaction) error {
					switch scenario {
					case "revoke":
						_, err := transaction.UpdateRole(role.ID, func(current *models.Role) error { current.Permissions = nil; return nil })
						return err
					case "disable-user":
						_, err := transaction.UpdateUser(owner.ID, func(current *models.User) error { current.DisabledAt = new(time.Now()); return nil })
						return err
					case "disable-agent":
						_, err := transaction.UpdateAgent(person.ID, func(current *models.Agent) error { current.Enabled = false; return nil })
						return err
					}
					return nil
				})
				if err != nil {
					select {
					case providerErrors <- err:
					default:
					}
					http.Error(writer, "fixture failure", 500)
					return
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{0.2, 0.4, 0.8}}}})
			}))
			defer provider.Close()
			configuration := config.Default()
			configuration.Agent.Enabled = true
			configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai", BaseURL: provider.URL, APIKey: "fixture"}}
			configuration.Agent.Models.Default = "fixture:writer"
			configuration.Agent.Models.Embedding = "fixture:embedding"
			registry, err := llm.Open(&configuration.Agent)
			if err != nil {
				test.Fatal(err)
			}
			worker := agent.New(&agent.Settings{Database: tracked, Registry: registry, Configuration: func() *config.Configuration { return configuration }})
			component, err := New(tracked, config.NewMemoryStore(configuration), nil, nil, nil, nil, nil, nil, nil, &api.Settings{Agent: worker})
			if err != nil {
				test.Fatal(err)
			}
			encodedRequest, err := json.Marshal(graphRequest{Query: `query Recall($include: Boolean!) { alias: RecallAgentMemory(question: "fixture workspace") @include(if: $include) { pages { path facts { text } } } __typename }`, Variables: map[string]interface{}{"include": scenario != "skipped"}})
			if err != nil {
				test.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(encodedRequest)))
			if scenario != "anonymous" {
				request.Header.Set(api.AuthenticatedUsernameHeader, owner.Username)
			}
			response := httptest.NewRecorder()
			component.(*graph).graphView(response, request)
			select {
			case err := <-providerErrors:
				test.Fatal(err)
			default:
			}
			if response.Code != http.StatusOK {
				test.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			var outcome struct {
				Data   map[string]json.RawMessage `json:"data"`
				Errors []json.RawMessage          `json:"errors"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
				test.Fatal(err)
			}
			shouldDeny := scenario != "recall" && scenario != "skipped"
			if (len(outcome.Errors) != 0) != shouldDeny {
				test.Fatalf("unexpected result: %s", response.Body.String())
			}
			if scenario == "recall" && !strings.Contains(string(outcome.Data["alias"]), "blue workspace") {
				test.Fatalf("missing recall: %s", response.Body.String())
			}
			if shouldDeny && strings.Contains(string(outcome.Data["alias"]), "blue workspace") {
				test.Fatal("revoked result was returned")
			}
			shouldAsk := scenario != "denied" && scenario != "anonymous" && scenario != "skipped"
			if (requestCount.Load() > 0) != shouldAsk || hasOpenTransaction.Load() || tracked.hasNestedTransaction.Load() {
				test.Fatalf("requests=%d, transaction during model=%v, nested=%v", requestCount.Load(), hasOpenTransaction.Load(), tracked.hasNestedTransaction.Load())
			}
		})
	}
}

func TestHTTPQueryAndMutationTransactionScopes(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	tracked := &queryTransactionDatabase{Database: database}
	resolver := &graph{database: tracked, config: config.NewMemoryStore(config.Default())}
	probe := func(parameters graphql.ResolveParams) (interface{}, error) {
		if api.ContextTransaction(parameters.Context) == nil || tracked.activeCount.Load() != 1 {
			test.Error("root resolver has no single active transaction")
		}
		if shouldFail, _ := parameters.Args["fail"].(bool); shouldFail {
			transaction := api.ContextTransaction(parameters.Context)
			if _, err := transaction.CreateUser(&models.User{Username: "fixture-abort"}); err != nil {
				return nil, err
			}
			_, err := transaction.CreateUser(&models.User{Username: "fixture-abort"})
			return nil, err
		}
		return true, nil
	}
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:    graphql.NewObject(graphql.ObjectConfig{Name: "Query", Fields: graphql.Fields{"Probe": &graphql.Field{Type: graphql.Boolean, Resolve: probe, Args: graphql.FieldConfigArgument{"fail": &graphql.ArgumentConfig{Type: graphql.Boolean}}}}}),
		Mutation: graphql.NewObject(graphql.ObjectConfig{Name: "Mutation", Fields: graphql.Fields{"Change": &graphql.Field{Type: graphql.Boolean, Resolve: probe}}}),
	})
	if err != nil {
		test.Fatal(err)
	}
	resolver.schema = schema
	resolver.wrapQueryTransactions()
	for _, fixture := range []struct {
		document         string
		transactionCount int64
		shouldFail       bool
	}{
		{document: `{ first: Probe second: Probe }`, transactionCount: 2},
		{document: `{ failed: Probe(fail: true) surviving: Probe }`, transactionCount: 2, shouldFail: true},
		{document: `mutation { first: Change second: Change }`, transactionCount: 1},
		{document: `{ Probe @skip(if: true) __typename }`, transactionCount: 0},
	} {
		tracked.beginCount.Store(0)
		encodedRequest, err := json.Marshal(graphRequest{Query: fixture.document})
		if err != nil {
			test.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(encodedRequest)))
		response := httptest.NewRecorder()
		resolver.graphView(response, request)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"errors"`) != fixture.shouldFail {
			test.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
		}
		if fixture.shouldFail {
			if !strings.Contains(response.Body.String(), `"surviving":true`) {
				test.Fatalf("a failed root field blocked its sibling: %s", response.Body.String())
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM "user" WHERE username = 'fixture-abort'`); count != "0" {
				test.Fatalf("failed field committed its partial write: %s", count)
			}
		}
		if tracked.beginCount.Load() != fixture.transactionCount || tracked.activeCount.Load() != 0 || tracked.hasNestedTransaction.Load() {
			test.Fatalf("%s: transactions=%d, active=%d, nested=%v", fixture.document, tracked.beginCount.Load(), tracked.activeCount.Load(), tracked.hasNestedTransaction.Load())
		}
	}
}
