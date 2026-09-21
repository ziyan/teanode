package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mcp"
	"github.com/ziyan/teanode/internal/models"
)

// mcpEndpoint is a server with two people on it, each with an agent, and
// the handler under test. One of them may only talk to their agent; the
// other may read their mail as well, which is what shows that the catalog
// a harness gets is the person's and not a fixed list.
func mcpEndpoint(test *testing.T) (*graph, *models.User, *models.User) {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)

	var person, reader *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "harness", Name: "Robin Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		grantAgentUse(test, tx, person)
		if _, err = tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"}); err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if reader, err = tx.CreateUser(&models.User{Username: "reader", Name: "Sam Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		grantMCPPermissions(test, tx, reader, models.PermissionAgentUse, models.PermissionMailRead)
		if _, err = tx.CreateAgent(&models.Agent{UserID: reader.ID, Enabled: true, Name: "Robin"}); err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
	})

	configuration := config.Default()
	worker := agentpackage.New(&agentpackage.Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	return &graph{database: database, settings: &api.Settings{Agent: worker}}, person, reader
}

// grantMCPPermissions gives a person permissions the way a deployment
// does: a role in a group they are in.
func grantMCPPermissions(test *testing.T, tx db.Transaction, person *models.User, permissions ...models.Permission) {
	test.Helper()
	role, err := tx.CreateRole(&models.Role{Name: "Role " + person.Username, Permissions: permissions})
	if err != nil {
		test.Fatalf("CreateRole: %s", err)
	}
	if _, err := tx.CreateGroup(&models.Group{
		Name: "Group " + person.Username, UserIDs: []string{person.ID}, RoleIDs: []string{role.ID},
	}); err != nil {
		test.Fatalf("CreateGroup: %s", err)
	}
}

// post sends one JSON-RPC message, as username when it is not empty.
func (self *graph) postMCP(test *testing.T, username, body string) (int, *mcp.Response) {
	test.Helper()
	request := httptest.NewRequest(http.MethodPost, api.PathAgentMCP, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if username != "" {
		request.Header.Set(api.AuthenticatedUsernameHeader, username)
	}
	recorder := httptest.NewRecorder()
	self.mcpView(recorder, request)
	if recorder.Code != http.StatusOK {
		return recorder.Code, nil
	}
	var answer mcp.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		test.Fatalf("the answer is not a JSON-RPC message: %s (%s)", err, recorder.Body.String())
	}
	return recorder.Code, &answer
}

const mcpHandshake = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
	`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"a harness","version":"1"}}}`

// The middleware turns away a request with no credential before it reaches
// here, so what this proves is the second lock: a request that arrives
// having authenticated as nobody -- the server's own console, which is not
// an account -- is refused rather than answered with somebody's tools.
func TestTheProtocolNeedsAnAccountAndNotOnlyACredential(test *testing.T) {
	endpoint, _, _ := mcpEndpoint(test)

	if status, _ := endpoint.postMCP(test, "", mcpHandshake); status != http.StatusUnauthorized {
		test.Fatalf("a request with nobody signed in was answered %d", status)
	}
	if status, _ := endpoint.postMCP(test, config.LocalUsername, mcpHandshake); status != http.StatusUnauthorized {
		test.Fatalf("the console was answered %d", status)
	}
	if status, _ := endpoint.postMCP(test, "nobody-by-that-name", mcpHandshake); status != http.StatusUnauthorized {
		test.Fatalf("an unknown account was answered %d", status)
	}
}

