package db_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// shoppingWorld is three people: the owner and sam share the circle "Home",
// and lee shares no circle with either. All three may use shopping lists,
// and the owner has an agent, which keeps their receipts.
type shoppingWorld struct {
	database        db.Database
	owner, sam, lee *models.User
	home            *models.Circle
	ownerAgentId    string
}

func newShoppingWorld(test *testing.T) *shoppingWorld {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)
	world := &shoppingWorld{database: database}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		for username, target := range map[string]**models.User{"owner": &world.owner, "sam": &world.sam, "lee": &world.lee} {
			made, err := tx.CreateUser(&models.User{Username: username})
			if err != nil {
				test.Fatalf("CreateUser %s: %s", username, err)
			}
			*target = made
			dbtest.GrantPermission(test, tx, made.ID, models.PermissionShoppingUse)
		}
		var err error
		if world.home, err = tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, world.owner.ID); err != nil {
			test.Fatalf("CreateCircle: %s", err)
		}
		if _, err = tx.AddCircleMember(world.home.ID, world.sam.ID, models.MemberRoleMember, world.owner.ID); err != nil {
			test.Fatalf("AddCircleMember: %s", err)
		}
		agent, err := tx.GetAgentByUser(world.owner.ID)
		if err == nil && agent == nil {
			agent, err = tx.CreateAgent(&models.Agent{UserID: world.owner.ID, Enabled: true})
		}
		if err != nil || agent == nil {
			test.Fatalf("the owner's agent: %v", err)
		}
		world.ownerAgentId = agent.ID
	})
	return world
}

func (self *shoppingWorld) run(test *testing.T, run func(tx db.Transaction)) {
	test.Helper()
	dbtest.RunTransactionOn(test, self.database, run)
}

func mustCreateShoppingList(test *testing.T, tx db.Transaction, userId, listName string) *models.ShoppingList {
	test.Helper()
	list, err := tx.CreateShoppingList(userId, listName)
	if err != nil {
		test.Fatalf("CreateShoppingList: %s", err)
	}
	return list
}

func mustAddShoppingItems(test *testing.T, tx db.Transaction, userId, listId string, items ...*models.ShoppingItem) []*models.ShoppingItem {
	test.Helper()
	added, _, err := tx.AddShoppingItems(userId, listId, items)
	if err != nil {
		test.Fatalf("AddShoppingItems: %s", err)
	}
	return added
}

// A list reaches its owner and the people its shares reach while they share
// the circle; a share to somebody outside every circle is refused, and only
// the owner shares and deletes. Leaving the circle ends the share.
func TestAShoppingListReachesWhoItIsSharedWith(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var list *models.ShoppingList
	world.run(t, func(tx db.Transaction) {
		list = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		if _, err := tx.GetShoppingListAccess(world.sam.ID, list.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("an unshared list reached sam: %v", err)
		}
		if _, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeUserID: world.lee.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with somebody in no common circle was not refused: %v", err)
		}
		if _, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeCircleID: world.home.ID}); err != nil {
			t.Fatalf("ShareShoppingList: %s", err)
		}
		access, err := tx.GetShoppingListAccess(world.sam.ID, list.ID)
		if err != nil || access.IsOwner {
			t.Fatalf("the circle's share does not reach sam as a reader: %+v %v", access, err)
		}
		if _, err := tx.ShareShoppingList(world.sam.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeUserID: world.owner.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("sam shared a list that is not theirs: %v", err)
		}
		if err := tx.DeleteShoppingList(world.sam.ID, list.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("sam deleted a list that is not theirs: %v", err)
		}
		if _, err := tx.RenameShoppingList(world.sam.ID, list.ID, "Weekly groceries"); err != nil {
			t.Fatalf("sam could not rename a list shared with them: %s", err)
		}
		readers, err := tx.ListShoppingListReaderUserIDs(list.ID)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(readers)
		want := []string{world.owner.ID, world.sam.ID}
		slices.Sort(want)
		if !slices.Equal(readers, want) {
			t.Fatalf("the list's readers are %v, want the owner and sam", readers)
		}
		lists, err := tx.ListShoppingLists(world.sam.ID)
		if err != nil || len(lists) != 1 || lists[0].IsOwner || lists[0].OwnerUsername != "owner" || !lists[0].IsShared {
			t.Fatalf("sam's lists: %+v %v", lists, err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.sam.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if _, err := tx.GetShoppingListAccess(world.sam.ID, list.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("the list still reaches sam after they left the circle: %v", err)
		}
	})
}

