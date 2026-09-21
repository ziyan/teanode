package db_test

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The fact a page already said is found however many facts the page holds.
//
// The search for duplicates looks at the whole page; the search for what
// they duplicate used to scan a listing capped at five hundred. On a page
// with more facts than that, a duplicate was found and then had nothing to
// be folded into: it was reported as merged, folded nowhere, and found
// again the next night. Six runs in a row folded the same sixty-eight with
// nothing read between them, which is how it was caught.
func TestTheEarlierTwinIsFoundPastTheOldScan(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var agentId, nodeId string
	var twin, copied *models.AgentFact
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		agentId = agent.ID
		if err := tx.EnsureAgentRoots(agentId); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		page, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: "topics/crowded", Kind: models.NodeTopic, Name: "Crowded",
		})
		if err != nil {
			test.Fatalf("PutAgentNode: %s", err)
		}
		nodeId = page.ID

		// Both of the pair sit past the five hundredth fact on the page,
		// which is where the old scan stopped. It listed by number
		// ascending, so a twin among the first five hundred was found and
		// folded perfectly well: the fault only shows when the pair itself
		// is further down than the scan reached.
		for index := 0; index < 600; index++ {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agentId, NodeID: nodeId,
				Text: fmt.Sprintf("Something else, number %d.", index),
			}); err != nil {
				test.Fatalf("AddAgentFact %d: %s", index, err)
			}
		}
		if twin, err = tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: nodeId, Text: "The roof was replaced in March.",
		}); err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
		for index := 600; index < 700; index++ {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agentId, NodeID: nodeId,
				Text: fmt.Sprintf("Something else, number %d.", index),
			}); err != nil {
				test.Fatalf("AddAgentFact %d: %s", index, err)
			}
		}
		// And the copy, further down still.
		if copied, err = tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: nodeId, Text: "The roof was replaced in March.",
		}); err != nil {
			test.Fatalf("AddAgentFact: %s", err)
		}
	})

	// The duplicate is found, as it always was.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		twice, err := tx.ListAgentFactsSaidTwice(agentId, 2000)
		if err != nil {
			test.Fatalf("ListAgentFactsSaidTwice: %s", err)
		}
		if len(twice) != 1 || twice[0].ID != copied.ID {
			test.Fatalf("what was said twice: %d rows", len(twice))
		}
	})

	// What the old scan would have seen: the page's first five hundred
	// facts by number, which is where it stopped. The twin is not among
	// them, so it found nothing to fold into -- and that was reported as a
	// fold. Asserted here so this test fails against the code it replaced
	// rather than passing against both.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		scanned, err := tx.ListAgentFacts(agentId, nodeId, false, 500)
		if err != nil {
			test.Fatalf("ListAgentFacts: %s", err)
		}
		for _, candidate := range scanned {
			if candidate.ID == twin.ID {
				test.Fatal("the twin was inside the old scan, so this does not reproduce the fault")
			}
		}
	})

	// And now so is what it duplicates, which is the whole fix.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		kept, err := tx.FirstAgentFactSayingIt(agentId, nodeId, copied.Text, copied.Number)
		if err != nil {
			test.Fatalf("FirstAgentFactSayingIt: %s", err)
		}
		if kept == nil {
			test.Fatal("the earlier twin was not found, so the copy could not be folded")
		}
		if kept.ID != twin.ID {
			test.Errorf("it found %q rather than the first saying it", kept.ID)
		}
	})

	// Folded, it is not said twice any more: the count drains instead of
	// standing at the same number every night.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.FoldAgentFact(agentId, copied.ID, twin.ID, "the page already said it"); err != nil {
			test.Fatalf("FoldAgentFact: %s", err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		twice, err := tx.ListAgentFactsSaidTwice(agentId, 2000)
		if err != nil {
			test.Fatalf("ListAgentFactsSaidTwice: %s", err)
		}
		if len(twice) != 0 {
			test.Errorf("after folding, %d are still said twice", len(twice))
		}
	})

	// And a fact with no earlier twin says so, rather than answering with
	// something that would be counted as a fold.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		kept, err := tx.FirstAgentFactSayingIt(agentId, nodeId, "Nothing else says this.", 9999)
		if err != nil {
			test.Fatalf("FirstAgentFactSayingIt: %s", err)
		}
		if kept != nil {
			test.Errorf("it found %q for words nothing else says", kept.ID)
		}
	})
}
