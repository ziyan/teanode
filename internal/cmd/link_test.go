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

// Link previews are on the API and the command line under the same name; the
// dashboard and the iPhone app call the same operation.
func TestLinkPreviewParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	kind := reflect.TypeFor[apigraph.LinkPreviewQuery]()
	for index := 0; index < kind.NumMethod(); index++ {
		operations[kind.Method(index).Name] = true
	}
	if len(operations) != 1 || !operations["ReadLinkPreviews"] {
		test.Fatalf("the link operations are %v; this test lists ReadLinkPreviews only, so a new one needs a command", operations)
	}
	command := NewLinkCommand()
	if command.Name != "link" || len(command.Commands) != 1 || command.Commands[0].Name != "preview" {
		test.Errorf("the command is not teanode link preview: %q with %d subcommands", command.Name, len(command.Commands))
	}
}

func linkPreviewServer(test *testing.T) (*httptest.Server, func() map[string]any) {
	test.Helper()
	var lock sync.Mutex
	var variables map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil || !strings.Contains(document.Query, "ReadLinkPreviews") {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		lock.Lock()
		variables = document.Variables
		lock.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"ReadLinkPreviews":[
			{"url":"https://www.example.com/shop/chair?gclid=abc&color=blue","cleanedUrl":"https://www.example.com/shop/chair?color=blue",
			 "displayText":"example.com/shop/chair?color=blue","isAvailable":true,"title":"Garden Chair","description":"A chair for the garden.",
			 "siteName":"Example Shop","imageUrl":"/api/v1/link-preview/image/link-preview-1","imageWidthPixels":600,"imageHeightPixels":400},
			{"url":"https://example.net/gone","cleanedUrl":"https://example.net/gone","displayText":"example.net/gone","isAvailable":false,
			 "title":"","description":"","siteName":"","imageUrl":null,"imageWidthPixels":null,"imageHeightPixels":null}]}}`))
	}))
	test.Cleanup(server.Close)
	return server, func() map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return variables
	}
}

func runLinkAgainst(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewLinkCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "link", "preview"}, arguments...))
	})
	return output, err
}

// Each link is printed cleaned, with what its page said, and one whose page
// could not be read says so rather than printing blanks.
func TestLinkPreviewPrintsTheCleanedLinkAndWhatThePageSaid(test *testing.T) {
	server, recorded := linkPreviewServer(test)
	output, err := runLinkAgainst(test, server, "--conversation", "conversation-1", "https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone")
	if err != nil {
		test.Fatal(err)
	}
	if recorded()["conversationId"] != "conversation-1" {
		test.Errorf("the conversation asked about was %v", recorded()["conversationId"])
	}
	if urls, _ := recorded()["urls"].([]any); len(urls) != 2 || urls[0] != "https://www.example.com/shop/chair?gclid=abc&color=blue" {
		test.Errorf("the links asked for were %v", recorded()["urls"])
	}
	for _, want := range []string{
		"link:", "https://www.example.com/shop/chair?color=blue",
		"site:", "Example Shop", "title:", "Garden Chair", "description:", "A chair for the garden.",
		"https://example.net/gone", "preview:", "none",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the output has no %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "gclid") {
		test.Errorf("the output carries the tracking parameter:\n%s", output)
	}
}

func TestLinkPreviewJSONIsTheOperationsAnswer(test *testing.T) {
	server, _ := linkPreviewServer(test)
	output, err := runLinkAgainst(test, server, "--conversation", "conversation-1", "--json", "https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone")
	if err != nil {
		test.Fatal(err)
	}
	var previews []map[string]any
	if err := json.Unmarshal([]byte(output), &previews); err != nil || len(previews) != 2 {
		test.Fatalf("the output is not two previews: %v\n%s", err, output)
	}
	if previews[0]["cleanedUrl"] != "https://www.example.com/shop/chair?color=blue" || previews[1]["isAvailable"] != false {
		test.Errorf("the previews are %v", previews)
	}
}

func TestLinkPreviewNeedsAConversationAndBetweenOneAndTenLinks(test *testing.T) {
	server, _ := linkPreviewServer(test)
	if _, err := runLinkAgainst(test, server, "https://example.com/page"); err == nil {
		test.Error("links with no conversation were accepted")
	}
	if _, err := runLinkAgainst(test, server, "--conversation", "conversation-1"); err == nil {
		test.Error("no links were accepted")
	}
	many := make([]string, 11)
	for index := range many {
		many[index] = "https://example.com/page"
	}
	if _, err := runLinkAgainst(test, server, append([]string{"--conversation", "conversation-1"}, many...)...); err == nil {
		test.Error("eleven links were accepted")
	}
}
