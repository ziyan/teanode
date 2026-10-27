package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// shoppingServer answers the documents the shopping commands send with an
// answer registered for the operation each names, and records what it was sent.
type shoppingServer struct {
	*httptest.Server
	mutex     sync.Mutex
	answers   map[string]string
	documents []string
	variables []map[string]any
}

const (
	shoppingGroceries = `{"id":"list-1","userId":"u-ada","listName":"Groceries","isOwner":true,"isReminded":true,"neededCount":2,"inCartCount":1,"notNeededCount":4,"isShared":true,"createdAt":"2030-03-05T09:50:00Z","updatedAt":"2030-03-05T09:50:00Z"}`
	shoppingApples    = `{"id":"item-1","shoppingListId":"list-1","listName":"Groceries","itemDescription":"Apples","itemDetails":"Fuji preferred","storeName":"Example Market","neededQuantity":"6","quantityUnit":"","shoppingItemState":"needed","aisleName":"Produce","createdAt":"2030-03-05T09:50:00Z","updatedAt":"2030-03-05T09:50:00Z"}`
)

func newShoppingServer(test *testing.T) *shoppingServer {
	test.Helper()
	self := &shoppingServer{answers: map[string]string{
		"ListShoppingLists":          `[` + shoppingGroceries + `]`,
		"GetShoppingList":            `{"totalCount":1,"shoppingList":` + shoppingGroceries + `,"shoppingItems":[` + shoppingApples + `],"shoppingTrips":[]}`,
		"AddShoppingItems":           `[{"isMerged":false,"shoppingItem":` + shoppingApples + `}]`,
		"PreviewShoppingImport":      `{"shoppingItems":[{"itemDescription":"Apples","itemDetails":"Fuji preferred","neededQuantity":"6","quantityUnit":"","aisleName":"Produce","storeName":"Example Market","isStoreBrand":false,"mergedShoppingItemId":"item-1","mergedShoppingItemState":"needed","mergedNeededQuantity":"4"}]}`,
		"UpdateShoppingItem":         shoppingApples,
		"CheckShoppingItem":          shoppingApples,
		"DeleteShoppingList":         `true`,
		"RemoveShoppingItems":        `2`,
		"ListCirclePeople":           `[{"userId":"u-bo","username":"bo"}]`,
		"ListCircles":                `[]`,
		"ShareShoppingList":          `{"id":"share-1","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"u-bo","granteeUsername":"bo","createdAt":"2030-03-05T09:50:00Z"}`,
		"DecideShoppingReceiptMatch": `{"id":"match-1","receiptMatchState":"applied"}`,
	}}
	self.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		var asked struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&asked); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		self.documents = append(self.documents, asked.Query)
		self.variables = append(self.variables, asked.Variables)
		data := map[string]json.RawMessage{}
		for operation, answer := range self.answers {
			if strings.Contains(asked.Query, operation+"(") || strings.Contains(asked.Query, operation+" ") {
				data[operation] = json.RawMessage(answer)
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
	}))
	test.Cleanup(self.Close)
	return self
}

// run runs `teanode shopping ...` against the server and returns what it wrote
// to the command's writer and to standard output.
func (self *shoppingServer) run(test *testing.T, arguments ...string) (string, error) {
	test.Helper()
	var writer bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &writer,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: self.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewShoppingCommand()},
	}
	var failure error
	standardOutput := captureStandardOutput(test, func() {
		failure = command.Run(test.Context(), append([]string{"fixture", "shopping"}, arguments...))
	})
	return writer.String() + standardOutput, failure
}

// sent is the variables of the last document that named the operation.
func (self *shoppingServer) sent(operation string) map[string]any {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for index := len(self.documents) - 1; index >= 0; index-- {
		if strings.Contains(self.documents[index], operation+"(") {
			return self.variables[index]
		}
	}
	return nil
}

func (self *shoppingServer) wasSent(operation string) bool {
	return self.sent(operation) != nil
}

func TestShoppingListsPrintsATableOrJSON(test *testing.T) {
	server := newShoppingServer(test)
	output, err := server.run(test, "lists")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"list-1", "Groceries", "you", "yes", "on"} {
		if !strings.Contains(output, want) {
			test.Fatalf("the table lacks %q:\n%s", want, output)
		}
	}
	output, err = server.run(test, "lists", "--json")
	var lists []map[string]any
	if err != nil || json.Unmarshal([]byte(output), &lists) != nil || len(lists) != 1 || lists[0]["listName"] != "Groceries" {
		test.Fatalf("%v: %s", err, output)
	}
}

