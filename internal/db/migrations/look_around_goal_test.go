package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// Each agent that was on and had alerts on gets a goal called "Look around",
// built in, at the day and time its settings named (Thursday at 17:00 when
// they named none). One that had alerts off, or was switched off, gets none,
// and the job's queued runs go. Run again it makes nothing twice, and its
// reverse takes away only the goals still as the migration made them.
func TestTheLookAroundMigrationMakesAGoalForEachAgentThatWasLookingAround(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	for _, person := range []struct{ name, timezone string }{{"around-default", ""}, {"around-chosen", "Europe/London"}, {"around-quiet", ""}, {"around-off", ""}, {"around-bad-zone", "Nowhere/Invented"}} {
		userId := dbtest.CreateUser(t, database, person.name)
		dbtest.Exec(t, database, `UPDATE "user" SET "timezone" = '`+person.timezone+`' WHERE "id" = '`+userId+`'`)
		enabled, alerts := "true", "true"
		weekday, clock := "", ""
		switch person.name {
		case "around-chosen":
			weekday, clock = "Friday", "18:30"
		case "around-quiet":
			alerts = "false"
		case "around-off":
			enabled = "false"
		}
		dbtest.Exec(t, database, `INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name", "enabled", "is_alerts_enabled", "look_around_weekday", "look_around_time")
			VALUES ('`+person.name+`-agent', now(), now(), '`+userId+`', 'Agent', `+enabled+`, `+alerts+`, '`+weekday+`', '`+clock+`')`)
	}
	dbtest.Exec(t, database, `INSERT INTO "agent_job" ("id", "created_at", "agent_id", "kind", "subject_id", "status")
		VALUES ('around-job', now(), 'around-default-agent', 'nearby', '2026-11-26T17:00', 'queued')`)

	forward, err := os.ReadFile("0285_look_around_goal.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	// The migration ran before the rows above existed, so it is run here
	// over them, twice: it makes each goal once.
	dbtest.Exec(t, database, string(forward))
	dbtest.Exec(t, database, string(forward))

	count := func(where string) string {
		return dbtest.QueryString(t, database, `SELECT count(*)::text FROM "agent_conversation" WHERE "kind" = 'goal' AND "goal_title" = 'Look around' AND `+where)
	}
	if got := count(`"goal_written_by" = 'built_in'`); got != "3" {
		t.Errorf("%s goals were made, want 3: the default, the chosen and the bad zone", got)
	}
	for _, agent := range []string{"around-quiet-agent", "around-off-agent"} {
		if got := count(`"agent_id" = '` + agent + `'`); got != "0" {
			t.Errorf("%s has %s look around goals, want none", agent, got)
		}
	}
	for agent, times := range map[string]string{
		"around-default-agent":  `["0 17 * * 4"]`,
		"around-chosen-agent":   `["30 18 * * 5"]`,
		"around-bad-zone-agent": `["0 17 * * 4"]`,
	} {
		if got := dbtest.QueryString(t, database, `SELECT "goal_times"::text FROM "agent_conversation" WHERE "agent_id" = '`+agent+`' AND "goal_title" = 'Look around'`); got != times {
			t.Errorf("%s runs at %s, want %s", agent, got, times)
		}
		if got := dbtest.QueryString(t, database, `SELECT ("goal_state" = 'working' AND "goal_next_at" > now() AND "goal_next_at" <= now() + interval '8 days')::text FROM "agent_conversation" WHERE "agent_id" = '`+agent+`' AND "goal_title" = 'Look around'`); got != "true" {
			t.Errorf("%s is not working with its next turn within a week", agent)
		}
	}
	// The next turn is on the chosen day, at the chosen time, in the
	// person's own zone.
	if got := dbtest.QueryString(t, database, `SELECT to_char("goal_next_at" AT TIME ZONE 'Europe/London', 'Dy HH24:MI') FROM "agent_conversation" WHERE "agent_id" = 'around-chosen-agent' AND "goal_title" = 'Look around'`); got != "Fri 18:30" {
		t.Errorf("the chosen goal is next due %s, want Fri 18:30", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "agent_job" WHERE "kind" = 'nearby'`); got != "0" {
		t.Errorf("%s look around jobs are still queued", got)
	}

	// The reverse takes away only goals still exactly as the migration made
	// them. One the person reworded (written by them), paused, moved to
	// another time or given other words is theirs, with its history, and
	// stays.
	update := func(agent, set string) {
		dbtest.Exec(t, database, `UPDATE "agent_conversation" SET `+set+` WHERE "agent_id" = '`+agent+`' AND "goal_title" = 'Look around'`)
	}
	update("around-chosen-agent", `"goal_written_by" = 'person'`)
	update("around-default-agent", `"goal_state" = 'paused'`)
	reverse, err := os.ReadFile("0285_look_around_goal.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := count(`true`); got != "2" {
		t.Errorf("%s look around goals are left after the reverse, want the reworded and the paused one", got)
	}
	if got := count(`"agent_id" = 'around-bad-zone-agent'`); got != "0" {
		t.Errorf("a goal exactly as made was left: %s", got)
	}

	// Moved to another time, or given other words, they stay as well.
	dbtest.Exec(t, database, string(forward))
	update("around-bad-zone-agent", `"goal_times" = '["0 9 * * 1"]'::jsonb`)
	dbtest.Exec(t, database, string(reverse))
	if got := count(`"agent_id" = 'around-bad-zone-agent'`); got != "1" {
		t.Errorf("a goal moved to another time was deleted: %s", got)
	}
	update("around-bad-zone-agent", `"goal_times" = '["0 17 * * 4"]'::jsonb, "goal" = "goal" || ' Also the library.'`)
	dbtest.Exec(t, database, string(reverse))
	if got := count(`"agent_id" = 'around-bad-zone-agent'`); got != "1" {
		t.Errorf("a goal with other words was deleted: %s", got)
	}
}
