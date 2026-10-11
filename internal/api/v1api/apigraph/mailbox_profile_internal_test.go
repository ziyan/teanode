package apigraph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
)

// A setup profile holds a password, so its address works once, and only
// for ten minutes.
func TestASetupProfileDownloadsOnceAndExpires(test *testing.T) {
	world := &graph{}
	router := mux.NewRouter()
	router.Path(api.PathMailboxSetupProfile).Methods(http.MethodGet).HandlerFunc(world.setupProfileView)
	download := func(token string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1), nil))
		return response
	}

	token, expiresAt := world.setupProfiles.put([]byte("<plist/>"), time.Now())
	if len(token) != 32 || time.Until(expiresAt) > setupProfileLifetime {
		test.Fatalf("token %q expires %s", token, expiresAt)
	}
	first := download(token)
	if first.Code != http.StatusOK || first.Body.String() != "<plist/>" {
		test.Fatalf("the first download must work: %d %q", first.Code, first.Body.String())
	}
	if contentType := first.Header().Get("Content-Type"); contentType != "application/x-apple-aspen-config" {
		test.Fatalf("a device installs only a profile served as one, not %q", contentType)
	}
	if first.Header().Get("Cache-Control") != "no-store" {
		test.Fatal("a profile with a password must not be cached")
	}
	if again := download(token); again.Code != http.StatusNotFound {
		test.Fatalf("a second download must fail, got %d", again.Code)
	}

	late, _ := world.setupProfiles.put([]byte("<plist/>"), time.Now().Add(-setupProfileLifetime-time.Second))
	if response := download(late); response.Code != http.StatusNotFound {
		test.Fatalf("an expired profile must not download, got %d", response.Code)
	}
	if response := download("never-made"); response.Code != http.StatusNotFound {
		test.Fatalf("an unknown token must not download, got %d", response.Code)
	}
}
