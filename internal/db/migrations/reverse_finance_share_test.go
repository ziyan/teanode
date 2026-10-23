package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A finance share is of one finance account, for one person or one circle, at
// one of three levels, once per grantee. It goes with its account, and the
// reverse file takes the table away.
func TestTheFinanceShareMigrationRefusesBadRowsAndReverses(t *testing.T) {
	t.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %s", err)
	}
	ownerId := dbtest.CreateUser(t, database, "reverse-finance-owner")
	granteeId := dbtest.CreateUser(t, database, "reverse-finance-grantee")
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-finance-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "agent" ("id", "created_at", "modified_at", "user_id", "name") VALUES ('reverse-finance-agent', now(), now(), '` + ownerId + `', 'Agent')`,
		`INSERT INTO "agent_source" ("id", "agent_id", "created_at", "modified_at", "kind", "name") VALUES ('reverse-finance-source', 'reverse-finance-agent', now(), now(), 'finance', 'Bank')`,
		`INSERT INTO "agent_finance_account" ("id", "agent_id", "source_id", "provider_account_id", "account_kind", "currency_code", "created_at", "modified_at")
			VALUES ('reverse-finance-account', 'reverse-finance-agent', 'reverse-finance-source', 'provider-account', 'depository', 'USD', now(), now())`,
		`INSERT INTO "finance_account_share" ("id", "finance_account_id", "grantee_user_id", "finance_share_level", "created_at")
			VALUES ('reverse-finance-share', 'reverse-finance-account', '` + granteeId + `', 'balance', now())`,
	} {
		dbtest.Exec(t, database, statement)
	}

	for name, statement := range map[string]string{
		"unknown level": `INSERT INTO "finance_account_share" ("id", "finance_account_id", "grantee_circle_id", "finance_share_level", "created_at")
			VALUES ('reverse-finance-bad', 'reverse-finance-account', 'reverse-finance-circle', 'owner', now())`,
		"no grantee": `INSERT INTO "finance_account_share" ("id", "finance_account_id", "finance_share_level", "created_at")
			VALUES ('reverse-finance-nobody', 'reverse-finance-account', 'balance', now())`,
		"both grantees": `INSERT INTO "finance_account_share" ("id", "finance_account_id", "grantee_user_id", "grantee_circle_id", "finance_share_level", "created_at")
			VALUES ('reverse-finance-both', 'reverse-finance-account', '` + granteeId + `', 'reverse-finance-circle', 'balance', now())`,
		"same person twice": `INSERT INTO "finance_account_share" ("id", "finance_account_id", "grantee_user_id", "finance_share_level", "created_at")
			VALUES ('reverse-finance-twice', 'reverse-finance-account', '` + granteeId + `', 'spending', now())`,
		"account that is not there": `INSERT INTO "finance_account_share" ("id", "finance_account_id", "grantee_user_id", "finance_share_level", "created_at")
			VALUES ('reverse-finance-orphan', 'nothing', '` + granteeId + `', 'balance', now())`,
	} {
		if err := execute(database, statement); err == nil {
			t.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// Deleting the account takes its shares.
	dbtest.Exec(t, database, `DELETE FROM "agent_finance_account" WHERE "id" = 'reverse-finance-account'`)
	if got := dbtest.QueryString(t, database, `SELECT count(*)::text FROM "finance_account_share"`); got != "0" {
		t.Errorf("%s shares are left after their account went", got)
	}

	reverse, err := os.ReadFile("0254_finance_account_share.reverse.sql")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(t, database, string(reverse))
	if got := dbtest.QueryString(t, database, `SELECT coalesce(to_regclass('"finance_account_share"')::text, '')`); got != "" {
		t.Errorf("the share table is still there: %q", got)
	}
}
