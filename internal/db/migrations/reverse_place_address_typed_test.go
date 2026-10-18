package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A place starts with an address nobody typed, and the reverse file takes the
// column away.
func TestThePlaceAddressTypedMigrationDefaultsToFalseAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-typed-user")
	dbtest.Exec(t, database, `INSERT INTO "place" ("id", "user_id", "latitude_degrees", "longitude_degrees", "created_at", "updated_at") VALUES ('reverse-typed-place', '`+userId+`', 10.5, 20.5, now(), now())`)
	if got := dbtest.QueryString(t, database, `SELECT is_address_typed::text FROM "place" WHERE "id" = 'reverse-typed-place'`); got != "false" {
		t.Errorf("a new place has a typed address: %q", got)
	}

	reverse, err := os.ReadFile("0241_place_address_typed.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'place' AND column_name = 'is_address_typed'`); got != "0" {
		t.Errorf("the column is still there: %s", got)
	}
}
