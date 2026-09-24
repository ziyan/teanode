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

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The split is shown the pages already under the one it divides, and a
// group that names one of them joins it, however few facts it brings.
// Shown only the facts, a page that kept growing was divided every night
// into new themes beside the ones the nights before had made.
func TestTheSplitJoinsAPageAlreadyThere(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	var mutex sync.Mutex
	var prompts []string
	answer, _ := json.Marshal(`{"groups": [{"name": "Spam filtering", "slug": "spam-filtering", "facts": [1, 2]}]}`)
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool             `json:"stream"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		mutex.Lock()
		for _, message := range body.Messages {
			prompts = append(prompts, fmt.Sprint(message["content"]))
		}
		mutex.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", answer)
	}))
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	var page *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if page, err = tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "projects/lantern", Kind: models.NodeProject, Name: "Lantern"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: "projects/lantern/spam-filtering", Kind: models.NodeProject, Name: "Spam filtering",
			Summary: "How incoming mail is scored.",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for index := range 6 {
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Lantern fact %d", index+1)}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	moved, err := worker.splitPage(context.Background(), run, &dreamBudget{}, page)
	if err != nil {
		t.Fatalf("splitPage: %s", err)
	}
	if moved != 2 {
		t.Fatalf("two facts join the page already there, not %d", moved)
	}
	mutex.Lock()
	shown := strings.Join(prompts, "\n")
	mutex.Unlock()
	if !strings.Contains(shown, "spam-filtering — Spam filtering: How incoming mail is scored.") {
		t.Fatalf("the split was not shown the page already under it")
	}
	if count := dbtest.QueryString(t, database, `SELECT count(*)::text FROM agent_node WHERE path LIKE 'projects/lantern/%'`); count != "1" {
		t.Fatalf("a second page was made beside the one there: %s pages", count)
	}
}
