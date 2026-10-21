package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const (
	listPasswordItemVersionsQuery   = `query ($itemId: String!) { ListPasswordItemVersions(itemId: $itemId) { versionId savedAt isSavedByPerson encryptedFields itemKey itemKind } }`
	restorePasswordItemVersionQuery = `mutation ($itemId: String!, $versionId: String!, $baseRevision: Int64!) { RestorePasswordItemVersion(itemId: $itemId, versionId: $versionId, baseRevision: $baseRevision) { ` + passwordItemFields + ` } }`
)

// An item key sealed for a key pair is stored as every client writes it and in
// no other form: no white space, the fields in their order, the size of a
// sealed 32 byte key.
func TestOnlyTheCanonicalTextOfASealedKeyIsAccepted(test *testing.T) {
	canonical := sealedForKeyPairOf("item key for ben")
	if len(canonical) > passwordsSealedKeyMostBytes || !isSealedKeyForKeyPairText(canonical) {
		test.Fatalf("the text every client writes (%d bytes) is not accepted", len(canonical))
	}
	var pieces struct{ EphemeralPublicKey, Nonce, Ciphertext string }
	if err := json.Unmarshal([]byte(canonical), &pieces); err != nil {
		test.Fatal(err)
	}
	for name, text := range map[string]string{
		"white space after the brace":     "{" + strings.Repeat(" ", 7500) + canonical[1:],
		"white space at the end":          canonical + strings.Repeat(" ", 300),
		"a newline between fields":        strings.Replace(canonical, `,"nonce"`, ",\n\"nonce\"", 1),
		"a space after a colon":           strings.Replace(canonical, `"version":1`, `"version": 1`, 1),
		"the nonce before the public key": fmt.Sprintf(`{"version":1,"nonce":%q,"ephemeralPublicKey":%q,"ciphertext":%q}`, pieces.Nonce, pieces.EphemeralPublicKey, pieces.Ciphertext),
		"a field in another case":         strings.Replace(canonical, `"version"`, `"Version"`, 1),
		"a ciphertext of another size":    sealedForKeyPairOfSize("item key for ben", 64),
		"more than the most bytes":        canonical[:len(canonical)-2] + strings.Repeat("0", 400) + `"}`,
		"an escaped character":            strings.Replace(canonical, `"nonce"`, "\"no\\u006ece\"", 1),
	} {
		if isSealedKeyForKeyPairText(text) {
			test.Errorf("%s was accepted", name)
		}
	}
}

// A token stores the keys of a share. The owner's tap then cannot replace one
// with the same key padded with white space, which would be longer than the
// row it replaces and take the recipient past the bounds on what is shared with
// them without being counted. The agent's copy and a re-key are the same.
func TestAPaddedShareKeyIsRefusedEverywhereItCouldBeSent(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	itemId := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(itemId, []any{map[string]any{"granteeUserId": world.ben.ID}}, nil))

	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery,
		map[string]any{"keys": []any{shareKeyInput(itemId, world.ben, ben)}})
	if len(failures) > 0 || data["AddPasswordShareKeys"] != float64(1) {
		test.Fatalf("a token stores the key: %v %v", data, failures)
	}
	storedBytes := func() string {
		return dbtest.QueryString(test, world.database, `SELECT octet_length(item_key_by_recipient_key)::text FROM password_share_key WHERE item_id = '`+itemId+`'`)
	}
	before := storedBytes()

	padded := func(key map[string]any) map[string]any {
		copied := map[string]any{}
		for name, value := range key {
			copied[name] = value
		}
		copied["itemKeyByRecipientKey"] = "{" + strings.Repeat(" ", 7500) + key["itemKeyByRecipientKey"].(string)[1:]
		return copied
	}
	const want = "itemKeyByRecipientKey must be"
	if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{padded(shareKeyInput(itemId, world.ben, ben))}}); !strings.Contains(message, want) {
		test.Fatalf("AddPasswordShareKeys with a padded key: %s", message)
	}
	if message := world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(itemId,
		[]any{map[string]any{"granteeUserId": world.ben.ID}}, []any{padded(shareSetKeyInput(itemId, world.ben, ben))})); !strings.Contains(message, want) {
		test.Fatalf("SetPasswordItemShares with a padded key: %s", message)
	}
	current := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)
	paddedRekeyKey := padded(shareKeyInput(itemId, world.ben, ben))
	delete(paddedRekeyKey, "itemKey")
	if message := world.refusal(test, world.ada, rekeyPasswordItemQuery, rekeyInput(itemId, current["revision"], []any{paddedRekeyKey}, nil)); !strings.Contains(message, want) {
		test.Fatalf("RekeyPasswordItem with a padded key: %s", message)
	}
	paddedAgentKey := "{" + strings.Repeat(" ", 7500) + sealedForKeyPairOf("item key for the agent")[1:]
	if message := world.refusal(test, world.ada, passwordAgentUseQuery,
		map[string]any{"input": map[string]any{"itemId": itemId, "isUsableByAgent": true, "itemKeyByAgentKey": paddedAgentKey}}); !strings.Contains(message, "itemKeyByAgentKey must be") {
		test.Fatalf("a padded agent copy: %s", message)
	}
	if after := storedBytes(); after != before {
		test.Fatalf("the stored key is %s bytes after the refusals, was %s", after, before)
	}
}