// A list is named by its name or its id, and the states are checked before
// anything is sent.
func TestShoppingShowFindsTheListByNameAndPagesItsItems(test *testing.T) {
	server := newShoppingServer(test)
	output, err := server.run(test, "show", "groceries", "--state", "needed", "--state", "in_cart", "--limit", "20", "--offset", "5")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"Groceries", "item-1", "Apples", "6", "Example Market", "Produce", "Fuji preferred"} {
		if !strings.Contains(output, want) {
			test.Fatalf("the list lacks %q:\n%s", want, output)
		}
	}
	variables := server.sent("GetShoppingList")
	if variables["shoppingListId"] != "list-1" || variables["pageSize"] != float64(20) || variables["offset"] != float64(5) {
		test.Fatalf("%v", variables)
	}
	if states, _ := variables["shoppingItemStates"].([]any); len(states) != 2 || states[0] != "needed" || states[1] != "in_cart" {
		test.Fatalf("%v", variables)
	}
	if _, err := server.run(test, "show", "Groceries", "--state", "bought"); err == nil || !strings.Contains(err.Error(), "needed, in_cart or not_needed") {
		test.Fatalf("an unknown state was accepted: %v", err)
	}
	if _, err := server.run(test, "show", "Nothing at all"); err == nil || !strings.Contains(err.Error(), "not a list you reach") {
		test.Fatalf("an unknown list was accepted: %v", err)
	}
}

