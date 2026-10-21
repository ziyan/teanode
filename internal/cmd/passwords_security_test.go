package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// zeroCode is a safety code that is not anybody's.
const zeroCode = "0000 0000 0000 0000 0000 0000 0000 0000"

const (
	benPassphrase   = "ben passphrase example"
	coraToken       = "token-cora"
	coraPassphrase  = "cora passphrase example"
	hostileName     = "Router\x1b]52;c;ZXZpbA==\x07\x1b[2J"
	hostilePassword = "pw\x1b]52;c;ZXZpbA==\x07"
)

// keyNotices records the lines printed when a key is recorded for the first
// time or has changed.
type keyNotices struct {
	text bytes.Buffer
}

// lines are the lines printed so far.
func (self *keyNotices) lines() []string {
	return strings.Split(strings.TrimSpace(self.text.String()), "\n")
}

// forget drops what was printed so far.
func (self *keyNotices) forget() {
	self.text.Reset()
}

// recordKeyNotices replaces where the key notices are printed for the test.
func recordKeyNotices(test *testing.T) *keyNotices {
	test.Helper()
	notices := &keyNotices{}
	original := keyNoticeOutput
	keyNoticeOutput = &notices.text
	test.Cleanup(func() { keyNoticeOutput = original })
	return notices
}

// addCora adds a third person to the fake server, set up with Passwords.
func (self *fakePasswordsServer) addCora() {
	self.test.Helper()
	self.users[coraToken] = &fakePasswordsUser{id: "user-cora", username: "cora", name: "Cora", lockAfterMinutes: 15}
	self.setUpAs(coraToken, coraPassphrase)
}

// unlockAs unlocks Passwords for the person whose token it is. The people of a
// test share one profile, so the unlock file is whoever unlocked last.
func (self *fakePasswordsServer) unlockAs(token string) {
	self.test.Helper()
	passphrases := map[string]string{testToken: testPassphrase, benToken: benPassphrase, coraToken: coraPassphrase}
	self.mustRun(token, passphrases[token]+"\n", "unlock", "--passphrase-stdin")
}

// itemKeyOf is the item key of an item the person owns, opened the way a
// command opens it.
func (self *fakePasswordsServer) itemKeyOf(token, itemID string) []byte {
	self.test.Helper()
	passphrases := map[string]string{testToken: testPassphrase, benToken: benPassphrase, coraToken: coraPassphrase}
	items, err := self.sessionAs(token, passphrases[token]).loadItems(false)
	if err != nil {
		self.test.Fatal(err)
	}
	for _, item := range items {
		if item.View.ID == itemID {
			return item.ItemKey
		}
	}
	self.test.Fatalf("no item %s", itemID)
	return nil
}

// containsTerminalControl is whether text has a character a terminal would act
// on.
func containsTerminalControl(text string) bool {
	return strings.ContainsFunc(text, func(character rune) bool {
		return isTerminalControl(character) || character == '\r'
	})
}

// A server can set up Passwords for an account of its own and share one item
// "from" it, which puts that account's key in front of the person. The key is
// the first one seen, so it is recorded and used for sharing with that account
// without a question, and what is sealed is sealed for exactly that key. A key
// that no command recorded is still refused by sealing itself.
func TestPasswordsAKeySeenOnAnItemIsUsedWithoutAsking(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	fake.addCora()

	// Ben is the server's account. He shares one planted item with Ada.
	notices := recordKeyNotices(test)
	fake.unlockAs(benToken)
	fake.mustRun(benToken, "", "add", "planted.example.com", "--password", "planted-example")
	fake.mustRun(benToken, "", "share", "planted.example.com", "ada")

	// Ada opens it: the owner's key is written down, with one line.
	notices.forget()
	fake.unlockAs(testToken)
	if listed := fake.list(testToken, ""); len(listed) != 1 || !listed[0].IsSharedWithMe {
		test.Fatalf("Ada's list: %+v", listed)
	}
	if record := knownRecord(test, "user-ben"); record == nil || record.PublicKey != fake.users[benToken].setup.PublicKey || record.ComparedAt != nil {
		test.Fatalf("the key seen on an item: %+v", record)
	}
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "first key for Ben recorded" {
		test.Fatalf("notices: %q", lines)
	}

	// Ada then shares a real item with Ben, with nobody at a terminal: it is
	// sealed at once for the key already recorded, and nothing more is said.
	notices.forget()
	fake.mustRun(testToken, "", "add", "bank.example.com", "--password", "real-secret-example")
	fake.mustRun(testToken, "", "share", "bank.example.com", "ben")
	if notices.text.Len() != 0 {
		test.Errorf("a key already known was said again: %q", notices.text.String())
	}
	sealedCount := 0
	for _, item := range fake.items {
		if item.ownerUserID == "user-ada" && item.keys["user-ben"] != nil {
			sealedCount++
		}
	}
	if sealedCount != 1 {
		test.Fatalf("%d items sealed for Ben, want 1", sealedCount)
	}

	// Sealing itself refuses a key that no command recorded.
	session := fake.sessionAs(testToken, testPassphrase)
	recipients, err := listRecipientsOf(session, "user-cora")
	if err != nil {
		test.Fatal(err)
	}
	if _, err := session.sealItemKeyFor("item", make([]byte, passwords.KeyLength), "", recipients); err == nil || !strings.Contains(err.Error(), "not the one recorded") {
		test.Errorf("sealing for a key that was not recorded: %v", err)
	}
}

