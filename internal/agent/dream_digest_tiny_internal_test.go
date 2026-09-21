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

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// What the night reads and what it marks read without reading.
//
// Marking read is the one thing the reading does that cannot be undone
// by tomorrow: nothing is offered twice, so a document stamped read
// without a call is a document nobody will ever read. The rule that
// skips the ones too slight to be worth a call is therefore worth a test
// through the phase, against a database, rather than at the arithmetic.

// tinyProvider is a model that answers every reading with no facts and
// keeps what it was shown, so that a test can say which documents were
// put to it and which were never offered at all.
func tinyProvider() (*httptest.Server, func() []string) {
	var mutex sync.Mutex
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		var shown strings.Builder
		for _, message := range body.Messages {
			_, _ = fmt.Fprintf(&shown, "%v\n", message["content"])
		}
		mutex.Lock()
		prompts = append(prompts, shown.String())
		mutex.Unlock()
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
	return server, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), prompts...)
	}
}

// tinyDocument is one thing a source filed: what kind of thing it is, the
// size the source recorded for it, and the text of its passages.
type tinyDocument struct {
	name  string
	kind  models.AgentDocumentKind
	bytes int64
	text  string
}

// fileTinyDocuments writes the given documents down as a source's, with
// their passages, and answers them by name.
func fileTinyDocuments(t *testing.T, database db.Database, run *Run, files []tinyDocument) map[string]*models.AgentDocument {
	t.Helper()
	documents := map[string]*models.AgentDocument{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceArchive, Name: "the checkouts", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/projects", Format: models.FormatFiles,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		happened := time.Date(2026, 9, 17, 5, 1, 0, 0, time.UTC)
		for _, file := range files {
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: run.Agent.ID, SourceID: source.ID, ExternalID: file.name,
				Kind: file.kind, Title: file.name, Hash: "hash-of-" + file.name,
				Bytes: file.bytes, HappenedAt: &happened,
				Metadata: map[string]any{"author": "Alice Example"},
			})
			if err != nil {
				t.Fatalf("PutAgentDocument %q: %s", file.name, err)
			}
			if file.text != "" {
				if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: file.text}}); err != nil {
					t.Fatalf("ReplaceAgentChunks %q: %s", file.name, err)
				}
			}
			documents[file.name] = document
		}
	})
	return documents
}

// wasOffered says whether any call carried this document.
func wasOffered(prompts []string, document *models.AgentDocument) bool {
	for _, prompt := range prompts {
		if strings.Contains(prompt, document.ID) {
			return true
		}
	}
	return false
}

// A commit is read, though nothing ever recorded a size for one.
//
// A commit is not a file: nothing stats it, and its words -- the subject,
// the body, the files it touched -- live in its passages. The reading
// used to ask the recorded size how much a document held, so every
// commit answered zero and was taken for a document with nothing in it:
// on the deployment this was written for, 1,874 of them were stamped
// read in a single minute without one call, and a commit is the only
// document that carries an author, so no authorship could be worked out
// from an archive at all.
func TestACommitWithNoSizeIsPutToTheModelRatherThanStampedRead(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, prompts := tinyProvider()
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := fileTinyDocuments(t, database, run, []tinyDocument{{
		name: "a commit of theirs", kind: models.DocumentCommit,
		text: "Obtain certificates without a cloud account\n\n" +
			"The renewal loop used to be started by the constructor, so a\n" +
			"test that built a manager contacted the authority.\n\n" +
			"Files: internal/util/autoacme/manager.go",
	}})

	worker.dreamDigest(context.Background(), run, &models.AgentDream{}, &dreamBudget{})

	commit := documents["a commit of theirs"]
	if !wasOffered(prompts(), commit) {
		t.Fatal("the commit was never put to the model, though its passages carry everything it says")
	}
	if !digested(t, database, commit) {
		t.Error("a document the model has read is marked read")
	}
}

// A document that really says nothing is still marked read without a
// call, which is what the rule was written for.
func TestADocumentThatSaysNothingIsMarkedReadWithoutACall(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider, prompts := tinyProvider()
	defer provider.Close()

	worker, run := digestSplitWorld(t, database, provider.URL)
	documents := fileTinyDocuments(t, database, run, []tinyDocument{
		{name: "a file of twenty bytes", kind: models.DocumentFile, bytes: 20, text: "a line, and no more\n"},
		{name: "a document with no passages", kind: models.DocumentFile},
	})

	worker.dreamDigest(context.Background(), run, &models.AgentDream{}, &dreamBudget{})

	if made := prompts(); len(made) > 0 {
		t.Fatalf("nothing here is worth a share of a call, and %d were made", len(made))
	}
	for name, document := range documents {
		if !digested(t, database, document) {
			t.Errorf("%q says nothing and is marked read rather than offered again every night", name)
		}
	}
}
