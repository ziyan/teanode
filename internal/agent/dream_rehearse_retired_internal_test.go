package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// oneDirection is a model that embeds everything the same way, so that
// every fact is equally near every question and what comes back is decided
// by the narrowing rather than by the distance. It answers a question put
// in words with yes, so a fact reaching the judge is a fact the night
// would have reported an answer from.
func oneDirection(t *testing.T) (*llm.Registry, *config.Configuration, func()) {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Input    []string `json:"input"`
			Messages []any    `json:"messages"`
			Stream   bool     `json:"stream"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		// By the path, not by what decoded: an embedding request whose
		// input did not decode would otherwise be answered with a chat
		// completion, and the search would fail for a reason that has
		// nothing to do with what is being tested.
		if strings.Contains(request.URL.Path, "embeddings") {
			data := make([]map[string]any, 0, len(body.Input))
			for index := range body.Input {
				vector := make([]float32, 8)
				vector[0] = 1
				data = append(data, map[string]any{"index": index, "embedding": vector})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"data": data, "usage": map[string]int{"prompt_tokens": 7, "total_tokens": 7},
			})
			return
		}
		// A yes that points at the first fact, which is what the judge
		// needs before it will say memory answered. Streamed when the
		// caller asked for a stream: a completion sent as plain JSON to a
		// client expecting events fails, and the verdict comes back as
		// unknown for a reason that has nothing to do with the test.
		verdict := `{"answered":true,"facts":[1]}`
		encoded, _ := json.Marshal(verdict)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n",
				encoded)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer,
			`{"id":"c1","model":"m","choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5}}`,
			encoded)
	}))

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	configuration.Agent.Models.Embedding = "fake:embedder"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		provider.Close()
		t.Fatalf("llm.Open: %s", err)
	}
	return registry, configuration, provider.Close
}

// A line the page has retired cannot answer a question.
//
// Recall drops a struck or superseded fact before it reaches anybody.
// Rehearsal did not: it took the nearest few straight from the search and
// handed them to its judge, so a night could report that memory answers a
// question out of a statement the page had already taken down. The person
// is then told they have an answer they do not have, which is worse than
// being told they have none.
func TestRehearsalDoesNotAnswerFromARetiredFact(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	// The same configuration the registry was built from: a fresh default
	// names no embedding model, so the search would never be made.
	registry, configuration, closeProvider := oneDirection(test)
	defer closeProvider()

	worker := New(&Settings{
		Database: database, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var person *models.Agent
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "projects/rivermouth", Kind: models.NodeProject, Name: "Rivermouth",
		})
		if err != nil {
			test.Fatalf("PutAgentNode: %s", err)
		}
		fact, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: person.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text:      "Promised the Rivermouth team a build by Friday.",
			Audiences: []models.AgentAudience{models.AudienceAsk},
		})
		if err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
		// Written into the vector store the way the night writes one, so
		// the search can reach it at all.
		if err := tx.PutAgentFactVector(person.ID, fact.ID, "fake:embedder", oneVector()); err != nil {
			test.Fatalf("PutVector: %s", err)
		}
		// And then taken off the page.
		if _, err := tx.StrikeAgentFact(person.ID, fact.ID, "it was withdrawn"); err != nil {
			test.Fatalf("StrikeAgentFact: %s", err)
		}
	})

	run := &Run{Agent: person}
	got := worker.canAnswerFromMemory(context.Background(), run,
		&dreamBudget{}, "what did I promise the Rivermouth team?")
	// A graph whose only fact has been retired has nothing to answer
	// with, so the verdict is a gap and the judge is never asked.
	//
	// What this proves and what it does not: without the narrowing the
	// retired fact reaches the judge and the verdict is no longer a gap,
	// which is the regression. It does not get as far as the judge saying
	// yes -- the model here is a stub and the verdict it produces in that
	// state is unknown rather than answered -- so the harm as a person
	// would meet it, being told of an answer that is not there, is one
	// step past what this harness shows.
	if got != rehearsalGap {
		test.Fatalf("the retired fact was taken into account: verdict %d", got)
	}
}

func oneVector() []float32 {
	vector := make([]float32, 8)
	vector[0] = 1
	return vector
}
