package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// digestSplitMarker is in the title of every document the tests file, so
// that the model standing in for the provider can count how many of them
// a request carried.
const digestSplitMarker = "digest-split-document-"

// digestSplitOperations is the API as the person, for a run nobody
// started: every call of a dream is a turn of the loop, and a turn needs
// somebody to act as. The reading asks nothing of the mailbox, so every
// document is answered with an empty object.
type digestSplitOperations struct{}

func (self *digestSplitOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions(nil)
}

func (self *digestSplitOperations) Execute(ctx context.Context, document string, variables map[string]any, result any) error {
	if result == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{}`), result)
}

// digestSplitCall is one request the model standing in for the provider
// was given: how many of the test's documents it carried, and whether it
// was refused for being too long.
type digestSplitCall struct {
	documents int
	refused   bool
}

// digestSplitProvider is a model with a small window. A request carrying
// more than most of the test's documents is refused the way llama.cpp
// refuses one -- 400, and a message naming the context size -- and
// anything smaller is answered with the object a reading ends with.
func digestSplitProvider(most int) (*httptest.Server, func() []digestSplitCall) {
	var mutex sync.Mutex
	var calls []digestSplitCall
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		shown := 0
		for _, message := range body.Messages {
			shown += strings.Count(fmt.Sprint(message["content"]), digestSplitMarker)
		}
		mutex.Lock()
		calls = append(calls, digestSplitCall{documents: shown, refused: shown > most})
		mutex.Unlock()
		if shown > most {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"the request (33012 tokens) exceeds the available context size (32768 tokens)"}}`))
			return
		}
		answer, _ := json.Marshal(`{"facts": []}`)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				answer)
			return
		}
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			answer)
	}))
	return server, func() []digestSplitCall {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]digestSplitCall(nil), calls...)
	}
}

// digestSplitWorld is an agent whose one model is the given provider, the
// person who owns it, and a run to read in.
func digestSplitWorld(t *testing.T, database db.Database, providerURL string) (*Agent, *Run) {
	t.Helper()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	// Whether a night is due depends on the wall clock, and this test
	// calls the reading itself rather than waiting for one.
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: providerURL, APIKey: "k"}}
	configuration.Agent.Models.Default = "p:thinker"
	configuration.Agent.Models.Scan = "p:scan"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := New(&Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	worker.SetOperationsFactory(func(context.Context, *models.User) (Operations, error) {
		return &digestSplitOperations{}, nil
	})

	var owner *models.User
	var person *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
	})
	return worker, &Run{Agent: person, Owner: owner, Now: time.Now(), settings: worker.settings}
}

// digestSplitDocuments files a source with the given number of documents
// in it, which is the batch the reading is handed.
func digestSplitDocuments(t *testing.T, database db.Database, run *Run, count int) []*models.AgentDocument {
	t.Helper()
	documents := make([]*models.AgentDocument, 0, count)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		for index := range count {
			name := fmt.Sprintf("%s%d", digestSplitMarker, index+1)
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: run.Agent.ID, SourceID: source.ID, ExternalID: name,
				Kind: models.DocumentChat, Title: name, Hash: "hash-of-" + name,
			})
			if err != nil {
				t.Fatalf("PutAgentDocument: %s", err)
			}
			documents = append(documents, document)
		}
	})
	return documents
}

// A batch the model's context cannot hold is read as two halves.
//
// Twenty openings of twelve hundred runes are a few hundred tokens over a
// thirty-two thousand window when the documents are written in Chinese,
// and the whole batch used to be marked read on the strength of that one
// refusal: eleven batches, two hundred and twenty documents, gone in two
// nights and never to be read again. Halves of such a batch fit.
func TestABatchTooLongForTheModelIsSplitInTwo(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := digestSplitProvider(2)
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := digestSplitDocuments(t, database, run, 4)

	_, answered := worker.digestBatch(context.Background(), run, documents, &dreamBudget{}, false)
	if !answered {
		t.Fatal("a batch whose halves the model answered is answered, and its documents are marked read")
	}

	made := calls()
	if len(made) < 2 {
		t.Fatalf("the batch that was refused is asked again in halves, so there is more than one call: %d", len(made))
	}
	overflowed, read := 0, 0
	for _, call := range made {
		if call.refused {
			overflowed++
			if call.documents != len(documents) {
				t.Fatalf("only the whole batch is too long, and this call carried %d documents", call.documents)
			}
			continue
		}
		read++
		// The halving is the whole point: nothing the model answered may
		// be as large as what it refused.
		if call.documents > 2 {
			t.Fatalf("a call the model answered carried %d documents, which is more than its window holds", call.documents)
		}
	}
	if overflowed == 0 {
		t.Fatal("the batch was never refused, so this proves nothing about what happens when it is")
	}
	if read != 2 {
		t.Fatalf("each half is read, and only once: %d calls were answered", read)
	}
}

// One document that on its own does not fit is still skipped: there is no
// half of it to try, and a reading that came back to it every night would
// never get past it.
func TestOneDocumentTooLongForTheModelIsSkippedRatherThanSplit(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, calls := digestSplitProvider(0)
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := digestSplitDocuments(t, database, run, 1)

	filed, answered := worker.digestBatch(context.Background(), run, documents, &dreamBudget{}, false)
	if !answered || filed != 0 {
		t.Fatalf("a document nothing can be done with counts as read and files nothing: %d %v", filed, answered)
	}
	// Nothing was read, and nothing was asked a second time in smaller
	// pieces: one document is the smallest a batch gets. A refusal is two
	// calls rather than one because the loop, told the stream could not
	// be opened, asks once more without streaming.
	made := calls()
	if len(made) == 0 || len(made) > 2 {
		t.Fatalf("the one document is put to the model once, and streaming or not is the only reason to ask twice: %d calls", len(made))
	}
	for _, call := range made {
		if !call.refused || call.documents != 1 {
			t.Fatalf("every call is the one document and every one of them is refused: %+v", call)
		}
	}
}
