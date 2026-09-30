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
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// overviewProvider answers every call with the same overview, and keeps
// every prompt it was sent in the order they came.
func overviewProvider(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	answer := `{"sections":[{"heading":"What it is","text":"A shed of garden tools, kept in order."},{"heading":"What stands out","text":""}],"citedPages":["things/garden-shed/rake","somewhere/made-up"],"citedFiles":["README.md","nowhere.txt"]}`
	content, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("json.Marshal: %s", err)
	}
	var mutex sync.Mutex
	var prompts []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		mutex.Lock()
		prompts = append(prompts, string(body))
		mutex.Unlock()
		var promptCount struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &promptCount)
		if promptCount.Stream {
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

// isAskedOfModel says whether a request to the provider named the model.
func isAskedOfModel(body, model string) bool {
	return strings.Contains(body, `"model":"`+model+`"`)
}

// synthesizeOnJudge assigns the synthesize work a model of its own, so a
// test can see which calls ask for it.
func synthesizeOnJudge(configuration *config.Configuration) {
	configuration.Agent.Models.Synthesize = "p:judge"
}

// A night writes the overviews that are due, the pages under a page
// before the page, cites only what it was shown, and shows a checkout's
// page its key files; the next night, with nothing changed, writes none.
func TestTheNightWritesOverviewsChildrenFirstAndOnlyWhenDue(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, sentPrompts := overviewProvider(t)
	worker, run := digestSplitWorldWith(t, database, provider.URL, synthesizeOnJudge)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "things/garden-shed", Kind: models.NodeThing, Name: "Garden shed"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for _, tool := range []string{"rake", "hoe"} {
			page, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "things/garden-shed/" + tool, Kind: models.NodeThing, Name: tool})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			for _, text := range []string{"The %s hangs on the left wall.", "The %s has an ash handle.", "The %s was sharpened in March."} {
				if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: fmt.Sprintf(text, tool)}); err != nil {
					t.Fatalf("AddAgentFact: %s", err)
				}
			}
		}

		// A checkout, with the line its profile keeps saying where it
		// is, and its readme indexed by the source that reads it.
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceComputer, Name: "code", Enabled: true, RootPath: "projects",
			Specification: models.AgentKnowledgeSpecification{Computer: "workbench", Path: "~/code"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		checkout, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "projects/example-app", Kind: models.NodeProject, Name: "example-app"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for key, text := range map[string]string{
			checkoutFactKey: checkoutLine("~/code/tools/example-app", "workbench"),
			"languages":     "Written in Go.",
			"history":       "12 commits by 2 people, May 2026 to June 2026.",
		} {
			if err := putKeyedRepositoryFact(tx, run.Agent.ID, checkout.ID, key, text, "0123456"); err != nil {
				t.Fatalf("putKeyedRepositoryFact: %s", err)
			}
		}
		readme, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: run.Agent.ID, SourceID: source.ID, ExternalID: "tools/example-app/README.md",
			Kind: models.AgentDocumentKind("file"), Title: "README.md", Hash: "hash-of-readme",
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if err := tx.ReplaceAgentChunks(readme, []*models.AgentChunk{{Text: "Example app turns seed catalogues into planting calendars."}}); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})

	// The first night writes the tools and the checkout; the shed, with
	// its tools due, waits.
	record := &models.AgentDream{}
	worker.dreamOverviews(t.Context(), run, record, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if record.OverviewsWritten != 3 {
		t.Fatalf("%d overviews were written; the two tools and the checkout were ready", record.OverviewsWritten)
	}
	askedAbout := func(prompts []string) map[string]bool {
		isAsked := map[string]bool{}
		for _, prompt := range prompts {
			for _, path := range []string{"things/garden-shed/rake", "things/garden-shed/hoe", "things/garden-shed", "projects/example-app"} {
				if strings.Contains(prompt, `## The page\n\n`+path+" ") {
					isAsked[path] = true
				}
			}
			if strings.Contains(prompt, `## The page\n\nprojects/example-app `) && !strings.Contains(prompt, "seed catalogues into planting calendars") {
				t.Errorf("the checkout's prompt does not carry its readme")
			}
		}
		return isAsked
	}
	firstNight := askedAbout(sentPrompts())
	for _, prompt := range sentPrompts() {
		if !isAskedOfModel(prompt, "judge") {
			t.Errorf("an overview was not written on the synthesize model")
		}
	}
	if !firstNight["things/garden-shed/rake"] || !firstNight["things/garden-shed/hoe"] || !firstNight["projects/example-app"] || firstNight["things/garden-shed"] {
		t.Errorf("the first night promptCount about %v", firstNight)
	}

	// The next night, the shed, from what its tools say now; the tools
	// are not written again.
	promptCount := len(sentPrompts())
	next := &models.AgentDream{}
	worker.dreamOverviews(t.Context(), run, next, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if secondNight := askedAbout(sentPrompts()[promptCount:]); !secondNight["things/garden-shed"] || secondNight["things/garden-shed/rake"] {
		t.Fatalf("the second night wrote %d overviews, asking about %v", next.OverviewsWritten, secondNight)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		shed, err := tx.GetAgentNode(run.Agent.ID, "things/garden-shed")
		if err != nil || shed == nil {
			t.Fatalf("GetAgentNode: %v %v", shed, err)
		}
		if shed.Overview != "## What it is\n\nA shed of garden tools, kept in order." || shed.OverviewWrittenAt == nil {
			t.Errorf("the shed's overview is %q", shed.Overview)
		}
		if len(shed.OverviewEvidence) != 1 || shed.OverviewEvidence[0].Quote != "things/garden-shed/rake" {
			t.Errorf("only the page the prompt showed is cited: %+v", shed.OverviewEvidence)
		}
		checkout, err := tx.GetAgentNode(run.Agent.ID, "projects/example-app")
		if err != nil || checkout == nil {
			t.Fatalf("GetAgentNode: %v %v", checkout, err)
		}
		if len(checkout.OverviewEvidence) != 1 || checkout.OverviewEvidence[0].Kind != models.EvidenceDocument ||
			checkout.OverviewEvidence[0].Quote != "tools/example-app/README.md" {
			t.Errorf("the checkout's overview cites its readme and nothing made up: %+v", checkout.OverviewEvidence)
		}
	})

	// The folder above the shed the night after; then nothing.
	worker.dreamOverviews(t.Context(), run, &models.AgentDream{}, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	again := &models.AgentDream{}
	worker.dreamOverviews(t.Context(), run, again, newDreamBudget(worker.settings.Configuration(), run.Agent, 1, 0))
	if again.OverviewsWritten != 0 {
		t.Errorf("a night with nothing changed wrote %d overviews", again.OverviewsWritten)
	}

	// With the night's share gone, nothing is promptCount at all.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.ClearAgentNodeOverviewInputs(run.Agent.ID, mustPage(t, tx, run.Agent.ID, "things/garden-shed").ID); err != nil {
			t.Fatalf("ClearAgentNodeOverviewInputs: %s", err)
		}
	})
	promptCount = len(sentPrompts())
	spent := &models.AgentDream{}
	worker.dreamOverviews(t.Context(), run, spent, &dreamBudget{exhausted: true})
	if spent.OverviewsWritten != 0 || len(sentPrompts()) != promptCount {
		t.Errorf("a night with no share left wrote %d overviews", spent.OverviewsWritten)
	}
}

