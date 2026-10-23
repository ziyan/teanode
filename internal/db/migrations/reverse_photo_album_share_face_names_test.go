package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A share made before the face names migration gives no names, and the
// migration reverses by taking the column away and leaving the share.
func TestThePhotoAlbumShareFaceNamesMigrationReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-names-user")
	otherId := dbtest.CreateUser(t, database, "reverse-names-other")
	dbtest.Exec(t, database, `INSERT INTO "photo_album" ("id", "user_id", "album_title", "created_at", "updated_at") VALUES ('reverse-names-album', '`+userId+`', 'Mine', now(), now())`)
	dbtest.Exec(t, database, `INSERT INTO "photo_album_share" ("id", "photo_album_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-names-share', 'reverse-names-album', '`+otherId+`', 'view', now())`)
	if got := dbtest.QueryString(t, database, `SELECT is_face_names_included::text FROM "photo_album_share" WHERE "id" = 'reverse-names-share'`); got != "false" {
		t.Errorf("a share that does not say gives face names: %s", got)
	}

	reverse, err := os.ReadFile("0253_photo_album_share_face_names.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'photo_album_share' AND column_name = 'is_face_names_included'`); got != "0" {
		t.Errorf("the column is still there")
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "photo_album_share"`); got != "1" {
		t.Errorf("the share was lost: %s shares", got)
	}
}
