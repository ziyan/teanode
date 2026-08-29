package apigraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

// storeWithoutUsers returns a configuration store holding a server that nobody
// has claimed yet.
func storeWithoutUsers(t *testing.T) config.Store {
	t.Helper()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "teanode1.key"), []byte("not a real key"), 0o600); err != nil {
		t.Fatalf("failed to write the key file: %s", err)
	}

	configuration := config.Example()
	configuration.Server.DataDirectory = directory
	configuration.Users = nil

	store := config.NewMemoryStore(configuration)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// A server nobody has claimed used to answer anyone, on the reasoning that
// onboarding must be reachable by a caller who cannot log in. That opened
// every operation rather than the two onboarding needs, so an unclaimed server
// would list its domains, aliases and signing selectors to whoever found it
// first — and then let them claim it.
func TestAnUnclaimedServerStillRefusesAnAnonymousCaller(t *testing.T) {
	t.Parallel()
	resolver := &graph{config: storeWithoutUsers(t)}

	if got := len(resolver.config.Current().Users); got != 0 {
		t.Fatalf("the fixture has %d accounts; it is meant to have none", got)
	}

	if err := resolver.requireOperator(context.Background()); !errors.Is(err, api.ErrNotLoggedIn) {
		t.Errorf("an unclaimed server answered an anonymous caller with %v, want %v", err, api.ErrNotLoggedIn)
	}

	// The domain helpers are the ones a listing goes through, and they must
	// refuse for the same reason.
	if _, err := resolver.domainsToList(context.Background(), ""); !errors.Is(err, api.ErrNotLoggedIn) {
		t.Errorf("listing every domain on an unclaimed server returned %v, want %v", err, api.ErrNotLoggedIn)
	}
	configuration := resolver.config.Current()
	if len(configuration.Domains) > 0 {
		if _, err := resolver.requireDomain(context.Background(), configuration.Domains[0].ID); !errors.Is(err, api.ErrNotLoggedIn) {
			t.Errorf("naming a domain on an unclaimed server returned %v, want %v", err, api.ErrNotLoggedIn)
		}
	}
}

// Claiming the server has to keep working, or the fix above locks everybody
// out of a server that has nobody. GetSession and CreateFirstAccount never
// reach requireOperator; TestEveryOperationAuthorises is what holds them to
// that, and this checks the authenticated case still passes.
func TestAnIdentifiedCallerIsAllowed(t *testing.T) {
	t.Parallel()
	resolver := &graph{config: storeWithoutUsers(t)}

	ctx := api.ContextWithAuthenticatedUsername(context.Background(), "ziyan")
	if err := resolver.requireOperator(ctx); err != nil {
		t.Errorf("an identified caller was refused: %s", err)
	}
}
