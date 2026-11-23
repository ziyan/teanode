package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// A screen follows the feed through a subscription, whose request carries no
// transaction of its own: the subscription finds the person's agent anyway.
func TestTheFeedIsFollowedThroughASubscriptionWithNoTransaction(t *testing.T) {
	world := newFeedWorld(t)
	principal := &api.Principal{User: world.person, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	ctx, cancel := context.WithCancel(api.ContextWithPrincipal(context.Background(), principal))
	defer cancel()
	changes, err := world.resolver.AgentFeedChanged(ctx)
	if err != nil {
		t.Fatalf("AgentFeedChanged: %s", err)
	}
	if changes == nil {
		t.Fatal("the subscription carries nothing")
	}
}
