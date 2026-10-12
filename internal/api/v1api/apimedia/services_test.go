package apimedia

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

func TestDisabledMailMediaUploadsDoNotStoreFiles(test *testing.T) {
	configuration := config.Default()
	configuration.Services = config.AssistantServices()
	store := config.NewMemoryStore(configuration)
	defer func() { _ = store.Close() }()
	component, err := New(nil, store, nil)
	if err != nil {
		test.Fatal(err)
	}
	router := mux.NewRouter()
	if err := component.AddRoutes(router); err != nil {
		test.Fatal(err)
	}
	for _, routePath := range []string{api.PathMediaUpload, api.PathBimiLogoUpload} {
		routePath = strings.ReplaceAll(routePath, "{domainId}", "test-domain")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, routePath, nil))
		if response.Code != http.StatusServiceUnavailable {
			test.Fatalf("%s: %d", routePath, response.Code)
		}
	}
}
