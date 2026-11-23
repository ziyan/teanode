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

// A check on the list buys an item with no trip and nobody told; unchecked
// soon after, the purchase is taken back and the item was last bought when
// it was before. An item in somebody else's cart stays in theirs.
func TestCheckingOffOnTheListBuysWithoutATrip(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var groceries *models.ShoppingList
	var milk, bread *models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		groceries = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		if _, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: groceries.ID, GranteeUserID: world.sam.ID}); err != nil {
			t.Fatalf("ShareShoppingList: %s", err)
		}
		added := mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID,
			&models.ShoppingItem{ItemDescription: "Milk"}, &models.ShoppingItem{ItemDescription: "Bread"})
		milk, bread = added[0], added[1]
	})
	earlier := time.Now().Add(-14 * 24 * time.Hour).UTC().Truncate(time.Second)
	dbtest.Exec(t, world.database, `UPDATE "shopping_item" SET "last_bought_at" = '`+earlier.Format(time.RFC3339)+`' WHERE "id" = '`+milk.ID+`'`)
	world.run(t, func(tx db.Transaction) {
		checked, err := tx.CheckOffShoppingItem(world.owner.ID, milk.ID, true)
		if err != nil || checked.ShoppingItemState != models.ShoppingItemStateNotNeeded || checked.LastBoughtAt == nil || !checked.LastBoughtAt.After(earlier) {
			t.Fatalf("the milk was not bought: %+v %v", checked, err)
		}
		if open, err := tx.ListOpenShoppingTrips(world.owner.ID); err != nil || len(open) != 0 {
			t.Fatalf("checking off started a trip: %+v %v", open, err)
		}
		unchecked, err := tx.CheckOffShoppingItem(world.owner.ID, milk.ID, false)
		if err != nil || unchecked.ShoppingItemState != models.ShoppingItemStateNeeded {
			t.Fatalf("the milk is not needed again: %+v %v", unchecked, err)
		}
		if unchecked.LastBoughtAt == nil || !unchecked.LastBoughtAt.Equal(earlier) {
			t.Fatalf("the purchase taken back left the milk last bought at %v, expected %v", unchecked.LastBoughtAt, earlier)
		}
		// In sam's cart, the owner's check leaves it there.
		if _, _, _, err := tx.CheckShoppingItem(world.sam.ID, bread.ID, true, "Example Market"); err != nil {
			t.Fatal(err)
		}
		left, err := tx.CheckOffShoppingItem(world.owner.ID, bread.ID, true)
		if err != nil || left.ShoppingItemState != models.ShoppingItemStateInCart || left.CartedByUserID != world.sam.ID {
			t.Fatalf("the owner's check took the bread out of sam's cart: %+v %v", left, err)
		}
	})
	// A purchase older than the undo window stays: unchecking needs it again.
	world.run(t, func(tx db.Transaction) {
		if _, err := tx.CheckOffShoppingItem(world.owner.ID, milk.ID, true); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.Exec(t, world.database, `UPDATE "shopping_item" SET "last_bought_at" = now() - interval '1 day' WHERE "id" = '`+milk.ID+`'`)
	world.run(t, func(tx db.Transaction) {
		unchecked, err := tx.CheckOffShoppingItem(world.owner.ID, milk.ID, false)
		if err != nil || unchecked.ShoppingItemState != models.ShoppingItemStateNeeded || unchecked.LastBoughtAt == nil ||
			time.Since(*unchecked.LastBoughtAt) < 23*time.Hour {
			t.Fatalf("an old purchase was taken back: %+v %v", unchecked, err)
		}
	})
}

// A trip finished by mistake is resumed: what it bought goes back in the
// shopper's cart, last bought when it was before, and the trip is open
// again. Only the shopper's own latest trip, within the undo window, and not
// while they are shopping another.
func TestAFinishedTripIsResumed(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var groceries *models.ShoppingList
	var milk, bread, eggs *models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		groceries = mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		added := mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID,
			&models.ShoppingItem{ItemDescription: "Milk"}, &models.ShoppingItem{ItemDescription: "Bread"}, &models.ShoppingItem{ItemDescription: "Eggs"})
		milk, bread, eggs = added[0], added[1], added[2]
	})
	var trip *models.ShoppingTrip
	world.run(t, func(tx db.Transaction) {
		if resumable, err := tx.GetResumableShoppingTrip(world.owner.ID); err != nil || resumable != nil {
			t.Fatalf("a trip to resume before any: %+v %v", resumable, err)
		}
		var err error
		if _, trip, _, err = tx.CheckShoppingItem(world.owner.ID, milk.ID, true, "Example Market"); err != nil || trip == nil {
			t.Fatalf("CheckShoppingItem: %+v %v", trip, err)
		}
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, bread.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if resumable, err := tx.GetResumableShoppingTrip(world.owner.ID); err != nil || resumable != nil {
			t.Fatalf("a trip to resume while shopping: %+v %v", resumable, err)
		}
		if _, _, err := tx.FinishShoppingTrip(world.owner.ID, trip.ID); err != nil {
			t.Fatal(err)
		}
		// Needed again after Done, the bread is not the trip's to take back.
		if _, err := tx.SetShoppingItemNeed(world.owner.ID, bread.ID, true, "2"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := tx.ResumeShoppingTrip(world.sam.ID, trip.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("somebody else resumed the trip: %v", err)
		}
		resumable, err := tx.GetResumableShoppingTrip(world.owner.ID)
		if err != nil || resumable == nil || resumable.ID != trip.ID || resumable.BoughtItemCount != 1 || resumable.FinishedAt == nil {
			t.Fatalf("the trip to resume: %+v %v", resumable, err)
		}
		resumed, changed, err := tx.ResumeShoppingTrip(world.owner.ID, trip.ID)
		if err != nil || resumed.FinishedAt != nil || !slices.Equal(changed, []string{groceries.ID}) {
			t.Fatalf("ResumeShoppingTrip: %+v %v %v", resumed, changed, err)
		}
		backInCart, _ := tx.GetShoppingItem(world.owner.ID, milk.ID)
		if backInCart.ShoppingItemState != models.ShoppingItemStateInCart || backInCart.CartedByUserID != world.owner.ID || backInCart.LastBoughtAt != nil {
			t.Fatalf("the milk is not back in the cart as it was: %+v", backInCart)
		}
		stillNeeded, _ := tx.GetShoppingItem(world.owner.ID, bread.ID)
		if stillNeeded.ShoppingItemState != models.ShoppingItemStateNeeded || stillNeeded.NeededQuantity != "2" {
			t.Fatalf("resuming changed the bread needed again since: %+v", stillNeeded)
		}
		if open, err := tx.ListOpenShoppingTrips(world.owner.ID); err != nil || len(open) != 1 || open[0].ID != trip.ID {
			t.Fatalf("the resumed trip is not open: %+v %v", open, err)
		}
		// Checking goes on in the resumed trip, and Done buys both.
		if _, started, _, err := tx.CheckShoppingItem(world.owner.ID, eggs.ID, true, ""); err != nil || started != nil {
			t.Fatalf("a check after resuming started another trip: %+v %v", started, err)
		}
		if _, _, err := tx.ResumeShoppingTrip(world.owner.ID, trip.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("an open trip was resumed: %v", err)
		}
		finished, bought, err := tx.FinishShoppingTrip(world.owner.ID, "")
		if err != nil || finished.ID != trip.ID || !slices.Equal(bought, []string{groceries.ID}) {
			t.Fatalf("Done after resuming: %+v %v %v", finished, bought, err)
		}
		if finished, _ := tx.GetShoppingItem(world.owner.ID, eggs.ID); finished.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("the eggs were not bought: %+v", finished)
		}
	})
	// Too long ago, it is over.
	dbtest.Exec(t, world.database, `UPDATE "shopping_trip" SET "finished_at" = now() - interval '4 hours' WHERE "id" = '`+trip.ID+`'`)
	world.run(t, func(tx db.Transaction) {
		if resumable, err := tx.GetResumableShoppingTrip(world.owner.ID); err != nil || resumable != nil {
			t.Fatalf("an old trip is offered: %+v %v", resumable, err)
		}
		if _, _, err := tx.ResumeShoppingTrip(world.owner.ID, trip.ID); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("an old trip was resumed: %v", err)
		}
	})
}

