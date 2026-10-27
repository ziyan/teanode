package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A list has a name and an owner, a share names one person or one circle, an
// item needs a description and a count above zero, its store and its key are
// set together, and an item in the cart says since when. The reverse file
// takes every table away, and shopping:use from every role.
func TestTheShoppingMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	ownerId := dbtest.CreateUser(t, database, "reverse-shopping-owner")
	granteeId := dbtest.CreateUser(t, database, "reverse-shopping-grantee")
	dbtest.Exec(t, database, `INSERT INTO "shopping_list" ("id", "user_id", "list_name", "created_at", "updated_at") VALUES ('reverse-list', '`+ownerId+`', 'Groceries', now(), now())`)
	dbtest.Exec(t, database, `INSERT INTO "shopping_item" ("id", "shopping_list_id", "item_description", "store_name", "store_key", "created_at", "updated_at")
		VALUES ('reverse-item', 'reverse-list', 'Apples', 'Example Market', 'examplemarket', now(), now())`)

	insertItem := func(id, columns, values string) string {
		return `INSERT INTO "shopping_item" ("id", "shopping_list_id", "created_at", "updated_at", ` + columns + `) VALUES ('` + id + `', 'reverse-list', now(), now(), ` + values + `)`
	}
	for name, statement := range map[string]string{
		"list without a name": `INSERT INTO "shopping_list" ("id", "user_id", "list_name", "created_at", "updated_at") VALUES ('reverse-unnamed', '` + ownerId + `', '', now(), now())`,
		"share to nobody":     `INSERT INTO "shopping_list_share" ("id", "shopping_list_id", "created_at") VALUES ('reverse-share-none', 'reverse-list', now())`,
		"share to both":       `INSERT INTO "shopping_list_share" ("id", "shopping_list_id", "grantee_user_id", "grantee_circle_id", "created_at") VALUES ('reverse-share-both', 'reverse-list', '` + granteeId + `', 'no-circle', now())`,
		"no description":      insertItem("reverse-blank", `"item_description"`, `''`),
		"zero needed":         insertItem("reverse-zero", `"item_description", "needed_quantity"`, `'Pears', 0`),
		"store without key":   insertItem("reverse-key", `"item_description", "store_name"`, `'Pears', 'Example Market'`),
		"cart without time":   insertItem("reverse-cart", `"item_description", "shopping_item_state"`, `'Pears', 'in_cart'`),
		"unknown state":       insertItem("reverse-state", `"item_description", "shopping_item_state"`, `'Pears', 'bought'`),
		"picture of nowhere":  insertItem("reverse-picture", `"item_description", "picture_file_id"`, `'Pears', 'file-one'`),
		"price below zero": `INSERT INTO "shopping_item_price" ("id", "shopping_item_id", "unit_price_amount", "observed_on", "price_source", "created_at")
			VALUES ('reverse-price', 'reverse-item', -1, '2026-10-26', 'person', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}
	dbtest.Exec(t, database, `INSERT INTO "shopping_list_share" ("id", "shopping_list_id", "grantee_user_id", "created_at") VALUES ('reverse-share', 'reverse-list', '`+granteeId+`', now())`)
	// A role that holds shopping:use, as the ordinary roles do on a server
	// that ran the migration, loses it with the reverse file.
	dbtest.Exec(t, database, `INSERT INTO "role" ("id", "name", "created_at", "modified_at") VALUES ('reverse-shopping-role', 'Shoppers', now(), now())`)
	dbtest.Exec(t, database, `INSERT INTO "role_permission" ("role_id", "permission_key") VALUES ('reverse-shopping-role', 'shopping:use')`)
	// Deleting the list takes its items and shares with it.
	dbtest.Exec(t, database, `DELETE FROM "shopping_list" WHERE "id" = 'reverse-list'`)
	if got := dbtest.QueryString(t, database, `SELECT (SELECT count(*) FROM "shopping_item") + (SELECT count(*) FROM "shopping_list_share")`); got != "0" {
		t.Errorf("%s items and shares are left after their list went", got)
	}

	reverse, err := os.ReadFile("0260_shopping.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"shopping_list", "shopping_list_share", "shopping_list_reminder_mute", "shopping_store_mute", "shopping_item",
		"shopping_item_price", "shopping_receipt_match", "shopping_trip", "shopping_trip_list"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "role_permission" WHERE "permission_key" = 'shopping:use'`); got != "0" {
		t.Errorf("shopping:use is still given to %s roles", got)
	}
}