// Adding an item already on the list, its description and store written
// another way, needs it again rather than adding it twice; the larger count
// wins, and new details are added. Aisles keep the order they came in.
func TestAddingAShoppingItemTwiceMergesIt(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		first := mustAddShoppingItems(t, tx, world.owner.ID, list.ID,
			&models.ShoppingItem{ItemDescription: "Apples", StoreName: "Example Market", AisleName: "Produce", NeededQuantity: "2"},
			&models.ShoppingItem{ItemDescription: "Frozen peas", StoreName: "Example Market", AisleName: "Frozen"},
			&models.ShoppingItem{ItemDescription: "Tomatoes", NeededQuantity: "4"},
		)
		if first[0].StoreKey != "examplemarket" || first[1].AisleRank <= first[0].AisleRank {
			t.Fatalf("the store key or the aisle order is wrong: %+v %+v", first[0], first[1])
		}
		again, isMerged, err := tx.AddShoppingItems(world.owner.ID, list.ID, []*models.ShoppingItem{
			{ItemDescription: "apples", StoreName: "EXAMPLE MARKET!", NeededQuantity: "3", ItemDetails: "Fuji preferred"},
			{ItemDescription: "Tomatoes", StoreName: "Elsewhere"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !isMerged[0] || again[0].ID != first[0].ID || again[0].NeededQuantity != "3" || again[0].ItemDetails != "Fuji preferred" {
			t.Fatalf("the same apples were not merged as needed: %v %+v", isMerged, again[0])
		}
		if isMerged[1] {
			t.Fatalf("tomatoes for another store were merged into the ones for any store")
		}
		items, total, err := tx.ListShoppingItems(world.owner.ID, list.ID, nil, 0, 50)
		if err != nil || total != 4 || len(items) != 4 {
			t.Fatalf("the list has %d items (%v), want 4", total, err)
		}
	})
}

// Checking off at the store puts an item in the cart; finishing the trip
// buys only what that shopper put in the cart; a trip left open long ago is
// finished by the next one on its list.
func TestAShoppingTripBuysWhatItsShopperPutInTheCart(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var list *models.ShoppingList
	var items []*models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		list = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		if _, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeUserID: world.sam.ID}); err != nil {
			t.Fatal(err)
		}
		items = mustAddShoppingItems(t, tx, world.owner.ID, list.ID,
			&models.ShoppingItem{ItemDescription: "Milk", StoreName: "Example Market"},
			&models.ShoppingItem{ItemDescription: "Bread", StoreName: "Example Market"},
			&models.ShoppingItem{ItemDescription: "Cookie kit", StoreName: "Example Market"},
		)
	})
	var trip, ownTrip *models.ShoppingTrip
	world.run(t, func(tx db.Transaction) {
		var err error
		if trip, _, err = tx.StartShoppingTrip(world.sam.ID, "Example Market", []string{list.ID}); err != nil {
			t.Fatalf("StartShoppingTrip: %s", err)
		}
		// sam is shopping already, so checking starts nothing new.
		if _, started, _, err := tx.CheckShoppingItem(world.sam.ID, items[0].ID, true, ""); err != nil || started != nil {
			t.Fatalf("sam's check: %+v %v", started, err)
		}
		// The owner put bread in the cart without starting: that started
		// their trip, at the bread's store.
		if _, ownTrip, _, err = tx.CheckShoppingItem(world.owner.ID, items[1].ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if ownTrip == nil || ownTrip.StoreName != "Example Market" || !slices.Equal(ownTrip.ShoppingListIDs, []string{list.ID}) {
			t.Fatalf("checking did not start the owner's trip: %+v", ownTrip)
		}
		marked, err := tx.SetShoppingItemOutOfStock(world.sam.ID, items[2].ID, true, "")
		if err != nil || marked.OutOfStockAt == nil || marked.OutOfStockStoreName != "Example Market" || marked.OutOfStockByName != "sam" {
			t.Fatalf("out of stock: %+v %v", marked, err)
		}
		open, err := tx.ListOpenShoppingTrips(world.owner.ID)
		if err != nil || len(open) != 2 || !slices.ContainsFunc(open, func(each *models.ShoppingTrip) bool { return each.ShopperUsername == "sam" }) {
			t.Fatalf("the owner does not see sam shopping: %+v %v", open, err)
		}
		if _, _, err := tx.FinishShoppingTrip(world.owner.ID, trip.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("the owner finished sam's trip: %v", err)
		}
		if _, _, err := tx.FinishShoppingTrip(world.sam.ID, trip.ID); err != nil {
			t.Fatalf("FinishShoppingTrip: %s", err)
		}
		milk, _ := tx.GetShoppingItem(world.owner.ID, items[0].ID)
		bread, _ := tx.GetShoppingItem(world.owner.ID, items[1].ID)
		if milk.ShoppingItemState != models.ShoppingItemStateNotNeeded || milk.LastBoughtAt == nil {
			t.Fatalf("sam's milk was not bought when the trip finished: %+v", milk)
		}
		if bread.ShoppingItemState != models.ShoppingItemStateInCart {
			t.Fatalf("the owner's bread was bought by sam's trip: %+v", bread)
		}
	})
	// The owner's trip, left open seven hours, is finished by sam's next one.
	dbtest.Exec(t, world.database, `UPDATE "shopping_trip" SET "started_at" = now() - interval '7 hours' WHERE "id" = '`+ownTrip.ID+`'`)
	world.run(t, func(tx db.Transaction) {
		_, bought, err := tx.StartShoppingTrip(world.sam.ID, "Example Market", []string{list.ID})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(bought, []string{list.ID}) {
			t.Fatalf("the stale trip's cart was not bought: %v", bought)
		}
		bread, _ := tx.GetShoppingItem(world.owner.ID, items[1].ID)
		if bread.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("the bread in a stale trip's cart was not bought: %+v", bread)
		}
	})
}