func mustPage(t *testing.T, tx db.Transaction, agentId, path string) *models.AgentNode {
	t.Helper()
	page, err := tx.GetAgentNode(agentId, path)
	if err != nil || page == nil {
		t.Fatalf("GetAgentNode %q: %v %v", path, page, err)
	}
	return page
}

// The lines a profile keeps are read back as they are written.
func TestTheLinesAProfileKeepsAreReadBack(t *testing.T) {
	where, computerName, isCheckout := checkoutLocationOf(checkoutLine("~/code/tools/example-app", "workbench"))
	if !isCheckout || where != "~/code/tools/example-app" || computerName != "workbench" {
		t.Errorf("checkoutLocationOf: %q %q %v", where, computerName, isCheckout)
	}
	if _, _, isCheckout := checkoutLocationOf("Written in Go."); isCheckout {
		t.Errorf("a line that is not the checkout's was read as one")
	}
	for _, each := range []struct {
		line, directory, buildFile string
	}{
		{componentLine("example-app", computerComponent("lib/parser", "lib/parser/go.mod")), "lib/parser", "lib/parser/go.mod"},
		{componentLine("example-app", computerComponent("", "build/example.modules")), "", "build/example.modules"},
	} {
		directory, buildFile, isComponent := componentLocationOf(each.line)
		if !isComponent || directory != each.directory || buildFile != each.buildFile {
			t.Errorf("componentLocationOf(%q) = %q %q %v", each.line, directory, buildFile, isComponent)
		}
	}
}