// A receipt snapped as the shopper finishes is read after Done bought the
// cart: what was just bought is still a candidate, and an old purchase is not.
func TestAReceiptStillMatchesWhatWasJustBought(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var milk, bread *models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		groceries := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		added := mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID,
			&models.ShoppingItem{ItemDescription: "Milk", StoreName: "Example Market"}, &models.ShoppingItem{ItemDescription: "Bread", StoreName: "Example Market"})
		milk, bread = added[0], added[1]
		for _, item := range added {
			if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, item.ID, true, ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := tx.FinishShoppingTrip(world.owner.ID, ""); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.Exec(t, world.database, `UPDATE "shopping_item" SET "last_bought_at" = now() - interval '2 days' WHERE "id" = '`+bread.ID+`'`)
	world.run(t, func(tx db.Transaction) {
		candidates, err := tx.ListShoppingReceiptCandidates(world.owner.ID, "EXAMPLE MARKET #12")
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, candidate := range candidates {
			ids = append(ids, candidate.ID)
		}
		if !slices.Equal(ids, []string{milk.ID}) {
			t.Fatalf("the candidates are %v, expected only the milk just bought", ids)
		}
	})
}

// Searching a list keeps the items whose description, details, store or
// aisle hold every word, without case, and a % in the search means itself.
func TestSearchingAListKeepsItemsHoldingEveryWord(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		groceries := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID,
			&models.ShoppingItem{ItemDescription: "Oat milk", StoreName: "Example Market", AisleName: "Dairy"},
			&models.ShoppingItem{ItemDescription: "Whole milk", ItemDetails: "the blue cap"},
			&models.ShoppingItem{ItemDescription: "Bread", AisleName: "Bakery"},
			&models.ShoppingItem{ItemDescription: "100% juice"})
		searched := func(searchText string) []string {
			items, total, err := tx.ListShoppingItems(world.owner.ID, groceries.ID, nil, searchText, 0, 50)
			if err != nil || int(total) != len(items) {
				t.Fatalf("ListShoppingItems(%q): %d of %d, %v", searchText, len(items), total, err)
			}
			descriptions := []string{}
			for _, item := range items {
				descriptions = append(descriptions, item.ItemDescription)
			}
			slices.Sort(descriptions)
			return descriptions
		}
		for searchText, expected := range map[string][]string{
			"MILK":         {"Oat milk", "Whole milk"},
			"milk example": {"Oat milk"},
			"blue":         {"Whole milk"},
			"bakery":       {"Bread"},
			"%":            {"100% juice"},
			"  ":           {"100% juice", "Bread", "Oat milk", "Whole milk"},
			"milk bread":   {},
		} {
			if found := searched(searchText); !slices.Equal(found, expected) {
				t.Errorf("searching %q found %v, expected %v", searchText, found, expected)
			}
		}
	})
}

