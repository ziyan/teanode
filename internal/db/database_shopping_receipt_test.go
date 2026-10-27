package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// agentOf is the person's agent, made when they have none.
func agentOf(t *testing.T, tx db.Transaction, userId string) string {
	t.Helper()
	agent, err := tx.GetAgentByUser(userId)
	if err == nil && agent == nil {
		agent, err = tx.CreateAgent(&models.Agent{UserID: userId, Enabled: true})
	}
	if err != nil || agent == nil {
		t.Fatalf("the agent of %s: %v", userId, err)
	}
	return agent.ID
}

// itemOf reads an item as its list's owner sees it.
func itemOf(t *testing.T, tx db.Transaction, userId, itemId string) *models.ShoppingItem {
	t.Helper()
	item, err := tx.GetShoppingItem(userId, itemId)
	if err != nil {
		t.Fatalf("GetShoppingItem: %s", err)
	}
	return item
}

// Reading a receipt again replaces its lines, and what was decided about them
// stays: the applied match can still be undone, and a dismissed pair is not
// proposed again.
func TestAReceiptReadAgainKeepsItsShoppingMatches(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		items := mustAddShoppingItems(t, tx, world.owner.ID, list.ID,
			&models.ShoppingItem{ItemDescription: "Oat flakes"}, &models.ShoppingItem{ItemDescription: "Dish soap"})
		receipt, err := tx.PutFinanceReceipt(inventedGroceryReceipt(world.ownerAgentId, "mail-read-twice"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, receipt.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: receipt.ReceiptLines[0].ID, ShoppingItemID: items[0].ID, IsSure: true},
			{FinanceReceiptLineID: receipt.ReceiptLines[3].ID, ShoppingItemID: items[1].ID, IsSure: false},
		}); err != nil {
			t.Fatal(err)
		}
		matches, _ := tx.ListShoppingReceiptMatches(world.owner.ID, receipt.ID)
		for _, match := range matches {
			if match.ShoppingItemID == items[1].ID {
				if _, _, err := tx.DecideShoppingReceiptMatch(world.owner.ID, match.ID, false); err != nil {
					t.Fatal(err)
				}
			}
		}
		again, err := tx.PutFinanceReceipt(inventedGroceryReceipt(world.ownerAgentId, "mail-read-twice"))
		if err != nil || again.ID != receipt.ID || again.ReceiptLines[0].ID == receipt.ReceiptLines[0].ID {
			t.Fatalf("the receipt was not read again with new lines: %v", err)
		}
		kept, err := tx.ListShoppingReceiptMatches(world.owner.ID, receipt.ID)
		if err != nil || len(kept) != 2 || kept[0].FinanceReceiptLineID != again.ReceiptLines[0].ID {
			t.Fatalf("the matches after the receipt was read again: %+v %v", kept, err)
		}
		written, _, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, receipt.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: again.ReceiptLines[3].ID, ShoppingItemID: items[1].ID, IsSure: true},
		})
		if err != nil || len(written) != 0 {
			t.Fatalf("a dismissed pair was proposed again: %d %v", len(written), err)
		}
		if _, err := tx.UndoShoppingReceipt(world.owner.ID, receipt.ID); err != nil {
			t.Fatal(err)
		}
		if oats := itemOf(t, tx, world.owner.ID, items[0].ID); oats.ShoppingItemState != models.ShoppingItemStateNeeded || oats.UnitPriceAmount != "" {
			t.Fatalf("undo after a second reading did not put the oats back: %+v", oats)
		}
	})
}

