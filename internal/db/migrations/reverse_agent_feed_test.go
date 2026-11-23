package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The feed's table and the agent's destination column exist after the
// migration, an agent starts with the feed as its destination, and the
// reverse file takes both away.
func TestTheFeedMigrationAddsTheTableAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.tables WHERE table_name = 'agent_feed_item'`); got != "1" {
		t.Fatal("the agent_feed_item table is missing")
	}
	if got := dbtest.QueryString(t, database, `SELECT column_default FROM information_schema.columns WHERE table_name = 'agent' AND column_name = 'feed_destination'`); got != "'feed'::text" {
		t.Errorf("an agent's feed destination starts as %s, expected feed", got)
	}

	// Schedules became goals after the feed, and the goals' migration
	// gives them back their table, which the feed's reverse changes.
	for _, filename := range []string{"0281_goal_takes_schedules.reverse.sql", "0276_agent_feed.reverse.sql"} {
		reverse, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("ReadFile: %s", err)
		}
		dbtest.Exec(t, database, string(reverse))
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.tables WHERE table_name = 'agent_feed_item'`); got != "0" {
		t.Error("the agent_feed_item table is still there")
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'agent' AND column_name = 'feed_destination'`); got != "0" {
		t.Error("the agent table still has the column")
	}
}
