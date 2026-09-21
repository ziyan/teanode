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
func TestDreamingRunsEveryPhase(t *testing.T) {
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
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		// The last thing the person's side said: every call of a dream is
		// a turn of the loop now, so the prompt sits after the persona.
		prompt := ""
		for _, message := range body.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
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
		case strings.Contains(prompt, "Do these notes answer the question"):
			// The judge: the notes nearest "what is the portal?" are about
			// the portal and answer it; nothing near the boiler does.
			answer = `{"answered": false, "facts": []}`
			if strings.Contains(prompt, "what is the portal?") {
				// Naming the note it answered from, which is what a
				// supported answer has to do: a yes that can point at
				// nothing liked the subject rather than found the answer.
				answer = `{"answered": true, "facts": [1]}`
			}
		}
		encoded, _ := json.Marshal(answer)
		if body.Stream {
			// A round of the loop streams: one chunk with the whole answer.
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				encoded)
			return
		}
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
	// Every call a dream makes is a turn of the loop, which acts as the
	// person and so needs somebody to act as.
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	var found *models.Agent
	var alice, portal, latch *models.AgentNode
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
		// A chain: Alice — Portal — Latch. The two ends are connected
		// but not joined, which is what the walk is for.
		alice = node("people/alice-chen", models.NodePerson, "Alice Chen", "a controls engineer")
		portal = node("projects/portal", models.NodeProject, "Portal", "the customer-facing site")
		latch = node("things/latch", models.NodeThing, "Latch", "the payload latch")
		for _, pair := range [][2]string{{alice.ID, portal.ID}, {portal.ID, latch.ID}} {
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
		// And a night behind it. The fade and the rise are both over the
		// stretch since the last pass, so an agent that has never had one
		// leaves every weight where it is and writes the watermark down;
		// this test is about the pass that follows.
		lastNight := time.Now().Add(-6 * time.Hour)
		if _, err := tx.UpdateAgent(found.ID, func(agent *models.Agent) error {
			agent.DecayedAt = &lastNight
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
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

	var dream *models.AgentDream
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		dreams, err := tx.ListAgentDreams(found.ID, 5)
		if err != nil || len(dreams) == 0 {
			t.Fatalf("a dream happened: %v %s", dreams, err)
		}
		dream = dreams[0]

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
		if weights["people/alice-chen projects/portal"] <= weights["projects/portal things/latch"] {
			t.Fatalf("the link used today ends above the one that was not: %v", weights)
		}

		// The generative half: a link the dream worked out, with the
		// sentence that justifies it and below the weight of a stated one.
		joined, err := tx.ListAgentEdges(found.ID, alice.ID)
		if err != nil {
			t.Fatalf("ListAgentEdges: %s", err)
		}
		var noticed *models.AgentEdge
		for _, edge := range joined {
			if edge.ToPath == "things/latch" || edge.FromPath == "things/latch" {
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
		if noticed.Status != models.EdgeProposed {
			t.Fatalf("stored as a guess and not as something stated, not %q", noticed.Status)
		}
		if len(noticed.Evidence) == 0 {
			t.Fatalf("and the walk that suggested it, so somebody can disagree")
		}
		if noticed.Evidence[0].Kind != models.EvidenceDream {
			t.Fatalf("the walk under its own kind, not %q: nothing read a document here",
				noticed.Evidence[0].Kind)
		}

		// The links the test itself stated are untouched by any of that.
		for _, edge := range edges {
			if edge.Status != models.EdgeStated {
				t.Fatalf("a link somebody stated stays stated: %v", edge)
			}
		}
	})

	if dream.Strengthened == 0 {
		t.Fatalf("the dream says how many links it reweighted")
	}
	if dream.Associated != 1 {
		t.Fatalf("and that it found one connection, not %d", dream.Associated)
	}
	// Rehearsal: two questions asked, and the one about a boiler nothing
	// in the graph mentions is reported as a gap rather than answered.
	if dream.Rehearsed != 2 {
		t.Fatalf("two questions rehearsed, not %d", dream.Rehearsed)
	}
	if dream.Gaps != 1 {
		t.Fatalf("one of them unanswerable, not %d", dream.Gaps)
	}
	if dream.Unknown != 0 {
		t.Fatalf("and both were really tried, not %d unknown", dream.Unknown)
	}
	gap := ""
	for _, proposal := range dream.Proposals {
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
	_ = latch
}

// A newer build goes back over what an older one wrote, rewords what it
// can, and leaves the rest where it is.
//
// This is what the version on a row is for: a line an early build worded
// badly is still on the page years later, and nothing can find it except
// by knowing which build wrote it. Going over a row is not a reason to
// take it off the page. A pass here once struck whatever a list of words
// called empty, and took real lines with it without telling anybody.
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
		if len(facts) != 3 {
			lines := make([]string, 0, len(facts))
			for _, fact := range facts {
				lines = append(lines, fact.Text)
			}
			t.Fatalf("the night reads the page and states all three of them, not:\n%s",
				strings.Join(lines, "\n"))
		}
		for _, fact := range facts {
			// Marked as this build's, so tomorrow night looks elsewhere.
			if fact.Version == "0.0.1-old" || fact.Version == "" {
				t.Fatalf("what was looked at carries the build that looked, not %q", fact.Version)
			}
		}
		// And the night took nothing off the page while nobody was
		// watching: a thin line is still a line the person's agent was
		// told, and striking one silently is how a person loses something
		// they never knew had gone.
		revisions, err := tx.ListAgentRevisions(found.ID, page.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		for _, revision := range revisions {
			if revision.Actor != models.ActorDream {
				continue
			}
			if revision.Kind == models.RevisionFactGone || revision.Kind == models.RevisionFactStruck {
				t.Fatalf("the night left the page as it found it, and did not leave a %q", revision.Kind)
			}
		}
	})
}
