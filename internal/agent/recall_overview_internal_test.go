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
		if blocks, err = turn.chooseRecalled(tx, []*models.AgentNode{page, theme}, []*models.AgentFact{hit}, nil); err != nil {
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

// Recall carries the section of an overview the question's meaning
// matched; else the one sharing most of its words; else the first. A
// match whose section has since been rewritten is not taken, and an
// overview with no headings is one section.
func TestTheSectionOfAnOverviewRecallCarries(t *testing.T) {
	shed := &models.AgentNode{ID: "shed", Overview: "## What it is\n\nA shed.\n\n## Its parts\n\nA door and a window.\n\n## How it relates\n\nThe orchard stores its ladders here."}
	sections := overviewSectionsOf(shed)
	if len(sections) != 3 || sections[1].Heading != "Its parts" || sections[1].Text != "A door and a window." {
		t.Fatalf("the sections are %+v", sections)
	}
	stale := overviewSectionId(shed.ID, 3, "How it relates", "Something it used to say.")
	for _, expected := range []struct {
		name, matched, question, section string
		characters                       int
	}{
		{"nothingMatches", "", "is it painted?", "## What it is\n\nA shed.", 100},
		{"matchedByMeaning", sections[2].ID, "what is it for?", "## How it relates\n\nThe orchard stores its ladders here.", 100},
		{"matchedByWords", "", "is there a door or a window?", "## Its parts\n\nA door and a window.", 100},
		{"aRewrittenSectionIsNotTaken", stale, "is it painted?", "## What it is\n\nA shed.", 100},
		{"cut", sections[2].ID, "", "## How it relates\n\nThe orchard st…", 33},
	} {
		if got := overviewSectionFor(shed, expected.matched, expected.question, expected.characters); got != expected.section {
			t.Errorf("%s: carried %q, not %q", expected.name, got, expected.section)
		}
	}
	// The page's own name says nothing about which section: it is in the
	// first, and the other word the question asks by points elsewhere.
	named := &models.AgentNode{ID: "named", Name: "Garden shed", Overview: "## What it is\n\nThe garden shed by the gate.\n\n## What stands out\n\nIts records are sparse."}
	if got := overviewSectionFor(named, "", "what is notable in the garden shed records?", 100); got != "## What stands out\n\nIts records are sparse." {
		t.Errorf("the page's name chose the section: %q", got)
	}
	// A meaning match on the first section, drawn there by the page's
	// name, gives way to the later section the question's words point at;
	// a meaning match on a later section stands.
	namedSections := overviewSectionsOf(named)
	if got := overviewSectionFor(named, namedSections[0].ID, "what is notable in the garden shed records?", 100); got != "## What stands out\n\nIts records are sparse." {
		t.Errorf("a match on the first section held against the question's words: %q", got)
	}
	if got := overviewSectionFor(shed, sections[2].ID, "is there a door or a window?", 100); got != "## How it relates\n\nThe orchard stores its ladders here." {
		t.Errorf("a match on a later section gave way: %q", got)
	}
	plain := &models.AgentNode{ID: "plain", Overview: "A shed with no heading."}
	if got := overviewSectionFor(plain, "", "", 100); got != "A shed with no heading." {
		t.Errorf("an overview with no headings carried %q", got)
	}
	if got := overviewSectionFor(&models.AgentNode{ID: "empty"}, "", "", 100); got != "" {
		t.Errorf("a page with no overview carried %q", got)
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

// What the overlay could not carry is counted and the memory tool that reads it
// is named, and nothing is said when everything found is carried.
func TestRecallSaysHowMuchItLeftOutAndHowToReadIt(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	worker, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	agentId := run.Agent.ID
	var pages []*models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for index := 0; index < recallPages+3; index++ {
			page, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: fmt.Sprintf("things/boat-%d", index), Kind: models.NodeThing, Name: fmt.Sprintf("Boat %d", index)})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: page.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Boat %d is kept at the pier.", index)}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			pages = append(pages, page)
		}
	})
	turn := &AskRun{agent: worker, settings: &AskSettings{Agent: run.Agent, Owner: run.Owner}, promptMemories: map[string]bool{}}
	choose := func(found []*models.AgentNode) []*recalledBlock {
		var blocks []*recalledBlock
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			if blocks, err = turn.chooseRecalled(tx, found, nil, nil); err != nil {
				t.Fatalf("chooseRecalled: %s", err)
			}
		})
		return blocks
	}
	blocks := choose(pages)
	last := blocks[len(blocks)-1]
	if last.NodeID != "" || !strings.Contains(last.Text, fmt.Sprintf("%d more pages and 0 more facts", recallPages+3-recallPagesUnhit)) || !strings.Contains(last.Text, "memory tool") {
		t.Errorf("the overlay did not say what it left out: %q", last.Text)
	}
	// Pages the words did not hit are carried only up to recallPagesUnhit.
	blocks = choose(pages[:recallPagesUnhit])
	for _, block := range blocks {
		if block.NodeID == "" {
			t.Errorf("nothing was left out, yet the overlay says so: %q", block.Text)
		}
	}
}
