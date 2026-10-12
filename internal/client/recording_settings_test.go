package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingSettingsTransport func(*http.Request) (*http.Response, error)

func (self recordingSettingsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

func TestRecordingSettingsAreReadWithoutBreakingOlderServers(test *testing.T) {
	for _, isUpdating := range []bool{false, true} {
		for _, hasRecordingLimits := range []bool{true, false} {
			transport := recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
				var body struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					test.Fatal(err)
				}
				responseJSON := `{"data":{"GetSettings":{"agent":{"models":{"default":"fixture:chat"},"limits":{"dailyTokensPerAgent":1200}}}}}`
				if strings.Contains(body.Query, "mutation") {
					responseJSON = strings.ReplaceAll(responseJSON, "GetSettings", "UpdateSettings")
				}
				if strings.Contains(body.Query, "maxRecordingMediaBytes") {
					if hasRecordingLimits {
						responseJSON = `{"data":{"GetSettings":{"agent":{"limits":{"maxRecordingMediaBytes":"3GB","maxRecordingAccountBytes":"60GB"}}}}}`
					} else {
						responseJSON = `{"errors":[{"message":"Cannot query field \"maxRecordingMediaBytes\" on type \"AgentLimitsSettings\"."}]}`
					}
				}
				if strings.Contains(body.Query, "models { transcription }") {
					if hasRecordingLimits {
						responseJSON = `{"data":{"GetSettings":{"agent":{"models":{"transcription":"fixture:audio"}}}}}`
					} else {
						responseJSON = `{"errors":[{"message":"Cannot query field \"transcription\" on type \"AgentModelsSettings\"."}]}`
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(responseJSON))}, nil
			})
			connection, err := New(Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				test.Fatal(err)
			}
			var settings Settings
			if isUpdating {
				settings, err = UpdateSettings(context.Background(), connection, map[string]any{"agent": map[string]any{"limits": map[string]any{"dailyTokensPerAgent": 1200}}})
			} else {
				settings, err = GetSettings(context.Background(), connection)
			}
			if err != nil {
				test.Fatal("older settings command stopped working", err)
			}
			var agentSettings struct {
				Limits map[string]any `json:"limits"`
				Models map[string]any `json:"models"`
			}
			if err := json.Unmarshal(settings["agent"], &agentSettings); err != nil {
				test.Fatal(err)
			}
			if agentSettings.Limits["dailyTokensPerAgent"] != float64(1200) {
				test.Fatal("additional settings replaced existing limits")
			}
			if agentSettings.Models["default"] != "fixture:chat" {
				test.Fatal("model settings replaced existing assignments")
			}
			if hasRecordingLimits && agentSettings.Models["transcription"] != "fixture:audio" {
				test.Fatal("CLI omitted transcription assignment")
			}
			if hasRecordingLimits && agentSettings.Limits["maxRecordingMediaBytes"] != "3GB" {
				test.Fatal("CLI omitted recording limits")
			}
		}
	}
}
