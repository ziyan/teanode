package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/search"
)

// Search across everything is on the API, the command line and the agent's
// search tool under the same name and the same arguments: words, the areas to
// narrow it to and how many results from each. The dashboard and the iPhone
// app call the same operation.
func TestSearchParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	kind := reflect.TypeFor[apigraph.SearchQuery]()
	for index := 0; index < kind.NumMethod(); index++ {
		operations[kind.Method(index).Name] = true
	}
	if len(operations) != 1 || !operations["SearchEverything"] {
		test.Fatalf("the search operations are %v; this test lists SearchEverything only, so a new one needs a command and a tool action", operations)
	}

	command := NewSearchCommand()
	if command.Name != "search" {
		test.Errorf("the command is %q, want search", command.Name)
	}
	flags := map[string]bool{}
	for _, flag := range command.Flags {
		flags[flag.Names()[0]] = true
	}
	for _, name := range []string{"area", "limit", "json"} {
		if !flags[name] {
			test.Errorf("teanode search has no --%s", name)
		}
	}

	var tool *tools.Tool
	for _, each := range tools.Build().All() {
		if each.Name == "search" {
			tool = each
		}
	}
	if tool == nil {
		test.Fatal("the agent has no search tool")
	}
	if tool.Risk != tools.RiskRead {
		test.Errorf("the search tool is %q, want read: it changes nothing", tool.Risk)
	}
	properties, _ := tool.Parameters["properties"].(map[string]any)
	for _, name := range []string{"words", "areas", "per_area_limit"} {
		if properties[name] == nil {
			test.Errorf("the search tool has no argument %s", name)
		}
	}
	if len(properties) != 3 {
		test.Errorf("the search tool takes %d arguments, want words, areas and per_area_limit", len(properties))
	}
	areas, _ := properties["areas"].(map[string]any)
	items, _ := areas["items"].(map[string]any)
	if enum, _ := items["enum"].([]string); !reflect.DeepEqual(enum, search.Areas) {
		test.Errorf("the tool offers the areas %v, want %v", enum, search.Areas)
	}
	for _, area := range search.Areas {
		if searchAreaCommands[area] == "" {
			test.Errorf("teanode search does not say where to find more in %s", area)
		}
	}
}

// searchServer answers SearchEverything with a mail group that has more, a
// contacts group, and one area that did not answer, and records the variables.
func searchServer(test *testing.T) (*httptest.Server, func() map[string]any) {
	test.Helper()
	var lock sync.Mutex
	var variables map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil || !strings.Contains(document.Query, "SearchEverything") {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		lock.Lock()
		variables = document.Variables
		lock.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"SearchEverything":{"words":"harbor","failedAreas":["photos"],"areas":[
			{"searchArea":"mail","hasMore":true,"results":[
				{"searchArea":"mail","targetId":"item-1","titleText":"Harbor schedule","detailText":"Ada Example","happenedAt":"2030-03-01T09:30:00Z"}]},
			{"searchArea":"contacts","hasMore":false,"results":[
				{"searchArea":"contacts","targetId":"contact-1","titleText":"Harbor Office","detailText":"","happenedAt":null}]}]}}}`))
	}))
	test.Cleanup(server.Close)
	return server, func() map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return variables
	}
}

func runSearchAgainst(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewSearchCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "search"}, arguments...))
	})
	return output, err
}

// The words are every argument after search, the flags narrow the areas and
// the count, and the groups are printed in the order the server gave, with
// where to find more of a group that has more and the areas that did not
// answer named apart from the ones that found nothing.
func TestSearchPrintsAGroupForEachArea(test *testing.T) {
	server, recorded := searchServer(test)
	output, err := runSearchAgainst(test, server, "harbor", "office", "--area", "mail", "--area", "contacts", "--limit", "3")
	if err != nil {
		test.Fatal(err)
	}
	variables := recorded()
	if variables["words"] != "harbor office" || variables["perAreaLimit"] != float64(3) || !reflect.DeepEqual(variables["areas"], []any{"mail", "contacts"}) {
		test.Errorf("the server was asked with %v", variables)
	}
	for _, wanted := range []string{"mail\n", "item-1", "Harbor schedule", "Ada Example", `more in mail: teanode mailbox message list --everywhere --search "harbor"`, "contacts\n", "contact-1", "no answer from: photos"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the output has no %q:\n%s", wanted, output)
		}
	}
	if strings.Contains(output, "more in contacts") {
		test.Errorf("a group with nothing more said there was more:\n%s", output)
	}
	if strings.Index(output, "mail\n") > strings.Index(output, "contacts\n") {
		test.Errorf("the groups are not in the server's order:\n%s", output)
	}
}

// With --json it prints what the server answered, and nothing else.
func TestSearchWithJSONPrintsTheResults(test *testing.T) {
	server, _ := searchServer(test)
	output, err := runSearchAgainst(test, server, "harbor", "--json")
	if err != nil {
		test.Fatal(err)
	}
	var results struct {
		Words       string   `json:"words"`
		FailedAreas []string `json:"failedAreas"`
		Areas       []struct {
			SearchArea string `json:"searchArea"`
			HasMore    bool   `json:"hasMore"`
		} `json:"areas"`
	}
	if err := json.Unmarshal([]byte(output), &results); err != nil || results.Words != "harbor" || len(results.Areas) != 2 || !results.Areas[0].HasMore || len(results.FailedAreas) != 1 {
		test.Errorf("it printed %q: %v", output, err)
	}
}

// Nothing to look for and an area that is not one are refused before the
// server is asked.
func TestSearchRefusesNoWordsAndAnUnknownArea(test *testing.T) {
	server, recorded := searchServer(test)
	if _, err := runSearchAgainst(test, server); err == nil || !strings.Contains(err.Error(), "what should I look for") {
		test.Errorf("no words were not refused: %v", err)
	}
	if _, err := runSearchAgainst(test, server, "harbor", "--area", "emails"); err == nil || !strings.Contains(err.Error(), `"emails" is not an area`) {
		test.Errorf("an unknown area was not refused: %v", err)
	}
	if recorded() != nil {
		test.Error("the server was asked")
	}
}
