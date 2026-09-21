package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A link fades by how long it has been, not by how often the night ran.
//
// The quiet half used to keep four fifths of every weight each time it
// ran, which was written for one pass a night. Bootstrapping runs a night
// every few minutes: a day of catching up took an untouched link from 1
// to the floor, and a graph whose weights all read 0.05 cannot say what
// matters. So two passes a minute apart leave a weight where it was, and
// a pass a month later halves it.
//
// Run against the stages rather than through the worker, because the
// thing under test is an interval of thirty days and no test can sit
// through one.
func TestDecayIsByElapsedTime(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	configuration := config.Default()
	worker := New(&Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test",
		Tick:          time.Hour,
	})

	ctx := context.Background()
	var owner *models.User
	var person *models.Agent
	var edgeFrom, edgeTo string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		alice, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		portal, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "projects/portal", Kind: models.NodeProject, Name: "Portal"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		edgeFrom, edgeTo = alice.ID, portal.ID
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: person.ID, FromID: alice.ID, ToID: portal.ID,
			Relation: models.EdgeWorksOn, Weight: 1}); err != nil {
			t.Fatalf("PutAgentEdge: %s", err)
		}
	})

	// Nothing has been wanted, so nothing rises and only the fade shows.
	weightNow := func() float32 {
		t.Helper()
		var weight float32
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			edges, err := tx.ListAgentEdges(person.ID, edgeFrom)
			if err != nil {
				t.Fatalf("ListAgentEdges: %s", err)
			}
			for _, edge := range edges {
				if edge.ToID == edgeTo {
					weight = edge.Weight
				}
			}
		})
		return weight
	}

	pass := func(at time.Time) {
		t.Helper()
		run := &Run{Agent: person, Owner: owner, Now: at, settings: worker.settings}
		worker.dreamQuietHalf(ctx, run, &models.AgentDream{AgentID: person.ID}, at)
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			found, err := tx.GetAgent(person.ID)
			if err != nil || found == nil {
				t.Fatalf("GetAgent: %v %s", found, err)
			}
			person = found
		})
	}

	start := time.Now()
	pass(start)
	if weight := weightNow(); weight != 1 {
		t.Fatalf("the first pass has no interval to account for and fades nothing, not %v", weight)
	}
	if person.DecayedAt == nil {
		t.Fatal("and it writes down when it ran, so the next pass knows the interval")
	}

	pass(start.Add(time.Minute))
	if weight := weightNow(); weight < 0.999 {
		t.Fatalf("a minute later a link is where it was, not %v", weight)
	}

	pass(start.Add(time.Minute).Add(30 * 24 * time.Hour))
	if weight := weightNow(); weight < 0.49 || weight > 0.51 {
		t.Fatalf("a month later it is worth half, not %v", weight)
	}
}

// The factor itself: one at no elapsed time, a half at the half-life, and
// floored so a server that was off for a year fades a link once rather
// than to nothing.
func TestTheDecayFactorIsClampedAtBothEnds(t *testing.T) {
	if factor := edgeDecayFactor(0); factor != 1 {
		t.Fatalf("no time passed, nothing lost: %v", factor)
	}
	if factor := edgeDecayFactor(-time.Hour); factor != 1 {
		t.Fatalf("a clock that went backwards takes nothing: %v", factor)
	}
	if factor := edgeDecayFactor(edgeHalfLife); factor < 0.499 || factor > 0.501 {
		t.Fatalf("half a life is half: %v", factor)
	}
	if factor := edgeDecayFactor(10 * 365 * 24 * time.Hour); factor != edgeDecayFloor {
		t.Fatalf("ten years is the floor, not %v", factor)
	}
}
