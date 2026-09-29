package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// surveyPageFor writes a page, with an overview when one is given, and
// says its id.
func surveyPageFor(t *testing.T, tx db.Transaction, agentId, path, overview string, importance float32) string {
	t.Helper()
	page, err := tx.PutAgentNode(&models.AgentNode{
		AgentID: agentId, Path: path, Kind: models.NodeTopic, Name: models.LastSegment(path), Summary: "About " + models.LastSegment(path) + ".", Importance: importance,
	})
	if err != nil {
		t.Fatalf("PutAgentNode %q: %s", path, err)
	}
	if overview != "" {
		if err := tx.SetAgentNodeOverview(agentId, page.ID, overview, nil, "written", time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview %q: %s", path, err)
		}
	}
	return page.ID
}

// surveyAbout links a theme to a member the way the theme phase does.
func surveyAbout(t *testing.T, tx db.Transaction, agentId, themeId, memberId string) {
	t.Helper()
	if err := tx.PutAgentEdge(&models.AgentEdge{
		AgentID: agentId, FromID: themeId, ToID: memberId, Relation: models.EdgeAboutPlace, Weight: 1, Status: models.EdgeStated,
		Evidence: []models.Evidence{{Kind: models.EvidenceDream, Quote: themeEvidenceQuote}},
	}); err != nil {
		t.Fatalf("PutAgentEdge: %s", err)
	}
}

func surveyPaths(scope *surveyScope) []string {
	paths := make([]string, 0, len(scope.pages))
	for _, asked := range scope.pages {
		paths = append(paths, asked.page.Path)
	}
	return paths
}

