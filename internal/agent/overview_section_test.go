package agent_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Every section of an overview is embedded once; a second pass has nothing
// to do; a section a dream rewrote is embedded again and its old vector
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

// The pass reads only the most important pages, and a vector of a page
// outside them is kept while that page still holds the section: slipping
// down the order is not being rewritten. A page outside them whose
// overview changed, or that went dormant, loses the vectors it no longer
// holds, and a vector of another model is never touched.
func TestAVectorOutsideThePagesReadIsJudgedByItsPage(test *testing.T) {
	world := newRememberWorldThatEmbeds(test, func(string) string { return "" })
	pageIds := map[string]string{}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(world.agent.ID); err != nil {
			test.Fatal(err)
		}
		for _, path := range []string{"topics/barn", "topics/silo", "topics/well"} {
			page, err := tx.PutAgentNode(&models.AgentNode{AgentID: world.agent.ID, Path: path, Kind: models.NodeTopic, Name: models.LastSegment(path), Summary: "A building."})
			if err != nil {
				test.Fatal(err)
			}
			pageIds[path] = page.ID
			if err := tx.SetAgentNodeOverview(world.agent.ID, page.ID, "## What it is\n\nThe "+models.LastSegment(path)+".", []models.Evidence{}, "inputs", time.Now()); err != nil {
				test.Fatal(err)
			}
		}
	})
	setImportance := func(path string, importance int) {
		dbtest.Exec(test, world.database, fmt.Sprintf(`UPDATE agent_node SET importance = %d WHERE id = '%s'`, importance, pageIds[path]))
	}
	setImportance("topics/barn", 3)
	setImportance("topics/silo", 2)
	setImportance("topics/well", 1)
	vectorCount := func() string {
		return dbtest.QueryString(test, world.database, `SELECT count(*)::text FROM agent_overview_section_vector`)
	}
	vectorCountOf := func(path string) string {
		return dbtest.QueryString(test, world.database, `SELECT count(*)::text FROM agent_overview_section_vector WHERE node_id = '`+pageIds[path]+`'`)
	}
	if written, err := world.worker.EmbedOverviewSectionsOfTheMostImportant(test.Context(), world.agent, 100, 3); err != nil || written != 3 {
		test.Fatalf("the first pass wrote %d: %v", written, err)
	}
	// A vector of another model, on a page about to be rewritten.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if err := tx.PutAgentOverviewSectionVector(world.agent.ID, pageIds["topics/barn"], "another-models-section", "another-model", []float32{1, 0, 0}); err != nil {
			test.Fatal(err)
		}
	})

	// Two pages read: the well's vector is kept.
	if _, err := world.worker.EmbedOverviewSectionsOfTheMostImportant(test.Context(), world.agent, 100, 2); err != nil || vectorCountOf("topics/well") != "1" {
		test.Fatalf("the well's vector after a pass that did not read it: %s (%v)", vectorCountOf("topics/well"), err)
	}
	// The order changes: the barn falls out, and its vector is kept.
	setImportance("topics/well", 5)
	setImportance("topics/barn", 0)
	if _, err := world.worker.EmbedOverviewSectionsOfTheMostImportant(test.Context(), world.agent, 100, 2); err != nil || vectorCountOf("topics/barn") != "2" {
		test.Fatalf("the barn's vectors after it fell out of the order: %s (%v)", vectorCountOf("topics/barn"), err)
	}
	// The barn's overview is rewritten while it is not read: its old
	// section goes, the other model's stays.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if err := tx.SetAgentNodeOverview(world.agent.ID, pageIds["topics/barn"], "## What it is\n\nThe barn, rebuilt.", []models.Evidence{}, "inputs", time.Now()); err != nil {
			test.Fatal(err)
		}
	})
	if _, err := world.worker.EmbedOverviewSectionsOfTheMostImportant(test.Context(), world.agent, 100, 2); err != nil || vectorCountOf("topics/barn") != "1" {
		test.Fatalf("the rewritten barn holds %s vectors (%v)", vectorCountOf("topics/barn"), err)
	}
	if other := dbtest.QueryString(test, world.database, `SELECT count(*)::text FROM agent_overview_section_vector WHERE model = 'another-model'`); other != "1" {
		test.Errorf("another model's vector was touched: %s", other)
	}
	// A page that went dormant loses its vector.
	dbtest.Exec(test, world.database, `UPDATE agent_node SET dormant = true WHERE id = '`+pageIds["topics/silo"]+`'`)
	if _, err := world.worker.EmbedOverviewSectionsOfTheMostImportant(test.Context(), world.agent, 100, 2); err != nil || vectorCountOf("topics/silo") != "0" {
		test.Fatalf("the dormant silo holds %s vectors (%v); %s in all", vectorCountOf("topics/silo"), err, vectorCount())
	}
}
