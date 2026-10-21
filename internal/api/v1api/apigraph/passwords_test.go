package apigraph

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// passwordsWorld is a server with ada and ben in one circle, cora in none of
// theirs, and dave, who may not use Passwords. Ada, ben and cora may.
type passwordsWorld struct {
	*healthWorld
}

func newPasswordsWorld(test *testing.T) *passwordsWorld {
	test.Helper()
	world := &passwordsWorld{healthWorld: &healthWorld{calendarShareWorld: newCalendarShareWorld(test)}}
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		for _, user := range []*models.User{world.ada, world.ben, world.cora} {
			dbtest.GrantPermission(test, tx, user.ID, models.PermissionPasswordsUse)
		}
	})
	return world
}

// passwordsDevice stands for a person's device: it makes the values the server
// stores and never opens, with a real P-256 public key, because the server
// checks that one is a point on the curve.
type passwordsDevice struct {
	person      *models.User
	publicKey   string
	passphrase  string
	recoveryKey string
	itemNumber  int
}

func newPasswordsDevice(test *testing.T, person *models.User) *passwordsDevice {
	test.Helper()
	return &passwordsDevice{
		person: person, publicKey: newPasswordsPublicKey(test),
		passphrase: "passphrase of " + person.Username, recoveryKey: "recovery key of " + person.Username,
	}
}

func newPasswordsPublicKey(test *testing.T) string {
	test.Helper()
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		test.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes())
}

// proofOf is the proof a device sends for a secret and the hash it stores.
func proofOf(secret string) (string, string) {
	digest := sha256.Sum256([]byte(secret))
	return base64.StdEncoding.EncodeToString([]byte(secret)), base64.StdEncoding.EncodeToString(digest[:])
}

// sealedOf is a sealed value in the shape the server checks: version 1, a 12
// byte nonce and a ciphertext of at least the 16 byte tag, in canonical base64.
// The server never opens it, so the bytes only need to be those of the text.
func sealedOf(text string) string {
	return `{"version":1,"nonce":"` + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 12)) + `","ciphertext":"` +
		base64.StdEncoding.EncodeToString(append([]byte(text), make([]byte, 16)...)) + `"}`
}

// sealedForKeyPairOf is a value sealed for a key pair, in the shape the server
// checks: the same, with an ephemeral public key of 65 bytes starting with 04.
func sealedForKeyPairOf(text string) string {
	ephemeralPublicKey := append([]byte{4}, bytes.Repeat([]byte{9}, 64)...)
	return `{"version":1,"ephemeralPublicKey":"` + base64.StdEncoding.EncodeToString(ephemeralPublicKey) + `","nonce":"` + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 12)) +
		`","ciphertext":"` + base64.StdEncoding.EncodeToString(append([]byte(text), make([]byte, 16)...)) + `"}`
}

const (
	passwordsSetupFields    = `isSetUp passphraseSalt passphraseIterationCount publicKey lockAfterMinutes itemCount`
	setUpPasswordsQuery     = `mutation ($input: PasswordsSetupInput!) { SetUpPasswords(input: $input) { ` + passwordsSetupFields + ` } }`
	readPasswordsSetupQuery = `query { ReadPasswordsSetup { ` + passwordsSetupFields + ` } }`
	unlockPasswordsQuery    = `mutation ($proof: String!, $isRecoveryKey: Boolean) { UnlockPasswords(proof: $proof, isRecoveryKey: $isRecoveryKey) { accountKeyWrapped privateKeyByAccountKey publicKey } }`
	erasePasswordsQuery     = `mutation { ErasePasswords }`
	passwordItemFields      = `id ownerUserId ownerName ownerPublicKey itemKind encryptedFields itemKey isSharedWithMe revision isUsableByAgent lastUsedByAgentAt needsNewKey deletedAt createdAt updatedAt
  shares { id granteeUserId granteeCircleId granteeName recipients { userId name username publicKey hasKey } }`
	savePasswordItemQuery      = `mutation ($input: PasswordItemInput!) { SavePasswordItem(input: $input) { ` + passwordItemFields + ` } }`
	listPasswordItemsQuery     = `query ($sinceRevision: Int64) { ListPasswordItems(sinceRevision: $sinceRevision) { items { ` + passwordItemFields + ` } itemIds revision } }`
	deletePasswordItemQuery    = `mutation ($id: String!) { DeletePasswordItem(id: $id) { ` + passwordItemFields + ` } }`
	restorePasswordItemQuery   = `mutation ($id: String!) { RestorePasswordItem(id: $id) { ` + passwordItemFields + ` } }`
	purgePasswordItemQuery     = `mutation ($id: String!) { PurgePasswordItem(id: $id) }`
	passwordRecipientsQuery    = `query ($granteeUserId: String, $granteeCircleId: String) { ListPasswordRecipients(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) { userId name username publicKey hasKey } }`
	waitingPasswordSharesQuery = `query { ListWaitingPasswordShares { itemId recipient { userId publicKey hasKey } } }`
	setPasswordItemSharesQuery = `mutation ($input: PasswordItemSharesInput!) { SetPasswordItemShares(input: $input) { ` + passwordItemFields + ` } }`
	addPasswordShareKeysQuery  = `mutation ($keys: [PasswordItemShareKeyInput!]!) { AddPasswordShareKeys(keys: $keys) }`
	passwordAgentUseQuery      = `mutation ($input: PasswordItemAgentUseInput!) { SetPasswordItemUsableByAgent(input: $input) { ` + passwordItemFields + ` } }`
	passwordsAgentKeyQuery     = `query { ReadPasswordsAgentKey }`
)

func (self *passwordsDevice) setupInput() map[string]any {
	_, passphraseHash := proofOf(self.passphrase)
	_, recoveryHash := proofOf(self.recoveryKey)
	return map[string]any{
		"accountKeyByPassphrase": sealedOf("account key by passphrase"), "passphraseSalt": base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")),
		"passphraseIterationCount": 600000, "passphraseProofHash": passphraseHash,
		"accountKeyByRecoveryKey": sealedOf("account key by recovery key"), "recoveryKeyProofHash": recoveryHash,
		"publicKey": self.publicKey, "privateKeyByAccountKey": sealedOf("private key"),
	}
}

func (self *passwordsWorld) setUp(test *testing.T, device *passwordsDevice) {
	test.Helper()
	self.run(test, device.person, setUpPasswordsQuery, map[string]any{"input": device.setupInput()})
}

// itemInput is the item a device saves: a new one with its own number.
func (self *passwordsDevice) itemInput(kind string, baseRevision any) map[string]any {
	self.itemNumber++
	return self.namedItemInput(passwordItemIdOfNumber(self.person, self.itemNumber), kind, baseRevision)
}

// passwordItemIdOfNumber is a lower case UUID that differs for each person and
// each number.
func passwordItemIdOfNumber(person *models.User, number int) string {
	digest := sha256.Sum256([]byte(person.Username))
	return fmt.Sprintf("%x-0000-4000-8000-%012d", digest[:4], number)
}