// One document names at most twenty-five Passwords mutations, however they are
// spelled: a request holds the global locks of Passwords until it commits, so
// a document of many would hold them for minutes. Reading is not counted.
func TestADocumentNamesAtMostTwentyFivePasswordsMutations(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	var itemIds []string
	for index := 0; index < passwordsMutationsPerDocumentMost+1; index++ {
		itemIds = append(itemIds, world.save(test, ada, "note")["id"].(string))
	}
	deletes := func(count int) string {
		var selections []string
		for index := 0; index < count; index++ {
			selections = append(selections, fmt.Sprintf(`a%d: DeletePasswordItem(id: %q) { id }`, index, itemIds[index]))
		}
		return "mutation { " + strings.Join(selections, " ") + " }"
	}
	const wantRefusal = "may change Passwords at most 25 times"

	// The document the tenth audit sent: 1,800 aliased mutations, each of which
	// would have taken the locks and summed the person's rows. It is refused
	// before any runs.
	started := time.Now()
	var aliased []string
	for index := 0; index < 1800; index++ {
		aliased = append(aliased, fmt.Sprintf(`a%d: DeletePasswordItem(id: "00000008-0000-4000-8000-%012d") { id }`, index, index))
	}
	_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, "mutation { "+strings.Join(aliased, " ")+" }", nil)
	if !strings.Contains(strings.Join(failures, "; "), wantRefusal) || time.Since(started) > 2*time.Second {
		test.Fatalf("1,800 deletes in one document: %.200v after %s", failures, time.Since(started))
	}

	_, failures = world.callWithCredential(test, world.ada, models.CredentialToken, deletes(passwordsMutationsPerDocumentMost+1), nil)
	if !strings.Contains(strings.Join(failures, "; "), wantRefusal) {
		test.Fatalf("26 deletes in one document: %v", failures)
	}
	if got := dbtest.QueryString(test, world.database, `SELECT count(*) FROM password_item WHERE deleted_at IS NOT NULL`); got != "0" {
		test.Fatalf("%s items deleted by a refused document", got)
	}
	// Other Passwords mutations count with them, through a fragment as well.
	mixed := deletes(passwordsMutationsPerDocumentMost) + ` `
	mixed = strings.Replace(mixed, "mutation {", `mutation { ...Settings`, 1) + ` fragment Settings on RootMutation { settings: UpdatePasswordsSettings(lockAfterMinutes: 30) { lockAfterMinutes } }`
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, mixed, nil); !strings.Contains(strings.Join(failures, "; "), wantRefusal) {
		test.Fatalf("25 deletes and a setting through a fragment: %v", failures)
	}
	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, deletes(passwordsMutationsPerDocumentMost), nil)
	if len(failures) > 0 || len(data) != passwordsMutationsPerDocumentMost {
		test.Fatalf("25 deletes in one document: %d answers, %v", len(data), failures)
	}
	// Reads are not Passwords mutations.
	var reads []string
	for index := 0; index < 60; index++ {
		reads = append(reads, fmt.Sprintf(`r%d: ReadPasswordsSetup { isSetUp }`, index))
	}
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, "query { "+strings.Join(reads, " ")+" }", nil); len(failures) > 0 {
		test.Fatalf("60 reads: %v", failures)
	}
}

