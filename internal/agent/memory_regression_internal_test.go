package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A correction that arrives a week after the fact it corrects retires it;
// one that speaks of an earlier day does not, because the line it would
// retire is the newer news.
func TestALateCorrectionRetiresWhatItCorrects(t *testing.T) {
	monday := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	retiring := &models.AgentFact{ID: "old", NodeID: "page", Kind: models.FactEvent, HappenedAt: &monday, Confidence: 1}
	weekLater, weekEarlier := monday.AddDate(0, 0, 7), monday.AddDate(0, 0, -7)
	later := &models.AgentFact{ID: "new", NodeID: "page", Kind: models.FactEvent, HappenedAt: &weekLater, Confidence: 1}
	earlier := &models.AgentFact{ID: "new", NodeID: "page", Kind: models.FactEvent, HappenedAt: &weekEarlier, Confidence: 1}
	if !replacementHolds(retiring, later) {
		t.Errorf("a correction a week later replaces the line")
	}
	if replacementHolds(retiring, earlier) {
		t.Errorf("a correction about an earlier day does not replace a newer line")
	}
}

// Two nights rewriting one page at once, both asked to merge the same
// pair: the pair is merged once, and neither run fails.
func TestTwoConsolidationsOfOnePageMergeOnce(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	answer := `{"summary": "A boat kept at the pier.", "same": [[2, 1]]}`
	provider := providerThatChangesThePage(answer, func() {})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	var page *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if page, err = tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "things/marigold", Kind: models.NodeThing, Name: "Marigold"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for _, text := range []string{"Marigold is kept at the pier.", "Marigold is moored at the pier."} {
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: text}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			budget := newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0)
			worker.consolidatePage(t.Context(), run, &models.AgentDream{}, page, budget)
		}()
	}
	wait.Wait()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stated, err := tx.ListAgentFacts(run.Agent.ID, page.ID, false, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(stated) != 1 {
			t.Fatalf("the pair is merged into one fact, not %d", len(stated))
		}
		all, err := tx.ListAgentFacts(run.Agent.ID, page.ID, true, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(all) != 2 {
			t.Fatalf("and nothing is lost: both rows are kept, not %d", len(all))
		}
	})
}
