package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// What this computer remembers of an item comes only from what it saw signed
// by the owner's devices and what it did itself. A computer that has never seen
// an item before cannot tell an old signed record from the newest one, so it
// does nothing on its own for it; the person's own command does.

// rememberedItem is what the keys file holds of an item, empty if nothing.
func rememberedItem(test *testing.T, itemID string) passwordsKnownItem {
	test.Helper()
	files, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", "passwords-keys-*.json"))
	if len(files) == 0 {
		return passwordsKnownItem{}
	}
	file, err := readPasswordsKnownKeysFile(files[0])
	if err != nil {
		test.Fatal(err)
	}
	for _, items := range file.Items {
		if items[itemID] != nil {
			return *items[itemID]
		}
	}
	return passwordsKnownItem{}
}

// A computer with nothing remembered (a new machine, a fresh container) is shown
// the item as it was while the agent could use it, marked as needing a new key.
// It writes nothing down from that marked view, and the next command does not
// give the item a new key, or the agent a copy of it, on its own: it says what
// the person runs. The person's own rekey does it.
func TestPasswordsFreshComputerLeavesTheNewKeyAndTheAgentCopyToThePerson(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	item := fake.items[itemID]
	snapshot := snapshotOf(item)
	oldAgentCopy := item.itemKeyByAgentKey
	if oldAgentCopy == "" {
		test.Fatal("no agent copy after agent on")
	}
	fake.mustRun(testToken, "", "agent", "off", "example.com")
	if item.itemKeyByAgentKey != "" || item.isUsableByAgent {
		test.Fatalf("the agent copy survived agent off: %+v", item)
	}

	// New machine: nothing remembered. The server shows the old item, marked.
	if err := os.Remove(knownKeysFile(test)); err != nil {
		test.Fatal(err)
	}
	snapshot.showAgainOn(item)
	item.isUsableByAgent, item.itemKeyByAgentKey, item.needsNewKey = true, oldAgentCopy, true
	item.revision += 100

	fake.mustRun(testToken, "", "list")
	if remembered := rememberedItem(test, itemID); remembered.HighestRecordVersion != 0 {
		test.Fatalf("a marked view was written down: %+v", remembered)
	}
	notices.forget()
	rekeysBefore := fake.rekeyCount
	_, err := fake.run(testToken, "", "edit", "example.com", "--user-name", "ada")
	if err == nil || !strings.Contains(err.Error(), "needs you: teanode passwords rekey "+itemID) {
		test.Fatalf("edit on a computer that remembers nothing: %v", err)
	}
	if fake.rekeyCount != rekeysBefore || item.itemKeyByAgentKey != oldAgentCopy {
		test.Fatalf("the item was given a new key, or the agent a copy, on its own: %+v", item)
	}

	// The person's own command does it, for the record as the owner signed it.
	fake.mustRun(testToken, "", "rekey", "example.com")
	if item.itemKeyByAgentKey == "" || item.itemKeyByAgentKey == oldAgentCopy || item.needsNewKey {
		test.Fatalf("rekey did not give the item and the agent a new key: %+v", item)
	}
	// What it did itself is remembered, so the next re-key is automatic.
	if remembered := rememberedItem(test, itemID); !remembered.HasMadeAgentCopy || remembered.HighestRecordMac == "" {
		test.Fatalf("after rekey: %+v", remembered)
	}
}

// Who holds a key and whether the agent has a copy are never taken from the
// server's list: a person the server shows holding a key, and an agent copy this
// computer did not make, are not remembered, so an automatic re-key seals for
// neither and says what the person runs.
func TestPasswordsOnlyOwnSealsAreRemembered(test *testing.T) {
	original := automaticRekeyWait
	automaticRekeyWait = 0
	test.Cleanup(func() { automaticRekeyWait = original })
	fake, _, notices := agentFake(test)
	itemID := shareWithBenAndAgent(test, fake)
	item := fake.items[itemID]
	if remembered := rememberedItem(test, itemID); len(remembered.SealedForUserIDs) != 1 || !remembered.HasMadeAgentCopy {
		test.Fatalf("what this computer did: %+v", remembered)
	}

	// Another computer opens the same unmarked item: it writes down the record
	// and nothing of the server's list.
	if err := os.Remove(knownKeysFile(test)); err != nil {
		test.Fatal(err)
	}
	fake.mustRun(testToken, "", "list")
	remembered := rememberedItem(test, itemID)
	if remembered.HighestRecordVersion == 0 || remembered.HighestRecordMac == "" || len(remembered.SealedForUserIDs) != 0 || remembered.HasMadeAgentCopy {
		test.Fatalf("what a view of the item wrote down: %+v", remembered)
	}

	// The item is then marked: the re-key seals for nobody and for no agent copy,
	// and says so.
	item.needsNewKey = true
	notices.forget()
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	if fake.lastRekeyKeyCount != 0 || item.itemKeyByAgentKey != "" || item.isUsableByAgent {
		test.Fatalf("a re-key sealed for what this computer never did: keys %d agent %v", fake.lastRekeyKeyCount, item.isUsableByAgent)
	}
	if text := notices.text.String(); strings.Count(text, "needs you: teanode passwords rekey "+itemID) != 2 || !strings.Contains(text, "Ben") || !strings.Contains(text, "agent") {
		test.Fatalf("the person was not told what is left to them:\n%s", text)
	}
}

