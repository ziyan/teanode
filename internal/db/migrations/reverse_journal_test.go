package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The journal migration keeps one day per person and date, one period per
// person, kind and start, one link per record on a day or a period, and a link
// belongs to exactly one of the two; a person's account takes all of it away,
// the ordinary roles get the permission, and its reverse file takes the
// tables, the columns and the permission away.
func TestTheJournalMigrationKeepsItsConstraintsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-journal-user")
	otherId := dbtest.CreateUser(t, database, "reverse-journal-other")
	for _, statement := range []string{
		`INSERT INTO "journal_day" ("id", "user_id", "day", "time_zone", "created_at", "modified_at") VALUES ('reverse-journal-day', '` + userId + `', '2026-03-03', 'UTC', now(), now())`,
		`INSERT INTO "journal_period" ("id", "user_id", "period_kind", "period_start", "created_at", "modified_at") VALUES ('reverse-journal-week', '` + userId + `', 'week', '2026-03-02', now(), now())`,
		`INSERT INTO "journal_link" ("id", "user_id", "journal_day_id", "record_kind", "record_id", "link_source", "created_at") VALUES ('reverse-journal-link', '` + userId + `', 'reverse-journal-day', 'mail', 'mail-1', 'person', now())`,
		`INSERT INTO "journal_day" ("id", "user_id", "day", "time_zone", "created_at", "modified_at") VALUES ('reverse-journal-other-day', '` + otherId + `', '2026-03-03', 'UTC', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	// A new day and a new person take the defaults the code relies on.
	if got := dbtest.QueryString(t, database, `SELECT words || ',' || touch_up_how || ',' || facts::text || ',' || narrative_corrections::text || ',' || coalesce(touched_up_words, 'null') || ',' || coalesce(mood_level::text, 'null') || ',' || is_narrative_requested::text FROM "journal_day" WHERE "id" = 'reverse-journal-day'`); got != ",,{},[],null,null,false" {
		t.Errorf("a new day has defaults %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT is_journal_weather_on::text || ',' || is_journal_evening_question_on::text || ',' || journal_evening_question_time FROM "user" WHERE "id" = '`+userId+`'`); got != "true,true,21:00" {
		t.Errorf("a person's journal settings start as %q", got)
	}

	for name, statement := range map[string]string{
		"sameday":    `INSERT INTO "journal_day" ("id", "user_id", "day", "time_zone", "created_at", "modified_at") VALUES ('reverse-journal-again', '` + userId + `', '2026-03-03', 'UTC', now(), now())`,
		"moodof6":    `INSERT INTO "journal_day" ("id", "user_id", "day", "time_zone", "mood_level", "created_at", "modified_at") VALUES ('reverse-journal-mood', '` + userId + `', '2026-03-04', 'UTC', 6, now(), now())`,
		"moodsource": `INSERT INTO "journal_day" ("id", "user_id", "day", "time_zone", "mood_source", "created_at", "modified_at") VALUES ('reverse-journal-source', '` + userId + `', '2026-03-04', 'UTC', 'agent', now(), now())`,
		"sameweek":   `INSERT INTO "journal_period" ("id", "user_id", "period_kind", "period_start", "created_at", "modified_at") VALUES ('reverse-journal-week-again', '` + userId + `', 'week', '2026-03-02', now(), now())`,
		"fortnight":  `INSERT INTO "journal_period" ("id", "user_id", "period_kind", "period_start", "created_at", "modified_at") VALUES ('reverse-journal-fortnight', '` + userId + `', 'fortnight', '2026-03-02', now(), now())`,
		"samerecord": `INSERT INTO "journal_link" ("id", "user_id", "journal_day_id", "record_kind", "record_id", "link_source", "created_at") VALUES ('reverse-journal-link-again', '` + userId + `', 'reverse-journal-day', 'mail', 'mail-1', 'agent', now())`,
		"noowner":    `INSERT INTO "journal_link" ("id", "user_id", "record_kind", "record_id", "link_source", "created_at") VALUES ('reverse-journal-orphan', '` + userId + `', 'mail', 'mail-2', 'agent', now())`,
		"bothowners": `INSERT INTO "journal_link" ("id", "user_id", "journal_day_id", "journal_period_id", "record_kind", "record_id", "link_source", "created_at") VALUES ('reverse-journal-both', '` + userId + `', 'reverse-journal-day', 'reverse-journal-week', 'mail', 'mail-3', 'agent', now())`,
		"linksource": `INSERT INTO "journal_link" ("id", "user_id", "journal_day_id", "record_kind", "record_id", "link_source", "created_at") VALUES ('reverse-journal-source-link', '` + userId + `', 'reverse-journal-day', 'mail', 'mail-4', 'robot', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// The same record on a period is a different link from the same record on a day.
	dbtest.Exec(t, database, `INSERT INTO "journal_link" ("id", "user_id", "journal_period_id", "record_kind", "record_id", "link_source", "created_at") VALUES ('reverse-journal-week-link', '`+userId+`', 'reverse-journal-week', 'mail', 'mail-1', 'agent', now())`)

	// A day takes its links with it, and an account takes everything of its own.
	dbtest.Exec(t, database, `DELETE FROM "journal_day" WHERE "id" = 'reverse-journal-day'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "journal_link" WHERE "id" = 'reverse-journal-link'`); got != "0" {
		t.Errorf("a deleted day leaves %s links", got)
	}
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+userId+`'`)
	for _, table := range []string{"journal_day", "journal_period", "journal_link"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "`+table+`" WHERE "user_id" = '`+userId+`'`); got != "0" {
			t.Errorf("a deleted account leaves %s rows in %s", got, table)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "journal_day" WHERE "user_id" = '`+otherId+`'`); got != "1" {
		t.Errorf("another person's day went with the account: %s", got)
	}

	reverse, err := os.ReadFile("0261_journal.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"journal_day", "journal_period", "journal_link"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE (table_name = 'user' AND column_name IN ('is_journal_weather_on', 'is_journal_evening_question_on', 'journal_evening_question_time'))
		OR (table_name = 'agent_dream' AND column_name IN ('journal_days_written', 'journal_periods_written', 'journal_narratives_refused'))`); got != "0" {
		t.Errorf("%s of the journal columns are still there", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "role_permission" WHERE "permission_key" = 'journal:use'`); got != "0" {
		t.Errorf("the permission is still granted to %s roles", got)
	}
}
