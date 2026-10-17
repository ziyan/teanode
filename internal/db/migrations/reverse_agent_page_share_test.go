package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A share names exactly one of a person and a circle, never a path with a
// trailing slash (the boundary the readers match at is the slash), and one
// path is shared with one grantee once. The reverse file takes the table away.
func TestThePageShareMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(t, database, "reverse-page-owner")
	otherId := dbtest.CreateUser(t, database, "reverse-page-reader")
	for _, statement := range []string{
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-page-agent', now(), now(), '` + userId + `', 'Agent')`,
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-page-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "agent_page_share" ("id", "agent_id", "path", "grantee_user_id", "created_at") VALUES ('reverse-page-share', 'reverse-page-agent', 'people/a', '` + otherId + `', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"nobody": `INSERT INTO "agent_page_share" ("id", "agent_id", "path", "created_at") VALUES ('reverse-page-none', 'reverse-page-agent', 'people/b', now())`,
		"both": `INSERT INTO "agent_page_share" ("id", "agent_id", "path", "grantee_user_id", "grantee_circle_id", "created_at") VALUES ('reverse-page-both', 'reverse-page-agent', 'people/b', '` +
			otherId + `', 'reverse-page-circle', now())`,
		"trailing": `INSERT INTO "agent_page_share" ("id", "agent_id", "path", "grantee_user_id", "created_at") VALUES ('reverse-page-slash', 'reverse-page-agent', 'people/b/', '` + otherId + `', now())`,
		"empty":    `INSERT INTO "agent_page_share" ("id", "agent_id", "path", "grantee_user_id", "created_at") VALUES ('reverse-page-empty', 'reverse-page-agent', '', '` + otherId + `', now())`,
		"twice":    `INSERT INTO "agent_page_share" ("id", "agent_id", "path", "grantee_user_id", "created_at") VALUES ('reverse-page-twice', 'reverse-page-agent', 'people/a', '` + otherId + `', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	reverse, err := os.ReadFile("0196_agent_page_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"agent_page_share"')::text, '')`); got != "" {
		t.Errorf("the share table is still there: %q", got)
	}
}
