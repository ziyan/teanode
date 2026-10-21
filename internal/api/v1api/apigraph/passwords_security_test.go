package apigraph

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const (
	rekeyPasswordItemQuery = `mutation ($input: PasswordItemRekeyInput!) { RekeyPasswordItem(input: $input) { ` + passwordItemFields + ` } }`
	passwordsSettingsQuery = `mutation ($lockAfterMinutes: Int!) { UpdatePasswordsSettings(lockAfterMinutes: $lockAfterMinutes) { lockAfterMinutes } }`
)

// rekeyInput is what a device sends to give an item a new item key.
func rekeyInput(id string, baseRevision any, shareKeys []any, agentKey any) map[string]any {
	if shareKeys == nil {
		shareKeys = []any{}
	}
	return map[string]any{"input": map[string]any{
		"id": id, "baseRevision": baseRevision, "encryptedFields": sealedOf("fields under the new key of " + id),
		"itemKey": sealedOf("new item key of " + id), "shareKeys": shareKeys, "itemKeyByAgentKey": agentKey,
	}}
}

// sealedOfSize is a sealed value of about this many bytes, in the shape the
// server checks.
func sealedOfSize(size int) string {
	return `{"version":1,"nonce":"` + base64.StdEncoding.EncodeToString(make([]byte, 12)) + `","ciphertext":"` + strings.Repeat("A", size-size%4) + `"}`
}

func lastRevisionOf(test *testing.T, world *passwordsWorld) string {
	test.Helper()
	return dbtest.QueryString(test, world.database, `SELECT last_value FROM "password_item_revision"`)
}

