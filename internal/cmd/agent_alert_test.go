package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// alertServer answers the alert and settings documents, and keeps what it
// was sent.
func alertServer(test *testing.T) (*httptest.Server, func() []map[string]any) {
	test.Helper()
	var mutex sync.Mutex
	var sent []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		sent = append(sent, document.Variables)
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "UnmuteAgentAlert"):
			_, _ = response.Write([]byte(`{"data":{"UnmuteAgentAlert":true}}`))
		case strings.Contains(document.Query, "MuteAgentAlert"):
			_, _ = response.Write([]byte(`{"data":{"MuteAgentAlert":{"id":"mute-1","muteScope":"domain","muteTarget":"shop.example.com","alertId":"alert-1","createdAt":"2026-09-29T10:00:00Z"}}}`))
		case strings.Contains(document.Query, "UpdateAgent"):
			_, _ = response.Write([]byte(`{"data":{"UpdateAgent":{"agent":{"id":"agent-1","name":"Agent","enabled":true,"isAlertsEnabled":false,"alertQuietStart":"21:00","alertDailyMost":3},"sources":[]}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)
	return server, func() []map[string]any {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]map[string]any(nil), sent...)
	}
}

// Muting from an alert, unmuting, and the settings keys each send what
// the dashboard would.
func TestTheAlertCommandsSendWhatTheDashboardWould(test *testing.T) {
	test.Parallel()
	server, sent := alertServer(test)

	if printed := runAgainst(test, server, "alert", "mute", "alert-1", "--scope", "domain"); !strings.Contains(printed, `muted domain "shop.example.com"`) {
		test.Errorf("mute printed %q", printed)
	}
	if printed := runAgainst(test, server, "alert", "unmute", "mute-1"); !strings.Contains(printed, "mute-1: unmuted") {
		test.Errorf("unmute printed %q", printed)
	}
	runAgainst(test, server, "settings", "set", "alerts=false", "alert-quiet-start=21:00", "alert-daily-most=3")

	variables := sent()
	if len(variables) != 3 {
		test.Fatalf("three documents: %v", variables)
	}
	if variables[0]["alertId"] != "alert-1" || variables[0]["muteScope"] != "domain" || variables[0]["muteTarget"] != nil {
		test.Errorf("mute sent %v", variables[0])
	}
	if variables[1]["muteId"] != "mute-1" {
		test.Errorf("unmute sent %v", variables[1])
	}
	if variables[2]["isAlertsEnabled"] != false || variables[2]["alertQuietStart"] != "21:00" || variables[2]["alertDailyMost"] != float64(3) {
		test.Errorf("settings sent %v", variables[2])
	}
}
