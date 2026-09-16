package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A whole night, through the worker: the graph's links are reweighted by
// what was used together, the walk finds a relation nobody wrote down,
// and rehearsal reports the question memory could not answer.
//
// The two generative phases are the ones worth a test through the worker
// rather than at the database: each is a prompt, a model, a parse and a
// write, and every one of those four is somewhere the phase can silently
// do nothing.
func TestDreamingANight(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var asked sync.Mutex
	var prompts []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "embeddings") {
			writeMeaning(writer, request)
			return
		}
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		prompt := ""
		if len(body.Messages) > 0 {
			prompt = body.Messages[len(body.Messages)-1].Content
		}
		asked.Lock()
		prompts = append(prompts, prompt)
		asked.Unlock()

		answer := "{}"
		switch {
		case strings.Contains(prompt, "have no link of their own"):
			answer = `{"related": true, "relation": "works_on", "note": "led the controls work on it"}`
		case strings.Contains(prompt, "most likely to ask you tomorrow"):
			// One the graph can answer and one it cannot.
			answer = `{"questions": ["what is the portal?", "what did the plumber quote for the boiler?"]}`
		}
		encoded, _ := json.Marshal(answer)
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			encoded)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	configuration.Agent.Models.Scan = "fake:scan"
	// Rehearsal asks the graph whether it knows a thing, and that is asked
	// by meaning: without an embedding model it cannot tell, and reports
	// no gaps rather than reporting every question as one.
	configuration.Agent.Models.Embedding = "fake:meaning"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var found *models.Agent
	var alice, portal, gripper *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(found.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		node := func(path string, kind models.AgentNodeKind, name, summary string) *models.AgentNode {
			written, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: found.ID, Path: path, Kind: kind, Name: name, Summary: summary,
			})
			if err != nil {
				t.Fatalf("PutAgentNode %q: %s", path, err)
			}
			return written
		}
		// A chain: Alice — Portal — Gripper. The two ends are connected
		// but not joined, which is what the walk is for.
		alice = node("people/alice-chen", models.NodePerson, "Alice Chen", "a controls engineer")
		portal = node("projects/portal", models.NodeProject, "Portal", "the customer-facing site")
		gripper = node("things/gripper", models.NodeThing, "Gripper", "the payload gripper")
		for _, pair := range [][2]string{{alice.ID, portal.ID}, {portal.ID, gripper.ID}} {
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: found.ID, FromID: pair[0], ToID: pair[1],
				Relation: models.EdgeRelatedTo, Weight: 2,
			}); err != nil {
				t.Fatalf("PutAgentEdge: %s", err)
			}
		}
		// Both ends of the first link wanted today, so the quiet half has
		// something to strengthen.
		if err := tx.TouchAgentNodes([]string{alice.ID, portal.ID}, time.Now()); err != nil {
			t.Fatalf("TouchAgentNodes: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: found.ID, NodeID: portal.ID, Kind: models.FactPlain,
			Text: "the portal is the site customers log in to",
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		if _, err := worker.Enqueue(tx, models.AgentJobDream, found.ID, "", "tonight"); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()

	var night *models.AgentDream
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		nights, err := tx.ListAgentDreams(found.ID, 5)
		if err != nil || len(nights) == 0 {
			t.Fatalf("a night happened: %v %s", nights, err)
		}
		night = nights[0]

		// The quiet half: the link whose both ends were wanted today ends
		// above where it started, and the one nothing touched below.
		edges, err := tx.ListAgentEdges(found.ID, portal.ID)
		if err != nil {
			t.Fatalf("ListAgentEdges: %s", err)
		}
		weights := map[string]float32{}
		for _, edge := range edges {
			weights[edge.FromPath+" "+edge.ToPath] = edge.Weight
		}
		if weights["people/alice-chen projects/portal"] <= weights["projects/portal things/gripper"] {
			t.Fatalf("the link used today ends above the one that was not: %v", weights)
		}

		// The generative half: a link the night worked out, with the
		// sentence that justifies it and below the weight of a stated one.
		joined, err := tx.ListAgentEdges(found.ID, alice.ID)
		if err != nil {
			t.Fatalf("ListAgentEdges: %s", err)
		}
		var noticed *models.AgentEdge
		for _, edge := range joined {
			if edge.ToPath == "things/gripper" || edge.FromPath == "things/gripper" {
				noticed = edge
			}
		}
		if noticed == nil {
			t.Fatalf("the walk's two ends are joined now: %v", joined)
		}
		if noticed.Note == "" {
			t.Fatalf("with the sentence that justifies it")
		}
		if noticed.Weight >= 1 {
			t.Fatalf("and below a stated link's weight, not %v", noticed.Weight)
		}
		if len(noticed.Evidence) == 0 {
			t.Fatalf("and the walk that suggested it, so somebody can disagree")
		}
	})

	if night.Strengthened == 0 {
		t.Fatalf("the night says how many links it reweighted")
	}
	if night.Associated != 1 {
		t.Fatalf("and that it found one connection, not %d", night.Associated)
	}
	// Rehearsal: two questions asked, and the one about a boiler nothing
	// in the graph mentions is reported as a gap rather than answered.
	if night.Rehearsed != 2 {
		t.Fatalf("two questions rehearsed, not %d", night.Rehearsed)
	}
	if night.Gaps != 1 {
		t.Fatalf("one of them unanswerable, not %d", night.Gaps)
	}
	gap := ""
	for _, proposal := range night.Proposals {
		if proposal.Kind == "gap" {
			gap = proposal.Reason
		}
	}
	if !strings.Contains(gap, "boiler") {
		t.Fatalf("and written down as the question it is, not %q", gap)
	}

	asked.Lock()
	defer asked.Unlock()
	walked := false
	for _, prompt := range prompts {
		if strings.Contains(prompt, "projects/portal") && strings.Contains(prompt, "have no link of their own") {
			walked = true
		}
	}
	if !walked {
		t.Fatalf("the walk put the way between the two ends to the model")
	}
	_ = gripper
}

