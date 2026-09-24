package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page's aliases enter its own embedding, and its name enters every fact
// embedding on that page. Other page controls do not change either input.
func TestGraphPageMetadataInvalidatesOnlyAffectedVectors(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const model = "metadata-vectors"
	var agentId string
	var first, second *models.AgentNode
	var firstFact, secondFact *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		var err error
		first, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/first", Kind: models.NodeTopic, Name: "First"})
		if err != nil {
			t.Fatal(err)
		}
		storedFirst, err := tx.GetAgentNode(agentId, first.Path)
		if err != nil || storedFirst == nil || first.Version == "" || first.Version != storedFirst.Version {
			t.Fatalf("new page result must carry its stored version: returned=%v stored=%v error=%v", first, storedFirst, err)
		}
		second, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/second", Kind: models.NodeTopic, Name: "Second"})
		if err != nil {
			t.Fatal(err)
		}
		firstFact, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: first.ID, Kind: models.FactPlain, Text: "A fact."})
		if err != nil {
			t.Fatal(err)
		}
		secondFact, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: second.ID, Kind: models.FactPlain, Text: "Another fact."})
		if err != nil {
			t.Fatal(err)
		}
	})
	seed := func(node *models.AgentNode, fact *models.AgentFact) {
		t.Helper()
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if err := tx.PutAgentNodeVector(agentId, node.ID, model, []float32{0.2, 0.8}); err != nil {
				t.Fatal(err)
			}
			if err := tx.PutAgentFactVector(agentId, fact.ID, model, []float32{0.4, 0.6}); err != nil {
				t.Fatal(err)
			}
		})
	}
	wantsMissing := func(nodeMissing, factMissing bool) {
		t.Helper()
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			nodes, err := tx.ListAgentNodesWithoutVector(agentId, model, 100)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := tx.ListAgentFactsWithoutVector(agentId, model, 100)
			if err != nil {
				t.Fatal(err)
			}
			foundNode, foundFact := false, false
			for _, node := range nodes {
				if node.ID == first.ID {
					foundNode = true
				}
				if node.ID == second.ID {
					t.Error("unrelated page vector changed")
				}
			}
			for _, fact := range facts {
				if fact.ID == firstFact.ID {
					foundFact = true
				}
				if fact.ID == secondFact.ID {
					t.Error("unrelated fact vector changed")
				}
			}
			if foundNode != nodeMissing || foundFact != factMissing {
				t.Errorf("missing first vectors: page=%t fact=%t; want page=%t fact=%t", foundNode, foundFact, nodeMissing, factMissing)
			}
		})
	}
	seed(first, firstFact)
	seed(second, secondFact)

	first.Aliases = []string{"One"}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(first); err != nil {
			t.Fatal(err)
		}
	})
	wantsMissing(true, false)
	seed(first, firstFact)

	first.Pinned, first.Importance = true, 0.7
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(first); err != nil {
			t.Fatal(err)
		}
	})
	wantsMissing(false, false)

	first.Name = "First renamed"
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(first); err != nil {
			t.Fatal(err)
		}
	})
	wantsMissing(true, true)
	seed(first, firstFact)

	first.Summary = "A new summary."
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutAgentNode(first); err != nil {
			t.Fatal(err)
		}
	})
	wantsMissing(true, false)
}