// listRecipientsOf is the server's view of one person as a recipient.
func listRecipientsOf(session *passwordsSession, userID string) (*client.PasswordRecipientView, error) {
	recipients, err := client.ListPasswordRecipients(session.ctx, session.connection, userID, "")
	if err != nil {
		return nil, err
	}
	if len(recipients) != 1 {
		return nil, errors.New("not one recipient")
	}
	return recipients[0], nil
}

// knownRecord is what the known keys file says of a person, nil if nothing.
func knownRecord(test *testing.T, userID string) *passwordsKnownKey {
	test.Helper()
	file, err := readPasswordsKnownKeysFile(knownKeysFile(test))
	if err != nil {
		test.Fatal(err)
	}
	for _, people := range file.Servers {
		if people[userID] != nil {
			return people[userID]
		}
	}
	return nil
}

// knownKeysFile is the one known keys file of a test.
func knownKeysFile(test *testing.T) string {
	test.Helper()
	files, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", "passwords-keys-*.json"))
	if len(files) != 1 {
		test.Fatalf("known keys files: %v", files)
	}
	return files[0]
}

// A known keys file from an earlier version is read as the key known for the
// person: sharing seals for it at once and says nothing, since it is not new.
func TestPasswordsKeysFromAnEarlierVersionAreUsed(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")

	// Write the file the way the earlier version did, with Ben's key in it.
	session := fake.sessionAs(testToken, testPassphrase)
	known, err := session.knownKeys()
	if err != nil {
		test.Fatal(err)
	}
	legacy := map[string]any{"servers": map[string]any{known.server: map[string]any{
		"user-ben": map[string]any{"username": "ben", "name": "Ben", "seenPublicKey": fake.users[benToken].setup.PublicKey, "seenAt": "2030-01-02T03:04:05Z"},
	}}}
	content, _ := json.Marshal(legacy)
	if err := writePrivateFile(known.path, content); err != nil {
		test.Fatal(err)
	}

	notices := recordKeyNotices(test)
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if fake.items[firstItemID(fake)].keys["user-ben"] == nil {
		test.Fatal("nothing was sealed for the key from the earlier version")
	}
	if notices.text.Len() != 0 {
		test.Errorf("a key already known was said again: %q", notices.text.String())
	}
}

// Nothing about a key needs a terminal, --force or TEANODE_FORCE: sharing with
// a first-seen person seals at once, and share still takes no --force.
func TestPasswordsSharingNeedsNoTerminalAndNoForce(test *testing.T) {
	useConfigurationDirectory(test)
	test.Setenv("TEANODE_FORCE", "")
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	notices := recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")

	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "first key for ben recorded" {
		test.Fatalf("notices: %q", lines)
	}
	if fake.items[firstItemID(fake)].keys["user-ben"] == nil {
		test.Fatal("nothing was sealed for a first-seen person")
	}
	if _, err := fake.run(testToken, "", "share", "example.com", "ben", "--force"); err == nil {
		test.Fatal("share still takes --force")
	}
}

