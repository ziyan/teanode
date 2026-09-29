package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page recall carries brings the first section of its overview and not
// the rest; a theme brings the reflections that stand, however few of
// them the words hit, and not the ones they replaced.
func TestRecallCarriesAnOverviewsFirstSectionAndAThemesReflections(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	agentId := run.Agent.ID
	var page, theme *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if page, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "projects/example-app", Kind: models.NodeProject, Name: "example-app"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: page.ID, Kind: models.FactPlain, Text: "It is written in Go."}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		overview := "## What it is\n\nA planting calendar made from seed catalogues.\n\n## Its parts\n\nA reader and a printer."
		if err := tx.SetAgentNodeOverview(agentId, page.ID, overview, nil, "written", time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
		// As the search hands it over: the whole row.
		if page, err = tx.GetAgentNode(agentId, page.Path); err != nil || page == nil {
			t.Fatalf("GetAgentNode: %v %v", page, err)
		}
		if theme, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "themes/garden-software", Kind: models.NodeTopic, Name: "Garden software"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		var first *models.AgentFact
		for number := 1; number <= 5; number++ {
			reflection, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agentId, NodeID: theme.ID, Kind: models.FactReflection, Inferred: true,
				Text: fmt.Sprintf("Observation %d about the garden tools.", number),
			})
			if err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			if first == nil {
				first = reflection
				continue
			}
			if number == 2 {
				if _, err := tx.FoldAgentFact(agentId, first.ID, reflection.ID, "a later reflection replaces it"); err != nil {
					t.Fatalf("FoldAgentFact: %s", err)
				}
			}
		}
	})

	turn := &AskRun{agent: worker, settings: &AskSettings{Agent: run.Agent, Owner: run.Owner}, promptMemories: map[string]bool{}}
	var blocks []*recalledBlock
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(agentId, theme.ID, false, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		// The words hit one reflection, the last.
		hit := facts[len(facts)-1]
		if blocks, err = turn.chooseRecalled(tx, []*models.AgentNode{page, theme}, []*models.AgentFact{hit}); err != nil {
			t.Fatalf("chooseRecalled: %s", err)
		}
	})
	if len(blocks) < 2 {
		t.Fatalf("recall carried %d blocks", len(blocks))
	}
	pageText, themeText := blocks[0].Text, blocks[1].Text
	if !strings.Contains(pageText, "## What it is\n\nA planting calendar made from seed catalogues.") || strings.Contains(pageText, "Its parts") {
		t.Errorf("the page carried %q", pageText)
	}
	if blocks[0].Overview == "" {
		t.Errorf("the block does not say it carried an overview")
	}
	if strings.Contains(themeText, "Observation 1 ") || strings.Count(themeText, "Observation") != recallReflections || !strings.Contains(themeText, "Observation 5 ") {
		t.Errorf("the theme carried %q", themeText)
	}
}

// An overview's first section is what comes before its second heading,
// cut to length.
func TestTheFirstSectionOfAnOverview(t *testing.T) {
	for _, expected := range []struct {
		overview, section string
		characters        int
	}{
		{"", "", 100},
		{"## What it is\n\nA shed.\n\n## Its parts\n\nA door.", "## What it is\n\nA shed.", 100},
		{"A shed with no heading.", "A shed with no heading.", 100},
		{"## What it is\n\nA very long shed.", "## What it is\n\nA very…", 21},
	} {
		if got := firstOverviewSection(expected.overview, expected.characters); got != expected.section {
			t.Errorf("the first section of %q is %q, not %q", expected.overview, got, expected.section)
		}
	}
}

// The themes at the top of the structure lead the prompt's index, though
// nothing about them would rank them there; the themes under them do
// not.
func TestTheIndexCarriesTheTopThemes(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, created := range []*models.AgentNode{
			{Path: "topics/weather", Kind: models.NodeTopic, Name: "Weather", Summary: "Rain and sun.", Importance: 0.9},
			{Path: "themes/outdoor-life", Kind: models.NodeTopic, Name: "Outdoor life", Summary: "Orchards and tides.", Importance: 0},
			{Path: "themes/outdoor-life/orchard-work", Kind: models.NodeTopic, Name: "Orchard work", Summary: "Pruning and picking.", Importance: 0},
		} {
			created.AgentID = run.Agent.ID
			if _, err := tx.PutAgentNode(created); err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
		}
	})
	lines, _ := worker.graphIndex(t.Context(), run.Agent, run.Owner, indexTokens)
	joined := strings.Join(lines, "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "themes/outdoor-life") {
		t.Errorf("the index opens with %q", joined)
	}
	if position := strings.Index(joined, "themes/outdoor-life/orchard-work"); position >= 0 && position < strings.Index(joined, "topics/weather") {
		t.Errorf("a theme under another is put ahead of the pages: %q", joined)
	}
}
