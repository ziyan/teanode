package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A recording share names exactly one grantee and one of two levels, is made
// once for a person and once for a circle, goes with the recording, and the
// reverse file takes the table away.
func TestTheRecordingShareMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	ownerId := dbtest.CreateUser(t, database, "reverse-recording-owner")
	granteeId := dbtest.CreateUser(t, database, "reverse-recording-grantee")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-recording-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "recording" ("id", "user_id", "created_at", "updated_at", "client_recording_id", "recording_title", "recording_origin", "recording_capture_state")
			VALUES ('reverse-recording', '` + ownerId + `', now(), now(), 'client', 'Title', 'audioImport', 'imported')`,
		`INSERT INTO "recording_share" ("id", "recording_id", "grantee_user_id", "recording_share_level", "created_at")
			VALUES ('reverse-recording-share', 'reverse-recording', '` + granteeId + `', 'summary', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"audio level": `INSERT INTO "recording_share" ("id", "recording_id", "grantee_circle_id", "recording_share_level", "created_at")
			VALUES ('reverse-recording-audio', 'reverse-recording', 'reverse-recording-circle', 'audio', now())`,
		"neither": `INSERT INTO "recording_share" ("id", "recording_id", "recording_share_level", "created_at")
			VALUES ('reverse-recording-neither', 'reverse-recording', 'summary', now())`,
		"both": `INSERT INTO "recording_share" ("id", "recording_id", "grantee_user_id", "grantee_circle_id", "recording_share_level", "created_at")
			VALUES ('reverse-recording-both', 'reverse-recording', '` + granteeId + `', 'reverse-recording-circle', 'summary', now())`,
		"person twice": `INSERT INTO "recording_share" ("id", "recording_id", "grantee_user_id", "recording_share_level", "created_at")
			VALUES ('reverse-recording-twice', 'reverse-recording', '` + granteeId + `', 'transcript', now())`,
		"no recording": `INSERT INTO "recording_share" ("id", "recording_id", "grantee_user_id", "recording_share_level", "created_at")
			VALUES ('reverse-recording-orphan', 'nobody', '` + granteeId + `', 'summary', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// A circle's share is a second share, and the recording taking its shares
	// with it is the database's doing.
	dbtest.Exec(t, database, `INSERT INTO "recording_share" ("id", "recording_id", "grantee_circle_id", "recording_share_level", "created_at")
		VALUES ('reverse-recording-circle-share', 'reverse-recording', 'reverse-recording-circle', 'transcript', now())`)
	dbtest.Exec(t, database, `DELETE FROM "recording" WHERE "id" = 'reverse-recording'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "recording_share"`); got != "0" {
		t.Errorf("a share outlived its recording: %s", got)
	}

	reverse, err := os.ReadFile("0256_recording_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"recording_share"')::text, '')`); got != "" {
		t.Errorf("the share table is still there: %q", got)
	}
}
