package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The location pause migration refuses a pause that ends when or before it
// begins, lets one last until resumed, goes with the account, and its reverse
// file takes the table away.
func TestTheLocationPauseMigrationRefusesAnEmptyPauseAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-pause-user")
	for _, statement := range []string{
		`INSERT INTO "location_pause" ("id", "user_id", "started_at", "ended_at", "created_at", "updated_at") VALUES ('reverse-timed-pause', '` + userId + `', '2026-01-01T14:00:00Z', '2026-01-01T15:00:00Z', now(), now())`,
		`INSERT INTO "location_pause" ("id", "user_id", "started_at", "created_at", "updated_at") VALUES ('reverse-open-pause', '` + userId + `', '2026-01-02T14:00:00Z', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"endsBeforeItBegins": `INSERT INTO "location_pause" ("id", "user_id", "started_at", "ended_at", "created_at", "updated_at") VALUES ('reverse-backwards', '` + userId + `', '2026-01-03T14:00:00Z', '2026-01-03T13:00:00Z', now(), now())`,
		"endsWhenItBegins":   `INSERT INTO "location_pause" ("id", "user_id", "started_at", "ended_at", "created_at", "updated_at") VALUES ('reverse-empty', '` + userId + `', '2026-01-03T14:00:00Z', '2026-01-03T14:00:00Z', now(), now())`,
		"noPerson":           `INSERT INTO "location_pause" ("id", "user_id", "started_at", "created_at", "updated_at") VALUES ('reverse-nobody', 'nobody', '2026-01-03T14:00:00Z', now(), now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// The account takes its pauses with it.
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+userId+`'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "location_pause"`); got != "0" {
		t.Errorf("a deleted account leaves %s pauses", got)
	}

	reverse, err := os.ReadFile("0257_location_pause.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"location_pause"')::text, '')`); got != "" {
		t.Errorf("the location_pause table is still there: %q", got)
	}
}