func (self *passwordsDevice) namedItemInput(id, kind string, baseRevision any) map[string]any {
	return map[string]any{
		"id": id, "itemKind": kind, "encryptedFields": sealedOf("fields of " + id), "itemKey": sealedOf("item key of " + id),
		"baseRevision": baseRevision,
	}
}

func (self *passwordsWorld) save(test *testing.T, device *passwordsDevice, kind string) map[string]any {
	test.Helper()
	return self.run(test, device.person, savePasswordItemQuery, map[string]any{"input": device.itemInput(kind, nil)})["SavePasswordItem"].(map[string]any)
}

func (self *passwordsWorld) list(test *testing.T, person *models.User, sinceRevision any) map[string]any {
	test.Helper()
	return self.run(test, person, listPasswordItemsQuery, map[string]any{"sinceRevision": sinceRevision})["ListPasswordItems"].(map[string]any)
}

func shareKeyInput(itemId string, recipient *models.User, device *passwordsDevice) map[string]any {
	return map[string]any{
		"itemId": itemId, "recipientUserId": recipient.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("item key for " + recipient.Username),
		"recipientPublicKey": device.publicKey,
		// The item key the device opened: the one passwordsDevice saves items with.
		"itemKey": sealedOf("item key of " + itemId),
	}
}

// shareSetKeyInput is shareKeyInput as SetPasswordItemShares takes it, which
// names the item once for all its keys (and the item key each was sealed from,
// as AddPasswordShareKeys does).
func shareSetKeyInput(itemId string, recipient *models.User, device *passwordsDevice) map[string]any {
	key := shareKeyInput(itemId, recipient, device)
	delete(key, "itemId")
	return key
}

func sharesInput(itemId string, grantees []any, keys []any) map[string]any {
	if grantees == nil {
		grantees = []any{}
	}
	if keys == nil {
		keys = []any{}
	}
	return map[string]any{"input": map[string]any{"itemId": itemId, "grantees": grantees, "keys": keys}}
}

func isTapRefusal(message string) bool { return strings.Contains(message, api.TapRequiredPhrase) }

// Passwords is set up once, by a person's tap, with a real public key and
// sealed values; the setup is read back with what a device needs to unlock
// and nothing that opens it.
func TestPasswordsAreSetUpOnceByATap(test *testing.T) {
	world := newPasswordsWorld(test)
	device := newPasswordsDevice(test, world.ada)

	if setup := world.run(test, world.ada, readPasswordsSetupQuery, nil)["ReadPasswordsSetup"].(map[string]any); setup["isSetUp"] != false ||
		setup["passphraseSalt"] != nil || setup["publicKey"] != nil || setup["lockAfterMinutes"] != float64(15) || setup["itemCount"] != float64(0) {
		test.Fatalf("before setting up: %v", setup)
	}
	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		_, failures := world.callWithCredential(test, world.ada, credentialKind, setUpPasswordsQuery, map[string]any{"input": device.setupInput()})
		if !isTapRefusal(strings.Join(failures, "; ")) {
			test.Fatalf("setting up with credential %q: %v", credentialKind, failures)
		}
	}
	for name, change := range map[string]func(map[string]any){
		"a public key that is not a point": func(input map[string]any) { input["publicKey"] = base64.StdEncoding.EncodeToString(make([]byte, 65)) },
		"a proof hash that is not a hash":  func(input map[string]any) { input["passphraseProofHash"] = "AAAA" },
		"a sealed value that is not JSON":  func(input map[string]any) { input["accountKeyByPassphrase"] = "not json" },
		"too few iterations":               func(input map[string]any) { input["passphraseIterationCount"] = 1 },
	} {
		input := device.setupInput()
		change(input)
		if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("%s: %s", name, message)
		}
	}
	if setup := world.run(test, world.ada, readPasswordsSetupQuery, nil)["ReadPasswordsSetup"].(map[string]any); setup["isSetUp"] != false {
		test.Fatalf("a refused setup set something up: %v", setup)
	}

	setup := world.run(test, world.ada, setUpPasswordsQuery, map[string]any{"input": device.setupInput()})["SetUpPasswords"].(map[string]any)
	if setup["isSetUp"] != true || setup["publicKey"] != device.publicKey || setup["passphraseIterationCount"] != float64(600000) {
		test.Fatalf("the setup: %v", setup)
	}
	if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": device.setupInput()}); !strings.Contains(message, "already set up") {
		test.Fatalf("setting up twice: %s", message)
	}

	// Somebody who may not use Passwords is told there is nothing.
	for name, query := range map[string]string{"setup": readPasswordsSetupQuery, "items": listPasswordItemsQuery, "waiting": waitingPasswordSharesQuery, "agent key": passwordsAgentKeyQuery} {
		if message := world.refusal(test, world.dave, query, nil); !strings.Contains(message, "not found") {
			test.Errorf("%s for a person without passwords:use: %s", name, message)
		}
	}
}

// Unlocking answers the sealed keys for the right proof of the passphrase or
// of the recovery key, says which was wrong, writes every wrong one to the
// audit log without the proof, and stops one person's tries without stopping
// another's.
func TestUnlockingPasswordsChecksTheProofAndLimitsTries(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)

	passphraseProof, _ := proofOf(ada.passphrase)
	recoveryProof, _ := proofOf(ada.recoveryKey)
	wrongProof, _ := proofOf("a wrong secret")
	unlock := world.run(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": passphraseProof})["UnlockPasswords"].(map[string]any)
	if unlock["accountKeyWrapped"] != sealedOf("account key by passphrase") || unlock["privateKeyByAccountKey"] != sealedOf("private key") || unlock["publicKey"] != ada.publicKey {
		test.Fatalf("unlocking with the passphrase: %v", unlock)
	}
	recovered := world.run(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": recoveryProof, "isRecoveryKey": true})["UnlockPasswords"].(map[string]any)
	if recovered["accountKeyWrapped"] != sealedOf("account key by recovery key") {
		test.Fatalf("unlocking with the recovery key: %v", recovered)
	}
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": wrongProof}); !strings.Contains(message, "That passphrase is not right.") {
		test.Fatalf("a wrong passphrase: %s", message)
	}
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": wrongProof, "isRecoveryKey": true}); !strings.Contains(message, "That recovery key is not right.") {
		test.Fatalf("a wrong recovery key: %s", message)
	}
	// Each secret opens only its own.
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": recoveryProof}); !strings.Contains(message, "That passphrase is not right.") {
		test.Fatalf("the recovery key as a passphrase: %s", message)
	}

	// The failures are in the audit log, and the proof is not.
	var failures []*models.AuditEvent
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourcePasswords), ResourceID: world.ada.ID, Limit: 100})
		if err != nil {
			test.Fatal(err)
		}
		failures = events
	})
	failureCount, unlockedCount := 0, 0
	for _, event := range failures {
		text := string(event.Before) + string(event.After)
		if strings.Contains(text, "unlock_failed") {
			failureCount++
		}
		if strings.Contains(text, `"unlocked"`) {
			unlockedCount++
		}
		for _, secret := range []string{wrongProof, passphraseProof, recoveryProof, sealedOf("private key")} {
			if strings.Contains(text, secret) {
				test.Errorf("an audit event holds a secret: %s", text)
			}
		}
	}
	if failureCount != 3 {
		test.Fatalf("%d unlock failures were audited, want 3: %v", failureCount, failures)
	}
	if unlockedCount != 2 {
		test.Fatalf("%d unlocks were audited, want 2 (the passphrase and the recovery key): %v", unlockedCount, failures)
	}

	// Seven more wrong ones empty the ten allowed; then even the right proof
	// waits, for Ada alone.
	for range 7 {
		world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
	}
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": passphraseProof}); !strings.Contains(message, "too many attempts") {
		test.Fatalf("the right proof after too many wrong ones: %s", message)
	}
	// The try that was turned away is in the audit log, with no proof in it.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourcePasswords), ResourceID: world.ada.ID, Limit: 100})
		if err != nil {
			test.Fatal(err)
		}
		refusedCount := 0
		for _, event := range events {
			text := string(event.Before) + string(event.After)
			if strings.Contains(text, "unlock_rate_limited") {
				refusedCount++
			}
			for _, secret := range []string{wrongProof, passphraseProof, recoveryProof} {
				if strings.Contains(text, secret) {
					test.Errorf("an audit event holds a secret: %s", text)
				}
			}
		}
		if refusedCount != 1 {
			test.Fatalf("%d refused tries were audited, want 1", refusedCount)
		}
	})
	benProof, _ := proofOf(ben.passphrase)
	world.run(test, world.ben, unlockPasswordsQuery, map[string]any{"proof": benProof})
}

