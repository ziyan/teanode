package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// execute runs a statement and returns its error, for the statements a test
// expects the database to refuse.
func execute(database db.Database, statement string) error {
	return database.(interface{ RawExec(string) error }).RawExec(statement)
}

// The calendar share migration refuses to be undone while a circle has a
// calendar, and with none it takes the share table and the circle column away
// and makes every calendar belong to an account again.
func TestTheCalendarShareMigrationReversesOnlyWithoutACircleCalendar(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-share-user")
	otherId := dbtest.CreateUser(t, database, "reverse-share-other")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "calendar" ("id", "user_id", "created_at", "modified_at") VALUES ('reverse-own', '` + userId + `', now(), now())`,
		`INSERT INTO "calendar_share" ("id", "calendar_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-share', 'reverse-own', '` + otherId + `', 'busy', now())`,
		`INSERT INTO "calendar" ("id", "circle_id", "created_at", "modified_at") VALUES ('reverse-family', 'reverse-circle', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	// A calendar must belong to exactly one of an account and a circle, and a
	// circle has one of each kind.
	for name, statement := range map[string]string{
		"neither":   `INSERT INTO "calendar" ("id", "created_at", "modified_at") VALUES ('reverse-none', now(), now())`,
		"both":      `INSERT INTO "calendar" ("id", "user_id", "circle_id", "created_at", "modified_at") VALUES ('reverse-both', '` + userId + `', 'reverse-circle', now(), now())`,
		"second":    `INSERT INTO "calendar" ("id", "circle_id", "created_at", "modified_at") VALUES ('reverse-second', 'reverse-circle', now(), now())`,
		"badlevel":  `INSERT INTO "calendar_share" ("id", "calendar_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-bad', 'reverse-family', '` + userId + `', 'owner', now())`,
		"nograntee": `INSERT INTO "calendar_share" ("id", "calendar_id", "share_level", "created_at") VALUES ('reverse-nobody', 'reverse-own', 'busy', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	reverse, err := os.ReadFile("0195_calendar_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	if err := execute(database, string(reverse)); err == nil || !strings.Contains(err.Error(), "a circle has a calendar of its own") {
		t.Fatalf("going back is refused while a circle has a calendar: %v", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "calendar_share"`); got != "1" {
		t.Errorf("a refused rollback deletes nothing: %s shares", got)
	}

	dbtest.Exec(t, database, `DELETE FROM "calendar" WHERE "id" = 'reverse-family'`)
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"calendar_share"')::text, '')`); got != "" {
		t.Errorf("the share table is still there: %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'calendar' AND column_name = 'circle_id'`); got != "0" {
		t.Errorf("the circle_id column is still there")
	}
	if got := dbtest.QueryString(t, database, `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'calendar' AND column_name = 'user_id'`); got != "NO" {
		t.Errorf("user_id is nullable again: %s", got)
	}
}
