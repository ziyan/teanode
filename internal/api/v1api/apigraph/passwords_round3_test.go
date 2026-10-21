package apigraph

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func sequenceNumberOf(test *testing.T, world *passwordsWorld) int64 {
	test.Helper()
	return numberOfText(test, lastRevisionOf(test, world))
}

// numberOfText is the number a query answered as text.
func numberOfText(test *testing.T, text string) int64 {
	test.Helper()
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		test.Fatalf("%q is not a number: %s", text, err)
	}
	return number
}

// An item is marked needsNewKey on the owner's view when access to it narrows,
// the mark is the owner's alone, and only a re-key clears it.
func TestAnItemShowsItNeedsANewKeyUntilItIsRekeyed(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	item := world.save(test, ada, "login")
	id := item["id"].(string)
	if item["needsNewKey"] != false {
		test.Fatalf("a new item: %v", item)
	}

	shared := world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(id, world.ben, ben)}))["SetPasswordItemShares"].(map[string]any)
	if shared["needsNewKey"] != false {
		test.Fatalf("sharing: %v", shared)
	}
	benView := world.list(test, world.ben, nil)["items"].([]any)[0].(map[string]any)
	if benView["needsNewKey"] != false {
		test.Fatalf("the reader sees the owner's mark: %v", benView)
	}
	unshared := world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, nil, nil))["SetPasswordItemShares"].(map[string]any)
	if unshared["needsNewKey"] != true {
		test.Fatalf("removing a grantee: %v", unshared)
	}
	// A save keeps the mark; a re-key clears it.
	input := ada.namedItemInput(id, "login", unshared["revision"])
	saved := world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})["SavePasswordItem"].(map[string]any)
	if saved["needsNewKey"] != true {
		test.Fatalf("a save cleared the mark: %v", saved)
	}
	rekeyed := world.run(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, saved["revision"], nil, nil))["RekeyPasswordItem"].(map[string]any)
	if rekeyed["needsNewKey"] != false {
		test.Fatalf("a re-key: %v", rekeyed)
	}

	// The listing carries the mark, deleted items too; a restore keeps it.
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeCircleId": world.home}}, nil))
	world.run(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{shareKeyInputFor(id, rekeyed["itemKey"].(string), world.ben, ben)}})
	deleted := world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": id})["DeletePasswordItem"].(map[string]any)
	if deleted["needsNewKey"] != true || deleted["deletedAt"] == nil {
		test.Fatalf("deleting a shared item: %v", deleted)
	}
	restored := world.run(test, world.ada, restorePasswordItemQuery, map[string]any{"id": id})["RestorePasswordItem"].(map[string]any)
	if restored["needsNewKey"] != true || restored["deletedAt"] != nil {
		test.Fatalf("restoring: %v", restored)
	}
	listed := world.list(test, world.ada, nil)["items"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["needsNewKey"] != true {
		test.Fatalf("the listing: %v", listed)
	}

	// A circle share ends when the member leaves; the mark follows.
	again := world.run(test, world.ada, rekeyPasswordItemQuery, rekeyInput(id, restored["revision"], []any{shareKeyInputFor(id, "", world.ben, ben)}, nil))["RekeyPasswordItem"].(map[string]any)
	if again["needsNewKey"] != false {
		test.Fatalf("re-keying a restored item: %v", again)
	}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(world.home, world.ben.ID); err != nil {
			test.Fatal(err)
		}
	})
	left := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)
	if left["needsNewKey"] != true {
		test.Fatalf("after a member with a key left the circle: %v", left)
	}
}

// shareKeyInputFor is shareKeyInput for an item whose current key, as the item
// view shows it, is itemKey ("" for none, as a re-key sends).
func shareKeyInputFor(itemId, itemKey string, recipient *models.User, device *passwordsDevice) map[string]any {
	key := shareKeyInput(itemId, recipient, device)
	if itemKey == "" {
		delete(key, "itemKey")
	} else {
		key["itemKey"] = itemKey
	}
	return key
}

