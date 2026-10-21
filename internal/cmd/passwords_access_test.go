package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// lastIndex is the position of the last element equal to value, -1 if none.
func lastIndex(values []string, value string) int {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] == value {
			return index
		}
	}
	return -1
}

// agentFake is a fake server with an agent key, Ada and Ben set up, and the
// notices about keys recorded.
func agentFake(test *testing.T) (*fakePasswordsServer, *passwords.KeyPair, *keyNotices) {
	test.Helper()
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	agentKeyPair, err := passwords.GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	fake.agentPublicKey = base64.StdEncoding.EncodeToString(agentKeyPair.PublicKey())
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	questions := recordKeyNotices(test)
	fake.unlockAs(testToken)
	return fake, agentKeyPair, questions
}

// The sealed record inside the item follows every share and the agent's
// permission, and is written in the order that keeps it from being wider than
// the server: widening saves the fields first, narrowing asks the server first
// and then re-keys with the narrower record.
func TestPasswordsSealedRecordFollowsSharesAndAgent(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	// A new item starts with a signed record that shares it with nobody.
	if access := fake.openedItemAccess(testToken, testPassphrase, itemID); access == nil || access.IsUsableByAgent || len(access.Grantees) != 0 || access.RecordVersion != 1 || len(access.RecordMac) == 0 {
		test.Fatalf("a new item's record: %+v", access)
	}

	fake.mustRun(testToken, "", "share", "example.com", "ben")
	access := fake.openedItemAccess(testToken, testPassphrase, itemID)
	if access == nil || access.IsUsableByAgent || len(access.Grantees) != 1 || access.Grantees[0].UserID != "user-ben" {
		test.Fatalf("after share: %+v", access)
	}
	if lastIndex(fake.documents, "SavePasswordItem") > lastIndex(fake.documents, "SetPasswordItemShares") {
		test.Errorf("sharing asked the server before the record was saved: %v", fake.documents)
	}

	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	access = fake.openedItemAccess(testToken, testPassphrase, itemID)
	if access == nil || !access.IsUsableByAgent || len(access.Grantees) != 1 {
		test.Fatalf("after agent on: %+v", access)
	}
	if lastIndex(fake.documents, "SavePasswordItem") > lastIndex(fake.documents, "SetPasswordItemUsableByAgent") {
		test.Errorf("the agent was allowed before the record was saved: %v", fake.documents)
	}

	// An edit keeps the record as it was.
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada")
	if edited := fake.openedItemAccess(testToken, testPassphrase, itemID); !slices.Equal(edited.Grantees, access.Grantees) || !edited.IsUsableByAgent {
		test.Fatalf("an edit changed the record: %+v", edited)
	}

	// Narrowing: the server first, then the re-key with the narrower record,
	// which keeps the agent's copy because the record still lets it.
	fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	if lastIndex(fake.documents, "SetPasswordItemShares") > lastIndex(fake.documents, "RekeyPasswordItem") {
		test.Errorf("unshare re-keyed before the server stopped sharing: %v", fake.documents)
	}
	access = fake.openedItemAccess(testToken, testPassphrase, itemID)
	if access == nil || !access.IsUsableByAgent || len(access.Grantees) != 0 {
		test.Fatalf("after unshare: %+v", access)
	}
	item := fake.items[itemID]
	if !item.isUsableByAgent || item.itemKeyByAgentKey == "" || item.needsNewKey {
		test.Fatalf("after unshare the agent lost its copy or the item still needs a key: %+v", item)
	}

	fake.mustRun(testToken, "", "agent", "off", "example.com")
	access = fake.openedItemAccess(testToken, testPassphrase, itemID)
	if access == nil || access.IsUsableByAgent {
		test.Fatalf("after agent off: %+v", access)
	}
	if item.isUsableByAgent || item.itemKeyByAgentKey != "" {
		test.Fatal("the agent kept a copy after agent off")
	}
}

