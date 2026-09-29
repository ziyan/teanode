package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// overviewPaths is the paths of the pages due an overview, in the order
// they are listed.
func overviewPaths(t *testing.T, tx db.Transaction, agentId string) []string {
	t.Helper()
	due, err := tx.ListAgentNodesForOverview(agentId, 3, 50)
	if err != nil {
		t.Fatalf("ListAgentNodesForOverview: %s", err)
	}
	paths := make([]string, 0, len(due))
	for _, page := range due {
		paths = append(paths, page.Path)
	}
	return paths
}

func addFacts(t *testing.T, tx db.Transaction, agentId, nodeId string, texts ...string) []*models.AgentFact {
	t.Helper()
	var facts []*models.AgentFact
	for _, text := range texts {
		fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: nodeId, Kind: models.FactPlain, Text: text})
		if err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		facts = append(facts, fact)
	}
	return facts
}

func isListed(paths []string, path string) bool {
	for _, each := range paths {
		if each == path {
			return true
		}
	}
	return false
}

// The hash of what an overview is written from stays the same while
// nothing changes, and moves when a fact does, and a page with too
// little to say is never due.
func TestOverviewInputsChangeOnlyWithWhatTheOverviewIsWrittenFrom(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		kettle := putNode(t, tx, agent.ID, "things/copper-kettle", models.NodeThing, "Copper kettle")
		facts := addFacts(t, tx, agent.ID, kettle.ID,
			"The copper kettle whistles when it boils.",
			"The copper kettle was bought at a flea market.",
			"The copper kettle needs polishing every autumn.")
		spoon := putNode(t, tx, agent.ID, "things/wooden-spoon", models.NodeThing, "Wooden spoon")
		addFacts(t, tx, agent.ID, spoon.ID, "The wooden spoon hangs by the stove.")

		paths := overviewPaths(t, tx, agent.ID)
		if !isListed(paths, kettle.Path) {
			t.Fatalf("a page of three facts and no overview is due, and was not listed: %v", paths)
		}
		if isListed(paths, spoon.Path) {
			t.Fatalf("a page of one fact and nothing under it gets no overview, and was listed: %v", paths)
		}

		first, err := tx.AgentNodeOverviewInputs(agent.ID, kettle.ID)
		if err != nil || len(first) != 64 {
			t.Fatalf("AgentNodeOverviewInputs: %q %v", first, err)
		}
		again, err := tx.AgentNodeOverviewInputs(agent.ID, kettle.ID)
		if err != nil || again != first {
			t.Fatalf("the same inputs hashed twice gave %q and %q (%v)", first, again, err)
		}
		if err := tx.SetAgentNodeOverview(agent.ID, kettle.ID, "## What it is\n\nA kettle.",
			[]models.Evidence{{Kind: models.EvidenceMemory, Quote: kettle.Path}}, first, time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
		if isListed(overviewPaths(t, tx, agent.ID), kettle.Path) {
			t.Fatalf("a page whose overview was written from its inputs as they are is not due")
		}

		// Writing the page whole, as a source does, keeps the overview.
		rewritten := *kettle
		rewritten.Aliases = []string{"the kettle"}
		if _, err := tx.PutAgentNode(&rewritten); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		stored, err := tx.GetAgentNode(agent.ID, kettle.Path)
		if err != nil || stored == nil {
			t.Fatalf("GetAgentNode: %v %v", stored, err)
		}
		if stored.Overview != "## What it is\n\nA kettle." || stored.OverviewInputs != first || stored.OverviewWrittenAt == nil ||
			len(stored.OverviewEvidence) != 1 {
			t.Fatalf("a page saved whole lost its overview: %+v", stored)
		}

		if _, err := tx.UpdateAgentFact(agent.ID, facts[2].ID, func(fact *models.AgentFact) error {
			fact.Text = "The copper kettle needs polishing every spring."
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentFact: %s", err)
		}
		changed, err := tx.AgentNodeOverviewInputs(agent.ID, kettle.ID)
		if err != nil || changed == first {
			t.Fatalf("a fact that changed left the hash where it was: %q (%v)", changed, err)
		}
		if !isListed(overviewPaths(t, tx, agent.ID), kettle.Path) {
			t.Fatalf("a page one of whose facts changed is due again")
		}

		// Asked for by hand: clearing the hash makes it due, and the
		// overview it has stays until the next is written.
		if err := tx.SetAgentNodeOverview(agent.ID, kettle.ID, "## What it is\n\nA kettle.", nil, changed, time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
		if err := tx.ClearAgentNodeOverviewInputs(agent.ID, kettle.ID); err != nil {
			t.Fatalf("ClearAgentNodeOverviewInputs: %s", err)
		}
		if !isListed(overviewPaths(t, tx, agent.ID), kettle.Path) {
			t.Fatalf("a page whose hash was cleared is due")
		}
	})
}

// A page waits while a page directly under it is due, so its overview is
// written from theirs; and a child written again makes its parent due.
func TestAPageWaitsForThePagesUnderIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		orchard := putNode(t, tx, agent.ID, "places/orchard", models.NodePlace, "Orchard")
		var rows []*models.AgentNode
		for index := 1; index <= 2; index++ {
			row := putNode(t, tx, agent.ID, fmt.Sprintf("places/orchard/row-%d", index), models.NodePlace, fmt.Sprintf("Row %d", index))
			addFacts(t, tx, agent.ID, row.ID,
				fmt.Sprintf("Row %d has apple trees.", index),
				fmt.Sprintf("Row %d is pruned in February.", index),
				fmt.Sprintf("Row %d faces south.", index))
			rows = append(rows, row)
		}

		paths := overviewPaths(t, tx, agent.ID)
		if !isListed(paths, rows[0].Path) || !isListed(paths, rows[1].Path) || isListed(paths, orchard.Path) {
			t.Fatalf("the rows are ready and the orchard waits for them: %v", paths)
		}
		writeOverviews(t, tx, agent.ID, rows...)
		if paths := overviewPaths(t, tx, agent.ID); !isListed(paths, orchard.Path) {
			t.Fatalf("with its rows written the orchard is ready: %v", paths)
		}
		writeOverviews(t, tx, agent.ID, orchard)
		if paths := overviewPaths(t, tx, agent.ID); isListed(paths, orchard.Path) || isListed(paths, rows[0].Path) {
			t.Fatalf("nothing changed and something is due: %v", paths)
		}

		// A child written again changes what its parent is written from.
		inputs, err := tx.AgentNodeOverviewInputs(agent.ID, rows[0].ID)
		if err != nil {
			t.Fatalf("AgentNodeOverviewInputs: %s", err)
		}
		if err := tx.SetAgentNodeOverview(agent.ID, rows[0].ID, "## What it is\n\nThe first row.", nil, inputs, time.Now().Add(time.Second)); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
		if !isListed(overviewPaths(t, tx, agent.ID), orchard.Path) {
			t.Fatalf("a page whose child's overview was written again is due")
		}
	})
}

