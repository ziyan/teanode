package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Through the fold a writer calls after writing, with a model that
// embeds, so that the second line finds the first as its twin: the same
// sentence about a second occurrence a year later stays a second fact,
// and the same sentence about the same day is said once.
func TestTheFoldKeepsAnEventOnAnotherDay(t *testing.T) {
	world := newRememberWorldThatEmbeds(t, func(string) string { return "" })
	lastYear := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	thisYear := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		page, err := tx.PutAgentNode(&models.AgentNode{AgentID: world.agent.ID, Path: "things/boiler", Kind: models.NodeThing, Name: "Boiler"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		write := func(when time.Time) *models.AgentFact {
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: world.agent.ID, NodeID: page.ID, Kind: models.FactEvent, HappenedAt: &when, Confidence: 1,
				Text: "Completed the annual inspection of the boiler.",
			})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			became, err := world.worker.FoldIntoWhatThePageSays(ctx, tx, fact, page)
			if err != nil {
				t.Fatalf("FoldIntoWhatThePageSays: %s", err)
			}
			return became
		}
		first := write(lastYear)
		second := write(thisYear)
		if second.ID == first.ID {
			t.Fatalf("this year's inspection was folded behind last year's")
		}
		again := write(thisYear)
		if again.ID != second.ID {
			t.Fatalf("the same inspection on the same day is said once, got fact #%d", again.Number)
		}
		stated, err := tx.ListAgentFacts(world.agent.ID, page.ID, false, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(stated) != 2 {
			t.Fatalf("the page states both years, not %d facts", len(stated))
		}
	})
}
