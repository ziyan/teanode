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

// Every operation on shared recordings is on the API, the command line and the
// agent's recording tool under the same names. The dashboard and the iPhone app
// call the same operations.
func TestRecordingSharingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.RecordingShareQuery](), reflect.TypeFor[apigraph.RecordingShareMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}
	table := map[string]string{
		"ListRecordingShares":  "shares",
		"ShareRecording":       "share",
		"UnshareRecording":     "unshare",
		"ListSharedRecordings": "shared",
		"ReadSharedRecording":  "read-shared",
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}
	commands := map[string]bool{}
	for _, subcommand := range NewRecordingCommand().Commands {
		commands[subcommand.Name] = true
	}
	operationsOfTool := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "recording" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		operation, _ := properties["operation"].(map[string]any)
		switch enum := operation["enum"].(type) {
		case []string:
			for _, name := range enum {
				operationsOfTool[name] = true
			}
		case []any:
			for _, name := range enum {
				operationsOfTool[name.(string)] = true
			}
		}
	}
	for operation, name := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if !commands[name] {
			test.Errorf("teanode recording has no %s", name)
		}
		if !operationsOfTool[name] {
			test.Errorf("the recording tool has no operation %s", name)
		}
	}
}

// recordingSharingServer answers the documents the sharing commands send, and
// records the variables each was sent with.
func recordingSharingServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	answers := map[string]string{
		"ListCirclePeople": `[{"userId":"u-sam","username":"sam","name":"Sam"}]`,
		"ListCircles":      `[{"id":"c1","name":"Family","circleKind":"family","members":[{"userId":"u-alex"},{"userId":"u-sam"}],"pendingInvitations":[]}]`,
		"ShareRecording":   `{"id":"s1","recordingId":"r1","granteeUserId":"u-sam","granteeUsername":"sam","recordingShareLevel":"transcript"}`,
		"ListRecordingShares": `[{"id":"s1","recordingId":"r1","granteeUserId":"u-sam","granteeUsername":"sam","recordingShareLevel":"transcript"},
			{"id":"s2","recordingId":"r1","granteeCircleId":"c1","granteeCircleName":"Family","recordingShareLevel":"summary"}]`,
		"UnshareRecording":     `true`,
		"ListSharedRecordings": `[{"recordingId":"r1","ownerUsername":"alex","recordingTitle":"Standup","createdAt":"2030-01-02T10:00:00Z","recordingShareLevel":"summary","speakerNames":[],"passages":[]}]`,
		"ReadSharedRecording": `{"recordingId":"r1","ownerUsername":"alex","recordingTitle":"Standup","createdAt":"2030-01-02T10:00:00Z","timelineDurationMS":125000,
			"recordingSummary":"Two items.","recordingShareLevel":"transcript","speakerNames":["Alex"],"passageCount":3,"hasMorePassages":true,
			"passages":[{"segmentStartMS":0,"segmentEndMS":1000,"speakerName":"Alex","transcriptText":"We ship on Friday.\u001b[31m"},{"segmentStartMS":65000,"segmentEndMS":66000,"speakerName":"Alex","transcriptText":"Agreed."}]}`,
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

func runRecordingCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewRecordingCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "recording"}, arguments...))
	})
	return output, err
}

