package cmd

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// The access record sealed in an item is signed by the owner's devices and
// versioned, and this computer remembers the highest version it saw. These
// tests play the server (and anyone who holds an item key) writing a record of
// their own, or showing an old one again.

// sealedAccessSnapshot is an item as the server held it at one moment: the
// fields and the item key, which a server can show again later.
type sealedAccessSnapshot struct {
	encryptedFields, itemKey string
}

func snapshotOf(item *fakePasswordsItem) sealedAccessSnapshot {
	return sealedAccessSnapshot{encryptedFields: item.encryptedFields, itemKey: item.itemKey}
}

func (self sealedAccessSnapshot) showAgainOn(item *fakePasswordsItem) {
	item.encryptedFields, item.itemKey = self.encryptedFields, self.itemKey
}

// replaceAccessOnServer is what somebody who holds the item key can do: open
// the fields, put another access record in them, and seal them again under the
// same item key. The change function gets the record as raw JSON and returns
// the one to write.
func (self *fakePasswordsServer) replaceAccessOnServer(itemID string, change func(access map[string]any) map[string]any) {
	self.test.Helper()
	itemKey := self.itemKeyOf(testToken, itemID)
	item := self.items[itemID]
	sealed, err := passwords.ParseSealed(item.encryptedFields)
	if err != nil {
		self.test.Fatal(err)
	}
	plain, err := passwords.Open(itemKey, sealed, passwords.ItemFieldsData(itemID, passwords.ItemKind(item.itemKind)))
	if err != nil {
		self.test.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(plain, &fields); err != nil {
		self.test.Fatal(err)
	}
	access, _ := fields["access"].(map[string]any)
	if access == nil {
		access = map[string]any{}
	}
	if changed := change(access); changed == nil {
		delete(fields, "access")
	} else {
		fields["access"] = changed
	}
	forged, err := json.Marshal(fields)
	if err != nil {
		self.test.Fatal(err)
	}
	resealed, err := passwords.Seal(itemKey, forged, passwords.ItemFieldsData(itemID, passwords.ItemKind(item.itemKind)))
	if err != nil {
		self.test.Fatal(err)
	}
	item.encryptedFields = resealed.String()
}

// shareWithBenAndAgent makes an item that is shared with Ben (whose key Ada
// confirmed) and that the agent may use, and returns its id.
func shareWithBenAndAgent(test *testing.T, fake *fakePasswordsServer) string {
	test.Helper()
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	return firstItemID(fake)
}

// openedItem opens one owned item the way a command does.
func (self *fakePasswordsServer) openedItem(itemID string) *passwordsItem {
	self.test.Helper()
	items, err := self.sessionAs(testToken, testPassphrase).loadItems(true)
	if err != nil {
		self.test.Fatal(err)
	}
	for _, item := range items {
		if item.View.ID == itemID {
			return item
		}
	}
	self.test.Fatalf("no item %s", itemID)
	return nil
}

// A record the owner's devices did not sign reaches nobody and gives the agent
// nothing, whoever wrote it: one with no signature, one signed under another
// key, and a signed one with a grantee added afterwards.
func TestPasswordsForgedAccessRecordReachesNobody(test *testing.T) {
	forgeries := map[string]func(access map[string]any) map[string]any{
		"missing signature": func(access map[string]any) map[string]any {
			delete(access, "recordMac")
			delete(access, "recordVersion")
			access["isUsableByAgent"] = true
			return access
		},
		"signature under another key": func(access map[string]any) map[string]any {
			otherKey, _ := passwords.NewKey()
			access["recordVersion"] = 99
			forged, _ := passwords.SignAccess(otherKey, "item", passwords.ItemKindLogin, &passwords.PasswordAccess{IsUsableByAgent: true}, 98)
			access["recordMac"] = base64.StdEncoding.EncodeToString(forged.RecordMac)
			return access
		},
		"grantee added to a signed record": func(access map[string]any) map[string]any {
			access["grantees"] = append(access["grantees"].([]any), map[string]any{"userId": "user-cora"})
			return access
		},
		"record taken away": func(map[string]any) map[string]any { return nil },
	}
	for name, forge := range forgeries {
		test.Run(name, func(test *testing.T) {
			fake, _, _ := agentFake(test)
			itemID := shareWithBenAndAgent(test, fake)
			item := fake.items[itemID]
			fake.replaceAccessOnServer(itemID, forge)

			// The server's flag still says the agent may use it; the list does
			// not repeat that, and says the sharing could not be checked.
			var listed []passwordsItemSummary
			if err := json.Unmarshal([]byte(fake.mustRun(testToken, "", "list", "--json")), &listed); err != nil || len(listed) != 1 {
				test.Fatalf("list: %v %+v", err, listed)
			}
			if listed[0].IsUsableByAgent || listed[0].SharingCheckVerdict != "notChecked" {
				test.Errorf("list --json: %+v", listed[0])
			}
			wanted := "sharing could not be checked: run teanode passwords share " + itemID
			for _, command := range [][]string{{"list"}, {"show", "example.com"}} {
				if output := fake.mustRun(testToken, "", command...); !strings.Contains(output, wanted) {
					test.Errorf("%v did not say %q:\n%s", command, wanted, output)
				}
			}
			if shown := fake.mustRun(testToken, "", "show", "example.com", "--json"); !strings.Contains(shown, `"isUsableByAgent": false`) {
				test.Errorf("show --json repeated the server's agent flag:\n%s", shown)
			}

			// With the item marked as needing a new key, nothing automatic
			// happens: no new key, nothing sealed for Ben, no agent copy.
			item.needsNewKey = true
			rekeysBefore := fake.rekeyCount
			for _, command := range [][]string{
				{"edit", "example.com", "--user-name", "ada"},
				{"rekey", "example.com"},
				{"share", "example.com", "ben"},
			} {
				_, err := fake.run(testToken, "", command...)
				if err == nil || !strings.Contains(err.Error(), "sharing could not be checked") {
					test.Errorf("%v: %v", command, err)
				}
			}
			if fake.rekeyCount != rekeysBefore || !item.needsNewKey {
				test.Fatalf("an item whose sharing could not be checked was given a new key (%d to %d)", rekeysBefore, fake.rekeyCount)
			}
			// share --waiting leaves it alone too.
			if output, err := fake.run(testToken, "", "share", "--waiting"); err != nil || strings.Contains(output, "sealed 1 item") {
				test.Errorf("share --waiting: %q %v", output, err)
			}
			if fake.rekeyCount != rekeysBefore {
				test.Fatal("share --waiting re-keyed an item whose sharing could not be checked")
			}
		})
	}
}

// An item whose sharing cannot be checked is set again by taking everyone off
// (which starts from a record that shares with nobody and signs it), and then
// sharing it: the record is signed one version above the highest seen.
func TestPasswordsSharingIsSetAgainAfterItCouldNotBeChecked(test *testing.T) {
	fake, _, _ := agentFake(test)
	itemID := shareWithBenAndAgent(test, fake)
	item := fake.items[itemID]
	versionBefore := fake.openedItemAccess(testToken, testPassphrase, itemID).RecordVersion
	fake.replaceAccessOnServer(itemID, func(access map[string]any) map[string]any {
		delete(access, "recordMac")
		return access
	})
	item.needsNewKey = true

	// Sharing is not added to what the server lists: the people on it are taken
	// off first, and the sentence says so.
	if _, err := fake.run(testToken, "", "share", "example.com", "ben"); err == nil || !strings.Contains(err.Error(), "teanode passwords unshare "+itemID) {
		test.Fatalf("share on an unchecked item that the server lists people on: %v", err)
	}
	if _, err := fake.run(testToken, "", "agent", "on", "example.com", "--force"); err == nil || !strings.Contains(err.Error(), "teanode passwords unshare "+itemID) {
		test.Fatalf("agent on on an unchecked item that the server lists people on: %v", err)
	}
	// Taking Ben off takes everyone off and gives the item a new key with a
	// freshly signed record that reaches nobody and gives the agent nothing.
	fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	opened := fake.openedItem(itemID)
	if opened.AccessCheck != accessCheckVerified || len(opened.VerifiedAccess.Grantees) != 0 || opened.VerifiedAccess.IsUsableByAgent || opened.VerifiedAccess.RecordVersion != versionBefore+1 {
		test.Fatalf("after unshare: check %v record %+v (version was %d)", opened.AccessCheck, opened.VerifiedAccess, versionBefore)
	}
	if len(item.keys) != 0 || item.isUsableByAgent || item.itemKeyByAgentKey != "" || item.needsNewKey {
		test.Fatalf("after unshare the server still holds keys: %+v", item)
	}
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if access := fake.openedItemAccess(testToken, testPassphrase, itemID); len(access.Grantees) != 1 || access.RecordVersion != versionBefore+2 {
		test.Fatalf("after share: %+v", access)
	}
}

// An old record the server shows again has a valid signature, but a lower
// version than this computer saw: nothing automatic is done with the item, and
// the person is told. This is the replay of a record that still listed
// somebody who was taken off, with the item marked as needing a new key.
func TestPasswordsReplayedAccessRecordIsRefused(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.addCora()
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "share", "example.com", "cora")
	itemID := firstItemID(fake)
	item := fake.items[itemID]
	old := snapshotOf(item)
	oldVersion := fake.openedItemAccess(testToken, testPassphrase, itemID).RecordVersion

	// Cora is taken off, which re-keys the item with a newer record.
	fake.mustRun(testToken, "", "unshare", "example.com", "cora")
	if now := fake.openedItemAccess(testToken, testPassphrase, itemID); now.RecordVersion <= oldVersion || len(now.Grantees) != 1 {
		test.Fatalf("after unshare: %+v", now)
	}

	// The server shows the old fields and key again, lists Cora on it again
	// with the key she had, and marks the item as needing a new key.
	old.showAgainOn(item)
	coraUserID := "user-cora"
	item.grantees = append(item.grantees, &client.PasswordShareGranteeInput{GranteeUserID: &coraUserID})
	item.keys["user-cora"] = &client.PasswordShareKeyInput{RecipientUserID: "user-cora", RecipientPublicKey: fake.users[coraToken].setup.PublicKey, ItemKeyByRecipientKey: "{}"}
	item.needsNewKey = true

	words := "the server changed this item's sharing back"
	for _, command := range [][]string{{"list"}, {"show", "example.com"}} {
		if output := fake.mustRun(testToken, "", command...); !strings.Contains(output, words) {
			test.Errorf("%v did not say %q:\n%s", command, words, output)
		}
	}
	if shown := fake.mustRun(testToken, "", "list", "--json"); !strings.Contains(shown, `"sharingCheckVerdict": "changedBack"`) {
		test.Errorf("list --json:\n%s", shown)
	}
	rekeysBefore := fake.rekeyCount
	keysBefore := len(item.keys)
	for _, command := range [][]string{
		{"edit", "example.com", "--user-name", "ada"},
		{"rekey", "example.com"},
		{"share", "example.com", "cora"},
		{"agent", "on", "example.com", "--force"},
	} {
		_, err := fake.run(testToken, "", command...)
		if err == nil || !strings.Contains(err.Error(), "the server changed this item's sharing back") {
			test.Errorf("%v: %v", command, err)
		}
	}
	if fake.rekeyCount != rekeysBefore || len(item.keys) != keysBefore || item.itemKeyByAgentKey != "" {
		test.Fatalf("something was done with an item whose sharing was changed back: re-keys %d to %d, keys %d to %d", rekeysBefore, fake.rekeyCount, keysBefore, len(item.keys))
	}

	// The person sets it again by taking everyone off and sharing.
	fake.mustRun(testToken, "", "unshare", "example.com")
	opened := fake.openedItem(itemID)
	if opened.AccessCheck != accessCheckVerified || len(opened.VerifiedAccess.Grantees) != 0 || opened.VerifiedAccess.RecordVersion <= oldVersion+1 {
		test.Fatalf("after unshare: check %v record %+v", opened.AccessCheck, opened.VerifiedAccess)
	}
}