// What a survey covers follows the structure: everything is the top of
// the themes; a theme of themes is its themes; a theme of pages is its
// members that have an overview, with its reflections; any other page
// is itself and the pages under it that have one. Where there are no
// themes, it is the pages that have an overview.
func TestTheSurveyScopeFollowsTheThemesAndThePages(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	_, run := digestSplitWorld(t, database, "http://127.0.0.1:1")
	agentId := run.Agent.ID
	written := "## What it is\n\nA part of the outdoors."
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		surveyPageFor(t, tx, agentId, "themes/outdoor-life", written, 0.1)
		orchardWork := surveyPageFor(t, tx, agentId, "themes/outdoor-life/orchard-work", written, 0.1)
		tideTables := surveyPageFor(t, tx, agentId, "themes/outdoor-life/tide-tables", written, 0.1)
		surveyPageFor(t, tx, agentId, "themes/loose-ends", written, 0.1)
		surveyPageFor(t, tx, agentId, "themes/unwritten", "", 0.1)
		north := surveyPageFor(t, tx, agentId, "projects/orchard-north", written, 0.9)
		south := surveyPageFor(t, tx, agentId, "projects/orchard-south", written, 0.5)
		east := surveyPageFor(t, tx, agentId, "projects/orchard-east", "", 0.7)
		surveyPageFor(t, tx, agentId, "projects/orchard-north/pruning", written, 0.2)
		surveyPageFor(t, tx, agentId, "projects/orchard-north/fencing", "", 0.2)
		tides := surveyPageFor(t, tx, agentId, "topics/tides", written, 0.3)
		for _, memberId := range []string{north, south, east} {
			surveyAbout(t, tx, agentId, orchardWork, memberId)
		}
		surveyAbout(t, tx, agentId, tideTables, tides)
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: orchardWork, Kind: models.FactReflection, Text: "Pruning runs late in every orchard.", Inferred: true,
			Evidence: []models.Evidence{
				{Kind: models.EvidenceDream, Quote: models.ReflectionEvidencePrefix + "pattern"},
				{Kind: models.EvidenceMemory, ID: north, Quote: "projects/orchard-north"},
				{Kind: models.EvidenceMemory, ID: south, Quote: "projects/orchard-south"},
			},
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: north, Kind: models.FactPlain, Text: "The north orchard has forty trees."}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})

	resolve := func(scopePath string) *surveyScope {
		t.Helper()
		var scope *surveyScope
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			if scope, err = resolveSurveyScope(tx, agentId, scopePath); err != nil {
				t.Fatalf("resolveSurveyScope %q: %s", scopePath, err)
			}
		})
		return scope
	}
	for _, expected := range []struct {
		scopePath string
		paths     string
	}{
		{"", "themes/loose-ends themes/outdoor-life"},
		{"themes/outdoor-life", "themes/outdoor-life/orchard-work themes/outdoor-life/tide-tables"},
		{"themes/outdoor-life/orchard-work", "projects/orchard-north projects/orchard-south"},
		{"projects/orchard-north", "projects/orchard-north projects/orchard-north/pruning"},
		{"projects/orchard-east", "projects/orchard-east"},
		// A page whose children are not themes still reaches the theme
		// found under it: all three of the orchard theme's members are
		// projects, and none of the tide theme's is.
		{"projects", "projects/orchard-north projects/orchard-south themes/outdoor-life/orchard-work"},
	} {
		if got := strings.Join(surveyPaths(resolve(expected.scopePath)), " "); got != expected.paths {
			t.Errorf("a survey of %q asks %q, not %q", expected.scopePath, got, expected.paths)
		}
	}

	theme := resolve("themes/outdoor-life/orchard-work")
	if len(theme.reflections) != 1 || !strings.Contains(theme.reflections[0], "Pruning runs late") || !strings.Contains(theme.reflections[0], "citing projects/orchard-north") {
		t.Errorf("the theme's reflections are %q", theme.reflections)
	}
	north := theme.pages[0]
	if len(north.facts) != 1 || north.facts[0] != "projects/orchard-north#1 The north orchard has forty trees." {
		t.Errorf("the north orchard's run is shown %q", north.facts)
	}
	if strings.Join(north.heldPages, "; ") != "projects/orchard-north/fencing — fencing; projects/orchard-north/pruning — pruning" {
		t.Errorf("the north orchard's run is named %q", north.heldPages)
	}
	orchardWork := resolve("themes/outdoor-life").pages[0]
	if len(orchardWork.reflections) != 1 || len(orchardWork.heldPages) != 3 {
		t.Errorf("a theme's run is shown its reflections %q and its members %q", orchardWork.reflections, orchardWork.heldPages)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := resolveSurveyScope(tx, agentId, "projects/nowhere"); !errors.Is(err, errNoSuchScope) {
			t.Errorf("a scope that names no page: %v", err)
		}
	})

	// A graph with no themes: the pages that have an overview.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "bob", Name: "Bob Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		other, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		surveyPageFor(t, tx, other.ID, "topics/kites", written, 0.4)
		surveyPageFor(t, tx, other.ID, "topics/string", "", 0.9)
		scope, err := resolveSurveyScope(tx, other.ID, "")
		if err != nil {
			t.Fatalf("resolveSurveyScope: %s", err)
		}
		if got := strings.Join(surveyPaths(scope), " "); got != "topics/kites" {
			t.Errorf("a survey of a graph with no themes asks %q", got)
		}
	})
}