// Done needs no trip and no receipt: putting something in the cart starts
// the person's trip, and a second list joins it; Done with no trip named
// buys the cart, and still buys a cart whose trip ended under it.
func TestDoneBuysTheCartWithoutAStartedTrip(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var groceries, party *models.ShoppingList
	var milk, plates *models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		groceries = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		party = mustCreateShoppingList(t, tx, world.owner.ID, "Party supplies")
		milk = mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID, &models.ShoppingItem{ItemDescription: "Milk"})[0]
		plates = mustAddShoppingItems(t, tx, world.owner.ID, party.ID, &models.ShoppingItem{ItemDescription: "Paper plates"})[0]
	})
	world.run(t, func(tx db.Transaction) {
		_, started, _, err := tx.CheckShoppingItem(world.owner.ID, milk.ID, true, "Example Market")
		if err != nil || started == nil || started.StoreName != "Example Market" {
			t.Fatalf("the first check did not start a trip at the store given: %+v %v", started, err)
		}
		if _, again, _, err := tx.CheckShoppingItem(world.owner.ID, plates.ID, true, ""); err != nil || again != nil {
			t.Fatalf("the second check started another trip: %+v %v", again, err)
		}
		open, err := tx.ListOpenShoppingTrips(world.owner.ID)
		if err != nil || len(open) != 1 || len(open[0].ShoppingListIDs) != 2 {
			t.Fatalf("the trip did not gain the second list: %+v %v", open, err)
		}
		finished, bought, err := tx.FinishShoppingTrip(world.owner.ID, "")
		if err != nil || finished.FinishedAt == nil || len(bought) != 2 {
			t.Fatalf("Done with no trip named: %+v %v %v", finished, bought, err)
		}
		if _, _, err := tx.FinishShoppingTrip(world.owner.ID, ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("Done with nothing in the cart: %v", err)
		}
	})
	// A cart whose trip ended under it is still bought by Done.
	world.run(t, func(tx db.Transaction) {
		if _, err := tx.SetShoppingItemNeed(world.owner.ID, milk.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, milk.ID, true, ""); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.Exec(t, world.database, `UPDATE "shopping_trip" SET "finished_at" = now() WHERE "finished_at" IS NULL`)
	world.run(t, func(tx db.Transaction) {
		finished, bought, err := tx.FinishShoppingTrip(world.owner.ID, "")
		if err != nil || !slices.Equal(bought, []string{groceries.ID}) || !slices.Equal(finished.ShoppingListIDs, []string{groceries.ID}) {
			t.Fatalf("Done did not buy a cart with no trip: %+v %v %v", finished, bought, err)
		}
		boughtMilk, _ := tx.GetShoppingItem(world.owner.ID, milk.ID)
		if boughtMilk.ShoppingItemState != models.ShoppingItemStateNotNeeded || boughtMilk.LastBoughtAt == nil {
			t.Fatalf("the milk was not bought: %+v", boughtMilk)
		}
	})
}

// Unchecking takes an item out of the shopper's own cart only: a screen that
// is behind cannot pull an item out of somebody else's cart, or bring back
// an item already bought.
func TestUncheckingTakesBackOnlyTheShoppersOwnCart(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var milk, bread *models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		if _, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeUserID: world.sam.ID}); err != nil {
			t.Fatal(err)
		}
		items := mustAddShoppingItems(t, tx, world.owner.ID, list.ID, &models.ShoppingItem{ItemDescription: "Milk"}, &models.ShoppingItem{ItemDescription: "Bread"})
		milk, bread = items[0], items[1]
	})
	world.run(t, func(tx db.Transaction) {
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, milk.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		unchecked, _, _, err := tx.CheckShoppingItem(world.sam.ID, milk.ID, false, "")
		if err != nil || unchecked.ShoppingItemState != models.ShoppingItemStateInCart || unchecked.CartedByUserID != world.owner.ID {
			t.Fatalf("sam took the milk out of the owner's cart: %+v %v", unchecked, err)
		}
		if unchecked, _, _, err = tx.CheckShoppingItem(world.owner.ID, milk.ID, false, ""); err != nil || unchecked.ShoppingItemState != models.ShoppingItemStateNeeded {
			t.Fatalf("the owner could not take the milk back out: %+v %v", unchecked, err)
		}
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, bread.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if _, _, err := tx.FinishShoppingTrip(world.owner.ID, ""); err != nil {
			t.Fatal(err)
		}
		if unchecked, _, _, err = tx.CheckShoppingItem(world.owner.ID, bread.ID, false, ""); err != nil || unchecked.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("unchecking brought back bread already bought: %+v %v", unchecked, err)
		}
	})
}