// A proof is needed to change the passphrase or the recovery key, and both
// need a tap; the old passphrase stops working and the recovery key goes on
// working until it is replaced.
func TestChangingTheSecretsNeedsAProofAndATap(test *testing.T) {
	world := newPasswordsWorld(test)
	device := newPasswordsDevice(test, world.ada)
	world.setUp(test, device)
	oldProof, _ := proofOf(device.passphrase)
	recoveryProof, _ := proofOf(device.recoveryKey)
	newProof, newHash := proofOf("a new passphrase")
	change := func(proof string, isRecoveryKey bool) map[string]any {
		return map[string]any{"input": map[string]any{
			"proof": proof, "isRecoveryKey": isRecoveryKey, "accountKeyByPassphrase": sealedOf("new account key by passphrase"),
			"passphraseSalt": base64.StdEncoding.EncodeToString([]byte("fedcba9876543210")), "passphraseIterationCount": 700000, "passphraseProofHash": newHash,
		}}
	}
	const changeQuery = `mutation ($input: PasswordsPassphraseChangeInput!) { ChangePasswordsPassphrase(input: $input) { passphraseIterationCount } }`
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, changeQuery, change(oldProof, false)); !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token changing the passphrase: %v", failures)
	}
	if message := world.refusal(test, world.ada, changeQuery, change(newProof, false)); !strings.Contains(message, "That passphrase is not right.") {
		test.Fatalf("a wrong old passphrase: %s", message)
	}
	changed := world.run(test, world.ada, changeQuery, change(recoveryProof, true))["ChangePasswordsPassphrase"].(map[string]any)
	if changed["passphraseIterationCount"] != float64(700000) {
		test.Fatalf("changed with the recovery key: %v", changed)
	}
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": oldProof}); !strings.Contains(message, "not right") {
		test.Fatalf("the old passphrase: %s", message)
	}
	world.run(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": newProof})

	const replaceQuery = `mutation ($input: PasswordsRecoveryKeyReplacementInput!) { ReplacePasswordsRecoveryKey(input: $input) { isSetUp } }`
	_, replacedHash := proofOf("a new recovery key")
	replace := func(proof string) map[string]any {
		return map[string]any{"input": map[string]any{"proof": proof, "accountKeyByRecoveryKey": sealedOf("new account key by recovery key"), "recoveryKeyProofHash": replacedHash}}
	}
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, replaceQuery, replace(newProof)); !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token replacing the recovery key: %v", failures)
	}
	if message := world.refusal(test, world.ada, replaceQuery, replace(oldProof)); !strings.Contains(message, "That passphrase is not right.") {
		test.Fatalf("replacing with a wrong passphrase: %s", message)
	}
	world.run(test, world.ada, replaceQuery, replace(newProof))
	replacementProof, _ := proofOf("a new recovery key")
	world.run(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": replacementProof, "isRecoveryKey": true})
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": recoveryProof, "isRecoveryKey": true}); !strings.Contains(message, "not right") {
		test.Fatalf("the old recovery key: %s", message)
	}

	const settingsQuery = `mutation ($lockAfterMinutes: Int!) { UpdatePasswordsSettings(lockAfterMinutes: $lockAfterMinutes) { lockAfterMinutes } }`
	for _, minutes := range []int{0, 10081} {
		if message := world.refusal(test, world.ada, settingsQuery, map[string]any{"lockAfterMinutes": minutes}); !strings.Contains(message, "lockAfterMinutes") {
			test.Errorf("locking after %d minutes: %s", minutes, message)
		}
	}
	if updated := world.run(test, world.ada, settingsQuery, map[string]any{"lockAfterMinutes": 60})["UpdatePasswordsSettings"].(map[string]any); updated["lockAfterMinutes"] != float64(60) {
		test.Fatalf("locking after an hour: %v", updated)
	}
}

