package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The invented bill: three days of five models. Every model uses a million
// input tokens, a hundred thousand output tokens and (for the first) a
// million cache write tokens a day, billed at exactly the rate named.
type billedModel struct {
	name        string
	inputRate   float64
	outputRate  float64
	cacheWrite  float64
	hasUsedHere bool
}

var billedModels = []billedModel{
	// Configured with a zero cache write price on an exact entry, and an
	// output price four percent under the bill's.
	{name: "alpha-model", inputRate: 0.2, outputRate: 1.3, cacheWrite: 0.25, hasUsedHere: true},
	// No entry of its own: the provider's own prices applied.
	{name: "beta-model", inputRate: 1, outputRate: 8, hasUsedHere: true},
	// The report names a snapshot; the agent uses the name without it.
	{name: "gamma-model-2026-03-17", outputRate: 10, hasUsedHere: false},
	// Covered by a pattern entry.
	{name: "delta-one", outputRate: 3, hasUsedHere: true},
	// Billed to the account, never used by the agent.
	{name: "never-used-model", outputRate: 5},
}

const (
	billingTestKey   = "billing-key-test"
	billedTokensADay = 1_000_000.0
	billedOutputADay = 100_000.0
)

type billingFixture struct {
	worker   *agent.Agent
	store    config.Store
	database db.Database
	server   *httptest.Server

	requestCount atomic.Int64
	isRefusing   atomic.Bool

	operatorAgentID    string
	nonOperatorAgentID string
}

func billingAmount(rate, tokens float64) map[string]any {
	return map[string]any{"value": rate * tokens / 1e6, "currency": "usd"}
}

func (self *billingFixture) serveBill(writer http.ResponseWriter, request *http.Request) {
	self.requestCount.Add(1)
	if self.isRefusing.Load() || request.Header.Get("Authorization") != "Bearer "+billingTestKey {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error": "Missing scopes: api.usage.read"}`))
		return
	}
	var data []map[string]any
	for dayIndex := 0; dayIndex < 3; dayIndex++ {
		start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, dayIndex-3).Unix()
		var results []map[string]any
		for _, billed := range billedModels {
			switch request.URL.Path {
			case "/v1/organization/costs":
				if billed.inputRate > 0 {
					results = append(results, map[string]any{"line_item": billed.name + ", input", "amount": billingAmount(billed.inputRate, billedTokensADay)})
				}
				if billed.cacheWrite > 0 {
					results = append(results, map[string]any{"line_item": billed.name + ", cache writes", "amount": billingAmount(billed.cacheWrite, billedTokensADay)})
				}
				results = append(results, map[string]any{"line_item": billed.name + ", output", "amount": billingAmount(billed.outputRate, billedOutputADay)})
			case "/v1/organization/usage/completions":
				counts := map[string]any{"model": billed.name, "output_tokens": billedOutputADay}
				if billed.inputRate > 0 {
					counts["input_uncached_tokens"] = billedTokensADay
				}
				if billed.cacheWrite > 0 {
					counts["input_cache_write_tokens"] = billedTokensADay
				}
				results = append(results, counts)
			}
		}
		data = append(data, map[string]any{"start_time": start, "results": results})
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"data": data, "has_more": false})
}

