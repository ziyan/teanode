package apigraph

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Asking for an overview again makes the page due at the next night and
// leaves the overview it has where it is; a page that is not there says
// so.
func TestAnOverviewAskedForAgainIsDueAndKept(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var person *models.User
	var agentId, pageId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "gardener", Name: "Example Gardener"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		created, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = created.ID
		page, err := tx.PutAgentNode(&models.AgentNode{AgentID: agentId, Path: "places/allotment", Kind: models.NodePlace, Name: "Allotment"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		pageId = page.ID
		if err := tx.SetAgentNodeOverview(agentId, page.ID, "## What it is\n\nA plot by the river.", nil, "an-earlier-hash", time.Now()); err != nil {
			t.Fatalf("SetAgentNodeOverview: %s", err)
		}
	})
	resolver := &graph{database: database, config: config.NewMemoryStore(config.Default()), settings: &api.Settings{}}
	principal := &api.Principal{User: person, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)
		if isAsked, err := resolver.RewriteAgentOverview(ctx, RewriteAgentOverviewArguments{Path: "places/allotment"}); err != nil || !isAsked {
			t.Fatalf("RewriteAgentOverview: %v %s", isAsked, err)
		}
		if _, err := resolver.RewriteAgentOverview(ctx, RewriteAgentOverviewArguments{Path: "places/nowhere"}); err == nil {
			t.Errorf("a page that is not there is refused")
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		page, err := tx.GetAgentNodeByID(agentId, pageId)
		if err != nil || page == nil {
			t.Fatalf("GetAgentNodeByID: %v %v", page, err)
		}
		if page.OverviewInputs != "" || page.Overview != "## What it is\n\nA plot by the river." {
			t.Errorf("the hash is cleared and the overview kept: %q %q", page.OverviewInputs, page.Overview)
		}
	})
}
