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

// providerThatChangesThePage answers every call with the given answer,
// and before its first answer runs during: what somebody else does to the
// page while the model is thinking.
func providerThatChangesThePage(answer string, during func()) *httptest.Server {
	var once sync.Once
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		once.Do(during)
		content, _ := json.Marshal(answer)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{}}`, content)
	}))
}

// consolidateWhile rewrites a page with two facts on it while during
// changes it, and answers with the page and its facts afterwards and
// whether the page is due to be rewritten again.
func consolidateWhile(t *testing.T, answer string, during func(tx db.Transaction, page *models.AgentNode, facts []*models.AgentFact)) (*models.AgentNode, []*models.AgentFact, bool) {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	var page *models.AgentNode
	var facts []*models.AgentFact
	provider := providerThatChangesThePage(answer, func() {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) { during(tx, page, facts) })
	})
	t.Cleanup(provider.Close)
	worker, run := digestSplitWorld(t, database, provider.URL)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if page, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: "things/marigold", Kind: models.NodeThing, Name: "Marigold",
			Summary: "A boat.",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for _, text := range []string{"Marigold is kept at the pier.", "Marigold is moored at the pier."} {
			fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: text})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			facts = append(facts, fact)
		}
	})
	budget := newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0)
	if !worker.consolidatePage(t.Context(), run, &models.AgentDream{}, page, budget) {
		t.Fatalf("the page was not rewritten")
	}
	var after *models.AgentNode
	var factsAfter []*models.AgentFact
	isDue := false
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if after, err = tx.GetAgentNodeByID(run.Agent.ID, page.ID); err != nil || after == nil {
			t.Fatalf("GetAgentNodeByID: %v %s", after, err)
		}
		if factsAfter, err = tx.ListAgentFacts(run.Agent.ID, page.ID, false, 10); err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		due, err := tx.ListAgentNodesToConsolidate(run.Agent.ID, 10)
		if err != nil {
			t.Fatalf("ListAgentNodesToConsolidate: %s", err)
		}
		for _, node := range due {
			isDue = isDue || node.ID == page.ID
		}
	})
	return after, factsAfter, isDue
}

const rewrittenOpening = `{"summary": "A boat kept at the pier.", "same": []}`

// A rename, an alias and a pin made while the model was answering stand:
// the rewrite writes the opening and nothing else.
func TestARewriteKeepsARenameMadeDuringIt(t *testing.T) {
	page, _, _ := consolidateWhile(t, rewrittenOpening, func(tx db.Transaction, page *models.AgentNode, _ []*models.AgentFact) {
		renamed := *page
		renamed.Name, renamed.Aliases, renamed.Pinned = "Marigold the boat", []string{"the blue boat"}, true
		if _, err := tx.PutAgentNode(&renamed); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
	})
	if page.Name != "Marigold the boat" || len(page.Aliases) != 1 || !page.Pinned {
		t.Fatalf("the rename, alias and pin were put back: %+v", page)
	}
	if page.Summary != "A boat kept at the pier." {
		t.Fatalf("and the opening is the rewrite's: %q", page.Summary)
	}
}

// A page archived while the model was answering stays archived.
func TestARewriteKeepsAPageArchivedDuringIt(t *testing.T) {
	page, _, _ := consolidateWhile(t, rewrittenOpening, func(tx db.Transaction, page *models.AgentNode, _ []*models.AgentFact) {
		if retired, err := tx.RetireAgentNodes(page.AgentID, 1, time.Now().Add(time.Hour)); err != nil || retired == 0 {
			t.Fatalf("RetireAgentNodes: %d %v", retired, err)
		}
	})
	if !page.Dormant {
		t.Fatalf("a page archived during the rewrite was brought back")
	}
}

// A fact added while the model was answering is newer than the mark, so
// the page is due again rather than never summarized.
func TestAFactAddedDuringARewriteMakesThePageDueAgain(t *testing.T) {
	_, _, isDue := consolidateWhile(t, rewrittenOpening, func(tx db.Transaction, page *models.AgentNode, _ []*models.AgentFact) {
		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: page.AgentID, NodeID: page.ID, Kind: models.FactPlain, Text: "Marigold was repainted in April."}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})
	if !isDue {
		t.Fatalf("a fact added during the rewrite is behind the mark, and the page is not due")
	}
}

// A merge of facts that changed while the model was answering is not
// applied: the model judged what it was shown, not what is there now.
func TestAMergeOfAFactChangedDuringTheRewriteIsSkipped(t *testing.T) {
	_, facts, _ := consolidateWhile(t, `{"summary": "A boat.", "same": [[2, 1]]}`, func(tx db.Transaction, _ *models.AgentNode, facts []*models.AgentFact) {
		if _, err := tx.UpdateAgentFact(facts[1].AgentID, facts[1].ID, func(fact *models.AgentFact) error {
			fact.Text = "Marigold is moored at the pier in winter only."
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentFact: %s", err)
		}
	})
	if len(facts) != 2 {
		t.Fatalf("both facts stay, not %d", len(facts))
	}
}