// Giving an item a new item key replaces its fields and keys at one revision,
// needs no tap, keeps only the keys it is given for people the item still
// reaches (the rest wait), and ends or keeps the agent's use of it but never
// begins it.
func TestRekeyingAPasswordItemReplacesItsKeys(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben, cora := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben), newPasswordsDevice(test, world.cora)
	world.setUp(test, ada)
	world.setUp(test, ben)
	world.setUp(test, cora)
	id := world.save(test, ada, "login")["id"].(string)
	other := world.save(test, ada, "login")
	notUsedByAgent, otherRevision := other["id"].(string), other["revision"]
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(id, world.ben, ben)}))
	usable := world.run(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{
		"itemId": id, "isUsableByAgent": true, "itemKeyByAgentKey": sealedForKeyPairOf("agent copy under the old key")}})["SetPasswordItemUsableByAgent"].(map[string]any)
	revision := usable["revision"]
	if got := world.list(test, world.ben, nil)["items"].([]any); len(got) != 1 {
		test.Fatalf("ben reads before: %v", got)
	}
	revisionBefore := lastRevisionOf(test, world)

	newAgentCopy := sealedForKeyPairOf("agent copy under the new key")
	for name, attempt := range map[string]struct {
		person *models.User
		input  map[string]any
		want   string
	}{
		"a stale base revision":                      {world.ada, rekeyInput(id, 1, []any{shareKeyInput(id, world.ben, ben)}, newAgentCopy), "changed on another device"},
		"a key for a person the item does not reach": {world.ada, rekeyInput(id, revision, []any{shareKeyInput(id, world.cora, cora)}, newAgentCopy), "not found"},
		"a key for an old public key": {world.ada, rekeyInput(id, revision, []any{map[string]any{
			"itemId": id, "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("key"), "recipientPublicKey": newPasswordsPublicKey(test)}}, newAgentCopy),
			"That person's key changed"},
		"a key that names another item": {world.ada, rekeyInput(id, revision, []any{shareKeyInput(notUsedByAgent, world.ben, ben)}, newAgentCopy), "invalid arguments"},
		"a key that is not sealed for a key pair": {world.ada, rekeyInput(id, revision, []any{map[string]any{
			"itemId": id, "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedOf("plain"), "recipientPublicKey": ben.publicKey}}, newAgentCopy), "invalid arguments"},
		"an agent copy that is not sealed for a key pair": {world.ada, rekeyInput(id, revision, nil, sealedOf("plain")), "invalid arguments"},
		"fields that are not sealed": {world.ada, map[string]any{"input": map[string]any{
			"id": id, "baseRevision": revision, "encryptedFields": "not json", "itemKey": sealedOf("k"), "shareKeys": []any{}, "itemKeyByAgentKey": nil}}, "invalid arguments"},
		"an agent copy for an item the agent does not use": {world.ada, rekeyInput(notUsedByAgent, otherRevision, nil, newAgentCopy), "invalid arguments"},
		"somebody who is not the owner":                    {world.ben, rekeyInput(id, revision, nil, nil), "not found"},
		"an item that is not there":                        {world.ada, rekeyInput("00000000-0000-4000-8000-00000000ffff", revision, nil, nil), "not found"},
	} {
		if message := world.refusal(test, attempt.person, rekeyPasswordItemQuery, attempt.input); !strings.Contains(message, attempt.want) {
			test.Errorf("%s: %s", name, message)
		}
	}
	if got := lastRevisionOf(test, world); got != revisionBefore {
		test.Fatalf("refused re-keys drew revisions: %s then %s", revisionBefore, got)
	}
	if got := world.list(test, world.ben, nil)["items"].([]any); len(got) != 1 || got[0].(map[string]any)["encryptedFields"] != sealedOf("fields of "+id) {
		test.Fatalf("a refused re-key changed what ben holds: %v", got)
	}

	// A token may re-key (it widens nothing). Keeping the agent: a new copy.
	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, rekeyPasswordItemQuery,
		rekeyInput(id, revision, []any{shareKeyInput(id, world.ben, ben)}, newAgentCopy))
	if len(failures) > 0 {
		test.Fatalf("a token re-keying: %v", failures)
	}
	rekeyed := data["RekeyPasswordItem"].(map[string]any)
	if rekeyed["revision"].(float64) <= revision.(float64) || rekeyed["encryptedFields"] != sealedOf("fields under the new key of "+id) ||
		rekeyed["itemKey"] != sealedOf("new item key of "+id) || rekeyed["isUsableByAgent"] != true {
		test.Fatalf("the re-keyed item: %v", rekeyed)
	}
	recipient := rekeyed["shares"].([]any)[0].(map[string]any)["recipients"].([]any)[0].(map[string]any)
	if recipient["userId"] != world.ben.ID || recipient["hasKey"] != true {
		test.Fatalf("ben keeps a key: %v", recipient)
	}
	benItem := world.list(test, world.ben, nil)["items"].([]any)[0].(map[string]any)
	if benItem["itemKey"] != sealedForKeyPairOf("item key for "+world.ben.Username) || benItem["encryptedFields"] != sealedOf("fields under the new key of "+id) {
		test.Fatalf("ben reads the new fields: %v", benItem)
	}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		usableItems, err := tx.ListAgentUsablePasswordItems(world.ada.ID)
		if err != nil || len(usableItems) != 1 || usableItems[0].ItemKeyByAgentKey != newAgentCopy {
			test.Fatalf("the agent's copy: %+v %v", usableItems, err)
		}
	})

	// Without a key for ben he waits; a null agent copy ends the agent's use.
	rekeyed = world.run(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, rekeyed["revision"], nil, nil))["RekeyPasswordItem"].(map[string]any)
	if rekeyed["isUsableByAgent"] != false {
		test.Fatalf("the agent still uses it: %v", rekeyed)
	}
	if recipient := rekeyed["shares"].([]any)[0].(map[string]any)["recipients"].([]any)[0].(map[string]any); recipient["hasKey"] != false {
		test.Fatalf("ben still has the old key: %v", recipient)
	}
	if got := world.list(test, world.ben, nil)["items"].([]any); len(got) != 0 {
		test.Fatalf("ben reads without a key: %v", got)
	}
	waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any)
	if len(waiting) != 1 || waiting[0].(map[string]any)["itemId"] != id || waiting[0].(map[string]any)["recipient"].(map[string]any)["userId"] != world.ben.ID {
		test.Fatalf("ben waits: %v", waiting)
	}

	// The audit log has the re-keys and no sealed value.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourcePasswordItem), ResourceID: id, Limit: 100})
		if err != nil {
			test.Fatal(err)
		}
		rekeyedCount := 0
		for _, event := range events {
			text := string(event.Before) + string(event.After)
			if strings.Contains(text, "rekeyed") {
				rekeyedCount++
			}
			for _, secret := range []string{"fields under the new key", "new item key", "agent copy", "ciphertext", "nonce"} {
				if strings.Contains(text, secret) {
					test.Errorf("an audit event holds %q: %s", secret, text)
				}
			}
		}
		if rekeyedCount != 2 {
			test.Fatalf("%d re-keys were audited, want 2", rekeyedCount)
		}
	})

	// A deleted item can be re-keyed, so that a restore can follow; it takes
	// no key for anybody.
	deleted := world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": id})["DeletePasswordItem"].(map[string]any)
	if message := world.refusal(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, deleted["revision"], []any{shareKeyInput(id, world.ben, ben)}, nil)); !strings.Contains(message, "not found") {
		test.Fatalf("a key for a deleted item: %s", message)
	}
	rekeyedDeleted := world.run(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, deleted["revision"], nil, nil))["RekeyPasswordItem"].(map[string]any)
	if rekeyedDeleted["deletedAt"] == nil || rekeyedDeleted["itemKey"] != sealedOf("new item key of "+id) {
		test.Fatalf("a deleted item re-keyed: %v", rekeyedDeleted)
	}
}

