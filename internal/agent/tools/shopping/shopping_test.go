package shopping

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// routing answers each document with the answer registered for the operation
// it names, and keeps the variables it was sent.
type routing struct {
	lock    sync.Mutex
	answers map[string]string
	sent    map[string][]map[string]any
}

func (self *routing) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionShoppingUse}}
}

func (self *routing) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	self.lock.Lock()
	defer self.lock.Unlock()
	data := map[string]json.RawMessage{}
	for operation, answer := range self.answers {
		if strings.Contains(document, operation+"(") || strings.Contains(document, operation+" ") || strings.Contains(document, operation+"\n") {
			data[operation] = json.RawMessage(answer)
			if self.sent == nil {
				self.sent = map[string][]map[string]any{}
			}
			self.sent[operation] = append(self.sent[operation], variables)
		}
	}
	if result == nil {
		return nil
	}
	encoded, _ := json.Marshal(data)
	return json.Unmarshal(encoded, result)
}

// called is the variables of every send of an operation.
func (self *routing) called(operation string) []map[string]any {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.sent[operation]
}

// fakeRun is a run whose operations are these, for a person in UTC.
type fakeRun struct {
	tools.Run
	operations tools.Operations
}

func (self *fakeRun) Operations() tools.Operations         { return self.operations }
func (self *fakeRun) Configuration() *config.Configuration { return config.Default() }
func (self *fakeRun) Owner() *models.User {
	return &models.User{ID: "u-ada", Username: "ada", Timezone: "UTC"}
}

func runOf(operations tools.Operations) *fakeRun { return &fakeRun{operations: operations} }

// act runs one action of the shopping tool as the model does: through the
// merged tool, naming the action.
func act(t *testing.T, run tools.Run, action, arguments string) (string, *tools.Result, error) {
	t.Helper()
	merged := tools.Build().Get("shopping")
	if merged == nil {
		t.Fatal("no shopping tool")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(arguments), &fields); err != nil {
		t.Fatalf("%s: %s", err, arguments)
	}
	fields["action"] = action
	encoded, _ := json.Marshal(fields)
	result, err := merged.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call", Arguments: encoded})
	if err != nil {
		return "", nil, err
	}
	return result.Content, result, nil
}