// A page with nothing due under it is ready the first night, however
// many deeper pages elsewhere are due; the most important come first;
// and a page below the median importance waits.
func TestAPageIsReadyWhenNothingUnderItIsDue(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		withFacts := func(path string, importance float32) *models.AgentNode {
			page, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: path, Kind: models.NodeThing, Name: path, Importance: importance})
			if err != nil {
				t.Fatalf("PutAgentNode %q: %s", path, err)
			}
			addFacts(t, tx, agent.ID, page.ID, "It is old.", "It is heavy.", "It is green.")
			return page
		}
		withFacts("things/boat/hull/keel/bolt", 0.5)
		withFacts("things/boat/hull/keel", 0.5)
		withFacts("things/copper-kettle", 0.9)
		withFacts("things/garden-hose", 0.7)
		withFacts("things/spare-button", 0.1)

		paths := overviewPaths(t, tx, agent.ID)
		want := []string{"things/copper-kettle", "things/garden-hose", "things/boat/hull/keel/bolt"}
		if fmt.Sprint(paths) != fmt.Sprint(want) {
			t.Fatalf("ready: %v, want %v", paths, want)
		}
	})
}

// Writing reflections on a theme does not make its overview due: they
// are written from the overview.
func TestReflectionsAreNotWhatAnOverviewIsWrittenFrom(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		putNode(t, tx, agent.ID, "themes", models.NodeFolder, "Themes")
		theme := putNode(t, tx, agent.ID, "themes/boatyard", models.NodeTopic, "Boatyard")
		member := putNode(t, tx, agent.ID, "things/rowing-boat", models.NodeThing, "Rowing boat")
		if err := tx.PutAgentEdge(&models.AgentEdge{AgentID: agent.ID, FromID: theme.ID, ToID: member.ID, Relation: models.EdgeAboutPlace, Status: models.EdgeStated}); err != nil {
			t.Fatalf("PutAgentEdge: %s", err)
		}
		writeOverviews(t, tx, agent.ID, theme)
		before, err := tx.AgentNodeOverviewInputs(agent.ID, theme.ID)
		if err != nil {
			t.Fatalf("AgentNodeOverviewInputs: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agent.ID, NodeID: theme.ID, Kind: models.FactReflection,
			Text: "Every boat here needs the same repair.", Confidence: 0.7}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		after, err := tx.AgentNodeOverviewInputs(agent.ID, theme.ID)
		if err != nil {
			t.Fatalf("AgentNodeOverviewInputs: %s", err)
		}
		if after != before {
			t.Errorf("a reflection made the theme's overview due")
		}
		due, err := tx.ListAgentThemesForOverview(agent.ID, 10)
		if err != nil {
			t.Fatalf("ListAgentThemesForOverview: %s", err)
		}
		if len(due) != 0 {
			t.Errorf("themes due after a reflection: %v", due)
		}
	})
}

// writeOverviews marks pages written from their inputs as they are now.
func writeOverviews(t *testing.T, tx db.Transaction, agentId string, pages ...*models.AgentNode) {
	t.Helper()
	for _, page := range pages {
		inputs, err := tx.AgentNodeOverviewInputs(agentId, page.ID)
		if err != nil {
			t.Fatalf("AgentNodeOverviewInputs: %s", err)
		}
		if err := tx.SetAgentNodeOverview(agentId, page.ID, "## What it is\n\nA page.", nil, inputs, time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
	}
}