// A long trip finished near the end of the undo window is open once
// resumed: the next check goes into it rather than starting a trip that
// would buy the resumed cart again.
func TestALongTripResumedLateStaysOpen(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var milk, eggs *models.ShoppingItem
	var trip *models.ShoppingTrip
	world.run(t, func(tx db.Transaction) {
		groceries := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		added := mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID,
			&models.ShoppingItem{ItemDescription: "Milk"}, &models.ShoppingItem{ItemDescription: "Eggs"})
		milk, eggs = added[0], added[1]
		var err error
		if _, trip, _, err = tx.CheckShoppingItem(world.owner.ID, milk.ID, true, "Example Market"); err != nil || trip == nil {
			t.Fatalf("CheckShoppingItem: %+v %v", trip, err)
		}
		if _, _, err := tx.FinishShoppingTrip(world.owner.ID, trip.ID); err != nil {
			t.Fatal(err)
		}
	})
	dbtest.Exec(t, world.database, `UPDATE "shopping_trip" SET "started_at" = now() - interval '7 hours',
		"finished_at" = now() - interval '2 hours 30 minutes' WHERE "id" = '`+trip.ID+`'`)
	world.run(t, func(tx db.Transaction) {
		if _, _, err := tx.ResumeShoppingTrip(world.owner.ID, trip.ID); err != nil {
			t.Fatalf("ResumeShoppingTrip: %v", err)
		}
		if _, started, _, err := tx.CheckShoppingItem(world.owner.ID, eggs.ID, true, ""); err != nil || started != nil {
			t.Fatalf("a check after a late resume started another trip: %+v %v", started, err)
		}
		inCart, _ := tx.GetShoppingItem(world.owner.ID, milk.ID)
		if inCart.ShoppingItemState != models.ShoppingItemStateInCart {
			t.Fatalf("the resumed milk left the cart: %+v", inCart)
		}
	})
}