// A program with a token needs no tap to save an item, so it can save garbage
// over one as often as it likes. The content from before the burst is kept.
func TestGarbageSavedByATokenDoesNotEraseTheOriginal(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	item := world.save(test, ada, "login")
	id := item["id"].(string)
	original := item["encryptedFields"].(string)
	revision := item["revision"]
	for index := 0; index < 25; index++ {
		input := ada.namedItemInput(id, "login", revision)
		input["encryptedFields"] = sealedOf(fmt.Sprintf("garbage %d", index))
		data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, savePasswordItemQuery, map[string]any{"input": input})
		if len(failures) > 0 {
			test.Fatalf("save %d: %v", index, failures)
		}
		revision = data["SavePasswordItem"].(map[string]any)["revision"]
	}
	escaped := strings.ReplaceAll(original, "'", "''")
	if got := dbtest.QueryString(test, world.database, fmt.Sprintf(`SELECT count(*) FROM "password_item_version" WHERE "item_id" = '%s' AND "encrypted_fields" = '%s'`, id, escaped)); got != "1" {
		test.Fatalf("the original is kept %s times after 25 saves by a token, want once", got)
	}
	if got := dbtest.QueryString(test, world.database, fmt.Sprintf(`SELECT count(*) FROM "password_item_version" WHERE "item_id" = '%s'`, id)); got != "1" {
		test.Fatalf("%s versions are kept after a burst of saves, want 1", got)
	}
}

// Making the time a device stays unlocked longer needs a person's tap, since a
// token could otherwise leave every device open for a week. Making it shorter,
// or keeping it, does not.
func TestLengtheningHowLongPasswordsStayUnlockedNeedsATap(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)

	_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, passwordsSettingsQuery, map[string]any{"lockAfterMinutes": 10080})
	if !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token lengthening the time: %v", failures)
	}
	if setup := world.run(test, world.ada, readPasswordsSetupQuery, nil)["ReadPasswordsSetup"].(map[string]any); setup["lockAfterMinutes"] != float64(15) {
		test.Fatalf("a refused change changed the time: %v", setup)
	}
	if updated := world.run(test, world.ada, passwordsSettingsQuery, map[string]any{"lockAfterMinutes": 120})["UpdatePasswordsSettings"].(map[string]any); updated["lockAfterMinutes"] != float64(120) {
		test.Fatalf("a tap lengthening the time: %v", updated)
	}
	for _, minutes := range []int{120, 5} {
		data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, passwordsSettingsQuery, map[string]any{"lockAfterMinutes": minutes})
		if len(failures) > 0 || data["UpdatePasswordsSettings"].(map[string]any)["lockAfterMinutes"] != float64(minutes) {
			test.Fatalf("a token keeping or shortening the time to %d: %v %v", minutes, data, failures)
		}
	}
	// Back up past what it is now: a token is refused again.
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, passwordsSettingsQuery, map[string]any{"lockAfterMinutes": 6}); !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token lengthening the time by a minute: %v", failures)
	}
}