// Two records of one version cannot both be the owner's: a signed record with
// the same version as the one remembered and another signature is an old record
// shown again, as a lower version is.
func TestPasswordsSameVersionWithAnotherSignatureIsReplayed(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.addCora()
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	itemID := firstItemID(fake)
	current := fake.openedItemAccess(testToken, testPassphrase, itemID)

	// The device that holds the account key signs another record at the same
	// version, which reaches Cora.
	session := fake.sessionAs(testToken, testPassphrase)
	other, err := passwords.SignAccess(session.keys.AccountKey, itemID, passwords.ItemKindLogin,
		&passwords.PasswordAccess{Grantees: []passwords.PasswordGrantee{{UserID: "user-cora"}}}, current.RecordVersion-1)
	if err != nil {
		test.Fatal(err)
	}
	if other.RecordVersion != current.RecordVersion || string(other.RecordMac) == string(current.RecordMac) {
		test.Fatalf("the other record: %+v, the current one: %+v", other, current)
	}
	fake.replaceAccessOnServer(itemID, func(map[string]any) map[string]any {
		var written map[string]any
		content, err := json.Marshal(other)
		if err != nil {
			test.Fatal(err)
		}
		if err := json.Unmarshal(content, &written); err != nil {
			test.Fatal(err)
		}
		return written
	})
	if shown := fake.mustRun(testToken, "", "list", "--json"); !strings.Contains(shown, `"sharingCheckVerdict": "changedBack"`) {
		test.Fatalf("a second record of the same version was believed:\n%s", shown)
	}
	if _, err := fake.run(testToken, "", "share", "example.com", "cora"); err == nil || !strings.Contains(err.Error(), "changed this item's sharing back") {
		test.Fatalf("share on a replayed record: %v", err)
	}

	// Signing counts up from the remembered version, so the next record the
	// person writes is above both.
	fake.mustRun(testToken, "", "unshare", "example.com")
	if now := fake.openedItemAccess(testToken, testPassphrase, itemID); now.RecordVersion != current.RecordVersion+1 {
		test.Fatalf("after unshare: %+v", now)
	}
}

// A person named by share who has no key yet is remembered as one the person
// shared the item with, so share --waiting gives them the key once they have
// one; a person the server lists as waiting, whom this computer was never told
// about, is left to rekey.
func TestPasswordsShareWaitingSealsOnlyForThePeopleThePersonNamed(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	notices := recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if remembered := rememberedItem(test, itemID); len(remembered.SealedForUserIDs) != 1 || remembered.SealedForUserIDs[0] != "user-ben" {
		test.Fatalf("the person named Ben: %+v", remembered)
	}

	// Ben sets Passwords up. share --waiting gives him the key.
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)
	notices.forget()
	fake.mustRun(testToken, "", "share", "--waiting")
	if fake.items[itemID].keys["user-ben"] == nil {
		test.Fatalf("Ben was not given the key he waited for:\n%s", notices.text.String())
	}

	// The same on a computer that was never told: it leaves him to rekey.
	fake.items[itemID].keys = map[string]*client.PasswordShareKeyInput{}
	if err := os.Remove(knownKeysFile(test)); err != nil {
		test.Fatal(err)
	}
	fake.unlockAs(testToken)
	notices.forget()
	fake.mustRun(testToken, "", "share", "--waiting")
	if fake.items[itemID].keys["user-ben"] != nil || !strings.Contains(notices.text.String(), "needs you: teanode passwords rekey "+itemID) {
		test.Fatalf("a computer that was never told sealed for Ben:\n%s", notices.text.String())
	}
}

