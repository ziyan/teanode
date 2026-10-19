package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
)

// A fake server for the commands that link a contact to a person in the
// circles. The contact lives in the second of two address books, so linking
// has to look past the first.
func contactAccountServer(test *testing.T, linked *[]map[string]any) *httptest.Server {
	test.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var asked struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if json.NewDecoder(request.Body).Decode(&asked) != nil {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(asked.Query, "ListAddressBooks"):
			_, _ = response.Write([]byte(`{"data":{"ListAddressBooks":[{"id":"book-one","name":"Contacts","contacts":1},{"id":"book-two","name":"Work","contacts":1}]}}`))
		case strings.Contains(asked.Query, "ListCirclePeople"):
			_, _ = response.Write([]byte(`{"data":{"ListCirclePeople":[{"userId":"user-sam","username":"sam-example","name":"Sam Example"}]}}`))
		case strings.Contains(asked.Query, "SetContactAccount("):
			*linked = append(*linked, asked.Variables)
			if asked.Variables["addressBookId"] != "book-two" {
				_, _ = response.Write([]byte(`{"errors":[{"message":"api: not found"}],"data":null}`))
				return
			}
			account := `null`
			if asked.Variables["accountUserId"] != nil {
				account = `{"userId":"user-sam","username":"sam-example","name":"Sam Example"}`
			}
			_, _ = response.Write([]byte(`{"data":{"SetContactAccount":{"id":"contact-sam","name":"Sam Example","emails":[],"phones":[],"account":` + account + `}}}`))
		case strings.Contains(asked.Query, "ListContactAccountSuggestions"):
			_, _ = response.Write([]byte(`{"data":{"ListContactAccountSuggestions":[{"addressBookId":"book-two","contact":{"id":"contact-riley","name":"Riley Example","emails":["riley@example.test"],"phones":[]},"account":{"userId":"user-riley","username":"riley-example","name":"Riley"}}]}}`))
		case strings.Contains(asked.Query, "GetContact("):
			_, _ = response.Write([]byte(`{"data":{"GetContact":{"id":"contact-sam","name":"Sam Example","emails":[],"phones":[],"card":"BEGIN:VCARD\r\nEND:VCARD\r\n","accountUserId":"user-sam","account":{"userId":"user-sam","username":"sam-example","name":"Sam Example"}}}}`))
		default:
			_, _ = response.Write([]byte(`{"data":{}}`))
		}
	}))
	test.Cleanup(server.Close)
	return server
}

func runContactCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	var writer bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &writer,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewContactCommand()},
	}
	var runErr error
	output := captureStandardOutput(test, func() {
		runErr = command.Run(test.Context(), append([]string{"fixture", "contact"}, arguments...))
	})
	return output + writer.String(), runErr
}

// Linking names the person by username, finds the book that holds the
// contact, and says who the contact is; unlinking sends no account; showing a
// contact says who it is in the circles.
func TestContactLinkAndUnlink(test *testing.T) {
	var linked []map[string]any
	server := contactAccountServer(test, &linked)

	output, err := runContactCommand(test, server, "link", "contact-sam", "@Sam-Example")
	if err != nil || !strings.Contains(output, "Sam Example is in your circles as sam-example") {
		test.Fatalf("%v: %s", err, output)
	}
	if len(linked) != 2 || linked[0]["addressBookId"] != "book-one" || linked[1]["addressBookId"] != "book-two" ||
		linked[1]["contactId"] != "contact-sam" || linked[1]["accountUserId"] != "user-sam" {
		test.Fatalf("%v", linked)
	}

	linked = nil
	output, err = runContactCommand(test, server, "unlink", "contact-sam")
	if err != nil || !strings.Contains(output, "no longer noted") || linked[len(linked)-1]["accountUserId"] != nil {
		test.Fatalf("%v: %s %v", err, output, linked)
	}

	if _, err := runContactCommand(test, server, "link", "contact-sam", "nobody-here"); err == nil || !strings.Contains(err.Error(), "not somebody you share a circle with") {
		test.Fatalf("a username that is nobody in the circles: %v", err)
	}
	if _, err := runContactCommand(test, server, "link", "contact-sam"); err == nil {
		test.Fatal("link needs a contact and a username")
	}

	output, err = runContactCommand(test, server, "show", "contact-sam")
	if err != nil || !strings.Contains(output, "In your circles as sam-example") {
		test.Fatalf("%v: %s", err, output)
	}
}

