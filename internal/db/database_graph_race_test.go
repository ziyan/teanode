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
					AgentID: agentId, Path: "work/northwind/dev/portal",
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
		for _, path := range []string{"work", "work/northwind", "work/northwind/dev", "work/northwind/dev/portal"} {
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

// Inserting a child keeps a foreign-key KEY SHARE lock on its parent until
// commit. A content edit of that parent must be able to finish while the
// child transaction is open. FOR UPDATE used to wait on the KEY SHARE,
// completing a lock cycle when several writers built the same subtree.
func TestGraphParentEditDoesNotWaitForChildForeignKeyLock(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "work/northwind", Kind: models.NodeProject, Name: "Northwind",
		}); err != nil {
			t.Fatal(err)
		}
	})

	childInserted := make(chan struct{})
	releaseChild := make(chan struct{})
	defer releaseGraphWriter(releaseChild)
	childDone := make(chan error, 1)
	go func() {
		childDone <- database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: agentId, Path: "work/northwind/dev", Kind: models.NodeFolder, Name: "Dev",
			}); err != nil {
				return err
			}
			close(childInserted)
			<-releaseChild
			return nil
		})
	}()
	select {
	case <-childInserted:
	case err := <-childDone:
		t.Fatalf("child insert ended early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("child insert did not finish")
	}

	parentDone := make(chan error, 1)
	go func() {
		parentDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: agentId, Path: "work/northwind", Kind: models.NodeProject,
				Name: "Northwind", Summary: "Updated while a child is being filed.",
			})
			return err
		})
	}()
	var parentErr error
	select {
	case parentErr = <-parentDone:
	case <-time.After(2 * time.Second):
		releaseGraphWriter(releaseChild)
		_ = awaitGraphWriter(t, childDone)
		_ = awaitGraphWriter(t, parentDone)
		t.Fatal("page content edit waited for an unrelated child's foreign-key lock")
	}
	releaseGraphWriter(releaseChild)
	if err := awaitGraphWriter(t, childDone); err != nil {
		t.Fatal(err)
	}
	if parentErr != nil {
		t.Fatal(parentErr)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		parent, err := tx.GetAgentNode(agentId, "work/northwind")
		if err != nil || parent == nil || parent.Summary != "Updated while a child is being filed." {
			t.Fatalf("parent after concurrent writes: %v %v", parent, err)
		}
		child, err := tx.GetAgentNode(agentId, "work/northwind/dev")
		if err != nil || child == nil || child.ParentID != parent.ID {
			t.Fatalf("child after concurrent writes: %v %v", child, err)
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

// Filing a fact on a page and editing another fact on it, at once, both
// commit.
//
// This is the ordinary shape of a night: one batch files a new line on a
// page (AddAgentFact takes the page row, for its fact counter) and then
// gives its evidence to the line already there (UpdateAgentFact takes that
// fact), while another batch or the person edits that same line. Each
// writer must take the page and the fact in the same order, or the two
// wait on each other until the database gives up on one.
func TestGraphFilingAndEditingOnePageDoNotDeadlock(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId string
	var page *models.AgentNode
	var standing *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		var err error
		if page, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "things/marigold", Kind: models.NodeThing, Name: "Marigold"}); err != nil {
			t.Fatal(err)
		}
		if standing, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: page.ID, Kind: models.FactPlain, Text: "Marigold is kept at the pier."}); err != nil {
			t.Fatal(err)
		}
	})

	filed := make(chan struct{})
	carryOn := make(chan struct{})
	defer releaseGraphWriter(carryOn)
	filerDone := make(chan error, 1)
	go func() {
		filerDone <- database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: page.ID, Kind: models.FactPlain, Text: "Marigold is kept at the pier."}); err != nil {
				return err
			}
			close(filed)
			<-carryOn
			_, err := tx.UpdateAgentFact(agentId, standing.ID, func(fact *models.AgentFact) error {
				fact.Confidence = 1
				return nil
			})
			return err
		})
	}()
	select {
	case <-filed:
	case err := <-filerDone:
		t.Fatalf("the filing ended early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the filing did not start")
	}
	editorDone := make(chan error, 1)
	go func() {
		editorDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.UpdateAgentFact(agentId, standing.ID, func(fact *models.AgentFact) error {
				fact.Text = "Marigold is moored at the pier."
				return nil
			})
			return err
		})
	}()
	// Let the edit reach whatever it waits on before the filing goes on.
	time.Sleep(300 * time.Millisecond)
	releaseGraphWriter(carryOn)
	if err := awaitGraphWriter(t, filerDone); err != nil {
		t.Fatalf("the filing failed: %v", err)
	}
	if err := awaitGraphWriter(t, editorDone); err != nil {
		t.Fatalf("the edit failed: %v", err)
	}
}
