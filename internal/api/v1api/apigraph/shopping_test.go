package apigraph

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// newShoppingWorld is the calendar share world (ada and ben in the circle
// Home, cora and dave elsewhere) with shopping lists allowed for all of them.
func newShoppingWorld(t *testing.T) *calendarShareWorld {
	t.Helper()
	world := newCalendarShareWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "shopping role", Permissions: []models.Permission{models.PermissionShoppingUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		for _, user := range []*models.User{world.ada, world.ben, world.cora, world.dave} {
			if _, err := tx.CreateGroup(&models.Group{Name: user.Username + " shopping group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
		}
	})
	return world
}

// shoppingEventReceived says whether the channel gets an event for the list
// within the wait.
func shoppingEventReceived(events <-chan db.ShoppingEvent, shoppingListId string, wait time.Duration) bool {
	timeout := time.After(wait)
	for {
		select {
		case event := <-events:
			if event.ShoppingListID == shoppingListId {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

// A list shared with a circle is read and checked off by its members through
// the API, each change reaches the screens of the people it reaches and no
// one else's, the store screen gathers it, the Sharing page lists it, and
// taking the share back tells the member's screen to drop it.
func TestAShoppingListIsSharedAndCheckedOffLive(t *testing.T) {
	world := newShoppingWorld(t)
	listId := world.call(t, world.ada, `mutation { CreateShoppingList(listName: "Groceries") { id listName isOwner } }`)["CreateShoppingList"].(map[string]any)["id"].(string)
	added := world.call(t, world.ada, fmt.Sprintf(`mutation { AddShoppingItems(shoppingListId: %q, shoppingItems: [
		{itemDescription: "Soup", storeName: "Example Market", isStoreBrand: true, aisleName: "Refrigerated"},
		{itemDescription: "Tomatoes", neededQuantity: "4"}
	]) { isMerged shoppingItem { id storeKey neededQuantity shoppingItemState } } }`, listId))["AddShoppingItems"].([]any)
	soupId := added[0].(map[string]any)["shoppingItem"].(map[string]any)["id"].(string)

	world.refused(t, world.ben, fmt.Sprintf(`query { GetShoppingList(shoppingListId: %q) { totalCount } }`, listId))

	hub := &world.graph.shoppingEvents
	adaEvents, releaseAda, err := hub.subscribe(world.database, world.ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAda()
	coraEvents, releaseCora, err := hub.subscribe(world.database, world.cora.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseCora()
	benEvents, releaseBen, err := hub.subscribe(world.database, world.ben.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBen()

	share := world.call(t, world.ada, fmt.Sprintf(`mutation { ShareShoppingList(shoppingListId: %q, granteeCircleId: %q) { id granteeCircleName listName } }`, listId, world.home))["ShareShoppingList"].(map[string]any)
	if share["granteeCircleName"] != "Home" || share["listName"] != "Groceries" {
		t.Fatalf("the share: %v", share)
	}
	page := world.call(t, world.ben, fmt.Sprintf(`query { GetShoppingList(shoppingListId: %q) { totalCount shoppingList { isOwner ownerUsername } } }`, listId))["GetShoppingList"].(map[string]any)
	if page["totalCount"].(float64) != 2 || page["shoppingList"].(map[string]any)["isOwner"] != false {
		t.Fatalf("ben's view of the list: %v", page)
	}

	world.call(t, world.ben, fmt.Sprintf(`mutation { StartShopping(storeName: "Example Market", shoppingListIds: [%q]) { id shopperUsername } }`, listId))
	checked := world.call(t, world.ben, fmt.Sprintf(`mutation { CheckShoppingItem(shoppingItemId: %q, isChecked: true) { shoppingItemState cartedByName } }`, soupId))["CheckShoppingItem"].(map[string]any)
	if checked["shoppingItemState"] != "in_cart" {
		t.Fatalf("checked: %v", checked)
	}
	if !shoppingEventReceived(adaEvents, listId, 5*time.Second) {
		t.Error("ada's screen did not hear that ben checked an item off")
	}
	if shoppingEventReceived(coraEvents, listId, 300*time.Millisecond) {
		t.Error("cora, whom the list does not reach, heard of it")
	}

	store := world.call(t, world.ada, `query { ListShoppingStoreItems(storeName: "EXAMPLE MARKET") { shoppingItems { itemDescription listName } shoppingLists { listName neededCount isIncluded } shoppingTrips { shopperUsername storeName } } }`)["ListShoppingStoreItems"].(map[string]any)
	if len(store["shoppingItems"].([]any)) != 2 || len(store["shoppingTrips"].([]any)) != 1 {
		t.Fatalf("the store screen: %v", store)
	}

	sharing := world.call(t, world.ada, `query { ListSharing { sharesMade { shareKind subjectName } } }`)["ListSharing"].(map[string]any)
	isListed := false
	for _, each := range sharing["sharesMade"].([]any) {
		if made := each.(map[string]any); made["shareKind"] == string(models.ShareKindShopping) && made["subjectName"] == "Groceries" {
			isListed = true
		}
	}
	if !isListed {
		t.Errorf("the Sharing page does not list the shopping list: %v", sharing)
	}

	// Ben may not share it further or delete it.
	world.refused(t, world.ben, fmt.Sprintf(`mutation { DeleteShoppingList(shoppingListId: %q) }`, listId))
	world.refused(t, world.ben, fmt.Sprintf(`mutation { ShareShoppingList(shoppingListId: %q, granteeUserId: %q) { id } }`, listId, world.ada.ID))

	for shoppingEventReceived(benEvents, listId, 200*time.Millisecond) {
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareShoppingList(shoppingListShareId: %q) }`, share["id"]))
	if !shoppingEventReceived(benEvents, listId, 5*time.Second) {
		t.Error("ben's screen was not told the list is gone from him")
	}
	world.refused(t, world.ben, fmt.Sprintf(`query { GetShoppingList(shoppingListId: %q) { totalCount } }`, listId))
}
