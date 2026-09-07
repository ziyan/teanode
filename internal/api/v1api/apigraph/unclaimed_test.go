package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// A server nobody has claimed used to answer anyone, on the reasoning that
// onboarding must be reachable by a caller who cannot log in. That opened
// every operation rather than the two onboarding needs, so an unclaimed server
// would list its domains, aliases and signing selectors to whoever found it
// first — and then let them claim it.
//
// Now every helper refuses a request that carries no principal, whatever the
// server's state.
func TestAnUnclaimedServerStillRefusesAnAnonymousCaller(t *testing.T) {
	t.Parallel()
	resolver := &graph{}
	ctx := context.Background()

	if _, err := resolver.requireSignedIn(ctx); !errors.Is(err, api.ErrNotLoggedIn) {
		t.Errorf("an anonymous caller was answered with %v, want %v", err, api.ErrNotLoggedIn)
	}
	if _, err := resolver.requirePermission(ctx, models.PermissionServerManage); !errors.Is(err, api.ErrNotLoggedIn) {
		t.Errorf("a permission check answered an anonymous caller with %v, want %v", err, api.ErrNotLoggedIn)
	}
	// The domain helpers are the ones a listing goes through, and they must
	// refuse for the same reason.
	if _, err := resolver.domainsToList(ctx, models.PermissionMailAudit, ""); !errors.Is(err, api.ErrNotLoggedIn) {
		t.Errorf("listing every domain answered an anonymous caller with %v, want %v", err, api.ErrNotLoggedIn)
	}
	if _, err := resolver.requireDomainPermission(ctx, models.PermissionDomainManage, "example.com"); !errors.Is(err, api.ErrNotLoggedIn) {
		t.Errorf("naming a domain answered an anonymous caller with %v, want %v", err, api.ErrNotLoggedIn)
	}
}

// A caller who is somebody but holds nothing is told not found, never
// forbidden: "you may not touch this" confirms the row exists.
func TestACallerWithoutThePermissionSeesNotFound(t *testing.T) {
	t.Parallel()
	resolver := &graph{}
	ctx := api.ContextWithPrincipal(context.Background(), &api.Principal{
		User:        &models.User{ID: "u1", Username: "member"},
		Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}}),
	})

	if _, err := resolver.requireSignedIn(ctx); err != nil {
		t.Errorf("an identified caller was refused: %s", err)
	}
	if _, err := resolver.requirePermission(ctx, models.PermissionServerManage); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("a missing permission answered %v, want %v", err, api.ErrNotFound)
	}
	if _, err := resolver.requireDomainPermission(ctx, models.PermissionDomainManage, "example.com"); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("a domain the caller may not touch answered %v, want %v", err, api.ErrNotFound)
	}
}

// The console — the command line run on the server itself — is not an
// account and may do everything: whoever can read the server secret holds the
// database anyway.
func TestTheConsoleMayDoEverything(t *testing.T) {
	t.Parallel()
	resolver := &graph{}
	principal, err := resolver.resolvePrincipal(nil, "console-user-name-is-irrelevant", nil)
	if err != nil || principal != nil {
		t.Fatalf("a username that is nobody resolved to %+v, %v", principal, err)
	}
	principal, err = resolver.resolvePrincipal(nil, localUsername, nil)
	if err != nil {
		t.Fatalf("the console did not resolve: %s", err)
	}
	if !principal.Console || !principal.Permissions.Has(models.PermissionRoleManage) {
		t.Errorf("the console holds %+v, want everything", principal.Permissions)
	}
	ctx := api.ContextWithPrincipal(context.Background(), principal)
	if _, err := resolver.requirePermission(ctx, models.PermissionServerManage); err != nil {
		t.Errorf("the console was refused: %s", err)
	}
}
