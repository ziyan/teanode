package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A card's media column starts as an empty list after the migration, and the
// reverse file takes it away.
func TestTheFeedMediaMigrationAddsTheColumnAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT column_default FROM information_schema.columns WHERE table_name = 'agent_feed_item' AND column_name = 'feed_media'`); got != "'[]'::jsonb" {
		t.Errorf("a card's media starts as %s, expected an empty list", got)
	}

	reverse, err := os.ReadFile("0277_agent_feed_media.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'agent_feed_item' AND column_name = 'feed_media'`); got != "0" {
		t.Error("the agent_feed_item table still has the column")
	}
}
