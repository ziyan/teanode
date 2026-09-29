package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A theme read from the terminal shows the night's reflections under
// their own heading, with their kind and what they cite, and not among
// the facts; the dream log counts themes and reflections.
func TestReflectionsAreShownApartWithTheirCitations(test *testing.T) {
	test.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(request.Body)
		switch {
		case strings.Contains(string(body), "AgentGraphPage"):
			_, _ = response.Write([]byte(`{"data":{"AgentGraphPage":{"node":{"id":"n1","path":"themes/allotments","kind":"topic","name":"Allotments",` +
				`"aliases":[],"summary":"Two plots by the river.","modifiedAt":"2030-01-02T10:00:00Z"},` +
				`"facts":[{"id":"f1","number":1,"kind":"fact","text":"The plots share a water butt.","evidence":[],"audiences":[],"createdAt":"2030-01-01T10:00:00Z"},` +
				`{"id":"f2","number":2,"kind":"reflection","text":"Both plots flood every spring.","inferred":true,"evidence":[` +
				`{"kind":"dream","id":"","quote":"reflection: pattern"},{"kind":"memory","id":"a","quote":"places/plot-east#3"},` +
				`{"kind":"memory","id":"b","quote":"places/plot-west"}],"audiences":[],"createdAt":"2030-01-02T10:00:00Z"}],` +
				`"edges":[],"children":[],"contact":null}}}`))
		case strings.Contains(string(body), "ListAgentDreams"):
			_, _ = response.Write([]byte(`{"data":{"ListAgentDreams":[{"id":"d1","startedAt":"2030-01-02T02:00:00Z","finishedAt":"2030-01-02T02:30:00Z",` +
				`"themesMade":3,"themesUpdated":1,"reflectionsWritten":4,"proposals":[]}]}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)

	page := runAgainst(test, server, "memory", "get", "themes/allotments")
	heading := strings.Index(page, "Reflections:")
	reflection := strings.Index(page, "#2 Both plots flood every spring.  (pattern)\n   citing places/plot-east#3, places/plot-west")
	fact := strings.Index(page, "#1 The plots share a water butt.")
	if heading < 0 || reflection < heading || fact < reflection {
		test.Errorf("the reflections are under their own heading, apart from the facts:\n%s", page)
	}
	if strings.Count(page, "Both plots flood") != 1 {
		test.Errorf("the reflection is shown twice:\n%s", page)
	}

	dreamLog := runAgainst(test, server, "dream", "log")
	for _, count := range []string{"3 themes made", "1 themes updated", "4 reflections written"} {
		if !strings.Contains(dreamLog, count) {
			test.Errorf("the dream log does not say %q:\n%s", count, dreamLog)
		}
	}
}