// Every mutation the interface declares is counted, with no list to keep in
// step.
func TestEveryPasswordsMutationIsCountedInADocument(test *testing.T) {
	mutations := reflect.TypeOf((*PasswordsMutation)(nil)).Elem()
	if len(passwordsMutationNames) != mutations.NumMethod() || mutations.NumMethod() < 15 {
		test.Fatalf("%d names for %d mutations", len(passwordsMutationNames), mutations.NumMethod())
	}
	for _, name := range []string{"SavePasswordItem", "DeletePasswordItem", "RestorePasswordItemVersion", "AddPasswordShareKeys", "RekeyPasswordItem", "UnlockPasswords"} {
		if !passwordsMutationNames[name] {
			test.Errorf("%s is not counted", name)
		}
	}
	if passwordsMutationNames["ListPasswordItems"] || passwordsMutationNames["ListPasswordItemVersions"] {
		test.Error("a query is counted as a mutation")
	}
}

// A change that could not get the locks in time is told to try again, as a
// limit, and not as a failure of the server.
func TestAChangeThatTimedOutOnTheLocksIsToldToTryAgain(test *testing.T) {
	for name, cause := range map[string]error{
		"the locks of a change": db.ErrPasswordsBusy,
		"a row lock after them": fmt.Errorf("lock a row: %w", &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}),
	} {
		failure := passwordsFailure(cause)
		if !errors.Is(failure, api.ErrTooManyRequests) || !strings.Contains(failure.Error(), "Passwords is busy") {
			test.Errorf("%s: %v", name, failure)
		}
	}
}