// A server that says an item is usable by the agent when the person never
// allowed it gets no copy of the new key at a re-key: the owner's own record is
// what decides.
func TestPasswordsHostileServerAgentFlag(test *testing.T) {
	fake, agentKeyPair, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "never-for-the-agent-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	item := fake.items[firstItemID(fake)]
	// The server flips the flag in its own database.
	item.isUsableByAgent = true
	fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	if item.itemKeyByAgentKey != "" || item.isUsableByAgent {
		test.Fatal("a copy of the new key was sealed for the agent key")
	}
	// Nothing the agent key holds opens the item.
	ownerPublicKey, _ := base64.StdEncoding.DecodeString(fake.userByID("user-ada").setup.PublicKey)
	if sealed, err := passwords.ParseSealedForKeyPair(item.itemKeyByAgentKey); err == nil {
		if _, err := agentKeyPair.Open(sealed, ownerPublicKey, passwords.ShareData(item.id, "user-ada", passwords.AgentRecipientID)); err == nil {
			test.Fatal("the agent key opened the item")
		}
	}
}

// A server that lists an item shared with nobody as waiting for a person gets
// nothing sealed for them, and no key of theirs is recorded.
func TestPasswordsHostileServerForgedWaiting(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.mustRun(testToken, "", "add", "shared", "--password", "ok-to-share-example")
	fake.mustRun(testToken, "", "add", "private", "--password", "ada-only-example")
	fake.mustRun(testToken, "", "share", "shared", "ben")
	var privateID string
	for id, item := range fake.items {
		if len(item.grantees) == 0 {
			privateID = id
		}
	}
	notices.forget()
	fake.forgedWaiting = map[string]string{privateID: "user-ben"}
	fake.mustRun(testToken, "", "share", "--waiting")
	if notices.text.Len() != 0 {
		test.Errorf("a key was recorded for a forged waiting person: %q", notices.text.String())
	}
	if fake.items[privateID].keys["user-ben"] != nil {
		test.Fatal("the private item's key was sealed for Ben")
	}
}

// A person the server lists on an item but the sealed record does not reach is
// given no key at a re-key either.
func TestPasswordsRekeySealsOnlyForTheSealedRecord(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.addCora()
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	// The server adds Cora to the shares on its own, with a key it made.
	item := fake.items[firstItemID(fake)]
	coraUserID := "user-cora"
	item.grantees = append(item.grantees, &client.PasswordShareGranteeInput{GranteeUserID: &coraUserID})
	fake.mustRun(testToken, "", "rekey", "example.com")
	if item.keys["user-ben"] == nil {
		test.Error("Ben lost the item in a re-key")
	}
	if item.keys["user-cora"] != nil {
		test.Fatal("a key was sealed for somebody the record does not reach")
	}
}