// A re-key nobody asked for never widens: it seals only for people this
// computer last saw holding a key. On a computer that remembers nothing of the
// item (a rollback shown to a new computer, say), a person the record reaches
// and whose key was confirmed here still waits.
func TestPasswordsAutomaticRekeyNeverWidens(test *testing.T) {
	original := automaticRekeyWait
	automaticRekeyWait = 0
	test.Cleanup(func() { automaticRekeyWait = original })
	fake, _, _ := agentFake(test)
	itemID := shareWithBenAndAgent(test, fake)
	item := fake.items[itemID]

	// A computer that remembers Ben holding a key and the agent's copy seals
	// for both.
	item.needsNewKey = true
	output := fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	if fake.lastRekeyKeyCount != 1 || !item.isUsableByAgent || item.itemKeyByAgentKey == "" || strings.Contains(output, "cannot open it until") {
		test.Fatalf("a computer that remembers them: keys %d, agent %v, output %q", fake.lastRekeyKeyCount, item.isUsableByAgent, output)
	}

	// The same item on a computer that remembers nothing of it: the record
	// reaches Ben, his key is confirmed here, but this computer never saw him
	// hold a key, so he waits, and the agent gets no copy.
	forgetRememberedItems(test)
	item.needsNewKey = true
	output = fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada again")
	if fake.lastRekeyKeyCount != 0 || !strings.Contains(output, "Ben (ben) cannot open it until they have the new key") {
		test.Errorf("a computer that remembers nothing sealed for Ben: keys %d\n%s", fake.lastRekeyKeyCount, output)
	}
	if item.isUsableByAgent || item.itemKeyByAgentKey != "" {
		test.Error("a computer that remembers nothing made an agent copy")
	}
	// The record still says the agent may use it: only the person narrows it.
	if access := fake.openedItemAccess(testToken, testPassphrase, itemID); !access.IsUsableByAgent || len(access.Grantees) != 1 {
		test.Errorf("the record was cut: %+v", access)
	}

	// share --waiting is what gives him the key, after the person confirms him.
	if output := fake.mustRun(testToken, "", "share", "--waiting"); !strings.Contains(output, "sealed 1 item") || item.keys["user-ben"] == nil {
		test.Errorf("share --waiting: %q", output)
	}

	// rekey, which the person runs on purpose, seals for everybody the record
	// reaches whose key is confirmed, and for the agent when the record and the
	// server agree.
	forgetRememberedItems(test)
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	forgetRememberedItems(test)
	fake.mustRun(testToken, "", "rekey", "example.com")
	if fake.lastRekeyKeyCount != 1 || !item.isUsableByAgent || item.itemKeyByAgentKey == "" {
		test.Errorf("rekey: keys %d agent %v", fake.lastRekeyKeyCount, item.isUsableByAgent)
	}
}

