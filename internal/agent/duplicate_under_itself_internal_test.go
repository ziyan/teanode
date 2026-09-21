package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page asked for under a page of the same name is that page.
//
// The graph held projects/teanode/teanode with thirteen facts under
// projects/teanode with a hundred and seventy-one: one subject on two
// pages, and neither of them whole. Nothing above catches it -- the path
// tier is looking for a path that does not exist yet, which is the one
// moment it can be caught, because once the page is made every later
// write goes to the wrong one.
func TestAPageUnderAPageOfTheSameNameIsThatPage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	configuration := config.Default()
	worker := New(&Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var person *models.Agent
	var above *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		if above, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "projects/teanode", Kind: models.NodeProject, Name: "TeaNode",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		// A page of another name under it is an ordinary child and must
		// go on being one.
		if _, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "projects/teanode/the-reader", Kind: models.NodeProject, Name: "The reader",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
	})

	for _, each := range []struct {
		what        string
		path        string
		name        string
		isTheParent bool
	}{
		{"the same name, spelled the same", "projects/teanode/teanode", "TeaNode", true},
		{"the same name, cased differently", "projects/teanode/TeaNode", "teanode", true},
		{"the slug alone says it, the name is empty", "projects/teanode/teanode", "", true},
		{"a child of its own name is still a child", "projects/teanode/the-reader", "The reader", false},
		{"a child of another name is not the parent", "projects/teanode/the-writer", "The writer", false},
	} {
		var found *models.AgentNode
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			found, err = worker.findExistingPage(tx, person.ID, each.path, models.NodeProject, each.name, nil)
			if err != nil {
				t.Fatalf("%s: findExistingPage: %s", each.what, err)
			}
		})
		landedAbove := found != nil && found.ID == above.ID
		if landedAbove != each.isTheParent {
			t.Errorf("%s: landed on the page above = %t, wanted %t (got %v)",
				each.what, landedAbove, each.isTheParent, found)
		}
	}
}
