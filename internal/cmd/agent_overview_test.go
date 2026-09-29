package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// overviewServer answers the documents the overview commands send, and
// keeps the path each rewrite was asked for.
func overviewServer(test *testing.T) (*httptest.Server, func() []string) {
	test.Helper()
	var mutex sync.Mutex
	var rewritten []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "RewriteAgentOverview"):
			mutex.Lock()
			rewritten = append(rewritten, document.Variables["path"].(string))
			mutex.Unlock()
			_, _ = response.Write([]byte(`{"data":{"RewriteAgentOverview":true}}`))
		case strings.Contains(document.Query, "AgentGraphPage"):
			if !strings.Contains(document.Query, "overview") {
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = response.Write([]byte(`{"data":{"AgentGraphPage":{"node":{"id":"n1","path":"projects/example-app","kind":"project","name":"example-app",` +
				`"aliases":[],"summary":"A planting calendar made from seed catalogues.","modifiedAt":"2030-01-02T10:00:00Z",` +
				`"overview":"## What it is\n\nAn app that reads seed catalogues.","overviewWrittenAt":"2030-01-02T10:00:00Z"},` +
				`"facts":[{"id":"f1","number":1,"kind":"fact","text":"Written in Go.","evidence":[],"audiences":[],"createdAt":"2030-01-01T10:00:00Z"}],` +
				`"edges":[],"children":[],"contact":null}}}`))
		case strings.Contains(document.Query, "ListAgentDreams"):
			_, _ = response.Write([]byte(`{"data":{"ListAgentDreams":[{"id":"d1","startedAt":"2030-01-02T02:00:00Z","finishedAt":"2030-01-02T02:30:00Z",` +
				`"rewritten":2,"overviewsWritten":7,"proposals":[]}]}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)
	return server, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), rewritten...)
	}
}

// runAgainst runs the agent command against a server and says what it
// printed.
func runAgainst(test *testing.T, server *httptest.Server, arguments ...string) string {
	test.Helper()
	var written bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &written,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewAgentCommand()},
	}
	if err := command.Run(test.Context(), append([]string{"fixture", "agent"}, arguments...)); err != nil {
		test.Fatalf("%v: %s", arguments, err)
	}
	return written.String()
}

// A page read from the terminal shows its overview under its opening; the
// overview command shows it alone, or asks for it to be written again; and
// the dream log counts the overviews a night wrote.
func TestTheOverviewIsShownAndCanBeAskedForAgain(test *testing.T) {
	test.Parallel()
	server, rewritten := overviewServer(test)

	page := runAgainst(test, server, "memory", "get", "projects/example-app")
	opening := strings.Index(page, "A planting calendar made from seed catalogues.")
	overview := strings.Index(page, "An app that reads seed catalogues.")
	facts := strings.Index(page, "#1 Written in Go.")
	if opening < 0 || overview < opening || facts < overview {
		test.Errorf("the overview is under the opening and above the facts:\n%s", page)
	}
	if !strings.Contains(page, "Overview, written ") {
		test.Errorf("the overview says when it was written:\n%s", page)
	}

	alone := runAgainst(test, server, "memory", "overview", "projects/example-app")
	if !strings.Contains(alone, "## What it is") || strings.Contains(alone, "Written in Go.") {
		test.Errorf("the overview command shows the overview and nothing else:\n%s", alone)
	}

	asked := runAgainst(test, server, "memory", "overview", "projects/example-app", "--rewrite")
	if got := rewritten(); len(got) != 1 || got[0] != "projects/example-app" {
		test.Errorf("--rewrite asks for that page to be written again: %v", got)
	}
	if !strings.Contains(asked, "next dream") {
		test.Errorf("--rewrite says when it happens:\n%s", asked)
	}

	dreamLog := runAgainst(test, server, "dream", "log")
	if !strings.Contains(dreamLog, "7 overviews written") {
		test.Errorf("the dream log counts the overviews written:\n%s", dreamLog)
	}
}