// The rollback of the audit: a record that listed somebody the owner later
// took off, shown again to a computer that never saw the newer record. The
// signature is valid and the person's key is confirmed here, but the person was
// never seen holding a key by this computer, so an automatic re-key leaves
// them waiting.
func TestPasswordsRolledBackRecordDoesNotGiveANewKeyToThePersonTakenOff(test *testing.T) {
	original := automaticRekeyWait
	automaticRekeyWait = 0
	test.Cleanup(func() { automaticRekeyWait = original })
	fake, _, _ := agentFake(test)
	fake.addCora()
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "share", "example.com", "cora")
	itemID := firstItemID(fake)
	item := fake.items[itemID]
	old := snapshotOf(item)
	fake.mustRun(testToken, "", "unshare", "example.com", "cora")

	// Another computer (nothing remembered) is shown the old record, with the
	// item marked as needing a new key and Cora listed on it again.
	old.showAgainOn(item)
	coraUserID := "user-cora"
	item.grantees = append(item.grantees, &client.PasswordShareGranteeInput{GranteeUserID: &coraUserID})
	item.needsNewKey = true
	forgetRememberedItems(test)

	rekeysBefore := fake.rekeyCount
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	if fake.rekeyCount != rekeysBefore+1 || item.keys["user-cora"] != nil || item.keys["user-ben"] != nil || fake.lastRekeyKeyCount != 0 {
		test.Fatalf("the new key was sealed for somebody this computer never saw holding one: %v", item.keys)
	}
}