// An item is saved by its owner on the revision the device read: a stale one
// is refused in the words the device shows, another person's item is not
// found, the kind stays, and a delete and a restore raise the revision.
func TestSavingPasswordItemsChecksRevisionAndOwner(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)

	login := world.save(test, ada, "login")
	if login["revision"].(float64) < 1 || login["itemKind"] != "login" || login["isSharedWithMe"] != false || login["ownerUserId"] != world.ada.ID ||
		login["itemKey"] != sealedOf("item key of "+login["id"].(string)) || len(login["shares"].([]any)) != 0 {
		test.Fatalf("a new item: %v", login)
	}
	revision := login["revision"]
	id := login["id"].(string)
	edited := ada.namedItemInput(id, "login", revision)
	edited["encryptedFields"] = sealedOf("changed fields")
	saved := world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": edited})["SavePasswordItem"].(map[string]any)
	if saved["revision"].(float64) <= revision.(float64) || saved["encryptedFields"] != sealedOf("changed fields") {
		test.Fatalf("a save on the current revision: %v", saved)
	}
	if message := world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": edited}); !strings.Contains(message, "This item changed on another device. Open it again.") {
		test.Fatalf("a stale base revision: %s", message)
	}
	if message := world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.namedItemInput(id, "login", nil)}); !strings.Contains(message, "changed on another device") {
		test.Fatalf("a new item with an id in use: %s", message)
	}
	if message := world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": ada.namedItemInput(id, "note", saved["revision"])}); !strings.Contains(message, "kind") {
		test.Fatalf("a change of kind: %s", message)
	}

	// Owner only: ben cannot save over it, with or without a base revision,
	// and cannot delete, restore or share it.
	for name, input := range map[string]map[string]any{"new": ben.namedItemInput(id, "login", nil), "based": ben.namedItemInput(id, "login", saved["revision"])} {
		if message := world.refusal(test, world.ben, savePasswordItemQuery, map[string]any{"input": input}); !strings.Contains(message, "not found") && !strings.Contains(message, "changed on another device") {
			test.Errorf("ben saving over her item (%s): %s", name, message)
		}
	}
	for name, query := range map[string]string{"delete": deletePasswordItemQuery, "restore": restorePasswordItemQuery} {
		if message := world.refusal(test, world.ben, query, map[string]any{"id": id}); !strings.Contains(message, "not found") {
			test.Errorf("ben %s her item: %s", name, message)
		}
	}
	if kept := world.list(test, world.ada, nil)["items"].([]any); len(kept) != 1 || kept[0].(map[string]any)["encryptedFields"] != sealedOf("changed fields") {
		test.Fatalf("ben's attempts changed her item: %v", kept)
	}

	for name, change := range map[string]func(map[string]any){
		"an uppercase id":      func(input map[string]any) { input["id"] = "AAAAAAAA-0000-4000-8000-000000000009" },
		"a kind nobody knows":  func(input map[string]any) { input["itemKind"] = "folder" },
		"fields not sealed":    func(input map[string]any) { input["encryptedFields"] = "the password is hunter2" },
		"an item key not JSON": func(input map[string]any) { input["itemKey"] = "plain" },
	} {
		input := ada.itemInput("login", nil)
		change(input)
		if message := world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("%s: %s", name, message)
		}
	}
	if message := world.refusal(test, world.dave, savePasswordItemQuery, map[string]any{"input": ada.itemInput("note", nil)}); !strings.Contains(message, "not found") {
		test.Errorf("saving without passwords:use: %s", message)
	}

	deleted := world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": id})["DeletePasswordItem"].(map[string]any)
	if deleted["deletedAt"] == nil || deleted["revision"].(float64) <= saved["revision"].(float64) {
		test.Fatalf("a delete: %v", deleted)
	}
	listed := world.list(test, world.ada, saved["revision"])
	if items := listed["items"].([]any); len(items) != 1 || items[0].(map[string]any)["deletedAt"] == nil || len(listed["itemIds"].([]any)) != 0 {
		test.Fatalf("a deleted item is listed once with its time and not among the identifiers: %v", listed)
	}
	restored := world.run(test, world.ada, restorePasswordItemQuery, map[string]any{"id": id})["RestorePasswordItem"].(map[string]any)
	if restored["deletedAt"] != nil || restored["revision"].(float64) <= deleted["revision"].(float64) {
		test.Fatalf("a restore: %v", restored)
	}
	if count := world.run(test, world.ada, readPasswordsSetupQuery, nil)["ReadPasswordsSetup"].(map[string]any)["itemCount"]; count != float64(1) {
		test.Fatalf("itemCount: %v", count)
	}
}