// A person's items hold at most models.PasswordsBytesMost bytes of sealed text,
// whoever saves them. The answer says what to do, and deleting makes room.
func TestPasswordsRefuseMoreThanTheBytesAPersonMayKeep(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)

	var firstID string
	savedCount := 0
	var message string
	for savedCount < 200 {
		input := ada.itemInput("note", nil)
		input["encryptedFields"] = sealedOfSize(500000)
		data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, savePasswordItemQuery, map[string]any{"input": input})
		if len(failures) > 0 {
			message = strings.Join(failures, "; ")
			break
		}
		if savedCount == 0 {
			firstID = data["SavePasswordItem"].(map[string]any)["id"].(string)
		}
		savedCount++
	}
	if !strings.Contains(message, "Passwords is full: delete items or make notes shorter") {
		test.Fatalf("after %d notes of 500000 bytes: %q", savedCount, message)
	}
	if limit := models.PasswordsBytesMost / 500000; savedCount < limit-3 || savedCount > limit+1 {
		test.Fatalf("%d notes were kept before the limit, want about %d", savedCount, limit)
	}
	// Somebody else's room is their own.
	input := ben.itemInput("note", nil)
	input["encryptedFields"] = sealedOfSize(500000)
	world.run(test, world.ben, savePasswordItemQuery, map[string]any{"input": input})

	// Deleting an item makes room for a new note at once, and restoring the
	// deleted one takes that room back, so it is refused while the room is full.
	world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": firstID})
	input = ada.itemInput("note", nil)
	input["encryptedFields"] = sealedOfSize(500000)
	world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})
	if message := world.refusal(test, world.ada, restorePasswordItemQuery, map[string]any{"id": firstID}); !strings.Contains(message, "Passwords is full") {
		test.Fatalf("a restore with no room: %s", message)
	}
}

