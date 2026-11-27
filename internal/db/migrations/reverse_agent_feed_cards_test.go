package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A card starts unresolved with no charts after the migration, and the reverse
// file takes all five columns away.
func TestTheFeedCardsMigrationAddsTheColumnsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	for column, expected := range map[string]string{
		"resolution_note": "''::text", "resolved_by_kind": "''::text", "resolved_by_id": "''::text", "chart_blocks": "'[]'::jsonb",
	} {
		if got := dbtest.QueryString(t, database, `SELECT column_default FROM information_schema.columns WHERE table_name = 'agent_feed_item' AND column_name = '`+column+`'`); got != expected {
			t.Errorf("%s starts as %s, expected %s", column, got, expected)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'agent_feed_item' AND column_name = 'resolved_at'`); got != "YES" {
		t.Errorf("resolved_at is nullable: %s", got)
	}

	reverse, err := os.ReadFile("0284_agent_feed_cards.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'agent_feed_item' AND column_name IN ('resolved_at', 'resolution_note', 'resolved_by_kind', 'resolved_by_id', 'chart_blocks')`); got != "0" {
		t.Errorf("%s of the columns are still there", got)
	}
}