// Sharing an item: the recipient is listed with the public key they set up,
// the first share needs a tap, the recipient reads the item with their own key
// and no list of shares, a person nobody shares a circle with is not found, a
// key for someone not reached or for an old public key is refused, and
// narrowing a share needs no tap.
func TestSharingAPasswordItemWithAPersonInACircle(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben, cora := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben), newPasswordsDevice(test, world.cora)
	world.setUp(test, ada)
	world.setUp(test, ben)
	world.setUp(test, cora)
	item := world.save(test, ada, "login")
	id := item["id"].(string)

	recipients := world.run(test, world.ada, passwordRecipientsQuery, map[string]any{"granteeUserId": world.ben.ID})["ListPasswordRecipients"].([]any)
	if len(recipients) != 1 || recipients[0].(map[string]any)["userId"] != world.ben.ID || recipients[0].(map[string]any)["publicKey"] != ben.publicKey ||
		recipients[0].(map[string]any)["hasKey"] != false {
		test.Fatalf("ben as a recipient: %v", recipients)
	}
	// Cora shares no circle with Ada: the same answer as for a person who is
	// not there.
	if message := world.refusal(test, world.ada, passwordRecipientsQuery, map[string]any{"granteeUserId": world.cora.ID}); !strings.Contains(message, "not found") {
		test.Fatalf("a person in no circle with ada: %s", message)
	}
	if message := world.refusal(test, world.ada, passwordRecipientsQuery, map[string]any{"granteeUserId": "nobody"}); !strings.Contains(message, "not found") {
		test.Fatalf("a person who is not there: %s", message)
	}
	if message := world.refusal(test, world.cora, passwordRecipientsQuery, map[string]any{"granteeCircleId": world.home}); !strings.Contains(message, "not found") {
		test.Fatalf("a circle cora is not in: %s", message)
	}
	if message := world.refusal(test, world.ada, passwordRecipientsQuery, nil); !strings.Contains(message, "invalid arguments") {
		test.Fatalf("asking about nobody: %s", message)
	}
	members := world.run(test, world.ada, passwordRecipientsQuery, map[string]any{"granteeCircleId": world.home})["ListPasswordRecipients"].([]any)
	if len(members) != 1 || members[0].(map[string]any)["userId"] != world.ben.ID {
		test.Fatalf("the members of home other than ada: %v", members)
	}

	shareWithBen := sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{map[string]any{
		"itemKey": sealedOf("item key of " + id), "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("item key for " + world.ben.Username), "recipientPublicKey": ben.publicKey,
	}})
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, setPasswordItemSharesQuery, shareWithBen); !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token adding a grantee: %v", failures)
	}
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 0 {
		test.Fatalf("a refused share reached ben: %v", got)
	}

	// Refused: a person no circle is shared with, a key for a person the
	// grantees do not reach, and a key for a public key that is not the
	// recipient's now. Each leaves nothing behind.
	for name, input := range map[string]map[string]any{
		"a person in no circle with her": sharesInput(id, []any{map[string]any{"granteeUserId": world.cora.ID}}, nil),
		"a circle she is not in":         sharesInput(id, []any{map[string]any{"granteeCircleId": "no-such-circle"}}, nil),
		"a key for a person not reached": sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{map[string]any{
			"itemKey": sealedOf("item key of " + id), "recipientUserId": world.cora.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("key"), "recipientPublicKey": cora.publicKey}}),
		"a key for an old public key": sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{map[string]any{
			"itemKey": sealedOf("item key of " + id), "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("key"), "recipientPublicKey": newPasswordsPublicKey(test)}}),
	} {
		message := world.refusal(test, world.ada, setPasswordItemSharesQuery, input)
		if name == "a key for an old public key" {
			if !strings.Contains(message, "That person's key changed") {
				test.Errorf("%s: %s", name, message)
			}
		} else if !strings.Contains(message, "not found") {
			test.Errorf("%s: %s", name, message)
		}
		if shares := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)["shares"].([]any); len(shares) != 0 {
			test.Fatalf("%s left a share behind: %v", name, shares)
		}
	}
	if message := world.refusal(test, world.ben, setPasswordItemSharesQuery, sharesInput(id, nil, nil)); !strings.Contains(message, "not found") {
		test.Fatalf("ben sharing ada's item: %s", message)
	}

	shared := world.run(test, world.ada, setPasswordItemSharesQuery, shareWithBen)["SetPasswordItemShares"].(map[string]any)
	shares := shared["shares"].([]any)
	if len(shares) != 1 || shares[0].(map[string]any)["granteeUserId"] != world.ben.ID || shares[0].(map[string]any)["granteeName"] != world.ben.DisplayName() ||
		shares[0].(map[string]any)["granteeCircleId"] != nil {
		test.Fatalf("the share as ada sees it: %v", shared)
	}
	recipient := shares[0].(map[string]any)["recipients"].([]any)[0].(map[string]any)
	if recipient["userId"] != world.ben.ID || recipient["hasKey"] != true || recipient["publicKey"] != ben.publicKey {
		test.Fatalf("the share as ada sees it: %v", shared)
	}

	// Ben reads the item with his own key and no shares; cora reads nothing.
	reading := world.list(test, world.ben, nil)
	readItems := reading["items"].([]any)
	if len(readItems) != 1 {
		test.Fatalf("what ben reads: %v", reading)
	}
	read := readItems[0].(map[string]any)
	if read["id"] != id || read["isSharedWithMe"] != true || read["itemKey"] != sealedForKeyPairOf("item key for "+world.ben.Username) || len(read["shares"].([]any)) != 0 ||
		read["encryptedFields"] != item["encryptedFields"] || read["ownerUserId"] != world.ada.ID || read["isUsableByAgent"] != false ||
		reading["itemIds"].([]any)[0] != id || reading["revision"] != shared["revision"] {
		test.Fatalf("what ben reads: %v", reading)
	}
	if got := world.list(test, world.cora, nil); len(got["items"].([]any)) != 0 || len(got["itemIds"].([]any)) != 0 {
		test.Fatalf("what cora reads: %v", got)
	}
	// Nothing new since the revision ben has.
	if got := world.list(test, world.ben, reading["revision"]); len(got["items"].([]any)) != 0 || len(got["itemIds"].([]any)) != 1 {
		test.Fatalf("since the revision ben has: %v", got)
	}
	// A save by the owner is seen by the recipient.
	edited := ada.namedItemInput(id, "login", shared["revision"])
	edited["encryptedFields"] = sealedOf("edited")
	world.run(test, world.ada, savePasswordItemQuery, map[string]any{"input": edited})
	if got := world.list(test, world.ben, reading["revision"])["items"].([]any); len(got) != 1 || got[0].(map[string]any)["encryptedFields"] != sealedOf("edited") {
		test.Fatalf("ben sees the edit: %v", got)
	}

	// Keeping the share as it is needs no tap, and neither does removing it.
	again := sharesInput(id, shareWithBen["input"].(map[string]any)["grantees"].([]any), nil)
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, setPasswordItemSharesQuery, again); len(failures) > 0 {
		test.Fatalf("a token keeping the share: %v", failures)
	}
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 1 {
		test.Fatalf("keeping the share without keys keeps the key ben has: %v", got)
	}
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, setPasswordItemSharesQuery, sharesInput(id, nil, nil)); len(failures) > 0 {
		test.Fatalf("a token removing the share: %v", failures)
	}
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 0 || len(got["itemIds"].([]any)) != 0 {
		test.Fatalf("after the share is removed ben reads: %v", got)
	}
}

// A share with a circle lists a member who sets up Passwords after the share
// was made as waiting. The owner's device adds the key, which needs no tap,
// and then the member reads the item. A key for a person nobody shared with, or
// for another person's item, is refused.
func TestACircleShareListsAWaitingMemberUntilTheirKeyIsAdded(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	item := world.save(test, ada, "note")
	id := item["id"].(string)

	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeCircleId": world.home}}, nil))
	if waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any); len(waiting) != 0 {
		test.Fatalf("a member who has not set up cannot be sealed for: %v", waiting)
	}

	world.setUp(test, ben)
	waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any)
	if len(waiting) != 1 {
		test.Fatalf("a member who set up after the share: %v", waiting)
	}
	entry := waiting[0].(map[string]any)
	recipient := entry["recipient"].(map[string]any)
	if entry["itemId"] != id || recipient["userId"] != world.ben.ID || recipient["publicKey"] != ben.publicKey || recipient["hasKey"] != false {
		test.Fatalf("the waiting share: %v", entry)
	}
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 0 {
		test.Fatalf("ben reads before he has a key: %v", got)
	}

	if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{shareKeyInput(id, world.cora, newPasswordsDevice(test, world.cora))}}); !strings.Contains(message, "not found") {
		test.Fatalf("a key for somebody the shares do not reach: %s", message)
	}
	if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{shareKeyInput(id, world.ben, &passwordsDevice{publicKey: newPasswordsPublicKey(test)})}}); !strings.Contains(message, "That person's key changed") {
		test.Fatalf("a key for an old public key: %s", message)
	}
	if message := world.refusal(test, world.ben, addPasswordShareKeysQuery, map[string]any{"keys": []any{shareKeyInput(id, world.ben, ben)}}); !strings.Contains(message, "not found") {
		test.Fatalf("a key for another person's item: %s", message)
	}
	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, addPasswordShareKeysQuery, map[string]any{"keys": []any{shareKeyInput(id, world.ben, ben)}})
	if len(failures) > 0 || data["AddPasswordShareKeys"] != float64(1) {
		test.Fatalf("adding the key with a token: %v %v", data, failures)
	}
	if waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any); len(waiting) != 0 {
		test.Fatalf("nobody waits now: %v", waiting)
	}
	reading := world.list(test, world.ben, nil)["items"].([]any)
	if len(reading) != 1 || reading[0].(map[string]any)["itemKey"] != sealedForKeyPairOf("item key for "+world.ben.Username) {
		test.Fatalf("what ben reads: %v", reading)
	}
}

// Leaving the circle that made a share possible ends it: the person reads
// nothing and the owner's item no longer lists them.
func TestLeavingTheCircleEndsAccessToASharedPasswordItem(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{map[string]any{
		"itemKey": sealedOf("item key of " + id), "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("item key for " + world.ben.Username), "recipientPublicKey": ben.publicKey}}))
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 1 {
		test.Fatalf("ben reads: %v", got)
	}

	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(world.home, world.ben.ID); err != nil {
			test.Fatalf("RemoveCircleMember: %s", err)
		}
	})
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 0 || len(got["itemIds"].([]any)) != 0 {
		test.Fatalf("after leaving ben reads: %v", got)
	}
	own := world.list(test, world.ada, nil)["items"].([]any)
	if len(own) != 1 || len(own[0].(map[string]any)["shares"].([]any)) != 0 {
		test.Fatalf("ada's item after ben left: %v", own)
	}
	// Coming back is not coming back to the share.
	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		if _, err := tx.AddCircleMember(world.home, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatalf("AddCircleMember: %s", err)
		}
	})
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 0 {
		test.Fatalf("after rejoining ben reads: %v", got)
	}
}