// Deleted items and the versions that saves keep have an allowance of their
// own (models.PasswordsKeptBytesMost), apart from the room for items that are
// not deleted. A token that fills the room with junk is undone by the person
// deleting it, which frees the room at once; what is deleted and kept cannot
// take the table far past its allowance, because a token's delete and a token's
// save that would keep a version are refused when it is full, while the person's
// own taps are not, and deleting forever empties it.
func TestDeletedPasswordItemsAndKeptVersionsHaveTheirOwnAllowance(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	const size = 512*1024 - 200
	save := func(person *models.User, device *passwordsDevice, id string, base any, size int, credentialKind models.CredentialKind) (map[string]any, []string) {
		input := device.namedItemInput(id, "note", base)
		input["encryptedFields"] = sealedOfSize(size)
		return world.callWithCredential(test, person, credentialKind, savePasswordItemQuery, map[string]any{"input": input})
	}
	rowBytes := func(person *models.User, isDeleted bool) int64 {
		condition := "deleted_at IS NULL"
		if isDeleted {
			condition = "deleted_at IS NOT NULL"
		}
		total := dbtest.QueryString(test, world.database, `SELECT (SELECT coalesce(sum(octet_length(encrypted_fields) + octet_length(item_key_by_account_key)), 0) FROM password_item WHERE owner_user_id = '`+person.ID+`' AND `+condition+`)::text`)
		return numberOfText(test, total)
	}
	versionBytes := func(person *models.User) int64 {
		total := dbtest.QueryString(test, world.database, `SELECT (SELECT coalesce(sum(octet_length(encrypted_fields) + octet_length(item_key_by_account_key)), 0) FROM password_item_version WHERE item_id IN (SELECT id FROM password_item WHERE owner_user_id = '`+person.ID+`'))::text`)
		return numberOfText(test, total)
	}

	// A token fills the room with junk; the person deletes all of it, and the
	// room is free again at once. Three rounds put about 192 MiB in the deleted
	// items, which the person's taps are never refused for.
	itemID := func(round, index int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012d", round+1, index) }
	var fullCounts []int
	for round := 0; round < 3; round++ {
		fullAt := -1
		for index := 0; index < 200; index++ {
			if _, failures := save(world.ada, ada, itemID(round, index), nil, size, models.CredentialToken); len(failures) > 0 {
				if !strings.Contains(strings.Join(failures, "; "), "Passwords is full") {
					test.Fatalf("round %d: %v", round, failures)
				}
				fullAt = index
				break
			}
		}
		if fullAt < 100 {
			test.Fatalf("round %d: the room ran out after %d notes of about 0.5 MiB", round, fullAt)
		}
		if held := rowBytes(world.ada, false); held > models.PasswordsBytesMost {
			test.Fatalf("round %d: the person's items hold %d bytes, past the %d limit", round, held, models.PasswordsBytesMost)
		}
		for index := 0; index < fullAt; index++ {
			world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": itemID(round, index)})
		}
		fullCounts = append(fullCounts, fullAt)
	}
	if held := rowBytes(world.ada, true); held < models.PasswordsKeptBytesMost {
		test.Fatalf("only %d bytes are in deleted items, not past the %d allowance", held, models.PasswordsKeptBytesMost)
	}

	// The allowance is full. A token still saves a new note (it adds nothing to
	// the allowance), but cannot delete it, nor save over it, in words that
	// say what to do; the person's own taps are not refused.
	if _, failures := save(world.ada, ada, itemID(9, 0), nil, size, models.CredentialToken); len(failures) > 0 {
		test.Fatalf("a token saving a new note into the free room: %v", failures)
	}
	if message := strings.Join(func() []string {
		_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, deletePasswordItemQuery, map[string]any{"id": itemID(9, 0)})
		return failures
	}(), "; "); !strings.Contains(message, "keeping too many deleted items") {
		test.Fatalf("a token's delete with the allowance full: %q", message)
	}
	current := world.list(test, world.ada, nil)
	var revision any
	for _, raw := range current["items"].([]any) {
		if item := raw.(map[string]any); item["id"] == itemID(9, 0) {
			revision = item["revision"]
		}
	}
	if _, failures := save(world.ada, ada, itemID(9, 0), revision, size-10, models.CredentialToken); len(failures) == 0 || !strings.Contains(strings.Join(failures, "; "), "keeping too many deleted items") {
		test.Fatalf("a token's save over a note, allowance full: %v", failures)
	}
	versionsBefore := versionBytes(world.ada)
	if _, failures := save(world.ada, ada, itemID(9, 0), revision, size-10, models.CredentialSession); len(failures) > 0 {
		test.Fatalf("a tap's save, allowance full: %v", failures)
	}
	if got := versionBytes(world.ada); got != versionsBefore {
		test.Fatalf("a tap's save kept a version with the allowance full: %d then %d bytes", versionsBefore, got)
	}

	// Deleting forever is the person's tap. A token cannot, and an item that
	// is not deleted is refused; each purged item frees its bytes.
	if message := strings.Join(func() []string {
		_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, purgePasswordItemQuery, map[string]any{"id": itemID(0, 0)})
		return failures
	}(), "; "); !isTapRefusal(message) {
		test.Fatalf("a token deleting forever: %q", message)
	}
	if message := world.refusal(test, world.ada, purgePasswordItemQuery, map[string]any{"id": itemID(9, 0)}); !strings.Contains(message, "only a deleted item") {
		test.Fatalf("deleting forever an item that is not deleted: %q", message)
	}
	before := rowBytes(world.ada, true)
	for round := 0; round < 2; round++ {
		for index := 0; index < fullCounts[round]; index++ {
			world.run(test, world.ada, purgePasswordItemQuery, map[string]any{"id": itemID(round, index)})
		}
	}
	if freed := before - rowBytes(world.ada, true); freed < int64(fullCounts[0]+fullCounts[1]-2)*size {
		test.Fatalf("the purged items freed %d bytes", freed)
	}
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, deletePasswordItemQuery, map[string]any{"id": itemID(9, 0)}); len(failures) > 0 {
		test.Fatalf("a token's delete after the allowance was emptied: %v", failures)
	}
	if message := world.refusal(test, world.cora, purgePasswordItemQuery, map[string]any{"id": itemID(1, 0)}); isTapRefusal(message) {
		test.Fatalf("somebody else's item is refused for want of a tap, not as not found: %q", message)
	}

	// Versions count toward the same allowance. Five notes saved again and
	// again, an hour apart, keep a version each time (up to 60 for each note,
	// about 150 MiB in all); the saves stop at the allowance.
	ben := newPasswordsDevice(test, world.ben)
	world.setUp(test, ben)
	revisions := map[string]any{}
	for index := 0; index < 5; index++ {
		id := itemID(8, index)
		input := ben.namedItemInput(id, "note", nil)
		input["encryptedFields"] = sealedOfSize(size)
		revisions[id] = world.run(test, world.ben, savePasswordItemQuery, map[string]any{"input": input})["SavePasswordItem"].(map[string]any)["revision"]
	}
	refusedAt := -1
	for step := 0; step < 70 && refusedAt < 0; step++ {
		dbtest.Exec(test, world.database, `UPDATE password_item_version SET replaced_at = replaced_at - interval '2 hours' WHERE item_id IN (SELECT id FROM password_item WHERE owner_user_id = '`+world.ben.ID+`')`)
		for index := 0; index < 5; index++ {
			id := itemID(8, index)
			data, failures := save(world.ben, ben, id, revisions[id], size-4*(step+1), models.CredentialToken)
			if len(failures) > 0 {
				if !strings.Contains(strings.Join(failures, "; "), "keeping too many deleted items") {
					test.Fatalf("step %d: %v", step, failures)
				}
				refusedAt = step
				break
			}
			revisions[id] = data["SavePasswordItem"].(map[string]any)["revision"]
		}
	}
	if refusedAt < 0 {
		test.Fatalf("every save of five 0.5 MiB notes was accepted; the kept versions are outside the allowance")
	}
	if kept := versionBytes(world.ben) + rowBytes(world.ben, true); kept > models.PasswordsKeptBytesMost {
		test.Fatalf("ben's deleted items and versions hold %d bytes, past the %d allowance", kept, models.PasswordsKeptBytesMost)
	}
	if held := rowBytes(world.ben, false); held > models.PasswordsBytesMost {
		test.Fatalf("ben's items hold %d bytes, past the %d limit", held, models.PasswordsBytesMost)
	}
}