// An item the server marks as needing a new key is told in list and show, is
// given one by rekey, and edit, share and agent do that first.
func TestPasswordsNeedsNewKey(test *testing.T) {
	// The wait between two re-keys nobody asked for is tested on its own.
	original := automaticRekeyWait
	automaticRekeyWait = 0
	test.Cleanup(func() { automaticRekeyWait = original })
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "share", "example.com", "ben")

	// Taking Ben off, with the new key failing, leaves the mark.
	fake.rekeyFailure = "the server is having a bad day"
	if _, err := fake.run(testToken, "", "unshare", "example.com", "ben"); err == nil {
		test.Fatal("the failed re-key was not told")
	}
	fake.rekeyFailure = ""
	item := fake.items[itemID]
	if !item.needsNewKey {
		test.Fatal("the fake did not mark the item")
	}
	wanted := "needs a new key: run teanode passwords rekey " + itemID
	for _, command := range [][]string{{"list"}, {"show", "example.com"}} {
		if output := fake.mustRun(testToken, "", command...); !strings.Contains(output, wanted) {
			test.Errorf("%v did not say %q:\n%s", command, wanted, output)
		}
	}
	if listed := fake.mustRun(testToken, "", "list", "--json"); !strings.Contains(listed, `"needsNewKey": true`) {
		test.Errorf("list --json: %s", listed)
	}

	// rekey clears it.
	output := fake.mustRun(testToken, "", "rekey", "example.com")
	if item.needsNewKey || fake.rekeyCount != 1 || !strings.Contains(output, "has a new key") {
		test.Fatalf("rekey: %q marked=%v count=%d", output, item.needsNewKey, fake.rekeyCount)
	}
	if output := fake.mustRun(testToken, "", "list"); strings.Contains(output, "needs a new key") {
		test.Errorf("list still says it:\n%s", output)
	}

	// edit, share and agent on re-key first when the server says so.
	for _, command := range [][]string{
		{"edit", "example.com", "--user-name", "ada"},
		{"share", "example.com", "ben"},
		{"agent", "on", "example.com", "--force"},
		{"agent", "off", "example.com"},
	} {
		before := fake.rekeyCount
		item.needsNewKey = true
		fake.mustRun(testToken, "", command...)
		if fake.rekeyCount <= before || item.needsNewKey {
			test.Errorf("%v did not re-key first (re-keys %d to %d, marked %v)", command, before, fake.rekeyCount, item.needsNewKey)
		}
	}
	// The edit did change the user name, on top of the new key.
	if shown := fake.mustRun(testToken, "", "show", "example.com", "--json"); !strings.Contains(shown, `"userName": "ada"`) {
		test.Errorf("the edit was lost:\n%s", shown)
	}
}

// code prints the person's own safety code, worked out from the key pair that
// unlocking opened.
func TestPasswordsCodeIsOwnSafetyCode(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.unlockAs(testToken)
	publicKey, _ := base64.StdEncoding.DecodeString(fake.users[testToken].setup.PublicKey)
	wanted := passwords.SafetyCode(publicKey)

	if output := fake.mustRun(testToken, "", "code"); !strings.Contains(output, wanted) {
		test.Errorf("code printed:\n%s\nwanted %s", output, wanted)
	}
	var printed struct {
		Username   string `json:"username"`
		SafetyCode string `json:"safetyCode"`
	}
	if err := json.Unmarshal([]byte(fake.mustRun(testToken, "", "code", "--json")), &printed); err != nil || printed.SafetyCode != wanted || printed.Username != "ada" {
		test.Errorf("code --json: %+v %v", printed, err)
	}
	// A server that reports another public key for the person changes nothing:
	// the code is from the key that was opened.
	otherPair, _ := passwords.GenerateKeyPair()
	fake.users[testToken].setup.PublicKey = base64.StdEncoding.EncodeToString(otherPair.PublicKey())
	if output := fake.mustRun(testToken, "", "code"); !strings.Contains(output, wanted) {
		test.Errorf("code followed the server's key:\n%s", output)
	}
}

// knownRecordIfAny is knownRecord, but nil when no known keys file was written.
func knownRecordIfAny(test *testing.T, userID string) *passwordsKnownKey {
	test.Helper()
	files, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", "passwords-keys-*.json"))
	if len(files) == 0 {
		return nil
	}
	return knownRecord(test, userID)
}

// runAgainst is fake.run, but against another URL (a server that lies).
func (self *fakePasswordsServer) runAgainst(url, token, standardInput string, arguments ...string) (string, error) {
	root := &cli.Command{
		Name:   "fixture",
		Reader: strings.NewReader(standardInput),
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: url}, &cli.StringFlag{Name: "token", Value: token},
		},
		Commands: []*cli.Command{NewPasswordsCommand()},
	}
	var err error
	output := captureStandardOutput(self.test, func() {
		err = root.Run(self.test.Context(), append([]string{"fixture", "passwords"}, arguments...))
	})
	return output, err
}