// Turning the agent off gives the item a new key that the agent copy of the old
// one does not open, and the people the item is shared with keep it.
func TestPasswordsAgentOffGivesTheItemANewKey(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	agentKeyPair, _ := passwords.GenerateKeyPair()
	fake.agentPublicKey = base64.StdEncoding.EncodeToString(agentKeyPair.PublicKey())
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "agent-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	itemID := firstItemID(fake)
	item := fake.items[itemID]
	oldItemKey := fake.itemKeyOf(testToken, itemID)
	oldFields := item.encryptedFields
	oldRevision := item.revision

	fake.unlockAs(testToken)
	output := fake.mustRun(testToken, "", "agent", "off", "example.com")
	if !strings.Contains(output, "no longer use") || !strings.Contains(output, "new key") {
		test.Errorf("agent off said %q", output)
	}
	if fake.rekeyCount != 1 || fake.operationCount("SetPasswordItemUsableByAgent") != 1 {
		test.Fatalf("agent off made %d re-keys (and %d plain agent calls, one of them the earlier on)", fake.rekeyCount, fake.operationCount("SetPasswordItemUsableByAgent"))
	}
	if item.isUsableByAgent || item.itemKeyByAgentKey != "" || item.revision <= oldRevision || item.encryptedFields == oldFields {
		test.Fatalf("the item after agent off: usable %v, agent copy %q, revision %d", item.isUsableByAgent, item.itemKeyByAgentKey, item.revision)
	}
	newItemKey := fake.itemKeyOf(testToken, itemID)
	if string(newItemKey) == string(oldItemKey) {
		test.Fatal("the item key is the one the agent could open")
	}
	sealedFields, _ := passwords.ParseSealed(item.encryptedFields)
	if _, err := passwords.Open(oldItemKey, sealedFields, passwords.ItemFieldsData(itemID, passwords.ItemKindLogin)); err == nil {
		test.Fatal("the old key opens the fields saved after agent off")
	}
	if plain, err := passwords.Open(newItemKey, sealedFields, passwords.ItemFieldsData(itemID, passwords.ItemKindLogin)); err != nil || !strings.Contains(string(plain), "agent-secret-example") {
		test.Fatalf("the new key does not open the item: %v", err)
	}

	// Ben keeps the item, under the new key.
	if fake.lastRekeyKeyCount != 1 {
		test.Errorf("the re-key carried %d recipient keys, want 1", fake.lastRekeyKeyCount)
	}
	fake.unlockAs(benToken)
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "example.com")); output != "agent-secret-example" {
		test.Errorf("Ben copies %q", output)
	}

	// Off again is nothing to do.
	fake.unlockAs(testToken)
	if output := fake.mustRun(testToken, "", "agent", "off", "example.com"); !strings.Contains(output, "could not use") || fake.rekeyCount != 1 {
		test.Errorf("agent off twice said %q after %d re-keys", output, fake.rekeyCount)
	}
}

