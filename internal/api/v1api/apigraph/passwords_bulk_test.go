package apigraph

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

// Some tests are about a bound (how many bytes a person may keep, how many
// versions fit in the allowance) and need data of real size to reach it. Sending
// each item through the API under the race detector costs about a tenth of a
// second, so these tests save the first item through the API, as the proof of
// the path, and make the many rows that fill the room with one SQL statement
// each. The API is then used again for the save that is meant to be refused.

// quotedPasswordItemIDs is the ids as a SQL list of text values. The ids the
// tests make are hexadecimal digits and hyphens only.
func quotedPasswordItemIDs(test *testing.T, itemIDs []string) string {
	test.Helper()
	quoted := make([]string, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		if strings.Trim(itemID, "0123456789abcdef-") != "" {
			test.Fatalf("%q is not an item id the tests make", itemID)
		}
		quoted = append(quoted, "'"+itemID+"'")
	}
	return "ARRAY[" + strings.Join(quoted, ", ") + "]::text[]"
}

// copyPasswordItems stores one row for each of these ids with the same owner,
// kind, sealed fields and sealed key as the item sourceItemID, which the test
// saved through the API. The rows are neither deleted nor shared.
func (self *passwordsWorld) copyPasswordItems(test *testing.T, sourceItemID string, itemIDs []string) {
	test.Helper()
	if len(itemIDs) == 0 {
		return
	}
	dbtest.Exec(test, self.database, `INSERT INTO "password_item" ("id", "owner_user_id", "item_kind", "encrypted_fields", "item_key_by_account_key", "created_at", "updated_at")
		SELECT copy.id, source.owner_user_id, source.item_kind, source.encrypted_fields, source.item_key_by_account_key, now(), now()
		FROM unnest(`+quotedPasswordItemIDs(test, itemIDs)+`) AS copy(id), "password_item" source WHERE source.id = '`+sourceItemID+`'`)
}

// deletePasswordItemsInBulk marks these items deleted the way a person's delete
// does: a new revision, no agent use and no copy of the key for the agent.
func (self *passwordsWorld) deletePasswordItemsInBulk(test *testing.T, itemIDs []string) {
	test.Helper()
	if len(itemIDs) == 0 {
		return
	}
	dbtest.Exec(test, self.database, `UPDATE "password_item" SET "deleted_at" = now(), "updated_at" = now(), "revision" = nextval('password_item_revision'),
		"is_usable_by_agent" = false, "item_key_by_agent_key" = NULL WHERE "id" = ANY(`+quotedPasswordItemIDs(test, itemIDs)+`)`)
}

// purgePasswordItemsInBulk deletes these rows forever, with their versions.
func (self *passwordsWorld) purgePasswordItemsInBulk(test *testing.T, itemIDs []string) {
	test.Helper()
	if len(itemIDs) == 0 {
		return
	}
	dbtest.Exec(test, self.database, `DELETE FROM "password_item" WHERE "id" = ANY(`+quotedPasswordItemIDs(test, itemIDs)+`)`)
}

// addKeptVersionsInBulk gives each of these items versionsEach kept versions,
// copies of what the item holds now, replaced from two hours ago back in steps
// of one hour, so that the next save of the item is more than the spacing after
// the newest of them and keeps its version too. None was saved by the person.
func (self *passwordsWorld) addKeptVersionsInBulk(test *testing.T, itemIDs []string, versionsEach int) {
	test.Helper()
	dbtest.Exec(test, self.database, `INSERT INTO "password_item_version" ("item_id", "revision", "encrypted_fields", "item_key_by_account_key", "is_saved_by_person", "replaced_at")
		SELECT item.id, nextval('password_item_revision'), item.encrypted_fields, item.item_key_by_account_key, false, now() - (steps.step + 1) * interval '1 hour'
		FROM "password_item" item, generate_series(1, `+strconv.Itoa(versionsEach)+`) AS steps(step) WHERE item.id = ANY(`+quotedPasswordItemIDs(test, itemIDs)+`)`)
}
