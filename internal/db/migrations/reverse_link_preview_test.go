package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The reverse file takes the table and its indexes away.
func TestTheLinkPreviewMigrationReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'link_preview'`); got != "1" {
		t.Fatalf("the table was not made: %s", got)
	}

	reverse, err := os.ReadFile("0250_link_preview.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'link_preview'`); got != "0" {
		t.Errorf("the table is still there: %s", got)
	}
}
