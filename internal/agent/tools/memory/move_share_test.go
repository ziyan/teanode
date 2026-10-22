package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A share is a path, so moving a page into a shared subtree shares it, and
// moving it out stops. The answer says so, because the person asked to file
// a page and not to share one.
func TestMovingAPageSaysWhetherItIsNowShared(t *testing.T) {
	run, database, closeDatabase := person(t, "alice")
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		friend, err := tx.CreateUser(&models.User{Username: "ben", Name: "Ben Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		circle, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, run.owner.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err = tx.AddCircleMember(circle.ID, friend.ID, models.MemberRoleMember, run.owner.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if err = tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		for _, path := range []string{"shared", "private", "people/a"} {
			if _, err = tx.PutAgentNode(&models.AgentNode{AgentID: run.agent.ID, Path: path, Kind: models.NodeFolder}); err != nil {
				t.Fatalf("PutAgentNode %s: %s", path, err)
			}
		}
		if _, err = tx.SharePage(run.owner.ID, &models.AgentPageShare{Path: "shared", GranteeUserID: friend.ID}); err != nil {
			t.Fatalf("SharePage: %s", err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}

	if answer := call(`{"action":"move","path":"people/a","to":"shared"}`); answer != "people/a is now shared/a, and it is now shared with ben, read only" {
		t.Errorf("moved into a shared subtree: %q", answer)
	}
	if answer := call(`{"action":"move","path":"shared/a","to":"private"}`); answer != "shared/a is now private/a, and it is no longer shared with ben" {
		t.Errorf("moved out of a shared subtree: %q", answer)
	}
	if answer := call(`{"action":"move","path":"private/a","to":"people"}`); strings.Contains(answer, "shared") {
		t.Errorf("moved between pages nobody shares: %q", answer)
	}
	// The shared page itself moves with its share, and nothing is said.
	if answer := call(`{"action":"move","path":"shared","to":"people"}`); answer != "shared is now people/shared" {
		t.Errorf("a shared page moved with its share: %q", answer)
	}
}
