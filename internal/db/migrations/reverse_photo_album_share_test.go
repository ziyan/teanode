package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The album share migration refuses to be undone while a circle has an album,
// deleting nothing, and with none it takes the share table and the circle
// column away and makes every album belong to an account again. The photos in
// a circle's album are in their owners' libraries, so deleting the album loses
// no photo.
func TestThePhotoAlbumShareMigrationReversesOnlyWithoutACircleAlbum(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-album-user")
	otherId := dbtest.CreateUser(t, database, "reverse-album-other")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "photo_album" ("id", "user_id", "album_title", "created_at", "updated_at") VALUES ('reverse-own', '` + userId + `', 'Mine', now(), now())`,
		`INSERT INTO "photo_album_share" ("id", "photo_album_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-share', 'reverse-own', '` + otherId + `', 'view', now())`,
		`INSERT INTO "photo_album" ("id", "circle_id", "album_title", "created_at", "updated_at") VALUES ('reverse-family', 'reverse-circle', 'Ours', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	// An album must belong to exactly one of an account and a circle, and a
	// share to exactly one of a person and a circle, at a level that exists.
	for name, statement := range map[string]string{
		"neither":   `INSERT INTO "photo_album" ("id", "album_title", "created_at", "updated_at") VALUES ('reverse-none', 'None', now(), now())`,
		"both":      `INSERT INTO "photo_album" ("id", "user_id", "circle_id", "album_title", "created_at", "updated_at") VALUES ('reverse-both', '` + userId + `', 'reverse-circle', 'Both', now(), now())`,
		"badlevel":  `INSERT INTO "photo_album_share" ("id", "photo_album_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-bad', 'reverse-family', '` + userId + `', 'owner', now())`,
		"nograntee": `INSERT INTO "photo_album_share" ("id", "photo_album_id", "share_level", "created_at") VALUES ('reverse-nobody', 'reverse-own', 'view', now())`,
		"twice":     `INSERT INTO "photo_album_share" ("id", "photo_album_id", "grantee_user_id", "share_level", "created_at") VALUES ('reverse-twice', 'reverse-own', '` + otherId + `', 'contribute', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	reverse, err := os.ReadFile("0224_photo_album_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	if err := execute(database, string(reverse)); err == nil || !strings.Contains(err.Error(), "a circle has a photo album of its own") {
		t.Fatalf("going back is refused while a circle has an album: %v", err)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "photo_album_share"`); got != "1" {
		t.Errorf("a refused rollback deletes nothing: %s shares", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "photo_album"`); got != "2" {
		t.Errorf("a refused rollback deletes nothing: %s albums", got)
	}

	dbtest.Exec(t, database, `DELETE FROM "photo_album" WHERE "id" = 'reverse-family'`)
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"photo_album_share"')::text, '')`); got != "" {
		t.Errorf("the share table is still there: %q", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'photo_album' AND column_name = 'circle_id'`); got != "0" {
		t.Errorf("the circle_id column is still there")
	}
	if got := dbtest.QueryString(t, database, `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'photo_album' AND column_name = 'user_id'`); got != "NO" {
		t.Errorf("user_id is nullable again: %s", got)
	}
	if got := dbtest.QueryString(t, database, `SELECT count(*) FROM "photo_album"`); got != "1" {
		t.Errorf("an account's album was lost: %s albums", got)
	}
}
