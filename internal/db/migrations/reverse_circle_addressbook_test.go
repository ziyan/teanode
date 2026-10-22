package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The circle address book migration refuses to be undone while a circle has an
// address book, deleting nothing, and with none it takes the circle column
// away and makes every address book belong to an account again.
func TestTheCircleAddressBookMigrationReversesOnlyWithoutACircleBook(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-book-user")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "addressbook" ("id", "user_id", "created_at", "modified_at") VALUES ('reverse-own', '` + userId + `', now(), now())`,
		`INSERT INTO "addressbook" ("id", "circle_id", "created_at", "modified_at") VALUES ('reverse-family', 'reverse-circle', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	// A book must belong to exactly one of an account and a circle, and a
	// circle has one.
	for name, statement := range map[string]string{
		"neither": `INSERT INTO "addressbook" ("id", "created_at", "modified_at") VALUES ('reverse-none', now(), now())`,
		"both":    `INSERT INTO "addressbook" ("id", "user_id", "circle_id", "created_at", "modified_at") VALUES ('reverse-both', '` + userId + `', 'reverse-circle', now(), now())`,
		"second":  `INSERT INTO "addressbook" ("id", "circle_id", "created_at", "modified_at") VALUES ('reverse-second', 'reverse-circle', now(), now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// The circle going takes its book, and the contacts in it, with it.
	dbtest.Exec(t, database, `INSERT INTO "contact" ("id", "addressbook_id", "created_at", "modified_at", "uid", "etag", "card") VALUES ('reverse-contact', 'reverse-family', now(), now(), 'reverse-uid', 'tag', 'card')`)

	reverse, err := os.ReadFile("0257_circle_addressbook.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	if err := execute(database, string(reverse)); err == nil || !strings.Contains(err.Error(), "a circle has an address book of its own") {
		t.Fatalf("going back is refused while a circle has an address book: %v", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "addressbook"`); got != "2" {
		t.Errorf("a refused rollback deletes nothing: %s address books", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "contact"`); got != "1" {
		t.Errorf("a refused rollback deletes nothing: %s contacts", got)
	}

	dbtest.Exec(t, database, `DELETE FROM "circle" WHERE "id" = 'reverse-circle'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "contact"`); got != "0" {
		t.Errorf("deleting the circle should take its contacts: %s contacts", got)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'addressbook' AND column_name = 'circle_id'`); got != "0" {
		t.Errorf("the circle_id column is still there")
	}
	if got := dbtest.QueryString(t, database, `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'addressbook' AND column_name = 'user_id'`); got != "NO" {
		t.Errorf("user_id is nullable again: %s", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "addressbook"`); got != "1" {
		t.Errorf("an account's address book was lost: %s address books", got)
	}
}