// A check that starts a trip after a stale one answers the stale trip's
// lists as well, so the screens showing them are told it ended.
func TestCheckingAfterAStaleTripAnswersItsLists(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var groceries, party *models.ShoppingList
	var milk, plates *models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		groceries = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		party = mustCreateShoppingList(t, tx, world.owner.ID, "Party supplies")
		milk = mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID, &models.ShoppingItem{ItemDescription: "Milk"})[0]
		plates = mustAddShoppingItems(t, tx, world.owner.ID, party.ID, &models.ShoppingItem{ItemDescription: "Paper plates"})[0]
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, milk.ID, true, ""); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.Exec(t, world.database, `UPDATE "shopping_trip" SET "started_at" = now() - interval '7 hours' WHERE "finished_at" IS NULL`)
	world.run(t, func(tx db.Transaction) {
		_, started, changedListIds, err := tx.CheckShoppingItem(world.owner.ID, plates.ID, true, "")
		if err != nil || started == nil || !slices.Equal(started.ShoppingListIDs, []string{party.ID}) {
			t.Fatalf("the check did not start a new trip: %+v %v", started, err)
		}
		if !slices.Contains(changedListIds, groceries.ID) {
			t.Fatalf("the stale trip's list was not answered: %v", changedListIds)
		}
	})
}

