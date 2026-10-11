package apigraph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// setupProfileTestSecret stands in for the server secret. Invented; it seals
// nothing outside these tests.
var setupProfileTestSecret = []byte("an invented server secret for setup profile tests")

// downloadSetupProfile fetches a setup profile by the path the resolver gave.
func downloadSetupProfile(world *graph, path string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.Path(api.PathMailboxSetupProfile).Methods(http.MethodGet).HandlerFunc(world.setupProfileView)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func setupProfilePath(token string) string {
	return strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1)
}

// A setup profile holds a password, so its address works once, and the
// download refuses whatever else is parked in the same store.
func TestASetupProfileDownloadsOnceAndOnlyAsAProfile(test *testing.T) {
	ctx := context.Background()
	world := &graph{ceremonies: ceremony.NewMemoryStore(), settings: &api.Settings{Secret: setupProfileTestSecret}}
	box, err := world.setupProfileBox()
	if err != nil {
		test.Fatal(err)
	}
	sealed, err := box.Seal([]byte("<plist/>"))
	if err != nil {
		test.Fatal(err)
	}
	token, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
	if err != nil {
		test.Fatal(err)
	}

	first := downloadSetupProfile(world, setupProfilePath(token))
	if first.Code != http.StatusOK || first.Body.String() != "<plist/>" {
		test.Fatalf("the first download must work: %d %q", first.Code, first.Body.String())
	}
	if contentType := first.Header().Get("Content-Type"); contentType != "application/x-apple-aspen-config" {
		test.Fatalf("a device installs only a profile served as one, not %q", contentType)
	}
	if first.Header().Get("Cache-Control") != "no-store" {
		test.Fatal("a profile with a password must not be cached")
	}
	if again := downloadSetupProfile(world, setupProfilePath(token)); again.Code != http.StatusNotFound {
		test.Fatalf("a second download must fail, got %d", again.Code)
	}

	// A passkey challenge parked in the same store is not a profile.
	challenge, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{SessionData: "{}"})
	if err != nil {
		test.Fatal(err)
	}
	if response := downloadSetupProfile(world, setupProfilePath(challenge)); response.Code != http.StatusNotFound {
		test.Fatalf("a passkey challenge must not download as a profile, got %d", response.Code)
	}
	// Nor is a profile a passkey challenge.
	profile, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
	if err != nil {
		test.Fatal(err)
	}
	if _, err := world.take(ctx, profile); !errors.Is(err, api.ErrInvalidArguments) {
		test.Fatalf("a setup profile must not finish a passkey ceremony, got %v", err)
	}

	// One sealed under another server secret opens to nothing.
	other, err := secretbox.New([]byte("another invented server secret"), setupProfileLabel)
	if err != nil {
		test.Fatal(err)
	}
	foreign, err := other.Seal([]byte("<plist/>"))
	if err != nil {
		test.Fatal(err)
	}
	stranger, err := world.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: foreign})
	if err != nil {
		test.Fatal(err)
	}
	if response := downloadSetupProfile(world, setupProfilePath(stranger)); response.Code != http.StatusNotFound {
		test.Fatalf("a profile sealed under another secret must not download, got %d", response.Code)
	}
	if response := downloadSetupProfile(world, setupProfilePath("never-made")); response.Code != http.StatusNotFound {
		test.Fatalf("an unknown token must not download, got %d", response.Code)
	}
}
