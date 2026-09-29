package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// promptedProvider answers each call with what answer makes of its
// prompt, and keeps every prompt it was sent.
func promptedProvider(t *testing.T, answer func(prompt string) string) (*httptest.Server, func() []string) {
	t.Helper()
	var mutex sync.Mutex
	var prompts []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var asked struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &asked)
		prompt := string(body)
		mutex.Lock()
		prompts = append(prompts, prompt)
		mutex.Unlock()
		content, err := json.Marshal(answer(prompt))
		if err != nil {
			t.Errorf("json.Marshal: %s", err)
		}
		if asked.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\ndata: [DONE]\n\n", content)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{}}`, content)
	}))
	t.Cleanup(provider.Close)
	return provider, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), prompts...)
	}
}

// namingAnswer names a group after the members its prompt shows: the
// block between the members tags, as the request body escapes them.
func namingAnswer(prompt string) string {
	members := prompt
	if start := strings.Index(prompt, `\u003cmembers\u003e`); start >= 0 {
		members = prompt[start:]
		if end := strings.Index(members, `\u003c/members\u003e`); end >= 0 {
			members = members[:end]
		}
	}
	switch {
	case strings.Contains(prompt, "These are themes"):
		return `{"name": "Outdoor life", "opening": "Orchards and tides."}`
	case strings.Contains(members, "orchard"):
		return `{"name": "Orchard work", "opening": "The orchard and its trees."}`
	case strings.Contains(members, "tide"):
		return `{"name": "Tide tables", "opening": "Reading the tides."}`
	}
	return `{"sections":[{"heading":"What it is","text":"A group of pages."}],"citedPages":[],"citedFiles":[]}`
}

// themeGraphWorld makes two groups of four linked pages, joined by one
// link, and answers with the ids by path.
func themeGraphWorld(t *testing.T, database db.Database, agentId string) map[string]string {
	t.Helper()
	idByPath := map[string]string{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, group := range [][]string{
			{"projects/orchard-north", "projects/orchard-south", "projects/orchard-east", "projects/orchard-west"},
			{"topics/tide-spring", "topics/tide-neap", "topics/tide-ebb", "topics/tide-flood"},
		} {
			for _, path := range group {
				page, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: path, Kind: models.NodeTopic, Name: models.LastSegment(path)})
				if err != nil {
					t.Fatalf("PutAgentNode: %s", err)
				}
				idByPath[path] = page.ID
			}
			for left := range group {
				for right := left + 1; right < len(group); right++ {
					link(t, tx, agentId, idByPath[group[left]], idByPath[group[right]])
				}
			}
		}
		link(t, tx, agentId, idByPath["projects/orchard-west"], idByPath["topics/tide-spring"])
	})
	return idByPath
}

func link(t *testing.T, tx db.Transaction, agentId, fromId, toId string) {
	t.Helper()
	if err := tx.PutAgentEdge(&models.AgentEdge{
		AgentID: agentId, FromID: fromId, ToID: toId, Relation: models.EdgeRelatedTo, Weight: 1, Status: models.EdgeStated,
		Evidence: []models.Evidence{{Kind: models.EvidenceConversation, Quote: "said so"}},
	}); err != nil {
		t.Fatalf("PutAgentEdge: %s", err)
	}
}

// themeMembers is the paths a theme's own links point at.
func themeMembers(t *testing.T, tx db.Transaction, agentId, themePath string) []string {
	t.Helper()
	theme := mustPage(t, tx, agentId, themePath)
	edges, err := tx.ListAgentEdges(agentId, theme.ID)
	if err != nil {
		t.Fatalf("ListAgentEdges: %s", err)
	}
	var members []string
	for _, edge := range edges {
		if edge.FromID == theme.ID && edge.Relation == models.EdgeAboutPlace && isThemeOwnLink(edge) {
			members = append(members, edge.ToPath)
		}
	}
	return members
}