func computerComponent(directory, buildFile string) computer.RepositoryComponent {
	return computer.RepositoryComponent{Path: directory, Name: "parser", Ecosystem: computer.EcosystemGo, File: buildFile}
}

// How an overview stands is counted by code: of the pages under it, how
// many there are and how many its prompt shows, and how many of those have
// no overview of their own; and whether what it is written from changed
// since. The prompt says when it shows only the most important of them.
func TestAnOverviewSaysWhatItCoversAndWhetherItIsCurrent(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	_, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	agentId := run.Agent.ID
	written := "## What it is\n\nA row of trees."
	var orchard *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		surveyPageFor(t, tx, agentId, "projects/orchard", written, 0.9)
		for index := 0; index < overviewChildCount+4; index++ {
			overview := written
			if index%10 == 0 {
				overview = ""
			}
			surveyPageFor(t, tx, agentId, fmt.Sprintf("projects/orchard/row-%02d", index), overview, float32(index)/100)
		}
		var err error
		if orchard, err = tx.GetAgentNode(agentId, "projects/orchard"); err != nil {
			t.Fatal(err)
		}
		inputsNow, err := tx.AgentNodeOverviewInputs(agentId, orchard.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.SetAgentNodeOverview(agentId, orchard.ID, written, nil, inputsNow, time.Now()); err != nil {
			t.Fatal(err)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		state, err := OverviewStateOf(tx, agentId, orchard)
		if err != nil {
			t.Fatal(err)
		}
		coverage := state.Coverage
		if state.IsStale || coverage.ChildCount != overviewChildCount+4 || coverage.ChildShownCount != overviewChildCount {
			t.Fatalf("stale %v, %d of %d children", state.IsStale, coverage.ChildShownCount, coverage.ChildCount)
		}
		// Rows 00, 10, 20 and 30 have none; the most important thirty
		// shown are rows 04 to 33, which hold rows 10, 20 and 30.
		if coverage.ChildWithoutOverviewCount != 3 {
			t.Errorf("%d shown children without an overview, want 3", coverage.ChildWithoutOverviewCount)
		}
		inputs, err := readOverviewInputs(tx, agentId, orchard)
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := render("overview.txt", map[string]any{
			"Path": orchard.Path, "Name": orchard.Name, "Kind": string(orchard.Kind), "Children": inputs.children, "Coverage": inputs.coverage,
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("These are the %d most important of the\n%d pages under it", overviewChildCount, overviewChildCount+4); !strings.Contains(prompt, want) {
			t.Errorf("the prompt does not say it shows the most important of them:\n%s", prompt)
		}

		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: orchard.ID, Kind: models.FactPlain, Text: "The orchard was replanted."}); err != nil {
			t.Fatal(err)
		}
		if state, err = OverviewStateOf(tx, agentId, orchard); err != nil || !state.IsStale {
			t.Errorf("a new fact did not make the overview stale: %v", err)
		}
	})
}
