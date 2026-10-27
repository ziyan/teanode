package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// "user update --units" sends metric or us, and automatic sends the empty
// word that goes back to following the language and region; anything else is
// refused before the server is asked.
func TestUserUpdateUnits(test *testing.T) {
	for given, wanted := range map[string]string{"metric": "metric", "US": "us", "automatic": ""} {
		var sent map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			var document struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.NewDecoder(request.Body).Decode(&document)
			response.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(document.Query, "GetCurrentUser"):
				_, _ = response.Write([]byte(`{"data":{"GetCurrentUser":{"id":"u-robin","username":"robin"}}}`))
			case strings.Contains(document.Query, "UpdateUser"):
				sent = document.Variables
				_, _ = response.Write([]byte(`{"data":{"UpdateUser":{"id":"u-robin","username":"robin"}}}`))
			default:
				response.WriteHeader(http.StatusBadRequest)
			}
		}))
		command := &cli.Command{
			Name:     "fixture",
			Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
			Commands: []*cli.Command{NewUserCommand()},
		}
		var err error
		output := captureStandardOutput(test, func() {
			err = command.Run(test.Context(), []string{"fixture", "user", "update", "robin", "--units", given})
		})
		server.Close()
		if err != nil {
			test.Fatal(err)
		}
		if measurementSystem, isSent := sent["measurementSystem"]; !isSent || measurementSystem != wanted || sent["userId"] != "u-robin" {
			test.Errorf("--units %s sent %v", given, sent)
		}
		if output != "changed robin\n" {
			test.Errorf("it printed %q", output)
		}
	}

	for _, refused := range []string{"imperial", "celsius", ""} {
		if _, err := unitsFlag(refused); err == nil || !strings.Contains(err.Error(), "automatic, metric or us") {
			test.Errorf("%q was not refused: %v", refused, err)
		}
	}
}