// trust never says what code it expected, and refuses a server that answers
// with another person than the one asked for, whatever code is given.
func TestPasswordsTrustRefusesAnotherPersonAndKeepsTheCodeToItself(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)
	benKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	realCode := passwords.SafetyCode(benKey)

	// A wrong code: nothing recorded, and the right one is not in the answer.
	_, err := fake.run(testToken, "", "trust", "ben", zeroCode)
	if err == nil || strings.Contains(err.Error(), realCode) || strings.Contains(strings.ReplaceAll(err.Error(), " ", ""), strings.ReplaceAll(realCode, " ", "")) {
		test.Fatalf("a wrong code was answered with the right one: %v", err)
	}

	// A server that answers the lookup with another account.
	liar := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		forwarded, _ := http.NewRequest(request.Method, fake.server.URL+request.URL.Path, bytes.NewReader(body))
		forwarded.Header = request.Header.Clone()
		upstream, err := http.DefaultClient.Do(forwarded)
		if err != nil {
			test.Error(err)
			return
		}
		defer func() { _ = upstream.Body.Close() }()
		answer, _ := io.ReadAll(upstream.Body)
		if bytes.Contains(body, []byte("ListPasswordRecipients")) {
			answer = bytes.ReplaceAll(answer, []byte(`"userId":"user-ben"`), []byte(`"userId":"user-mallory"`))
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(answer)
	}))
	defer liar.Close()
	if _, err := fake.runAgainst(liar.URL, testToken, "", "trust", "ben", realCode); err == nil || !strings.Contains(err.Error(), "different person") {
		test.Fatalf("trust against a server that answers with another user id: %v", err)
	}
	if record := knownRecordIfAny(test, "user-mallory"); record != nil {
		test.Fatalf("a key was recorded under the account the server named: %+v", record)
	}
	// The same for a user name that is not the one typed.
	liarName := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		forwarded, _ := http.NewRequest(request.Method, fake.server.URL+request.URL.Path, bytes.NewReader(body))
		forwarded.Header = request.Header.Clone()
		upstream, err := http.DefaultClient.Do(forwarded)
		if err != nil {
			test.Error(err)
			return
		}
		defer func() { _ = upstream.Body.Close() }()
		answer, _ := io.ReadAll(upstream.Body)
		if bytes.Contains(body, []byte("ListPasswordRecipients")) {
			answer = bytes.ReplaceAll(answer, []byte(`"username":"ben"`), []byte(`"username":"mallory"`))
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(answer)
	}))
	defer liarName.Close()
	if _, err := fake.runAgainst(liarName.URL, testToken, "", "trust", "ben", realCode); err == nil || !strings.Contains(err.Error(), "different person") {
		test.Fatalf("trust against a server that answers with another user name: %v", err)
	}
	if record := knownRecordIfAny(test, "user-ben"); record != nil && record.ComparedAt != nil {
		test.Fatal("a refused trust recorded a key")
	}
	// The honest server, the right code, and the name typed in any case.
	output := fake.mustRun(testToken, "", "trust", "Ben", strings.ReplaceAll(realCode, " ", ""))
	if !strings.Contains(output, `marked the key of Ben as compared`) {
		test.Errorf("trust said %q", output)
	}
}

// A display name that carries a safety code cannot pass for the line about a
// key: the line names the user name the person typed, never the server's
// display name, and the key sealed for is the one the server handed out.
func TestPasswordsKeyNoticeCannotCarryAServerName(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	realKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	realCode := passwords.SafetyCode(realKey)

	// The server hands out another key for ben, and a display name that carries
	// ben's real code, a line break and a name with invisible characters.
	otherPair, _ := passwords.GenerateKeyPair()
	fake.users[benToken].setup.PublicKey = base64.StdEncoding.EncodeToString(otherPair.PublicKey())
	fake.users[benToken].name = "Ben (ben)  safety code " + realCode + "\n  Cora (cora)  safety code " + realCode + "\u202e\u200b"

	notices := recordKeyNotices(test)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "first key for ben recorded" {
		test.Fatalf("notices: %q", lines)
	}
	if record := knownRecord(test, "user-ben"); record == nil || record.PublicKey != fake.users[benToken].setup.PublicKey {
		test.Errorf("the key the server handed out was not recorded: %+v", record)
	}
}