// actJSON is act for an action that answers an object.
func actJSON(t *testing.T, run tools.Run, action, arguments string) (map[string]any, *tools.Result) {
	t.Helper()
	content, result, err := act(t, run, action, arguments)
	if err != nil {
		t.Fatalf("%s: %s", action, err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		t.Fatalf("%s: %s", err, content)
	}
	return answer, result
}

const groceriesList = `{"id":"list-1","listName":"Groceries","isOwner":true,"isReminded":true,"neededCount":2,"inCartCount":0,"notNeededCount":1,"isShared":false}`

func applesItem(identifier string) string {
	return `{"id":"` + identifier + `","shoppingListId":"list-1","listName":"Groceries","itemDescription":"Apples","itemDetails":"Fuji preferred",` +
		`"storeName":"Example Market","neededQuantity":"6","quantityUnit":"","shoppingItemState":"needed","aisleName":"Produce"}`
}

// The risk of each action is what the framework asks the person about: the
// reads never, the changes the person can change back never, and deleting a
// list, taking items off for good, sharing and ending a share always.
func TestTheRiskOfEachAction(t *testing.T) {
	merged := tools.Build().Get("shopping")
	if merged == nil {
		t.Fatal("no shopping tool")
	}
	if len(merged.Permissions) != 1 || merged.Permissions[0] != models.PermissionShoppingUse {
		t.Errorf("the tool needs shopping:use and nothing else: %v", merged.Permissions)
	}
	risks := map[string]tools.Risk{}
	for _, action := range []string{"lists", "list", "item", "shares", "stores", "store", "trips", "prices", "receipt", "import_preview"} {
		risks[action] = tools.RiskRead
	}
	for _, action := range []string{
		"create_list", "rename_list", "remind", "store_remind", "add_items", "update_item", "check", "uncheck", "need", "not_needed", "out_of_stock",
		"price", "find_picture", "start_shopping", "finish_shopping", "match_receipt", "decide_match", "match_line", "undo_receipt", "add_from_receipt",
	} {
		risks[action] = tools.RiskWrite
	}
	risks["delete_list"], risks["remove_items"], risks["unshare"] = tools.RiskDestructive, tools.RiskDestructive, tools.RiskDestructive

	// Sharing lets others see and change the list, which is a way in for
	// somebody else: the class the framework keeps for that, and it asks too.
	risks["share"] = tools.RiskGranting

	enum := merged.Parameters["properties"].(map[string]any)["action"].(map[string]any)["enum"].([]string)
	if len(enum) != len(risks) {
		t.Errorf("the tool has %d actions and the test knows %d", len(enum), len(risks))
	}
	for _, action := range enum {
		want, isKnown := risks[action]
		if !isKnown {
			t.Errorf("the action %s has no risk in this test", action)
			continue
		}
		arguments := json.RawMessage(fmt.Sprintf(`{"action":%q}`, action))
		if got := merged.RiskFor(arguments); got != want {
			t.Errorf("%s is %s, not %s", action, got, want)
		}
		if asks := tools.NeedsConfirmation(merged, arguments, nil, nil); asks != (want != tools.RiskRead && want != tools.RiskWrite) {
			t.Errorf("%s: asks the person is %v", action, asks)
		}
	}
	// A call nobody can read takes the strictest risk, never the gentlest.
	if got := merged.RiskFor(json.RawMessage(`not json`)); got != tools.RiskDestructive {
		t.Errorf("an unreadable call: %s", got)
	}
}

// The description is where the model learns what to do with a list the person
// sends, which no operation does for it.
func TestTheDescriptionSaysHowToReadAList(t *testing.T) {
	merged := tools.Build().Get("shopping")
	for _, want := range []string{
		"pastes a list", "screenshot", "photo", "you read it yourself", "add_items", "without the count", "item_details", "every preference",
		"needed_quantity", "quantity_unit", "aisle_name", "section heading", "store_name", "heading", "is_store_brand", "store's own product",
		"merges items already there", "everything but share it further or delete it", "without case or punctuation",
		"matched to the person's lists on its own", "receipt shows what it did", "undo_receipt reverts it",
	} {
		if !strings.Contains(merged.Description, want) {
			t.Errorf("the description does not say %q", want)
		}
	}
	// The framework puts an em dash between an action and its description; the
	// words on either side are ours.
	ours := strings.ReplaceAll(merged.Description, " \u2014 ", " ")
	for _, dash := range []string{"\u2014", "\u2013"} {
		if strings.Contains(ours, dash) {
			t.Error("the description has a dash that is not a hyphen")
		}
	}
}

// What a call must name, it is refused without, before anything is sent.
func TestArgumentsAreCheckedBeforeAnythingIsSent(t *testing.T) {
	operations := &routing{answers: map[string]string{}}
	tooMany := make([]string, 0, 201)
	for index := 0; index < 201; index++ {
		tooMany = append(tooMany, `{"item_description":"Apples"}`)
	}
	for _, testCase := range []struct{ action, arguments, want string }{
		{"list", `{}`, "list_id"},
		{"item", `{}`, "item_id"},
		{"shares", `{}`, "list_id"},
		{"store", `{}`, "store_name"},
		{"prices", `{}`, "item_id"},
		{"receipt", `{}`, "receipt_id"},
		{"import_preview", `{}`, "import_text or attachment_ids"},
		{"create_list", `{"list_name":"  "}`, "list_name"},
		{"rename_list", `{"list_id":"list-1"}`, "list_name"},
		{"delete_list", `{}`, "list_id"},
		{"share", `{"list_id":"list-1"}`, "with whom"},
		{"unshare", `{}`, "share_id"},
		{"remind", `{}`, "list_id"},
		{"store_remind", `{}`, "store_name"},
		{"add_items", `{"list_id":"list-1","items":[]}`, "1 to 200 items"},
		{"add_items", `{"list_id":"list-1","items":[{"item_description":" "}]}`, "item 1 of items has no item_description"},
		{"add_items", `{"list_id":"list-1","items":[` + strings.Join(tooMany, ",") + `]}`, "1 to 200 items"},
		{"update_item", `{"item_id":"item-1"}`, "at least one of"},
		{"remove_items", `{}`, "item_id or item_ids"},
		{"check", `{}`, "item_id or item_ids"},
		{"need", `{"item_ids":[]}`, "item_id or item_ids"},
		{"out_of_stock", `{}`, "item_id"},
		{"price", `{"item_id":"item-1"}`, "unit_price_amount"},
		{"start_shopping", `{"list_ids":[" "]}`, "list_ids"},
		{"finish_shopping", `{}`, "trip_id"},
		{"decide_match", `{"match_id":"match-1"}`, "is_applied"},
		{"match_line", `{"item_id":"item-1"}`, "receipt_line_id"},
		{"add_from_receipt", `{"receipt_line_id":"line-1"}`, "list_id"},
		{"lists", `{"page_size":500}`, "page_size is 1 to 200"},
	} {
		_, _, err := act(t, runOf(operations), testCase.action, testCase.arguments)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("%s %s: %v, want it to say %q", testCase.action, testCase.arguments, err, testCase.want)
		}
	}
	if len(operations.sent) != 0 {
		t.Errorf("something was sent: %v", operations.sent)
	}
}