// A person taken off an item's shares no longer opens it, and the key they
// held opens nothing saved after. The person who stays keeps access, and the
// agent too when it still uses the item.
func TestPasswordsUnshareGivesTheItemANewKey(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	agentKeyPair, _ := passwords.GenerateKeyPair()
	fake.agentPublicKey = base64.StdEncoding.EncodeToString(agentKeyPair.PublicKey())
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	fake.addCora()
	recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "share", "example.com", "cora")
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	itemID := firstItemID(fake)
	item := fake.items[itemID]
	oldItemKey := fake.itemKeyOf(testToken, itemID)
	oldAgentCopy := item.itemKeyByAgentKey

	fake.unlockAs(testToken)
	output := fake.mustRun(testToken, "", "unshare", "example.com", "cora")
	if !strings.Contains(output, "stopped sharing") {
		test.Errorf("unshare said %q", output)
	}
	if fake.rekeyCount != 1 || fake.lastRekeyKeyCount != 1 {
		test.Fatalf("unshare made %d re-keys carrying %d recipient keys, want 1 and 1", fake.rekeyCount, fake.lastRekeyKeyCount)
	}
	if item.keys["user-cora"] != nil || item.keys["user-ben"] == nil {
		test.Fatalf("the keys after unshare: %+v", item.keys)
	}
	newItemKey := fake.itemKeyOf(testToken, itemID)
	sealedFields, _ := passwords.ParseSealed(item.encryptedFields)
	if _, err := passwords.Open(oldItemKey, sealedFields, passwords.ItemFieldsData(itemID, passwords.ItemKindLogin)); err == nil {
		test.Fatal("the old key opens the fields saved after unshare")
	}

	// The agent still uses the item, with a copy of the new key.
	if !item.isUsableByAgent || item.itemKeyByAgentKey == "" || item.itemKeyByAgentKey == oldAgentCopy {
		test.Fatalf("the agent copy after unshare: usable %v, %q", item.isUsableByAgent, item.itemKeyByAgentKey)
	}
	sealedForAgent, err := passwords.ParseSealedForKeyPair(item.itemKeyByAgentKey)
	if err != nil {
		test.Fatal(err)
	}
	ownerPublicKey, _ := base64.StdEncoding.DecodeString(fake.users[testToken].setup.PublicKey)
	agentItemKey, err := agentKeyPair.Open(sealedForAgent, ownerPublicKey, passwords.ShareData(itemID, "user-ada", passwords.AgentRecipientID))
	if err != nil || string(agentItemKey) != string(newItemKey) {
		test.Fatalf("the agent copy is not the new key: %v", err)
	}

	// Ben still opens it, Cora does not.
	fake.unlockAs(benToken)
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "example.com")); output != "shared-secret-example" {
		test.Errorf("Ben copies %q", output)
	}
	fake.unlockAs(coraToken)
	if len(fake.list(coraToken, "")) != 0 {
		test.Error("Cora still reaches the item")
	}

	// Without a person, everyone is taken off.
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "unshare", "example.com")
	if fake.rekeyCount != 2 || fake.lastRekeyKeyCount != 0 || len(item.keys) != 0 {
		test.Errorf("unshare with nobody: %d re-keys, %d recipient keys, keys %v", fake.rekeyCount, fake.lastRekeyKeyCount, item.keys)
	}
}

// Taking somebody off is the person's own command, so the people who remain get
// the new key even when this computer never sealed the item for them. A re-key
// nobody asked for (an edit of an item the server marked) does not give the key
// to a person this computer never sealed for. The command says so in one line
// that names rekey, share --waiting leaves them too, and rekey, which the
// person runs on purpose, gives them the key.
func TestPasswordsRekeyLeavesAPersonThisComputerNeverSealedForToThePerson(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	fake.addCora()
	notices := recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "share", "example.com", "cora")
	itemID := firstItemID(fake)

	// Forget that this computer ever sealed anything.
	if err := os.Remove(knownKeysFile(test)); err != nil {
		test.Fatal(err)
	}
	fake.unlockAs(testToken)
	notices.forget()
	fake.mustRun(testToken, "", "unshare", "example.com", "cora")
	if fake.rekeyCount != 1 || fake.lastRekeyKeyCount != 1 {
		test.Fatalf("%d re-keys carrying %d recipient keys, want 1 and 1", fake.rekeyCount, fake.lastRekeyKeyCount)
	}
	fake.unlockAs(benToken)
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "example.com")); output != "shared-secret-example" {
		test.Fatalf("Ben copies %q after the person took Cora off", output)
	}

	// The server marks the item again, and this computer has sealed for nobody.
	fake.unlockAs(testToken)
	rewriteRememberedItem(test, itemID, func(known *passwordsKnownItem) { known.SealedForUserIDs = nil })
	fake.items[itemID].needsNewKey = true
	notices.forget()
	rekeysBefore := fake.rekeyCount
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	if !strings.Contains(notices.text.String(), "needs you: teanode passwords rekey "+itemID) || !strings.Contains(notices.text.String(), "Ben") {
		test.Errorf("the edit did not say Ben is left to the person:\n%s", notices.text.String())
	}
	if fake.rekeyCount != rekeysBefore+1 || fake.lastRekeyKeyCount != 0 {
		test.Fatalf("%d re-keys carrying %d recipient keys, want one more and 0", fake.rekeyCount, fake.lastRekeyKeyCount)
	}
	fake.unlockAs(benToken)
	if len(fake.list(benToken, "")) != 0 {
		test.Fatal("Ben opens an item that was sealed for nobody")
	}

	// share --waiting does not give him the key either: the server's list of who
	// waits is not enough.
	fake.unlockAs(testToken)
	notices.forget()
	fake.mustRun(testToken, "", "share", "--waiting")
	if fake.items[itemID].keys["user-ben"] != nil || !strings.Contains(notices.text.String(), "needs you: teanode passwords rekey "+itemID) {
		test.Fatalf("share --waiting sealed for somebody this computer never sealed for:\n%s", notices.text.String())
	}

	fake.mustRun(testToken, "", "rekey", "example.com")
	fake.unlockAs(benToken)
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "example.com")); output != "shared-secret-example" {
		test.Errorf("Ben copies %q after rekey sealed the new key for him", output)
	}
}