// Sharing names a person by username or a circle by name, sends the
// identifier the server wants, and asks first (with the warning that the
// others who speak did not agree) unless told not to.
func TestRecordingShareResolvesWhoItIsForAndAsksFirst(test *testing.T) {
	server, called := recordingSharingServer(test)

	// Standard input is not a terminal here, so asking refuses, in words, and
	// nothing is sent.
	_, err := runRecordingCommand(test, server, "share", "r1", "sam", "--level", "transcript")
	if err == nil || !strings.Contains(err.Error(), "have not agreed to it being shared") || !strings.Contains(err.Error(), "--force") {
		test.Fatalf("the warning and the way past it: %v", err)
	}
	if called("ShareRecording") != nil {
		test.Fatal("something was shared before the person agreed")
	}

	output, err := runRecordingCommand(test, server, "share", "r1", "sam", "--level", "transcript", "--force")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ShareRecording")
	if variables["recordingId"] != "r1" || variables["granteeUserId"] != "u-sam" || variables["granteeCircleId"] != nil || variables["recordingShareLevel"] != "transcript" {
		test.Errorf("a person is shared with by their account: %v", variables)
	}
	if !strings.Contains(output, "sam") || !strings.Contains(output, "transcript") {
		test.Errorf("it says what it did: %q", output)
	}

	output, err = runRecordingCommand(test, server, "share", "r1", "Family", "--level", "summary", "--force")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(output, `the circle "Family" (2 people)`) {
		test.Errorf("a circle is named as one, with its size: %q", output)
	}
	if variables = called("ShareRecording"); variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil || variables["recordingShareLevel"] != "summary" {
		test.Errorf("a circle is shared with by name: %v", variables)
	}

	if _, err = runRecordingCommand(test, server, "share", "r1", "stranger", "--level", "summary", "--force"); err == nil || !strings.Contains(err.Error(), "circle") {
		test.Errorf("a stranger: %v", err)
	}
	if _, err = runRecordingCommand(test, server, "share", "r1", "sam", "--force"); err == nil || !strings.Contains(err.Error(), "--level") {
		test.Errorf("no level: %v", err)
	}
	if _, err = runRecordingCommand(test, server, "share", "r1", "sam", "--level", "audio", "--force"); err == nil {
		test.Errorf("a level there is not")
	}
}

func TestRecordingSharesUnshareAndShared(test *testing.T) {
	server, called := recordingSharingServer(test)

	output, err := runRecordingCommand(test, server, "shares", "r1")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"sam", "transcript", "s1", `the circle "Family"`, "summary", "s2"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shares lacks %q:\n%s", wanted, output)
		}
	}
	if _, err = runRecordingCommand(test, server, "unshare", "s2"); err != nil {
		test.Fatal(err)
	}
	if called("UnshareRecording")["recordingShareId"] != "s2" {
		test.Errorf("unshare sends the share: %v", called("UnshareRecording"))
	}
	output, err = runRecordingCommand(test, server, "shared")
	if err != nil {
		test.Fatal(err)
	}
	for _, wanted := range []string{"r1", "Standup", "alex", "summary", "2030-01-02"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("shared lacks %q:\n%s", wanted, output)
		}
	}
	if variables := called("ListSharedRecordings"); variables["pageSize"] != float64(25) || variables["beforeRecordingId"] != nil {
		test.Errorf("the first page: %v", variables)
	}
}

// Reading a shared recording prints another person's words with control
// characters made visible, and says where the next page starts.
func TestReadingASharedRecordingPrintsItSafelyAndSaysWhereToContinue(test *testing.T) {
	server, called := recordingSharingServer(test)
	output, err := runRecordingCommand(test, server, "read-shared", "r1", "--offset", "10", "--limit", "2")
	if err != nil {
		test.Fatal(err)
	}
	if variables := called("ReadSharedRecording"); variables["recordingId"] != "r1" || variables["passageOffset"] != float64(10) || variables["pageSize"] != float64(2) {
		test.Errorf("the page asked for: %v", variables)
	}
	for _, wanted := range []string{"Standup", "Two items.", "speakers: Alex", "[00:00] Alex: We ship on Friday.", "[01:05] Alex: Agreed.", "more: --offset 12"} {
		if !strings.Contains(output, wanted) {
			test.Errorf("the reading lacks %q:\n%s", wanted, output)
		}
	}
	if strings.Contains(output, "\x1b") {
		test.Errorf("a control character reached the terminal: %q", output)
	}
	if _, err := runRecordingCommand(test, server, "read-shared"); err == nil {
		test.Error("read-shared without a recording")
	}
}
