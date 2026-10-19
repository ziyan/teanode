package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The health migration refuses a reading reported twice, a reading that ends
// before it starts, a share with no grantee or two or no categories, and a
// second share with the same grantee. The totals of readings follow every
// insert and delete by themselves. A person's account takes all of it away,
// and its reverse file takes the tables, the functions, the two columns and
// the permission away.
func TestTheHealthMigrationKeepsItsConstraintsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-health-user")
	otherId := dbtest.CreateUser(t, database, "reverse-health-other")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-health-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "health_sample" ("id", "user_id", "health_sample_id", "health_type", "started_at", "ended_at", "received_at") VALUES ('reverse-health-sample', '` + userId + `', 'sample-one', 'stepCount', '2026-01-01T10:00:00Z', '2026-01-01T10:05:00Z', now())`,
		`INSERT INTO "health_day_summary" ("user_id", "health_type", "summary_day", "updated_at") VALUES ('` + userId + `', 'stepCount', '2026-01-01', now())`,
		`INSERT INTO "health_workout_route" ("user_id", "health_sample_id", "route_points", "received_at") VALUES ('` + userId + `', 'sample-one', '[[0, 10.5, 20.5, 3]]', now())`,
		`INSERT INTO "health_characteristic" ("user_id", "health_type", "characteristic_value", "updated_at") VALUES ('` + userId + `', 'bloodType', 'aPositive', now())`,
		`INSERT INTO "health_share" ("id", "user_id", "grantee_user_id", "health_categories", "created_at") VALUES ('reverse-health-share', '` + userId + `', '` + otherId + `', '{activity}', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	// A new reading and a new person take the defaults the code relies on.
	if got := dbtest.QueryString(t, database, `SELECT quantity_unit || ',' || workout_activity_type || ',' || sample_metadata::text || ',' || coalesce(quantity_value::text, 'null') FROM "health_sample" WHERE "id" = 'reverse-health-sample'`); got != ",,{},null" {
		t.Errorf("a new reading has defaults %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT can_agent_read_health::text || ',' || coalesce(health_erased_at::text, 'null') FROM "user" WHERE "id" = '`+userId+`'`); got != "false,null" {
		t.Errorf("a person's Health starts with the agent switch off and nothing erased: %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT category_seconds::text || ',' || sample_count::text FROM "health_day_summary" WHERE "user_id" = '`+userId+`'`); got != "{},0" {
		t.Errorf("a new day summary has defaults %q", got)
	}

	for name, statement := range map[string]string{
		"samereading":        `INSERT INTO "health_sample" ("id", "user_id", "health_sample_id", "health_type", "started_at", "ended_at", "received_at") VALUES ('reverse-health-again', '` + userId + `', 'sample-one', 'stepCount', '2026-01-01T11:00:00Z', '2026-01-01T11:05:00Z', now())`,
		"endsbeforestart":    `INSERT INTO "health_sample" ("id", "user_id", "health_sample_id", "health_type", "started_at", "ended_at", "received_at") VALUES ('reverse-health-backwards', '` + userId + `', 'sample-two', 'stepCount', '2026-01-01T11:05:00Z', '2026-01-01T11:00:00Z', now())`,
		"sameday":            `INSERT INTO "health_day_summary" ("user_id", "health_type", "summary_day", "updated_at") VALUES ('` + userId + `', 'stepCount', '2026-01-01', now())`,
		"sameroute":          `INSERT INTO "health_workout_route" ("user_id", "health_sample_id", "route_points", "received_at") VALUES ('` + userId + `', 'sample-one', '[]', now())`,
		"samecharacteristic": `INSERT INTO "health_characteristic" ("user_id", "health_type", "characteristic_value", "updated_at") VALUES ('` + userId + `', 'bloodType', 'bPositive', now())`,
		"nograntee":          `INSERT INTO "health_share" ("id", "user_id", "health_categories", "created_at") VALUES ('reverse-share-nobody', '` + userId + `', '{sleep}', now())`,
		"bothgrantees":       `INSERT INTO "health_share" ("id", "user_id", "grantee_user_id", "grantee_circle_id", "health_categories", "created_at") VALUES ('reverse-share-both', '` + userId + `', '` + otherId + `', 'reverse-health-circle', '{sleep}', now())`,
		"nocategories":       `INSERT INTO "health_share" ("id", "user_id", "grantee_circle_id", "health_categories", "created_at") VALUES ('reverse-share-empty', '` + userId + `', 'reverse-health-circle', '{}', now())`,
		"sharetwice":         `INSERT INTO "health_share" ("id", "user_id", "grantee_user_id", "health_categories", "created_at") VALUES ('reverse-share-twice', '` + userId + `', '` + otherId + `', '{sleep}', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// The count of readings is kept by the database: one reading sent, one
	// counted, and none once it is deleted.
	if got := dbtest.QueryString(t, database, `SELECT sample_count::text FROM "health_type_total" WHERE "user_id" = '`+userId+`' AND "health_type" = 'stepCount'`); got != "1" {
		t.Errorf("the total after one reading: %q", got)
	}
	dbtest.Exec(t, database, `INSERT INTO "health_sample" ("id", "user_id", "health_sample_id", "health_type", "started_at", "ended_at", "received_at") VALUES ('reverse-health-second', '`+userId+`', 'sample-second', 'stepCount', '2026-01-02T10:00:00Z', '2026-01-02T10:05:00Z', now())`)
	if got := dbtest.QueryString(t, database, `SELECT sample_count::text FROM "health_type_total" WHERE "user_id" = '`+userId+`' AND "health_type" = 'stepCount'`); got != "2" {
		t.Errorf("the total after two readings: %q", got)
	}
	dbtest.Exec(t, database, `DELETE FROM "health_sample" WHERE "id" = 'reverse-health-second'`)
	if got := dbtest.QueryString(t, database, `SELECT sample_count::text FROM "health_type_total" WHERE "user_id" = '`+userId+`' AND "health_type" = 'stepCount'`); got != "1" {
		t.Errorf("the total after deleting one: %q", got)
	}

	// The same HealthKit identifier is a different reading for a different
	// person.
	dbtest.Exec(t, database, `INSERT INTO "health_sample" ("id", "user_id", "health_sample_id", "health_type", "started_at", "ended_at", "received_at") VALUES ('reverse-health-other-sample', '`+otherId+`', 'sample-one', 'stepCount', '2026-01-01T10:00:00Z', '2026-01-01T10:05:00Z', now())`)

	dbtest.Exec(t, database, `INSERT INTO "health_deleted_sample" ("user_id", "health_sample_id", "health_type", "started_at", "ended_at", "deleted_at") VALUES ('`+otherId+`', 'gone', 'stepCount', now(), now(), now())`)

	// An account takes everything of its own with it.
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+otherId+`'`)
	for _, table := range []string{"health_type_total", "health_deleted_sample"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "`+table+`" WHERE "user_id" = '`+otherId+`'`); got != "0" {
			t.Errorf("a deleted account leaves %s rows in %s", got, table)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "health_sample" WHERE "user_id" = '`+otherId+`'`); got != "0" {
		t.Errorf("a deleted account leaves %s readings", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "health_share"`); got != "0" {
		t.Errorf("a deleted grantee leaves %s shares", got)
	}

	reverse, err := os.ReadFile("0245_health.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"health_sample", "health_day_summary", "health_workout_route", "health_characteristic", "health_share", "health_type_total", "health_deleted_sample"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM pg_proc WHERE proname LIKE 'health\_sample\_count%'`); got != "0" {
		t.Errorf("%s functions are still there", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'user' AND column_name IN ('can_agent_read_health', 'health_erased_at')`); got != "0" {
		t.Errorf("%s of the two user columns are still there", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "role_permission" WHERE "permission_key" = 'health:use'`); got != "0" {
		t.Errorf("the permission is still granted to %s roles", got)
	}
}
