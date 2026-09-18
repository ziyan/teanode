package memory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// person is a person with an agent and nothing filed yet.
func person(t *testing.T, username string) (*fakeRun, db.Database, func()) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: username, Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		run.owner = owner
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now(),
		}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	return run, database, closeDatabase
}

// factOn is one fact of a page, by number.
func factOn(t *testing.T, database db.Database, agentId, path string, number int) (*models.AgentFact, int) {
	t.Helper()
	var fact *models.AgentFact
	var count int
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(agentId, path)
		if err != nil || node == nil {
			t.Fatalf("GetAgentNode %s: %v %s", path, node, err)
		}
		facts, err := tx.ListAgentFacts(agentId, node.ID, true, 100)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		count = len(facts)
		for _, candidate := range facts {
			if candidate.Number == number {
				fact = candidate
			}
		}
	})
	if fact == nil {
		t.Fatalf("there is no %s#%d", path, number)
	}
	return fact, count
}

// A correction rewrites the sentence where it stands rather than writing
// a second one.
//
// Everything about the fact that is not the sentence has to survive it:
// its number, because that is how it was cited; the day it was learned,
// because a correction is not a new thing learned today; the words it
// came from; and who reads it, since a fact addressed to triage that
// quietly stopped being addressed to triage would change how mail is
// sorted with nobody asking for it.
func TestNotingWithANumberCorrectsTheFactWhereItStands(t *testing.T) {
	run, database, closeDatabase := person(t, "alice")
	defer closeDatabase()

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	first := call(`{"action":"note","path":"projects/portal","kind":"project","name":"Portal","text":"The portal ships on Fridays.","applies_to":["triage"],"fact_kind":"decision","happened":"2026-06-14"}`)
	if !strings.Contains(first, "projects/portal#1") {
		t.Fatalf("a fact is answered with its reference: %q", first)
	}
	before, count := factOn(t, database, run.agent.ID, "projects/portal", 1)
	if count != 1 {
		t.Fatalf("one fact so far, got %d", count)
	}

	corrected := call(`{"action":"note","path":"projects/portal","number":1,"text":"The portal ships on Tuesdays."}`)
	if !strings.Contains(corrected, "projects/portal#1") || !strings.Contains(corrected, "Tuesdays") {
		t.Fatalf("the correction answers with the same reference, reworded: %q", corrected)
	}

	after, count := factOn(t, database, run.agent.ID, "projects/portal", 1)
	if count != 1 {
		t.Fatalf("correcting a fact does not add one: %d facts on the page", count)
	}
	if after.Text != "The portal ships on Tuesdays." {
		t.Fatalf("it says the new thing: %q", after.Text)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("it was learned when it was learned: %s, was %s", after.CreatedAt, before.CreatedAt)
	}
	// A correction to the words alone: what sort of statement it is and
	// when it was true are not the call's to reset by leaving them out.
	if after.Kind != models.FactDecision || after.HappenedAt == nil || !after.HappenedAt.Equal(*before.HappenedAt) {
		t.Fatalf("a correction that said nothing about the kind or the date leaves them: %s %v, were %s %v", after.Kind, after.HappenedAt, before.Kind, before.HappenedAt)
	}
	if len(after.Audiences) != len(before.Audiences) {
		t.Fatalf("a correction that said nothing about audiences leaves them: %v, were %v", after.Audiences, before.Audiences)
	}
	for index, audience := range before.Audiences {
		if after.Audiences[index] != audience {
			t.Fatalf("a correction that said nothing about audiences leaves them: %v, were %v", after.Audiences, before.Audiences)
		}
	}
	// The words it came from stay, with the correction beside them.
	if len(after.Evidence) <= len(before.Evidence) {
		t.Fatalf("the evidence is kept and added to: %v, was %v", after.Evidence, before.Evidence)
	}
	if after.Evidence[0].Quote != before.Evidence[0].Quote {
		t.Fatalf("what it was first learned from is still first: %q, was %q", after.Evidence[0].Quote, before.Evidence[0].Quote)
	}

	// And a number for a fact that is not there is a typo, not an
	// invitation to write a new one.
	_, err := tool.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"note","path":"projects/portal","number":9,"text":"Something else."}`)})
	if err == nil || !strings.Contains(err.Error(), "projects/portal#9") {
		t.Fatalf("a number nothing is filed under is refused: %v", err)
	}
	if _, count := factOn(t, database, run.agent.ID, "projects/portal", 1); count != 1 {
		t.Fatalf("and writes nothing: %d facts on the page", count)
	}
}

// A page's history says what happened to it and who did it, which is the
// only place the graph says where a sentence came from.
func TestHistorySaysWhatHappenedToAPage(t *testing.T) {
	run, _, closeDatabase := person(t, "bob")
	defer closeDatabase()

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	call(`{"action":"note","path":"projects/portal","kind":"project","name":"Portal","text":"The portal ships on Fridays."}`)
	call(`{"action":"note","path":"projects/portal","number":1,"text":"The portal ships on Tuesdays."}`)

	history := call(`{"action":"history","path":"projects/portal"}`)
	if !strings.Contains(history, "added a fact") {
		t.Fatalf("the history has the fact being written: %q", history)
	}
	if !strings.Contains(history, "changed a fact") {
		t.Fatalf("and the correction: %q", history)
	}
	// What it used to say, which is the half somebody wants at the moment
	// they go looking.
	if !strings.Contains(history, "was: The portal ships on Fridays.") {
		t.Fatalf("with the words it moved: %q", history)
	}
	if !strings.Contains(history, "now: The portal ships on Tuesdays.") {
		t.Fatalf("and the words it moved to: %q", history)
	}
	// Newest first, the way the API and the command line give it.
	if strings.Index(history, "changed a fact") > strings.Index(history, "added a fact") {
		t.Fatalf("newest first: %q", history)
	}

	// A path that is not a page is answered the way a read of one is:
	// what is near, so the next guess lands.
	if missed := call(`{"action":"history","path":"projects/portals"}`); !strings.Contains(missed, "no page at projects/portals") {
		t.Fatalf("a history of nothing says so: %q", missed)
	}
}