func TestGraphConflictingPageInsertInvalidatesWinnerVectors(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	const model = "conflicting-page"
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics", Name: "Topics"}); err != nil {
			t.Fatal(err)
		}
	})

	seeded := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- database.Transaction(func(tx db.Transaction) error {
			node, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/conflict", Name: "Before", Pinned: true, Importance: 0.7})
			if err != nil {
				return err
			}
			fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: node.ID, Kind: models.FactPlain, Text: "A fact."})
			if err != nil {
				return err
			}
			if err := tx.PutAgentNodeVector(agentId, node.ID, model, []float32{0.2, 0.8}); err != nil {
				return err
			}
			if err := tx.PutAgentFactVector(agentId, fact.ID, model, []float32{0.2, 0.8}); err != nil {
				return err
			}
			close(seeded)
			<-release
			return nil
		})
	}()
	defer releaseGraphWriter(release)
	select {
	case <-seeded:
	case err := <-first:
		t.Fatalf("first writer stopped before seeding: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("first writer did not seed the vectors")
	}
	second := make(chan error, 1)
	go func() {
		second <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/conflict", Name: "After"})
			return err
		})
	}()
	// Prove the second writer is waiting on the unique-path insert, not on
	// creation of an ancestor or a later update of an existing child.
	waitForGraphLock(t, database, `query LIKE 'INSERT INTO "agent_node"%'`, second)
	close(release)
	if err := awaitGraphWriter(t, first); err != nil {
		t.Fatal(err)
	}
	if err := awaitGraphWriter(t, second); err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(agentId, "topics/conflict")
		if err != nil || node == nil || node.Name != "After" {
			t.Fatalf("settled page: %v %v", node, err)
		}
		if !node.Pinned || node.Importance != 0.7 {
			t.Errorf("insert-conflict winner lost its ranking state: pinned=%t importance=%v", node.Pinned, node.Importance)
		}
		facts, err := tx.ListAgentFacts(agentId, node.ID, true, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("settled fact: %v %v", facts, err)
		}
		missingNodes, err := tx.ListAgentNodesWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		missingFacts, err := tx.ListAgentFactsWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		foundNode, foundFact := false, false
		for _, candidate := range missingNodes {
			if candidate.ID == node.ID {
				foundNode = true
			}
		}
		for _, candidate := range missingFacts {
			if candidate.ID == facts[0].ID {
				foundFact = true
			}
		}
		if !foundNode || !foundFact {
			t.Errorf("conflict replacement retained stale vectors: page=%t fact=%t", !foundNode, !foundFact)
		}
	})
}

func TestGraphGuardedVectorWriteAndPageEditSerialize(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	const model = "guarded-page-edit"
	var agentId string
	var node *models.AgentNode
	var fact *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		var err error
		node, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/guarded", Name: "Before"})
		if err != nil {
			t.Fatal(err)
		}
		fact, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: node.ID, Kind: models.FactPlain, Text: "A fact."})
		if err != nil {
			t.Fatal(err)
		}
		// AddAgentFact records a page revision, so retain its current time.
		node, err = tx.GetAgentNode(agentId, node.Path)
		if err != nil {
			t.Fatal(err)
		}
	})
	vectorWritten := make(chan struct{})
	release := make(chan struct{})
	vectorResult := make(chan error, 1)
	go func() {
		vectorResult <- database.Transaction(func(tx db.Transaction) error {
			count, err := tx.PutAgentGraphVectors(agentId, []db.AgentGraphVector{
				{NodeID: node.ID, NodeModifiedAt: node.ModifiedAt, Model: model, Vector: []float32{0.2, 0.8}},
				{NodeID: node.ID, NodeModifiedAt: node.ModifiedAt, FactID: fact.ID, FactModifiedAt: fact.ModifiedAt, Model: model, Vector: []float32{0.4, 0.6}},
			})
			if err != nil {
				return err
			}
			if count != 2 {
				return errors.New("guarded vectors were not written")
			}
			close(vectorWritten)
			<-release
			return nil
		})
	}()
	defer releaseGraphWriter(release)
	select {
	case <-vectorWritten:
	case err := <-vectorResult:
		t.Fatalf("guarded writer stopped before writing vectors: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("guarded writer did not write vectors")
	}
	editResult := make(chan error, 1)
	go func() {
		editResult <- database.Transaction(func(tx db.Transaction) error {
			changed := *node
			changed.Name = "After"
			_, err := tx.PutAgentNode(&changed)
			return err
		})
	}()
	waitForGraphLock(t, database, `query LIKE 'SELECT%FROM "agent_node"%' AND query LIKE '%FOR UPDATE%'`, editResult)
	close(release)
	if err := awaitGraphWriter(t, vectorResult); err != nil {
		t.Fatal(err)
	}
	if err := awaitGraphWriter(t, editResult); err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		missingNodes, err := tx.ListAgentNodesWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		missingFacts, err := tx.ListAgentFactsWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		foundNode, foundFact := false, false
		for _, candidate := range missingNodes {
			if candidate.ID == node.ID {
				foundNode = true
			}
		}
		for _, candidate := range missingFacts {
			if candidate.ID == fact.ID {
				foundFact = true
			}
		}
		if !foundNode || !foundFact {
			t.Errorf("page edit retained stale guarded vectors: page=%t fact=%t", !foundNode, !foundFact)
		}
	})
}

