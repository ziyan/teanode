package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// An embedding model that answers with nothing stops the pass.
//
// The loop asked for the passages with no vector, embedded them, wrote
// what came back and went round again until it had written its quota. A
// provider that answers -- so there is no error to stop on -- with an
// empty vector for every passage wrote nothing, so the quota never
// filled: the same batch was fetched and sent again for as long as the
// job had, thousands of times, against the person's account.
func TestEmbeddingStopsWhenTheModelAnswersWithNothing(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var asked int32
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&asked, 1)
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		data := make([]map[string]any, 0, len(body.Input))
		for index := range body.Input {
			data = append(data, map[string]any{"index": index, "embedding": []float32{}})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"data": data, "usage": map[string]int{"prompt_tokens": 7, "total_tokens": 7},
		})
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	configuration.Agent.Models.Embedding = "fake:embedder"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	worker := New(&Settings{
		Database: database, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var person *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: person.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: person.ID, SourceID: source.ID, ExternalID: "monday.jsonl",
			Kind: models.DocumentChat, Title: "Monday", Hash: "hash-of-monday",
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{
			{Text: "what was said on Monday", Segmented: true},
			{Text: "and what was said after it", Segmented: true},
		}); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})

	written, left, err := worker.embedChunks(context.Background(), person, 100)
	if err != nil {
		t.Fatalf("embedChunks: %s", err)
	}
	if written != 0 {
		t.Fatalf("nothing came back to write: %d", written)
	}
	if left != 2 {
		t.Fatalf("both passages are still waiting: %d", left)
	}
	if rounds := atomic.LoadInt32(&asked); rounds != 1 {
		t.Fatalf("the provider is asked once and the pass gives up, not %d times", rounds)
	}
}
