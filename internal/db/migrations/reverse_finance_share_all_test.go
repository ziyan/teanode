package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A share of all of a person's finance is the owner's, for one person or one
// circle, at one of two levels, once per grantee. It goes with the owner and
// with its grantee, and the reverse file takes the table away.
func TestTheWholeFinanceShareMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	ownerId := dbtest.CreateUser(t, database, "reverse-whole-owner")
	granteeId := dbtest.CreateUser(t, database, "reverse-whole-grantee")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-whole-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_user_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-share', '` + ownerId + `', '` + granteeId + `', 'spending', now(), now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"balance level": `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_circle_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-balance', '` + ownerId + `', 'reverse-whole-circle', 'balance', now(), now())`,
		"unknown level": `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_circle_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-unknown', '` + ownerId + `', 'reverse-whole-circle', 'owner', now(), now())`,
		"no grantee": `INSERT INTO "finance_share" ("id", "owner_user_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-nobody', '` + ownerId + `', 'spending', now(), now())`,
		"both grantees": `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_user_id", "grantee_circle_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-both', '` + ownerId + `', '` + granteeId + `', 'reverse-whole-circle', 'spending', now(), now())`,
		"same person twice": `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_user_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-twice', '` + ownerId + `', '` + granteeId + `', 'transactions', now(), now())`,
		"the owner themselves": `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_user_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-self', '` + ownerId + `', '` + ownerId + `', 'spending', now(), now())`,
		"an owner who is not there": `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_user_id", "finance_share_level", "created_at", "updated_at")
			VALUES ('reverse-whole-orphan', 'nobody', '` + granteeId + `', 'spending', now(), now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// A circle's share is a second share, once per circle, and the circle
	// taking its shares with it is the database's doing.
	dbtest.Exec(t, database, `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_circle_id", "finance_share_level", "created_at", "updated_at")
		VALUES ('reverse-whole-circle-share', '`+ownerId+`', 'reverse-whole-circle', 'transactions', now(), now())`)
	if err := execute(database, `INSERT INTO "finance_share" ("id", "owner_user_id", "grantee_circle_id", "finance_share_level", "created_at", "updated_at")
		VALUES ('reverse-whole-circle-twice', '`+ownerId+`', 'reverse-whole-circle', 'spending', now(), now())`); err == nil {
		t.Error("the same circle twice was accepted")
	}
	dbtest.Exec(t, database, `DELETE FROM "circle" WHERE "id" = 'reverse-whole-circle'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "finance_share"`); got != "1" {
		t.Errorf("%s shares are left after the circle went, want the one to a person", got)
	}
	dbtest.Exec(t, database, `DELETE FROM "user" WHERE "id" = '`+granteeId+`'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "finance_share"`); got != "0" {
		t.Errorf("%s shares are left after their grantee went", got)
	}

	reverse, err := os.ReadFile("0268_finance_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"finance_share"')::text, '')`); got != "" {
		t.Errorf("the share table is still there: %q", got)
	}
}