func releaseGraphWriter(release chan struct{}) {
	select {
	case <-release:
	default:
		close(release)
	}
}

func awaitGraphWriter(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("graph writer did not finish")
		return nil
	}
}

func waitForGraphLock(t *testing.T, database db.Database, queryCondition string, result <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		count := dbtest.QueryString(t, database, `SELECT count(*)::text FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND `+queryCondition)
		if count != "0" {
			return
		}
		select {
		case err := <-result:
			t.Fatalf("graph writer finished before blocking on its row: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("graph writer did not block on its row")
}

func TestGraphMetadataVectorInvalidationRollsBack(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	const model = "metadata-rollback"
	var agentId string
	var node *models.AgentNode
	var fact *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId = graphAgent(t, tx).ID
		var err error
		node, err = tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/rollback", Name: "Before"})
		if err != nil {
			t.Fatal(err)
		}
		fact, err = tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: node.ID, Kind: models.FactPlain, Text: "A fact."})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.PutAgentNodeVector(agentId, node.ID, model, []float32{0.2, 0.8}); err != nil {
			t.Fatal(err)
		}
		if err := tx.PutAgentFactVector(agentId, fact.ID, model, []float32{0.2, 0.8}); err != nil {
			t.Fatal(err)
		}
	})
	rollback := errors.New("rollback")
	err := database.Transaction(func(tx db.Transaction) error {
		changed := *node
		changed.Name = "After"
		if _, err := tx.PutAgentNode(&changed); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("transaction: %v", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, err := tx.GetAgentNode(agentId, node.Path)
		if err != nil || stored.Name != "Before" {
			t.Fatalf("rolled-back page: %v %v", stored, err)
		}
		nodes, err := tx.ListAgentNodesWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := tx.ListAgentFactsWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, missing := range nodes {
			if missing.ID == node.ID {
				t.Error("page vector was deleted after rollback")
			}
		}
		for _, missing := range facts {
			if missing.ID == fact.ID {
				t.Error("fact vector was deleted after rollback")
			}
		}
	})
}

func TestGraphMovingFactInvalidatesChangedPageName(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	const model = "moved-fact"
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agentId := graphAgent(t, tx).ID
		from, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/from", Name: "Shared"})
		if err != nil {
			t.Fatal(err)
		}
		same, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/same", Name: "Shared"})
		if err != nil {
			t.Fatal(err)
		}
		different, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "topics/different", Name: "Different"})
		if err != nil {
			t.Fatal(err)
		}
		fact, err := tx.AddAgentFact(&models.AgentFact{AgentID: agentId, NodeID: from.ID, Kind: models.FactPlain, Text: "A fact."})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.PutAgentFactVector(agentId, fact.ID, model, []float32{0.2, 0.8}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.MoveAgentFact(agentId, fact.ID, same.ID); err != nil {
			t.Fatal(err)
		}
		missing, err := tx.ListAgentFactsWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range missing {
			if candidate.ID == fact.ID {
				t.Error("same page name invalidated unchanged embedding")
			}
		}
		if _, err := tx.MoveAgentFact(agentId, fact.ID, different.ID); err != nil {
			t.Fatal(err)
		}
		missing, err = tx.ListAgentFactsWithoutVector(agentId, model, 100)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, candidate := range missing {
			if candidate.ID == fact.ID {
				found = true
			}
		}
		if !found {
			t.Error("fact vector survived a change in embedded page name")
		}
	})
}
