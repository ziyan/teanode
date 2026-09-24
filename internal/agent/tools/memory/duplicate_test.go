package memory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeRun knows what a fact means, as a run with an embedding model does:
// whatever it is told is a twin, is one.
type fakeRun struct {
	tools.Run
	database     db.Database
	store        storage.Storage
	owner        *models.User
	agent        *models.Agent
	conversation *models.AgentConversation
	twins        []*models.AgentFact
	noted        []string
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Storage() storage.Storage                { return self.store }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Owner() *models.User                     { return self.owner }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) CanAsk() bool                            { return !self.Headless() }
func (self *fakeRun) Configuration() *config.Configuration    { return config.Default() }
func (self *fakeRun) Offered() []*tools.Tool                  { return nil }
func (self *fakeRun) Recall(string)                           {}
func (self *fakeRun) Recalled() []string                      { return nil }

func (self *fakeRun) NoteFact(_ context.Context, fact *models.AgentFact) []*models.AgentFact {
	self.noted = append(self.noted, fact.ID)
	return self.twins
}

func (self *fakeRun) NoteNode(context.Context, *models.AgentNode) {}

// PreparePage uses the requested path without calling an embedding provider.
func (self *fakeRun) PreparePage(_ context.Context, path string, kind models.AgentNodeKind, name string) tools.PreparedPage {
	return func(tx db.Transaction) (*models.AgentNode, error) {
		existing, err := tx.GetAgentNode(self.agent.ID, path)
		if err != nil || existing != nil {
			return existing, err
		}
		if !models.IsAgentNodeKind(kind) {
			kind = models.NodeTopic
		}
		if name == "" {
			name = path
		}
		return tx.PutAgentNode(&models.AgentNode{AgentID: self.agent.ID, Path: path, Kind: kind, Name: name})
	}
}

func find(t *testing.T, name string) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

// A fact that says what a fact already on the page says is written all
// the same, and the answer says, with both references, to merge them.
//
// Written all the same on purpose: refusing was answered with the same
// call again, and a hedge ("this may be a duplicate") was read and
// ignored. An imperative naming both is obeyed.
func TestNotingSaysWhenThePageAlreadySaysIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		run.owner = owner
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
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

	// Nothing is near it yet, and the page is made on the way.
	first := call(`{"action":"note","path":"things/marigold","kind":"thing","name":"Marigold","text":"The neighbour's boat, repainted every spring."}`)
	if !strings.Contains(first, "things/marigold#1") {
		t.Fatalf("a fact is answered with its reference: %q", first)
	}
	if strings.Contains(first, "already says") {
		t.Fatalf("the first of its kind is a copy of nothing: %q", first)
	}
	if len(run.noted) != 1 {
		t.Fatalf("what it means is worked out as it is written: %v", run.noted)
	}

	// The page is real, and reading it back shows the fact.
	page := call(`{"action":"get","path":"things/marigold"}`)
	if !strings.Contains(page, "Marigold") || !strings.Contains(page, "#1") {
		t.Fatalf("the page reads back: %q", page)
	}

	// Now there is a twin.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(run.agent.ID, "things/marigold")
		if err != nil || node == nil {
			t.Fatalf("GetAgentNode: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(run.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("ListAgentFacts: %v %s", facts, err)
		}
		run.twins = facts
	})
	second := call(`{"action":"note","path":"things/marigold","text":"Marigold gets a coat of paint each spring."}`)
	if !strings.Contains(second, "things/marigold#1") || !strings.Contains(second, "already says") {
		t.Fatalf("it should name the one it repeats: %q", second)
	}
	if !strings.Contains(second, "forget things/marigold#2") {
		t.Fatalf("and say what to do about it, with the reference: %q", second)
	}
	if !strings.Contains(second, "in this turn") {
		t.Fatalf("now, rather than eventually: %q", second)
	}
}

// A path is an address a model can guess. A guess that misses is answered
// with what is near, so the next call lands rather than searching.
func TestAMissedGuessIsAnsweredWithWhatIsNear(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "bob", Name: "Bob Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		run.owner = owner
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: run.agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"get","path":"people/alice"}`)})
	if err != nil {
		t.Fatalf("get: %s", err)
	}
	if !strings.Contains(result.Content, "no page at people/alice") {
		t.Fatalf("it says the page is not there: %q", result.Content)
	}
	if !strings.Contains(result.Content, "people/alice-chen") {
		t.Fatalf("and what is near, so the next guess lands: %q", result.Content)
	}
}

// Reading changes nothing, and the tool says so per call: a run that may
// only read still gets index, get and search.
func TestReadingTheGraphIsNotAWrite(t *testing.T) {
	tool := find(t, "memory")
	if tool.RiskOf == nil {
		t.Fatalf("the memory tool judges the call, not the tool")
	}
	cases := map[string]tools.Risk{
		`{"action":"get","path":"self"}`:                   tools.RiskRead,
		`{"action":"index"}`:                               tools.RiskRead,
		`{"action":"search","query":"boat"}`:               tools.RiskRead,
		`{"action":"history","path":"self"}`:               tools.RiskRead,
		`{"action":"note","path":"self","text":"x"}`:       tools.RiskWrite,
		`{"action":"forget","path":"self","number":2}`:     tools.RiskWrite,
		`{"action":"forget","path":"people/alice-chen"}`:   tools.RiskDestructive,
		`{"action":"batch","items":[{"action":"forget"}]}`: tools.RiskDestructive,
		`{"action":"batch","items":[{"action":"get"}]}`:    tools.RiskRead,
		`{"action":"batch","items":[{"action":"note"}]}`:   tools.RiskWrite,
	}
	for arguments, want := range cases {
		if got := tool.RiskOf([]byte(arguments)); got != want {
			t.Fatalf("%s is %q, want %q", arguments, got, want)
		}
	}
}
