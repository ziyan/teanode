package migrations_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestMigrate(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()

	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
}