// A list read to the tool is added with every field, a count written as a
// number is accepted, and an item that merged says so. The answer carries the
// words of the person and of whoever shares the list, so it is data.
func TestAddItemsSendsEveryFieldOfEveryItem(t *testing.T) {
	operations := &routing{answers: map[string]string{"AddShoppingItems": `[
		{"isMerged":false,"shoppingItem":` + applesItem("item-1") + `},
		{"isMerged":true,"shoppingItem":{"id":"item-2","shoppingListId":"list-1","itemDescription":"Bread","neededQuantity":"2","shoppingItemState":"needed"}}]`}}
	answer, result := actJSON(t, runOf(operations), "add_items", `{"list_id":"list-1","items":[
		{"item_description":"Apples","item_details":"Fuji preferred","needed_quantity":6,"aisle_name":"Produce","store_name":"Example Market","is_store_brand":true},
		{"item_description":"Bread","needed_quantity":"2","quantity_unit":"loaf","unit_price_amount":3.5,"currency_code":"USD"}]}`)
	if !result.Untrusted {
		t.Error("an answer that carries a list's words is data")
	}
	if answer["added_count"] != float64(2) || answer["merged_count"] != float64(1) {
		t.Errorf("the counts: %v", answer)
	}
	rows := answer["items"].([]any)
	if rows[0].(map[string]any)["item_id"] != "item-1" || rows[1].(map[string]any)["is_merged"] != true {
		t.Errorf("the rows: %v", rows)
	}
	sent := operations.called("AddShoppingItems")
	if len(sent) != 1 || sent[0]["shoppingListId"] != "list-1" {
		t.Fatalf("what was sent: %v", sent)
	}
	entries := sent[0]["shoppingItems"].([]any)
	apples, bread := entries[0].(map[string]any), entries[1].(map[string]any)
	if apples["itemDescription"] != "Apples" || apples["itemDetails"] != "Fuji preferred" || apples["neededQuantity"] != "6" || apples["aisleName"] != "Produce" ||
		apples["storeName"] != "Example Market" || apples["isStoreBrand"] != true {
		t.Errorf("the first item: %v", apples)
	}
	if bread["neededQuantity"] != "2" || bread["quantityUnit"] != "loaf" || bread["unitPriceAmount"] != "3.5" || bread["currencyCode"] != "USD" || bread["isStoreBrand"] != nil {
		t.Errorf("the second item: %v", bread)
	}
}

