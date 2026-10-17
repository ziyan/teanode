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
)

// Sharing a page of the agent's memory is on the API, the command line and
// the agent's memory tool under the same names. The dashboard and the iPhone
// app call the same operations.
func TestPageSharingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.AgentPageShareQuery](), reflect.TypeFor[apigraph.AgentPageShareMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// The operations, then the subcommand of teanode agent memory that does
	// each and the action of the memory tool.
	table := map[string]struct {
		subcommand string
		action     string
	}{
		"ListAgentPageShares":   {"shares", "shares"},
		"ShareAgentPage":        {"share", "share"},
		"UnshareAgentPage":      {"unshare", "unshare"},
		"ListSharedAgentPages":  {"shared", "shared"},
		"PreviewAgentPageShare": {"share", ""},
		"ReadSharedAgentPage":   {"get", "get"},
	}
	for operation := range operations {
		if _, listed := table[operation]; !listed {
			test.Errorf("the operation %s is not in the parity table", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range newAgentMemoryCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	actions := map[string]bool{}
	var properties map[string]any
	for _, tool := range tools.Build().All() {
		if tool.Name != "memory" {
			continue
		}
		properties, _ = tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if len(actions) == 0 {
		test.Fatal("the agent has no memory tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if commands[names.subcommand] == nil {
			test.Errorf("teanode agent memory has no %s", names.subcommand)
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the memory tool has no action %s", names.action)
		}
	}
	hasFlag := func(command *cli.Command, name string) bool {
		for _, flag := range command.Flags {
			for _, each := range flag.Names() {
				if each == name {
					return true
				}
			}
		}
		return false
	}
	if !hasFlag(commands["get"], "owner") {
		test.Error("teanode agent memory get has no --owner")
	}
	if !hasFlag(commands["share"], "with-quotes") || !hasFlag(commands["share"], "preview") {
		test.Error("teanode agent memory share lost --with-quotes or --preview")
	}
	if properties["owner"] == nil || properties["with"] == nil || properties["is_quote_shared"] == nil || properties["share_id"] == nil {
		test.Error("the memory tool lost owner, with, is_quote_shared or share_id")
	}
}

// pageSharingServer answers the documents the page-sharing commands send with
// a small world: ben and cora share a circle with the caller, and ada, who is
// not the caller, shared a page.
func pageSharingServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	const sharedPage = `{"ownerUserId":"u-ada","ownerUsername":"ada","ownerName":"Ada Example","path":"people/a-grandparent","name":"A Grandparent","nodeKind":"person",
		"aliases":["grandma"],"opening":"Lives by the sea.","isQuoteShared":false,
		"facts":[{"number":3,"factKind":"fact","text":"Next appointment is Tuesday at three.","isInferred":false,"evidenceSource":"from Ada Example's mail","evidenceQuote":""}],
		"edges":[],"children":[{"path":"people/a-grandparent/doctor","name":"The doctor","nodeKind":"person","opening":""}]}`
	answers := map[string]string{
		"ListCirclePeople": `[{"userId":"u-ben","username":"ben","name":"Ben"},{"userId":"u-cora","username":"cora","name":"Cora"}]`,
		"ListCircles":      `[{"id":"c1","name":"Home","circleKind":"family","members":[],"pendingInvitations":[]}]`,
		"ShareAgentPage":   `{"id":"s1","path":"people/a","pageName":"A","granteeUserId":"u-ben","granteeUsername":"ben","isQuoteShared":true}`,
		"ListAgentPageShares": `[{"id":"s1","path":"people/a","pageName":"A","granteeUserId":"u-ben","granteeUsername":"ben","isQuoteShared":true},
			{"id":"s2","path":"people/b","pageName":"B","granteeCircleId":"c1","granteeCircleName":"Home","isQuoteShared":false}]`,
		"UnshareAgentPage":      `true`,
		"PreviewAgentPageShare": `{"page":` + sharedPage + `,"reachedPageCount":4,"isReachedPageCountCut":false}`,
		"ListSharedAgentPages":  `[{"ownerUserId":"u-ada","ownerUsername":"ada","ownerName":"Ada Example","path":"people/a-grandparent","name":"A Grandparent","opening":"Lives by the sea."}]`,
		"ReadSharedAgentPage":   sharedPage,
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		data := map[string]json.RawMessage{}
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation+"(") || strings.Contains(document.Query, operation+" ") || strings.Contains(document.Query, operation+"\n") {
				data[operation] = json.RawMessage(answer)
				lock.Lock()
				calls[operation] = document.Variables
				lock.Unlock()
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
	}))
	test.Cleanup(server.Close)
	return server, func(operation string) map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return calls[operation]
	}
}

func runMemoryCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{newAgentMemoryCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "memory"}, arguments...))
	})
	return output, err
}

// Sharing names a person by username or a circle by name, sends the
// identifier the server wants and whether the quotes go too.
func TestSharingAPageResolvesWhoItIsFor(test *testing.T) {
	server, called := pageSharingServer(test)

	output, err := runMemoryCommand(test, server, "share", "/People/A/", "ben", "--with-quotes")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ShareAgentPage")
	if variables["path"] != "people/a" || variables["granteeUserId"] != "u-ben" || variables["granteeCircleId"] != nil || variables["isQuoteShared"] != true {
		test.Errorf("a person is shared with by their account: %v", variables)
	}
	if !strings.Contains(output, "people/a") || !strings.Contains(output, "ben") || !strings.Contains(output, "with the quotes") {
		test.Errorf("it says what it did: %q", output)
	}

	if _, err = runMemoryCommand(test, server, "share", "people/a", "Home"); err != nil {
		test.Fatal(err)
	}
	if variables = called("ShareAgentPage"); variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil || variables["isQuoteShared"] != false {
		test.Errorf("a circle is shared with by name, without quotes unless asked: %v", variables)
	}

	// Somebody the caller has no circle with, and a missing argument, are
	// refused here, in words, before anything is sent.
	if _, err = runMemoryCommand(test, server, "share", "people/a", "stranger"); err == nil || !strings.Contains(err.Error(), "circle") {
		test.Errorf("a stranger: %v", err)
	}
	if _, err = runMemoryCommand(test, server, "share", "people/a"); err == nil {
		test.Errorf("no one to share with")
	}
}

// A preview sends nothing to be shared and says what would be seen and how
// many pages it reaches.
func TestPreviewingAShareSharesNothing(test *testing.T) {
	server, called := pageSharingServer(test)
	output, err := runMemoryCommand(test, server, "share", "people/a-grandparent", "ben", "--preview")
	if err != nil {
		test.Fatal(err)
	}
	if called("ShareAgentPage") != nil {
		test.Errorf("a preview shares: %v", called("ShareAgentPage"))
	}
	if variables := called("PreviewAgentPageShare"); variables["granteeUserId"] != "u-ben" || variables["isQuoteShared"] != false {
		test.Errorf("the preview: %v", variables)
	}
	for _, wanted := range []string{"ben would see this of people/a-grandparent", "#3 Next appointment is Tuesday at three.", "from Ada Example's mail", "reaches 4 pages"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the preview lacks %q:\n%s", wanted, output)
		}
	}
}

func TestSharesUnshareAndShared(test *testing.T) {
	server, called := pageSharingServer(test)

	output, err := runMemoryCommand(test, server, "shares")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"people/a", "ben", "yes", "s1", "everyone in Home", "s2"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shares lacks %q:\n%s", wanted, output)
		}
	}
	if _, err = runMemoryCommand(test, server, "unshare", "s2"); err != nil {
		test.Fatal(err)
	}
	if called("UnshareAgentPage")["shareId"] != "s2" {
		test.Errorf("unshare sends the share: %v", called("UnshareAgentPage"))
	}
	output, err = runMemoryCommand(test, server, "shared")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"ada", "people/a-grandparent", "A Grandparent", "Lives by the sea."} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shared lacks %q:\n%s", wanted, output)
		}
	}
}

// Reading a shared page names the owner, and says whose it is; somebody who
// shared nothing is answered in the same words as no account.
func TestGettingASharedPage(test *testing.T) {
	server, called := pageSharingServer(test)
	output, err := runMemoryCommand(test, server, "get", "people/a-grandparent", "--owner", "ADA")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("ReadSharedAgentPage"); variables["ownerUserId"] != "u-ada" || variables["path"] != "people/a-grandparent" {
		test.Errorf("what was asked: %v", variables)
	}
	for _, wanted := range []string{"people/a-grandparent - A Grandparent (person), shared by Ada Example, read only", "also called: grandma", "#3 Next appointment is Tuesday at three.", "under it: doctor"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the page lacks %q:\n%s", wanted, output)
		}
	}
	for _, owner := range []string{"cora", "nobody"} {
		if _, err := runMemoryCommand(test, server, "get", "people/a-grandparent", "--owner", owner); err == nil || !strings.Contains(err.Error(), `nobody called "`+owner+`" has shared pages with you`) {
			test.Errorf("%s shared nothing: %v", owner, err)
		}
	}
}
