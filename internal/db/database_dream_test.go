package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// What was used together gets stronger; what nothing has touched gets
// weaker. This is the whole of the quiet half of a night, and it is
// arithmetic so that it can run over the entire graph every time.
func TestDreamUseStrengthensALinkAndDisuseWeakensIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		today := time.Now()

		// Two pages wanted today, and a third nobody has opened.
		alice := putNode(t, tx, agent.ID, "people/alice-chen", models.NodePerson, "Alice Chen")
		portal := putNode(t, tx, agent.ID, "projects/portal", models.NodeProject, "Portal")
		attic := putNode(t, tx, agent.ID, "projects/attic", models.NodeProject, "Attic")
		touch(t, tx, alice.ID, today)
		touch(t, tx, portal.ID, today)
		touch(t, tx, attic.ID, today.AddDate(0, -6, 0))

		for _, pair := range [][2]string{{alice.ID, portal.ID}, {alice.ID, attic.ID}} {
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: agent.ID, FromID: pair[0], ToID: pair[1],
				Relation: models.EdgeWorksOn, Weight: 1,
			}); err != nil {
				t.Fatalf("PutAgentEdge: %s", err)
			}
		}

		changed, err := tx.StrengthenAgentEdges(agent.ID, today.Add(-6*time.Hour), 0.25, 0.8)
		if err != nil {
			t.Fatalf("StrengthenAgentEdges: %s", err)
		}
		if changed != 1 {
			t.Fatalf("only the link whose both ends were wanted rises, not %d", changed)
		}

		edges, err := tx.ListAgentEdges(agent.ID, alice.ID)
		if err != nil {
			t.Fatalf("ListAgentEdges: %s", err)
		}
		weights := map[string]float32{}
		for _, edge := range edges {
			weights[edge.ToPath] = edge.Weight
		}
		// Down first then up: a link used today ends the night above
		// where it started, which is the point of doing it in that order.
		if weights["projects/portal"] <= 1 {
			t.Fatalf("a link used today ends above 1, not %v", weights["projects/portal"])
		}
		if weights["projects/attic"] >= 1 {
			t.Fatalf("a link nothing touched ends below 1, not %v", weights["projects/attic"])
		}
	})
}

// The bar to stay in the index comes from the graph rather than from a
// constant, and there is no bar at all until the graph is past its size.
func TestDreamTheBarRisesOnlyWhenTheGraphIsLarge(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		for index := 0; index < 12; index++ {
			putNode(t, tx, agent.ID, "projects/p"+itoa(index), models.NodeProject, "P"+itoa(index))
		}
		threshold, err := tx.AgentImportanceThreshold(agent.ID, 400)
		if err != nil {
			t.Fatalf("AgentImportanceThreshold: %s", err)
		}
		if threshold != 0 {
			t.Fatalf("a small graph has no bar to clear, got %v", threshold)
		}
		// Past its size, the bar is real, and nothing that has not had a
		// chance to be wanted falls under it.
		threshold, err = tx.AgentImportanceThreshold(agent.ID, 4)
		if err != nil {
			t.Fatalf("AgentImportanceThreshold: %s", err)
		}
		if threshold <= 0 {
			t.Fatalf("past its size the bar is above zero, got %v", threshold)
		}
		retired, err := tx.RetireAgentNodes(agent.ID, threshold, time.Now().Add(-45*24*time.Hour))
		if err != nil {
			t.Fatalf("RetireAgentNodes: %s", err)
		}
		if retired != 0 {
			t.Fatalf("a page written today has not had its chance yet, and %d were retired", retired)
		}
	})
}

