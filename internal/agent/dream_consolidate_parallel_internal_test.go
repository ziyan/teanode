package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Pages are rewritten a few at once, never more than the operator allows,
// and every one is counted.
func TestPagesAreRewrittenSideBySide(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	var mutex sync.Mutex
	inFlight, mostInFlight := 0, 0
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		inFlight++
		if inFlight > mostInFlight {
			mostInFlight = inFlight
		}
		mutex.Unlock()
		time.Sleep(150 * time.Millisecond)
		mutex.Lock()
		inFlight--
		mutex.Unlock()
		// Streamed or not, whichever the call asked for.
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"summary\\\":\\\"Rewritten.\\\",\\\"same\\\":[]}\"},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\ndata: [DONE]\n\n")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"choices":[{"message":{"role":"assistant","content":"{\"summary\":\"Rewritten.\",\"same\":[]}"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	t.Cleanup(provider.Close)
	worker, run := digestSplitWorld(t, database, provider.URL)
	worker.settings.Configuration().Agent.Limits.RewriteConcurrency = 3

	const pages = 6
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for index := 0; index < pages; index++ {
			page, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: run.Agent.ID, Path: fmt.Sprintf("things/boat-%d", index), Kind: models.NodeThing, Name: fmt.Sprintf("Boat %d", index),
			})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Boat %d is kept at the pier.", index)}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	// Every page due, which is these and whatever of the agent's own roots
	// is due beside them.
	due := 0
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		listed, err := tx.ListAgentNodesToConsolidate(run.Agent.ID, dreamConsolidate)
		if err != nil {
			t.Fatalf("ListAgentNodesToConsolidate: %s", err)
		}
		due = len(listed)
	})
	if due < pages {
		t.Fatalf("only %d pages are due", due)
	}
	record := &models.AgentDream{}
	worker.dreamConsolidate(t.Context(), run, record, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if record.Rewritten != due {
		t.Errorf("%d pages were counted rewritten, not %d", record.Rewritten, due)
	}
	if mostInFlight < 2 || mostInFlight > 3 {
		t.Errorf("%d rewrites ran at once; between two and three were allowed and expected", mostInFlight)
	}
}