// When the new key cannot be made, the person is told the access is removed
// but the key is old, and the same command again finishes the job. When the
// item moved on in between, it is read again and the key is made once more.
func TestPasswordsRekeyFailureAndRetry(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	agentKeyPair, _ := passwords.GenerateKeyPair()
	fake.agentPublicKey = base64.StdEncoding.EncodeToString(agentKeyPair.PublicKey())
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	item := fake.items[firstItemID(fake)]

	// Agent off fails: nothing changed, and the agent still uses the item.
	fake.rekeyFailure = "the server is having a bad day"
	if _, err := fake.run(testToken, "", "agent", "off", "example.com"); err == nil || !strings.Contains(err.Error(), "bad day") {
		test.Fatalf("a failed re-key: %v", err)
	}
	if !item.isUsableByAgent {
		test.Fatal("a failed re-key turned the agent off")
	}

	// Unshare fails after the narrowing: the person is off, and told so.
	_, err := fake.run(testToken, "", "unshare", "example.com", "ben")
	if err == nil || !strings.HasPrefix(err.Error(), "Removed. The item could not be given a new key; run the command again") {
		test.Fatalf("unshare when the new key fails: %v", err)
	}
	if len(item.grantees) != 0 {
		test.Fatal("the person was not removed")
	}
	if fake.rekeyCount != 0 {
		test.Fatal("a failed re-key was counted")
	}

	// The same command again makes the key, the item having moved on once.
	fake.rekeyFailure = ""
	fake.staleOnce = true
	output := fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	if fake.rekeyCount != 1 {
		test.Fatalf("running unshare again made %d re-keys", fake.rekeyCount)
	}
	if !strings.Contains(output, "stopped sharing") {
		test.Errorf("the second unshare said %q", output)
	}
	// The retry sealed the narrower record too.
	if opened := fake.openedItemAccess(testToken, testPassphrase, item.id); opened == nil || len(opened.Grantees) != 0 {
		test.Errorf("the record still reaches somebody after the retry: %+v", opened)
	}
	if !item.isUsableByAgent || item.itemKeyByAgentKey == "" {
		test.Error("the agent lost the item in the re-key")
	}
}

