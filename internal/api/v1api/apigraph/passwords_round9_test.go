package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A person with more deleted rows than PostgreSQL takes parameters in a
// statement (65,535) still reads their whole list, and a device that holds
// everything reads what changed.
func TestAFirstReadAnswersMoreDeletedRowsThanAStatementTakesParameters(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	fields, key := sealedOf(""), sealedOf("")
	dbtest.Exec(test, world.database, fmt.Sprintf(`INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at, deleted_at, revision)
		SELECT '00000009-0000-4000-8000-' || lpad(n::text, 12, '0'), '%s', 'note', '%s', '%s', now(), now(), now(), nextval('password_item_revision')
		FROM generate_series(1, 70000) n`, world.ada.ID, fields, key))
	list := world.list(test, world.ada, nil)
	if got := len(list["items"].([]any)); got != 70000 {
		test.Fatalf("the first read answered %d items, want 70000", got)
	}
	if got := len(world.list(test, world.ada, list["revision"])["items"].([]any)); got != 0 {
		test.Fatalf("a read from the revision it was told answered %d items", got)
	}
}

// A save that makes an item larger for a person it is shared with, who would
// then hold too much, says whose allowance it is and what to do.
func TestSavingAnItemLargerThanAnotherPersonMayHoldSaysSo(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	saved := world.save(test, ada, "note")
	itemId := saved["id"].(string)
	shared := world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(itemId,
		[]any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(itemId, world.ben, ben)}))["SetPasswordItemShares"].(map[string]any)
	if recipient := shared["shares"].([]any)[0].(map[string]any)["recipients"].([]any)[0].(map[string]any); recipient["hasKey"] != true {
		test.Fatalf("ben has no key: %v", recipient)
	}
	// Ben already holds a large item from somebody else, put in past the checks.
	dbtest.Exec(test, world.database, fmt.Sprintf(`INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at, revision)
		VALUES ('00000007-0000-4000-8000-000000000001', '%s', 'note', repeat('x', %d), '%s', now(), now(), nextval('password_item_revision'))`,
		world.cora.ID, models.PasswordsSharedBytesMost-100_000, sealedOf("")))
	dbtest.Exec(test, world.database, fmt.Sprintf(`INSERT INTO password_share_key (item_id, recipient_user_id, item_key_by_recipient_key, recipient_public_key, is_stored_by_person, created_at)
		VALUES ('00000007-0000-4000-8000-000000000001', '%s', 'key', '%s', true, now())`, world.ben.ID, ben.publicKey))

	input := ada.namedItemInput(itemId, "note", shared["revision"])
	input["encryptedFields"] = sealedOfSize(500 * 1024)
	message := world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})
	const want = "ben has too much shared with them: make this item smaller or stop sharing it with them"
	if !strings.Contains(message, want) {
		test.Fatalf("the save past the allowance: %s", message)
	}
	input["encryptedFields"] = sealedOf("a small change")
	world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})
}
