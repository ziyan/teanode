package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The migration lets a schedule run on arriving mail, and the reverse file
// removes the schedules that did, which have no time to run at without it,
// and keeps the rest.
func TestTheScheduleMailMigrationAddsTheConditionsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-schedule-mail-user")
	for _, statement := range []string{
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-schedule-mail-agent', now(), now(), '` + userId + `', 'Agent')`,
		`INSERT INTO "agent_schedule" ("id", "created_at", "modified_at", "agent_id", "name", "cron", "prompt") VALUES ('reverse-schedule-clock', now(), now(), 'reverse-schedule-mail-agent', 'Morning', '0 8 * * *', 'Say good morning')`,
		`INSERT INTO "agent_schedule" ("id", "created_at", "modified_at", "agent_id", "name", "cron", "prompt", "mail_conditions") VALUES ('reverse-schedule-mail', now(), now(), 'reverse-schedule-mail-agent', 'Receipts', '', 'File the receipt', '[{"field":"from","operator":"contains","value":"shop.example.com"}]')`,
	} {
		dbtest.Exec(t, database, statement)
	}

	reverse, err := os.ReadFile("0273_agent_schedule_mail.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	countColumns := `SELECT count(*)::text FROM information_schema.columns WHERE table_name = 'agent_schedule' AND column_name IN ('mail_conditions', 'mail_arrivals')`
	if got := dbtest.QueryString(t, database, countColumns); got != "0" {
		t.Errorf("agent_schedule still keeps %s of the mail columns", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT string_agg("id", ',' ORDER BY "id") FROM "agent_schedule"`); got != "reverse-schedule-clock" {
		t.Errorf("the reverse left the schedules %q, want only the one on a clock", got)
	}
}