// Everything the server or a shared item chose is written to the terminal with
// its control characters escaped: a name, the owner's name, a person's name.
func TestPasswordsTerminalEscapesFromAShare(test *testing.T) {
	useConfigurationDirectory(test)
	fakeClipboard(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", hostileName, "--password", "shared-secret-example", "--user-name", "ada\x1b[31m")
	fake.mustRun(testToken, "", "share", "Router", "ben")
	id := firstItemID(fake)
	fake.unlockAs(benToken)

	for _, arguments := range [][]string{{"list"}, {"show", id}, {"copy", id, "--clear-after", "0"}, {"copy", id, "--clear-after", "1"}} {
		output, err := fake.run(benToken, "", arguments...)
		if err != nil {
			test.Fatalf("%v: %v", arguments, err)
		}
		if containsTerminalControl(strings.ReplaceAll(output, "\n", "")) {
			test.Errorf("%v wrote a control character: %q", arguments, output)
		}
	}

	// The same for the owner's own commands.
	fake.unlockAs(testToken)
	for _, arguments := range [][]string{
		{"edit", id, "--notes", "changed"}, {"delete", id}, {"restore", id}, {"unshare", id}, {"agent", "off", id},
	} {
		output, err := fake.run(testToken, "", arguments...)
		if err != nil {
			test.Fatalf("%v: %v", arguments, err)
		}
		if containsTerminalControl(strings.ReplaceAll(output, "\n", "")) {
			test.Errorf("%v wrote a control character: %q", arguments, output)
		}
	}
	output := fake.mustRun(testToken, "", "add", hostileName+"two", "--password", "another-example")
	if containsTerminalControl(strings.ReplaceAll(output, "\n", "")) {
		test.Errorf("add wrote a control character: %q", output)
	}
}

// Without a clipboard program the value is printed, unless it has a control
// character that a terminal would act on, which is refused: printing it
// escaped would show a value other than the one saved.
func TestPasswordsCopyWithoutAClipboardRefusesControlCharacters(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "Router", "--password", hostilePassword)
	fake.mustRun(testToken, "", "add", "Plain", "--password", "plain-secret-example")

	output, err := fake.run(testToken, "", "copy", "Router")
	if err == nil || !strings.Contains(err.Error(), "control characters") {
		test.Fatalf("copy of a password with an escape and no clipboard: %v", err)
	}
	if containsTerminalControl(output) || strings.Contains(output, "pw") {
		test.Errorf("the value was printed: %q", output)
	}
	// It can still be had, escaped, as JSON.
	if shown := fake.mustRun(testToken, "", "show", "Router", "--reveal", "--json"); !strings.Contains(shown, `\u001b`) {
		test.Errorf("show --json did not escape the value: %q", shown)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Plain")); output != "plain-secret-example" {
		test.Errorf("copy of an ordinary password prints %q", output)
	}
}

// A person who has not set Passwords up yet waits; their name is the server's
// word and is escaped.
func TestPasswordsWaitingRecipientNameIsEscaped(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.users[benToken].name = "Ben\x1b]52;c;ZXZpbA==\x07"
	fake.setUpAs(testToken, testPassphrase)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "Router", "--password", "pw")
	output := fake.mustRun(testToken, "", "share", "Router", "ben")
	if !strings.Contains(output, "has not set up Passwords yet") || containsTerminalControl(strings.ReplaceAll(output, "\n", "")) {
		test.Errorf("share with a person who has not set up: %q", output)
	}
}

// Unlocking keeps unencrypted keys, so it writes them only where a login's
// tmpfs is. With none it refuses before asking for anything, says what happens
// instead, and leaves nothing in the configuration directory.
func TestPasswordsUnlockNeedsARuntimeDirectory(test *testing.T) {
	useConfigurationDirectory(test)
	test.Setenv("XDG_RUNTIME_DIR", "")
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)

	_, err := fake.run(testToken, "", "unlock")
	if err == nil || !strings.Contains(err.Error(), "XDG_RUNTIME_DIR") || !strings.Contains(err.Error(), "ask for the passphrase") {
		test.Fatalf("unlock without a runtime directory: %v", err)
	}
	if strings.Contains(err.Error(), "terminal") {
		test.Errorf("unlock asked for the passphrase before it refused: %v", err)
	}
	if fake.operationCount("UnlockPasswords") != 0 {
		test.Error("unlock asked the server before it refused")
	}
	if _, err := fake.run(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin"); err == nil {
		test.Error("unlock with --passphrase-stdin wrote the keys to a disk")
	}
	if files := unlockFilesIn(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode")); len(files) != 0 {
		test.Errorf("unlock left %v in the configuration directory", files)
	}
	// Each command asks for the passphrase instead.
	fake.mustRun(testToken, testPassphrase+"\n", "add", "example.com", "--password", "secret-example", "--passphrase-stdin")
	if items := fake.list(testToken, testPassphrase+"\n"); len(items) != 1 {
		test.Errorf("list with the passphrase: %+v", items)
	}
	if _, err := fake.run(testToken, "", "list"); err == nil || !strings.Contains(err.Error(), "terminal") {
		test.Errorf("list without a passphrase: %v", err)
	}
	if files := unlockFilesIn(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode")); len(files) != 0 {
		test.Errorf("a command left %v in the configuration directory", files)
	}
}