// AddPasswordShareKeys names the item key each key was sealed from and is
// refused when that is no longer the item's. A key already stored for the
// recipient's public key is not replaced, by it or by SetPasswordItemShares,
// even by the owner's own token.
func TestShareKeysNameTheItemKeyAndNeverReplaceAWorkingKey(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(id, world.ben, ben)}))
	working := world.list(test, world.ben, nil)["items"].([]any)[0].(map[string]any)["itemKey"]

	garbage := shareKeyInput(id, world.ben, ben)
	garbage["itemKeyByRecipientKey"] = sealedForKeyPairOf("garbage")
	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery, map[string]any{"keys": []any{garbage}})
	if len(failures) > 0 || data["AddPasswordShareKeys"] != float64(0) {
		test.Fatalf("a second key for ben: %v %v", data, failures)
	}
	garbageSet := shareSetKeyInput(id, world.ben, ben)
	garbageSet["itemKeyByRecipientKey"] = sealedForKeyPairOf("garbage two")
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, setPasswordItemSharesQuery,
		sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{garbageSet})); len(failures) > 0 {
		test.Fatalf("sharing again with the same grantee: %v", failures)
	}
	if after := world.list(test, world.ben, nil)["items"].([]any)[0].(map[string]any)["itemKey"]; after != working {
		test.Fatalf("ben's working key was replaced by %v", after)
	}

	// The item key is required, and it must be the current one.
	missing := shareKeyInputFor(id, "", world.ben, ben)
	if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{missing}}); !strings.Contains(message, "itemKey") {
		test.Fatalf("a key with no itemKey: %s", message)
	}
	stale := shareKeyInputFor(id, sealedOf("an older item key"), world.ben, ben)
	before := lastRevisionOf(test, world)
	if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{stale}}); !strings.Contains(message, "changed on another device") {
		test.Fatalf("a key sealed from an older item key: %s", message)
	}
	if after := lastRevisionOf(test, world); after != before {
		test.Fatalf("refused keys drew revisions: %s then %s", before, after)
	}
}

// One AddPasswordShareKeys call raises the revision only of the items whose
// stored keys changed, so keys that are already there burn no numbers, and it
// counts one write for each item it names.
func TestAddingShareKeysBurnsRevisionsOnlyForWhatChanged(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	const itemCount = 25
	keys := []any{}
	for index := 0; index < itemCount; index++ {
		id := world.save(test, ada, "login")["id"].(string)
		world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeCircleId": world.home}}, nil))
		keys = append(keys, shareKeyInput(id, world.ben, ben))
	}
	world.setUp(test, ben)

	before := sequenceNumberOf(test, world)
	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery, map[string]any{"keys": keys})
	if len(failures) > 0 || data["AddPasswordShareKeys"] != float64(itemCount) {
		test.Fatalf("adding the keys: %v %v", data, failures)
	}
	if after := sequenceNumberOf(test, world); after != before+itemCount {
		test.Fatalf("%d keys moved the revision sequence from %d to %d", itemCount, before, after)
	}
	// The same call again stores nothing and burns nothing.
	before = sequenceNumberOf(test, world)
	data, failures = world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery, map[string]any{"keys": keys})
	if len(failures) > 0 || data["AddPasswordShareKeys"] != float64(0) {
		test.Fatalf("adding them again: %v %v", data, failures)
	}
	if after := sequenceNumberOf(test, world); after != before {
		test.Fatalf("keys that were already there moved the sequence from %d to %d", before, after)
	}
}

// A call that names several items counts one write for each of them.
func TestAChangeToSeveralItemsCountsOneWriteForEach(test *testing.T) {
	previous := passwordWritesBurst
	passwordWritesBurst = 8
	test.Cleanup(func() { passwordWritesBurst = previous })

	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	ids := []string{}
	for range 4 {
		// A token's allowance is counted apart from the person's taps, and a
		// token cannot add a grantee: the saves are the token's, the shares a tap's.
		data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, savePasswordItemQuery, map[string]any{"input": ada.itemInput("login", nil)})
		if len(failures) > 0 {
			test.Fatalf("a save: %v", failures)
		}
		id := data["SavePasswordItem"].(map[string]any)["id"].(string)
		world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, nil))
		ids = append(ids, id)
	}
	// The four saves used half of the token's eight; four are left.
	keysFor := func(count int) []any {
		keys := []any{}
		for _, id := range ids[:count] {
			keys = append(keys, shareKeyInput(id, world.ben, ben))
		}
		return keys
	}
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery, map[string]any{"keys": keysFor(4)}); len(failures) > 0 {
		test.Fatalf("four items with four writes left: %v", failures)
	}
	// Four writes were taken at once: none are left for another call.
	_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery, map[string]any{"keys": keysFor(2)})
	if message := strings.Join(failures, "; "); !strings.Contains(message, "Too many changes to Passwords") {
		test.Fatalf("a call after the allowance was used by one that named four items: %q", message)
	}
}

