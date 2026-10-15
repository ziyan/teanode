package server

import (
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// Reading the last price check is free; running one asks first, because it
// sends the billing key to the provider and can change prices that every
// budget and cost on the server is worked out from.
func TestRunningAPriceCheckAsksAndReadingOneDoesNot(t *testing.T) {
	t.Parallel()

	settings := tools.Build().Get("settings")
	if settings == nil {
		t.Fatal("the settings tool is not registered")
	}
	if got := settings.RiskFor(json.RawMessage(`{"action":"prices_show"}`)); got != tools.RiskRead {
		t.Errorf("showing the last check reads: %s", got)
	}
	if got := settings.RiskFor(json.RawMessage(`{"action":"prices_check"}`)); got != tools.RiskOutward {
		t.Errorf("running a check leaves the server and can change prices: %s", got)
	}
}