// A save that is refused does not use up a revision number; the counter is
// shared by everybody's items.
func TestRefusedPasswordSavesDrawNoRevisionNumber(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	item := world.save(test, ada, "login")
	id, revision := item["id"].(string), item["revision"]
	before := lastRevisionOf(test, world)
	for range 5 {
		// Somebody else's item, new or changed; an old revision; a new item
		// that claims a revision; a change of kind.
		world.refusal(test, world.ben, savePasswordItemQuery, map[string]any{"input": ben.namedItemInput(id, "login", nil)})
		world.refusal(test, world.ben, savePasswordItemQuery, map[string]any{"input": ben.namedItemInput(id, "login", revision)})
		world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.namedItemInput(id, "login", 999999)})
		world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.namedItemInput(passwordItemIdOfNumber(world.ada, 500), "login", 999999)})
		world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.namedItemInput(id, "note", revision)})
	}
	if after := lastRevisionOf(test, world); after != before {
		test.Fatalf("refused saves drew revision numbers: %s then %s", before, after)
	}
}

// One person's writes to Passwords are limited, separately for their own taps
// and for every other credential, so a program with a token cannot wear out
// what the person may do on their dashboard, nor one person what another may
// do. Reads are not limited.
func TestWritesToPasswordsAreLimitedForEachPerson(test *testing.T) {
	previous := passwordWritesBurst
	passwordWritesBurst = 5
	test.Cleanup(func() { passwordWritesBurst = previous })

	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	var lastID string
	for range 5 {
		input := ada.itemInput("login", nil)
		data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, savePasswordItemQuery, map[string]any{"input": input})
		if len(failures) > 0 {
			test.Fatalf("a save within the limit: %v", failures)
		}
		lastID = data["SavePasswordItem"].(map[string]any)["id"].(string)
	}
	before := lastRevisionOf(test, world)
	for name, attempt := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"save":       {savePasswordItemQuery, map[string]any{"input": ada.itemInput("login", nil)}},
		"delete":     {deletePasswordItemQuery, map[string]any{"id": lastID}},
		"restore":    {restorePasswordItemQuery, map[string]any{"id": lastID}},
		"share":      {setPasswordItemSharesQuery, sharesInput(lastID, nil, nil)},
		"share keys": {addPasswordShareKeysQuery, map[string]any{"keys": []any{}}},
		"agent use":  {passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": lastID, "isUsableByAgent": false}}},
		"re-key":     {rekeyPasswordItemQuery, rekeyInput(lastID, 1, nil, nil)},
	} {
		_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, attempt.query, attempt.variables)
		if message := strings.Join(failures, "; "); !strings.Contains(message, "too many attempts") || !strings.Contains(message, "Too many changes to Passwords") {
			test.Errorf("a %s over the limit: %q", name, message)
		}
	}
	if after := lastRevisionOf(test, world); after != before {
		test.Fatalf("calls turned away drew revision numbers: %s then %s", before, after)
	}
	// Reads are not counted, the person's own taps have their own allowance,
	// and so does another person.
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, listPasswordItemsQuery, nil); len(failures) > 0 {
		test.Fatalf("a read over the write limit: %v", failures)
	}
	for range 5 {
		world.save(test, ada, "login")
	}
	world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.itemInput("login", nil)})
	for range 5 {
		world.save(test, ben, "login")
	}
}

