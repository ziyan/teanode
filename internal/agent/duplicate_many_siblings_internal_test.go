package agent

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page is found however many pages share its parent.
//
// The name tier used to list the parent and look through what came back.
// That listing is of the whole subtree, sorted by path and capped, so once
// a parent held more pages than the cap the one being looked for was
// usually not in what came back, the tier found nothing, and a second page
// was filed under the same parent with the same name.
func TestAPageIsFoundHoweverManySiblingsItHas(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	configuration := config.Default()
	worker := New(&Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var person *models.Agent
	var wanted *models.AgentNode
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
		// The page we are looking for, named so that it sorts last of all
		// of them: exactly the position the cap used to hide.
		if wanted, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "work/zzz-the-late-one", Kind: models.NodeProject,
			Name: "Nadia Vale", Aliases: []string{"Nadia V."},
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		// And six hundred others, each with children of its own, so the
		// subtree listing fills its cap long before it reaches the one
		// above.
		for index := 0; index < 600; index++ {
			parent := fmt.Sprintf("work/aaa-%04d", index)
			if _, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: person.ID, Path: parent, Kind: models.NodeProject,
				Name: fmt.Sprintf("Another %d", index),
			}); err != nil {
				t.Fatalf("PutAgentNode %d: %s", index, err)
			}
		}
	})

	for _, each := range []struct {
		what string
		path string
		name string
	}{
		{"by the name it was given", "work/nadia-vale", "Nadia Vale"},
		{"by the name, cased differently", "work/nadia-vale", "nadia vale"},
		{"by a name it also answers to", "work/vale", "Nadia V."},
	} {
		var found *models.AgentNode
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			if found, err = worker.findExistingPage(tx, person.ID, each.path, models.NodeProject, each.name, nil); err != nil {
				t.Fatalf("%s: findExistingPage: %s", each.what, err)
			}
		})
		if found == nil || found.ID != wanted.ID {
			t.Errorf("%s: the page past the cap was not found, so a second one would be made: %v", each.what, found)
		}
	}

	// And a name nothing answers to is still not found, so this does not
	// simply say yes to everything.
	var none *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if none, err = worker.findExistingPage(tx, person.ID, "work/somebody-else", models.NodeProject, "Somebody Else", nil); err != nil {
			t.Fatalf("findExistingPage: %s", err)
		}
	})
	if none != nil {
		t.Errorf("a name no page answers to found %q", none.Path)
	}
}
