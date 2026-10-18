package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureServer answers CaptureAgentCodingSession with isAsked and keeps
// the variables of the last call.
func captureServer(test *testing.T, isAsked bool) (*httptest.Server, map[string]any) {
	test.Helper()
	variables := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		raw, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(raw, &body)
		if !strings.Contains(body.Query, "CaptureAgentCodingSession(") {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		for key, value := range body.Variables {
			variables[key] = value
		}
		answer, _ := json.Marshal(map[string]any{"data": map[string]any{"CaptureAgentCodingSession": isAsked}})
		_, _ = response.Write(answer)
	}))
	test.Cleanup(server.Close)
	return server, variables
}

func TestCaptureReadsAComputersNewSessionsNow(test *testing.T) {
	server, variables := captureServer(test, true)
	output, _, err := runRecordCommand(test, server, "capture", "codex", "--computer", "desk")
	if err != nil {
		test.Fatal(err)
	}
	if variables["computerName"] != "desk" || variables["assistant"] != "codex" {
		test.Errorf("the computer and the assistant were asked for: %v", variables)
	}
	if !strings.Contains(output, "Reading the new codex sessions on desk again") {
		test.Errorf("it says the sessions are being read:\n%s", output)
	}
}

func TestCaptureSaysWhenNoSourceReadsTheSessions(test *testing.T) {
	server, _ := captureServer(test, false)
	output, _, err := runRecordCommand(test, server, "capture", "claude-code", "--computer", "desk")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, "Nothing: no enabled claude-code source reads sessions on desk") {
		test.Errorf("no source is said plainly:\n%s", output)
	}
}

func TestCaptureRefusesAnAssistantItDoesNotKnow(test *testing.T) {
	server, variables := captureServer(test, true)
	if _, _, err := runRecordCommand(test, server, "capture", "gemini", "--computer", "desk"); err == nil {
		test.Fatal("an unknown assistant is refused")
	}
	if len(variables) != 0 {
		test.Errorf("the server was not asked: %v", variables)
	}
}
