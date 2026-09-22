package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestCalendarStopUsesOnlyRetainedIdentity(test *testing.T) {
	for _, completion := range []string{`null`, `{"requestId":"fixture","calendarId":"calendar","objectId":"event","operation":"save","completedAt":"2030-01-02T10:00:00Z","isMissing":false}`} {
		test.Run(completion, func(test *testing.T) {
			var cancellationCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				var document struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
					response.WriteHeader(400)
					return
				}
				if !strings.Contains(document.Query, "CancelCalendarRequest") || document.Variables["requestId"] != "fixture" {
					response.WriteHeader(400)
					return
				}
				cancellationCount.Add(1)
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":{"CancelCalendarRequest":` + completion + `}}`))
			}))
			defer server.Close()
			command := &cli.Command{Name: "fixture", Flags: []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}}, Commands: []*cli.Command{NewCalendarCommand()}}
			if err := command.Run(test.Context(), []string{"fixture", "calendar", "stop", "fixture", "--json"}); err != nil {
				test.Fatal(err)
			}
			if cancellationCount.Load() != 1 {
				test.Fatalf("cancellations=%d", cancellationCount.Load())
			}
		})
	}
}
