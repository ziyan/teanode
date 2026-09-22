package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestCalendarSaveRetainsAnIdentifierForExplicitRetries(test *testing.T) {
	for _, suppliedId := range []string{"", "fixture-request"} {
		test.Run("identifier-"+suppliedId, func(test *testing.T) {
			requests := make(chan map[string]any, 2)
			var saveCount atomic.Int32
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
				if strings.Contains(document.Query, "ListCalendars") {
					_, _ = response.Write([]byte(`{"data":{"ListCalendars":[{"id":"calendar-fixture","timezone":"UTC"}]}}`))
					return
				}
				if !strings.Contains(document.Query, "requestId: $requestId") {
					response.WriteHeader(400)
					return
				}
				requests <- document.Variables
				if saveCount.Add(1) == 1 {
					response.WriteHeader(500)
					return
				}
				_, _ = response.Write([]byte(`{"data":{"SaveCalendarEvent":{"id":"accepted-event","summary":"Fixture"}}}`))
			}))
			defer server.Close()
			invoke := func(requestId string) error {
				command := &cli.Command{Name: "fixture", Flags: []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}}, Commands: []*cli.Command{NewCalendarCommand()}}
				arguments := []string{"fixture", "calendar", "add", "--summary", "Fixture", "--starts", "2030-01-02T10:00:00Z"}
				if requestId != "" {
					arguments = append(arguments, "--request-id", requestId)
				}
				return command.Run(test.Context(), arguments)
			}
			err := invoke(suppliedId)
			if err == nil {
				test.Fatal("expected lost response")
			}
			if saveCount.Load() != 1 {
				test.Fatalf("request did not reach the server: %v", err)
			}
			first := <-requests
			requestId, _ := first["requestId"].(string)
			if requestId == "" || !strings.Contains(err.Error(), requestId) || (suppliedId != "" && requestId != suppliedId) {
				test.Fatalf("retry identifier=%q, error=%v", requestId, err)
			}
			if err := invoke(requestId); err != nil {
				test.Fatal(err)
			}
			if saveCount.Load() != 2 {
				test.Fatal("retry did not reach the server")
			}
			second := <-requests
			if !reflect.DeepEqual(first, second) || saveCount.Load() != 2 {
				test.Fatalf("retry changed request: %+v, %+v", first, second)
			}
		})
	}
}

func TestCalendarRequestLookupNeedsNoCalendarOrEvent(test *testing.T) {
	var lookupCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		if !strings.Contains(document.Query, "GetCalendarRequest") || strings.Contains(document.Query, "mutation") || document.Variables["requestId"] != "fixture-request" {
			response.WriteHeader(400)
			return
		}
		lookupCount.Add(1)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"GetCalendarRequest":{"requestId":"fixture-request","calendarId":"deleted-calendar","objectId":"deleted-event","operation":"save","completedAt":"2030-01-02T10:00:00Z","isMissing":true}}}`))
	}))
	defer server.Close()
	command := &cli.Command{Name: "fixture", Flags: []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}}, Commands: []*cli.Command{NewCalendarCommand()}}
	if err := command.Run(test.Context(), []string{"fixture", "calendar", "request", "fixture-request", "--json"}); err != nil {
		test.Fatal(err)
	}
	if lookupCount.Load() != 1 {
		test.Fatalf("lookups=%d", lookupCount.Load())
	}
}
