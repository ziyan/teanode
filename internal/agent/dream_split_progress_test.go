package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestDreamSplitCountsUniqueCommittedFacts(test *testing.T) {
	for _, fixture := range []struct {
		name           string
		groups         string
		movedCount     int
		shouldFail     bool
		shouldRunPhase bool
	}{
		{name: "duplicate minimum", groups: `[{"name":"Small","slug":"small","facts":[1,1,1]}]`},
		{name: "duplicate count", groups: `[{"name":"First","slug":"first","facts":[1,2,3,1]}]`, movedCount: 3},
		{name: "overlapping groups", groups: `[{"name":"First","slug":"first","facts":[1,2,3]},{"name":"Second","slug":"second","facts":[1,2,3,4,5,6]}]`, movedCount: 6},
		{name: "rollback", groups: `[{"name":"Rejected","slug":"rejected","facts":[1,2,3]}]`, shouldFail: true},
		{name: "partial completion", groups: `[{"name":"First","slug":"first","facts":[1,2,3]},{"name":"Rejected","slug":"rejected","facts":[4,5,6]}]`, movedCount: 3, shouldFail: true},
		{name: "phase retains committed count", groups: `[{"name":"First","slug":"first","facts":[1,2,3]},{"name":"Rejected","slug":"rejected","facts":[4,5,6]}]`, movedCount: 3, shouldFail: true, shouldRunPhase: true},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			database, release := dbtest.AcquireDatabase(test)
			defer release()
			answer, err := json.Marshal(`{"groups":` + fixture.groups + `}`)
			if err != nil {
				test.Fatal(err)
			}
			provider := scriptedProvider([]string{fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, answer)})
			defer provider.Close()
			worker, run := digestSplitWorld(test, database, provider.URL)
			var page *models.AgentNode
			factCount := 6
			if fixture.shouldRunPhase {
				factCount = splitAbove + 5
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				page, err = transaction.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "projects/fixture", Kind: models.NodeProject, Name: "Fixture"})
				if err != nil {
					test.Fatal(err)
				}
				for index := range factCount {
					if _, err := transaction.AddAgentFact(&models.AgentFact{AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: fmt.Sprintf("Fixture fact %d", index+1)}); err != nil {
						test.Fatal(err)
					}
				}
			})
			if fixture.shouldFail {
				dbtest.Exec(test, database, `CREATE FUNCTION refuse_split_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.path = 'projects/fixture/rejected' THEN RAISE EXCEPTION 'fixture split failure'; END IF; RETURN NEW; END $$`)
				dbtest.Exec(test, database, `CREATE TRIGGER refuse_split_completion BEFORE UPDATE OF consolidated_at ON agent_node FOR EACH ROW EXECUTE FUNCTION refuse_split_completion()`)
			}
			movedCount := 0
			if fixture.shouldRunPhase {
				record := &models.AgentDream{AgentID: run.Agent.ID}
				worker.dreamSplit(context.Background(), run, record, &dreamBudget{})
				movedCount = record.Moved
			} else {
				movedCount, err = worker.splitPage(context.Background(), run, &dreamBudget{}, page)
				if (err != nil) != fixture.shouldFail {
					test.Fatalf("split error = %v", err)
				}
			}
			if movedCount != fixture.movedCount {
				test.Errorf("reported %d moves, want %d committed facts", movedCount, fixture.movedCount)
			}
			storedCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_fact f JOIN agent_node n ON n.id = f.node_id WHERE n.path LIKE 'projects/fixture/%'`)
			if storedCount != fmt.Sprint(fixture.movedCount) {
				test.Errorf("stored %s moved facts, want %d", storedCount, fixture.movedCount)
			}
			if childCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_node WHERE path IN ('projects/fixture/rejected', 'projects/fixture/small')`); childCount != "0" {
				test.Errorf("invalid or rolled-back child pages persisted: %s", childCount)
			}
		})
	}
}