func TestShoppingAddSendsEveryItemWithTheFlagsOfTheCommand(test *testing.T) {
	server := newShoppingServer(test)
	server.answers["AddShoppingItems"] = `[{"isMerged":false,"shoppingItem":` + shoppingApples + `},{"isMerged":true,"shoppingItem":` + shoppingApples + `}]`
	output, err := server.run(test, "add", "Groceries", "Apples", "Bread", "--store", "Example Market", "--quantity", "2", "--unit", "kg", "--aisle", "Produce", "--details", "Fuji preferred", "--store-brand")
	if err != nil || !strings.Contains(output, "added 2 items to Groceries, 1 merged into items already there") {
		test.Fatalf("%v: %s", err, output)
	}
	variables := server.sent("AddShoppingItems")
	entries, _ := variables["shoppingItems"].([]any)
	if variables["shoppingListId"] != "list-1" || len(entries) != 2 {
		test.Fatalf("%v", variables)
	}
	bread := entries[1].(map[string]any)
	if bread["itemDescription"] != "Bread" || bread["storeName"] != "Example Market" || bread["neededQuantity"] != "2" || bread["quantityUnit"] != "kg" ||
		bread["aisleName"] != "Produce" || bread["itemDetails"] != "Fuji preferred" || bread["isStoreBrand"] != true {
		test.Fatalf("%v", bread)
	}
	before := len(server.documents)
	for _, arguments := range [][]string{{"add"}, {"add", "Groceries"}, {"add", "Groceries", "Apples", "--from-file", "list.txt"}} {
		if _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	if len(server.documents) != before {
		test.Fatal("something was sent for a command that was refused")
	}
}

// Importing a list shows what the model read and what each item merges into,
// and adds nothing until the person agrees.
func TestShoppingAddFromFileShowsThePreviewAndAsksFirst(test *testing.T) {
	server := newShoppingServer(test)
	path := filepath.Join(test.TempDir(), "list.txt")
	if err := os.WriteFile(path, []byte("Produce\n6 Apples, Fuji preferred\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	output, err := server.run(test, "add", "Groceries", "--from-file", path)
	if err == nil || !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "1 item to Groceries, of which 1 merge into items already there") {
		test.Fatalf("%v", err)
	}
	if !strings.Contains(output, "merges into the item already there (needed, 4)") {
		test.Fatalf("the preview:\n%s", output)
	}
	if server.wasSent("AddShoppingItems") {
		test.Fatal("items were added without confirmation")
	}
	if preview := server.sent("PreviewShoppingImport"); preview["shoppingListId"] != "list-1" || preview["importText"] != "Produce\n6 Apples, Fuji preferred\n" {
		test.Fatalf("%v", preview)
	}
	if _, err := server.run(test, "add", "Groceries", "--from-file", path, "--force"); err != nil {
		test.Fatal(err)
	}
	entries, _ := server.sent("AddShoppingItems")["shoppingItems"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["itemDescription"] != "Apples" || entries[0].(map[string]any)["neededQuantity"] != "6" || entries[0].(map[string]any)["aisleName"] != "Produce" {
		test.Fatalf("%v", entries)
	}
}

// What cannot be undone, and what lets others in, asks first.
func TestShoppingDeleteRemoveAndShareAskFirst(test *testing.T) {
	server := newShoppingServer(test)
	for _, arguments := range [][]string{
		{"delete", "Groceries"}, {"remove", "item-1", "item-2"}, {"share", "Groceries", "bo"}, {"unshare", "share-1"},
	} {
		if _, err := server.run(test, arguments...); err == nil || !strings.Contains(err.Error(), "--force") {
			test.Fatalf("%v: %v", arguments, err)
		}
	}
	for _, operation := range []string{"DeleteShoppingList", "RemoveShoppingItems", "ShareShoppingList", "UnshareShoppingList"} {
		if server.wasSent(operation) {
			test.Fatalf("%s was sent without confirmation", operation)
		}
	}
	if output, err := server.run(test, "delete", "Groceries", "--force"); err != nil || !strings.Contains(output, "deleted the list Groceries") || server.sent("DeleteShoppingList")["shoppingListId"] != "list-1" {
		test.Fatalf("%v: %s", err, output)
	}
	if output, err := server.run(test, "remove", "item-1", "item-2", "--force"); err != nil || !strings.Contains(output, "removed 2 items") {
		test.Fatalf("%v: %s", err, output)
	}
	if ids, _ := server.sent("RemoveShoppingItems")["shoppingItemIds"].([]any); len(ids) != 2 {
		test.Fatalf("%v", ids)
	}
	if _, err := server.run(test, "share", "Groceries", "bo", "--force"); err != nil {
		test.Fatal(err)
	}
	if variables := server.sent("ShareShoppingList"); variables["shoppingListId"] != "list-1" || variables["granteeUserId"] != "u-bo" {
		test.Fatalf("%v", variables)
	}
}

// edit changes only what is named, and an empty store makes it any store.
func TestShoppingEditChangesOnlyWhatIsNamed(test *testing.T) {
	server := newShoppingServer(test)
	if _, err := server.run(test, "edit", "item-1"); err == nil {
		test.Fatal("an edit that changes nothing was accepted")
	}
	if _, err := server.run(test, "edit", "item-1", "--store-brand", "--not-store-brand"); err == nil {
		test.Fatal("opposite flags were accepted")
	}
	if _, err := server.run(test, "edit", "item-1", "--store", "", "--quantity", "3"); err != nil {
		test.Fatal(err)
	}
	variables := server.sent("UpdateShoppingItem")
	if store, isSent := variables["storeName"]; !isSent || store != "" || variables["neededQuantity"] != "3" || variables["itemDetails"] != nil || variables["isStoreBrand"] != nil {
		test.Fatalf("%v", variables)
	}
}

func TestShoppingCheckAndDecideSendWhatTheyAreGiven(test *testing.T) {
	server := newShoppingServer(test)
	if _, err := server.run(test, "check", "item-1", "item-2"); err != nil {
		test.Fatal(err)
	}
	if variables := server.sent("CheckShoppingItem"); variables["shoppingItemId"] != "item-2" || variables["isChecked"] != true {
		test.Fatalf("%v", variables)
	}
	if _, err := server.run(test, "uncheck", "item-1"); err != nil {
		test.Fatal(err)
	}
	if variables := server.sent("CheckShoppingItem"); variables["isChecked"] != false {
		test.Fatalf("%v", variables)
	}
	if _, err := server.run(test, "decide", "match-1", "maybe"); err == nil {
		test.Fatal("a decision that is neither apply nor dismiss was accepted")
	}
	if _, err := server.run(test, "decide", "match-1", "dismiss"); err != nil {
		test.Fatal(err)
	}
	if variables := server.sent("DecideShoppingReceiptMatch"); variables["shoppingReceiptMatchId"] != "match-1" || variables["isApplied"] != false {
		test.Fatalf("%v", variables)
	}
}