func newBillingFixture(test *testing.T) *billingFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)

	fixture := &billingFixture{database: database}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveBill))
	test.Cleanup(fixture.server.Close)

	configuration := config.Default()
	// What Update validates before it stores anything.
	configuration.Server.Name = "mail.example.com"
	configuration.TLS.ACME.Enabled = false
	configuration.Listen.HTTPS = ""
	configuration.Agent.Enabled = true
	dreamingOff := false
	configuration.Agent.Features.Dreaming = &dreamingOff
	configuration.Agent.Models.Default = "openai-test:alpha-model"
	configuration.Agent.Providers = []config.AgentProvider{{
		Name: "openai-test", Kind: config.AgentProviderKindOpenAI, APIKey: "model-key",
		BillingAPIKey: billingTestKey, BaseURL: fixture.server.URL + "/v1",
		Pricing: config.AgentPricing{Input: 1, Output: 4, CacheRead: 0.1},
		ModelPricing: []config.AgentModelPricing{
			{Model: "alpha-model", Input: 0.2, Output: 1.25, CacheRead: 0.02},
			{Model: "gamma-model", Output: 5},
			{Model: "delta-*", Output: 1},
		},
	}}
	fixture.store = config.NewMemoryStore(configuration)
	fixture.worker = fixture.newWorker()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "operators", Permissions: []models.Permission{models.PermissionServerManage}})
		if err != nil {
			test.Fatalf("CreateRole: %s", err)
		}
		operator, err := tx.CreateUser(&models.User{Username: "operator"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		other, err := tx.CreateUser(&models.User{Username: "member"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "operators", UserIDs: []string{operator.ID}, RoleIDs: []string{role.ID}}); err != nil {
			test.Fatalf("CreateGroup: %s", err)
		}
		operatorAgent, err := tx.CreateAgent(&models.Agent{UserID: operator.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		otherAgent, err := tx.CreateAgent(&models.Agent{UserID: other.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		fixture.operatorAgentID, fixture.nonOperatorAgentID = operatorAgent.ID, otherAgent.ID

		for _, billed := range billedModels {
			if !billed.hasUsedHere {
				continue
			}
			if err := tx.PutAgentUsage(&db.AgentUsage{
				AgentID: operatorAgent.ID, Model: "openai-test:" + billed.name, Kind: "turn", At: time.Now(),
				Values: []uint64{1000, 100, 0, 0, 1},
			}); err != nil {
				test.Fatalf("PutAgentUsage: %s", err)
			}
		}
		// The agent used the undated name of the snapshot the bill names,
		// and a model of another provider that the bill does not cover.
		for _, used := range []string{"openai-test:gamma-model", "elsewhere:never-used-model"} {
			if err := tx.PutAgentUsage(&db.AgentUsage{
				AgentID: operatorAgent.ID, Model: used, Kind: "turn", At: time.Now(),
				Values: []uint64{1000, 100, 0, 0, 1},
			}); err != nil {
				test.Fatalf("PutAgentUsage: %s", err)
			}
		}
	})
	return fixture
}

// newWorker is a worker over the fixture's database and store, as a second
// server instance, or the same one after a restart, would be.
func (self *billingFixture) newWorker() *agent.Agent {
	return agent.New(&agent.Settings{
		Database:            self.database,
		Configuration:       self.store.Current,
		UpdateConfiguration: self.store.Update,
		Instance:            "test",
		Tick:                time.Hour,
	})
}

func (self *billingFixture) provider() config.AgentProvider {
	return self.store.Current().Agent.Providers[0]
}

func (self *billingFixture) entry(test *testing.T, model string) *config.AgentModelPricing {
	test.Helper()
	provider := self.provider()
	for index := range provider.ModelPricing {
		if provider.ModelPricing[index].Model == model {
			return &provider.ModelPricing[index]
		}
	}
	return nil
}

func (self *billingFixture) told(test *testing.T, agentId string) []*models.AgentMessage {
	test.Helper()
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(agentId, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if err != nil {
			test.Fatalf("ListAgentConversations: %s", err)
		}
		for _, conversation := range conversations {
			found, err := tx.ListAgentMessages(conversation.ID, nil)
			if err != nil {
				test.Fatalf("ListAgentMessages: %s", err)
			}
			messages = append(messages, found...)
		}
	})
	return messages
}

func (self *billingFixture) auditedPrices(test *testing.T) []*models.AuditEvent {
	test.Helper()
	var events []*models.AuditEvent
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if events, err = tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourceConfiguration)}); err != nil {
			test.Fatalf("ListAuditEvents: %s", err)
		}
	})
	return events
}

