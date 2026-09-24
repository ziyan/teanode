package db_test

import (
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The same question, asked of a database that can rank vectors and one
// that cannot, must come back with the same rows in the same order. That
// is the whole promise of keeping the column a plain array: the index is
// an optimization, never a different answer.
//
// Run against the stock image as well to prove the second half:
//
//	TEST_POSTGRES_IMAGE=postgres:17 make test
func TestVectorRanksTheSameEitherWay(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	indexing := database.VectorIndexing()
	t.Logf("vector indexing: %v", indexing)
	if expected := os.Getenv("TEANODE_TEST_VECTOR"); expected != "" {
		if (expected == "on") != indexing {
			t.Fatalf("this database was expected to have vector indexing %q, and has %v", expected, indexing)
		}
	}

	const width = 64
	const model = "test-embedding@64"

	// Vectors spread around a circle in the first two dimensions, so that
	// which is nearest to which is arithmetic rather than luck.
	made := func(angle float64) []float32 {
		vector := make([]float32, width)
		vector[0] = float32(math.Cos(angle))
		vector[1] = float32(math.Sin(angle))
		source := rand.New(rand.NewSource(int64(angle * 1000)))
		for index := 2; index < width; index++ {
			vector[index] = float32(source.NormFloat64()) * 0.001
		}
		return vector
	}

	var agent *models.Agent
	var nodeIds []string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent = graphAgent(t, tx)
		if err := database.EnsureVectorIndex(db.AgentNodeTable, model, width); err != nil {
			t.Fatalf("EnsureVectorIndex: %s", err)
		}
		for index := 0; index < 24; index++ {
			node, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: agent.ID, Path: "topics/" + string(rune('a'+index)), Kind: models.NodeTopic,
				Name: "Topic " + string(rune('a'+index)),
			})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			nodeIds = append(nodeIds, node.ID)
			angle := float64(index) * math.Pi / 12
			if err := tx.PutAgentNodeVector(agent.ID, node.ID, model, made(angle)); err != nil {
				t.Fatalf("PutAgentNodeVector: %s", err)
			}
		}
		// The width is remembered, which is what lets the index be built
		// at start without anybody having to say how wide the model is.
		widths, err := tx.ListVectorModels()
		if err != nil {
			t.Fatalf("ListVectorModels: %s", err)
		}
		if widths[model] != width {
			t.Fatalf("the model's width was recorded: %v", widths)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// A query a hair off the first vector: its five nearest are the
		// five smallest angles either side of it.
		query := made(0.01)
		found, err := tx.Nearest(db.AgentNodeTable, agent.ID, model, query, 5, db.VectorQuery{Floor: 0.25})
		if err != nil {
			t.Fatalf("Nearest: %s", err)
		}
		if len(found) != 5 {
			t.Fatalf("five nearest, and got %d", len(found))
		}
		if found[0].ID != nodeIds[0] {
			t.Fatalf("the nearest is the one it was aimed at")
		}
		for index := 1; index < len(found); index++ {
			if found[index].Score > found[index-1].Score {
				t.Fatalf("best first: %v", found)
			}
		}
		// Every answer is above the floor, and the floor keeps out what is
		// merely least unrelated: a query at right angles to everything
		// finds nothing rather than the closest of the far away.
		away := make([]float32, width)
		away[2] = 1
		none, err := tx.Nearest(db.AgentNodeTable, agent.ID, model, away, 5, db.VectorQuery{Floor: 0.25})
		if err != nil {
			t.Fatalf("Nearest, away: %s", err)
		}
		if len(none) != 0 {
			t.Fatalf("nothing here is about that: %v", none)
		}
	})

	// Another agent's vectors are never ranked, whichever path runs.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		other := graphAgent(t, tx)
		found, err := tx.Nearest(db.AgentNodeTable, other.ID, model, made(0.01), 5, db.VectorQuery{Floor: 0.25})
		if err != nil {
			t.Fatalf("Nearest for another agent: %s", err)
		}
		if len(found) != 0 {
			t.Fatalf("somebody else's graph is not searched: %v", found)
		}
	})
}

