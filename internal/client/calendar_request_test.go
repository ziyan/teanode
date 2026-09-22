package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCalendarSaveRetainsLegacyWireAndDeletedReplay(test *testing.T) {
	for _, requestId := range []string{"", "retained-request"} {
		test.Run("request-"+requestId, func(test *testing.T) {
			queries := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				var document struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
					response.WriteHeader(400)
					return
				}
				queries <- document.Query
				if requestId != "" && document.Variables["requestId"] != requestId {
					response.WriteHeader(400)
					return
				}
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"data":{"SaveCalendarEvent":null}}`))
			}))
			defer server.Close()
			connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
			if err != nil {
				test.Fatal(err)
			}
			event, err := SaveCalendarEvent(test.Context(), connection, &SaveCalendarEventFields{RequestID: requestId, CalendarID: "fixture-calendar", Summary: new("Fixture")})
			if err != nil || event != nil {
				test.Fatalf("deleted replay=%+v, %v", event, err)
			}
			query := <-queries
			if strings.Contains(query, "requestId") != (requestId != "") {
				test.Fatalf("unexpected request API dependency: %s", query)
			}
		})
	}
}