// The agent may use a login after a person's tap, never a card, and an item
// stops being the agent's when it is turned off or deleted. The agent key is
// made once and is a P-256 point.
func TestLettingTheAgentUseAnItemNeedsATapAndRefusesCards(test *testing.T) {
	world := newPasswordsWorld(test)
	ada := newPasswordsDevice(test, world.ada)
	world.setUp(test, ada)
	login := world.save(test, ada, "login")["id"].(string)
	card := world.save(test, ada, "card")["id"].(string)

	agentKey := world.run(test, world.ada, passwordsAgentKeyQuery, nil)["ReadPasswordsAgentKey"].(string)
	decoded, err := base64.StdEncoding.DecodeString(agentKey)
	if err != nil || len(decoded) != 65 || decoded[0] != 4 {
		test.Fatalf("the agent key: %q %v", agentKey, err)
	}
	if _, err := ecdh.P256().NewPublicKey(decoded); err != nil {
		test.Fatalf("the agent key is not a P-256 point: %s", err)
	}
	if again := world.run(test, world.ada, passwordsAgentKeyQuery, nil)["ReadPasswordsAgentKey"]; again != agentKey {
		test.Fatalf("the agent key changed: %v", again)
	}

	on := func(itemId string) map[string]any {
		return map[string]any{"input": map[string]any{"itemId": itemId, "isUsableByAgent": true, "itemKeyByAgentKey": sealedForKeyPairOf("item key for the agent")}}
	}
	off := func(itemId string) map[string]any {
		return map[string]any{"input": map[string]any{"itemId": itemId, "isUsableByAgent": false}}
	}
	if message := world.refusal(test, world.ada, passwordAgentUseQuery, on(card)); !strings.Contains(message, "A card cannot be used by the agent.") {
		test.Fatalf("a card: %s", message)
	}
	if _, failures := world.callWithCredential(test, world.ada, models.CredentialToken, passwordAgentUseQuery, on(login)); !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token turning it on: %v", failures)
	}
	if message := world.refusal(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": login, "isUsableByAgent": true}}); !strings.Contains(message, "invalid arguments") {
		test.Fatalf("turning it on without a key: %s", message)
	}
	if message := world.refusal(test, world.ben, passwordAgentUseQuery, on(login)); !strings.Contains(message, "not found") {
		test.Fatalf("somebody else's item: %s", message)
	}
	usable := world.run(test, world.ada, passwordAgentUseQuery, on(login))["SetPasswordItemUsableByAgent"].(map[string]any)
	if usable["isUsableByAgent"] != true {
		test.Fatalf("turned on: %v", usable)
	}
	data, failures := world.callWithCredential(test, world.ada, models.CredentialToken, passwordAgentUseQuery, off(login))
	if len(failures) > 0 || data["SetPasswordItemUsableByAgent"].(map[string]any)["isUsableByAgent"] != false {
		test.Fatalf("a token turning it off: %v %v", data, failures)
	}
	world.run(test, world.ada, passwordAgentUseQuery, on(login))
	deleted := world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": login})["DeletePasswordItem"].(map[string]any)
	if deleted["isUsableByAgent"] != false {
		test.Fatalf("a delete ends the agent's use: %v", deleted)
	}
}

// Erasing needs a tap and takes the setup and the items, and what was shared
// with the person is no longer theirs to open.
func TestErasingPasswordsNeedsATapAndTakesEverything(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	world.save(test, ben, "note")
	shared := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(shared, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{map[string]any{
		"itemKey": sealedOf("item key of " + shared), "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("item key for " + world.ben.Username), "recipientPublicKey": ben.publicKey}}))

	if _, failures := world.callWithCredential(test, world.ben, models.CredentialToken, erasePasswordsQuery, nil); !isTapRefusal(strings.Join(failures, "; ")) {
		test.Fatalf("a token erasing: %v", failures)
	}
	if erased := world.run(test, world.ben, erasePasswordsQuery, nil)["ErasePasswords"]; erased != true {
		test.Fatalf("erasing: %v", erased)
	}
	if setup := world.run(test, world.ben, readPasswordsSetupQuery, nil)["ReadPasswordsSetup"].(map[string]any); setup["isSetUp"] != false || setup["itemCount"] != float64(0) {
		test.Fatalf("after erasing: %v", setup)
	}
	if got := world.list(test, world.ben, nil); len(got["items"].([]any)) != 0 || len(got["itemIds"].([]any)) != 0 {
		test.Fatalf("after erasing ben reads: %v", got)
	}
	// Ada's item is hers still, and Ben, who has no key now, is not waited for
	// until he sets up again.
	if own := world.list(test, world.ada, nil)["items"].([]any); len(own) != 1 {
		test.Fatalf("ada's items: %v", own)
	}
	if waiting := world.run(test, world.ada, waitingPasswordSharesQuery, nil)["ListWaitingPasswordShares"].([]any); len(waiting) != 0 {
		test.Fatalf("waiting: %v", waiting)
	}
	if message := world.refusal(test, world.ben, savePasswordItemQuery, map[string]any{"input": ben.itemInput("note", nil)}); !strings.Contains(message, "not set up") {
		test.Fatalf("saving without a setup: %s", message)
	}
}

// What the audit log keeps about Passwords says what happened and never holds
// a sealed value, a key or a proof.
func TestThePasswordsAuditLogNeverHoldsASealedValue(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	id := world.save(test, ada, "login")["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{map[string]any{
		"itemKey": sealedOf("item key of " + id), "recipientUserId": world.ben.ID, "itemKeyByRecipientKey": sealedForKeyPairOf("item key for " + world.ben.Username), "recipientPublicKey": ben.publicKey}}))
	world.run(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": id, "isUsableByAgent": true, "itemKeyByAgentKey": sealedForKeyPairOf("item key for the agent")}})
	world.run(test, world.ada, deletePasswordItemQuery, map[string]any{"id": id})
	world.run(test, world.ada, erasePasswordsQuery, nil)

	dbtest.RunTransactionOn(test, world.database, func(tx db.Transaction) {
		wanted := map[string]bool{}
		for _, resourceType := range []models.AuditResourceType{models.AuditResourcePasswords, models.AuditResourcePasswordItem} {
			events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(resourceType), Limit: 100})
			if err != nil {
				test.Fatal(err)
			}
			for _, event := range events {
				text := string(event.Before) + string(event.After)
				for _, secret := range []string{"fields of", "item key", "ciphertext", ada.publicKey, ben.publicKey, "nonce", "private key"} {
					if strings.Contains(text, secret) {
						test.Errorf("an audit event holds %q: %s", secret, text)
					}
				}
				for _, name := range []string{"set_up", "saved", "shares_changed", "agent_use_changed", "deleted", "erased"} {
					if strings.Contains(text, `"event": "`+name+`"`) || strings.Contains(text, `"event":"`+name+`"`) {
						wanted[name] = true
					}
				}
			}
		}
		for _, name := range []string{"set_up", "saved", "shares_changed", "agent_use_changed", "deleted", "erased"} {
			if !wanted[name] {
				test.Errorf("no audit event for %s", name)
			}
		}
	})
}

