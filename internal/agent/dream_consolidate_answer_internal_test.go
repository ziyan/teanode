package agent

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A rewrite whose answer cannot be read leaves the page as it was. `{}`
// and an error object used to read as an empty opening, and blanked the
// page's summary. A valid empty opening still clears it.
func TestAnUnreadableRewriteLeavesThePageAlone(t *testing.T) {
	for _, each := range []struct {
		name         string
		answer       string
		isRewritten  bool
		summaryAfter string
	}{
		{"an empty object", `{}`, false, "A blue boat kept at the pier."},
		{"an error object", `{"error": "overloaded"}`, false, "A blue boat kept at the pier."},
		{"a cut-off answer", `{"summary": "A red`, false, "A blue boat kept at the pier."},
		{"a valid empty opening", `{"summary": "", "same": []}`, true, ""},
	} {
		t.Run(each.name, func(t *testing.T) {
			database, release := dbtest.AcquireDatabase(t)
			defer release()
			content, _ := json.Marshal(each.answer)
			provider := scriptedProvider([]string{fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, content)})
			defer provider.Close()
			worker, run := digestSplitWorld(t, database, provider.URL)

			var page *models.AgentNode
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				var err error
				if page, err = tx.PutAgentNode(&models.AgentNode{
					AgentID: run.Agent.ID, Path: "things/marigold", Kind: models.NodeThing, Name: "Marigold",
					Summary: "A blue boat kept at the pier.",
				}); err != nil {
					t.Fatalf("PutAgentNode: %s", err)
				}
				if _, err := tx.AddAgentFact(&models.AgentFact{
					AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: "Marigold is kept at the pier.",
				}); err != nil {
					t.Fatalf("AddAgentFact: %s", err)
				}
			})
			budget := newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0)
			if rewritten := worker.consolidatePage(t.Context(), run, &models.AgentDream{}, page, budget); rewritten != each.isRewritten {
				t.Fatalf("rewritten %v, want %v", rewritten, each.isRewritten)
			}
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				after, err := tx.GetAgentNode(run.Agent.ID, page.Path)
				if err != nil || after == nil {
					t.Fatalf("GetAgentNode: %v %s", after, err)
				}
				if after.Summary != each.summaryAfter {
					t.Fatalf("the opening is %q, want %q", after.Summary, each.summaryAfter)
				}
			})
		})
	}
}
