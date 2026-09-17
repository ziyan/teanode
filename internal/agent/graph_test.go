package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// recallWorld is an agent with a graph and a turn to write an overlay
// into. No model and no embedder: writeRecalled is given what a search
// found, so there is nothing here for either to do.
type recallWorld struct {
	run      *AskRun
	database db.Database
	agent    *models.Agent
}

func newRecallWorld(t *testing.T) *recallWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)

	configuration := config.Default()
	worker := New(&Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	world := &recallWorld{database: database}
	var owner *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(world.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
	})
	world.run = &AskRun{
		agent:          worker,
		settings:       &AskSettings{Agent: world.agent, Owner: owner},
		promptMemories: map[string]bool{},
	}
	return world
}

// page makes a page with a summary and some facts on it.
func (self *recallWorld) page(t *testing.T, path, name, summary string, facts ...string) *models.AgentNode {
	t.Helper()
	var node *models.AgentNode
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		node, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: self.agent.ID, Path: path, Kind: models.NodeProject, Name: name, Summary: summary,
		})
		if err != nil {
			t.Fatalf("PutAgentNode(%q): %s", path, err)
		}
		for _, text := range facts {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: self.agent.ID, NodeID: node.ID, Kind: models.FactPlain, Text: text,
				Audiences: []models.AgentAudience{models.AudienceAsk},
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	return node
}

// wanted is how many of a page's facts have a use recorded against them.
func (self *recallWorld) wanted(t *testing.T, node *models.AgentNode) int {
	t.Helper()
	count := 0
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(self.agent.ID, node.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		for _, fact := range facts {
			if fact.UsedAt != nil {
				count++
			}
		}
	})
	return count
}

// overlay is the whole of what the turn would carry, as one string.
func (self *recallWorld) overlay() string {
	return strings.Join(self.run.Recalled(), "\n")
}

// A page whose block does not fit is left alone entirely: its facts are
// not marked as used and the next page down is still tried.
//
// Both halves were wrong. The facts were marked as the block was built
// and the budget was checked afterwards, so a page that never reached the
// prompt still had `used_at` moved on every fact in it -- which feeds
// importance, decay, and what the index carries tomorrow. And a block
// that did not fit ended the loop, so one long page hid every shorter one
// behind it.
func TestAPageOverTheBudgetIsPassedOver(t *testing.T) {
	world := newRecallWorld(t)

	// Five facts of nine hundred characters and an opening of six
	// hundred: more than the whole recall budget on its own.
	long := strings.Repeat("what this project decided and why, at length. ", 20)
	big := world.page(t, "projects/big-one", "Big One", strings.Repeat("an opening at length. ", 40),
		long, long, long, long, long)
	small := world.page(t, "projects/small-one", "Small One", "A short one.", "Ships on Fridays.")

	world.run.writeRecalled(context.Background(), []*models.AgentNode{big, small}, nil)

	carried := world.overlay()
	if strings.Contains(carried, "projects/big-one") {
		t.Fatalf("the page over the budget is not carried:\n%s", carried)
	}
	if !strings.Contains(carried, "projects/small-one") || !strings.Contains(carried, "Ships on Fridays.") {
		t.Fatalf("and the smaller page behind it still is:\n%s", carried)
	}
	if count := world.wanted(t, big); count != 0 {
		t.Fatalf("nothing of a page that was not carried is marked as used, and %d was", count)
	}
	if count := world.wanted(t, small); count != 1 {
		t.Fatalf("what was carried is marked as used, and %d was", count)
	}
}

// A page the prompt's own index already names still has its facts
// expanded when the turn's words hit it.
//
// The index line says what a page is about. It is not what the page
// knows, and skipping the page for being in the index meant that asking
// about the one project the agent thinks most important was answered from
// a single line of description.
func TestAnIndexedPageStillGetsItsFacts(t *testing.T) {
	world := newRecallWorld(t)

	node := world.page(t, "projects/portal", "Portal", "The customer-facing portal.",
		"Runs on the Frankfurt cluster.")
	// As carryIndex leaves it: the prompt already carries this page's line.
	world.run.promptMemories[node.ID] = true

	world.run.writeRecalled(context.Background(), []*models.AgentNode{node}, nil)

	carried := world.overlay()
	if !strings.Contains(carried, "Runs on the Frankfurt cluster.") {
		t.Fatalf("a page in the index still gives up its facts:\n%s", carried)
	}
	// The opening is the one part the index line already has the gist of.
	if strings.Contains(carried, "The customer-facing portal.") {
		t.Fatalf("and not its opening, which the index line carries:\n%s", carried)
	}
	if count := world.wanted(t, node); count != 1 {
		t.Fatalf("the fact that was carried is marked as used, and %d was", count)
	}
}
