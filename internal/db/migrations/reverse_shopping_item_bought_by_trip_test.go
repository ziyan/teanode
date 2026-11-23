package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// An item names the trip that bought it, the name goes when the trip does,
// and the reverse file takes both columns away.
func TestTheBoughtByTripMigrationFollowsTheTripAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-bought-user")
	dbtest.Exec(t, database, `INSERT INTO "shopping_list" ("id", "user_id", "list_name", "created_at", "updated_at") VALUES ('reverse-bought-list', '`+userId+`', 'Groceries', now(), now())`)
	dbtest.Exec(t, database, `INSERT INTO "shopping_trip" ("id", "user_id", "started_at", "finished_at") VALUES ('reverse-bought-trip', '`+userId+`', now(), now())`)
	dbtest.Exec(t, database, `INSERT INTO "shopping_item" ("id", "shopping_list_id", "item_description", "shopping_item_state", "last_bought_at", "previous_last_bought_at", "bought_shopping_trip_id", "created_at", "updated_at")
		VALUES ('reverse-bought-item', 'reverse-bought-list', 'Milk', 'not_needed', now(), now() - interval '7 days', 'reverse-bought-trip', now(), now())`)
	dbtest.Exec(t, database, `DELETE FROM "shopping_trip" WHERE "id" = 'reverse-bought-trip'`)
	if got := dbtest.QueryString(t, database, `SELECT coalesce("bought_shopping_trip_id", '') FROM "shopping_item" WHERE "id" = 'reverse-bought-item'`); got != "" {
		t.Errorf("the item still names a deleted trip: %q", got)
	}

	reverse, err := os.ReadFile("0280_shopping_item_bought_by_trip.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'shopping_item' AND column_name IN ('bought_shopping_trip_id', 'previous_last_bought_at')`); got != "0" {
		t.Errorf("%s of the columns are still there", got)
	}
}
