package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The location notification mute migration keeps one mute for each pair of
// people, refuses muting oneself, goes with either account, and its reverse
// file takes the table away.
func TestTheLocationNotificationMuteMigrationKeepsOneMuteForEachPairAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-mute-user")
	mutedUserId := dbtest.CreateUser(t, database, "reverse-muted-user")
	otherUserId := dbtest.CreateUser(t, database, "reverse-mute-other")
	for _, statement := range []string{
		`INSERT INTO "location_notification_mute" ("user_id", "muted_user_id", "muted_until", "created_at", "updated_at") VALUES ('` + userId + `', '` + mutedUserId + `', '2026-01-01T15:00:00Z', now(), now())`,
		`INSERT INTO "location_notification_mute" ("user_id", "muted_user_id", "created_at", "updated_at") VALUES ('` + userId + `', '` + otherUserId + `', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"twice":    `INSERT INTO "location_notification_mute" ("user_id", "muted_user_id", "created_at", "updated_at") VALUES ('` + userId + `', '` + mutedUserId + `', now(), now())`,
		"oneself":  `INSERT INTO "location_notification_mute" ("user_id", "muted_user_id", "created_at", "updated_at") VALUES ('` + userId + `', '` + userId + `', now(), now())`,
		"noPerson": `INSERT INTO "location_notification_mute" ("user_id", "muted_user_id", "created_at", "updated_at") VALUES ('` + userId + `', 'nobody', now(), now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// Either account takes the mute with it.
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+mutedUserId+`'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "location_notification_mute"`); got != "1" {
		t.Errorf("a deleted muted account leaves %s mutes, want 1", got)
	}
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+userId+`'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "location_notification_mute"`); got != "0" {
		t.Errorf("a deleted account leaves %s mutes", got)
	}

	reverse, err := os.ReadFile("0259_location_notification_mute.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"location_notification_mute"')::text, '')`); got != "" {
		t.Errorf("the location_notification_mute table is still there: %q", got)
	}
}
