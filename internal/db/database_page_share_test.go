package db_test

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A world of four people: ada and ben share the circle "home", ben and cora
// share "club", and dave is in none. Ada keeps three pages that matter here:
// people/a and the page under it, and people/ab, which is not under it.
type pageShareWorld struct {
	ada, ben, cora, dave *models.User
	home, club           *models.Circle
	adaAgent             *models.Agent
}

func newPageShareWorld(t *testing.T, tx db.Transaction) *pageShareWorld {
	t.Helper()
	shared := newShareWorld(t, tx)
	world := &pageShareWorld{
		ada: shared.ada, ben: shared.ben, cora: shared.cora, dave: shared.dave,
		home: shared.home, club: shared.club,
	}
	var err error
	if world.adaAgent, err = tx.EnsureAgent(world.ada.ID); err != nil {
		t.Fatalf("EnsureAgent: %s", err)
	}
	if err = tx.EnsureAgentRoots(world.adaAgent.ID); err != nil {
		t.Fatalf("EnsureAgentRoots: %s", err)
	}
	for path, name := range map[string]string{
		"people/a": "A", "people/a/x": "A and X", "people/ab": "AB",
	} {
		if _, err = tx.PutAgentNode(&models.AgentNode{AgentID: world.adaAgent.ID, Path: path, Kind: models.NodePerson, Name: name}); err != nil {
			t.Fatalf("PutAgentNode %s: %s", path, err)
		}
	}
	return world
}

func reachingPaths(t *testing.T, tx db.Transaction, reader *models.User) []string {
	t.Helper()
	shares, err := tx.ListPageSharesReaching(reader.ID)
	if err != nil {
		t.Fatalf("ListPageSharesReaching: %s", err)
	}
	paths := []string{}
	for _, share := range shares {
		paths = append(paths, share.Path)
	}
	return paths
}

// A person shares only a page they have, with somebody they share a circle
// with or a circle they are in, and a refusal says not found rather than why.
func TestAPageIsSharedOnlyWithinACircle(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)

		share, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "/People/A/", GranteeUserID: world.ben.ID})
		if err != nil || share.ID == "" || share.Path != "people/a" || share.OwnerUsername != "ada" || share.GranteeUsername != "ben" {
			t.Fatalf("a share with someone in a shared circle: %+v %v", share, err)
		}
		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: stranger.ID}); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeCircleID: world.home.ID}); err != nil {
			t.Fatalf("a share with her own circle: %s", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeCircleID: world.club.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with a circle she is not in: %v", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/nobody", GranteeUserID: world.ben.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a page she does not have: %v", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ada.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a share with herself: %v", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a share with nobody: %v", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a share with a person and a circle at once: %v", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "", GranteeUserID: world.ben.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a share of the whole graph: %v", err)
		}
	})
}

// Sharing the same page with the same person again changes whether the quotes
// are shared and keeps the share's identifier.
func TestSharingAgainChangesTheQuotesAndKeepsTheShare(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)
		first, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID})
		if err != nil || first.IsQuoteShared {
			t.Fatalf("first: %+v %v", first, err)
		}
		second, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID, IsQuoteShared: true})
		if err != nil || second.ID != first.ID || !second.IsQuoteShared {
			t.Fatalf("second: %+v %v", second, err)
		}
		listed, err := tx.ListPageSharesBy(world.ada.ID)
		if err != nil || len(listed) != 1 {
			t.Fatalf("one share, not two: %+v %v", listed, err)
		}
	})
}

// A share reaches the reader it is for, through a person or a circle, and
// nobody else; the owner is not a reader of their own share.
func TestAShareReachesOnlyItsReaders(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/ab", GranteeCircleID: world.home.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		if got := reachingPaths(t, tx, world.ben); len(got) != 2 || got[0] != "people/a" || got[1] != "people/ab" {
			t.Errorf("ben reads both: %v", got)
		}
		for _, other := range []*models.User{world.ada, world.cora, world.dave} {
			if got := reachingPaths(t, tx, other); len(got) != 0 {
				t.Errorf("%s reads nothing: %v", other.Username, got)
			}
		}
		shares, err := tx.ListPageSharesReaching(world.ben.ID)
		if err != nil || shares[0].OwnerUserID != world.ada.ID || shares[0].OwnerName != "ada" || shares[0].OwnerUser == nil {
			t.Fatalf("the owner is named: %+v %v", shares, err)
		}
	})
}

