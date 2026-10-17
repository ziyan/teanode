package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The location migration refuses a row outside its vocabularies, a place
// whose radius is outside what a person can set, a share with no grantee or
// two, and a position reported twice. Its reverse file takes the four tables
// and the permission away.
func TestTheLocationMigrationKeepsItsVocabulariesAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-location-user")
	otherId := dbtest.CreateUser(t, database, "reverse-location-other")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-location-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "place" ("id", "user_id", "latitude_degrees", "longitude_degrees", "created_at", "updated_at") VALUES ('reverse-place', '` + userId + `', 10.5, 20.5, now(), now())`,
		`INSERT INTO "location_point" ("id", "user_id", "latitude_degrees", "longitude_degrees", "accuracy_meters", "measured_at", "received_at", "location_source") VALUES ('reverse-point', '` + userId + `', 10.5, 20.5, 5, '2026-01-01T00:00:00Z', now(), 'phone')`,
		`INSERT INTO "place_visit" ("id", "user_id", "place_id", "latitude_degrees", "longitude_degrees", "visit_source", "client_visit_key", "created_at", "updated_at") VALUES ('reverse-visit', '` + userId + `', 'reverse-place', 10.5, 20.5, 'visit', 'key-one', now(), now())`,
		`INSERT INTO "location_share" ("id", "user_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-location-share', '` + userId + `', '` + otherId + `', 'current', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	// A new place takes the defaults the code relies on.
	if got := dbtest.QueryString(t, database, `SELECT radius_meters::text || ',' || place_label || ',' || is_named_by_person::text || ',' || coalesce(named_at::text, 'null') || ',' || naming_error_message FROM "place" WHERE "id" = 'reverse-place'`); got != "75,,false,null," {
		t.Errorf("a new place has defaults %q", got)
	}

	for name, statement := range map[string]string{
		"samepoint":    `INSERT INTO "location_point" ("id", "user_id", "latitude_degrees", "longitude_degrees", "accuracy_meters", "measured_at", "received_at", "location_source") VALUES ('reverse-point-again', '` + userId + `', 11, 21, 5, '2026-01-01T00:00:00Z', now(), 'phone')`,
		"badsource":    `INSERT INTO "location_point" ("id", "user_id", "latitude_degrees", "longitude_degrees", "accuracy_meters", "measured_at", "received_at", "location_source") VALUES ('reverse-point-bad', '` + userId + `', 11, 21, 5, now(), now(), 'watch')`,
		"badlabel":     `INSERT INTO "place" ("id", "user_id", "place_label", "latitude_degrees", "longitude_degrees", "created_at", "updated_at") VALUES ('reverse-bad-label', '` + userId + `', 'office', 1, 1, now(), now())`,
		"smallradius":  `INSERT INTO "place" ("id", "user_id", "radius_meters", "latitude_degrees", "longitude_degrees", "created_at", "updated_at") VALUES ('reverse-small', '` + userId + `', 10, 1, 1, now(), now())`,
		"largeradius":  `INSERT INTO "place" ("id", "user_id", "radius_meters", "latitude_degrees", "longitude_degrees", "created_at", "updated_at") VALUES ('reverse-large', '` + userId + `', 2001, 1, 1, now(), now())`,
		"badosmtype":   `INSERT INTO "place" ("id", "user_id", "osm_type", "latitude_degrees", "longitude_degrees", "created_at", "updated_at") VALUES ('reverse-bad-osm', '` + userId + `', 'area', 1, 1, now(), now())`,
		"badvisit":     `INSERT INTO "place_visit" ("id", "user_id", "place_id", "latitude_degrees", "longitude_degrees", "visit_source", "client_visit_key", "created_at", "updated_at") VALUES ('reverse-visit-bad', '` + userId + `', 'reverse-place', 1, 1, 'guess', 'key-two', now(), now())`,
		"samevisitkey": `INSERT INTO "place_visit" ("id", "user_id", "place_id", "latitude_degrees", "longitude_degrees", "visit_source", "client_visit_key", "created_at", "updated_at") VALUES ('reverse-visit-again', '` + userId + `', 'reverse-place', 1, 1, 'visit', 'key-one', now(), now())`,
		"badlevel":     `INSERT INTO "location_share" ("id", "user_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-share-bad', '` + userId + `', '` + otherId + `', 'edit', now())`,
		"nograntee":    `INSERT INTO "location_share" ("id", "user_id", "share_level", "created_at") VALUES ('reverse-share-nobody', '` + userId + `', 'current', now())`,
		"bothgrantees": `INSERT INTO "location_share" ("id", "user_id", "grantee_user_id", "grantee_circle_id", "share_level", "created_at") VALUES ('reverse-share-both', '` + userId + `', '` + otherId + `', 'reverse-location-circle', 'current', now())`,
		"sharetwice":   `INSERT INTO "location_share" ("id", "user_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-share-twice', '` + userId + `', '` + otherId + `', 'history', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// A place takes its visits with it, and an account takes everything.
	dbtest.Exec(t, database, `DELETE FROM "place" WHERE "id" = 'reverse-place'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "place_visit"`); got != "0" {
		t.Errorf("a deleted place leaves %s visits", got)
	}

	reverse, err := os.ReadFile("0240_location.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for _, table := range []string{"location_point", "place", "place_visit", "location_share"} {
		if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			t.Errorf("the %s table is still there: %q", table, got)
		}
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "role_permission" WHERE "permission_key" = 'location:use'`); got != "0" {
		t.Errorf("the permission is still granted to %s roles", got)
	}
}
