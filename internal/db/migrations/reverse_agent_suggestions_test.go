package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The ideas and memory check switches become one switch for suggestions: an
// agent whose person had switched either off stays quiet about both, one with
// both on suggests, and the reverse gives each switch the one switch's value.
func TestTheSuggestionsMigrationMergesTheSwitchesAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	reverse, err := os.ReadFile("0282_agent_suggestions.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	forward, err := os.ReadFile("0282_agent_suggestions.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	userId := dbtest.CreateUser(t, database, "suggestions-user")
	otherId := dbtest.CreateUser(t, database, "suggestions-other")
	dbtest.Exec(t, database, `INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name", "is_ideas_enabled", "is_memory_check_enabled")
		VALUES ('suggestions-quiet', now(), now(), '`+userId+`', 'Quiet', false, true),
			('suggestions-on', now(), now(), '`+otherId+`', 'On', true, true)`)
	dbtest.Exec(t, database, string(forward))
	for agentId, wanted := range map[string]string{"suggestions-quiet": "false", "suggestions-on": "true"} {
		if got := dbtest.QueryString(t, database, `SELECT "is_suggestions_enabled"::text FROM "agent" WHERE "id" = '`+agentId+`'`); got != wanted {
			t.Errorf("agent %s suggests: %s, wanted %s", agentId, got, wanted)
		}
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT "is_ideas_enabled"::text || ' ' || "is_memory_check_enabled"::text FROM "agent" WHERE "id" = 'suggestions-quiet'`); got != "false false" {
		t.Errorf("back again, the quiet agent's switches are %s", got)
	}
	dbtest.Exec(t, database, string(forward))
}
