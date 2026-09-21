package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

func TestGraphEmbeddingRejectsChangedInputs(test *testing.T) {
	for _, change := range []string{"page", "fact", "delete"} {
		test.Run(change, func(test *testing.T) {
			database, worker, _, source := ingestionPageFixture(test)
			var person *models.Agent
			var page *models.AgentNode
			var fact *models.AgentFact
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				person, err = transaction.GetAgent(source.AgentID)
				if err != nil {
					test.Fatal(err)
				}
				if err := transaction.EnsureAgentRoots(person.ID); err != nil {
					test.Fatal(err)
				}
				page, err = transaction.PutAgentNode(&models.AgentNode{AgentID: person.ID, Path: "projects/fixture", Kind: models.NodeProject, Name: "Original fixture"})
				if err != nil {
					test.Fatal(err)
				}
				fact, err = transaction.AddAgentFact(&models.AgentFact{AgentID: person.ID, NodeID: page.ID, Kind: models.FactPlain, Text: "Original statement.", Confidence: 1})
				if err != nil {
					test.Fatal(err)
				}
			})
			var requestCount atomic.Int64
			providerErrors := make(chan error, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var body struct {
					Input []string `json:"input"`
				}
				_ = json.NewDecoder(request.Body).Decode(&body)
				if requestCount.Add(1) == 1 {
					err := database.Transaction(func(transaction db.Transaction) error {
						switch change {
						case "page":
							page.Name = "Changed fixture"
							_, err := transaction.PutAgentNode(page)
							return err
						case "fact":
							_, err := transaction.UpdateAgentFact(person.ID, fact.ID, func(current *models.AgentFact) error { current.Text = "Changed statement."; return nil })
							return err
						default:
							_, err := transaction.DeleteAgentNode(person.ID, page.Path)
							return err
						}
					})
					if err != nil {
						providerErrors <- err
						http.Error(writer, "fixture edit failed", 500)
						return
					}
				}
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
			configuration.Agent.Models.Embedding = "fixture:meaning"
			registry, err := llm.Open(&configuration.Agent)
			if err != nil {
				test.Fatal(err)
			}
			worker.settings.Registry = registry
			worker.settings.Configuration = func() *config.Configuration { return configuration }
			writtenCount, err := worker.EmbedGraph(test.Context(), person, 100)
			if err != nil || writtenCount == 0 {
				test.Fatalf("first batch=%d: %v", writtenCount, err)
			}
			select {
			case err := <-providerErrors:
				test.Fatal(err)
			default:
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_fact_vector`); count != "0" {
				test.Fatalf("stale fact vectors=%s", count)
			}
			if change != "fact" {
				if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_node_vector WHERE node_id = '`+page.ID+`'`); count != "0" {
					test.Fatalf("stale page vectors=%s", count)
				}
			}
			count := dbtest.QueryString(test, database, `SELECT ((SELECT count(*) FROM agent_node_vector) + (SELECT count(*) FROM agent_fact_vector))::text`)
			if count != strconv.Itoa(writtenCount) {
				test.Fatalf("reported %d writes, stored %s", writtenCount, count)
			}
			writtenCount, err = worker.EmbedGraph(test.Context(), person, 100)
			expectedCount := 2
			if change == "fact" {
				expectedCount = 1
			}
			if change == "delete" {
				expectedCount = 0
			}
			if err != nil || writtenCount != expectedCount {
				test.Fatalf("retry=%d, expected %d: %v", writtenCount, expectedCount, err)
			}
		})
	}
}