// Wrong proofs from a token are counted apart from the person's own taps: the
// token runs out of tries, and the person still unlocks on the dashboard. The
// taps have their own tries, which run out for them alone.
func TestWrongProofsFromATokenDoNotLockOutThePersonsTaps(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	wrongProof, _ := proofOf("a wrong secret")
	rightProof, _ := proofOf(ada.passphrase)

	for range passwordUnlockFailuresBurst {
		_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
		if message := strings.Join(failures, "; "); !strings.Contains(message, "That passphrase is not right.") {
			test.Fatalf("a wrong proof from a token: %s", message)
		}
	}
	_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, unlockPasswordsQuery, map[string]any{"proof": rightProof})
	if !strings.Contains(strings.Join(failures, "; "), "too many attempts") {
		test.Fatalf("the token after its tries: %v", failures)
	}
	if unlock := world.run(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": rightProof})["UnlockPasswords"].(map[string]any); unlock["publicKey"] != ada.publicKey {
		test.Fatalf("the person's tap after the token's wrong tries: %v", unlock)
	}
	// The taps run out on their own, and the token's bucket is untouched by it.
	for range passwordUnlockFailuresBurst {
		world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
	}
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": rightProof}); !strings.Contains(message, "too many attempts") {
		test.Fatalf("the taps after their tries: %s", message)
	}
}