// A newer build goes back over what an older one wrote.
//
// This is what the version on a row is for. The first real ingest filed
// twenty-eight lines saying no more than "X is a project", because the
// prompt of the day invited them; fixing the prompt does nothing about
// what is already on the pages, and nothing could find them except
// knowing which build had written them.
func TestDreamingRevisesWhatAnOlderBuildWrote(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "embeddings") {
			writeMeaning(writer, request)
			return
		}
		_, _ = fmt.Fprint(writer,
			`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	configuration.Agent.Models.Scan = "fake:scan"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var found *models.Agent
	var page *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(found.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		page, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: found.ID, Path: "projects/formatting", Kind: models.NodeProject, Name: "Formatting",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		for _, text := range []string{
			"Formatting is a project or work channel.",
			"The formatting work channel had activity in September 2026.",
			"Formatting runs as a check in CI and fails the build on a diff.",
		} {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: found.ID, NodeID: page.ID, Kind: models.FactPlain, Text: text,
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})

	// As an older build would have left them. Nothing above the store can
	// write a version other than its own, which is the point of it.
	dbtest.Exec(t, database, `UPDATE "agent_fact" SET "version" = '0.0.1-old'`)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := worker.Enqueue(tx, models.AgentJobDream, found.ID, "", "tonight"); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(found.ID, page.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(facts) != 1 {
			lines := make([]string, 0, len(facts))
			for _, fact := range facts {
				lines = append(lines, fact.Text)
			}
			t.Fatalf("the two that say nothing are gone and the one that says something stays:\n%s",
				strings.Join(lines, "\n"))
		}
		if !strings.Contains(facts[0].Text, "fails the build") {
			t.Fatalf("and it is the one about the thing, not %q", facts[0].Text)
		}
		// Marked as this build's, so tomorrow night looks elsewhere.
		if facts[0].Version == "0.0.1-old" || facts[0].Version == "" {
			t.Fatalf("what was looked at carries the build that looked, not %q", facts[0].Version)
		}
		// And the striking is in the page's history, so it can be undone.
		revisions, err := tx.ListAgentRevisions(found.ID, page.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		struck := 0
		for _, revision := range revisions {
			if revision.Kind == models.RevisionFactGone && revision.Actor == models.ActorDream {
				struck++
				if revision.TextBefore() == "" {
					t.Fatalf("with what the line used to say")
				}
			}
		}
		if struck != 2 {
			t.Fatalf("two strikings in the history, not %d", struck)
		}
	})
}
