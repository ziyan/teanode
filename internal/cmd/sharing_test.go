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

	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Everything shared is on the API, the command line and the agent's circle
// tool under the same words: the list is "teanode sharing" and declining is
// "teanode sharing decline". TestCircleParity checks the agent's actions.
func TestSharingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.SharingQuery](), reflect.TypeFor[apigraph.SharingMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}
	if len(operations) != 2 || !operations["ListSharing"] || !operations["DeclineShare"] {
		test.Fatalf("the sharing operations are %v", operations)
	}
	command := NewSharingCommand()
	if command.Action == nil {
		test.Error("teanode sharing alone does not list")
	}
	if len(command.Commands) != 1 || command.Commands[0].Name != "decline" {
		test.Errorf("teanode sharing has the subcommands %v, want decline only", command.Commands)
	}
}

// everythingSharedServer answers ListSharing with one share of each side and records
// the document DeclineShare was sent.
func everythingSharedServer(test *testing.T) (*httptest.Server, func() map[string]any) {
	test.Helper()
	var lock sync.Mutex
	var declined map[string]any
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
		case strings.Contains(document.Query, "DeclineShare"):
			lock.Lock()
			declined = document.Variables
			lock.Unlock()
			_, _ = response.Write([]byte(`{"data":{"DeclineShare":true}}`))
		case strings.Contains(document.Query, "ListSharing"):
			_, _ = response.Write([]byte(`{"data":{"ListSharing":{
				"sharesMade":[
					{"shareKind":"calendar","shareId":"s-1","subjectId":"c-1","subjectName":"Calendar","shareLevel":"titles","shareScopes":[],"passwordItemCount":0,
					 "granteeUserId":"u-sam","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":null,"granteeCircleName":"","createdAt":"2030-01-02T00:00:00Z"},
					{"shareKind":"passwords","shareId":"","subjectId":"","subjectName":"","shareLevel":"","shareScopes":[],"passwordItemCount":3,
					 "granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"g-1","granteeCircleName":"Home","createdAt":"2030-01-03T00:00:00Z"}],
				"sharesReceived":[
					{"shareKind":"memory_page","shareId":"s-2","subjectId":"people/kim","subjectName":"Kim","shareLevel":"","shareScopes":["quotes"],"passwordItemCount":0,
					 "ownerUserId":"u-sam","ownerUsername":"sam","ownerName":"Sam Lee","granteeCircleId":null,"granteeCircleName":"","isDirect":true,"canDecline":true,"createdAt":"2030-01-04T00:00:00Z"},
					{"shareKind":"location","shareId":"s-3","subjectId":"","subjectName":"","shareLevel":"current","shareScopes":[],"passwordItemCount":0,
					 "ownerUserId":"u-alex","ownerUsername":"alex","ownerName":"","granteeCircleId":"g-1","granteeCircleName":"Home","isDirect":false,"canDecline":false,"createdAt":"2030-01-05T00:00:00Z"}]}}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)
	return server, func() map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return declined
	}
}

func runEverythingSharedAgainst(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewSharingCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "sharing"}, arguments...))
	})
	return output, err
}

// "teanode sharing" prints the two lists, a share of passwords as a count, and
// a share through a circle as such.
func TestSharingPrintsWhatIsSharedBothWays(test *testing.T) {
	server, _ := everythingSharedServer(test)
	output, err := runEverythingSharedAgainst(test, server)
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{
		"Shared by you", "Shared with you",
		"calendar", "sam (Sam Lee)", "titles", "2030-01-02",
		"passwords", "3 items", "circle Home",
		"memory page", "Kim", "quotes", "by name",
		"location", "alex", "current",
	} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the output has no %q:\n%s", wanted, output)
		}
	}
	if strings.Index(output, "Shared by you") > strings.Index(output, "Shared with you") {
		test.Errorf("the lists are the wrong way round:\n%s", output)
	}
}

func TestSharingWithJSONPrintsBothLists(test *testing.T) {
	server, _ := everythingSharedServer(test)
	output, err := runEverythingSharedAgainst(test, server, "--json")
	if err != nil {
		test.Fatal(err)
	}
	var sharing struct {
		SharesMade     []map[string]any `json:"sharesMade"`
		SharesReceived []map[string]any `json:"sharesReceived"`
	}
	if err := json.Unmarshal([]byte(output), &sharing); err != nil || len(sharing.SharesMade) != 2 || len(sharing.SharesReceived) != 2 {
		test.Errorf("it printed %q: %v", output, err)
	}
}

// "sharing decline" sends the kind as the server names it, whether typed with
// a hyphen or an underscore, and needs both arguments.
func TestSharingDeclineSendsTheKindAndTheShare(test *testing.T) {
	server, recordedDecline := everythingSharedServer(test)
	output, err := runEverythingSharedAgainst(test, server, "decline", "--force", "memory-page", "s-2")
	if err != nil || !strings.Contains(output, "declined the share") {
		test.Fatalf("it printed %q: %v", output, err)
	}
	if variables := recordedDecline(); variables["shareKind"] != "memory_page" || variables["shareId"] != "s-2" {
		test.Errorf("the decline was %v", variables)
	}
	if _, err := runEverythingSharedAgainst(test, server, "decline", "--force", "calendar"); err == nil || !strings.Contains(err.Error(), "usage: teanode sharing decline") {
		test.Errorf("a missing share was not refused with the usage: %v", err)
	}
}
