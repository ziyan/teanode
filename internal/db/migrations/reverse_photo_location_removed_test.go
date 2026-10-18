package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A photo starts with its location in place, and the reverse file takes the
// column away.
func TestThePhotoLocationRemovedMigrationDefaultsToFalseAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT column_default FROM information_schema.columns WHERE table_name = 'photo' AND column_name = 'is_location_removed'`); got != "false" {
		t.Errorf("a new photo has its location removed: default %q", got)
	}

	reverse, err := os.ReadFile("0244_photo_location_removed.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'photo' AND column_name = 'is_location_removed'`); got != "0" {
		t.Errorf("the column is still there: %s", got)
	}
}