// Automatic re-keys of one item are spaced by a wait, and one command makes
// only so many; the person runs rekey to go on.
func TestPasswordsAutomaticRekeysAreSpacedAndLimited(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	item := fake.items[itemID]

	item.needsNewKey = true
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	if fake.rekeyCount != 1 {
		test.Fatalf("the first automatic re-key: %d", fake.rekeyCount)
	}
	item.needsNewKey = true
	if _, err := fake.run(testToken, "", "edit", "example.com", "--user-name", "ada again"); err == nil || !strings.Contains(err.Error(), "teanode passwords rekey "+itemID) {
		test.Fatalf("the second automatic re-key within the wait: %v", err)
	}
	if fake.rekeyCount != 1 || !item.needsNewKey {
		test.Fatalf("the second automatic re-key was made: %d", fake.rekeyCount)
	}
	// The person's own rekey is never held back.
	fake.mustRun(testToken, "", "rekey", "example.com")
	if fake.rekeyCount != 2 || item.needsNewKey {
		test.Fatalf("rekey: %d", fake.rekeyCount)
	}

	// The limit for one command.
	session := fake.sessionAs(testToken, testPassphrase)
	session.automaticRekeyCount = automaticRekeyLimit
	opened := fake.openedItem(itemID)
	if _, err := session.rekeyItem(opened, nil, rekeyAutomatic); err == nil || !strings.Contains(err.Error(), "rekey") {
		test.Errorf("past the limit: %v", err)
	}
}