// A refused SetPasswordItemShares (a key for somebody it does not reach, or
// for an old public key) draws no revision number.
func TestARefusedShareDrawsNoRevisionNumber(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben, cora := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben), newPasswordsDevice(test, world.cora)
	world.setUp(test, ada)
	world.setUp(test, ben)
	world.setUp(test, cora)
	id := world.save(test, ada, "login")["id"].(string)
	before := lastRevisionOf(test, world)
	grantees := []any{map[string]any{"granteeUserId": world.ben.ID}}
	staleKey := shareSetKeyInput(id, world.ben, ben)
	staleKey["recipientPublicKey"] = newPasswordsPublicKey(test)
	for name, keys := range map[string][]any{
		"a key for somebody the share does not reach": {shareSetKeyInput(id, world.cora, cora)},
		"a key for an old public key":                 {staleKey},
	} {
		world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, grantees, keys))
		if after := lastRevisionOf(test, world); after != before {
			test.Fatalf("%s drew revision numbers: %s then %s", name, before, after)
		}
	}
	if shares := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)["shares"].([]any); len(shares) != 0 {
		test.Fatalf("a refused share left shares behind: %v", shares)
	}
}

// Revisions are 64-bit: past the 32 bits a GraphQL Int holds, saves and
// listings still answer, and sinceRevision takes the same numbers.
func TestRevisionsPassTheThirtyTwoBitRange(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	world.save(test, ada, "login")
	dbtest.Exec(test, world.database, `SELECT setval('password_item_revision', 2147483646)`)
	var revision float64
	for step := 0; step < 3; step++ {
		saved := world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.itemInput("login", nil)})["SavePasswordItem"].(map[string]any)
		revision = saved["revision"].(float64)
	}
	if revision != 2147483649 {
		test.Fatalf("the third revision past the 32 bit range is %v", revision)
	}
	list := world.list(test, world.ada, nil)
	if list["revision"] != revision || len(list["items"].([]any)) != 4 {
		test.Fatalf("the listing: %v items, revision %v", len(list["items"].([]any)), list["revision"])
	}
	since := world.list(test, world.ada, 2147483647)
	if len(since["items"].([]any)) != 2 {
		test.Fatalf("what changed after revision 2147483647: %v", since["items"])
	}
	// A save based on that revision is accepted.
	last := since["items"].([]any)[1].(map[string]any)
	input := ada.namedItemInput(last["id"].(string), "login", last["revision"])
	if saved := world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": input})["SavePasswordItem"].(map[string]any); saved["revision"].(float64) != revision+1 {
		test.Fatalf("a save based on a 64-bit revision: %v", saved)
	}
}

// A document that lists Passwords items more than once is refused, and a
// second listing for a person while their first is running is turned away.
func TestPasswordsAreListedOnceAtATime(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	world.save(test, ada, "login")

	var aliases strings.Builder
	for index := 0; index < 3; index++ {
		fmt.Fprintf(&aliases, "a%d: ListPasswordItems { itemIds } ", index)
	}
	if message := world.refusal(test, world.ada, "query { "+aliases.String()+" }", nil); !strings.Contains(message, "only once") {
		test.Fatalf("aliased listings: %s", message)
	}
	// Through a fragment too.
	if message := world.refusal(test, world.ada, "query { ListPasswordItems { itemIds } ...Again } fragment Again on RootQuery { again: ListPasswordItems { itemIds } }", nil); !strings.Contains(message, "only once") {
		test.Fatalf("a listing in a fragment: %s", message)
	}
	// One listing and then the other, in turn, are fine.
	world.run(test, world.ada, "query { ListPasswordItems { itemIds } }", nil)
	world.run(test, world.ada, "query { ListWaitingPasswordShares { itemId } }", nil)

	release, err := world.graph.takePasswordsListing(&api.Principal{User: world.ada, CredentialKind: models.CredentialSession})
	if err != nil {
		test.Fatal(err)
	}
	if message := world.refusal(test, world.ada, listPasswordItemsQuery, nil); !strings.Contains(message, "Try again in a moment") || !strings.Contains(message, "too many") {
		test.Fatalf("a second listing: %s", message)
	}
	world.run(test, world.ben, listPasswordItemsQuery, nil)
	release()
	world.run(test, world.ada, listPasswordItemsQuery, nil)
	// A listing that finished gives its place back by itself.
	world.run(test, world.ada, listPasswordItemsQuery, nil)
}