// Leaving the circle two people shared ends the direct shares between them in
// the same transaction, both ways, and a share to a circle stops reaching a
// person who left it and is deleted when its maker left.
func TestLeavingACircleEndsPageShares(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/ab", GranteeCircleID: world.home.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		// Ben shares his own page with ada, to prove it goes both ways.
		benAgent, err := tx.EnsureAgent(world.ben.ID)
		if err != nil {
			t.Fatalf("EnsureAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(benAgent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		if _, err := tx.PutAgentNode(&models.AgentNode{AgentID: benAgent.ID, Path: "people/b", Kind: models.NodePerson, Name: "B"}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.SharePage(world.ben.ID, &models.AgentPageShare{Path: "people/b", GranteeUserID: world.ada.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		// Ben also shares with the club, which ada is not in.
		if _, err := tx.SharePage(world.ben.ID, &models.AgentPageShare{Path: "people/b", GranteeCircleID: world.club.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}

		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}

		if got := reachingPaths(t, tx, world.ben); len(got) != 0 {
			t.Errorf("ben no longer reads ada's pages: %v", got)
		}
		if got := reachingPaths(t, tx, world.ada); len(got) != 0 {
			t.Errorf("ada no longer reads ben's page: %v", got)
		}
		if got := reachingPaths(t, tx, world.cora); len(got) != 1 || got[0] != "people/b" {
			t.Errorf("cora still reads what ben shared with the club: %v", got)
		}
		adas, err := tx.ListPageSharesBy(world.ada.ID)
		if err != nil || len(adas) != 1 || adas[0].GranteeCircleID != world.home.ID {
			t.Errorf("ada's share to the circle she is still in stays: %+v %v", adas, err)
		}
		bens, err := tx.ListPageSharesBy(world.ben.ID)
		if err != nil || len(bens) != 1 || bens[0].GranteeCircleID != world.club.ID {
			t.Errorf("only ben's share to the club is left: %+v %v", bens, err)
		}
	})
}

// A direct share stays while the two still share another circle.
func TestADirectShareStaysWhileAnotherCircleIsShared(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)
		other, err := tx.CreateCircle(&models.Circle{Name: "Other", CircleKind: models.CircleKindFriends}, world.ada.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err := tx.AddCircleMember(other.ID, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if got := reachingPaths(t, tx, world.ben); len(got) != 1 {
			t.Errorf("they still share a circle: %v", got)
		}
	})
}

// Deleting a circle ends the shares between the people it joined, and the
// shares made to it go with it.
func TestDeletingACircleEndsPageShares(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/a", GranteeUserID: world.ben.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		if _, err := tx.SharePage(world.ada.ID, &models.AgentPageShare{Path: "people/ab", GranteeCircleID: world.home.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
		if err := tx.DeleteCircle(world.home.ID); err != nil {
			t.Fatalf("DeleteCircle: %s", err)
		}
		if got := reachingPaths(t, tx, world.ben); len(got) != 0 {
			t.Errorf("nothing reaches ben: %v", got)
		}
		if shares, err := tx.ListPageSharesBy(world.ada.ID); err != nil || len(shares) != 0 {
			t.Errorf("no share is left: %+v %v", shares, err)
		}
	})
}

// A search within the roots of a share keeps to them at a slash boundary:
// people/a and what is under it, and never people/ab.
func TestASearchWithinRootsStopsAtTheBoundary(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newPageShareWorld(t, tx)
		for path, text := range map[string]string{
			"people/a": "keeps bees", "people/a/x": "keeps bees too", "people/ab": "keeps bees as well",
		} {
			node, err := tx.GetAgentNode(world.adaAgent.ID, path)
			if err != nil || node == nil {
				t.Fatalf("GetAgentNode %s: %v %s", path, node, err)
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: world.adaAgent.ID, NodeID: node.ID, Kind: models.FactPlain, Text: text, Confidence: 1}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
		nodes, facts, err := tx.SearchAgentGraphWithin(world.adaAgent.ID, []string{"people/a"}, "bees", 20)
		if err != nil {
			t.Fatalf("SearchAgentGraphWithin: %s", err)
		}
		if len(facts) != 2 {
			t.Errorf("two facts under people/a, not three: %d", len(facts))
		}
		for _, fact := range facts {
			if fact.Text == "keeps bees as well" {
				t.Errorf("a fact of people/ab was found")
			}
		}
		for _, node := range nodes {
			if node.Path == "people/ab" {
				t.Errorf("people/ab was found")
			}
		}
	})
}