// forgetRememberedItems removes what the keys file remembers of items (the
// versions, who held keys, the agent's copy) and leaves the confirmed keys, as
// a computer that was only just given the person's confirmations would be.
func forgetRememberedItems(test *testing.T) {
	test.Helper()
	path := knownKeysFile(test)
	file, err := readPasswordsKnownKeysFile(path)
	if err != nil {
		test.Fatal(err)
	}
	file.Items = nil
	content, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		test.Fatal(err)
	}
	if err := writePrivateFile(path, content); err != nil {
		test.Fatal(err)
	}
}

// What a computer remembers of an item is written when it is first opened, and
// the version only goes up.
func TestPasswordsRemembersTheHighestRecordVersion(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	remembered := func() passwordsKnownItem {
		file, err := readPasswordsKnownKeysFile(knownKeysFile(test))
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
	if got := remembered(); got.HighestRecordVersion != 1 {
		test.Fatalf("after add: %+v", got)
	}
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if got := remembered(); got.HighestRecordVersion != 2 || !slices.Equal(got.KeyHolderUserIDs, []string{"user-ben"}) {
		test.Fatalf("after share: %+v", got)
	}
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	if got := remembered(); got.HighestRecordVersion != 3 || !got.HasAgentCopy {
		test.Fatalf("after agent on: %+v", got)
	}
	fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	if got := remembered(); got.HighestRecordVersion != 4 || len(got.KeyHolderUserIDs) != 0 || !got.HasAgentCopy {
		test.Fatalf("after unshare: %+v", got)
	}
	fake.mustRun(testToken, "", "agent", "off", "example.com")
	if got := remembered(); got.HighestRecordVersion != 5 || got.HasAgentCopy {
		test.Fatalf("after agent off: %+v", got)
	}
	// Edits and reads leave the record as it is.
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	fake.mustRun(testToken, "", "list")
	if got := remembered(); got.HighestRecordVersion != 5 {
		test.Fatalf("after edit: %+v", got)
	}
}

// The keys SetPasswordItemShares carries name the item key they were sealed
// from, and the server refuses them when the item moved on.
func TestPasswordsShareKeysCarryTheItemKey(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	item := fake.items[firstItemID(fake)]
	key := item.keys["user-ben"]
	if key == nil || key.ItemKey == "" || key.ItemKey != item.itemKey {
		test.Fatalf("the stored share key does not name the item key: %+v", key)
	}
	// The input type always writes it.
	encoded, err := json.Marshal((&client.PasswordItemShareKeyInput{RecipientUserID: "user-ben", ItemKey: "sealed"}).ForShare())
	if err != nil || !strings.Contains(string(encoded), `"itemKey":"sealed"`) {
		test.Errorf("ForShare: %s %v", encoded, err)
	}
}

// A key the server shows for an owner that differs from the one used before is
// accepted whichever it is, an older one shown again included: it is recorded
// as the key for the owner, with a line each time saying it changed.
func TestPasswordsAnOwnerKeyThatDiffersIsAcceptedEachTime(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	adaKey := fake.users[testToken].setup.PublicKey
	fake.unlockAs(benToken)

	oldPair, _ := passwords.GenerateKeyPair()
	oldKey := base64.StdEncoding.EncodeToString(oldPair.PublicKey())
	session := fake.sessionAs(benToken, benPassphrase)
	known, err := session.knownKeys()
	if err != nil {
		test.Fatal(err)
	}
	// Ben once saw Ada's old key; Ada then set Passwords up again.
	if _, err := known.rememberKey("user-ada", "Ada", "Ada", oldKey); err != nil {
		test.Fatal(err)
	}
	notices.forget()
	if len(fake.list(benToken, "")) != 1 {
		test.Fatal("the item was not read with the owner's new key")
	}
	if record := knownRecord(test, "user-ada"); record.PublicKey != adaKey {
		test.Fatalf("the new key was not recorded: %+v", record)
	}
	if lines := notices.lines(); len(lines) != 1 || !strings.Contains(lines[0], "Ada's key changed") {
		test.Fatalf("notices: %q", lines)
	}

	// The server now presents an item whose owner key is the old one: it is
	// accepted as the key for the owner, and said again.
	notices.forget()
	fresh := fake.sessionAs(benToken, benPassphrase)
	view := &client.PasswordItemView{OwnerUserID: "user-ada", OwnerName: "Ada", OwnerPublicKey: oldKey}
	if _, err := fresh.checkedOwnerKey(view); err != nil {
		test.Fatalf("an owner's key that differs was refused: %v", err)
	}
	fresh.recordOwnerKey(view)
	if record := knownRecord(test, "user-ada"); record.PublicKey != oldKey {
		test.Fatalf("the key shown last was not recorded: %+v", record)
	}
	if lines := notices.lines(); len(lines) != 1 || !strings.Contains(lines[0], "Ada's key changed") {
		test.Fatalf("notices: %q", lines)
	}
}

// The agent flag shown is the server's flag and the owner's signed record
// together.
func TestPasswordsAgentFlagNeedsTheSignedRecordToo(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "never-for-the-agent-example")
	item := fake.items[firstItemID(fake)]
	item.isUsableByAgent = true
	if shown := fake.list(testToken, ""); len(shown) != 1 || shown[0].IsUsableByAgent {
		test.Fatalf("the server's flag alone was shown: %+v", shown)
	}
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	if shown := fake.list(testToken, ""); len(shown) != 1 || !shown[0].IsUsableByAgent {
		test.Fatalf("both agree and it was not shown: %+v", shown)
	}
	// The record says yes but the server says no: not shown either.
	item.isUsableByAgent = false
	if shown := fake.list(testToken, ""); len(shown) != 1 || shown[0].IsUsableByAgent {
		test.Fatalf("the record alone was shown: %+v", shown)
	}
}

