package contact_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/contact"
	"github.com/ziyan/teanode/internal/models"
)

// Taking a picture off a contact reaches the person's phone and has no undo,
// so it asks first, as forgetting the contact does.
func TestRemovingAContactsPictureIsAnActionThatAsksFirst(test *testing.T) {
	tool := tools.Build().Get("contact_book")
	if tool == nil {
		test.Fatal("the agent has no contact_book tool")
	}
	properties, _ := tool.Parameters["properties"].(map[string]any)
	action, _ := properties["action"].(map[string]any)
	if values, _ := json.Marshal(action["enum"]); !strings.Contains(string(values), `"remove-picture"`) {
		test.Fatalf("remove-picture is not an action: %s", values)
	}
	arguments := json.RawMessage(`{"action":"remove-picture","id":"contact-one"}`)
	if risk := tool.RiskOf(arguments); risk != tools.RiskDestructive {
		test.Fatalf("%v", risk)
	}
	if line := tool.Preview(arguments); !strings.Contains(line, "picture") {
		test.Fatalf("%q", line)
	}
	if risk := tool.RiskOf(json.RawMessage(`{"action":"list"}`)); risk != tools.RiskRead {
		test.Fatalf("%v", risk)
	}
}

// fakeBookOperations answers the documents of the address book tool from a
// table by the operation each one calls, and remembers the variables.
type fakeBookOperations struct {
	answers   map[string]string
	variables map[string]map[string]any
}

func (self *fakeBookOperations) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionContactsUse}}
}

func (self *fakeBookOperations) Execute(ctx context.Context, document string, variables map[string]any, answered any) error {
	for operationName, answer := range self.answers {
		if strings.Contains(document, operationName+"(") || strings.Contains(document, operationName+" ") {
			if self.variables == nil {
				self.variables = map[string]map[string]any{}
			}
			self.variables[operationName] = variables
			return json.Unmarshal([]byte(`{"`+operationName+`":`+answer+`}`), answered)
		}
	}
	return errors.New("not answered: " + document)
}

type fakeBookRun struct {
	tools.Run
	operations *fakeBookOperations
}

func (self *fakeBookRun) Operations() tools.Operations { return self.operations }

func callContactBook(test *testing.T, operations *fakeBookOperations, arguments string) (*tools.Result, error) {
	test.Helper()
	tool := tools.Build().Get("contact_book")
	return tool.Run(tools.WithRun(context.Background(), &fakeBookRun{operations: operations}), &tools.Call{ID: "fixture-call", Arguments: json.RawMessage(arguments)})
}

const (
	grantedBook = `[{"id":"book-one","name":"Contacts","contacts":2,"agentGranted":true}]`
	samContact  = `{"id":"contact-sam","name":"Sam Example","emails":["sam@example.test"],"account":{"userId":"user-sam","username":"sam-example"}}`
)

// Linking a contact names the person by username among the people in the
// circles, writes without asking (it tells nobody), and unlinking sends an
// empty account.
func TestLinkingAContactToAPersonInTheCircles(test *testing.T) {
	tool := tools.Build().Get("contact_book")
	for _, action := range []string{"link-account", "unlink-account"} {
		arguments := json.RawMessage(`{"action":"` + action + `","contact_id":"contact-sam"}`)
		if risk := tool.RiskOf(arguments); risk != tools.RiskWrite {
			test.Errorf("%s is %v, wanted a write that tells nobody", action, risk)
		}
		if tool.Preview(arguments) == "The address book" {
			test.Errorf("%s has no preview of its own", action)
		}
	}

	operations := &fakeBookOperations{answers: map[string]string{
		"ListAddressBooks":  grantedBook,
		"ListCirclePeople":  `[{"userId":"user-riley","username":"riley-example","name":"Riley"},{"userId":"user-sam","username":"sam-example","name":"Sam"}]`,
		"SetContactAccount": samContact,
	}}
	result, err := callContactBook(test, operations, `{"action":"link-account","contact_id":"contact-sam","username":"@Sam-Example"}`)
	if err != nil {
		test.Fatal(err)
	}
	variables := operations.variables["SetContactAccount"]
	if variables["addressBookId"] != "book-one" || variables["contactId"] != "contact-sam" || variables["accountUserId"] != "user-sam" {
		test.Fatalf("%v", variables)
	}
	if !strings.Contains(result.Content, `"account_username":"sam-example"`) {
		test.Fatalf("the answer shows who the contact is: %s", result.Content)
	}
	if _, err := callContactBook(test, operations, `{"action":"link-account","contact_id":"contact-sam","username":"nobody-here"}`); err == nil || !strings.Contains(err.Error(), "nobody in their circles") {
		test.Fatalf("a username that is nobody in the circles: %v", err)
	}
	if _, err := callContactBook(test, operations, `{"action":"link-account","username":"sam-example"}`); err == nil {
		test.Fatal("a link needs a contact")
	}

	if _, err := callContactBook(test, operations, `{"action":"unlink-account","contact_id":"contact-sam"}`); err != nil {
		test.Fatal(err)
	}
	if variables := operations.variables["SetContactAccount"]; variables["accountUserId"] != nil {
		test.Fatalf("unlinking sends no account: %v", variables)
	}
}

// A list shows who a linked contact is, and offers the suggestions without
// acting on them or saying which address matched.
func TestAListShowsLinksAndOffersSuggestions(test *testing.T) {
	operations := &fakeBookOperations{answers: map[string]string{
		"ListAddressBooks":              grantedBook,
		"ListContacts":                  `[` + samContact + `,{"id":"contact-riley","name":"Riley Example","emails":["riley@example.test"]}]`,
		"ListContactAccountSuggestions": `[{"addressBookId":"book-one","contact":{"id":"contact-riley","name":"Riley Example"},"account":{"userId":"user-riley","username":"riley-example"}}]`,
	}}
	result, err := callContactBook(test, operations, `{"action":"list"}`)
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{`"account_username":"sam-example"`, `"suggested_links":[{"address_book_id":"book-one","contact_id":"contact-riley","contact_name":"Riley Example","username":"riley-example"}]`, "look like people in the person's circles"} {
		if !strings.Contains(result.Content, want) {
			test.Errorf("the list lacks %s:\n%s", want, result.Content)
		}
	}
	if strings.Contains(result.Content, "userId") {
		test.Errorf("the list shows account ids the model does not need:\n%s", result.Content)
	}

	// With more than one book, the suggestion names the book it is in.
	operations.answers["ListAddressBooks"] = `[{"id":"book-one","name":"Contacts","contacts":2,"agentGranted":true},{"id":"book-two","name":"Work","contacts":1,"agentGranted":true}]`
	operations.answers["ListContactAccountSuggestions"] = `[{"addressBookId":"book-two","contact":{"id":"contact-riley","name":"Riley Example"},"account":{"userId":"user-riley","username":"riley-example"}}]`
	result, err = callContactBook(test, operations, `{"action":"list"}`)
	if err != nil || !strings.Contains(result.Content, `"address_book_id":"book-two","address_book_name":"Work"`) {
		test.Fatalf("%v %s", err, result.Content)
	}
	operations.answers["ListAddressBooks"] = grantedBook

	// With no suggestions the list says nothing of them.
	delete(operations.answers, "ListContactAccountSuggestions")
	operations.answers["ListContactAccountSuggestions"] = `[]`
	result, err = callContactBook(test, operations, `{"action":"list"}`)
	if err != nil || strings.Contains(result.Content, "suggest") {
		test.Fatalf("%v %s", err, result.Content)
	}
}
