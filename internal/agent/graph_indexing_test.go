package agent_test

import (
	"strconv"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestGraphEmbeddingCountsOnlyCommittedVectors(test *testing.T) {
	for _, failure := range []string{"statement", "commit"} {
		test.Run(failure, func(test *testing.T) {
			world := newRememberWorldThatEmbeds(test, func(string) string { return "" })
			dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
				if err := transaction.EnsureAgentRoots(world.agent.ID); err != nil {
					test.Fatal(err)
				}
				node, err := transaction.GetAgentNode(world.agent.ID, models.PathNotes)
				if err != nil || node == nil {
					test.Fatalf("notes page: %v, %v", node, err)
				}
				if _, err := transaction.AddAgentFact(&models.AgentFact{
					AgentID: world.agent.ID, NodeID: node.ID, Kind: models.FactPlain,
					Text: "The fixture requires a searchable fact.", Confidence: 1,
				}); err != nil {
					test.Fatal(err)
				}
			})
			// Facts follow pages in the same transaction. Refusing the fact must
			// roll back those pages and must not report them as completed work.
			dbtest.Exec(test, world.database, `CREATE FUNCTION refuse_fact_vector() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture vector failure'; END $$`)
			if failure == "commit" {
				dbtest.Exec(test, world.database, `CREATE CONSTRAINT TRIGGER refuse_fact_vector AFTER INSERT ON agent_fact_vector DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION refuse_fact_vector()`)
			} else {
				dbtest.Exec(test, world.database, `CREATE TRIGGER refuse_fact_vector BEFORE INSERT ON agent_fact_vector FOR EACH ROW EXECUTE FUNCTION refuse_fact_vector()`)
			}
			writtenCount, err := world.worker.EmbedGraph(test.Context(), world.agent, 100)
			if err == nil || writtenCount != 0 {
				test.Fatalf("failed batch reported %d writes: %v", writtenCount, err)
			}
			vectorCount := func() string {
				return dbtest.QueryString(test, world.database, `SELECT ((SELECT count(*) FROM agent_node_vector) + (SELECT count(*) FROM agent_fact_vector))::text`)
			}
			if count := vectorCount(); count != "0" {
				test.Fatalf("rolled-back batch retained %s vectors", count)
			}
			dbtest.Exec(test, world.database, `DROP FUNCTION refuse_fact_vector() CASCADE`)
			writtenCount, err = world.worker.EmbedGraph(test.Context(), world.agent, 100)
			if err != nil || writtenCount < 2 {
				test.Fatalf("retry reported %d writes: %v", writtenCount, err)
			}
			if count := vectorCount(); count != strconv.Itoa(writtenCount) {
				test.Fatalf("retry reported %d writes, stored %s", writtenCount, count)
			}
			writtenCount, err = world.worker.EmbedGraph(test.Context(), world.agent, 100)
			if err != nil || writtenCount != 0 {
				test.Fatalf("completed batch was repeated: %d, %v", writtenCount, err)
			}
		})
	}
}