// The passphrase's iteration count is from 600000 to 10000000 when setting up
// and when changing the passphrase, and nothing is stored when it is not.
func TestThePassphraseIterationCountHasARange(test *testing.T) {
	world := newPasswordsWorld(test)
	device := newPasswordsDevice(test, world.ada)
	for _, count := range []int{1, 100000, 599999, 10000001, 100000000} {
		input := device.setupInput()
		input["passphraseIterationCount"] = count
		if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": input}); !strings.Contains(message, "passphraseIterationCount is from 600000 to 10000000") {
			test.Errorf("setting up with %d iterations: %s", count, message)
		}
	}
	if setup := world.run(test, world.ada, readPasswordsSetupQuery, nil)["ReadPasswordsSetup"].(map[string]any); setup["isSetUp"] != false {
		test.Fatalf("a refused setup set something up: %v", setup)
	}
	for _, count := range []int{600000, 10000000} {
		device := newPasswordsDevice(test, world.ben)
		input := device.setupInput()
		input["passphraseIterationCount"] = count
		world.run(test, world.ben, setUpPasswordsQuery, map[string]any{"input": input})
		world.run(test, world.ben, erasePasswordsQuery, nil)
	}

	world.setUp(test, device)
	proof, _ := proofOf(device.passphrase)
	_, newHash := proofOf("a new passphrase")
	const changeQuery = `mutation ($input: PasswordsPassphraseChangeInput!) { ChangePasswordsPassphrase(input: $input) { passphraseIterationCount } }`
	change := func(count int) map[string]any {
		return map[string]any{"input": map[string]any{
			"proof": proof, "accountKeyByPassphrase": sealedOf("new account key"), "passphraseSalt": base64.StdEncoding.EncodeToString([]byte("fedcba9876543210")),
			"passphraseIterationCount": count, "passphraseProofHash": newHash,
		}}
	}
	for _, count := range []int{1, 599999, 10000001} {
		if message := world.refusal(test, world.ada, changeQuery, change(count)); !strings.Contains(message, "passphraseIterationCount is from 600000 to 10000000") {
			test.Errorf("changing to %d iterations: %s", count, message)
		}
	}
	if changed := world.run(test, world.ada, changeQuery, change(10000000))["ChangePasswordsPassphrase"].(map[string]any); changed["passphraseIterationCount"] != float64(10000000) {
		test.Fatalf("changing to the most: %v", changed)
	}
}

// An item carries its owner's public key, for the owner and for the person it
// is shared with, who opens the key sealed for them with it.
func TestPasswordItemsCarryTheirOwnersPublicKey(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	saved := world.save(test, ada, "login")
	if saved["ownerPublicKey"] != ada.publicKey {
		test.Fatalf("the saved item's owner key is %v, want Ada's", saved["ownerPublicKey"])
	}
	id := saved["id"].(string)
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(id, world.ben, ben)}))
	owned := world.list(test, world.ada, nil)["items"].([]any)
	shared := world.list(test, world.ben, nil)["items"].([]any)
	if len(owned) != 1 || owned[0].(map[string]any)["ownerPublicKey"] != ada.publicKey {
		test.Fatalf("the owner's list: %v", owned)
	}
	if len(shared) != 1 || shared[0].(map[string]any)["ownerPublicKey"] != ada.publicKey || shared[0].(map[string]any)["isSharedWithMe"] != true {
		test.Fatalf("what is shared with Ben carries the owner's key: %v", shared)
	}
}