// check and need take several items at once, and a failure says which were
// already done so that the model does not repeat them.
func TestCheckTakesSeveralItems(t *testing.T) {
	operations := &routing{answers: map[string]string{"CheckShoppingItem": applesItem("item-1")}}
	answer, _ := actJSON(t, runOf(operations), "check", `{"item_id":"item-1","item_ids":["item-1","item-2"]}`)
	if rows := answer["items"].([]any); len(rows) != 2 {
		t.Errorf("one row for each item named, once: %v", rows)
	}
	sent := operations.called("CheckShoppingItem")
	if len(sent) != 2 || sent[0]["isChecked"] != true || sent[0]["shoppingItemId"] != "item-1" || sent[1]["shoppingItemId"] != "item-2" {
		t.Errorf("what was sent: %v", sent)
	}

	operations = &routing{answers: map[string]string{"SetShoppingItemNeed": applesItem("item-1")}}
	if _, _, err := act(t, runOf(operations), "need", `{"item_id":"item-1","needed_quantity":3}`); err != nil {
		t.Fatal(err)
	}
	if sent := operations.called("SetShoppingItemNeed"); len(sent) != 1 || sent[0]["isNeeded"] != true || sent[0]["neededQuantity"] != "3" {
		t.Errorf("need: %v", sent)
	}
	operations = &routing{answers: map[string]string{"SetShoppingItemNeed": applesItem("item-1")}}
	if _, _, err := act(t, runOf(operations), "not_needed", `{"item_id":"item-1"}`); err != nil {
		t.Fatal(err)
	}
	if sent := operations.called("SetShoppingItemNeed"); len(sent) != 1 || sent[0]["isNeeded"] != false || sent[0]["neededQuantity"] != nil {
		t.Errorf("not_needed: %v", sent)
	}
}

// A long list is read a page at a time, and the answer says there is more and
// where to read on.
func TestAListIsReadAPageAtATime(t *testing.T) {
	items := make([]string, 0, 5)
	for index := 1; index <= 5; index++ {
		items = append(items, applesItem(fmt.Sprintf("item-%d", index)))
	}
	operations := &routing{answers: map[string]string{
		"GetShoppingList": `{"totalCount":12,"shoppingList":` + groceriesList + `,"shoppingItems":[` + strings.Join(items, ",") + `],"shoppingTrips":[]}`,
	}}
	answer, result := actJSON(t, runOf(operations), "list", `{"list_id":"list-1","shopping_item_states":["needed"],"offset":4,"page_size":5}`)
	if answer["has_more"] != true || answer["next_offset"] != float64(9) || answer["total_count"] != float64(12) {
		t.Errorf("the paging: %v", answer)
	}
	if !result.Untrusted {
		t.Error("a list's items are data")
	}
	sent := operations.called("GetShoppingList")
	if len(sent) != 1 || sent[0]["offset"] != 4 || sent[0]["pageSize"] != 5 {
		t.Errorf("what was sent: %v", sent)
	}

	// The store screen is held whole and paged here.
	operations = &routing{answers: map[string]string{
		"ListShoppingStoreItems": `{"storeName":"Example Market","isReminded":true,"shoppingItems":[` + strings.Join(items, ",") + `],"shoppingLists":[],"shoppingTrips":[]}`,
	}}
	answer, _ = actJSON(t, runOf(operations), "store", `{"store_name":"Example Market","page_size":2,"offset":2}`)
	if rows := answer["items"].([]any); len(rows) != 2 || rows[0].(map[string]any)["item_id"] != "item-3" {
		t.Errorf("the second page: %v", answer["items"])
	}
	if answer["has_more"] != true || answer["next_offset"] != float64(4) || answer["total_count"] != float64(5) {
		t.Errorf("the paging: %v", answer)
	}
	answer, _ = actJSON(t, runOf(operations), "store", `{"store_name":"Example Market","page_size":2,"offset":4}`)
	if answer["has_more"] != false || answer["next_offset"] != nil {
		t.Errorf("the last page says there is no more: %v", answer)
	}
}

