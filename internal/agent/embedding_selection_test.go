package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

func TestEmbeddingKeepsRegistryIdentityUntilRestart(test *testing.T) {
	for _, pendingModel := range []string{"fixture:replacement", ""} {
		test.Run("pending="+pendingModel, func(test *testing.T) {
			database, worker, run, source, _ := sentPageFixture(test, 1)
			var requestCount atomic.Int64
			var hasWrongSelection atomic.Bool
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body struct {
					Model      string   `json:"model"`
					Dimensions int      `json:"dimensions"`
					Input      []string `json:"input"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					hasWrongSelection.Store(true)
				}
				if body.Model != "original" || body.Dimensions != 3 {
					hasWrongSelection.Store(true)
				}
				requestCount.Add(1)
				data := make([]map[string]any, len(body.Input))
				for index := range body.Input {
					data[index] = map[string]any{"index": index, "embedding": []float32{1, 0, 0}}
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"data": data})
			}))
			defer provider.Close()
			configuration := config.Default()
			configuration.Agent.Enabled = true
			configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai", BaseURL: provider.URL, APIKey: "fixture"}}
			configuration.Agent.Models.Embedding = "fixture:original"
			configuration.Agent.Models.EmbeddingDimensions = 3
			registry, err := llm.Open(&configuration.Agent)
			if err != nil {
				test.Fatal(err)
			}
			worker.settings.Registry = registry
			worker.settings.Configuration = func() *config.Configuration { return configuration }
			configuration.Agent.Models.Embedding = pendingModel
			configuration.Agent.Models.EmbeddingDimensions = 5
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				run.Agent, err = transaction.GetAgent(source.AgentID)
				if err != nil {
					test.Fatal(err)
				}
				run.Owner, err = transaction.GetUser(run.Agent.UserID)
				if err != nil {
					test.Fatal(err)
				}
				run.Mailbox, err = transaction.GetMailbox(source.Specification.MailboxID)
				if err != nil {
					test.Fatal(err)
				}
				if err := transaction.EnsureAgentRoots(run.Agent.ID); err != nil {
					test.Fatal(err)
				}
			})
			run.Source = &models.AgentMailbox{Granted: true, Search: true}
			mailId := dbtest.QueryString(test, database, `SELECT id FROM mail LIMIT 1`)
			run.Job = &models.AgentJob{SubjectID: mailId}
			if err := worker.runEmbed(test.Context(), run); err != nil {
				test.Fatal(err)
			}
			if name := dbtest.QueryString(test, database, `SELECT model FROM mail_embedding LIMIT 1`); name != "fixture:original@3" {
				test.Fatalf("mail vector model=%q", name)
			}
			found, err := worker.meaningSearch(test.Context(), run.Agent, run.Mailbox.ID, "fixture", 10)
			if err != nil || len(found) != 1 || found[0] != mailId {
				test.Fatalf("search=%v, %v", found, err)
			}
			if err := worker.backfillEmbeddings(test.Context(), run); err != nil {
				test.Fatal(err)
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_job`); count != "0" {
				test.Fatalf("backfill queued %s jobs for completed vectors", count)
			}
			if _, _, err := worker.readSentMail(test.Context(), run, source, map[string]any{"before": "2031-01-01T00:00:00Z"}); err != nil {
				test.Fatal(err)
			}
			writtenCount, leftCount, err := worker.embedChunks(test.Context(), run.Agent, 10)
			if err != nil || writtenCount != 1 || leftCount != 0 {
				test.Fatalf("chunks=%d left=%d: %v", writtenCount, leftCount, err)
			}
			if name := dbtest.QueryString(test, database, `SELECT model FROM agent_chunk_vector LIMIT 1`); name != "fixture:original@3" {
				test.Fatalf("chunk vector model=%q", name)
			}
			writtenCount, err = worker.EmbedGraph(test.Context(), run.Agent, 100)
			if err != nil || writtenCount == 0 {
				test.Fatalf("graph=%d: %v", writtenCount, err)
			}
			if name := dbtest.QueryString(test, database, `SELECT DISTINCT model FROM agent_node_vector`); name != "fixture:original@3" {
				test.Fatalf("graph vector model=%q", name)
			}
			if hasWrongSelection.Load() || requestCount.Load() != 4 {
				test.Fatalf("wrong selection=%t, requests=%d", hasWrongSelection.Load(), requestCount.Load())
			}
		})
	}
}