// A night finds two groups joined by one link as two themes, groups the
// two as a theme of themes, and names all three; the next night keeps
// them where they are as a member moves and another joins, asking the
// model nothing; and when a group comes apart its theme sleeps.
func TestTheNightKeepsThemesInTwoLevels(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, sentPrompts := promptedProvider(t, namingAnswer)
	worker, run := digestSplitWorld(t, database, provider.URL)
	idByPath := themeGraphWorld(t, database, run.Agent.ID)

	var memberInputsBefore string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if memberInputsBefore, err = tx.AgentNodeOverviewInputs(run.Agent.ID, idByPath["projects/orchard-north"]); err != nil {
			t.Fatalf("AgentNodeOverviewInputs: %s", err)
		}
	})

	record := &models.AgentDream{}
	worker.dreamThemes(t.Context(), run, record, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if record.ThemesMade != 3 || len(sentPrompts()) != 3 {
		t.Fatalf("%d themes made with %d calls; two themes and one of themes were due", record.ThemesMade, len(sentPrompts()))
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if got := themeMembers(t, tx, run.Agent.ID, "themes/outdoor-life/orchard-work"); len(got) != 4 {
			t.Errorf("the orchard theme's members are %v", got)
		}
		if got := themeMembers(t, tx, run.Agent.ID, "themes/outdoor-life/tide-tables"); len(got) != 4 {
			t.Errorf("the tide theme's members are %v", got)
		}
		outdoor := mustPage(t, tx, run.Agent.ID, "themes/outdoor-life")
		if outdoor.Kind != models.NodeTopic || outdoor.Summary != "Orchards and tides." {
			t.Errorf("the theme of themes is %+v", outdoor)
		}
		// A theme's links to a page are not what the page is written from.
		memberInputsAfter, err := tx.AgentNodeOverviewInputs(run.Agent.ID, idByPath["projects/orchard-north"])
		if err != nil {
			t.Fatalf("AgentNodeOverviewInputs: %s", err)
		}
		if memberInputsAfter != memberInputsBefore {
			t.Errorf("joining a theme made a member's overview due")
		}
		due, err := tx.ListAgentThemesForOverview(run.Agent.ID, 10)
		if err != nil {
			t.Fatalf("ListAgentThemesForOverview: %s", err)
		}
		if len(due) != 3 || due[2].Path != "themes/outdoor-life" {
			t.Errorf("the themes due an overview are %v", pathsOf(due))
		}
	})

	// The overviews: a theme is written from its members, and the theme
	// of themes last.
	overviews := &models.AgentDream{}
	worker.dreamOverviews(t.Context(), run, overviews, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	var themePrompt string
	for _, prompt := range sentPrompts() {
		if strings.Contains(prompt, `## The page\n\nthemes/outdoor-life/orchard-work `) {
			themePrompt = prompt
		}
	}
	if !strings.Contains(themePrompt, "This page is a theme") || !strings.Contains(themePrompt, "### projects/orchard-north") {
		t.Errorf("the theme's overview was not written from its members: %q", themePrompt)
	}

	// A member moves and another page joins the group: the theme keeps
	// its path and nothing is named.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.MoveAgentNode(run.Agent.ID, "projects/orchard-north", "topics"); err != nil {
			t.Fatalf("MoveAgentNode: %s", err)
		}
		page, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "projects/orchard-cider", Kind: models.NodeProject, Name: "cider"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for _, path := range []string{"projects/orchard-south", "projects/orchard-east", "projects/orchard-west"} {
			link(t, tx, run.Agent.ID, page.ID, idByPath[path])
		}
	})
	asked := len(sentPrompts())
	again := &models.AgentDream{}
	worker.dreamThemes(t.Context(), run, again, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if again.ThemesMade != 0 || len(sentPrompts()) != asked {
		t.Errorf("a night with the same groups made %d themes and asked %d times", again.ThemesMade, len(sentPrompts())-asked)
	}
	if again.ThemesUpdated != 1 {
		t.Errorf("%d themes updated; the orchard gained a member", again.ThemesUpdated)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		got := themeMembers(t, tx, run.Agent.ID, "themes/outdoor-life/orchard-work")
		if len(got) != 5 || !strings.Contains(strings.Join(got, " "), "topics/orchard-north") {
			t.Errorf("the orchard theme's members are %v", got)
		}
	})

	// The tides come apart: their theme sleeps and loses its links, the
	// theme of themes has one theme left and sleeps too, and the orchard
	// theme moves up to stand on its own.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, edge := range mustEdges(t, tx, run.Agent.ID) {
			if edge.Relation == models.EdgeRelatedTo && strings.HasPrefix(edge.ToPath, "topics/tide-") {
				if err := tx.DeleteAgentEdge(run.Agent.ID, edge.FromID, edge.ToID, edge.Relation); err != nil {
					t.Fatalf("DeleteAgentEdge: %s", err)
				}
			}
		}
	})
	apart := &models.AgentDream{}
	worker.dreamThemes(t.Context(), run, apart, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if tides := mustPage(t, tx, run.Agent.ID, "themes/outdoor-life/tide-tables"); !tides.Dormant {
			t.Errorf("the tide theme is still awake")
		}
		if got := themeMembers(t, tx, run.Agent.ID, "themes/outdoor-life/tide-tables"); len(got) != 0 {
			t.Errorf("the sleeping tide theme still has members %v", got)
		}
		if outdoor := mustPage(t, tx, run.Agent.ID, "themes/outdoor-life"); !outdoor.Dormant {
			t.Errorf("the theme of themes with one theme left is still awake")
		}
		if orchard, err := tx.GetAgentNode(run.Agent.ID, "themes/orchard-work"); err != nil || orchard == nil || orchard.Dormant {
			t.Errorf("the orchard theme did not move up: %v %v", orchard, err)
		}
	})
}

func mustEdges(t *testing.T, tx db.Transaction, agentId string) []*models.AgentEdge {
	t.Helper()
	edges, err := tx.ListAgentEdgesByRelation(agentId, models.EdgeRelatedTo)
	if err != nil {
		t.Fatalf("ListAgentEdgesByRelation: %s", err)
	}
	return edges
}

func pathsOf(pages []*models.AgentNode) []string {
	paths := make([]string, 0, len(pages))
	for _, page := range pages {
		paths = append(paths, page.Path)
	}
	return paths
}

// A night of naming stops at the budget: a group it cannot pay to name
// is left for tomorrow, and nothing is made without a name.
func TestThemesAreNotNamedWithoutBudget(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, sentPrompts := promptedProvider(t, namingAnswer)
	worker, run := digestSplitWorld(t, database, provider.URL)
	themeGraphWorld(t, database, run.Agent.ID)

	record := &models.AgentDream{}
	worker.dreamThemes(t.Context(), run, record, &dreamBudget{exhausted: true})
	if record.ThemesMade != 0 || len(sentPrompts()) != 0 {
		t.Errorf("a night with nothing left made %d themes and asked %d times", record.ThemesMade, len(sentPrompts()))
	}
}
