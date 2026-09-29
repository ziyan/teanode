package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The survey command sends the question and the scope, and prints the
// report and the runs it made.
func TestTheSurveyCommandPrintsTheReport(test *testing.T) {
	var mutex sync.Mutex
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil || !strings.Contains(document.Query, "SurveyAgentMemory") {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		asked = append(asked, document.Variables)
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"SurveyAgentMemory":{"report":"## Strengths\n\nThe orchards are pruned (projects/orchard-north#1).\n\n---\n\nCovered: ` +
			"`projects/orchard-north`" + `.","coveredPaths":["projects/orchard-north"],"failedPaths":[],"runIds":["run-1","run-2"]}}}`))
	}))
	test.Cleanup(server.Close)

	printed := runAgainst(test, server, "survey", "--scope", "themes/orchard-work", "what are the strengths of the orchards?")
	if !strings.HasPrefix(printed, "## Strengths") || !strings.Contains(printed, "Covered: `projects/orchard-north`.") ||
		!strings.Contains(printed, "'teanode agent run show': run-1, run-2") {
		test.Errorf("the survey printed %q", printed)
	}
	runAgainst(test, server, "survey", "what", "is", "everything?")
	mutex.Lock()
	defer mutex.Unlock()
	if len(asked) != 2 || asked[0]["scopePath"] != "themes/orchard-work" || asked[0]["question"] != "what are the strengths of the orchards?" {
		test.Fatalf("the survey sent %v", asked)
	}
	if _, isSent := asked[1]["scopePath"]; isSent || asked[1]["question"] != "what is everything?" {
		test.Errorf("a survey of everything sent %v", asked[1])
	}
}
