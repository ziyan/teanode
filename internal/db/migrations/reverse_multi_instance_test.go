package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A challenge is one of the two kinds the server answers, and the reverse
// file takes every table the multi-instance migration made away.
func TestTheMultiInstanceMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	if err := execute(database, `INSERT INTO "acme_challenge" ("challenge_key", "challenge_kind", "key_authorization", "expires_at")
		VALUES ('reverse-token', 'dns-01', 'reverse-key', now())`); err == nil {
		t.Errorf("the database accepted a challenge of a kind the server does not answer")
	}
	dbtest.Exec(t, database, `INSERT INTO "acme_challenge" ("challenge_key", "challenge_kind", "key_authorization", "expires_at")
		VALUES ('reverse-token', 'http-01', 'reverse-key', now())`)
	dbtest.Exec(t, database, `INSERT INTO "throttle" ("throttle_key", "taken_at") VALUES ('reverse-throttle', now())`)

	reverse, err := os.ReadFile("0264_multi_instance.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"throttle", "rate_limit_bucket", "agent_turn", "acme_challenge", "mcp_held_result",
		"agent_provider_sign_in", "agent_provider_access_token", "location_visit_notice", "agent_computer_attachment"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
}
