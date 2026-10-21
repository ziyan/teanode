package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The app mark belongs to the TeaNode phone app alone. An earlier migration
// gave it to every token a client held that named no resource, so the
// Passwords migration takes it off the tokens of any client that is not
// registered with exactly the TeaNode app's one return address.
func TestThePasswordsMigrationTakesTheAppMarkOffOtherAppsTokens(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "app-mark-user")
	for id, redirects := range map[string]string{
		"app-mark-teanode": "com.teanode.app:/oauth",
		"app-mark-other":   "com.example.app:/oauth",
		"app-mark-mixed":   "com.teanode.app:/oauth\nhttps://harness.example.com/",
	} {
		dbtest.Exec(t, database, `INSERT INTO "oauth_client" ("id", "created_at", "modified_at", "name", "redirect_uris") VALUES ('`+id+`', now(), now(), 'a client', '`+redirects+`')`)
		dbtest.Exec(t, database, `INSERT INTO "token" ("id", "created_at", "modified_at", "user_id", "name", "key_hash", "client_id", "resource", "client_kind")
			VALUES ('`+id+`', now(), now(), '`+userId+`', 'a token', 'hash', '`+id+`', '', 'app')`)
	}

	forward, err := os.ReadFile("0251_passwords.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	// The statement is run here over the rows above because the migration
	// itself ran before they existed.
	statement := string(forward)[strings.Index(string(forward), `UPDATE "token" SET "client_kind" = NULL`):]
	statement = statement[:strings.Index(statement, ";")+1]
	dbtest.Exec(t, database, statement)
	for id, expected := range map[string]string{"app-mark-teanode": "app", "app-mark-other": "", "app-mark-mixed": ""} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce("client_kind", '') FROM "token" WHERE "id" = '`+id+`'`); got != expected {
			t.Errorf("%s is marked %q, expected %q", id, got, expected)
		}
	}
}
