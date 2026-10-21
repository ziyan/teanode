package migrations_test

import (
	"os"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// The passwords migration refuses an item that is not a login, card or note,
// an id that is not a lower case UUID, a card the agent may use, an agent key
// without the agent's use, a share with no grantee or two, a second share with
// the same grantee, a second key for one recipient, a second agent key, and a
// second saved version of an item with the same revision. A deleted account
// takes everything of its own with it, and the reverse file takes the tables,
// the sequence and the permission away.
func TestThePasswordsMigrationKeepsItsConstraintsAndReverses(test *testing.T) {
	test.Parallel()

	database, releaseDatabase := dbtest.AcquireDatabase(test)
	defer releaseDatabase()
	if err := database.Migrate(); err != nil {
		test.Fatalf("failed to migrate: %s", err)
	}
	userId := dbtest.CreateUser(test, database, "reverse-passwords-user")
	otherId := dbtest.CreateUser(test, database, "reverse-passwords-other")
	const firstItemId = "11111111-1111-4111-8111-111111111111"
	const secondItemId = "22222222-2222-4222-8222-222222222222"
	for _, statement := range []string{
		`INSERT INTO "circle" ("id", "created_at", "modified_at", "name", "circle_kind") VALUES ('reverse-passwords-circle', now(), now(), 'Home', 'family')`,
		`INSERT INTO "password_setup" ("user_id", "account_key_by_passphrase", "passphrase_salt", "passphrase_iteration_count", "passphrase_proof_hash", "account_key_by_recovery_key", "recovery_key_proof_hash", "public_key", "private_key_by_account_key", "created_at", "updated_at") VALUES ('` + userId + `', '{}', 'salt', 600000, 'hash', '{}', 'hash', 'key', '{}', now(), now())`,
		`INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "created_at", "updated_at") VALUES ('` + firstItemId + `', '` + userId + `', 'login', '{}', '{}', now(), now())`,
		`INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "created_at", "updated_at") VALUES ('` + secondItemId + `', '` + userId + `', 'note', '{}', '{}', now(), now())`,
		`INSERT INTO "password_share" ("id", "item_id", "owner_user_id", "grantee_user_id", "created_at") VALUES ('reverse-passwords-share', '` + firstItemId + `', '` + userId + `', '` + otherId + `', now())`,
		`INSERT INTO "password_share_key" ("item_id", "recipient_user_id", "item_key_by_recipient_key", "recipient_public_key", "created_at") VALUES ('` + firstItemId + `', '` + otherId + `', '{}', 'key', now())`,
		`INSERT INTO "password_agent_key" ("public_key", "private_key_sealed", "created_at") VALUES ('key', 'sealed', now())`,
		`INSERT INTO "password_item_version" ("item_id", "revision", "encrypted_fields", "item_key_by_account_key", "replaced_at") VALUES ('` + firstItemId + `', 1, '{}', '{}', now())`,
	} {
		dbtest.Exec(test, database, statement)
	}

	// A new item takes the defaults the code relies on, and the revisions of
	// two items differ and rise.
	if got := dbtest.QueryString(test, database, `SELECT is_usable_by_agent::text || ',' || coalesce(item_key_by_agent_key, 'null') || ',' || coalesce(deleted_at::text, 'null') FROM "password_item" WHERE "id" = '`+firstItemId+`'`); got != "false,null,null" {
		test.Errorf("a new item has defaults %q", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT (SELECT revision FROM "password_item" WHERE "id" = '`+secondItemId+`') > (SELECT revision FROM "password_item" WHERE "id" = '`+firstItemId+`')`); got != "true" {
		test.Errorf("the second item does not have the higher revision: %q", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT lock_after_minutes::text FROM "password_setup" WHERE "user_id" = '`+userId+`'`); got != "15" {
		test.Errorf("a new setup locks after %q minutes", got)
	}

	// The columns the migration adds to existing and new tables take their
	// defaults, and exist until the reverse file runs.
	if got := dbtest.QueryString(test, database, `SELECT needs_new_key::text || ',' || is_saved_by_person::text FROM "password_item" WHERE "id" = '`+firstItemId+`'`); got != "false,false" {
		test.Errorf("a new item has needs_new_key and is_saved_by_person %q", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT is_saved_by_person::text FROM "password_item_version" WHERE "item_id" = '`+firstItemId+`'`); got != "false" {
		test.Errorf("a new version has is_saved_by_person %q", got)
	}
	addedColumns := `SELECT count(*)::text FROM information_schema.columns WHERE table_schema = current_schema() AND (
		(table_name = 'agent_interaction' AND column_name = 'is_started_by_person') OR
		(table_name = 'password_item' AND column_name IN ('needs_new_key', 'is_saved_by_person')) OR
		(table_name = 'password_item_version' AND column_name = 'is_saved_by_person'))`
	if got := dbtest.QueryString(test, database, addedColumns); got != "4" {
		test.Errorf("%s of the 4 columns the migration adds are there before the reverse", got)
	}

	for name, statement := range map[string]string{
		"badkind":        `INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "created_at", "updated_at") VALUES ('33333333-3333-4333-8333-333333333333', '` + userId + `', 'vault', '{}', '{}', now(), now())`,
		"upperid":        `INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "created_at", "updated_at") VALUES ('AAAAAAAA-3333-4333-8333-333333333333', '` + userId + `', 'login', '{}', '{}', now(), now())`,
		"notauuid":       `INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "created_at", "updated_at") VALUES ('not-a-uuid', '` + userId + `', 'login', '{}', '{}', now(), now())`,
		"usablecard":     `INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "is_usable_by_agent", "item_key_by_agent_key", "created_at", "updated_at") VALUES ('44444444-4444-4444-8444-444444444444', '` + userId + `', 'card', '{}', '{}', true, '{}', now(), now())`,
		"agentkeyalone":  `INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "item_key_by_agent_key", "created_at", "updated_at") VALUES ('55555555-5555-4555-8555-555555555555', '` + userId + `', 'login', '{}', '{}', '{}', now(), now())`,
		"badlock":        `UPDATE "password_setup" SET "lock_after_minutes" = 10081`,
		"zeroiterations": `UPDATE "password_setup" SET "passphrase_iteration_count" = 0`,
		"nograntee":      `INSERT INTO "password_share" ("id", "item_id", "owner_user_id", "created_at") VALUES ('reverse-passwords-nobody', '` + firstItemId + `', '` + userId + `', now())`,
		"bothgrantees":   `INSERT INTO "password_share" ("id", "item_id", "owner_user_id", "grantee_user_id", "grantee_circle_id", "created_at") VALUES ('reverse-passwords-both', '` + secondItemId + `', '` + userId + `', '` + otherId + `', 'reverse-passwords-circle', now())`,
		"sharetwice":     `INSERT INTO "password_share" ("id", "item_id", "owner_user_id", "grantee_user_id", "created_at") VALUES ('reverse-passwords-twice', '` + firstItemId + `', '` + userId + `', '` + otherId + `', now())`,
		"keytwice":       `INSERT INTO "password_share_key" ("item_id", "recipient_user_id", "item_key_by_recipient_key", "recipient_public_key", "created_at") VALUES ('` + firstItemId + `', '` + otherId + `', '{}', 'key', now())`,
		"secondagentkey": `INSERT INTO "password_agent_key" ("singleton", "public_key", "private_key_sealed", "created_at") VALUES (false, 'key', 'sealed', now())`,
		"twoagentkeys":   `INSERT INTO "password_agent_key" ("public_key", "private_key_sealed", "created_at") VALUES ('key', 'sealed', now())`,
		"versiontwice":   `INSERT INTO "password_item_version" ("item_id", "revision", "encrypted_fields", "item_key_by_account_key", "replaced_at") VALUES ('` + firstItemId + `', 1, '{}', '{}', now())`,
		"versionnoitem":  `INSERT INTO "password_item_version" ("item_id", "revision", "encrypted_fields", "item_key_by_account_key", "replaced_at") VALUES ('66666666-6666-4666-8666-666666666666', 1, '{}', '{}', now())`,
	} {
		if err := execute(database, statement); err == nil {
			test.Errorf("%s: the database accepted a row it should refuse", name)
		}
	}

	// An item takes its shares and keys with it, a circle takes the shares
	// made to it, and an account takes everything of its own.
	dbtest.Exec(test, database, `INSERT INTO "password_share" ("id", "item_id", "owner_user_id", "grantee_circle_id", "created_at") VALUES ('reverse-passwords-circle-share', '`+secondItemId+`', '`+userId+`', 'reverse-passwords-circle', now())`)
	dbtest.Exec(test, database, `DELETE FROM "circle" WHERE "id" = 'reverse-passwords-circle'`)
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share" WHERE "id" = 'reverse-passwords-circle-share'`); got != "0" {
		test.Errorf("a deleted circle leaves %s shares", got)
	}
	dbtest.Exec(test, database, `DELETE FROM "user" WHERE "id" = '`+otherId+`'`)
	for _, table := range []string{"password_share", "password_share_key"} {
		if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "`+table+`"`); got != "0" {
			test.Errorf("a deleted grantee leaves %s rows in %s", got, table)
		}
	}
	dbtest.Exec(test, database, `DELETE FROM "user" WHERE "id" = '`+userId+`'`)
	for _, table := range []string{"password_setup", "password_item", "password_item_version"} {
		if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "`+table+`"`); got != "0" {
			test.Errorf("a deleted account leaves %s rows in %s", got, table)
		}
	}

	reverse, err := os.ReadFile("0251_passwords.reverse.sql")
	if err != nil {
		test.Fatalf("ReadFile: %s", err)
	}
	dbtest.Exec(test, database, string(reverse))
	for _, table := range []string{"password_setup", "password_item", "password_item_version", "password_share", "password_share_key", "password_agent_key"} {
		if got := dbtest.QueryString(test, database, `SELECT coalesce(to_regclass('"`+table+`"')::text, '')`); got != "" {
			test.Errorf("the %s table is still there: %q", table, got)
		}
	}
	if got := dbtest.QueryString(test, database, addedColumns); got != "0" {
		test.Errorf("%s of the columns the migration added are still there after the reverse", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT coalesce(to_regclass('"password_item_revision"')::text, '')`); got != "" {
		test.Errorf("the revision sequence is still there: %q", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "role_permission" WHERE "permission_key" = 'passwords:use'`); got != "0" {
		test.Errorf("the permission is still granted to %s roles", got)
	}
}