// A walk crosses pages that are not joined to each other, which is what
// gives the generative half of a night something to ask about.
func TestDreamAWalkReachesWhatIsNotAdjacent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		alice := putNode(t, tx, agent.ID, "people/alice-chen", models.NodePerson, "Alice Chen")
		portal := putNode(t, tx, agent.ID, "projects/portal", models.NodeProject, "Portal")
		latch := putNode(t, tx, agent.ID, "things/latch", models.NodeThing, "Latch")
		for _, pair := range [][2]string{{alice.ID, portal.ID}, {portal.ID, latch.ID}} {
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: agent.ID, FromID: pair[0], ToID: pair[1],
				Relation: models.EdgeRelatedTo, Weight: 2,
			}); err != nil {
				t.Fatalf("PutAgentEdge: %s", err)
			}
		}
		touch(t, tx, alice.ID, time.Now())

		starts, err := tx.ListAgentNodesForWalking(agent.ID, 5)
		if err != nil {
			t.Fatalf("ListAgentNodesForWalking: %s", err)
		}
		if len(starts) == 0 {
			t.Fatalf("a page with a link is somewhere to start from")
		}
		path, err := tx.WalkAgentGraph(agent.ID, alice.ID, 5)
		if err != nil {
			t.Fatalf("WalkAgentGraph: %s", err)
		}
		if len(path) != 2 {
			t.Fatalf("two steps and then nowhere left to go: %v", path)
		}
		if path[len(path)-1].Path != "things/latch" {
			t.Fatalf("the walk ends somewhere not joined to where it began, not %q", path[len(path)-1].Path)
		}
		// And it never comes back to where it started, or the pair put to
		// the model would be a page and itself.
		for _, node := range path {
			if node.ID == alice.ID {
				t.Fatalf("a walk does not return to its own beginning")
			}
		}
	})
}

// --- helpers ----------------------------------------------------------

func putNode(t *testing.T, tx db.Transaction, agentId, path string, kind models.AgentNodeKind, name string) *models.AgentNode {
	t.Helper()
	node, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: path, Kind: kind, Name: name})
	if err != nil {
		t.Fatalf("PutAgentNode %q: %s", path, err)
	}
	return node
}

// touch is a page having been wanted at a moment, which is what the quiet
// half reads.
func touch(t *testing.T, tx db.Transaction, nodeId string, when time.Time) {
	t.Helper()
	if err := tx.TouchAgentNodes([]string{nodeId}, when); err != nil {
		t.Fatalf("TouchAgentNodes: %s", err)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// A page written last night is worth something before anybody has used
// it.
//
// Without this the three other terms make a trap: no use means no
// importance, no importance means it is not in the index, not in the
// index means nothing can use it, and so it never will be. The grace is
// a fortnight and it is small enough that it cannot hold a page in the
// index by itself.
func TestDreamANewPageIsWorthSomethingBeforeItIsUsed(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		fresh := putNode(t, tx, agent.ID, "topics/latch-calibration", models.NodeTopic, "Latch calibration")

		if _, err := tx.RecomputeAgentImportance(agent.ID, time.Now()); err != nil {
			t.Fatalf("RecomputeAgentImportance: %s", err)
		}
		found, err := tx.GetAgentNodeByID(agent.ID, fresh.ID)
		if err != nil || found == nil {
			t.Fatalf("GetAgentNodeByID: %v %s", found, err)
		}
		if found.Importance <= 0.06 {
			t.Fatalf("a page made today is worth more than its kind alone: %v", found.Importance)
		}

		// A month on, with still nothing having wanted it, the grace is
		// gone and it is down to what it has actually earned.
		if _, err := tx.RecomputeAgentImportance(agent.ID, time.Now().AddDate(0, 1, 0)); err != nil {
			t.Fatalf("RecomputeAgentImportance later: %s", err)
		}
		later, err := tx.GetAgentNodeByID(agent.ID, fresh.ID)
		if err != nil || later == nil {
			t.Fatalf("GetAgentNodeByID: %v %s", later, err)
		}
		if later.Importance >= found.Importance {
			t.Fatalf("the grace runs out: %v then %v", found.Importance, later.Importance)
		}
	})
}