// Keys an earlier build left in the configuration directory are deleted when
// found, by the next command, with or without a runtime directory.
func TestPasswordsUnlockFileInTheConfigurationDirectoryIsDeleted(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.unlockAs(testToken)
	runtimeFiles := unlockFilesIn(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "teanode"))
	if len(runtimeFiles) != 1 {
		test.Fatalf("unlock files: %v", runtimeFiles)
	}
	content, _ := os.ReadFile(runtimeFiles[0])
	leftover := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", filepath.Base(runtimeFiles[0]))
	if err := os.MkdirAll(filepath.Dir(leftover), 0o700); err != nil {
		test.Fatal(err)
	}

	// With a runtime directory.
	if err := os.WriteFile(leftover, content, 0o600); err != nil {
		test.Fatal(err)
	}
	fake.list(testToken, "")
	if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
		test.Error("a command left the old unlock file in the configuration directory")
	}
	// Without one.
	if err := os.WriteFile(leftover, content, 0o600); err != nil {
		test.Fatal(err)
	}
	test.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := fake.run(testToken, "", "list"); err == nil {
		test.Fatal("a command used the keys of an old unlock file")
	}
	if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
		test.Error("a command left the old unlock file in the configuration directory")
	}
	// And lock removes it too.
	if err := os.WriteFile(leftover, content, 0o600); err != nil {
		test.Fatal(err)
	}
	if output := fake.mustRun(testToken, "", "lock"); output != "locked\n" {
		test.Errorf("lock said %q", output)
	}
	if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
		test.Error("lock left the old unlock file")
	}
}

// The one-time code can be given on standard input, after the passphrase and
// the password, so it is not in the process list.
func TestPasswordsOneTimeCodeFromStandardInput(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)

	const secret = "JBSWY3DPEHPK3PXP"
	fake.mustRun(testToken, testPassphrase+"\nlogin-secret-example\n"+secret+"\n", "add", "example.com",
		"--passphrase-stdin", "--password-stdin", "--one-time-code-stdin")
	fake.unlockAs(testToken)
	shown := fake.mustRun(testToken, "", "show", "example.com", "--json", "--reveal")
	var parsed struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal([]byte(shown), &parsed); err != nil {
		test.Fatal(err)
	}
	if code, _ := parsed.Fields["oneTimeCode"].(string); !strings.Contains(code, "s left") {
		test.Errorf("the one-time code: %v", parsed.Fields)
	}
	if password, _ := parsed.Fields["password"].(string); password != "login-secret-example" {
		test.Errorf("the password: %v", parsed.Fields["password"])
	}

	// Without a password on standard input, the line after the passphrase is the code.
	fake.mustRun(testToken, testPassphrase+"\n"+secret+"\n", "add", "second.example.com", "--password", "other-secret", "--passphrase-stdin", "--one-time-code-stdin")
	if shown := fake.mustRun(testToken, "", "show", "second.example.com", "--json"); !strings.Contains(shown, "s left") {
		test.Errorf("the second one-time code: %s", shown)
	}

	// One way only; and a login only.
	if _, err := fake.run(testToken, secret+"\n", "add", "third.example.com", "--one-time-code", secret, "--one-time-code-stdin"); err == nil || !strings.Contains(err.Error(), "one way") {
		test.Errorf("both ways: %v", err)
	}
	if _, err := fake.run(testToken, secret+"\n", "add", "note", "--kind", "note", "--one-time-code-stdin"); err == nil || !strings.Contains(err.Error(), "not a field of a note") {
		test.Errorf("on a note: %v", err)
	}
	if _, err := fake.run(testToken, "not a secret!\n", "add", "fourth.example.com", "--one-time-code-stdin"); err == nil || !strings.Contains(err.Error(), "--one-time-code") {
		test.Errorf("a bad secret: %v", err)
	}
	// The help says the plain flag is visible to other programs.
	for _, flag := range passwordsFieldFlags() {
		if flag.Names()[0] == "one-time-code" && !strings.Contains(flag.(interface{ GetUsage() string }).GetUsage(), "process list") {
			test.Error("the help of --one-time-code does not say it is visible")
		}
	}
}

