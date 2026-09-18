package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A retrieval that did not happen is not a graph with nothing in it.
//
// Rehearsal asked whether an embedding model was configured, which is a
// different question from whether the search worked. A provider that
// timed out, a model that answered with nothing, a database that dropped
// the connection -- all were configured, so all came back as a graph that
// had been asked and had nothing. Those go in front of the person as
// questions their agent cannot answer, and every one of them sends them
// chasing an answer it already has.
func TestRehearsalCannotCallAFailedRetrievalAGap(t *testing.T) {
	// No registry, so nothing to ask with. The path is the same one a
	// provider that does not answer takes.
	configuration := config.Default()
	worker := &Agent{settings: &Settings{
		Configuration: func() *config.Configuration { return configuration },
	}}
	run := &Run{Agent: &models.Agent{ID: "agent"}}
	if got := worker.canAnswerFromMemory(context.Background(), run, &dreamBudget{allowed: 1000}, "what did I promise the Osaka team?"); got != rehearsalUnknown {
		t.Fatalf("a search that could not be made is unknown, not %d", got)
	}
}

// And a retrieval that did happen and found nothing is a gap, which is
// the verdict the phase exists to produce.
func TestRehearsalCallsAnEmptyGraphAGap(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		data := make([]map[string]any, 0, len(body.Input))
		for index := range body.Input {
			vector := make([]float32, 8)
			vector[0] = 1
			data = append(data, map[string]any{"index": index, "embedding": vector})
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
		// A graph with pages and no facts: the search runs, and a page
		// says its subject exists rather than answering anything.
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
	})

	run := &Run{Agent: person}
	if got := worker.canAnswerFromMemory(context.Background(), run, &dreamBudget{allowed: 1000}, "what did I promise the Osaka team?"); got != rehearsalGap {
		t.Fatalf("a graph that was asked and had nothing is a gap, not %d", got)
	}
}