// delete --forever needs the person's tap, which a command line never has: it
// is refused at once, before the passphrase, and says where to do it. When the
// server accepts it, only a deleted item is removed.
func TestPasswordsDeleteForeverNeedsATap(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "delete", "example.com")
	fake.mustRun(testToken, "", "lock")

	fake.isTapRefused = true
	listedBefore := fake.operationCount("ListPasswordItems")
	_, err := fake.run(testToken, "", "delete", itemID, "--forever", "--force")
	if err == nil || !strings.HasPrefix(err.Error(), "Removing an item for good") || !strings.Contains(err.Error(), "dashboard") || strings.Contains(err.Error(), "passphrase") {
		test.Fatalf("delete --forever without a tap: %v", err)
	}
	if fake.operationCount("ListPasswordItems") != listedBefore || fake.items[itemID] == nil {
		test.Fatal("the command went on to open Passwords or removed the item")
	}

	// A server that does accept it removes a deleted item and refuses a live one.
	fake.isTapRefused = false
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "alive.example.com", "--password", "secret-example")
	if _, err := fake.run(testToken, "", "delete", "alive.example.com", "--forever", "--force"); err == nil || !strings.Contains(err.Error(), "not deleted") {
		test.Errorf("a live item: %v", err)
	}
	output := fake.mustRun(testToken, "", "delete", itemID, "--forever", "--force")
	if fake.items[itemID] != nil || !strings.Contains(output, "removed example.com for good") {
		test.Fatalf("delete --forever: %q", output)
	}
	if len(fake.items) != 1 {
		test.Fatalf("items left: %d", len(fake.items))
	}
}
