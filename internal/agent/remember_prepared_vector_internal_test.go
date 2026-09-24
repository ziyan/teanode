package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The fact meaning is prepared before its write transaction. A page rename
// during that interval changes the text the vector purports to represent.
// The old path persisted it and used it to find a twin on the renamed page.
func TestPreparedFactMeaningFollowsCurrentPageName(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		newName    string
		editFact   bool
		wantFolded bool
	}{
		{name: "renamed page", newName: "After", wantFolded: false},
		{name: "unrelated metadata", newName: "Before", wantFolded: true},
		{name: "fact changed after preparation", newName: "Before", editFact: true, wantFolded: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(t)
			defer closeDatabase()

			const model = "prepared-fact-meaning"
			const words = "The boiler needs an annual inspection."
			vector := []float32{1, 0, 0}
			var agentId string
			var preparedNode, existing *models.AgentNode
			var standing *models.AgentFact
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				owner, err := tx.CreateUser(&models.User{Username: "prepared-fact", Name: "Example Person"})
				if err != nil {
					t.Fatal(err)
				}
				agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				agentId = agent.ID
				if err := tx.EnsureAgentRoots(agentId); err != nil {
					t.Fatal(err)
				}
				preparedNode, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "things/boiler", Kind: models.NodeThing, Name: "Before"})
				if err != nil {
					t.Fatal(err)
				}
				standing, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: preparedNode.ID, Kind: models.FactPlain, Text: words})
				if err != nil {
					t.Fatal(err)
				}
			})

			// The prepared vector still describes Before. Change the page,
			// then supply an exact neighbor to prove a stale lookup would fold.
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				changed := *preparedNode
				changed.Name = testCase.newName
				changed.Pinned = true
				var err error
				existing, err = tx.PutAgentNode(&changed)
				if err != nil {
					t.Fatal(err)
				}
				if err := tx.PutAgentFactVector(agentId, standing.ID, model, vector); err != nil {
					t.Fatal(err)
				}
			})

			var written *models.AgentFact
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				var err error
				written, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: preparedNode.ID, Kind: models.FactPlain, Text: words})
				if err != nil {
					t.Fatal(err)
				}
				if testCase.editFact {
					if _, err := tx.UpdateAgentFact(agentId, written.ID, func(fact *models.AgentFact) error {
						fact.Text = "The boiler was replaced instead."
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				became, err := (&Agent{}).foldIntoWhatThePageSays(tx, written, preparedNode, &meaning{ModelName: model, Vector: vector})
				if err != nil {
					t.Fatal(err)
				}
				if (became.ID == standing.ID) != testCase.wantFolded {
					t.Errorf("fold result %q into %q; want folded=%t", became.ID, standing.ID, testCase.wantFolded)
				}
			})

			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				page, err := tx.GetAgentNodeByID(agentId, preparedNode.ID)
				if err != nil || page == nil || page.Name != existing.Name {
					t.Fatalf("current page: %v %v", page, err)
				}
				scores, err := tx.Nearest(db.AgentFactTable, agentId, model, vector, 1, db.VectorQuery{
					Where: []string{`"fact_id" = ?`}, Arguments: []any{written.ID}, Floor: 0.9,
				})
				if err != nil {
					t.Fatal(err)
				}
				vectorPresent := len(scores) == 1 && scores[0].ID == written.ID
				if vectorPresent != testCase.wantFolded {
					t.Errorf("prepared fact vector present=%t; want present=%t", vectorPresent, testCase.wantFolded)
				}
				if testCase.editFact {
					stored, err := tx.GetAgentFacts(agentId, []string{written.ID})
					if err != nil || len(stored) != 1 {
						t.Fatalf("edited fact: %v %v", stored, err)
					}
					if stored[0].Dormant || stored[0].Text != "The boiler was replaced instead." {
						t.Errorf("stale fold retired a changed fact: %+v", stored[0])
					}
				}
			})
		})
	}
}
