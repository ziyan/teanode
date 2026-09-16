package db_test

import (
	"sync"
	"testing"

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