// A key that changed is accepted: the item is sealed for the new key, and one
// line says the key changed and to compare codes. Nothing waits for an answer.
func TestPasswordsAChangedKeyIsAcceptedAndSaidOnce(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	fake.mustRun(testToken, "", "add", "second.example.com", "--password", "second-secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "first key for ben recorded" {
		test.Fatalf("notices: %q", lines)
	}
	otherPair, _ := passwords.GenerateKeyPair()
	newKey := base64.StdEncoding.EncodeToString(otherPair.PublicKey())
	fake.users[benToken].setup.PublicKey = newKey
	notices.forget()
	output := fake.mustRun(testToken, "", "share", "second.example.com", "ben")
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "ben's key changed (they set Passwords up again): compare codes with them; your code: teanode passwords code" {
		test.Fatalf("notices: %q", lines)
	}
	if !strings.Contains(output, "can open it") {
		test.Errorf("sharing said %q", output)
	}
	if record := knownRecord(test, "user-ben"); record == nil || record.PublicKey != newKey || record.ComparedAt != nil {
		test.Errorf("the new key was not recorded: %+v", record)
	}
	for _, item := range fake.items {
		if item.keys["user-ben"] == nil {
			test.Errorf("nothing was sealed for Ben on %s", item.id)
		}
	}
	// Said once: the same key again prints nothing.
	notices.forget()
	fake.mustRun(testToken, "", "share", "--waiting")
	if notices.text.Len() != 0 {
		test.Errorf("the change was said again: %q", notices.text.String())
	}
}

// A keys file with a bad mode does not block taking access away: the
// narrowing still happens, the people the item is still shared with wait, and
// the message says how to fix the mode.
func TestPasswordsBadKeysFileDoesNotBlockNarrowing(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.addCora()
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	fake.mustRun(testToken, "", "share", "example.com", "cora")
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	item := fake.items[firstItemID(fake)]

	path := knownKeysFile(test)
	if err := os.Chmod(path, 0o644); err != nil {
		test.Fatal(err)
	}
	if _, err := readPasswordsKnownKeysFile(path); err == nil || !strings.Contains(err.Error(), "chmod 600 "+path) {
		test.Errorf("the advice for a bad mode: %v", err)
	}

	output := fake.mustRun(testToken, "", "agent", "off", "example.com")
	if item.isUsableByAgent || item.itemKeyByAgentKey != "" {
		test.Fatal("the agent still has a copy after agent off")
	}
	if item.keys["user-ben"] != nil || item.keys["user-cora"] != nil || !strings.Contains(notices.text.String(), "needs you: teanode passwords rekey "+item.id) {
		test.Errorf("the people still shared with should wait for the person:\n%s\n%s\nkeys: %v", output, notices.text.String(), item.keys)
	}

	fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	if len(item.grantees) != 1 {
		test.Fatalf("grantees after unshare: %d", len(item.grantees))
	}
	if item.needsNewKey {
		test.Error("the item still needs a new key")
	}
}

// Sharing with somebody new from a command line is refused before the
// passphrase is asked for, as the agent is.
func TestPasswordsShareAsksForTheTapFirst(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, benPassphrase)
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	fake.mustRun(testToken, "", "lock")
	fake.isTapRefused = true
	listedBefore := fake.operationCount("ListPasswordItems")
	_, err := fake.run(testToken, "", "share", "example.com", "ben")
	if err == nil || !strings.Contains(err.Error(), "done on the Passwords page") || strings.Contains(err.Error(), "passphrase") {
		test.Fatalf("share without a tap: %v", err)
	}
	if fake.operationCount("ListPasswordItems") != listedBefore || fake.operationCount("UnlockPasswords") > 4 {
		test.Error("the command went on to open Passwords")
	}
}

