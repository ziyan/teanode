package apigraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A search for photos embeds its words, which takes as long as the model takes,
// and so a listing or a count that has words is read in phases: no database
// transaction is open while the model is asked.
func TestSearchingPhotosEmbedsTheWordsWithNoTransactionOpen(test *testing.T) {
	world := newPhotoWorld(test)
	watched := &queryTransactionDatabase{Database: world.database}
	world.graph.database = watched

	var mutex sync.Mutex
	var openDuringEmbeddings []int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		mutex.Lock()
		openDuringEmbeddings = append(openDuringEmbeddings, watched.activeCount.Load())
		mutex.Unlock()
		data := make([]map[string]any, 0, len(body.Input))
		for index := range body.Input {
			data = append(data, map[string]any{"index": index, "embedding": []float32{1, 0, 0}})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	test.Cleanup(server.Close)
	configuration := world.config.Current()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: server.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "p:thinker"
	configuration.Agent.Models.Embedding = "p:embedder"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	worker := agent.New(&agent.Settings{Database: world.database, Registry: registry, Configuration: world.config.Current, Instance: "test", Tick: time.Hour})
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return nil, nil })
	world.graph.settings = &api.Settings{Agent: worker}
	world.withSchema()

	allowed := []models.Permission{models.PermissionPhotosUse, models.PermissionAgentUse}
	ownerId := world.person("search-embed-owner", allowed...)
	world.giveAgent(ownerId, true)
	lighthouse := world.storePhoto(ownerId, "lighthouse", "IMG_1.jpg", []byte("one"), nil)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoDescription(ownerId, lighthouse.ID, &db.PhotoDescription{PhotoDescription: "A red lighthouse by the sea.", ModelName: "p:embedder"}); err != nil {
			test.Fatal(err)
		}
	})

	execute := func(document string) string {
		body, _ := json.Marshal(map[string]any{"query": document, "variables": map[string]any{"searchText": "lighthouse"}})
		response := world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
			asSession("search-embed-owner")(request)
			request.Header.Set("Content-Type", "application/json")
		}, body)
		return readBody(test, response)
	}
	if text := execute(client.DocumentListPhotos); !strings.Contains(text, lighthouse.ID) || strings.Contains(text, `"errors"`) {
		test.Fatalf("the search did not find the photo: %s", text)
	}
	if text := execute(client.DocumentCountPhotos); !strings.Contains(text, `"CountPhotos":1`) {
		test.Fatalf("the count: %s", text)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(openDuringEmbeddings) == 0 {
		test.Fatal("the words were never embedded")
	}
	for _, open := range openDuringEmbeddings {
		if open != 0 {
			test.Fatalf("%d transactions were open while the model was asked", open)
		}
	}
}
