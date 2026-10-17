package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// circleUserServer answers CreateUser and AddCircleMember, refusing the
// circles named in refused, and records the operations in the order they
// came.
func circleUserServer(test *testing.T, refused ...string) (*httptest.Server, func() []circleUserCall) {
	test.Helper()
	var mutex sync.Mutex
	var calls []circleUserCall
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "CreateUser"):
			calls = append(calls, circleUserCall{operation: "CreateUser", variables: document.Variables})
			_, _ = response.Write([]byte(`{"data":{"CreateUser":{"id":"user-fixture","username":"personx"}}}`))
		case strings.Contains(document.Query, "AddCircleMember"):
			calls = append(calls, circleUserCall{operation: "AddCircleMember", variables: document.Variables})
			for _, circleId := range refused {
				if document.Variables["circleId"] == circleId {
					_, _ = response.Write([]byte(`{"errors":[{"message":"not found"}],"data":null}`))
					return
				}
			}
			_, _ = response.Write([]byte(`{"data":{"AddCircleMember":{"id":"` + document.Variables["circleId"].(string) +
				`","name":"Fixture Home","circleKind":"family","createdAt":"2030-01-01T00:00:00Z","memberRole":"","members":[],"pendingInvitations":[]}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)
	return server, func() []circleUserCall {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]circleUserCall(nil), calls...)
	}
}

type circleUserCall struct {
	operation string
	variables map[string]any
}

// runUserCreateAgainst runs "teanode user create --stdin" with the password
// on standard input, which is replaced for the length of the call.
func runUserCreateAgainst(test *testing.T, server *httptest.Server, arguments ...string) error {
	test.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	if _, err := writer.WriteString("fixture-password\n"); err != nil {
		test.Fatal(err)
	}
	_ = writer.Close()
	original := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = original
		_ = reader.Close()
	}()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewUserCommand()},
	}
	return command.Run(test.Context(), append([]string{"fixture", "user", "create", "--stdin"}, arguments...))
}

// "user create --circle" makes the account and then puts it in each circle
// named, as a member, with the operation "circle add" uses: the same two
// operations the dashboard's circle page calls one after the other.
func TestUserCreatePutsTheNewAccountInEachCircleNamed(test *testing.T) {
	server, recorded := circleUserServer(test)

	if err := runUserCreateAgainst(test, server, "--circle", "circle-one", "--circle", "circle-two", "personx"); err != nil {
		test.Fatal(err)
	}

	calls := recorded()
	if len(calls) != 3 || calls[0].operation != "CreateUser" || calls[1].operation != "AddCircleMember" || calls[2].operation != "AddCircleMember" {
		test.Fatalf("the account, then each circle in turn: %v", calls)
	}
	if calls[0].variables["username"] != "personx" || calls[0].variables["password"] != "fixture-password" {
		test.Errorf("the account was made with %v", calls[0].variables)
	}
	for index, circleId := range []string{"circle-one", "circle-two"} {
		variables := calls[index+1].variables
		if variables["circleId"] != circleId || variables["username"] != "personx" || variables["memberRole"] != "member" {
			test.Errorf("circle %s was asked with %v", circleId, variables)
		}
	}
}

// Without --circle nothing about circles is asked, so the command behaves as
// it always did.
func TestUserCreateWithoutACircleAsksForNone(test *testing.T) {
	server, recorded := circleUserServer(test)

	if err := runUserCreateAgainst(test, server, "personx"); err != nil {
		test.Fatal(err)
	}

	if calls := recorded(); len(calls) != 1 || calls[0].operation != "CreateUser" {
		test.Fatalf("only the account: %v", calls)
	}
}

// A circle that refuses does not undo the account or stop the other circles,
// and the error names the circle and the command that finishes the job.
func TestUserCreateKeepsTheAccountWhenACircleRefuses(test *testing.T) {
	server, recorded := circleUserServer(test, "circle-gone")

	err := runUserCreateAgainst(test, server, "--circle", "circle-gone", "--circle", "circle-two", "personx")
	if err == nil {
		test.Fatal("a circle that refused should be an error")
	}
	for _, wanted := range []string{"personx was added", "circle circle-gone", "teanode circle add circle-gone personx"} {
		if !strings.Contains(err.Error(), wanted) {
			test.Errorf("the error %q does not say %q", err, wanted)
		}
	}
	if strings.Contains(err.Error(), "circle-two") {
		test.Errorf("the circle that took it is not in the error: %q", err)
	}
	if calls := recorded(); len(calls) != 3 || calls[2].variables["circleId"] != "circle-two" {
		test.Errorf("the circle after the refused one was still tried: %v", calls)
	}
}
