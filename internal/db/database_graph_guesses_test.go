package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// migrationSQL is one migration's two halves, by the name of its files.
// The backfill has to be run against rows seeded after it, so the test
// applies it itself rather than letting the runner do it on an empty
// database.
func migrationSQL(t *testing.T, id string) (string, string) {
	t.Helper()
	for _, migration := range migrations.Migrations() {
		if migration.ID == id {
			return migration.SQL, migration.ReverseSQL
		}
	}
	t.Fatalf("there is no migration called %q", id)
	return "", ""
}

// The backfill in 0088 tells the links an earlier night guessed from the
// links somebody stated, using only what is already stored, and leaves
// alone everything whose provenance is not clear.
//
// The rows here are the shapes a real graph holds: the walk as an older
// build wrote it, a link the person drew, a guess the person later drew
// themselves, and the month page's own arithmetic -- which the night also
// writes, and which must not be called a guess.
func TestMigration0088MarksOnlyTheNightsGuesses(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agentId string
	var pages map[string]*models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		agentId = agent.ID
		pages = map[string]*models.AgentNode{}
		for _, page := range []struct {
			path string
			kind models.AgentNodeKind
		}{
			{"people/alice-chen", models.NodePerson},
			{"projects/portal", models.NodeProject},
			{"projects/attic", models.NodeProject},
			{"things/latch", models.NodeThing},
			{"places/rivermouth", models.NodePlace},
			{"topics/controls", models.NodeTopic},
			{"time/2026/09", models.NodePeriod},
		} {
			pages[page.path] = putNode(t, tx, agent.ID, page.path, page.kind, page.path)
		}
	})
	link := func(actor models.RevisionActor, from, to string, relation models.AgentEdgeRelation, note string, evidence []models.Evidence) {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			tx.AsActor(actor)
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: agentId, FromID: pages[from].ID, ToID: pages[to].ID,
				Relation: relation, Note: note, Evidence: evidence,
			}); err != nil {
				t.Fatalf("PutAgentEdge %s -> %s: %s", from, to, err)
			}
		})
	}
	statusOf := func(where, from, to string) models.AgentEdgeStatus {
		var status models.AgentEdgeStatus
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			edges, err := tx.ListAgentEdges(agentId, pages[from].ID)
			if err != nil {
				t.Fatalf("ListAgentEdges %s: %s", where, err)
			}
			for _, edge := range edges {
				if edge.ToPath == to {
					status = edge.Status
					return
				}
			}
			t.Fatalf("the link %s -> %s is gone %s", from, to, where)
		})
		return status
	}

	// A walk as the build before the 'dream' evidence kind wrote it: the
	// path it took, filed as a document that does not exist, under the
	// dream's own actor.
	link(models.ActorDream, "people/alice-chen", "things/latch", models.EdgeWorksOn,
		"she may have written the payload angle check",
		[]models.Evidence{{Kind: models.EvidenceDocument, Quote: "people/alice-chen → projects/portal → things/latch"}})

	// A link the person drew in the Link dialog, which says nothing about
	// where it came from because there is nothing to say.
	link(models.ActorPerson, "people/alice-chen", "projects/portal", models.EdgeWorksOn, "since 2019", nil)

	// A guess the person went on to draw themselves. The night wrote it
	// down as a proposal, so the walk is recognizable, but somebody has
	// since said the same thing and a link somebody said is stated.
	link(models.ActorDream, "people/alice-chen", "places/rivermouth", models.EdgeLocatedIn, "perhaps", nil)
	link(models.ActorPerson, "people/alice-chen", "places/rivermouth", models.EdgeLocatedIn, "she moved there in 2019", nil)

	// A walk from a build that wrote no evidence at all. The night's own
	// record of what it proposed is the only trace left, and it is
	// enough.
	link(models.ActorDream, "projects/portal", "topics/controls", models.EdgeRelatedTo, "perhaps", nil)

	// The month's page links what it was about by counting facts. The
	// dream writes it, under the dream's actor, and it is arithmetic
	// rather than a guess: nothing marks it, and nothing may change it.
	link(models.ActorDream, "time/2026/09", "projects/attic", models.EdgeAboutPlace, "4 facts from September 2026", nil)

	// The night's own record of what it proposed, which is the third
	// place a walk leaves a trace and the only one left for the link the
	// person later drew.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		dream, err := tx.StartAgentDream(&models.AgentDream{AgentID: agentId, StartedAt: time.Now()})
		if err != nil {
			t.Fatalf("StartAgentDream: %s", err)
		}
		finished := time.Now()
		dream.FinishedAt = &finished
		dream.Proposals = []models.DreamProposal{
			{Kind: "linked", Path: "people/alice-chen", To: "places/rivermouth", Reason: "perhaps"},
			{Kind: "linked", Path: "projects/portal", To: "topics/controls", Reason: "perhaps"},
			{Kind: "gap", Reason: "who runs the portal now"},
		}
		if err := tx.FinishAgentDream(dream); err != nil {
			t.Fatalf("FinishAgentDream: %s", err)
		}
	})

	// Everything starts stated, which is what 0085's default left behind.
	for _, pair := range [][2]string{
		{"people/alice-chen", "things/latch"},
		{"people/alice-chen", "projects/portal"},
		{"people/alice-chen", "places/rivermouth"},
		{"projects/portal", "topics/controls"},
		{"time/2026/09", "projects/attic"},
	} {
		if status := statusOf("before the backfill", pair[0], pair[1]); status != models.EdgeStated {
			t.Fatalf("%s -> %s starts stated, not %q", pair[0], pair[1], status)
		}
	}

	forward, reverse := migrationSQL(t, "0088_agent_edge_dream_guesses")
	dbtest.Exec(t, database, forward)

	if status := statusOf("after the backfill", "people/alice-chen", "things/latch"); status != models.EdgeProposed {
		t.Fatalf("the walk is a guess, not %q", status)
	}
	if status := statusOf("after the backfill", "people/alice-chen", "projects/portal"); status != models.EdgeStated {
		t.Fatalf("what the person drew stays stated, not %q", status)
	}
	if status := statusOf("after the backfill", "people/alice-chen", "places/rivermouth"); status != models.EdgeStated {
		t.Fatalf("a guess the person went on to state is stated, not %q", status)
	}
	if status := statusOf("after the backfill", "projects/portal", "topics/controls"); status != models.EdgeProposed {
		t.Fatalf("the night's own record of a proposal is enough, not %q", status)
	}
	if status := statusOf("after the backfill", "time/2026/09", "projects/attic"); status != models.EdgeStated {
		t.Fatalf("the month's arithmetic is not a guess, not %q", status)
	}

	// Running it twice changes nothing, and marks nothing twice: the
	// rows it would touch are no longer stated.
	dbtest.Exec(t, database, forward)
	marks := dbtest.QueryString(t, database,
		`SELECT count(*)::text FROM "agent_edge", jsonb_array_elements("evidence") AS entry
		 WHERE entry->>'quote' = 'marked by migration 0088 as a link the night guessed'`)
	if marks != "2" {
		t.Fatalf("the two rows it changed are marked, once each, and got %q", marks)
	}

	// And going back is exact: the one row it changed, with its own
	// evidence as it was and the marker gone.
	dbtest.Exec(t, database, reverse)
	if status := statusOf("after reverting", "people/alice-chen", "things/latch"); status != models.EdgeStated {
		t.Fatalf("reverting puts it back, not %q", status)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		edges, err := tx.ListAgentEdges(agentId, pages["things/latch"].ID)
		if err != nil || len(edges) != 1 {
			t.Fatalf("ListAgentEdges: %v %s", edges, err)
		}
		if len(edges[0].Evidence) != 1 || !strings.Contains(edges[0].Evidence[0].Quote, "→") {
			t.Fatalf("the walk keeps its own evidence and loses the marker: %v", edges[0].Evidence)
		}
	})
}
