package db_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page keeps how it got to say what it says: what changed, who changed
// it, and what was there before.
//
// Without the "before", a history can be read but not undone, which is
// the half of it somebody actually wants at the moment they go looking.
func TestRevisionsRecordWhatChangedAndWhoChangedIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		tx.AsActor(models.ActorDream)
		portal := putNode(t, tx, agent.ID, "projects/portal", models.NodeProject, "Portal")
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "projects/portal", Kind: models.NodeProject,
			Name: "Portal", Summary: "the customer-facing site",
		}); err != nil {
			t.Fatalf("rewriting the page: %s", err)
		}
		tx.AsActor(models.ActorPerson)
		if _, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "projects/portal", Kind: models.NodeProject,
			Name: "Portal", Summary: "the site customers log in to",
		}); err != nil {
			t.Fatalf("the person rewriting it: %s", err)
		}

		revisions, err := tx.ListAgentRevisions(agent.ID, portal.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		if len(revisions) != 3 {
			t.Fatalf("made, written, rewritten — three changes, not %d: %v", len(revisions), revisions)
		}
		// Newest first, so the person's own rewrite is at the top, and it
		// carries what the nightly run had put there.
		newest := revisions[0]
		if newest.Actor != models.ActorPerson {
			t.Fatalf("the last change was the person's, not %q", newest.Actor)
		}
		if newest.TextBefore() != "the customer-facing site" {
			t.Fatalf("a change carries what was there before, not %q", newest.TextBefore())
		}
		if !strings.Contains(newest.Describe(), "you") {
			t.Fatalf("a history says who: %q", newest.Describe())
		}
		if revisions[1].Actor != models.ActorDream {
			t.Fatalf("the one before was the nightly run's, not %q", revisions[1].Actor)
		}
		// And the numbers rise rather than being reused.
		if revisions[0].Revision <= revisions[1].Revision {
			t.Fatalf("revision numbers rise: %d then %d", revisions[1].Revision, revisions[0].Revision)
		}
	})
}

// A link is a change to both pages it joins, and each page's history
// names the other end. "Linked it to something" tells nobody anything.
func TestRevisionsNameTheOtherEndOfALink(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		alice := putNode(t, tx, agent.ID, "people/alice-chen", models.NodePerson, "Alice Chen")
		portal := putNode(t, tx, agent.ID, "projects/portal", models.NodeProject, "Portal")
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: portal.ID,
			Relation: models.EdgeWorksOn, Note: "led the controls work",
		}); err != nil {
			t.Fatalf("PutAgentEdge: %s", err)
		}

		for _, side := range []struct {
			nodeId string
			other  string
		}{{alice.ID, "projects/portal"}, {portal.ID, "people/alice-chen"}} {
			revisions, err := tx.ListAgentRevisions(agent.ID, side.nodeId, 50)
			if err != nil {
				t.Fatalf("ListAgentRevisions: %s", err)
			}
			if len(revisions) == 0 || revisions[0].Kind != models.RevisionLinked {
				t.Fatalf("both ends record the link: %v", revisions)
			}
			if !strings.Contains(revisions[0].Describe(), side.other) {
				t.Fatalf("each end names the other: %q", revisions[0].Describe())
			}
		}

		// The nightly run writes every edge every night to reweight it.
		// If that filed a change each time, a page's history would fill
		// with changes nobody made.
		before, err := tx.ListAgentRevisions(agent.ID, alice.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: portal.ID,
			Relation: models.EdgeWorksOn, Note: "led the controls work", Weight: 2,
		}); err != nil {
			t.Fatalf("PutAgentEdge again: %s", err)
		}
		after, err := tx.ListAgentRevisions(agent.ID, alice.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		if len(after) != len(before) {
			t.Fatalf("the same link said again is not a change: %d then %d", len(before), len(after))
		}

		// A link whose meaning changed is a change, and carries the old
		// words.
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agent.ID, FromID: alice.ID, ToID: portal.ID,
			Relation: models.EdgeWorksOn, Note: "led the controls work until 2025",
		}); err != nil {
			t.Fatalf("PutAgentEdge with a new note: %s", err)
		}
		changed, err := tx.ListAgentRevisions(agent.ID, alice.ID, 50)
		if err != nil {
			t.Fatalf("ListAgentRevisions: %s", err)
		}
		if len(changed) != len(before)+1 {
			t.Fatalf("a link that means something else is a change: %d then %d", len(before), len(changed))
		}
		if changed[0].TextBefore() != "led the controls work" {
			t.Fatalf("and carries what it said before, not %q", changed[0].TextBefore())
		}
	})
}
