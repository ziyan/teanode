package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The token client kind marks the phone app's own sign-in. The migration
// marks the tokens that exist already, which are the ones that belong to a
// client and name no resource: a token minted by hand has no client, and every
// other program was given the agent tools endpoint as its resource. Only the
// one known kind is accepted, and the reverse file takes the column away.
func TestTheTokenClientKindMigrationMarksTheAppsTokensAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-kind-user")
	dbtest.Exec(t, database, `INSERT INTO "oauth_client" ("id", "created_at", "modified_at", "name", "redirect_uris") VALUES ('reverse-kind-client', now(), now(), 'a client', 'com.example.app:/oauth')`)
	for id, columns := range map[string]string{
		"reverse-kind-hand":    `NULL, ''`,
		"reverse-kind-program": `'reverse-kind-client', '/api/v1/mcp'`,
		"reverse-kind-app":     `'reverse-kind-client', ''`,
	} {
		dbtest.Exec(t, database, `INSERT INTO "token" ("id", "created_at", "modified_at", "user_id", "name", "key_hash", "client_id", "resource")
			VALUES ('`+id+`', now(), now(), '`+userId+`', 'a token', 'hash', `+columns+`)`)
	}

	// The marking is the last statement of the file, run here over the rows
	// above because the migration itself ran before they existed.
	forward, err := os.ReadFile("0198_token_client_kind.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	marking := string(forward)[strings.LastIndex(string(forward), "UPDATE"):]
	dbtest.Exec(t, database, marking)
	for id, expected := range map[string]string{"reverse-kind-hand": "", "reverse-kind-program": "", "reverse-kind-app": "app"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce("client_kind", '') FROM "token" WHERE "id" = '`+id+`'`); got != expected {
			t.Errorf("%s is marked %q, expected %q", id, got, expected)
		}
	}
	if err := execute(database, `UPDATE "token" SET "client_kind" = 'device' WHERE "id" = 'reverse-kind-hand'`); err == nil {
		t.Error("the database accepted a kind nothing knows")
	}

	reverse, err := os.ReadFile("0198_token_client_kind.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"token", "oauth_authorization", "oauth_connect_code"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = '`+table+`' AND column_name = 'client_kind'`); got != "0" {
			t.Errorf("the %s table still has the column", table)
		}
	}
}