// Turned-away unlock tries write one audit row for each person in five
// minutes, not one each.
func TestTurnedAwayUnlockTriesWriteOneAuditRowInFiveMinutes(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	wrongProof, _ := proofOf("a wrong secret")
	count := func() string {
		return dbtest.QueryString(test, world.database, `SELECT count(*)::text FROM audit_event WHERE resource_type = 'passwords' AND resource_id = '`+world.ada.ID+`' AND after->>'event' = 'unlock_rate_limited'`)
	}
	for range passwordUnlockFailuresBurst + 15 {
		world.callWithCredential(test, world.ada, models.CredentialToken, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
	}
	if got := count(); got != "1" {
		test.Fatalf("%s rows were written for the tries turned away, want 1", got)
	}
	// One request with many tries in it writes none more.
	var body strings.Builder
	for index := 0; index < 100; index++ {
		fmt.Fprintf(&body, "a%d: UnlockPasswords(proof: %q) { publicKey } ", index, wrongProof)
	}
	world.callWithCredential(test, world.ada, models.CredentialToken, "mutation { "+body.String()+" }", nil)
	if got := count(); got != "1" {
		test.Fatalf("%s rows after a request of a hundred tries, want 1", got)
	}
	// Five minutes on, the next one is written.
	dbtest.Exec(test, world.database, `UPDATE audit_event SET created_at = created_at - interval '6 minutes' WHERE resource_type = 'passwords' AND resource_id = '`+world.ada.ID+`'`)
	world.callWithCredential(test, world.ada, models.CredentialToken, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
	if got := count(); got != "2" {
		test.Fatalf("%s rows five minutes later, want 2", got)
	}
}

// A token saving over an edit the person made a few minutes ago does not lose
// the edit, whatever is kept from the hour before.
func TestATokenSaveRightAfterAnEditKeepsTheEdit(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	id := passwordItemIdOfNumber(world.ada, 1)
	put := func(credential models.CredentialKind, text string, base any) any {
		input := ada.namedItemInput(id, "login", base)
		input["encryptedFields"] = sealedOf(text)
		data, failures := world.callWithCredential(test, world.ada, credential, savePasswordItemQuery, map[string]any{"input": input})
		if len(failures) > 0 {
			test.Fatalf("%v", failures)
		}
		return data["SavePasswordItem"].(map[string]any)["revision"]
	}
	revision := put(models.CredentialSession, "first password", nil)
	revision = put(models.CredentialSession, "new password the person just chose", revision)
	dbtest.Exec(test, world.database, `UPDATE password_item_version SET replaced_at = replaced_at - interval '10 minutes' WHERE item_id = '`+id+`'`)
	revision = put(models.CredentialToken, "garbage", revision)
	put(models.CredentialToken, "more garbage", revision)
	held := func(text string) bool {
		return dbtest.QueryString(test, world.database, `SELECT count(*)::text FROM password_item_version WHERE item_id = '`+id+`' AND encrypted_fields = '`+sealedOf(text)+`'`) == "1"
	}
	if !held("new password the person just chose") || !held("first password") || held("garbage") {
		test.Fatalf("kept: first %v, edit %v, garbage %v", held("first password"), held("new password the person just chose"), held("garbage"))
	}
}

// A document that saves many items and then shares one takes the circle lock
// first, so a person leaving a circle meanwhile waits for it instead of
// deadlocking with it.
func TestADocumentThatSavesThenSharesDoesNotDeadlockWithLeavingACircle(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	existing := world.save(test, ada, "login")["id"].(string)
	// As many saves as a document may hold beside the share.
	const count = passwordsMutationsPerDocumentMost - 1
	var declarations, body strings.Builder
	variables := map[string]any{}
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("i%d", index)
		fmt.Fprintf(&declarations, "$%s: PasswordItemInput!, ", name)
		fmt.Fprintf(&body, "a%d: SavePasswordItem(input: $%s) { id } ", index, name)
		variables[name] = ada.namedItemInput(fmt.Sprintf("%08x-0000-4000-8000-%012d", 5, index), "login", nil)
	}
	variables["shares"] = sharesInput(existing, []any{map[string]any{"granteeUserId": world.ben.ID}}, nil)["input"]
	document := "mutation (" + declarations.String() + "$shares: PasswordItemSharesInput!) { " + body.String() + " z: SetPasswordItemShares(input: $shares) { id } }"

	done := make(chan []string, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		_, failures := world.callWithCredential(test, world.ben, models.CredentialSession, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home), nil)
		done <- failures
	}()
	_, failures := world.callWithCredential(test, world.ada, models.CredentialSession, document, variables)
	if len(failures) > 0 {
		test.Fatalf("ada's document: %v", failures)
	}
	select {
	case benFailures := <-done:
		if len(benFailures) > 0 {
			test.Fatalf("ben leaving the circle: %v", benFailures)
		}
	case <-time.After(30 * time.Second):
		test.Fatal("ben leaving the circle did not finish")
	}
}
