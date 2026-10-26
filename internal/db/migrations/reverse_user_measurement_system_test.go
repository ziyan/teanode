package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The measurement system is automatic for every account that exists before a
// person chooses, only the three known values are accepted, and the reverse
// file takes the column away.
func TestTheMeasurementSystemMigrationStartsAutomaticAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-units-user")
	if got := dbtest.QueryString(t, database, `SELECT "measurement_system" FROM "user" WHERE "id" = '`+userId+`'`); got != "" {
		t.Errorf("a new account reads measurements as %q, expected automatic", got)
	}
	for _, measurementSystem := range []string{"metric", "us", ""} {
		dbtest.Exec(t, database, `UPDATE "user" SET "measurement_system" = '`+measurementSystem+`' WHERE "id" = '`+userId+`'`)
		if got := dbtest.QueryString(t, database, `SELECT "measurement_system" FROM "user" WHERE "id" = '`+userId+`'`); got != measurementSystem {
			t.Errorf("stored %q, read %q", measurementSystem, got)
		}
	}
	for _, measurementSystem := range []string{"imperial", "Metric", "metrics"} {
		if err := execute(database, `UPDATE "user" SET "measurement_system" = '`+measurementSystem+`' WHERE "id" = '`+userId+`'`); err == nil {
			t.Errorf("the database accepted %q", measurementSystem)
		}
	}

	reverse, err := os.ReadFile("0262_user_measurement_system.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'user' AND column_name = 'measurement_system'`); got != "0" {
		t.Error("the user table still has the column")
	}
}
