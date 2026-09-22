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

func TestCalendarRemoveResolvesLostResponseBeforeLoadingDeletedCalendar(test *testing.T) {
	var mutationCount atomic.Int32
	var calendarReadCount atomic.Int32
	var retainedIdentity atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "GetCalendarRequest"):
			requestId, _ := retainedIdentity.Load().(string)
			if document.Variables["requestId"] != requestId {
				response.WriteHeader(400)
				return
			}
			_, _ = response.Write([]byte(`{"data":{"GetCalendarRequest":{"requestId":"` + requestId + `","calendarId":"calendar-fixture","objectId":"event-fixture","operation":"delete","completedAt":"2030-01-02T10:00:00Z","isMissing":true}}}`))
		case strings.Contains(document.Query, "ListCalendars"):
			calendarReadCount.Add(1)
			_, _ = response.Write([]byte(`{"data":{"ListCalendars":[{"id":"calendar-fixture","timezone":"UTC"}]}}`))
		case strings.Contains(document.Query, "DeleteCalendarEvent"):
			requestId, _ := document.Variables["requestId"].(string)
			retainedIdentity.Store(requestId)
			if requestId == "" || document.Variables["id"] != "event-fixture" {
				response.WriteHeader(400)
				return
			}
			mutationCount.Add(1)
			response.WriteHeader(500)
		default:
			response.WriteHeader(400)
		}
	}))
	defer server.Close()
	invoke := func(retainedId string) error {
		command := &cli.Command{Name: "fixture", Flags: []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}}, Commands: []*cli.Command{NewCalendarCommand()}}
		arguments := []string{"fixture", "calendar", "remove", "event-fixture", "--force"}
		if retainedId != "" {
			arguments = append(arguments, "--request-id", retainedId)
		}
		return command.Run(test.Context(), arguments)
	}
	err := invoke("")
	requestId, _ := retainedIdentity.Load().(string)
	if err == nil || requestId == "" || !strings.Contains(err.Error(), requestId) {
		test.Fatalf("lost response=%v, request=%q", err, requestId)
	}
	if mutationCount.Load() != 1 {
		test.Fatalf("mutations=%d", mutationCount.Load())
	}
	if err := invoke(requestId); err != nil {
		test.Fatal(err)
	}
	if mutationCount.Load() != 1 || calendarReadCount.Load() != 1 {
		test.Fatalf("retry mutations=%d calendar reads=%d", mutationCount.Load(), calendarReadCount.Load())
	}
}
