package dbtest_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestAcquireDatabase(t *testing.T) {
	t.Parallel()
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()

	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to re-run migration: %s", err)
	}
}

func TestRunTransaction(t *testing.T) {
	t.Parallel()
	dbtest.RunTransaction(t, func(tx db.Transaction) {
		if err := tx.Commit(); err != nil {
			t.Fatalf("failed to commit transaction: %s", err)
		}
	})
}
