package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

type pageTransactionDatabase struct {
	db.Database
	activeCount atomic.Int64
}

func (self *pageTransactionDatabase) Transaction(function func(db.Transaction) error) error {
	return self.TransactionContext(context.Background(), function)
}

func (self *pageTransactionDatabase) TransactionContext(ctx context.Context, function func(db.Transaction) error) error {
	return self.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		self.activeCount.Add(1)
		defer self.activeCount.Add(-1)
		return function(transaction)
	})
}

func TestMemoryNotePreparesEmbeddingBeforeTransaction(test *testing.T) {
	for _, fixture := range []struct {
		name                  string
		isProviderUnavailable bool
		isEmbeddingDisabled   bool
		shouldRejectFact      bool
	}{
		{name: "embedded"},
		{name: "embedding disabled", isEmbeddingDisabled: true},
		{name: "word fallback", isProviderUnavailable: true},
		{name: "write rollback", shouldRejectFact: true},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(test)
			defer closeDatabase()
			tracked := &pageTransactionDatabase{Database: database}
			var requestCount atomic.Int64
			var hasOpenTransaction atomic.Bool
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requestCount.Add(1)
				if tracked.activeCount.Load() != 0 {
					hasOpenTransaction.Store(true)
				}
				if fixture.isProviderUnavailable {
					http.Error(writer, "fixture provider unavailable", http.StatusServiceUnavailable)
					return
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": []float32{0.2, 0.4, 0.8}}}})
			}))
			defer provider.Close()
			configuration := config.Default()
			configuration.Agent.Enabled = true
			configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai", BaseURL: provider.URL, APIKey: "fixture"}}
			configuration.Agent.Models.Default = "fixture:writer"
			if !fixture.isEmbeddingDisabled {
				configuration.Agent.Models.Embedding = "fixture:embedding"
			}
			registry, err := llm.Open(&configuration.Agent)
			if err != nil {
				test.Fatal(err)
			}
			var owner *models.User
			var person *models.Agent
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				owner, err = transaction.CreateUser(&models.User{Username: "fixture-owner"})
				if err != nil {
					test.Fatal(err)
				}
				person, err = transaction.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
				if err != nil {
					test.Fatal(err)
				}
			})
			if fixture.shouldRejectFact {
				dbtest.Exec(test, database, `ALTER TABLE agent_fact ADD CONSTRAINT fixture_reject_fact CHECK (text <> 'Fixture statement.')`)
			}
			worker := &Agent{settings: &Settings{Database: tracked, Registry: registry, Configuration: func() *config.Configuration { return configuration }}}
			run := &AskRun{agent: worker, settings: &AskSettings{Agent: person, Owner: owner}}
			var memoryTool *tools.Tool
			for _, candidate := range tools.Build().All() {
				if candidate.Name == "memory" {
					memoryTool = candidate
					break
				}
			}
			if memoryTool == nil {
				test.Fatal("memory tool is missing")
			}
			_, err = memoryTool.Run(tools.WithRun(test.Context(), run), &tools.Call{ID: "fixture-call", Arguments: []byte(`{"action":"note","path":"projects/fixture","kind":"project","name":"Fixture","text":"Fixture statement."}`)})
			if (err != nil) != fixture.shouldRejectFact {
				test.Fatalf("note error=%v, expected rejection=%v", err, fixture.shouldRejectFact)
			}
			if (requestCount.Load() == 0) != fixture.isEmbeddingDisabled || hasOpenTransaction.Load() {
				test.Fatalf("embedding requests=%d, called inside transaction=%v", requestCount.Load(), hasOpenTransaction.Load())
			}
			pageCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_node WHERE path = 'projects/fixture'`)
			factCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_fact`)
			if fixture.shouldRejectFact {
				if pageCount != "0" || factCount != "0" {
					test.Fatalf("failed note retained page=%s, fact=%s", pageCount, factCount)
				}
			} else if pageCount != "1" || factCount != "1" {
				test.Fatalf("note persisted page=%s, fact=%s", pageCount, factCount)
			}
		})
	}
}
