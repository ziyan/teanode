package agent

import (
	"strings"
	"sync/atomic"
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
// observation needs two citations of what its prompt showed.
func TestTheNightReflectsOnATheme(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, sentPrompts := promptedProvider(t, func(string) string { return reflectionAnswerText })
	worker, run := digestSplitWorldWith(t, database, provider.URL, synthesizeOnJudge)
	idByPath := reflectionWorld(t, database, run.Agent.ID)

	record := &models.AgentDream{}
	worker.dreamReflect(t.Context(), run, record, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if record.ReflectionsWritten != 1 || len(sentPrompts()) != 1 {
		t.Fatalf("%d reflections written with %d calls", record.ReflectionsWritten, len(sentPrompts()))
	}
	if prompt := sentPrompts()[0]; !strings.Contains(prompt, "projects/orchard-north#1 The trees were pruned late again.") {
		t.Errorf("the prompt does not show the members' facts to cite: %q", prompt)
	}
	if !isAskedOfModel(sentPrompts()[0], "judge") {
		t.Errorf("the reflection was not asked of the synthesize model")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(run.Agent.ID, idByPath["themes/orchard-work"], false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("ListAgentFacts: %v %s", facts, err)
		}
		if facts[0].Kind != models.FactReflection || facts[0].ReflectionKind() != "pattern" {
			t.Errorf("the reflection is %+v", facts[0])
		}
		if got := strings.Join(facts[0].Citations(), " "); got != "projects/orchard-north#1 projects/orchard-south#1" {
			t.Errorf("the reflection cites %q", got)
		}
	})

	// Nothing changed: nothing is asked.
	worker.dreamReflect(t.Context(), run, &models.AgentDream{}, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if len(sentPrompts()) != 1 {
		t.Errorf("a theme with the same overview was reflected on again")
	}
}

// Refreshing a theme's reflections adds to what stands: a standing
// observation goes only when an answer names it, replaced by an
// observation that passed its checks or retired with a reason, and
// saying less, or nothing, keeps the rest.
func TestAReflectionReplacesOrRetiresOnlyWhatItNames(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	var answer atomic.Value
	answer.Store(reflectionAnswerText)
	provider, sentPrompts := promptedProvider(t, func(string) string { return answer.Load().(string) })
	worker, run := digestSplitWorldWith(t, database, provider.URL, synthesizeOnJudge)
	idByPath := reflectionWorld(t, database, run.Agent.ID)
	themeId := idByPath["themes/orchard-work"]

	reflectAgain := func(answerText string) {
		t.Helper()
		answer.Store(answerText)
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if err := tx.SetAgentNodeOverview(run.Agent.ID, themeId, "## What it is\n\nThree orchards.", nil, "again", time.Now()); err != nil {
				t.Fatalf("SetAgentNodeOverview: %s", err)
			}
		})
		worker.dreamReflect(t.Context(), run, &models.AgentDream{}, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	}
	// standing is the live reflections' texts by their references.
	standing := func() map[string]string {
		t.Helper()
		texts := map[string]string{}
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			facts, err := tx.ListAgentFacts(run.Agent.ID, themeId, false, 20)
			if err != nil {
				t.Fatalf("ListAgentFacts: %s", err)
			}
			for _, fact := range facts {
				texts[fact.Reference("themes/orchard-work")] = fact.Text
			}
		})
		return texts
	}
	// reflectionNamed is a reflection, live or not, by its reference.
	reflectionNamed := func(reference string) *models.AgentFact {
		t.Helper()
		var found *models.AgentFact
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			facts, err := tx.ListAgentFacts(run.Agent.ID, themeId, true, 20)
			if err != nil {
				t.Fatalf("ListAgentFacts: %s", err)
			}
			for _, fact := range facts {
				if fact.Reference("themes/orchard-work") == reference {
					found = fact
				}
			}
		})
		if found == nil {
			t.Fatalf("no reflection %s", reference)
		}
		return found
	}

	worker.dreamReflect(t.Context(), run, &models.AgentDream{}, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if got := standing(); len(got) != 1 || got["themes/orchard-work#1"] == "" {
		t.Fatalf("after the first reflection: %v", got)
	}

	// A partial refresh: one new observation, naming nothing. The first
	// stays, and the prompt showed it by its name.
	reflectAgain(`{"reflections": [{"text": "Late pruning puts the harvest at risk.", "reflectionKind": "risk", "citations": ["projects/orchard-north#1", "projects/orchard-east#1"]}]}`)
	if prompt := sentPrompts()[len(sentPrompts())-1]; !strings.Contains(prompt, "themes/orchard-work#1 (pattern) Pruning runs late in every orchard.") {
		t.Errorf("the prompt does not show the standing observation by its name: %q", prompt)
	}
	if got := standing(); len(got) != 2 || got["themes/orchard-work#1"] == "" || got["themes/orchard-work#2"] == "" {
		t.Fatalf("a partial refresh left %v", got)
	}

	// One that replaces the first by name: the first is superseded by
	// it, the second is untouched.
	reflectAgain(`{"reflections": [{"text": "Pruning runs late in every orchard, and later each year.", "reflectionKind": "trend", "citations": ["projects/orchard-north#1", "projects/orchard-south#1"], "replacedObservations": ["themes/orchard-work#1"]}]}`)
	if got := standing(); len(got) != 2 || got["themes/orchard-work#2"] == "" || got["themes/orchard-work#3"] == "" {
		t.Fatalf("a replacement left %v", got)
	}
	if first, third := reflectionNamed("themes/orchard-work#1"), reflectionNamed("themes/orchard-work#3"); first.SupersededBy != third.ID {
		t.Errorf("the first reflection is superseded by %q, not the one that replaced it", first.SupersededBy)
	}

	// Nothing worth saying: nothing goes.
	reflectAgain(`{"reflections": []}`)
	// An observation that fails its checks replaces nothing, and a
	// retirement without a reason, or of something not shown, retires
	// nothing.
	reflectAgain(`{"reflections": [{"text": "Something thin.", "reflectionKind": "risk", "citations": ["projects/orchard-north#1"], "replacedObservations": ["themes/orchard-work#2"]}],
		"retiredObservations": [{"observation": "themes/orchard-work#3"}, {"observation": "themes/orchard-work#9", "retiredReason": "made up"}, {"observation": "projects/orchard-north#1", "retiredReason": "not an observation"}]}`)
	if got := standing(); len(got) != 2 || got["themes/orchard-work#2"] == "" || got["themes/orchard-work#3"] == "" {
		t.Fatalf("an empty or failed refresh left %v", got)
	}

	// A retirement with a reason: struck, with nothing standing in its
	// place, and readable afterwards.
	reflectAgain(`{"reflections": [], "retiredObservations": [{"observation": "themes/orchard-work#2", "retiredReason": "The harvest came in on time."}]}`)
	if got := standing(); len(got) != 1 || got["themes/orchard-work#3"] == "" {
		t.Fatalf("a retirement left %v", got)
	}
	if retired := reflectionNamed("themes/orchard-work#2"); !retired.Dormant || retired.SupersededBy != "" {
		t.Errorf("the retired reflection is %+v", retired)
	}
}

// The top-level themes are reflected on together once a week, onto the
// person's own page of reflections.
func TestTheThemesAreReflectedOnTogetherOnceAWeek(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	answer := `{"reflections": [{"text": "Both themes follow the seasons.", "reflectionKind": "trend", "citations": ["themes/orchard-work", "themes/tide-tables"]}]}`
	provider, sentPrompts := promptedProvider(t, func(string) string { return answer })
	worker, run := digestSplitWorldWith(t, database, provider.URL, synthesizeOnJudge)
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
	if !isAskedOfModel(sentPrompts()[0], "judge") {
		t.Errorf("the weekly reflection was not asked of the synthesize model")
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