// The earlier versions of an item are the person's to read and to put back, by
// their own tap and on the revision they read: a token does neither. Putting
// one back makes it the item, ends what other people and the agent had of the
// old key, and marks the item for a new key.
func TestListingAndRestoringAnItemsEarlierVersions(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	first := world.save(test, ada, "login")
	itemId := first["id"].(string)
	// A second save by the person's tap keeps the first content as a version.
	secondInput := ada.namedItemInput(itemId, "login", first["revision"])
	secondInput["encryptedFields"] = sealedOf("the second fields")
	secondInput["itemKey"] = sealedOf("the second item key")
	second := world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": secondInput})["SavePasswordItem"].(map[string]any)
	// The item is shared with ben, who holds a key for it.
	sharedKey := shareSetKeyInput(itemId, world.ben, ben)
	sharedKey["itemKey"] = sealedOf("the second item key")
	shared := world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(itemId, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{sharedKey}))["SetPasswordItemShares"].(map[string]any)
	if got := len(world.list(test, world.ben, nil)["itemIds"].([]any)); got != 1 {
		test.Fatalf("ben lists %d items before the restore, want the shared one", got)
	}

	versions := world.run(test, world.ada, listPasswordItemVersionsQuery, map[string]any{"itemId": itemId})["ListPasswordItemVersions"].([]any)
	if len(versions) != 1 {
		test.Fatalf("%d versions", len(versions))
	}
	version := versions[0].(map[string]any)
	if version["versionId"] != fmt.Sprint(int64(first["revision"].(float64))) || version["isSavedByPerson"] != true || version["itemKind"] != "login" ||
		version["encryptedFields"] != first["encryptedFields"] || version["itemKey"] != first["itemKey"] || version["savedAt"] == nil {
		test.Fatalf("the version: %v", version)
	}
	// A token reads and restores nothing.
	_, failures := world.callWithCredential(test, world.ada, models.CredentialToken, listPasswordItemVersionsQuery, map[string]any{"itemId": itemId})
	if !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token lists versions: %v", failures)
	}
	restoreVariables := func(versionId string, baseRevision any) map[string]any {
		return map[string]any{"itemId": itemId, "versionId": versionId, "baseRevision": baseRevision}
	}
	_, failures = world.callWithCredential(test, world.ada, models.CredentialToken, restorePasswordItemVersionQuery, restoreVariables(version["versionId"].(string), shared["revision"]))
	if !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token restores a version: %v", failures)
	}
	// An operator signed in as the person is no tap either.
	operatorAsPerson := &api.Principal{
		User: world.ada, Impersonator: world.ben, CredentialKind: models.CredentialSession,
		Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionPasswordsUse}}),
	}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), operatorAsPerson), tx)
		if _, err := world.graph.ListPasswordItemVersions(ctx, ListPasswordItemVersionsArguments{ItemID: itemId}); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
			test.Errorf("an operator lists versions: %v", err)
		}
		revision := int64(shared["revision"].(float64))
		if _, err := world.graph.RestorePasswordItemVersion(ctx, RestorePasswordItemVersionArguments{ItemID: itemId, VersionID: version["versionId"].(string), BaseRevision: revision}); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
			test.Errorf("an operator restores a version: %v", err)
		}
	})

	for name, attempt := range map[string]struct {
		person    *models.User
		variables map[string]any
		want      string
	}{
		"an old revision":         {world.ada, restoreVariables(version["versionId"].(string), first["revision"]), "changed on another device"},
		"a version that is gone":  {world.ada, restoreVariables("987654", shared["revision"]), "not found"},
		"a version that is no id": {world.ada, restoreVariables("the first one", shared["revision"]), "versionId"},
		"somebody else's item":    {world.ben, restoreVariables(version["versionId"].(string), shared["revision"]), "not found"},
		"a revision of nothing":   {world.ada, restoreVariables(version["versionId"].(string), 0), "baseRevision"},
		"an item that is no uuid": {world.ada, map[string]any{"itemId": "x", "versionId": "1", "baseRevision": 1}, "itemId"},
	} {
		if message := world.refusal(test, attempt.person, restorePasswordItemVersionQuery, attempt.variables); !strings.Contains(message, attempt.want) {
			test.Errorf("%s: %s", name, message)
		}
	}
	if message := world.refusal(test, world.ben, listPasswordItemVersionsQuery, map[string]any{"itemId": itemId}); !strings.Contains(message, "not found") {
		test.Errorf("ben lists the versions of ada's item: %s", message)
	}

	restored := world.run(test, world.ada, restorePasswordItemVersionQuery, restoreVariables(version["versionId"].(string), shared["revision"]))["RestorePasswordItemVersion"].(map[string]any)
	if restored["encryptedFields"] != first["encryptedFields"] || restored["itemKey"] != first["itemKey"] || restored["needsNewKey"] != true || restored["isUsableByAgent"] != false {
		test.Fatalf("the restored item: %v", restored)
	}
	if restored["revision"].(float64) <= shared["revision"].(float64) || second["revision"].(float64) >= restored["revision"].(float64) {
		test.Fatalf("the revision did not move: %v then %v", shared["revision"], restored["revision"])
	}
	for _, share := range restored["shares"].([]any) {
		for _, recipient := range share.(map[string]any)["recipients"].([]any) {
			if recipient.(map[string]any)["hasKey"] == true {
				test.Fatal("ben still has a key")
			}
		}
	}
	if got := len(world.list(test, world.ben, nil)["itemIds"].([]any)); got != 0 {
		test.Fatalf("ben still lists %d items after the restore", got)
	}
	// What the restore replaced is a version now.
	after := world.run(test, world.ada, listPasswordItemVersionsQuery, map[string]any{"itemId": itemId})["ListPasswordItemVersions"].([]any)
	if len(after) != 2 || after[0].(map[string]any)["encryptedFields"] != sealedOf("the second fields") {
		test.Fatalf("the versions after the restore: %v", after)
	}
	// The device then gives it a new key from the revision it was given.
	rekeyed := world.run(test, world.ada, rekeyPasswordItemQuery, rekeyInput(itemId, restored["revision"], nil, nil))["RekeyPasswordItem"].(map[string]any)
	if rekeyed["needsNewKey"] != false {
		test.Fatalf("the re-key after a restore: %v", rekeyed)
	}
}