// A receipt's sure matches check items off and keep their price at the
// store; an unsure one is only proposed; a count bought short of the count
// needed leaves the rest needed; undoing puts everything back.
func TestAReceiptChecksShoppingItemsOffAndUndoes(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var list *models.ShoppingList
	var items []*models.ShoppingItem
	var receipt *models.FinanceReceipt
	world.run(t, func(tx db.Transaction) {
		list = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		items = mustAddShoppingItems(t, tx, world.owner.ID, list.ID,
			&models.ShoppingItem{ItemDescription: "Oat flakes", StoreName: "Corner Grocer"},
			&models.ShoppingItem{ItemDescription: "Pears", NeededQuantity: "3"},
			&models.ShoppingItem{ItemDescription: "Dish soap"},
			&models.ShoppingItem{ItemDescription: "Lamp oil", StoreName: "Hardware Barn"},
		)
		var err error
		receipt, err = tx.PutFinanceReceipt(inventedGroceryReceipt(world.ownerAgentId, "mail-shopping-receipt"))
		if err != nil {
			t.Fatalf("PutFinanceReceipt: %s", err)
		}
		candidates, err := tx.ListShoppingReceiptCandidates(world.owner.ID, receipt.MerchantName)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range candidates {
			if candidate.ID == items[3].ID {
				t.Fatalf("an item for another store is a candidate for this receipt")
			}
		}
		if len(candidates) != 3 {
			t.Fatalf("%d candidates, want the grocer's oats and the two for any store", len(candidates))
		}
		lines := receipt.ReceiptLines
		written, changed, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, receipt.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: lines[0].ID, ShoppingItemID: items[0].ID, IsSure: true},
			{FinanceReceiptLineID: lines[1].ID, ShoppingItemID: items[1].ID, MatchedQuantity: "1", IsSure: true},
			{FinanceReceiptLineID: lines[3].ID, ShoppingItemID: items[2].ID, IsSure: false},
		})
		if err != nil {
			t.Fatalf("ApplyShoppingReceiptMatches: %s", err)
		}
		if len(written) != 3 || !slices.Equal(changed, []string{list.ID}) {
			t.Fatalf("written %d, changed %v", len(written), changed)
		}
		oats, _ := tx.GetShoppingItem(world.owner.ID, items[0].ID)
		if oats.ShoppingItemState != models.ShoppingItemStateNotNeeded || oats.UnitPriceAmount != "12.5" {
			t.Fatalf("the oats were not checked off with the receipt's price: %+v", oats)
		}
		pears, _ := tx.GetShoppingItem(world.owner.ID, items[1].ID)
		if pears.ShoppingItemState != models.ShoppingItemStateNeeded || pears.NeededQuantity != "2" || pears.UnitPriceAmount != "3" {
			t.Fatalf("one pear of three bought should leave two needed at the printed unit price: %+v", pears)
		}
		soap, _ := tx.GetShoppingItem(world.owner.ID, items[2].ID)
		if soap.ShoppingItemState != models.ShoppingItemStateNeeded {
			t.Fatalf("an unsure match checked the soap off: %+v", soap)
		}
		prices, err := tx.ListShoppingItemPrices(world.owner.ID, items[0].ID)
		if err != nil || len(prices) != 1 || prices[0].StoreName != "Corner Grocer" || prices[0].PriceSource != models.ShoppingPriceSourceReceipt || prices[0].ObservedOn != "2026-09-10" {
			t.Fatalf("the oats' price history: %+v %v", prices, err)
		}
		// Running the matcher again changes nothing decided.
		again, _, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, receipt.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: lines[0].ID, ShoppingItemID: items[0].ID, IsSure: true},
		})
		if err != nil || len(again) != 0 {
			t.Fatalf("a second run wrote %d matches (%v)", len(again), err)
		}
		// Sam reaches none of this.
		if _, err := tx.ListShoppingReceiptMatches(world.sam.ID, receipt.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("sam read the owner's receipt matches: %v", err)
		}
		if _, err := tx.UndoShoppingReceipt(world.owner.ID, receipt.ID); err != nil {
			t.Fatalf("UndoShoppingReceipt: %s", err)
		}
		oats, _ = tx.GetShoppingItem(world.owner.ID, items[0].ID)
		pears, _ = tx.GetShoppingItem(world.owner.ID, items[1].ID)
		if oats.ShoppingItemState != models.ShoppingItemStateNeeded || oats.UnitPriceAmount != "" {
			t.Fatalf("undo did not put the oats back: %+v", oats)
		}
		if pears.NeededQuantity != "3" {
			t.Fatalf("undo did not put the pears' count back: %+v", pears)
		}
		matches, err := tx.ListShoppingReceiptMatches(world.owner.ID, receipt.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range matches {
			if match.ShoppingItemID != items[2].ID && match.ReceiptMatchState != models.ReceiptMatchStateDismissed {
				t.Fatalf("an undone match is %s", match.ReceiptMatchState)
			}
		}
	})
}

// The store screen holds the store's items and, when asked, those for any
// store, across the lists the person reaches; the arrival reminder leaves out
// a list the person muted.
func TestTheStoreScreenGathersItemsAcrossLists(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		groceries := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		party := mustCreateShoppingList(t, tx, world.owner.ID, "Party")
		mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID,
			&models.ShoppingItem{ItemDescription: "Soup", StoreName: "Example Market"},
			&models.ShoppingItem{ItemDescription: "Tomatoes"},
		)
		mustAddShoppingItems(t, tx, world.owner.ID, party.ID, &models.ShoppingItem{ItemDescription: "Napkins", StoreName: "example market"})
		items, err := tx.ListShoppingStoreItems(world.owner.ID, "Example Market", nil, true)
		if err != nil || len(items) != 3 || items[2].ItemDescription != "Tomatoes" {
			t.Fatalf("the store screen: %d items (%v)", len(items), err)
		}
		listNames := map[string]bool{}
		for _, item := range items {
			listNames[item.ListName] = true
		}
		if !listNames["Groceries"] || !listNames["Party"] {
			t.Fatalf("each item does not say its list: %v", listNames)
		}
		if err := tx.SetShoppingListReminder(world.owner.ID, party.ID, false); err != nil {
			t.Fatal(err)
		}
		needed, err := tx.ListShoppingItemsNeededAt(world.owner.ID, models.ShoppingStoreKey("Example Market"), false)
		if err != nil || len(needed) != 1 || needed[0].ItemDescription != "Soup" {
			t.Fatalf("the reminder: %+v %v", needed, err)
		}
		stores, err := tx.ListShoppingStores(world.owner.ID)
		if err != nil || len(stores) != 1 || stores[0].NeededCount != 2 {
			t.Fatalf("stores: %+v %v", stores, err)
		}
	})
}

