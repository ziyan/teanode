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

// A page that already answers to as many names as it may keep is still
// the page, and is not written past its bound.
//
// A page whose meaning matched gained the writer's spelling as another
// alias, with nothing counting them. PutAgentNode refuses a page with
// more than models.AliasCount of them, so the seventeenth did not add a
// name: it made every write of that page fail validation -- from here,
// from the nightly run, from the person editing it -- and the page could
// not be written again at all. Which is the worst place for it to happen,
// because this is the code that keeps one thing to one page.
func TestAPageWithEveryAliasIsStillTheSamePage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const model = "test-embedding@8"
	const width = 8
	// Two directions far enough apart that neither page is offered for
	// the other's name.
	sense := &meaning{ModelName: model, Vector: []float32{1, 0, 0, 0, 0, 0, 0, 0}}
	otherSense := &meaning{ModelName: model, Vector: []float32{0, 1, 0, 0, 0, 0, 0, 0}}

	configuration := config.Default()
	worker := New(&Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var person *models.Agent
	var full, spare *models.AgentNode
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
		if err := database.EnsureVectorIndex(db.AgentNodeTable, model, width); err != nil {
			t.Fatalf("EnsureVectorIndex: %s", err)
		}
		aliases := make([]string, 0, models.AliasCount)
		for index := 0; index < models.AliasCount; index++ {
			aliases = append(aliases, fmt.Sprintf("Spelling %d", index))
		}
		if full, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "people/alice-chen", Kind: models.NodePerson,
			Name: "Alice Chen", Aliases: aliases,
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if spare, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: person.ID, Path: "people/dana-ito", Kind: models.NodePerson, Name: "Dana Ito",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if err = tx.PutAgentNodeVector(person.ID, full.ID, model, sense.Vector); err != nil {
			t.Fatalf("PutAgentNodeVector: %s", err)
		}
		if err = tx.PutAgentNodeVector(person.ID, spare.ID, model, otherSense.Vector); err != nil {
			t.Fatalf("PutAgentNodeVector: %s", err)
		}
	})

	// The page with no room for another name: found, unchanged, and still
	// writable afterwards.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := worker.findExistingPage(tx, person.ID, "people/a-chen", models.NodePerson, "A. Chen", sense)
		if err != nil {
			t.Fatalf("findExistingPage: %s", err)
		}
		if found == nil || found.Path != "people/alice-chen" {
			t.Fatalf("the page that means the same is the page: %v", found)
		}
		if len(found.Aliases) != models.AliasCount {
			t.Fatalf("and keeps the names it may keep, not %d", len(found.Aliases))
		}
		if _, err := tx.PutAgentNode(found); err != nil {
			t.Fatalf("the page can still be written: %s", err)
		}
	})

	// And one with room still learns the name it was called, which is
	// what saves the next writer an embedding call.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		found, err := worker.findExistingPage(tx, person.ID, "people/dana", models.NodePerson, "Dana", otherSense)
		if err != nil {
			t.Fatalf("findExistingPage: %s", err)
		}
		if found == nil || found.Path != "people/dana-ito" {
			t.Fatalf("the page that means the same is the page: %v", found)
		}
		if !hasAlias(found, "Dana") {
			t.Fatalf("and answers to the name it was called: %v", found.Aliases)
		}
	})
}

// A page proposed at the root of the graph goes under the folder its
// kind belongs to; only a folder and the person's own page live there.
//
// A one-word path with no folder in it would otherwise sit beside people/
// and projects/, where nothing lists it and the dashboard's move and merge
// are hidden.
func TestAPageProposedAtTheRootGoesUnderItsKindsFolder(t *testing.T) {
	t.Parallel()
	for _, trial := range []struct {
		path string
		kind models.AgentNodeKind
		want string
	}{
		{"bramble", models.NodeThing, "things/bramble"},
		{"bramble", "", "topics/bramble"},
		{"alice-chen", models.NodePerson, "people/alice-chen"},
		{"work", models.NodeFolder, "work"},
		{"self", models.NodeSelf, "self"},
		{"projects/portal", models.NodeProject, "projects/portal"},
	} {
		path, _, _ := pageIdentity(trial.path, trial.kind, "")
		if path != trial.want {
			t.Errorf("pageIdentity(%q, %q) filed at %q, not %q", trial.path, trial.kind, path, trial.want)
		}
	}
}

// A root folder said twice is said once.
//
// A model reading a directory of people files the first at
// "people/people/alice-chen": the prompt's rule and the document's own
// shelf, one after the other. Nine pages arrived that way in one night,
// three of them a second copy of somebody who already had a page.
func TestARootFolderSaidTwiceIsSaidOnce(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		given string
		want  string
	}{
		{"a person under the folder twice", "people/people/alice-chen", "people/alice-chen"},
		{"the folder twice and nothing else", "people/people", "people"},
		{"a project under the folder twice", "projects/projects/portal", "projects/portal"},
		{"said once, left alone", "people/alice-chen", "people/alice-chen"},
		{"a folder's name deeper down is theirs", "people/alice-chen/people", "people/alice-chen/people"},
		{"two segments that are not a root folder", "work/work/mc", "work/work/mc"},
	} {
		got, _, _ := pageIdentity(testCase.given, models.NodePerson, "Somebody")
		if got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}
