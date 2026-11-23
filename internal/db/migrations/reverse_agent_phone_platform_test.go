package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A phone registered before Android was served is an iPhone, the column
// accepts only the two platforms, and the reverse file takes it away.
func TestThePhonePlatformMigrationStartsEveryPhoneAnIPhoneAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	const columnQuery = `SELECT %s FROM information_schema.columns WHERE table_name = 'agent_phone' AND column_name = 'phone_platform'`
	if got := dbtest.QueryString(t, database, strings.Replace(columnQuery, "%s", "column_default", 1)); !strings.HasPrefix(got, "'ios'") {
		t.Errorf("a phone with no platform is %s, expected ios", got)
	}
	definition := dbtest.QueryString(t, database, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'agent_phone'::regclass AND pg_get_constraintdef(oid) LIKE '%phone_platform%'`)
	if !strings.Contains(definition, "'ios'") || !strings.Contains(definition, "'android'") {
		t.Errorf("the platform is checked as %q, expected ios and android", definition)
	}

	reverse, err := os.ReadFile("0275_agent_phone_platform.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'agent_phone' AND column_name = 'phone_platform'`); got != "0" {
		t.Error("the agent_phone table still has the column")
	}
}