// A CSV export is for importing into another password manager, which needs each
// value exactly: a value that begins with a formula character is written as it
// is, and the help says the file is not for a spreadsheet.
func TestPasswordsCSVExportIsForImportNotForSpreadsheets(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "formula.example.com", "--user-name", "+1+1", "--password", "=1+1")
	path := filepath.Join(test.TempDir(), "out.csv")
	fake.mustRun(testToken, "", "export", path, "--force")
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "=1+1") || !strings.Contains(string(content), "+1+1") {
		test.Errorf("the exported values were changed:\n%s", content)
	}
	for _, command := range NewPasswordsCommand().Commands {
		if command.Name == "export" && !strings.Contains(command.Description, "spreadsheet") {
			test.Error("the help of export does not say the CSV is not for a spreadsheet")
		}
	}
}

// trust confirms a key without a terminal, but only when the code given is the
// code of the key the server hands out now. A wrong code records nothing.
func TestPasswordsTrustComparesTheSafetyCode(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")

	// Ben has not set up Passwords: there is no key to confirm.
	if _, err := fake.run(testToken, "", "trust", "ben", zeroCode); err == nil || !strings.Contains(err.Error(), "has not set up Passwords") {
		test.Errorf("trust for somebody with no key: %v", err)
	}
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)
	benKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	code := passwords.SafetyCode(benKey)

	// A wrong code, and codes that are not codes, record nothing.
	wrong := zeroCode
	if wrong == code {
		wrong = strings.ReplaceAll(zeroCode, "0", "1")
	}
	if _, err := fake.run(testToken, "", "trust", "ben", wrong); err == nil || !strings.Contains(err.Error(), "nothing was recorded") {
		test.Errorf("a wrong code: %v", err)
	}
	for _, bad := range []string{"1234", "abcd abcd abcd abcd abcd", strings.ReplaceAll(code, " ", "") + "1"} {
		if _, err := fake.run(testToken, "", "trust", "ben", bad); err == nil || !strings.Contains(err.Error(), "eight groups") {
			test.Errorf("trust with %q: %v", bad, err)
		}
	}
	if _, err := fake.run(testToken, "", "trust", "ben"); err == nil {
		test.Error("trust with no code")
	}
	if _, err := fake.run(testToken, "", "trust", "ben", code, "--code-stdin"); err == nil {
		test.Error("trust with a code both ways")
	}
	if matches, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", "passwords-keys-*.json")); len(matches) != 0 {
		if record := knownRecord(test, "user-ben"); record != nil && record.ComparedAt != nil {
			test.Fatal("a refused trust recorded a key")
		}
	}
	// The right code, with its spaces dropped, from standard input.
	output := fake.mustRun(testToken, strings.ReplaceAll(code, " ", "")+"\n", "trust", "ben", "--code-stdin")
	if !strings.Contains(output, "marked the key of ben as compared") {
		test.Errorf("trust said %q", output)
	}
	if record := knownRecord(test, "user-ben"); record == nil || record.PublicKey != fake.users[benToken].setup.PublicKey || record.ComparedAt == nil {
		test.Fatalf("the key was not marked as compared: %+v", record)
	}
	// Sharing works after it, as it did before it.
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if fake.items[firstItemID(fake)].keys["user-ben"] == nil {
		test.Fatal("nothing was sealed after trust")
	}

	// Ben erases Passwords and sets it up again: the key changed, the old code
	// marks nothing, the new one does.
	fake.unlockAs(benToken)
	fake.mustRun(benToken, "", "erase", "--force")
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)
	newKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	if _, err := fake.run(testToken, "", "trust", "ben", code); err == nil {
		test.Fatal("the old code marked a new key")
	}
	fake.mustRun(testToken, "", "trust", "ben", passwords.SafetyCode(newKey))
	if record := knownRecord(test, "user-ben"); record.PublicKey != fake.users[benToken].setup.PublicKey || record.ComparedAt == nil {
		test.Errorf("the new key was not marked as compared: %+v", record)
	}
}

// openedItemAccess is the access record sealed inside an item the person owns,
// opened the way a command opens it.
func (self *fakePasswordsServer) openedItemAccess(token, passphrase, itemID string) *passwords.PasswordAccess {
	self.test.Helper()
	items, err := self.sessionAs(token, passphrase).loadItems(true)
	if err != nil {
		self.test.Fatal(err)
	}
	for _, item := range items {
		if item.View.ID == itemID {
			return item.Access()
		}
	}
	self.test.Fatalf("no item %s", itemID)
	return nil
}