// surveyProvider answers a survey's calls: each page's run after a short
// wait, counting how many are asked at once, and the combining call with
// a report. The pages named in isNothing answer that nothing bears on the
// question, and those in isEmpty answer nothing at all.
func surveyProvider(t *testing.T, isNothing, isEmpty map[string]bool) (*httptest.Server, func() (int, []string)) {
	t.Helper()
	var mutex sync.Mutex
	inFlight, mostInFlight := 0, 0
	var bodies []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		read, _ := io.ReadAll(request.Body)
		body := string(read)
		mutex.Lock()
		bodies = append(bodies, body)
		mutex.Unlock()
		answer := "## Strengths\n\nEvery orchard is pruned (projects/orchard-1#1)."
		if !strings.Contains(body, "Combine them into one report") {
			mutex.Lock()
			inFlight++
			mostInFlight = max(mostInFlight, inFlight)
			mutex.Unlock()
			time.Sleep(150 * time.Millisecond)
			mutex.Lock()
			inFlight--
			mutex.Unlock()
			answer = ""
			for number := 1; number <= 8; number++ {
				name := fmt.Sprintf("orchard-%d", number)
				if !strings.Contains(body, name+" (topic)") {
					continue
				}
				switch {
				case isNothing[name]:
					answer = surveyNothingRelevant
				case isEmpty[name]:
					answer = ""
				default:
					answer = fmt.Sprintf("The %s is pruned (projects/%s#1).", name, name)
				}
			}
		}
		content, _ := json.Marshal(answer)
		var asked struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(read, &asked)
		if asked.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\ndata: [DONE]\n\n", content)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{}}`, content)
	}))
	t.Cleanup(provider.Close)
	return provider, func() (int, []string) {
		mutex.Lock()
		defer mutex.Unlock()
		return mostInFlight, append([]string(nil), bodies...)
	}
}

// surveyOrchards is a theme of eight orchards, each with an overview.
func surveyOrchards(t *testing.T, database db.Database, agentId string) {
	t.Helper()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		themeId := surveyPageFor(t, tx, agentId, "themes/orchards", "## What it is\n\nEight orchards.", 0.1)
		for number := 1; number <= 8; number++ {
			memberId := surveyPageFor(t, tx, agentId, fmt.Sprintf("projects/orchard-%d", number), "## What it is\n\nAn orchard.", float32(number)/10)
			surveyAbout(t, tx, agentId, themeId, memberId)
		}
	})
}

// A survey asks each page in scope once, at most six at once, read-only
// and without the survey tool itself; hands the combining call the parts
// that said something; and ends its report with what it covered and
// what it could not. Every run is of kind survey.
func TestTheSurveyAsksEachPageAtMostSixAtOnceAndCombinesTheParts(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, asked := surveyProvider(t, map[string]bool{"orchard-2": true}, map[string]bool{"orchard-3": true})
	worker, run := digestSplitWorldWith(t, database, provider.URL, func(configuration *config.Configuration) {
		configuration.Agent.Models.Synthesize = "p:judge"
		configuration.Agent.Models.Research = "p:researcher"
	})
	surveyOrchards(t, database, run.Agent.ID)

	surveyed, err := worker.Survey(t.Context(), run.Agent, run.Owner, "What are the strengths of the orchards?", "themes/orchards")
	if err != nil {
		t.Fatalf("Survey: %s", err)
	}
	mostInFlight, bodies := asked()
	if len(bodies) != 9 {
		t.Fatalf("%d calls for eight pages and one report", len(bodies))
	}
	if mostInFlight > surveyConcurrency || mostInFlight < 2 {
		t.Errorf("%d pages were asked at once", mostInFlight)
	}
	var combining string
	for _, body := range bodies {
		if strings.Contains(body, "Combine them into one report") {
			combining = body
			if !isAskedOfModel(body, "researcher") {
				t.Errorf("the combining call is not on the research model")
			}
			continue
		}
		if !isAskedOfModel(body, "judge") {
			t.Errorf("a page's run is not on the synthesize model")
		}
		if !strings.Contains(body, `"name":"memory"`) || strings.Contains(body, `"name":"survey"`) {
			t.Errorf("a page's run has the lookups and not the survey")
		}
	}
	if !strings.Contains(combining, "The orchard-1 is pruned (projects/orchard-1#1).") || strings.Contains(combining, surveyNothingRelevant) {
		t.Errorf("the combining call is handed the wrong parts")
	}
	if len(surveyed.CoveredPaths) != 7 || strings.Join(surveyed.FailedPaths, " ") != "projects/orchard-3" {
		t.Errorf("covered %v, failed %v", surveyed.CoveredPaths, surveyed.FailedPaths)
	}
	if !strings.HasPrefix(surveyed.Report, "## Strengths") || !strings.Contains(surveyed.Report, "Covered: `projects/orchard-8`") ||
		!strings.Contains(surveyed.Report, "Not covered, the run did not finish: `projects/orchard-3`.") {
		t.Errorf("the report is %q", surveyed.Report)
	}
	if len(surveyed.RunIDs) != 9 {
		t.Fatalf("%d runs recorded", len(surveyed.RunIDs))
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, runId := range surveyed.RunIDs {
			conversation, err := tx.GetAgentConversation(runId)
			if err != nil || conversation == nil {
				t.Fatalf("GetAgentConversation: %v %v", conversation, err)
			}
			if conversation.JobKind != string(models.AgentJobSurvey) || !strings.HasPrefix(conversation.Title, "Survey: What are the strengths") {
				t.Errorf("the run is %q of kind %q", conversation.Title, conversation.JobKind)
			}
		}
	})

	// A survey whose caller has gone stops, and says so.
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := worker.Survey(cancelled, run.Agent, run.Owner, "Anything?", "themes/orchards"); err == nil {
		t.Errorf("a cancelled survey answered")
	}
}