// A person the server says has too much shared with them is named and skipped
// by share --waiting, and a waiting list the server cut short says to run it
// again.
func TestPasswordsShareWaitingSkipsTooMuchSharedAndSaysMoreWait(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	notices := recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)

	fake.tooMuchSharedUserIDs = map[string]bool{"user-ben": true}
	fake.hasMoreWaiting = true
	notices.forget()
	output := fake.mustRun(testToken, "", "share", "--waiting")
	if fake.items[itemID].keys["user-ben"] != nil || !strings.Contains(notices.text.String(), "Ben has too much shared with them") {
		test.Fatalf("a person with too much shared was not skipped and named:\n%s", notices.text.String())
	}
	if !strings.Contains(output, "more are waiting: run this again") {
		test.Fatalf("a cut short waiting list did not say so: %q", output)
	}

	fake.tooMuchSharedUserIDs = nil
	fake.hasMoreWaiting = false
	output = fake.mustRun(testToken, "", "share", "--waiting")
	if fake.items[itemID].keys["user-ben"] == nil || strings.Contains(output, "more are waiting") {
		test.Fatalf("Ben was not given the key once he had room: %q", output)
	}
}

// A member of a circle this computer never sealed the item for is told in one
// line, with the circle's name; a person it sealed for before, or one the record
// names itself, is not.
func TestPasswordsCircleMemberLines(test *testing.T) {
	circleID := "circle-one"
	record := &passwords.PasswordAccess{Grantees: []passwords.PasswordGrantee{{CircleID: circleID}, {UserID: "user-dan"}}}
	item := &passwordsItem{Login: &passwords.LoginFields{Name: "Example login"}, SealedForUserIDs: []string{"user-cora"}}
	recipients := []*client.PasswordRecipientView{
		{UserID: "user-ben", Name: "Ben", Username: "ben"},
		{UserID: "user-cora", Name: "Cora", Username: "cora"},
		{UserID: "user-dan", Name: "Dan", Username: "dan"},
		{UserID: "user-eve", Name: "Eve", Username: "eve"},
	}
	circleIDs := map[string][]string{"user-ben": {circleID}, "user-cora": {circleID}, "user-dan": {circleID}}
	lines := item.circleMemberLines(record, recipients, circleIDs, map[string]string{circleID: "Neighbors"})
	if len(lines) != 1 || lines[0] != "Ben got Example login through Neighbors" {
		test.Fatalf("lines: %q", lines)
	}
}

// A keys file with a bad mode does not stop an item shared with the person from
// opening: it opens with the owner's key as the server gives it and a note says
// how to fix the mode. Sealing still refuses without a readable file.
func TestPasswordsBadKeysFileStillOpensSharedItems(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.unlockAs(benToken)
	path := knownKeysFile(test)
	if err := os.Chmod(path, 0o644); err != nil {
		test.Fatal(err)
	}
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "example.com")); output != "shared-secret-example" {
		test.Fatalf("Ben copies %q", output)
	}
	if !strings.Contains(notices.text.String(), "chmod 600 "+path) {
		test.Fatalf("the person was not told how to fix the mode:\n%s", notices.text.String())
	}

	// Ben shares an item of his own: sealing needs the file.
	fake.mustRun(benToken, "", "add", "ben.example.com", "--password", "another-secret-example")
	if _, err := fake.run(benToken, "", "share", "ben.example.com", "ada"); err == nil || !strings.Contains(err.Error(), "chmod 600 "+path) {
		test.Fatalf("sealing with a file that others can change: %v", err)
	}
}

// Bringing an item back from recently deleted needs the person's tap, which a
// command line never gives: it is refused at once, before the passphrase is
// asked for, as delete --forever is.
func TestPasswordsRestoreNeedsATap(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "delete", "example.com")
	fake.mustRun(testToken, "", "lock")

	fake.isTapRefused = true
	listedBefore := fake.operationCount("ListPasswordItems")
	_, err := fake.run(testToken, "", "restore", itemID)
	if err == nil || !strings.HasPrefix(err.Error(), "Bringing an item back from recently deleted") || !strings.Contains(err.Error(), "dashboard") || strings.Contains(err.Error(), "passphrase") {
		test.Fatalf("restore without a tap: %v", err)
	}
	if fake.operationCount("ListPasswordItems") != listedBefore || fake.items[itemID].deletedAt == nil {
		test.Fatal("the command went on to open Passwords or brought the item back")
	}

	// A server that does accept it brings the item back.
	fake.isTapRefused = false
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "restore", itemID)
	if fake.items[itemID].deletedAt != nil {
		test.Fatal("restore did not bring the item back")
	}
}
