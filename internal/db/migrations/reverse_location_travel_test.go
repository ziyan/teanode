package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The location travel migration accepts well-formed activities, trips, legs
// and corrections, refuses an unknown mode or kind, a span that ends before it
// begins and a second activity at the same instant, takes the rows with the
// account, and its reverse file takes the tables away.
func TestTheLocationTravelMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-travel-user")
	for _, statement := range []string{
		`INSERT INTO "motion_activity" ("id", "user_id", "started_at", "activity_kind", "activity_confidence", "created_at") VALUES ('reverse-activity', '` + userId + `', '2026-01-01T08:00:00Z', 'walking', 'high', now())`,
		`INSERT INTO "trip" ("id", "user_id", "started_at", "ended_at", "distance_meters", "created_at") VALUES ('reverse-trip', '` + userId + `', '2026-01-01T08:00:00Z', '2026-01-01T08:30:00Z', 1200, now())`,
		`INSERT INTO "trip_leg" ("id", "trip_id", "user_id", "started_at", "ended_at", "detected_travel_mode", "detection_source", "distance_meters") VALUES ('reverse-leg', 'reverse-trip', '` + userId + `', '2026-01-01T08:00:00Z', '2026-01-01T08:30:00Z', 'walk', 'motion', 1200)`,
		`INSERT INTO "travel_mode_correction" ("id", "user_id", "started_at", "ended_at", "travel_mode", "created_at", "updated_at") VALUES ('reverse-correction', '` + userId + `', '2026-01-01T08:00:00Z', '2026-01-01T08:30:00Z', 'bus', now(), now())`,
		`INSERT INTO "trip_backfill" ("user_id", "backfilled_at") VALUES ('` + userId + `', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"unknownActivityKind":       `INSERT INTO "motion_activity" ("id", "user_id", "started_at", "activity_kind", "activity_confidence", "created_at") VALUES ('reverse-bad-kind', '` + userId + `', '2026-01-02T08:00:00Z', 'flying', 'high', now())`,
		"unknownActivityConfidence": `INSERT INTO "motion_activity" ("id", "user_id", "started_at", "activity_kind", "activity_confidence", "created_at") VALUES ('reverse-bad-confidence', '` + userId + `', '2026-01-02T08:00:00Z', 'walking', 'certain', now())`,
		"duplicateActivityStart":    `INSERT INTO "motion_activity" ("id", "user_id", "started_at", "activity_kind", "activity_confidence", "created_at") VALUES ('reverse-twice', '` + userId + `', '2026-01-01T08:00:00Z', 'running', 'low', now())`,
		"tripEndsBeforeItBegins":    `INSERT INTO "trip" ("id", "user_id", "started_at", "ended_at", "distance_meters", "created_at") VALUES ('reverse-backwards-trip', '` + userId + `', '2026-01-03T09:00:00Z', '2026-01-03T08:00:00Z', 10, now())`,
		"legUnknownMode":            `INSERT INTO "trip_leg" ("id", "trip_id", "user_id", "started_at", "ended_at", "detected_travel_mode", "detection_source", "distance_meters") VALUES ('reverse-bad-mode', 'reverse-trip', '` + userId + `', '2026-01-01T08:00:00Z', '2026-01-01T08:10:00Z', 'rocket', 'motion', 1)`,
		"legUnknownSource":          `INSERT INTO "trip_leg" ("id", "trip_id", "user_id", "started_at", "ended_at", "detected_travel_mode", "detection_source", "distance_meters") VALUES ('reverse-bad-source', 'reverse-trip', '` + userId + `', '2026-01-01T08:00:00Z', '2026-01-01T08:10:00Z', 'walk', 'guess', 1)`,
		"legEndsBeforeItBegins":     `INSERT INTO "trip_leg" ("id", "trip_id", "user_id", "started_at", "ended_at", "detected_travel_mode", "detection_source", "distance_meters") VALUES ('reverse-backwards-leg', 'reverse-trip', '` + userId + `', '2026-01-01T08:10:00Z', '2026-01-01T08:00:00Z', 'walk', 'motion', 1)`,
		"legWithoutTrip":            `INSERT INTO "trip_leg" ("id", "trip_id", "user_id", "started_at", "ended_at", "detected_travel_mode", "detection_source", "distance_meters") VALUES ('reverse-orphan-leg', 'nothing', '` + userId + `', '2026-01-01T08:00:00Z', '2026-01-01T08:10:00Z', 'walk', 'motion', 1)`,
		"correctionUnknownMode":     `INSERT INTO "travel_mode_correction" ("id", "user_id", "started_at", "ended_at", "travel_mode", "created_at", "updated_at") VALUES ('reverse-bad-correction', '` + userId + `', '2026-01-02T08:00:00Z', '2026-01-02T09:00:00Z', 'rocket', now(), now())`,
		"correctionEmptySpan":       `INSERT INTO "travel_mode_correction" ("id", "user_id", "started_at", "ended_at", "travel_mode", "created_at", "updated_at") VALUES ('reverse-empty-correction', '` + userId + `', '2026-01-02T08:00:00Z', '2026-01-02T08:00:00Z', 'bus', now(), now())`,
		"activityNoPerson":          `INSERT INTO "motion_activity" ("id", "user_id", "started_at", "activity_kind", "activity_confidence", "created_at") VALUES ('reverse-nobody', 'nobody', '2026-01-02T08:00:00Z', 'walking', 'high', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// The account takes everything with it.
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+userId+`'`)
	for _, table := range []string{"motion_activity", "trip", "trip_leg", "travel_mode_correction", "trip_backfill"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "`+table+`"`); got != "0" {
			t.Errorf("a deleted account leaves %s rows in %s", got, table)
		}
	}

	reverse, err := os.ReadFile("0263_location_travel.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"motion_activity", "trip", "trip_leg", "travel_mode_correction", "trip_backfill"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
}