// Sharing resolves who it is for before it sends anything, and tells the
// person in the answer what the others may do.
func TestShareResolvesWhoItIsFor(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListCirclePeople":  `[{"userId":"u-bo","username":"bo"}]`,
		"ListCircles":       `[{"id":"circle-1","name":"Family","members":[{"userId":"u-ada"},{"userId":"u-bo"}]}]`,
		"ShareShoppingList": `{"id":"share-1","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"u-bo","granteeUsername":"bo"}`,
	}}
	answer, _ := actJSON(t, runOf(operations), "share", `{"list_id":"list-1","with":"bo"}`)
	if answer["share_id"] != "share-1" || !strings.Contains(answer["note"].(string), "only the owner can share it further or delete it") {
		t.Errorf("the answer: %v", answer)
	}
	sent := operations.called("ShareShoppingList")
	if len(sent) != 1 || sent[0]["granteeUserId"] != "u-bo" || sent[0]["granteeCircleId"] != nil {
		t.Errorf("a person: %v", sent)
	}
	operations.sent = nil
	if _, _, err := act(t, runOf(operations), "share", `{"list_id":"list-1","with":"Family"}`); err != nil {
		t.Fatal(err)
	}
	if sent := operations.called("ShareShoppingList"); len(sent) != 1 || sent[0]["granteeCircleId"] != "circle-1" || sent[0]["granteeUserId"] != nil {
		t.Errorf("a circle: %v", sent)
	}
	operations.sent = nil
	if _, _, err := act(t, runOf(operations), "share", `{"list_id":"list-1","with":"nobody"}`); err == nil || len(operations.called("ShareShoppingList")) != 0 {
		t.Errorf("somebody the person does not share a circle with is refused before sending: %v", err)
	}
}

// The card of each action that asks names what it is about in words, and says
// something even for a call that is half empty.
func TestTheCardsOfTheActionsThatAskSayWhatTheyDo(t *testing.T) {
	operations := &routing{answers: map[string]string{
		"ListShoppingLists": `[` + groceriesList + `]`,
		"GetShoppingItem":   applesItem("item-1"),
		"ListCirclePeople":  `[{"userId":"u-bo","username":"bo"}]`,
		"ListCircles":       `[]`,
	}}
	ctx := tools.WithRun(context.Background(), runOf(operations))
	merged := tools.Build().Get("shopping")
	for _, testCase := range []struct{ arguments, want string }{
		{`{"action":"delete_list","list_id":"list-1"}`, `Delete the shopping list "Groceries" with its 3 items`},
		{`{"action":"delete_list"}`, "Delete a shopping list"},
		{`{"action":"share","list_id":"list-1","with":"bo"}`, `Share the shopping list "Groceries" with bo`},
		{`{"action":"share"}`, "Share a shopping list with someone"},
		{`{"action":"unshare","share_id":"share-1"}`, "Stop sharing a shopping list"},
		{`{"action":"remove_items","item_ids":["item-1","item-2"]}`, `Remove "Apples", "Apples" from their shopping lists for good`},
		{`{"action":"remove_items","item_id":"item-1"}`, `Remove "Apples" from their shopping lists for good`},
		{`{"action":"remove_items"}`, "Remove items from their shopping lists for good"},
	} {
		if line := merged.PreviewLine(ctx, json.RawMessage(testCase.arguments)); !strings.Contains(line, testCase.want) {
			t.Errorf("%s: %q, want it to say %q", testCase.arguments, line, testCase.want)
		}
	}
}