// A price typed for an earlier day is kept in the history without replacing
// the item's newer price.
func TestAnOlderPriceDoesNotReplaceTheNewest(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		item := mustAddShoppingItems(t, tx, world.owner.ID, list.ID, &models.ShoppingItem{ItemDescription: "Pumpkin", StoreName: "Example Market"})[0]
		today := time.Now().Format(time.DateOnly)
		if _, err := tx.RecordShoppingItemPrice(world.owner.ID, &models.ShoppingItemPrice{ShoppingItemID: item.ID, UnitPriceAmount: "$4.99", ObservedOn: today}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.RecordShoppingItemPrice(world.owner.ID, &models.ShoppingItemPrice{ShoppingItemID: item.ID, UnitPriceAmount: "3.99", ObservedOn: "2025-10-18", StoreName: "Farm stand"}); err != nil {
			t.Fatal(err)
		}
		stored, _ := tx.GetShoppingItem(world.owner.ID, item.ID)
		if stored.UnitPriceAmount != "4.99" || stored.CurrencyCode != "USD" {
			t.Fatalf("the item's price is %q %q, want the newest 4.99", stored.UnitPriceAmount, stored.CurrencyCode)
		}
		prices, _ := tx.ListShoppingItemPrices(world.owner.ID, item.ID)
		if len(prices) != 2 || prices[1].StoreName != "Farm stand" {
			t.Fatalf("prices: %+v", prices)
		}
		if _, err := tx.RecordShoppingItemPrice(world.lee.ID, &models.ShoppingItemPrice{ShoppingItemID: item.ID, UnitPriceAmount: "1"}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("lee priced an item on a list that does not reach them: %v", err)
		}
	})
}

// A person's trips of a stretch of time are their own, started inside it, and
// name only the lists they still reach: a share taken back takes its list off
// the trips made on it.
func TestShoppingTripsBetweenAreThePersonsOwn(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		share, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeCircleID: world.home.ID})
		if err != nil {
			t.Fatalf("ShareShoppingList: %s", err)
		}
		samTrip, _, err := tx.StartShoppingTrip(world.sam.ID, "Corner Market", []string{list.ID})
		if err != nil {
			t.Fatalf("StartShoppingTrip: %s", err)
		}
		now := time.Now()
		trips, err := tx.ListShoppingTripsBetween(world.sam.ID, now.Add(-time.Hour), now.Add(time.Hour))
		if err != nil || len(trips) != 1 || trips[0].ID != samTrip.ID || len(trips[0].ShoppingListIDs) != 1 {
			t.Fatalf("sam's trip of the hour: %+v %v", trips, err)
		}
		if later, err := tx.ListShoppingTripsBetween(world.sam.ID, now.Add(time.Hour), now.Add(2*time.Hour)); err != nil || len(later) != 0 {
			t.Fatalf("a trip outside the stretch: %+v %v", later, err)
		}
		if owners, err := tx.ListShoppingTripsBetween(world.owner.ID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil || len(owners) != 0 {
			t.Fatalf("sam's trip on the owner's list is not the owner's: %+v %v", owners, err)
		}
		if _, err := tx.GetShoppingTrip(world.owner.ID, samTrip.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("the owner read sam's trip: %v", err)
		}
		if err := tx.DeleteShoppingListShare(world.owner.ID, share.ID); err != nil {
			t.Fatalf("DeleteShoppingListShare: %s", err)
		}
		trip, err := tx.GetShoppingTrip(world.sam.ID, samTrip.ID)
		if err != nil || len(trip.ShoppingListIDs) != 0 || trip.StoreName != "Corner Market" {
			t.Fatalf("a trip on a list no longer shared: %+v %v", trip, err)
		}
	})
}
