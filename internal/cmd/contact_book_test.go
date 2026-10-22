package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A fake server with the caller's own address book and a circle's. It records
// every document that reached it with its variables.
func contactBookServer(test *testing.T, asked *[]map[string]any) *httptest.Server {
	test.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var received struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if json.NewDecoder(request.Body).Decode(&received) != nil {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		circleBook := `{"id":"book-family","name":"Family","description":"","contacts":3,"circleId":"circle-home","circleName":"Family","isCircleBook":true,"canManage":true}`
		switch {
		case strings.Contains(received.Query, "ListReachableAddressBooks"):
			_, _ = response.Write([]byte(`{"data":{"ListReachableAddressBooks":[{"id":"book-own","name":"Contacts","description":"mine","contacts":1,"circleId":null,"circleName":"","isCircleBook":false,"canManage":true},` + circleBook + `]}}`))
		case strings.Contains(received.Query, "ListAddressBooks"):
			_, _ = response.Write([]byte(`{"data":{"ListAddressBooks":[{"id":"book-own","name":"Contacts","description":"mine","contacts":1}]}}`))
		case strings.Contains(received.Query, "ListCircles"):
			_, _ = response.Write([]byte(`{"data":{"ListCircles":[{"id":"circle-home","name":"Family","circleKind":"family","members":[{"userId":"user-one"},{"userId":"user-two"}]},{"id":"circle-club","name":"Club","circleKind":"friends","members":[{"userId":"user-one"}]}]}}`))
		case strings.Contains(received.Query, "CreateCircleAddressBook("):
			*asked = append(*asked, map[string]any{"CreateCircleAddressBook": received.Variables})
			_, _ = response.Write([]byte(`{"data":{"CreateCircleAddressBook":` + circleBook + `}}`))
		case strings.Contains(received.Query, "SaveAddressBook("):
			*asked = append(*asked, map[string]any{"SaveAddressBook": received.Variables})
			_, _ = response.Write([]byte(`{"data":{"SaveAddressBook":` + circleBook + `}}`))
		case strings.Contains(received.Query, "DeleteCircleAddressBook("):
			*asked = append(*asked, map[string]any{"DeleteCircleAddressBook": received.Variables})
			_, _ = response.Write([]byte(`{"data":{"DeleteCircleAddressBook":true}}`))
		case strings.Contains(received.Query, "ListContacts("):
			*asked = append(*asked, map[string]any{"ListContacts": received.Variables})
			_, _ = response.Write([]byte(`{"data":{"ListContacts":[{"id":"contact-kim","name":"Kim Example","emails":["kim@example.test"],"phones":[]}]}}`))
		case strings.Contains(received.Query, "SaveContact("):
			*asked = append(*asked, map[string]any{"SaveContact": received.Variables})
			_, _ = response.Write([]byte(`{"data":{"SaveContact":{"id":"contact-kim","name":"Kim Example","emails":[],"phones":[]}}}`))
		default:
			_, _ = response.Write([]byte(`{"data":{}}`))
		}
	}))
	test.Cleanup(server.Close)
	return server
}

// The books are listed with whose each is, a circle's is made by name or id,
// and the contact commands work in a circle's book when it is chosen and in
// the caller's own when it is not.
func TestContactBooksOfCircles(test *testing.T) {
	var asked []map[string]any
	server := contactBookServer(test, &asked)

	output, err := runContactCommand(test, server, "book", "list")
	if err != nil || !strings.Contains(output, "Contacts") || !strings.Contains(output, "you") ||
		!strings.Contains(output, `the circle "Family"`) || !strings.Contains(output, "book-family") {
		test.Fatalf("%v: %s", err, output)
	}

	if _, err := runContactCommand(test, server, "book", "create"); err == nil {
		test.Fatal("a circle is needed")
	}
	output, err = runContactCommand(test, server, "book", "create", "--circle", "family")
	if err != nil || !strings.Contains(output, `made "Family"`) || !strings.Contains(output, "--book book-family") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := asked[len(asked)-1]["CreateCircleAddressBook"].(map[string]any); variables["circleId"] != "circle-home" || variables["name"] != nil {
		test.Fatalf("the circle is found by its name: %v", variables)
	}
	if _, err := runContactCommand(test, server, "book", "create", "--circle", "nowhere"); err == nil || !strings.Contains(err.Error(), "no circle") {
		test.Fatalf("a circle one is not in: %v", err)
	}
	if _, err := runContactCommand(test, server, "book", "create", "--circle", "circle-club", "--name", "Everyone"); err != nil {
		test.Fatal(err)
	}
	if variables := asked[len(asked)-1]["CreateCircleAddressBook"].(map[string]any); variables["circleId"] != "circle-club" || variables["name"] != "Everyone" {
		test.Fatalf("by id, with a name: %v", variables)
	}

	// Only the name changes: what the book says about itself is kept.
	if _, err := runContactCommand(test, server, "book", "rename", "book-own", "Everybody"); err != nil {
		test.Fatal(err)
	}
	if variables := asked[len(asked)-1]["SaveAddressBook"].(map[string]any); variables["id"] != "book-own" || variables["name"] != "Everybody" || variables["description"] != "mine" {
		test.Fatalf("%v", variables)
	}
	if _, err := runContactCommand(test, server, "book", "rename", "book-missing", "Nobody"); err == nil {
		test.Fatal("a book that is not there")
	}

	if _, err := runContactCommand(test, server, "book", "delete", "book-own", "--force"); err == nil || !strings.Contains(err.Error(), "no circle address book") {
		test.Fatalf("a person's own book is not deleted: %v", err)
	}
	output, err = runContactCommand(test, server, "book", "delete", "book-family", "--force")
	if err != nil || !strings.Contains(output, `deleted "Family"`) {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := asked[len(asked)-1]["DeleteCircleAddressBook"].(map[string]any); variables["addressBookId"] != "book-family" {
		test.Fatalf("%v", variables)
	}

	// The contact commands: own by default, a circle's with --book.
	asked = nil
	if _, err := runContactCommand(test, server, "list"); err != nil {
		test.Fatal(err)
	}
	if asked[0]["ListContacts"].(map[string]any)["addressBookId"] != "book-own" {
		test.Fatalf("a list is the person's own: %v", asked)
	}
	if _, err := runContactCommand(test, server, "list", "--book", "book-family"); err != nil {
		test.Fatal(err)
	}
	if asked[1]["ListContacts"].(map[string]any)["addressBookId"] != "book-family" {
		test.Fatalf("a list of the circle's book: %v", asked)
	}
	if _, err := runContactCommand(test, server, "add", "--book", "book-family", "--name", "Kim Example"); err != nil {
		test.Fatal(err)
	}
	if asked[2]["SaveContact"].(map[string]any)["addressBookId"] != "book-family" {
		test.Fatalf("adding to the circle's book: %v", asked)
	}
	if _, err := runContactCommand(test, server, "list", "--book", "book-elsewhere"); err == nil || !strings.Contains(err.Error(), "no address book") {
		test.Fatalf("a book one cannot use: %v", err)
	}
}