func TestAHarnessCanShakeHandsAndListThePersonsTools(test *testing.T) {
	endpoint, person, reader := mcpEndpoint(test)

	status, answer := endpoint.postMCP(test, person.Username, mcpHandshake)
	if status != http.StatusOK || answer.Error != nil {
		test.Fatalf("the handshake was answered %d: %+v", status, answer.Error)
	}
	var shook struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(answer.Result, &shook); err != nil {
		test.Fatalf("reading the handshake: %s", err)
	}
	if shook.ProtocolVersion != mcp.ProtocolVersion {
		test.Fatalf("the server answered protocol %q", shook.ProtocolVersion)
	}

	status, answer = endpoint.postMCP(test, person.Username, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if status != http.StatusOK || answer.Error != nil {
		test.Fatalf("tools/list was answered %d: %+v", status, answer.Error)
	}
	var listed struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(answer.Result, &listed); err != nil {
		test.Fatalf("reading the catalog: %s", err)
	}
	if len(listed.Tools) < 10 {
		test.Fatalf("the catalog came back with %d tools in it", len(listed.Tools))
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
		if tool.Description == "" {
			test.Fatalf("%s has no description, so nothing on the other end knows what it is for", tool.Name)
		}
		if tool.InputSchema == nil || tool.InputSchema["type"] != "object" {
			test.Fatalf("%s has no object schema: %+v", tool.Name, tool.InputSchema)
		}
	}
	// A tool anybody with an agent has, so the list is a catalog and not
	// an accident.
	if !names["datetime"] || !names["memory"] {
		test.Fatalf("the catalog has neither datetime nor memory in it: %d tools", len(listed.Tools))
	}
	// And a tool this person may not have, because reading mail is a
	// permission and they do not hold it. What a harness is offered is
	// what the person may do, not what the server can do.
	if names["mail_search"] {
		test.Fatal("mail_search was offered to somebody who may not read mail")
	}
	theirs := mcpToolNames(test, endpoint, reader.Username)
	if !theirs["mail_search"] {
		test.Fatal("mail_search was withheld from somebody who may read mail")
	}
	// The two that are deliberately left out, for the reasons in
	// internal/agent/direct.go: nobody is at this end to answer a
	// question, and a caller that lists once wants the list.
	if names["ask_user"] {
		test.Fatal("ask_user was offered to a caller with nobody to ask")
	}
	if names["tool_search"] {
		test.Fatal("tool_search was offered to a caller that already has the whole catalog")
	}
}

// mcpToolNames is the catalog one person is offered, by name.
func mcpToolNames(test *testing.T, endpoint *graph, username string) map[string]bool {
	test.Helper()
	status, answer := endpoint.postMCP(test, username, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if status != http.StatusOK || answer.Error != nil {
		test.Fatalf("tools/list for %s was answered %d: %+v", username, status, answer.Error)
	}
	var listed struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(answer.Result, &listed); err != nil {
		test.Fatalf("reading the catalog: %s", err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	return names
}

// A tool that runs, end to end, through the protocol: the call goes in as
// JSON-RPC and real work comes back. datetime is the one tool that needs
// nothing of the person's, so it proves the path rather than the data.
func TestAToolCallRunsAndAnswers(test *testing.T) {
	endpoint, person, _ := mcpEndpoint(test)

	status, answer := endpoint.postMCP(test, person.Username,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"datetime","arguments":{}}}`)
	if status != http.StatusOK || answer.Error != nil {
		test.Fatalf("the call was answered %d: %+v", status, answer.Error)
	}
	var result mcp.CallResult
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		test.Fatalf("reading the result: %s", err)
	}
	if result.IsError {
		test.Fatalf("the call failed: %s", result.Text())
	}
	if !strings.Contains(result.Text(), "weekday") {
		test.Fatalf("datetime answered %q", result.Text())
	}
}

// A tool that is not there is the tool failing, which the caller's model
// can read and work around, rather than the call failing, which tells it
// the server is broken.
func TestAMissingToolIsAnAnswerAndNotABrokenCall(test *testing.T) {
	endpoint, person, _ := mcpEndpoint(test)

	status, answer := endpoint.postMCP(test, person.Username,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nonesuch","arguments":{}}}`)
	if status != http.StatusOK || answer.Error != nil {
		test.Fatalf("answered %d: %+v", status, answer.Error)
	}
	var result mcp.CallResult
	if err := json.Unmarshal(answer.Result, &result); err != nil {
		test.Fatalf("reading the result: %s", err)
	}
	if !result.IsError || !strings.Contains(result.Text(), "nonesuch") {
		test.Fatalf("a missing tool came back as %+v", result)
	}
}

// Declaring this endpoint as one of the agent's own connected servers
// would hand the agent a namespaced copy of its own catalog, and a call
// into it would come straight back out here. The handshake is where that
// is caught, because the client says who it is there.
func TestThisServerRefusesToBeConnectedToItself(test *testing.T) {
	endpoint, person, _ := mcpEndpoint(test)

	status, answer := endpoint.postMCP(test, person.Username,
		`{"jsonrpc":"2.0","id":5,"method":"initialize","params":{"protocolVersion":"2025-03-26",`+
			`"capabilities":{},"clientInfo":{"name":"TeaNode","version":"0.41.3"}}}`)
	if status != http.StatusOK {
		test.Fatalf("answered %d", status)
	}
	if answer.Error == nil {
		test.Fatal("this server shook its own hand")
	}
	if !strings.Contains(answer.Error.Message, "itself") {
		test.Fatalf("refused with %q", answer.Error.Message)
	}

	// And a harness that merely has the word in its name is not caught by
	// it: the guard is the client saying it is this program, not a
	// substring.
	status, answer = endpoint.postMCP(test, person.Username,
		`{"jsonrpc":"2.0","id":6,"method":"initialize","params":{"protocolVersion":"2025-03-26",`+
			`"capabilities":{},"clientInfo":{"name":"teanode-cli-helper","version":"1"}}}`)
	if status != http.StatusOK || answer.Error != nil {
		test.Fatalf("a harness named after this one was refused: %d %+v", status, answer.Error)
	}
}

// A notification is told to the server, not asked of it. Answering one
// puts a message on the wire that nothing is waiting for.
func TestANotificationIsAcceptedWithNoAnswer(test *testing.T) {
	endpoint, person, _ := mcpEndpoint(test)

	request := httptest.NewRequest(http.MethodPost, api.PathAgentMCP,
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, person.Username)
	recorder := httptest.NewRecorder()
	endpoint.mcpView(recorder, request)

	if recorder.Code != http.StatusAccepted {
		test.Fatalf("a notification was answered %d with %q", recorder.Code, recorder.Body.String())
	}
	if strings.TrimSpace(recorder.Body.String()) != "" {
		test.Fatalf("a notification was answered with %q", recorder.Body.String())
	}
}