// Undo puts back what applying changed (the out-of-stock mark, when it was
// last bought, a price typed with the item), and leaves an item changed since
// as it was changed.
func TestUndoingAReceiptPutsBackExactlyWhatItChanged(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		items := mustAddShoppingItems(t, tx, world.owner.ID, list.ID,
			&models.ShoppingItem{ItemDescription: "Oat flakes", UnitPriceAmount: "5"}, &models.ShoppingItem{ItemDescription: "Dish soap"})
		if _, err := tx.SetShoppingItemOutOfStock(world.owner.ID, items[0].ID, true, "Corner Grocer"); err != nil {
			t.Fatal(err)
		}
		receipt, err := tx.PutFinanceReceipt(inventedGroceryReceipt(world.ownerAgentId, "mail-undo-exact"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, receipt.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: receipt.ReceiptLines[0].ID, ShoppingItemID: items[0].ID, IsSure: true},
			{FinanceReceiptLineID: receipt.ReceiptLines[3].ID, ShoppingItemID: items[1].ID, IsSure: true},
		}); err != nil {
			t.Fatal(err)
		}
	})
	// The soap is needed again, two of them, after the receipt was applied.
	dbtest.Exec(t, world.database, `UPDATE "shopping_receipt_match" SET "decided_at" = now() - interval '1 minute'`)
	dbtest.Exec(t, world.database, `UPDATE "shopping_item" SET "updated_at" = now() - interval '2 minutes'`)
	var items []*models.ShoppingItem
	world.run(t, func(tx db.Transaction) {
		lists, _ := tx.ListShoppingLists(world.owner.ID)
		items, _, _ = tx.ListShoppingItems(world.owner.ID, lists[0].ID, nil, 0, 10)
		if _, err := tx.SetShoppingItemNeed(world.owner.ID, items[1].ID, true, "2"); err != nil {
			t.Fatal(err)
		}
		receipts, err := tx.ListFinanceReceipts(world.ownerAgentId, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.UndoShoppingReceipt(world.owner.ID, receipts.FinanceReceipts[0].ID); err != nil {
			t.Fatal(err)
		}
		oats := itemOf(t, tx, world.owner.ID, items[0].ID)
		if oats.ShoppingItemState != models.ShoppingItemStateNeeded || oats.OutOfStockStoreName != "Corner Grocer" || oats.LastBoughtAt != nil || oats.UnitPriceAmount != "5" {
			t.Fatalf("undo did not put the oats back exactly: %+v", oats)
		}
		soap := itemOf(t, tx, world.owner.ID, items[1].ID)
		if soap.ShoppingItemState != models.ShoppingItemStateNeeded || soap.NeededQuantity != "2" {
			t.Fatalf("undo overwrote a change made since: %+v", soap)
		}
	})
}

// A person who no longer reaches a list cannot change its item by tying the
// receipt line that bought it to something else.
func TestTyingALineAgainLeavesAnItemOnAListNoLongerReached(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	var ownersItemId, samsItemId, lineId, shareId string
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		share, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: list.ID, GranteeUserID: world.sam.ID})
		if err != nil {
			t.Fatal(err)
		}
		shareId = share.ID
		ownersItemId = mustAddShoppingItems(t, tx, world.owner.ID, list.ID, &models.ShoppingItem{ItemDescription: "Oat flakes"})[0].ID
		samsList := mustCreateShoppingList(t, tx, world.sam.ID, "Mine")
		samsItemId = mustAddShoppingItems(t, tx, world.sam.ID, samsList.ID, &models.ShoppingItem{ItemDescription: "Oats"})[0].ID
		receipt, err := tx.PutFinanceReceipt(inventedGroceryReceipt(agentOf(t, tx, world.sam.ID), "mail-sam"))
		if err != nil {
			t.Fatal(err)
		}
		lineId = receipt.ReceiptLines[0].ID
		if _, _, err := tx.ApplyShoppingReceiptMatches(world.sam.ID, receipt.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: lineId, ShoppingItemID: ownersItemId, IsSure: true},
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.DeleteShoppingListShare(world.owner.ID, shareId); err != nil {
			t.Fatal(err)
		}
		if _, _, err := tx.MatchShoppingReceiptLine(world.sam.ID, lineId, samsItemId); err != nil {
			t.Fatalf("MatchShoppingReceiptLine: %s", err)
		}
		if oats := itemOf(t, tx, world.owner.ID, ownersItemId); oats.ShoppingItemState != models.ShoppingItemStateNotNeeded || oats.UnitPriceAmount == "" {
			t.Fatalf("sam changed an item on a list he no longer reaches: %+v", oats)
		}
		if mine := itemOf(t, tx, world.sam.ID, samsItemId); mine.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("sam's own item was not checked off: %+v", mine)
		}
	})
}

