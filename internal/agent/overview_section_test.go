package agent_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Every section of an overview is embedded once; a second pass has nothing
// to do; a section the night rewrote is embedded again and its old vector
// removed, and the sections that did not change are left as they were.
func TestOverviewSectionsAreEmbeddedOnceAndFollowRewrites(test *testing.T) {
	world := newRememberWorldThatEmbeds(test, func(string) string { return "" })
	var shed *models.AgentNode
	setOverview := func(overview string) {
		dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
			if err := tx.SetAgentNodeOverview(world.agent.ID, shed.ID, overview, []models.Evidence{}, "inputs", time.Now()); err != nil {
				test.Fatal(err)
			}
		})
	}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(world.agent.ID); err != nil {
			test.Fatal(err)
		}
		created, err := tx.PutAgentNode(&models.AgentNode{AgentID: world.agent.ID, Path: "topics/garden-shed", Kind: models.NodeTopic, Name: "Garden shed", Summary: "A shed."})
		if err != nil {
			test.Fatal(err)
		}
		shed = created
	})
	setOverview("## What it is\n\nA shed.\n\n## Its parts\n\nA door and a window.\n\n## How it relates\n\nThe orchard stores its ladders here.")

	sectionCount := func() string {
		return dbtest.QueryString(test, world.database, `SELECT count(*)::text FROM agent_overview_section_vector`)
	}
	written, err := world.worker.EmbedOverviewSections(test.Context(), world.agent, 100)
	if err != nil || written != 3 || sectionCount() != "3" {
		test.Fatalf("the first pass wrote %d (stored %s): %v", written, sectionCount(), err)
	}
	written, err = world.worker.EmbedOverviewSections(test.Context(), world.agent, 100)
	if err != nil || written != 0 {
		test.Fatalf("a second pass wrote %d: %v", written, err)
	}

	setOverview("## What it is\n\nA shed.\n\n## Its parts\n\nA door, a window and a new roof.\n\n## How it relates\n\nThe orchard stores its ladders here.")
	written, err = world.worker.EmbedOverviewSections(test.Context(), world.agent, 100)
	if err != nil || written != 1 || sectionCount() != "3" {
		test.Fatalf("after a rewrite the pass wrote %d (stored %s): %v", written, sectionCount(), err)
	}
}