// Cosine is one function, used by the indexed path's floor and the
// server-side path's ranking alike.
func TestVectorCosine(t *testing.T) {
	same := []float32{1, 0, 0}
	if score := db.CosineSimilarity(same, same); math.Abs(score-1) > 1e-6 {
		t.Fatalf("a vector is itself: %v", score)
	}
	if score := db.CosineSimilarity([]float32{1, 0}, []float32{0, 1}); math.Abs(score) > 1e-6 {
		t.Fatalf("at right angles: %v", score)
	}
	if score := db.CosineSimilarity([]float32{1, 0}, []float32{2, 0}); math.Abs(score-1) > 1e-6 {
		t.Fatalf("length does not matter: %v", score)
	}
	if score := db.CosineSimilarity([]float32{1, 0}, []float32{1, 0, 0}); score != 0 {
		t.Fatalf("two widths are not comparable: %v", score)
	}
	if score := db.CosineSimilarity([]float32{0, 0}, []float32{1, 0}); score != 0 {
		t.Fatalf("a vector of nothing is near nothing: %v", score)
	}
}

func TestVectorIndexesKeepCollidingModelNames(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	if !database.VectorIndexing() {
		test.Skip("vector extension is not installed")
	}
	// A legacy index must survive while distinct new names receive their own indexes.
	dbtest.Exec(test, database, `CREATE INDEX fixture_legacy_vector ON agent_node_vector USING hnsw ((vector::vector(32)) vector_cosine_ops) WHERE model = 'fixture:a'`)
	modelNames := []string{"fixture:a", "fixture-a", "FIXTURE:a", strings.Repeat("prefix", 20) + "a", strings.Repeat("prefix", 20) + "b"}
	for repeat := 0; repeat < 2; repeat++ {
		for _, modelName := range modelNames {
			if err := database.EnsureVectorIndex(db.AgentNodeTable, modelName, 32); err != nil {
				test.Fatal(err)
			}
		}
	}
	// The legacy index serves its own model, so no second one is built
	// beside it; each of the other four names gets its own.
	indexCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM pg_indexes WHERE tablename = 'agent_node_vector' AND indexdef LIKE '%USING hnsw%'`)
	if indexCount != "5" {
		test.Fatalf("wanted the retained legacy index and four new ones, got %s", indexCount)
	}
	for _, modelName := range modelNames {
		covered := dbtest.QueryString(test, database, `SELECT count(*)::text FROM pg_indexes WHERE tablename = 'agent_node_vector' AND indexdef LIKE '%USING hnsw%' AND indexdef LIKE '%= ' || quote_literal('`+modelName+`') || '::text)'`)
		if covered != "1" {
			test.Fatalf("%q is covered by %s indexes, not one", modelName, covered)
		}
	}
}

// An index for another distance, or another width, is not the index a
// cosine search needs, whatever its predicate says: the right one is built
// beside it.
func TestAVectorIndexForAnotherDistanceIsNotTaken(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	if !database.VectorIndexing() {
		test.Skip("vector extension is not installed")
	}
	dbtest.Exec(test, database, `CREATE INDEX fixture_l2_vector ON agent_node_vector USING hnsw ((vector::vector(32)) vector_l2_ops) WHERE model = 'fixture:l2'`)
	dbtest.Exec(test, database, `CREATE INDEX fixture_wide_vector ON agent_node_vector USING hnsw ((vector::vector(64)) vector_cosine_ops) WHERE model = 'fixture:l2'`)
	if err := database.EnsureVectorIndex(db.AgentNodeTable, "fixture:l2", 32); err != nil {
		test.Fatal(err)
	}
	cosine := dbtest.QueryString(test, database, `SELECT count(*)::text FROM pg_indexes WHERE tablename = 'agent_node_vector' AND indexdef LIKE '%vector(32)) vector_cosine_ops)%' AND indexdef LIKE '%''fixture:l2''::text)'`)
	if cosine != "1" {
		test.Fatalf("a cosine index of width 32 is built beside the other two, and there are %s", cosine)
	}
}