func TestCheckProviderPricesFollowsTheBill(test *testing.T) {
	fixture := newBillingFixture(test)
	checks, err := fixture.worker.CheckProviderPrices(context.Background(), false)
	if err != nil {
		test.Fatalf("CheckProviderPrices: %s", err)
	}
	if len(checks) != 1 || checks[0].ErrorMessage != "" {
		test.Fatalf("expected one clean check, got %+v", checks)
	}

	// A zero cache write price on an exact entry is set.
	alpha := fixture.entry(test, "alpha-model")
	if alpha == nil || alpha.CacheWrite != 0.25 || alpha.Input != 0.2 || alpha.CacheRead != 0.02 {
		test.Errorf("alpha-model entry %+v: cache writes should now be 0.25, the rest as they were", alpha)
	}
	// An output price four percent under the bill's is left alone.
	if alpha != nil && alpha.Output != 1.25 {
		test.Errorf("alpha-model output changed to %v, within five percent", alpha.Output)
	}
	// A model with no entry gets one, seeded with the prices it was
	// charged at, so the kinds the bill did not speak about keep them.
	beta := fixture.entry(test, "beta-model")
	if beta == nil || beta.Output != 8 || beta.Input != 1 || beta.CacheRead != 0.1 || beta.CacheWrite != 0 {
		test.Errorf("beta-model entry %+v: output should be 8, the rest the provider's own", beta)
	}
	// A dated report name is written under the undated name the agent uses.
	gamma := fixture.entry(test, "gamma-model")
	if gamma == nil || gamma.Output != 10 {
		test.Errorf("gamma-model entry %+v: output should be 10", gamma)
	}
	if fixture.entry(test, "gamma-model-2026-03-17") != nil {
		test.Errorf("an entry was written under the dated name")
	}
	// A pattern is reported and not changed.
	if delta := fixture.entry(test, "delta-*"); delta == nil || delta.Output != 1 {
		test.Errorf("delta-* entry %+v changed", delta)
	}
	isPatternReported := false
	for _, compared := range checks[0].Compared {
		if compared.Model == "delta-one" && compared.TokenKind == "output" && compared.IsPattern && compared.ConfiguredPerMillion == 1 && compared.BilledPerMillion == 3 {
			isPatternReported = true
		}
	}
	if !isPatternReported {
		test.Errorf("the pattern's difference is not in what was compared: %+v", checks[0].Compared)
	}
	for _, change := range checks[0].Changed {
		if change.Model == "delta-one" {
			test.Errorf("a model under a pattern was changed: %+v", change)
		}
	}
	// A model the agent never used is left alone, and not even compared.
	if fixture.entry(test, "never-used-model") != nil {
		test.Errorf("an entry was written for a model the agent never used")
	}
	for _, compared := range checks[0].Compared {
		if compared.Model == "never-used-model" {
			test.Errorf("a model the agent never used was compared: %+v", compared)
		}
	}
	// alpha cache writes, beta output, gamma output; beta input matches.
	if len(checks[0].Changed) != 3 {
		test.Errorf("expected 3 changes, got %+v", checks[0].Changed)
	}

	// One audit event per changed model, holding the old and new price.
	events := fixture.auditedPrices(test)
	resourceIds := map[string]*models.AuditEvent{}
	for _, event := range events {
		resourceIds[event.ResourceID] = event
	}
	if len(events) != 3 {
		test.Fatalf("expected 3 audit events, got %d", len(events))
	}
	event := resourceIds["agent.providers/openai-test/alpha-model"]
	if event == nil || event.Action != models.AuditActionUpdate {
		test.Fatalf("alpha-model audit event %+v", event)
	}
	var beforePrices, afterPrices map[string]float64
	if err := json.Unmarshal(event.Before, &beforePrices); err != nil {
		test.Fatalf("audit event before: %s", err)
	}
	if err := json.Unmarshal(event.After, &afterPrices); err != nil {
		test.Fatalf("audit event after: %s", err)
	}
	if len(beforePrices) != 1 || beforePrices["cacheWrite"] != 0 || len(afterPrices) != 1 || afterPrices["cacheWrite"] != 0.25 {
		test.Errorf("alpha-model audit event holds %v then %v", beforePrices, afterPrices)
	}
	for _, resourceId := range []string{"agent.providers/openai-test/beta-model", "agent.providers/openai-test/gamma-model"} {
		if resourceIds[resourceId] == nil {
			test.Errorf("no audit event for %s", resourceId)
		}
	}

	// The operator is told once in the main conversation; a member is not.
	messages := fixture.told(test, fixture.operatorAgentID)
	if len(messages) != 2 || messages[1].Role != "assistant" {
		test.Fatalf("expected the check-in and the telling, got %+v", messages)
	}
	for _, want := range []string{
		"Prices now follow the provider's bill",
		"alpha-model cache writes: 0 USD → 0.25 USD per million tokens",
		"beta-model output: 4 USD → 8 USD per million tokens",
		"gamma-model output: 5 USD → 10 USD per million tokens",
	} {
		if !strings.Contains(messages[1].Content, want) {
			test.Errorf("the telling lacks %q:\n%s", want, messages[1].Content)
		}
	}
	if told := fixture.told(test, fixture.nonOperatorAgentID); len(told) != 0 {
		test.Errorf("a member was told of the price changes: %+v", told)
	}

	// The check is kept for the dashboard.
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		stored, err := tx.GetAgentBillingCheck("openai-test")
		if err != nil || stored == nil || len(stored.Changed) != 3 || len(stored.Compared) != len(checks[0].Compared) {
			test.Errorf("stored check %+v, %v", stored, err)
		}
		if missing, err := tx.GetAgentBillingCheck("no-such-provider"); err != nil || missing != nil {
			test.Errorf("a provider never checked answered %+v, %v", missing, err)
		}
	})

	// A second check the same day does nothing, here or after a restart;
	// a forced one runs, finds the prices now right, and says nothing.
	requestsBefore := fixture.requestCount.Load()
	for _, worker := range []*agent.Agent{fixture.worker, fixture.newWorker()} {
		again, err := worker.CheckProviderPrices(context.Background(), false)
		if err != nil || len(again) != 0 {
			test.Errorf("an unforced second check answered %+v, %v", again, err)
		}
	}
	if fixture.requestCount.Load() != requestsBefore {
		test.Errorf("an unforced second check read the bill again")
	}
	forced, err := fixture.worker.CheckProviderPrices(context.Background(), true)
	if err != nil || len(forced) != 1 || len(forced[0].Changed) != 0 {
		test.Errorf("a forced check answered %+v, %v", forced, err)
	}
	if fixture.requestCount.Load() == requestsBefore {
		test.Errorf("a forced check did not read the bill")
	}
	if len(fixture.auditedPrices(test)) != 3 || len(fixture.told(test, fixture.operatorAgentID)) != 2 {
		test.Errorf("a check that changed nothing wrote an audit event or told an operator")
	}
}

func TestCheckProviderPricesRefusedKey(test *testing.T) {
	fixture := newBillingFixture(test)
	fixture.isRefusing.Store(true)
	before := fixture.provider()

	checks, err := fixture.worker.CheckProviderPrices(context.Background(), true)
	if err != nil {
		test.Fatalf("CheckProviderPrices: %s", err)
	}
	if len(checks) != 1 || !strings.Contains(checks[0].ErrorMessage, "billing key was refused") || len(checks[0].Changed) != 0 {
		test.Fatalf("expected the refusal as the check's error, got %+v", checks)
	}
	after := fixture.provider()
	if len(after.ModelPricing) != len(before.ModelPricing) || fixture.entry(test, "alpha-model").CacheWrite != 0 {
		test.Errorf("a refused key changed the prices: %+v", after.ModelPricing)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		stored, err := tx.GetAgentBillingCheck("openai-test")
		if err != nil || stored == nil || !strings.Contains(stored.ErrorMessage, "billing key was refused") {
			test.Errorf("stored check %+v, %v", stored, err)
		}
	})
	if len(fixture.auditedPrices(test)) != 0 || len(fixture.told(test, fixture.operatorAgentID)) != 0 {
		test.Errorf("a refused key wrote an audit event or told an operator")
	}
}
