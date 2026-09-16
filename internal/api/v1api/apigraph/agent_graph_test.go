package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A sentence filed under the wrong name used to leave the person one way
// out: strike it and type it again on the right page, which throws away
// the words it came from and the day it was learned. This carries the
// fact over whole. It takes a new number where it lands, because a
// number belongs to the page, and the page it is sent to has to exist —
// a mistyped path that made a page would hide the sentence somewhere
// nobody reads.
func TestMoveAgentFactCarriesTheSentenceOverWhole(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	var agent *models.Agent
	var from, destination *models.AgentNode
	var fact *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "fact-mover", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err = tx.EnsureAgentRoots(agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		if from, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if destination, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "projects/portal", Kind: models.NodeProject, Name: "Portal",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if fact, err = tx.AddAgentFact(&models.AgentFact{
			AgentID: agent.ID, NodeID: from.ID, Kind: models.FactPlain,
			Text:      "The portal ships on Fridays.",
			Evidence:  []models.Evidence{{Kind: models.EvidencePerson, Quote: "we ship it on Fridays"}},
			Audiences: []models.AgentAudience{models.AudienceAsk},
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})

	principal := &api.Principal{
		User: owner,
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionAgentUse},
		}),
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		moved, err := resolver.MoveAgentFact(ctx, MoveAgentFactArguments{
			Path: "people/alice-chen", Number: fact.Number, To: "projects/portal",
		})
		if err != nil {
			t.Fatalf("MoveAgentFact: %s", err)
		}
		if moved.ID != fact.ID {
			t.Errorf("the sentence itself moves, not a copy: %q then %q", fact.ID, moved.ID)
		}
		if moved.Text != fact.Text || len(moved.Evidence) != len(fact.Evidence) {
			t.Errorf("it keeps its words and where they came from: %+v", moved)
		}
		if moved.NodeID != destination.ID {
			t.Errorf("it is on %q, want %q", moved.NodeID, destination.ID)
		}

		found, err := tx.GetAgentFact(agent.ID, destination.ID, moved.Number)
		if err != nil || found == nil || found.ID != fact.ID {
			t.Errorf("it is reached by the new page and number: %v %s", found, err)
		}
		gone, err := tx.GetAgentFact(agent.ID, from.ID, fact.Number)
		if err != nil || gone != nil {
			t.Errorf("and is no longer on the page it left: %v %s", gone, err)
		}

		if _, err := resolver.MoveAgentFact(ctx, MoveAgentFactArguments{
			Path: "projects/portal", Number: moved.Number, To: "projects/atlas",
		}); err == nil {
			t.Errorf("a page that is not there is refused, not made on the way")
		}
		if _, err := resolver.MoveAgentFact(ctx, MoveAgentFactArguments{
			Path: "projects/portal", Number: moved.Number + 99, To: "people/alice-chen",
		}); err == nil {
			t.Errorf("a number that names no fact is refused")
		}
	})
}