// Every sealed value the server stores is checked for its shape: version 1, a
// 12 byte nonce, a ciphertext of at least the 16 byte tag, canonical base64,
// and for a key sealed for a key pair an ephemeral public key of 65 bytes that
// starts with 04. A public key must be a point on the curve.
func TestSealedValuesAreCheckedForTheirShape(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ben)
	nonce := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 12))
	ciphertext := base64.StdEncoding.EncodeToString(make([]byte, 20))
	ephemeralPublicKey := base64.StdEncoding.EncodeToString(append([]byte{4}, bytes.Repeat([]byte{9}, 64)...))
	sealedWith := func(version int, nonceText, ciphertextText string) string {
		return fmt.Sprintf(`{"version":%d,"nonce":"%s","ciphertext":"%s"}`, version, nonceText, ciphertextText)
	}
	sealedForKeyPairWith := func(ephemeralText string) string {
		return fmt.Sprintf(`{"version":1,"ephemeralPublicKey":"%s","nonce":"%s","ciphertext":"%s"}`, ephemeralText, nonce, ciphertext)
	}
	bad := map[string]string{
		"not JSON":                 "not json",
		"empty":                    "",
		"version 2":                sealedWith(2, nonce, ciphertext),
		"version 0":                sealedWith(0, nonce, ciphertext),
		"an 11 byte nonce":         sealedWith(1, base64.StdEncoding.EncodeToString(make([]byte, 11)), ciphertext),
		"a 13 byte nonce":          sealedWith(1, base64.StdEncoding.EncodeToString(make([]byte, 13)), ciphertext),
		"a ciphertext under 16":    sealedWith(1, nonce, base64.StdEncoding.EncodeToString(make([]byte, 15))),
		"padding left out":         sealedWith(1, nonce, strings.TrimRight(ciphertext, "=")),
		"a line break in base64":   sealedWith(1, nonce, ciphertext[:4]+`\n`+ciphertext[4:]),
		"unused bits set":          sealedWith(1, nonce, base64.StdEncoding.EncodeToString(make([]byte, 20))[:26]+"B="),
		"a field that is no field": `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + ciphertext + `","extra":1}`,
		"a plain object":           `{}`,
	}
	for name, sealed := range bad {
		input := ada.setupInput()
		input["accountKeyByPassphrase"] = sealed
		if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("setup, account key by passphrase, %s: %s", name, message)
		}
		input = ada.setupInput()
		input["accountKeyByRecoveryKey"] = sealed
		if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("setup, account key by recovery key, %s: %s", name, message)
		}
		input = ada.setupInput()
		input["privateKeyByAccountKey"] = sealed
		if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("setup, private key, %s: %s", name, message)
		}
	}
	for name, change := range map[string]func(map[string]any){
		"a public key that is not on the curve": func(input map[string]any) { input["publicKey"] = base64.StdEncoding.EncodeToString(make([]byte, 65)) },
		"a compressed public key": func(input map[string]any) {
			input["publicKey"] = base64.StdEncoding.EncodeToString(append([]byte{2}, make([]byte, 32)...))
		},
		"a public key without padding": func(input map[string]any) { input["publicKey"] = strings.TrimRight(ada.publicKey, "=") },
		"a salt without padding": func(input map[string]any) {
			input["passphraseSalt"] = strings.TrimRight(input["passphraseSalt"].(string), "=")
		},
		"a proof hash with a line break": func(input map[string]any) {
			input["passphraseProofHash"] = input["passphraseProofHash"].(string)[:8] + "\n" + input["passphraseProofHash"].(string)[8:]
		},
	} {
		input := ada.setupInput()
		change(input)
		if message := world.refusal(test, world.ada, setUpPasswordsQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("setup, %s: %s", name, message)
		}
	}
	world.setUp(test, ada)

	id := world.save(test, ada, "login")["id"].(string)
	revision := world.list(test, world.ada, nil)["items"].([]any)[0].(map[string]any)["revision"]
	for name, sealed := range bad {
		for field, input := range map[string]map[string]any{
			"encryptedFields": {"id": id, "itemKind": "login", "encryptedFields": sealed, "itemKey": sealedOf("key"), "baseRevision": revision},
			"itemKey":         {"id": id, "itemKind": "login", "encryptedFields": sealedOf("fields"), "itemKey": sealed, "baseRevision": revision},
		} {
			if message := world.refusal(test, world.ada, savePasswordItemQuery, map[string]any{"input": input}); !strings.Contains(message, "invalid arguments") {
				test.Errorf("saving an item, %s, %s: %s", field, name, message)
			}
		}
	}

	badForKeyPair := map[string]string{
		"a plain sealed value":         sealedOf("key"),
		"no ephemeral key":             sealedWith(1, nonce, ciphertext),
		"a 64 byte ephemeral key":      sealedForKeyPairWith(base64.StdEncoding.EncodeToString(append([]byte{4}, bytes.Repeat([]byte{9}, 63)...))),
		"a 66 byte ephemeral key":      sealedForKeyPairWith(base64.StdEncoding.EncodeToString(append([]byte{4}, bytes.Repeat([]byte{9}, 65)...))),
		"an ephemeral key starting 02": sealedForKeyPairWith(base64.StdEncoding.EncodeToString(append([]byte{2}, bytes.Repeat([]byte{9}, 64)...))),
		"an ephemeral key, no padding": sealedForKeyPairWith(strings.TrimRight(ephemeralPublicKey, "=")),
		"not JSON":                     "not json",
		"version 2":                    strings.Replace(sealedForKeyPairOf("key"), `"version":1`, `"version":2`, 1),
		"a short nonce":                strings.Replace(sealedForKeyPairOf("key"), nonce, base64.StdEncoding.EncodeToString(make([]byte, 8)), 1),
	}
	for name, sealed := range badForKeyPair {
		if message := world.refusal(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": id, "isUsableByAgent": true, "itemKeyByAgentKey": sealed}}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("the agent's key, %s: %s", name, message)
		}
		recipientKey := shareKeyInput(id, world.ben, ben)
		recipientKey["itemKeyByRecipientKey"] = sealed
		setKey := shareSetKeyInput(id, world.ben, ben)
		setKey["itemKeyByRecipientKey"] = sealed
		if message := world.refusal(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{setKey})); !strings.Contains(message, "invalid arguments") {
			test.Errorf("a recipient's key, %s: %s", name, message)
		}
		if message := world.refusal(test, world.ada, addPasswordShareKeysQuery, map[string]any{"keys": []any{recipientKey}}); !strings.Contains(message, "invalid arguments") {
			test.Errorf("a waiting recipient's key, %s: %s", name, message)
		}
	}
	if got := world.list(test, world.ben, nil)["items"].([]any); len(got) != 0 {
		test.Fatalf("a refused key reached Ben: %v", got)
	}
	// The same calls with well formed values are accepted.
	world.run(test, world.ada, setPasswordItemSharesQuery, sharesInput(id, []any{map[string]any{"granteeUserId": world.ben.ID}}, []any{shareSetKeyInput(id, world.ben, ben)}))
	world.run(test, world.ada, passwordAgentUseQuery, map[string]any{"input": map[string]any{"itemId": id, "isUsableByAgent": true, "itemKeyByAgentKey": sealedForKeyPairOf("key")}})
}

// Wrong proofs sent at the same moment are counted before any is judged: ten
// are judged and the rest wait, however many arrive together. A right proof
// gives its try back, so right proofs do not use up the allowance.
func TestParallelWrongProofsAreLimited(test *testing.T) {
	world := newPasswordsWorld(test)
	ada, ben := newPasswordsDevice(test, world.ada), newPasswordsDevice(test, world.ben)
	world.setUp(test, ada)
	world.setUp(test, ben)
	wrongProof, _ := proofOf("a wrong secret")
	rightProof, _ := proofOf(ben.passphrase)

	const tries = 40
	var judgedCount, waitedCount, otherCount atomic.Int32
	var group sync.WaitGroup
	for range tries {
		group.Add(1)
		go func() {
			defer group.Done()
			_, failures := world.callWith(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": wrongProof})
			switch message := strings.Join(failures, "; "); {
			case strings.Contains(message, "That passphrase is not right."):
				judgedCount.Add(1)
			case strings.Contains(message, "too many attempts"):
				waitedCount.Add(1)
			default:
				otherCount.Add(1)
			}
		}()
	}
	group.Wait()
	if judgedCount.Load() != passwordUnlockFailuresBurst || waitedCount.Load() != tries-passwordUnlockFailuresBurst || otherCount.Load() != 0 {
		test.Fatalf("%d proofs were judged, %d waited, %d were something else; want %d, %d, 0", judgedCount.Load(), waitedCount.Load(), otherCount.Load(), passwordUnlockFailuresBurst, tries-passwordUnlockFailuresBurst)
	}
	if message := world.refusal(test, world.ada, unlockPasswordsQuery, map[string]any{"proof": mustProof(ada.passphrase)}); !strings.Contains(message, "too many attempts") {
		test.Fatalf("the right proof after the wrong ones: %s", message)
	}

	// Right proofs, many at once, all open it: each gives its try back.
	var failedCount atomic.Int32
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, failures := world.callWith(test, world.ben, unlockPasswordsQuery, map[string]any{"proof": rightProof}); len(failures) > 0 {
				failedCount.Add(1)
			}
		}()
	}
	group.Wait()
	if failedCount.Load() != 0 {
		test.Fatalf("%d right proofs were refused", failedCount.Load())
	}
	for range 15 {
		world.run(test, world.ben, unlockPasswordsQuery, map[string]any{"proof": rightProof})
	}
}

func mustProof(secret string) string {
	proof, _ := proofOf(secret)
	return proof
}
