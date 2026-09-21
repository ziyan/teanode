package agent_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// A turn embeds its question once, and gives nobody's vectors a backfill
// while the person waits.
//
// Both halves of recall rank by the same words -- the graph on one side,
// the person's own documents on the other -- and each used to embed them
// for itself. On top of that the turn backfilled twenty rows that had no
// vector, which is a call to another service per batch for rows this turn
// was never going to read; the night does that now, two hundred at a
// time, with nobody waiting.
func TestATurnEmbedsItsQuestionOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var mutex sync.Mutex
	var embedded [][]string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/embeddings") {
			var body struct {
				Input []string `json:"input"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			mutex.Lock()
			embedded = append(embedded, body.Input)
			mutex.Unlock()
			data := make([]map[string]any, 0, len(body.Input))
			for index := range body.Input {
				data = append(data, map[string]any{"index": index, "embedding": []float32{0.1, 0.2, 0.3}})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 7, "total_tokens": 7}})
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`{"id":"s1","model":"m","choices":[{"delta":{"content":"She works on Portal."},"finish_reason":"stop"}]}`,
			`{"id":"s1","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":5}}`,
		} {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// No night: whether one is due depends on the hour the test runs at,
	// and a night embeds on purpose.
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	configuration.Agent.Models.Embedding = "fake:embedder"
	configuration.Agent.Models.EmbeddingDimensions = 0
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})

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
		// A page with no vector, which is what the turn used to stop and
		// embed on its way to the model.
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: found.ID, Path: "projects/portal",
			Kind: models.NodeProject, Name: "Portal", Summary: "The site customers log in to."}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}

	run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations,
		Conversation: conversation, Message: "what is Alice working on?", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	collect(run)

	mutex.Lock()
	calls := append([][]string(nil), embedded...)
	mutex.Unlock()
	if len(calls) != 1 {
		t.Fatalf("one embedding call for the turn's question, and got %d: %v", len(calls), calls)
	}
	if len(calls[0]) != 1 || !strings.Contains(calls[0][0], "what is Alice working on?") {
		t.Fatalf("and it is the question: %v", calls[0])
	}

	// The page still has no vector: the night writes it, not the person's
	// turn.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		missing, err := tx.ListAgentNodesWithoutVector(found.ID, "fake:embedder", 10)
		if err != nil {
			t.Fatalf("ListAgentNodesWithoutVector: %s", err)
		}
		var paths []string
		for _, node := range missing {
			paths = append(paths, node.Path)
		}
		if !slicesContain(paths, "projects/portal") {
			t.Fatalf("the turn embedded the graph on its way past: %v", paths)
		}
	})
}

func slicesContain(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
