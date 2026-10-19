package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The contact account migration keeps one linked contact per account in each
// address book, lets the same account be linked in another book, only clears
// the link when the account goes, and its reverse file takes the column away.
func TestTheContactAccountMigrationKeepsOnePerBookAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	ownerId := dbtest.CreateUser(t, database, "reverse-contact-owner")
	accountId := dbtest.CreateUser(t, database, "reverse-contact-account")
	for _, bookId := range []string{"reverse-book-one", "reverse-book-two"} {
		dbtest.Exec(t, database, `INSERT INTO "addressbook" ("id", "user_id", "created_at", "modified_at", "name") VALUES ('`+
			bookId+`', '`+ownerId+`', now(), now(), 'Contacts')`)
	}
	insert := func(id, bookId, uid, account string) string {
		return `INSERT INTO "contact" ("id", "addressbook_id", "created_at", "modified_at", "uid", "etag", "card", "name", "account_user_id") VALUES ('` +
			id + `', '` + bookId + `', now(), now(), '` + uid + `', 'e', 'card', 'Name', ` + account + `)`
	}
	dbtest.Exec(t, database, insert("reverse-contact-a", "reverse-book-one", "uid-a", "'"+accountId+"'"))
	dbtest.Exec(t, database, insert("reverse-contact-b", "reverse-book-two", "uid-b", "'"+accountId+"'"))
	dbtest.Exec(t, database, insert("reverse-contact-c", "reverse-book-one", "uid-c", "NULL"))
	dbtest.Exec(t, database, insert("reverse-contact-d", "reverse-book-one", "uid-d", "NULL"))

	if err := execute(database, insert("reverse-contact-e", "reverse-book-one", "uid-e", "'"+accountId+"'")); err == nil {
		t.Errorf("the database accepted a second contact for one account in one book")
	}
	if err := execute(database, insert("reverse-contact-f", "reverse-book-one", "uid-f", "'nobody-by-that-id'")); err == nil {
		t.Errorf("the database accepted a link to an account that does not exist")
	}

	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+accountId+`'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "contact"`); got != "4" {
		t.Errorf("a deleted account leaves %s contacts, wanted 4", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "contact" WHERE "account_user_id" IS NOT NULL`); got != "0" {
		t.Errorf("a deleted account leaves %s links", got)
	}

	reverse, err := os.ReadFile("0246_contact_account.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'contact' AND column_name = 'account_user_id'`); got != "0" {
		t.Errorf("the account_user_id column is still there")
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "contact"`); got != "4" {
		t.Errorf("reversing lost contacts: %s", got)
	}
}
