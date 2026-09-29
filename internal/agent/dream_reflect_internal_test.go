package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// reflectionWorld is a theme of three pages with facts, its overview
// written, and the ids by path.
func reflectionWorld(t *testing.T, database db.Database, agentId string) map[string]string {
	t.Helper()
	idByPath := map[string]string{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		theme, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "themes/orchard-work", Kind: models.NodeTopic, Name: "Orchard work"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		idByPath[theme.Path] = theme.ID
		for _, path := range []string{"projects/orchard-north", "projects/orchard-south", "projects/orchard-east"} {
			page, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: path, Kind: models.NodeProject, Name: models.LastSegment(path)})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			idByPath[path] = page.ID
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: page.ID, Kind: models.FactPlain, Text: "The trees were pruned late again."}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: agentId, FromID: theme.ID, ToID: page.ID, Relation: models.EdgeAboutPlace, Weight: 1, Status: models.EdgeStated,
				Evidence: []models.Evidence{{Kind: models.EvidenceDream, Quote: themeEvidenceQuote}},
			}); err != nil {
				t.Fatalf("PutAgentEdge: %s", err)
			}
		}
		if err := tx.SetAgentNodeOverview(agentId, theme.ID, "## What it is\n\nThree orchards.", nil, "written", time.Now().Add(-time.Hour)); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
	})
	return idByPath
}

// reflectionAnswerText is three observations: one that cites two things
// the prompt showed, one whose second citation was made up, and one of a
// kind that is not one.
const reflectionAnswerText = `{"reflections": [
	{"text": "Pruning runs late in every orchard.", "reflectionKind": "Pattern", "citations": ["projects/orchard-north#1", "projects/orchard-south#1", "projects/orchard-south#1", "projects/nowhere#4"]},
	{"text": "The east orchard stands apart.", "reflectionKind": "tension", "citations": ["projects/orchard-east", "projects/orchard-made-up"]},
	{"text": "Something vague.", "reflectionKind": "musing", "citations": ["projects/orchard-north", "projects/orchard-east"]}
]}`

// A theme whose overview is newer than its last reflection gets one; an
// observation needs two citations of what its prompt showed; the next
// reflection supersedes the last rather than deleting it.
func TestTheNightReflectsOnAThemeAndSupersedesTheLastReflection(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, sentPrompts := promptedProvider(t, func(string) string { return reflectionAnswerText })
	worker, run := digestSplitWorld(t, database, provider.URL)
	idByPath := reflectionWorld(t, database, run.Agent.ID)

	record := &models.AgentDream{}
	worker.dreamReflect(t.Context(), run, record, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if record.ReflectionsWritten != 1 || len(sentPrompts()) != 1 {
		t.Fatalf("%d reflections written with %d calls", record.ReflectionsWritten, len(sentPrompts()))
	}
	if prompt := sentPrompts()[0]; !strings.Contains(prompt, "projects/orchard-north#1 The trees were pruned late again.") {
		t.Errorf("the prompt does not show the members' facts to cite: %q", prompt)
	}
	var first *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(run.Agent.ID, idByPath["themes/orchard-work"], false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("ListAgentFacts: %v %s", facts, err)
		}
		first = facts[0]
		if first.Kind != models.FactReflection || first.ReflectionKind() != "pattern" {
			t.Errorf("the reflection is %+v", first)
		}
		if got := strings.Join(first.Citations(), " "); got != "projects/orchard-north#1 projects/orchard-south#1" {
			t.Errorf("the reflection cites %q", got)
		}
	})

	// Nothing changed: nothing is asked.
	worker.dreamReflect(t.Context(), run, &models.AgentDream{}, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if len(sentPrompts()) != 1 {
		t.Errorf("a theme with the same overview was reflected on again")
	}

	// The overview is written again: the next reflection replaces it.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.SetAgentNodeOverview(run.Agent.ID, idByPath["themes/orchard-work"], "## What it is\n\nThree orchards, pruned late.", nil, "again", time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
	})
	worker.dreamReflect(t.Context(), run, &models.AgentDream{}, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		live, err := tx.ListAgentFacts(run.Agent.ID, idByPath["themes/orchard-work"], false, 10)
		if err != nil || len(live) != 1 || live[0].ID == first.ID {
			t.Fatalf("the live reflections are %v %v", live, err)
		}
		all, err := tx.ListAgentFacts(run.Agent.ID, idByPath["themes/orchard-work"], true, 10)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		for _, fact := range all {
			if fact.ID == first.ID && fact.SupersededBy != live[0].ID {
				t.Errorf("the first reflection is not superseded by the second: %+v", fact)
			}
		}
	})
}

// The top-level themes are reflected on together once a week, onto the
// person's own page of reflections.
func TestTheThemesAreReflectedOnTogetherOnceAWeek(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	answer := `{"reflections": [{"text": "Both themes follow the seasons.", "reflectionKind": "trend", "citations": ["themes/orchard-work", "themes/tide-tables"]}]}`
	provider, sentPrompts := promptedProvider(t, func(string) string { return answer })
	worker, run := digestSplitWorld(t, database, provider.URL)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, path := range []string{"themes/orchard-work", "themes/tide-tables"} {
			theme, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: path, Kind: models.NodeTopic, Name: models.LastSegment(path)})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			if err := tx.SetAgentNodeOverview(run.Agent.ID, theme.ID, "## What it is\n\nA theme.", nil, "written", time.Now()); err != nil {
				t.Fatalf("SetAgentNodeOverview: %s", err)
			}
		}
	})

	now := time.Now()
	budget := newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0)
	if written := worker.reflectAcrossThemes(t.Context(), run, budget, now); written != 1 {
		t.Fatalf("%d reflections across the themes", written)
	}
	var reflections *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		reflections = mustPage(t, tx, run.Agent.ID, models.PathReflections)
		facts, err := tx.ListAgentFacts(run.Agent.ID, reflections.ID, false, 10)
		if err != nil || len(facts) != 1 || facts[0].Kind != models.FactReflection {
			t.Errorf("the page of reflections holds %v %v", facts, err)
		}
	})

	if written := worker.reflectAcrossThemes(t.Context(), run, budget, now.Add(6*24*time.Hour)); written != 0 || len(sentPrompts()) != 1 {
		t.Errorf("reflected across the themes again within the week")
	}
	if written := worker.reflectAcrossThemes(t.Context(), run, budget, now.Add(8*24*time.Hour)); written != 1 || len(sentPrompts()) != 2 {
		t.Errorf("did not reflect across the themes after a week")
	}
}