// An owner's key is written down only after an item from them opened with it.
// When the key differs from the one used before, the item opens with the new
// key, which replaces the old one, with one line to compare codes. Nobody is
// asked.
func TestPasswordsOwnerKeyIsRecordedAfterOpeningAndAChangeIsAccepted(test *testing.T) {
	fake, _, notices := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	itemID := firstItemID(fake)
	adaKey := fake.users[testToken].setup.PublicKey

	// A key that does not open the item is not written down.
	genuine := fake.items[itemID].keys["user-ben"].ItemKeyByRecipientKey
	fake.items[itemID].keys["user-ben"].ItemKeyByRecipientKey = "not a sealed key"
	fake.unlockAs(benToken)
	if shared := fake.list(benToken, ""); len(shared) != 0 {
		test.Fatalf("an item with a broken key was listed: %+v", shared)
	}
	if record := knownRecord(test, "user-ada"); record != nil && record.PublicKey != "" {
		test.Fatalf("a key that opened nothing was written down: %+v", record)
	}
	notices.forget()
	fake.items[itemID].keys["user-ben"].ItemKeyByRecipientKey = genuine
	if len(fake.list(benToken, "")) != 1 {
		test.Fatal("the genuine key stopped opening")
	}
	if record := knownRecord(test, "user-ada"); record == nil || record.PublicKey != adaKey {
		test.Fatalf("the owner's key after opening: %+v", record)
	}
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "first key for Ada recorded" {
		test.Fatalf("notices: %q", lines)
	}

	// The key Ben used for Ada was another one: the server now shows Ada's real
	// key. The item opens with it and the key is replaced, with one line.
	otherPair, _ := passwords.GenerateKeyPair()
	otherKey := base64.StdEncoding.EncodeToString(otherPair.PublicKey())
	session := fake.sessionAs(benToken, benPassphrase)
	known, err := session.knownKeys()
	if err != nil {
		test.Fatal(err)
	}
	if _, err := known.rememberKey("user-ada", "Ada", "Ada", otherKey); err != nil {
		test.Fatal(err)
	}
	notices.forget()
	if len(fake.list(benToken, "")) != 1 {
		test.Fatal("the item with the owner's changed key was not read")
	}
	if record := knownRecord(test, "user-ada"); record.PublicKey != adaKey {
		test.Fatalf("the changed key was not recorded: %+v", record)
	}
	if lines := notices.lines(); len(lines) != 1 || lines[0] != "Ada's key changed (they set Passwords up again): compare codes with them; your code: teanode passwords code" {
		test.Fatalf("notices: %q", lines)
	}
}

// Circle members are the server's word; a person reached through a circle the
// sealed record names is reached, and one it does not name is not.
func TestPasswordsCircleReach(test *testing.T) {
	circleID, otherCircleID := "circle-one", "circle-two"
	shares := []*client.PasswordShareView{
		{GranteeCircleID: &circleID, Recipients: []*client.PasswordRecipientView{{UserID: "user-ben"}, {UserID: "user-cora"}}},
		{GranteeCircleID: &otherCircleID, Recipients: []*client.PasswordRecipientView{{UserID: "user-cora"}}},
	}
	circleIDs := circleIDsOfRecipients(shares)
	access := &passwords.PasswordAccess{Grantees: []passwords.PasswordGrantee{{CircleID: circleID}}}
	for userID, wanted := range map[string]bool{"user-ben": true, "user-cora": true, "user-dan": false} {
		if got := passwords.IsReachedByAccess(access, userID, circleIDs[userID]); got != wanted {
			test.Errorf("%s reached: %v, wanted %v", userID, got, wanted)
		}
	}
	narrowed := withoutGrantee(access, "", circleID)
	if passwords.IsReachedByAccess(narrowed, "user-ben", circleIDs["user-ben"]) {
		test.Error("removing the circle from the record still reaches its members")
	}
	if widened := withGrantee(narrowed, "user-dan", ""); !passwords.IsReachedByAccess(widened, "user-dan", nil) || len(narrowed.Grantees) != 0 {
		test.Error("adding a person to the record")
	}
}