// Done buys what the shopper put in the cart on a list switched on after the
// trip began; an item in somebody's cart stays in theirs, a not needed item
// cannot be checked, and a count that rounds to nothing is refused.
func TestTheEdgesOfShoppingAStore(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		groceries := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		party := mustCreateShoppingList(t, tx, world.owner.ID, "Party")
		if _, err := tx.ShareShoppingList(world.owner.ID, &models.ShoppingListShare{ShoppingListID: groceries.ID, GranteeUserID: world.sam.ID}); err != nil {
			t.Fatal(err)
		}
		bread := mustAddShoppingItems(t, tx, world.owner.ID, groceries.ID, &models.ShoppingItem{ItemDescription: "Bread"})[0]
		napkins := mustAddShoppingItems(t, tx, world.owner.ID, party.ID, &models.ShoppingItem{ItemDescription: "Napkins"})[0]
		trip, _, err := tx.StartShoppingTrip(world.owner.ID, "Example Market", []string{groceries.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, napkins.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, bread.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if again, _, _, err := tx.CheckShoppingItem(world.sam.ID, bread.ID, true, ""); err != nil || again.CartedByUserID != world.owner.ID {
			t.Fatalf("sam took the bread out of the owner's cart: %+v %v", again, err)
		}
		_, bought, err := tx.FinishShoppingTrip(world.owner.ID, trip.ID)
		if err != nil {
			t.Fatal(err)
		}
		// Both lists had something bought, so both are announced; nothing else.
		if len(bought) != 2 {
			t.Fatalf("Done says it bought on %v, want the two lists", bought)
		}
		if bought := itemOf(t, tx, world.owner.ID, napkins.ID); bought.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("the napkins of a list switched on at the store were not bought: %+v", bought)
		}
		if _, _, _, err := tx.CheckShoppingItem(world.owner.ID, napkins.ID, true, ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a not needed item was put in the cart: %v", err)
		}
		if marked, err := tx.SetShoppingItemOutOfStock(world.owner.ID, napkins.ID, true, ""); err != nil || marked.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("out of stock made a not needed item needed: %+v %v", marked, err)
		}
		if _, _, err := tx.AddShoppingItems(world.owner.ID, groceries.ID, []*models.ShoppingItem{{ItemDescription: "Salt", NeededQuantity: "0.0004"}}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a count that rounds to nothing was not refused: %v", err)
		}
	})
}

// A line that adds an item it bought, undone, leaves the item bought rather
// than on the list to buy; a weighed line without a unit price is priced by
// its weight.
func TestAReceiptLineAddedToAListStaysBoughtWhenUndone(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		receipt := inventedGroceryReceipt(world.ownerAgentId, "mail-weighed")
		receipt.ReceiptLines[1].UnitPriceAmount = ""
		stored, err := tx.PutFinanceReceipt(receipt)
		if err != nil {
			t.Fatal(err)
		}
		pears, err := tx.AddShoppingItemFromReceiptLine(world.owner.ID, stored.ReceiptLines[1].ID, list.ID)
		if err != nil {
			t.Fatal(err)
		}
		if pears.ShoppingItemState != models.ShoppingItemStateNotNeeded || pears.UnitPriceAmount != "3" {
			t.Fatalf("1.5 lb for 4.50 is 3 a pound, bought: %+v", pears)
		}
		if _, err := tx.UndoShoppingReceipt(world.owner.ID, stored.ID); err != nil {
			t.Fatal(err)
		}
		if after := itemOf(t, tx, world.owner.ID, pears.ID); after.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("undoing put a bought item on the list to buy: %+v", after)
		}
	})
	_ = time.Now
}