// The tool is the survey as a turn calls it: offered to a person's turn
// and not to a run with nobody present, reading, and answering with the
// report and where the runs are.
func TestTheSurveyToolAnswersWithTheReport(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	provider, asked := surveyProvider(t, nil, nil)
	worker, run := digestSplitWorld(t, database, provider.URL)
	surveyOrchards(t, database, run.Agent.ID)

	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	personTurn, err := worker.Ask(&AskSettings{
		Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "hello", Surface: "cli",
	})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events, unsubscribe := personTurn.Subscribe()
	for range events {
	}
	unsubscribe()
	if _, bodies := asked(); len(bodies) == 0 || !strings.Contains(bodies[0], `"name":"survey"`) || !strings.Contains(bodies[0], `"name":"background_work"`) {
		t.Errorf("a person's turn is not offered the survey and what it leaves in the background")
	}

	tool := worker.surveyTool()
	if tool.Risk != tools.RiskRead {
		t.Errorf("the survey is %q", tool.Risk)
	}
	turn := &AskRun{agent: worker, settings: &AskSettings{Agent: run.Agent, Owner: run.Owner}, promptMemories: map[string]bool{}}
	result, err := tool.Run(tools.WithRun(t.Context(), turn), &tools.Call{Arguments: json.RawMessage(`{"question": "What are the strengths of the orchards?", "scope": "themes/orchards", "background": false}`)})
	if err != nil {
		t.Fatalf("the survey tool: %s", err)
	}
	var answered struct {
		Report       string   `json:"report"`
		CoveredPaths []string `json:"coveredPaths"`
		FailedPaths  []string `json:"failedPaths"`
		RunIDs       []string `json:"runIds"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answered); err != nil {
		t.Fatalf("the answer is not an object: %s", err)
	}
	if !strings.HasPrefix(answered.Report, "## Strengths") || len(answered.CoveredPaths) != 8 || len(answered.RunIDs) != 9 {
		t.Errorf("the tool answered %+v", answered)
	}
	if _, err := tool.Run(tools.WithRun(t.Context(), turn), &tools.Call{Arguments: json.RawMessage(`{"question": " "}`)}); err == nil {
		t.Errorf("a survey of no question ran")
	}
	// One survey a turn: a second is refused without a call.
	_, bodiesBefore := asked()
	if _, err := tool.Run(tools.WithRun(t.Context(), turn), &tools.Call{Arguments: json.RawMessage(`{"question": "And the weaknesses?"}`)}); !errors.Is(err, errSurveyedThisTurn) {
		t.Errorf("a second survey in one turn: %v", err)
	}
	if _, bodiesAfter := asked(); len(bodiesAfter) != len(bodiesBefore) {
		t.Errorf("a refused survey made %d calls", len(bodiesAfter)-len(bodiesBefore))
	}
}