// Suggestions are listed with the command that confirms each, and are JSON
// with --json. The address that matched is not in them.
func TestContactSuggestions(test *testing.T) {
	var linked []map[string]any
	server := contactAccountServer(test, &linked)

	output, err := runContactCommand(test, server, "suggestions")
	if err != nil || !strings.Contains(output, "Riley Example") || !strings.Contains(output, "riley-example") ||
		!strings.Contains(output, "teanode contact link contact-riley riley-example") ||
		!strings.Contains(output, "ADDRESS BOOK") || !strings.Contains(output, "Work") {
		test.Fatalf("%v: %s", err, output)
	}
	if len(linked) != 0 {
		test.Fatalf("a suggestion was acted on: %v", linked)
	}
	output, err = runContactCommand(test, server, "suggestions", "--json")
	var suggestions []*client.ContactAccountSuggestion
	if err != nil || json.Unmarshal([]byte(output), &suggestions) != nil || len(suggestions) != 1 || suggestions[0].Account.Username != "riley-example" {
		test.Fatalf("%v: %s", err, output)
	}
}

// Linking a contact to a person in the circles is on every surface that keeps
// contacts: the API, the command line and the contact tool. One table names
// each, and the photo listing's account filter is in the documents that send
// the filters.
func TestContactAccountParity(test *testing.T) {
	test.Parallel()
	table := map[string]struct {
		document       string
		subcommands    []string
		toolOperations []string
	}{
		"SetContactAccount":             {client.DocumentSetContactAccount, []string{"link", "unlink"}, []string{"link-account", "unlink-account"}},
		"ListContactAccountSuggestions": {client.DocumentListContactAccountSuggestions, []string{"suggestions"}, []string{"list"}},
	}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.AddressBookQuery](), reflect.TypeFor[apigraph.AddressBookMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}
	subcommands := map[string]bool{}
	for _, subcommand := range NewContactCommand().Commands {
		subcommands[subcommand.Name] = true
	}
	toolOperations := map[string]bool{}
	contactTool := tools.Build().Get("contact_book")
	if contactTool == nil {
		test.Fatal("the agent has no contact_book tool")
	}
	properties, _ := contactTool.Parameters["properties"].(map[string]any)
	action, _ := properties["action"].(map[string]any)
	switch enum := action["enum"].(type) {
	case []string:
		for _, name := range enum {
			toolOperations[name] = true
		}
	case []any:
		for _, name := range enum {
			toolOperations[name.(string)] = true
		}
	}

	for name, entry := range table {
		if !operations[name] {
			test.Errorf("the API has no operation %s", name)
		}
		if !strings.Contains(entry.document, " "+name+"(") && !strings.Contains(entry.document, " "+name+" ") {
			test.Errorf("the document for %s does not call it:\n%s", name, entry.document)
		}
		for _, subcommand := range entry.subcommands {
			if !subcommands[subcommand] {
				test.Errorf("teanode contact has no %s, which does %s", subcommand, name)
			}
		}
		for _, toolOperation := range entry.toolOperations {
			if !toolOperations[toolOperation] {
				test.Errorf("the contact tool has no %s, which does %s", toolOperation, name)
			}
		}
	}

	// The photo listing takes the account on every surface: the document, the
	// command line's flag and the photo tool's argument.
	if !strings.Contains(client.DocumentListPhotos, "accountUserId") || !strings.Contains(client.DocumentCountPhotos, "accountUserId") {
		test.Error("the photo listing documents do not send accountUserId")
	}
	foundFlag := false
	for _, subcommand := range NewPhotoCommand().Commands {
		if subcommand.Name != "list" {
			continue
		}
		for _, flag := range subcommand.Flags {
			foundFlag = foundFlag || slices.Contains(flag.Names(), "account")
		}
	}
	if !foundFlag {
		test.Error("teanode photo list has no --account")
	}
	photoProperties, _ := tools.Build().Get("photo").Parameters["properties"].(map[string]any)
	if _, isThere := photoProperties["username"]; !isThere {
		test.Error("the photo tool takes no username")
	}
}