// Two lines tied to one item undo in turn and put it back as it was; a line
// added to a list twice writes its price once; a receipt line whose quantity is
// zero does not refuse the rest of the receipt; and a receipt read again with
// its lines numbered differently does not apply a match to another line.
func TestTheSecondRoundOfReceiptEdges(t *testing.T) {
	t.Parallel()
	world := newShoppingWorld(t)
	world.run(t, func(tx db.Transaction) {
		list := mustCreateShoppingList(t, tx, world.owner.ID, "Groceries")
		items := mustAddShoppingItems(t, tx, world.owner.ID, list.ID,
			&models.ShoppingItem{ItemDescription: "Oat flakes", NeededQuantity: "3"}, &models.ShoppingItem{ItemDescription: "Dish soap"})
		receipt := inventedGroceryReceipt(world.ownerAgentId, "mail-second-round")
		receipt.ReceiptLines[3].Quantity = "0"
		stored, err := tx.PutFinanceReceipt(receipt)
		if err != nil {
			t.Fatal(err)
		}
		lines := stored.ReceiptLines
		// A zero quantity on the soap's line does not refuse the batch.
		if _, _, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, stored.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: lines[3].ID, ShoppingItemID: items[1].ID, IsSure: true},
		}); err != nil {
			t.Fatalf("a zero quantity refused the receipt: %s", err)
		}
		// Two lines tied by hand to the oats, one of each, then undone.
		if _, _, err := tx.MatchShoppingReceiptLine(world.owner.ID, lines[0].ID, items[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := tx.MatchShoppingReceiptLine(world.owner.ID, lines[1].ID, items[0].ID); err != nil {
			t.Fatal(err)
		}
		if oats := itemOf(t, tx, world.owner.ID, items[0].ID); oats.ShoppingItemState != models.ShoppingItemStateNotNeeded {
			t.Fatalf("the oats were not bought: %+v", oats)
		}
		if _, err := tx.UndoShoppingReceipt(world.owner.ID, stored.ID); err != nil {
			t.Fatal(err)
		}
		if oats := itemOf(t, tx, world.owner.ID, items[0].ID); oats.ShoppingItemState != models.ShoppingItemStateNeeded || oats.NeededQuantity != "3" {
			t.Fatalf("undo did not put the oats back: %+v", oats)
		}
		// The pears' line added twice is one item with one price.
		first, err := tx.AddShoppingItemFromReceiptLine(world.owner.ID, lines[1].ID, list.ID)
		if err != nil {
			t.Fatal(err)
		}
		second, err := tx.AddShoppingItemFromReceiptLine(world.owner.ID, lines[1].ID, list.ID)
		if err != nil || second.ID != first.ID {
			t.Fatalf("adding the line again made another item: %v", err)
		}
		if prices, _ := tx.ListShoppingItemPrices(world.owner.ID, first.ID); len(prices) != 1 {
			t.Fatalf("adding the line twice wrote %d prices", len(prices))
		}
	})
	// The receipt read again with the oats and the pears swapped: a match
	// proposed for line 1 (the oats) is not applied to what line 1 is now.
	world.run(t, func(tx db.Transaction) {
		lists, _ := tx.ListShoppingLists(world.owner.ID)
		items, _, _ := tx.ListShoppingItems(world.owner.ID, lists[0].ID, []models.ShoppingItemState{models.ShoppingItemStateNeeded}, 0, 10)
		var soapId string
		for _, item := range items {
			if item.ItemDescription == "Dish soap" {
				soapId = item.ID
			}
		}
		receipts, _ := tx.ListFinanceReceipts(world.ownerAgentId, nil)
		stored := receipts.FinanceReceipts[0]
		written, _, err := tx.ApplyShoppingReceiptMatches(world.owner.ID, stored.ID, []*models.ShoppingReceiptMatchProposal{
			{FinanceReceiptLineID: stored.ReceiptLines[3].ID, ShoppingItemID: soapId, IsSure: false},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(written) == 0 {
			// The soap's line was undone, hence dismissed, and is not proposed again.
			return
		}
		t.Fatalf("a dismissed line was proposed again: %+v", written)
	})
}
