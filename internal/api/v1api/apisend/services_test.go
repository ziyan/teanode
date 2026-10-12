package apisend

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

func TestDisabledMailRoutingRefusesTemplatedSendBeforeAccessingDependencies(test *testing.T) {
	for _, isSelected := range []bool{false, true} {
		configuration := config.Default()
		configuration.Services = config.AssistantServices()
		configuration.Services.MailRouting.IsEnabled = new(isSelected)
		store := config.NewMemoryStore(configuration)
		defer func() { _ = store.Close() }()
		runningServices := config.AssistantServices()
		current := &send{config: store, settings: &api.Settings{RunningServices: &runningServices}}
		response := httptest.NewRecorder()
		current.sendView(response, httptest.NewRequest(http.MethodPost, "/", nil))
		if response.Code != http.StatusServiceUnavailable {
			test.Fatalf("disabled send returned %d", response.Code)
		}
	}
}
