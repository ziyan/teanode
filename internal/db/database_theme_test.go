package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A theme's opening is the theme phase's: the night's consolidation
// neither blanks it for standing over no facts nor rewrites it from the
// reflections, and the quiet half never retires a theme for going
// unused, since whether a theme is live is the theme phase's to say.
func TestAThemeIsNeitherConsolidatedNorRetired(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		theme, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "themes/orchard-work", Kind: models.NodeTopic, Name: "Orchard work", Summary: "The orchard and its trees.",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		loose, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "topics/loose-page", Kind: models.NodeTopic, Name: "Loose page", Summary: "Nothing under it.",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: agent.ID, NodeID: theme.ID, Kind: models.FactReflection, Text: "Pruning runs late.", Inferred: true,
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}

		due, err := tx.ListAgentNodesToConsolidate(agent.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentNodesToConsolidate: %s", err)
		}
		var paths []string
		for _, page := range due {
			paths = append(paths, page.Path)
		}
		if isListed(paths, theme.Path) || !isListed(paths, loose.Path) {
			t.Errorf("the pages due a rewrite are %v", paths)
		}

		if _, err := tx.RetireAgentNodes(agent.ID, 100, time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("RetireAgentNodes: %s", err)
		}
		if kept := mustNode(t, tx, agent.ID, theme.Path); kept.Dormant {
			t.Errorf("the theme was retired")
		}
		if retired := mustNode(t, tx, agent.ID, loose.Path); !retired.Dormant {
			t.Errorf("the page beside it was not retired, so the test says nothing")
		}
	})
}

// The night reflects on a theme when its overview is newer than the last
// time it did, and records when that was.
func TestAThemeIsDueAReflectionWhenItsOverviewIsNewer(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		theme, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "themes/tide-tables", Kind: models.NodeTopic, Name: "Tide tables"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		due := func() int {
			themes, err := tx.ListAgentThemesToReflect(agent.ID, 5)
			if err != nil {
				t.Fatalf("ListAgentThemesToReflect: %s", err)
			}
			return len(themes)
		}
		if due() != 0 {
			t.Errorf("a theme with no overview is due a reflection")
		}
		writtenAt := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
		if err := tx.SetAgentNodeOverview(agent.ID, theme.ID, "## What it is\n\nTides.", nil, "written", writtenAt); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
		if due() != 1 {
			t.Errorf("a theme with a new overview is not due a reflection")
		}
		if err := tx.MarkAgentNodeReflected(agent.ID, theme.ID, writtenAt); err != nil {
			t.Fatalf("MarkAgentNodeReflected: %s", err)
		}
		if reflectedAt, err := tx.AgentNodeReflectedAt(agent.ID, theme.ID); err != nil || reflectedAt == nil || !reflectedAt.Equal(writtenAt) {
			t.Errorf("AgentNodeReflectedAt: %v %v", reflectedAt, err)
		}
		if due() != 0 {
			t.Errorf("a theme reflected on since its overview is still due")
		}
	})
}

func mustNode(t *testing.T, tx db.Transaction, agentId, path string) *models.AgentNode {
	t.Helper()
	node, err := tx.GetAgentNode(agentId, path)
	if err != nil || node == nil {
		t.Fatalf("GetAgentNode %q: %v %v", path, node, err)
	}
	return node
}
