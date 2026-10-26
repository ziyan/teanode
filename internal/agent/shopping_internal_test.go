package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// A receipt is one system: reading a receipt from a mail queues the shopping
// match, and the match checks off the item the receipt bought, at the
// merchant's price, and leaves alone an item for another store, which the
// model is never shown.
func TestEveryReceiptChecksOffWhatTheShoppingListsNeeded(t *testing.T) {
	shoppingAnswer := `{"shoppingMatches": [{"lineNumber": 3, "itemNumber": 1, "quantity": "", "isSure": true}]}`
	fixture, model := receiptJobFixture(t, inventedReceiptAnswer, shoppingAnswer)
	var peachesId, toothpasteId string
	var mailbox *models.Mailbox
	var mail *models.Mail
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, fixture.owner.ID, models.PermissionShoppingUse)
		list, err := tx.CreateShoppingList(fixture.owner.ID, "Groceries")
		if err != nil {
			t.Fatalf("CreateShoppingList: %s", err)
		}
		items, _, err := tx.AddShoppingItems(fixture.owner.ID, list.ID, []*models.ShoppingItem{
			{ItemDescription: "Peaches", NeededQuantity: "1"},
			{ItemDescription: "Toothpaste", StoreName: "Elsewhere Pharmacy"},
		})
		if err != nil {
			t.Fatalf("AddShoppingItems: %s", err)
		}
		peachesId, toothpasteId = items[0].ID, items[1].ID
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if mail, err = tx.CreateMail(&models.Mail{Subject: "Your receipt", From: "receipts@example.com", Kind: models.MailKindIncoming}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
	})
	if err := fixture.worker.settings.Storage.Put(t.Context(), mail.ID, []string{"From: receipts@example.com", "Subject: Your receipt", "Content-Type: text/plain"}, []byte(inventedReceiptEmail)); err != nil {
		t.Fatalf("Put: %s", err)
	}
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(mailbox, mail.ID)); err != nil {
		t.Fatalf("runReadReceipt: %s", err)
	}
	receipts := fixture.receipts(t)
	if len(receipts) != 1 {
		t.Fatalf("%d receipts", len(receipts))
	}
	if got := dbtest.QueryString(t, fixture.database, `SELECT count(*)::text FROM "agent_job" WHERE "kind" = 'match_shopping' AND "subject_id" = '`+receipts[0].ID+`'`); got != "1" {
		t.Fatalf("the receipt queued %s shopping matches, want 1", got)
	}

	run := fixture.run()
	run.Job = &models.AgentJob{ID: "job-shopping", AgentID: fixture.agent.ID, Kind: models.AgentJobMatchShopping, SubjectID: receipts[0].ID}
	if err := fixture.worker.runMatchShopping(t.Context(), run); err != nil {
		t.Fatalf("runMatchShopping: %s", err)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		peaches, err := tx.GetShoppingItem(fixture.owner.ID, peachesId)
		if err != nil {
			t.Fatal(err)
		}
		if peaches.ShoppingItemState != models.ShoppingItemStateNotNeeded || peaches.UnitPriceAmount != "3.99" {
			t.Fatalf("the peaches the receipt bought: %+v", peaches)
		}
		prices, err := tx.ListShoppingItemPrices(fixture.owner.ID, peachesId)
		if err != nil || len(prices) != 1 || prices[0].StoreName != "Maple Street Market" || prices[0].FinanceReceiptID != receipts[0].ID {
			t.Fatalf("the peaches' price: %+v %v", prices, err)
		}
		toothpaste, _ := tx.GetShoppingItem(fixture.owner.ID, toothpasteId)
		if toothpaste.ShoppingItemState != models.ShoppingItemStateNeeded {
			t.Fatalf("an item for another store was checked off: %+v", toothpaste)
		}
	})
	model.mutex.Lock()
	defer model.mutex.Unlock()
	if len(model.prompts) < 2 {
		t.Fatalf("the model was asked %d times", len(model.prompts))
	}
	prompt := model.prompts[len(model.prompts)-1]
	if !strings.Contains(prompt, "PEACHES") || !strings.Contains(prompt, "Peaches") || strings.Contains(prompt, "Toothpaste") {
		t.Errorf("the match prompt shows the wrong items: %s", prompt)
	}
}

// A place is the store the person named it, else its name on the map; things
// for any store are bought only at a grocery-type shop.
func TestAPlaceIsTheStoreItIsNamed(t *testing.T) {
	for _, each := range []struct {
		place     *models.Place
		storeName string
		isGrocery bool
	}{
		{&models.Place{PlaceName: "Example Market", OSMName: "Example Market", OSMCategory: "shop=supermarket"}, "Example Market", true},
		{&models.Place{PlaceName: "The good market", IsNamedByPerson: true, OSMName: "Example Market", OSMCategory: "shop=supermarket"}, "The good market", true},
		{&models.Place{PlaceName: "Downtown", OSMName: "Corner Pharmacy", OSMCategory: "amenity=pharmacy"}, "Corner Pharmacy", false},
		{&models.Place{PlaceName: "Somewhere"}, "Somewhere", false},
		{nil, "", false},
	} {
		storeName, isGrocery := ShoppingStoreOfPlace(each.place)
		if storeName != each.storeName || isGrocery != each.isGrocery {
			t.Errorf("%+v is %q (grocery %v), want %q (%v)", each.place, storeName, isGrocery, each.storeName, each.isGrocery)
		}
	}
}

// The arrival reminder names a few items of each list and how many more,
// and a store brand's picture is searched for under the store's name.
func TestTheArrivalReminderNamesAFewItemsOfEachList(t *testing.T) {
	names := map[string][]string{
		"Groceries": {"Soup", "2 Apples", "Oranges", "Chili", "Bread", "Milk", "Eggs"},
		"Party":     {"Napkins"},
	}
	body := shoppingArrivalBody([]string{"Groceries", "Party"}, names, push.LanguageEnglish)
	want := "Groceries needs Soup, 2 Apples, Oranges, Chili, Bread and 2 more. Party needs Napkins."
	if body != want {
		t.Errorf("the reminder says %q, want %q", body, want)
	}
	if query := shoppingPictureQuery(&models.ShoppingItem{ItemDescription: "Soup", StoreName: "Example Market", IsStoreBrand: true}); query != "Example Market Soup" {
		t.Errorf("a store brand is searched as %q", query)
	}
	if query := shoppingPictureQuery(&models.ShoppingItem{ItemDescription: "Tomatoes"}); query != "Tomatoes grocery" {
		t.Errorf("a plain item is searched as %q", query)
	}
}

// The same notice is claimed once in its interval, and again after it.
func TestAShoppingNoticeIsClaimedOncePerInterval(t *testing.T) {
	var notices shoppingNotices
	now := time.Now()
	if !notices.claim("arrival one", time.Hour, now) {
		t.Fatal("the first notice was not claimed")
	}
	if notices.claim("arrival one", time.Hour, now.Add(30*time.Minute)) {
		t.Error("the same notice was claimed again within its interval")
	}
	if !notices.claim("arrival two", time.Hour, now.Add(30*time.Minute)) {
		t.Error("another notice was refused")
	}
	if !notices.claim("arrival one", time.Hour, now.Add(2*time.Hour)) {
		t.Error("the notice was refused after its interval")
	}
}
