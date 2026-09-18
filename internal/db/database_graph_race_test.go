package db_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Two transactions writing the same page at once settle on one page
// rather than one of them failing.
//
// Found in production within a minute of the first deployment: a turn
// building its prompt makes the roots while the run that files what a
// conversation taught is making them too, both find nothing there, and
// both insert. Whoever was second got a unique-constraint error and the
// job retried for a minute. The fix is an upsert, and this is the test
// that would have caught it.
func TestGraphTwoWritersOfOnePageSettle(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
	})

	const writers = 6
	var waiting sync.WaitGroup
	failures := make(chan error, writers)
	start := make(chan struct{})

	for index := 0; index < writers; index++ {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			<-start
			if err := database.Transaction(func(tx db.Transaction) error {
				if err := tx.EnsureAgentRoots(agentId); err != nil {
					return err
				}
				_, err := tx.PutAgentNode(&models.AgentNode{
					AgentID: agentId, Path: "work/mujin/dev/portal",
					Kind: models.NodeProject, Name: "Portal",
				})
				return err
			}); err != nil {
				failures <- err
			}
		}()
	}
	close(start)
	waiting.Wait()
	close(failures)

	for err := range failures {
		t.Fatalf("writing the same page twice at once should settle, and one writer failed: %s", err)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// One page, whoever won, with the folders above it made once each.
		for _, path := range []string{"work", "work/mujin", "work/mujin/dev", "work/mujin/dev/portal"} {
			node, err := tx.GetAgentNode(agentId, path)
			if err != nil || node == nil {
				t.Fatalf("%q exists once: %v %s", path, node, err)
			}
		}
		nodes, err := tx.ListAgentNodesUnder(agentId, "work", 100)
		if err != nil {
			t.Fatalf("ListAgentNodesUnder: %s", err)
		}
		if len(nodes) != 4 {
			t.Fatalf("four pages, not %d: %v", len(nodes), nodes)
		}
	})
}

// A page saved while a fact is being filed on it keeps the fact counter
// where the fact left it.
//
// The counter is a high-water mark, never a count: a number is cited --
// "people/alice-chen#2" -- so handing a number out twice silently
// rewrites what somebody else's sentence pointed at, and the unique index
// on (node_id, number) turns it into a failed write instead. PutAgentNode
// used to read the counter and write it back through Save, which is a
// lost update whenever an AddAgentFact commits between the two: the page
// save puts the mark back to where it was and the next fact collides.
//
// The two transactions are interleaved rather than raced: the fact is
// filed and left uncommitted so that the page save reads the old mark,
// then blocks on the row until the fact commits, which is exactly the
// order that loses the increment.
func TestGraphSavingAPageKeepsTheFactCounter(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId, nodeId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		nodeId = node.ID
	})

	filed := make(chan struct{})
	filing := make(chan error, 1)
	go func() {
		filing <- database.Transaction(func(tx db.Transaction) error {
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agentId, NodeID: nodeId, Kind: models.FactPlain,
				Text: "She does the books.",
			})
			if err != nil {
				return err
			}
			if fact.Number != 1 {
				t.Errorf("the first fact is #1, not #%d", fact.Number)
			}
			// The mark has moved and the row is locked; let the page save
			// start, and hold the lock long enough for it to reach its
			// own write and wait there.
			close(filed)
			time.Sleep(500 * time.Millisecond)
			return nil
		})
	}()

	<-filed
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "people/alice-chen", Kind: models.NodePerson,
			Name: "Alice Chen", Summary: "Does the books.",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
	})
	if err := <-filing; err != nil {
		t.Fatalf("filing a fact: %s", err)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		next, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: nodeId, Kind: models.FactPlain,
			Text: "She is in Berlin.",
		})
		if err != nil {
			t.Fatalf("the page save reset the counter, so the next fact took a number already taken: %s", err)
		}
		if next.Number != 2 {
			t.Errorf("the next fact is #2, not #%d", next.Number)
		}
	})
}
