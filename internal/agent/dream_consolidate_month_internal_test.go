package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A month's page and a folder have openings and no facts of their own,
// and that is how they are meant to be: the rewrite leaves them alone. A
// page of any other kind with an opening and no facts left is still due,
// to have the opening cleared.
func TestAMonthAndAFolderKeepTheirOpenings(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	_, run := digestSplitWorld(t, database, "http://127.0.0.1:1")

	var month, folder, orphan *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if month, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: "time/2019/04", Kind: models.NodePeriod, Name: "April 2019",
			Summary: "# April 2019\n\nThe regatta, and the new mooring.",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if folder, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: "things/boats", Kind: models.NodeFolder, Name: "Boats",
			Summary: "The boats kept at the pier.",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if orphan, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: run.Agent.ID, Path: "things/dinghy", Kind: models.NodeThing, Name: "Dinghy",
			Summary: "A dinghy, from facts since struck.",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
	})
	due := map[string]bool{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		listed, err := tx.ListAgentNodesToConsolidate(run.Agent.ID, 100)
		if err != nil {
			t.Fatalf("ListAgentNodesToConsolidate: %s", err)
		}
		for _, node := range listed {
			due[node.ID] = true
		}
	})
	if due[month.ID] {
		t.Error("a month's page is not written from facts, and its opening was put up to be cleared")
	}
	if due[folder.ID] {
		t.Error("a folder is not written from facts, and its opening was put up to be cleared")
	}
	if !due[orphan.ID] {
		t.Error("a page whose facts are gone should still have its opening cleared")
	}
}
