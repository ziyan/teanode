package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// Every account reads its new photos until the person says otherwise, a
// photo's document kind is one of the known ones, one photo is copied into
// one upload of an agent, and the reverse file takes all of it away.
func TestThePhotoReadingMigrationStartsOnAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-photo-reading-user")
	if got := dbtest.QueryString(t, database, `SELECT "is_photo_reading_enabled"::text FROM "user" WHERE "id" = '`+userId+`'`); got != "true" {
		t.Errorf("a new account reads its new photos: %q", got)
	}
	dbtest.Exec(t, database, `INSERT INTO "photo" ("id", "user_id", "created_at", "updated_at", "client_photo_id", "photo_kind", "photo_source", "photo_processing_state")
		VALUES ('photo-reading-1', '`+userId+`', now(), now(), 'client-1', 'picture', 'iphone', 'ready')`)
	if got := dbtest.QueryString(t, database, `SELECT "photo_document_kind" FROM "photo" WHERE "id" = 'photo-reading-1'`); got != "" {
		t.Errorf("an undescribed photo is no kind yet: %q", got)
	}
	for _, documentKind := range []string{"none", "receipt", "statement", "taxDocument"} {
		dbtest.Exec(t, database, `UPDATE "photo" SET "photo_document_kind" = '`+documentKind+`' WHERE "id" = 'photo-reading-1'`)
	}
	if err := execute(database, `UPDATE "photo" SET "photo_document_kind" = 'invoice' WHERE "id" = 'photo-reading-1'`); err == nil {
		t.Error("the database accepted a kind it does not know")
	}

	reverse, err := os.ReadFile("0265_photo_reading.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	for table, column := range map[string]string{"user": "is_photo_reading_enabled", "photo": "photo_document_kind", "agent_attachment": "photo_id"} {
		if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM information_schema.columns WHERE table_name = '`+table+`' AND column_name = '`+column+`'`); got != "0" {
			t.Errorf("the %s table still has %s", table, column)
		}
	}
}
