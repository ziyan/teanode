package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

const (
	testPassphrase = "correct horse battery staple"
	testToken      = "token-ada"
	benToken       = "token-ben"
)

// run is a command that must succeed.
func (self *fakePasswordsServer) mustRun(token, standardInput string, arguments ...string) string {
	self.test.Helper()
	output, err := self.run(token, standardInput, arguments...)
	if err != nil {
		self.test.Fatalf("teanode passwords %s: %s", strings.Join(arguments, " "), err)
	}
	return output
}

// setUpAs sets Passwords up for the token's person and locks it again.
func (self *fakePasswordsServer) setUpAs(token, passphrase string) string {
	self.test.Helper()
	output := self.mustRun(token, passphrase+"\n", "setup", "--passphrase-stdin", "--json")
	var made struct {
		RecoveryKey string `json:"recoveryKey"`
	}
	if err := json.Unmarshal([]byte(output), &made); err != nil {
		self.test.Fatalf("setup output: %s: %s", err, output)
	}
	self.mustRun(token, "", "lock")
	return made.RecoveryKey
}

func useConfigurationDirectory(test *testing.T) {
	test.Helper()
	test.Setenv("XDG_CONFIG_HOME", test.TempDir())
	// The unlock file goes to the runtime directory when there is one, which
	// on a developer's machine is a real one.
	test.Setenv("XDG_RUNTIME_DIR", test.TempDir())
	original := clipboardLookPath
	clipboardLookPath = func(string) (string, error) { return "", errors.New("no clipboard in a test") }
	test.Cleanup(func() { clipboardLookPath = original })
}

type listedItem struct {
	ID              string   `json:"id"`
	ItemKind        string   `json:"itemKind"`
	Name            string   `json:"name"`
	UserName        string   `json:"userName"`
	Sites           []string `json:"sites"`
	Folder          string   `json:"folder"`
	IsShared        bool     `json:"isShared"`
	IsSharedWithMe  bool     `json:"isSharedWithMe"`
	IsUsableByAgent bool     `json:"isUsableByAgent"`
}

func (self *fakePasswordsServer) list(token, standardInput string, arguments ...string) []listedItem {
	self.test.Helper()
	// --passphrase-stdin always reads a line, so it is given only when there is
	// one: an unlocked list has none.
	flags := []string{"list", "--json"}
	if standardInput != "" {
		flags = append(flags, "--passphrase-stdin")
	}
	output := self.mustRun(token, standardInput, append(flags, arguments...)...)
	var items []listedItem
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		self.test.Fatalf("list output: %s: %s", err, output)
	}
	return items
}

// Setting up makes the keys on this computer and sends only sealed values, the
// recovery key opens what the passphrase opens, and a wrong passphrase is the
// server's refusal.
func TestPasswordsSetupAndUnlock(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)

	text := fake.mustRun(testToken, testPassphrase+"\n", "setup", "--passphrase-stdin")
	recoveryKey := ""
	for _, line := range strings.Split(text, "\n") {
		if candidate := strings.TrimSpace(line); len(candidate) == 64 && strings.Count(candidate, " ") == 12 {
			recoveryKey = candidate
		}
	}
	if _, err := passwords.ParseRecoveryKey(recoveryKey); err != nil {
		test.Fatalf("setup printed no recovery key:\n%s", text)
	}
	if !strings.Contains(text, "safe") {
		test.Errorf("setup does not say to keep the recovery key somewhere safe:\n%s", text)
	}

	// What the server holds is sealed values, a salt and hashes, and neither
	// the passphrase nor the recovery key.
	stored := fake.users[testToken].setup
	for _, sealed := range []string{stored.AccountKeyByPassphrase, stored.AccountKeyByRecoveryKey, stored.PrivateKeyByAccountKey} {
		if _, err := passwords.ParseSealed(sealed); err != nil {
			test.Errorf("a sealed value the server holds is not sealed: %s", err)
		}
	}
	serverText, _ := json.Marshal(stored)
	if strings.Contains(string(serverText), testPassphrase) || strings.Contains(string(serverText), strings.ReplaceAll(recoveryKey, " ", "")) {
		test.Error("the server was sent a secret in the clear")
	}
	if stored.PassphraseIterationCount < passwords.MinimumIterationCount {
		test.Errorf("iteration count %d", stored.PassphraseIterationCount)
	}

	// Setting up again is refused before a passphrase is asked for.
	if _, err := fake.run(testToken, testPassphrase+"\n", "setup", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), "already set Passwords up") {
		test.Errorf("a second setup: %v", err)
	}

	// Setup leaves it unlocked for the lock time; lock removes that.
	if len(fake.list(testToken, "")) != 0 {
		test.Error("a new Passwords has items")
	}
	fake.mustRun(testToken, "", "lock")
	if _, err := fake.run(testToken, "wrong passphrase\n", "list", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), "That passphrase is not right.") {
		test.Errorf("a wrong passphrase: %v", err)
	}
	if output := fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin"); !strings.Contains(output, "unlocked until") {
		test.Errorf("unlock said %q", output)
	}
	fake.mustRun(testToken, "", "lock")
	if output := fake.mustRun(testToken, recoveryKey+"\n", "unlock", "--recovery-key", "--passphrase-stdin"); !strings.Contains(output, "unlocked until") {
		test.Errorf("unlock with the recovery key said %q", output)
	}
	if _, err := fake.run(testToken, "AAAA\n", "unlock", "--recovery-key", "--passphrase-stdin"); err == nil {
		test.Error("a made-up recovery key unlocked")
	}
}

// The server's refusal of setup for want of a tap is shown with where to go,
// and before a passphrase is asked for: with nothing on standard input and no
// --passphrase-stdin, a command that asked first would complain it has no
// terminal to type on.
func TestPasswordsSetupNeedsATap(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.isTapRefused = true
	for _, arguments := range [][]string{{"setup", "--passphrase-stdin"}, {"setup"}} {
		_, err := fake.run(testToken, testPassphrase+"\n", arguments...)
		if err == nil || !strings.Contains(err.Error(), "dashboard") || !strings.Contains(err.Error(), "iPhone app") || !strings.Contains(err.Error(), "unlock") {
			test.Fatalf("a refused setup (%v) said: %v", arguments, err)
		}
		// One sentence: the server's own refusal is not printed beside it.
		if strings.Count(err.Error(), "dashboard") != 1 || strings.Contains(err.Error(), api.TapRequiredPhrase) {
			test.Errorf("a refused setup (%v) said more than one sentence: %v", arguments, err)
		}
		if strings.Contains(err.Error(), "terminal") {
			test.Errorf("setup asked for a passphrase before it asked the server: %v", err)
		}
	}
	if fake.users[testToken].setup != nil {
		test.Fatal("a refused setup set something up")
	}

	// Where a tap is not needed, the probe is refused as the empty setup it is
	// and sets nothing up, and setup goes on to ask for the passphrase.
	fake.isTapRefused = false
	if _, err := fake.run(testToken, "", "setup"); err == nil || !strings.Contains(err.Error(), "terminal") {
		test.Errorf("setup without a tap-free refusal did not go on to the passphrase: %v", err)
	}
	if fake.users[testToken].setup != nil {
		test.Fatal("the probe set Passwords up")
	}
	// A passphrase that is too short is refused before anything is sent.
	if _, err := fake.run(testToken, "short one\n", "setup", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), "at least 12 characters") {
		test.Errorf("a short passphrase: %v", err)
	}
	if fake.users[testToken].setup != nil {
		test.Fatal("a short passphrase set Passwords up")
	}
}

// unlockFilesIn lists the unlock files in a directory.
func unlockFilesIn(directory string) []string {
	files, _ := filepath.Glob(filepath.Join(directory, "passwords-unlock-*.json"))
	return files
}

// An unlock lasts until its time, is a file only the owner reads in the
// runtime directory, and is gone after lock.
func TestPasswordsUnlockFile(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, "", "settings", "--lock-after-minutes", "5")
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	runtimeDirectory := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "teanode")
	files := unlockFilesIn(runtimeDirectory)
	if len(files) != 1 {
		test.Fatalf("unlock files: %v", files)
	}
	if left := unlockFilesIn(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode")); len(left) != 0 {
		test.Errorf("the keys were also left in the configuration directory: %v", left)
	}
	information, _ := os.Stat(files[0])
	if information.Mode().Perm() != 0o600 {
		test.Errorf("the unlock file is %v", information.Mode().Perm())
	}
	var kept passwordsUnlockFile
	content, _ := os.ReadFile(files[0])
	if err := json.Unmarshal(content, &kept); err != nil {
		test.Fatal(err)
	}
	if remaining := time.Until(kept.ExpiresAt); remaining < 4*time.Minute || remaining > 5*time.Minute+time.Second {
		test.Errorf("the lock time is %s away, not 5 minutes", remaining)
	}

	// Commands use it without asking: nothing is on standard input.
	askedBefore := fake.operationCount("UnlockPasswords")
	fake.list(testToken, "")
	if fake.operationCount("UnlockPasswords") != askedBefore {
		test.Error("a command asked the server to unlock though Passwords was unlocked here")
	}

	// A file that others could read is not used, and says so.
	if err := os.Chmod(files[0], 0o644); err != nil {
		test.Fatal(err)
	}
	if _, err := fake.run(testToken, "", "list", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), "can be read or changed by other people") {
		test.Errorf("a world-readable unlock file: %v", err)
	}
	if err := os.Chmod(files[0], 0o600); err != nil {
		test.Fatal(err)
	}
	// So is a link to one.
	linked := filepath.Join(test.TempDir(), "elsewhere.json")
	if err := os.Rename(files[0], linked); err != nil {
		test.Fatal(err)
	}
	if err := os.Symlink(linked, files[0]); err != nil {
		test.Fatal(err)
	}
	if _, err := fake.run(testToken, "", "list", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		test.Errorf("a linked unlock file: %v", err)
	}
	if err := os.Remove(files[0]); err != nil {
		test.Fatal(err)
	}
	if err := os.Rename(linked, files[0]); err != nil {
		test.Fatal(err)
	}

	// Past its time it is removed and the passphrase is asked for.
	kept.ExpiresAt = time.Now().Add(-time.Second)
	content, _ = json.Marshal(kept)
	if err := os.WriteFile(files[0], content, 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := fake.run(testToken, "", "list", "--passphrase-stdin"); err == nil {
		test.Error("an expired unlock was used")
	}
	if _, err := os.Stat(files[0]); !errors.Is(err, os.ErrNotExist) {
		test.Error("an expired unlock file was left")
	}

	// A file that says it lasts longer than it ever may is removed too.
	kept.ExpiresAt = time.Now().Add(passwordsUnlockLifetimeMost + time.Hour)
	content, _ = json.Marshal(kept)
	if err := os.WriteFile(files[0], content, 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := fake.run(testToken, "", "list", "--passphrase-stdin"); err == nil {
		test.Error("an unlock that outlasts the longest was used")
	}
	if _, err := os.Stat(files[0]); !errors.Is(err, os.ErrNotExist) {
		test.Error("an unlock file that outlasts the longest was left")
	}
}

// However long a lock time is chosen, the keys are left in the runtime
// directory for 8 hours at most.
func TestPasswordsUnlockFileLifetime(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, "", "settings", "--lock-after-minutes", "10080")
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	files := unlockFilesIn(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "teanode"))
	if len(files) != 1 {
		test.Fatalf("unlock files: %v", files)
	}
	var kept passwordsUnlockFile
	content, _ := os.ReadFile(files[0])
	if err := json.Unmarshal(content, &kept); err != nil {
		test.Fatal(err)
	}
	if remaining := time.Until(kept.ExpiresAt); remaining > passwordsUnlockLifetimeMost || remaining < passwordsUnlockLifetimeMost-time.Minute {
		test.Errorf("a week of lock time left the keys for %s", remaining)
	}
	if output := fake.mustRun(testToken, "", "settings", "--json"); !strings.Contains(output, `"lockAfterMinutes": 10080`) {
		test.Errorf("the lock time on the server is still a week: %s", output)
	}
	fake.mustRun(testToken, "", "lock")
	if left := unlockFilesIn(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "teanode")); len(left) != 0 {
		test.Errorf("lock left %v", left)
	}
}

// Writing a private file leaves nothing but the file, whether it works or not.
func TestWritePrivateFileLeavesNoTemporaryFile(test *testing.T) {
	directory := test.TempDir()
	target := filepath.Join(directory, "keys.json")
	if err := writePrivateFile(target, []byte("first")); err != nil {
		test.Fatal(err)
	}
	if err := writePrivateFile(target, []byte("second")); err != nil {
		test.Fatal(err)
	}
	content, err := readPrivateFile(target)
	if err != nil || string(content) != "second" {
		test.Fatalf("read back %q, %v", content, err)
	}
	// Renaming a file over a directory fails after the temporary file was
	// written; it must not stay.
	blocked := filepath.Join(directory, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "inside"), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := writePrivateFile(blocked, []byte("never")); err == nil {
		test.Fatal("a file was written over a directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		test.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "keys.json" && entry.Name() != "blocked" {
			test.Errorf("a stray file was left: %s", entry.Name())
		}
	}
}

// A password is saved sealed, comes back the same, is hidden unless asked for,
// and a change based on an old revision is refused.
func TestPasswordsItemRoundTrip(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)

	// The passphrase is the first line of standard input, the password the second.
	fake.mustRun(testToken, testPassphrase+"\nhunter2-example\n", "add", "example.com", "--user-name", "ada@example.com",
		"--site", "https://example.com", "--password-stdin", "--passphrase-stdin", "--folder", "web",
		"--one-time-code", "JBSWY3DPEHPK3PXP")
	for _, item := range fake.items {
		if strings.Contains(item.encryptedFields, "hunter2") || strings.Contains(item.encryptedFields, "example.com") || strings.Contains(item.itemKey, "hunter2") {
			test.Fatal("the server holds a field in the clear")
		}
	}

	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	items := fake.list(testToken, "")
	if len(items) != 1 || items[0].Name != "example.com" || items[0].UserName != "ada@example.com" || items[0].Folder != "web" ||
		len(items[0].Sites) != 1 || items[0].Sites[0] != "https://example.com" || items[0].ItemKind != "login" {
		test.Fatalf("list: %+v", items)
	}
	// Found by a word of its site as well as by name.
	if len(fake.list(testToken, "", "example")) != 1 || len(fake.list(testToken, "", "nothing-like-it")) != 0 {
		test.Error("list [search]")
	}

	hidden := fake.mustRun(testToken, "", "show", "example.com")
	if strings.Contains(hidden, "hunter2") || !strings.Contains(hidden, "hidden") || !strings.Contains(hidden, "ada@example.com") {
		test.Errorf("show without --reveal:\n%s", hidden)
	}
	if !strings.Contains(hidden, "s left)") {
		test.Errorf("show has no one-time code with its seconds:\n%s", hidden)
	}
	revealed := fake.mustRun(testToken, "", "show", "example.com", "--reveal", "--json")
	if !strings.Contains(revealed, "hunter2-example") {
		test.Errorf("show --reveal:\n%s", revealed)
	}
	if json := fake.mustRun(testToken, "", "show", "example.com", "--json"); strings.Contains(json, "hunter2") {
		test.Errorf("show --json leaked the password:\n%s", json)
	}

	// With no clipboard program the value is printed.
	if output := fake.mustRun(testToken, "", "copy", "example.com"); strings.TrimSpace(output) != "hunter2-example" {
		test.Errorf("copy printed %q", output)
	}
	if output := fake.mustRun(testToken, "", "copy", "example.com", "--user-name"); strings.TrimSpace(output) != "ada@example.com" {
		test.Errorf("copy --user-name printed %q", output)
	}
	if output := fake.mustRun(testToken, "", "copy", "example.com", "--code"); len(strings.TrimSpace(output)) != 6 {
		test.Errorf("copy --code printed %q", output)
	}

	// An edit changes what it was given and keeps the rest.
	fake.mustRun(testToken, "", "edit", "example.com", "--user-name", "ada.l@example.com", "--add-site", "https://login.example.com")
	edited := fake.mustRun(testToken, "", "show", "example.com", "--reveal", "--json")
	for _, wanted := range []string{"ada.l@example.com", "https://login.example.com", "https://example.com", "hunter2-example"} {
		if !strings.Contains(edited, wanted) {
			test.Errorf("after edit, show has no %q:\n%s", wanted, edited)
		}
	}
	// A generated password replaces it, and never reaches the server in the clear.
	fake.mustRun(testToken, "", "edit", "example.com", "--generate", "--length", "30")
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "example.com")); len(output) != 30 {
		test.Errorf("generated password %q", output)
	}
	if _, err := fake.run(testToken, "", "edit", "example.com", "--text", "no such field"); err == nil || !strings.Contains(err.Error(), "not a field of a login") {
		test.Errorf("a note's flag on a login: %v", err)
	}

	// A change made from an old revision is refused, not merged over.
	session := fake.sessionAs(testToken, testPassphrase)
	loaded, err := session.loadItems(false)
	if err != nil || len(loaded) != 1 {
		test.Fatalf("loadItems: %v %d", err, len(loaded))
	}
	stale := loaded[0]
	fake.mustRun(testToken, "", "edit", "example.com", "--notes", "changed elsewhere")
	stale.Login.Notes = "changed here"
	if _, err := session.saveChangedItem(stale); err == nil || !strings.Contains(err.Error(), "changed on another device") {
		test.Errorf("a stale save: %v", err)
	}

	// Delete, list --deleted, restore.
	fake.mustRun(testToken, "", "delete", "example.com")
	if len(fake.list(testToken, "")) != 0 || len(fake.list(testToken, "", "--deleted")) != 1 {
		test.Error("delete did not move the item to recently deleted")
	}
	fake.mustRun(testToken, "", "restore", "example.com")
	if len(fake.list(testToken, "")) != 1 {
		test.Error("restore did not bring it back")
	}
}

// sessionAs opens Passwords the way a command does, from the unlock that
// `unlock` left, for a test that calls the functions under the commands.
func (self *fakePasswordsServer) sessionAs(token, passphrase string) *passwordsSession {
	self.test.Helper()
	self.mustRun(token, passphrase+"\n", "unlock", "--passphrase-stdin")
	var session *passwordsSession
	root := &cli.Command{
		Name: "fixture",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: self.server.URL}, &cli.StringFlag{Name: "token", Value: token},
		},
		Commands: []*cli.Command{{
			Name: "probe",
			Action: func(ctx context.Context, command *cli.Command) error {
				connection, err := openClient(command)
				if err != nil {
					return err
				}
				session, err = openPasswords(ctx, command, connection)
				return err
			},
		}},
	}
	if err := root.Run(self.test.Context(), []string{"fixture", "probe"}); err != nil {
		self.test.Fatal(err)
	}
	return session
}

// Cards, notes and every other kind of item go through the same path, and a
// field that belongs to another kind is refused.
func TestPasswordsCardsAndNotes(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "Travel card", "--kind", "card", "--cardholder-name", "Ada Example",
		"--number", "4111111111111111", "--security-code", "123", "--expiry-month", "09", "--expiry-year", "2030")
	fake.mustRun(testToken, "", "add", "Spare key", "--kind", "note", "--text", "under the blue pot")
	if _, err := fake.run(testToken, "", "add", "Bad", "--kind", "note", "--user-name", "x"); err == nil {
		test.Error("--user-name on a note")
	}

	shown := fake.mustRun(testToken, "", "show", "Travel card")
	if strings.Contains(shown, "4111111111111111") || !strings.Contains(shown, "ending 1111") || !strings.Contains(shown, "Ada Example") {
		test.Errorf("a card shown:\n%s", shown)
	}
	for _, line := range strings.Split(shown, "\n") {
		if strings.HasPrefix(line, "security code") && !strings.Contains(line, "hidden") {
			test.Errorf("the security code is shown: %s", line)
		}
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Travel card")); output != "4111111111111111" {
		test.Errorf("copy of a card: %q", output)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Travel card", "--security-code")); output != "123" {
		test.Errorf("copy of a security code: %q", output)
	}
	if output := fake.mustRun(testToken, "", "show", "Spare key"); strings.Contains(output, "blue pot") {
		test.Errorf("a note shown without --reveal:\n%s", output)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Spare key")); output != "under the blue pot" {
		test.Errorf("copy of a note: %q", output)
	}
	// A card cannot be used by the agent, said before anything is sent.
	if _, err := fake.run(testToken, "", "agent", "on", "Travel card", "--force"); err == nil || !strings.Contains(err.Error(), "card cannot be used by the agent") {
		test.Errorf("agent on for a card: %v", err)
	}
	if fake.operationCount("SetPasswordItemUsableByAgent") != 0 {
		test.Error("the server was asked to let the agent use a card")
	}
}

// A name that matches more than one item is not guessed at.
func TestPasswordsAmbiguousName(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--user-name", "ada", "--password", "one-example")
	fake.mustRun(testToken, "", "add", "example.com", "--user-name", "ben", "--password", "two-example")
	_, err := fake.run(testToken, "", "copy", "example.com")
	if err == nil || !strings.Contains(err.Error(), "matches 2 items") {
		test.Fatalf("an ambiguous name: %v", err)
	}
	items := fake.list(testToken, "")
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", items[0].ID)); output != "one-example" && output != "two-example" {
		test.Errorf("copy by id: %q", output)
	}
}

// Sharing seals the item key for the other person's public key and shows their
// safety code; they open it with their own key pair; a person who has not set
// up Passwords waits until they have; unsharing ends it.
func TestPasswordsShare(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--user-name", "ada", "--password", "shared-secret-example")

	// Ben has not set up Passwords: nothing can be sealed for him yet.
	output := fake.mustRun(testToken, "", "share", "example.com", "ben")
	if !strings.Contains(output, "has not set up Passwords yet") {
		test.Errorf("sharing with somebody who has not set up:\n%s", output)
	}
	fake.mustRun(testToken, "", "lock")

	fake.setUpAs(benToken, "ben passphrase example")
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	// Without --force and without a terminal the safety codes cannot be asked
	// about, so nothing is sealed.
	_, err := fake.run(testToken, "", "share", "example.com", "ben")
	if err == nil || !strings.Contains(err.Error(), "safety code") || !strings.Contains(err.Error(), "ben") {
		test.Fatalf("no safety code was shown before sharing: %v", err)
	}
	// The code shown is the code of the key Ben set up.
	benKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	if !strings.Contains(err.Error(), passwords.SafetyCode("user-ben", benKey)) {
		test.Errorf("the safety code of Ben's key %s is not in:\n%s", passwords.SafetyCode("user-ben", benKey), err)
	}
	if _, err := fake.run(testToken, "", "share", "--waiting"); err == nil || !strings.Contains(err.Error(), "safety code") {
		test.Errorf("sealing for the waiting without checking safety codes: %v", err)
	}
	if fake.items[firstItemID(fake)].keys["user-ben"] != nil {
		test.Fatal("a key was sealed for Ben before the safety code was confirmed")
	}
	// The person at a terminal compares the safety code and says yes.
	questions := recordKeyQuestions(test)
	questions.isYes = true
	fake.mustRun(testToken, "", "share", "--waiting")
	if fake.items[firstItemID(fake)].keys["user-ben"] == nil {
		test.Fatal("no key was sealed for Ben")
	}
	if len(questions.warnings) != 1 || !strings.Contains(questions.warnings[0], passwords.SafetyCode("user-ben", benKey)) {
		test.Errorf("the question about Ben's key: %q", questions.warnings)
	}
	fake.mustRun(testToken, "", "lock")

	// Ben opens it with his own key pair.
	fake.mustRun(benToken, "ben passphrase example\n", "unlock", "--passphrase-stdin")
	shared := fake.list(benToken, "")
	if len(shared) != 1 || !shared[0].IsSharedWithMe {
		test.Fatalf("Ben's list: %+v", shared)
	}
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "example.com")); output != "shared-secret-example" {
		test.Errorf("Ben copies %q", output)
	}
	// Only the owner changes it.
	if _, err := fake.run(benToken, "", "edit", "example.com", "--user-name", "ben"); err == nil || !strings.Contains(err.Error(), "only its owner") {
		test.Errorf("Ben edits: %v", err)
	}
	fake.mustRun(benToken, "", "lock")

	// The public key of Ben is written down, in a file only the owner reads,
	// once the safety code was confirmed; the file holds nothing secret.
	knownFiles, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "teanode", "passwords-keys-*.json"))
	if len(knownFiles) != 1 {
		test.Fatalf("known keys files: %v", knownFiles)
	}
	if information, err := os.Stat(knownFiles[0]); err != nil || information.Mode().Perm() != 0o600 {
		test.Errorf("the known keys file: %v %v", information, err)
	}
	if content, _ := os.ReadFile(knownFiles[0]); !strings.Contains(string(content), fake.users[benToken].setup.PublicKey) || strings.Contains(string(content), testPassphrase) {
		test.Errorf("the known keys file:\n%s", content)
	}

	// Unsharing ends it, and sends no keys.
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	if items := fake.list(testToken, ""); len(items) != 1 || !items[0].IsShared {
		test.Fatalf("the owner's list does not mark the share: %+v", items)
	}
	keysBefore := fake.items[firstItemID(fake)].keys["user-ben"]
	fake.mustRun(testToken, "", "unshare", "example.com", "ben")
	if fake.lastSetKeyCount != 0 {
		test.Errorf("unshare sent %d keys", fake.lastSetKeyCount)
	}
	fake.mustRun(testToken, "", "lock")
	fake.mustRun(benToken, "ben passphrase example\n", "unlock", "--passphrase-stdin")
	if len(fake.list(benToken, "")) != 0 {
		test.Error("Ben still reaches the item after unshare")
	}
	fake.mustRun(benToken, "", "lock")
	if keysBefore == nil {
		test.Fatal("Ben had no key before unshare")
	}

	// Ben's key is known now, so sharing again asks about nothing: it works
	// with no terminal and no --force.
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if fake.items[firstItemID(fake)].keys["user-ben"] == nil {
		test.Fatal("sharing with a person whose key is known sealed nothing")
	}
	// People who already have a key keep it: sharing again seals nothing.
	sentBefore := fake.lastSetKeyCount
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	if fake.lastSetKeyCount != 0 || sentBefore != 1 {
		test.Errorf("keys sent: first %d then %d, want 1 then 0", sentBefore, fake.lastSetKeyCount)
	}

	// Ben erases Passwords and sets it up again: a new key. Sealing for it is
	// shown with both safety codes and needs a yes, which no terminal gives.
	oldBenKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	fake.mustRun(benToken, "", "erase", "--force")
	fake.setUpAs(benToken, "ben passphrase example")
	newBenKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	// Both people use one profile here, so locking Ben locked Ada too.
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	questions.isYes = false
	_, err = fake.run(testToken, "", "share", "--waiting")
	if err == nil || !strings.Contains(err.Error(), "KEY CHANGED") || !strings.Contains(err.Error(), passwords.SafetyCode("user-ben", oldBenKey)) || !strings.Contains(err.Error(), passwords.SafetyCode("user-ben", newBenKey)) {
		test.Fatalf("a changed key was not shown with both safety codes:\n%v", err)
	}
	if key := fake.items[firstItemID(fake)].keys["user-ben"]; key.RecipientPublicKey == fake.users[benToken].setup.PublicKey {
		test.Fatal("a key was sealed for a key nobody confirmed")
	}
	questions.isYes = true
	fake.mustRun(testToken, "", "share", "--waiting")
	if key := fake.items[firstItemID(fake)].keys["user-ben"]; key.RecipientPublicKey != fake.users[benToken].setup.PublicKey {
		test.Fatal("a confirmed new key was not sealed for")
	}
}

// A key the server planted, sealed by anybody but the owner, does not open,
// and an owner whose key changed is not trusted without a yes.
func TestPasswordsSharedItemsOpenOnlyFromTheirOwner(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.setUpAs(benToken, "ben passphrase example")
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--password", "shared-secret-example")
	recordKeyQuestions(test).isYes = true
	fake.mustRun(testToken, "", "share", "example.com", "ben")
	itemID := firstItemID(fake)
	adaSession := fake.sessionAs(testToken, testPassphrase)
	items, err := adaSession.loadItems(false)
	if err != nil || len(items) != 1 {
		test.Fatalf("Ada's items: %v %d", err, len(items))
	}
	itemKey := items[0].ItemKey
	genuine := fake.items[itemID].keys["user-ben"].ItemKeyByRecipientKey

	fake.mustRun(benToken, "ben passphrase example\n", "unlock", "--passphrase-stdin")
	if len(fake.list(benToken, "")) != 1 {
		test.Fatal("Ben cannot open what Ada shared")
	}

	// The server seals the item key for Ben itself, with its own key pair.
	serverKeyPair, _ := passwords.GenerateKeyPair()
	benPublicKey, _ := base64.StdEncoding.DecodeString(fake.users[benToken].setup.PublicKey)
	planted, err := passwords.SealForKeyPair(serverKeyPair, benPublicKey, itemKey, passwords.ShareData(itemID, "user-ada", "user-ben"))
	if err != nil {
		test.Fatal(err)
	}
	fake.items[itemID].keys["user-ben"].ItemKeyByRecipientKey = planted.String()
	if shared := fake.list(benToken, ""); len(shared) != 0 {
		test.Fatalf("an item sealed by the server opened for Ben: %+v", shared)
	}
	fake.items[itemID].keys["user-ben"].ItemKeyByRecipientKey = genuine
	if len(fake.list(benToken, "")) != 1 {
		test.Fatal("the genuine key stopped opening")
	}

	// The owner's key is the one Ben saw; one that differs is not trusted
	// without a yes, which no terminal gives. Nothing is changed by refusing.
	benSession := fake.sessionAs(benToken, "ben passphrase example")
	view := &client.PasswordItemView{ID: itemID, OwnerUserID: "user-ada", OwnerName: "Ada", OwnerPublicKey: fake.users[testToken].setup.PublicKey, IsSharedWithMe: true}
	if _, err := benSession.checkedOwnerKey(view); err != nil {
		test.Fatalf("the owner's own key: %v", err)
	}
	otherKeyPair, _ := passwords.GenerateKeyPair()
	view.OwnerPublicKey = base64.StdEncoding.EncodeToString(otherKeyPair.PublicKey())
	if _, err := benSession.checkedOwnerKey(view); err == nil || !strings.Contains(err.Error(), "changed") {
		test.Fatalf("a changed owner key: %v", err)
	}
	view.OwnerPublicKey = "not a key"
	if _, err := benSession.checkedOwnerKey(view); err == nil {
		test.Fatal("an owner key that is not a key")
	}
}

// With --passphrase-stdin the first line of standard input is the passphrase
// even when an earlier `unlock` left the keys, so the line after it is the
// password or the note's text and never the passphrase stored as one.
func TestPasswordsPassphraseLineIsReadWhenUnlocked(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	fake.mustRun(testToken, testPassphrase+"\nunlocked-secret-example\n", "add", "unlocked.example.com",
		"--password-stdin", "--passphrase-stdin")
	fake.mustRun(testToken, testPassphrase+"\nthe note text example\n", "add", "Unlocked note",
		"--kind", "note", "--text-stdin", "--passphrase-stdin")
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "unlocked.example.com")); output != "unlocked-secret-example" {
		test.Errorf("the password stored was %q", output)
	}
	if output := fake.mustRun(testToken, "", "show", "Unlocked note", "--reveal", "--json"); !strings.Contains(output, "the note text example") || strings.Contains(output, testPassphrase) {
		test.Errorf("the note stored was:\n%s", output)
	}
	csv := "name,url,username,password,note\ncsv.example.com,https://csv.example.com,ada,csv-secret-example,\n"
	counts, _ := fake.importCounts(testPassphrase+"\n"+csv, "csv", "-", "--passphrase-stdin")
	if counts.FoundCount != 1 || counts.ImportedCount != 1 {
		test.Errorf("an import after the passphrase line: %+v", counts)
	}
	if output := fake.mustRun(testToken, "", "show", "csv.example.com", "--reveal", "--json"); !strings.Contains(output, "csv-secret-example") {
		test.Errorf("the imported password was not kept:\n%s", output)
	}
}

func firstItemID(fake *fakePasswordsServer) string {
	for id := range fake.items {
		return id
	}
	return ""
}

// Letting the agent use an item seals its key for the server's agent key, with
// the agent as recipient in the associated data, and turning it off clears it.
func TestPasswordsAgent(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	agentKeyPair, err := passwords.GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	fake.agentPublicKey = base64.StdEncoding.EncodeToString(agentKeyPair.PublicKey())
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--password", "agent-secret-example")

	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	item := fake.items[firstItemID(fake)]
	if !item.isUsableByAgent {
		test.Fatal("the agent may not use the item")
	}
	sealed, err := passwords.ParseSealedForKeyPair(item.itemKeyByAgentKey)
	if err != nil {
		test.Fatal(err)
	}
	// The agent opens it with the owner's public key, which is mixed into the
	// sealing, and the owner's id.
	ownerPublicKey, _ := base64.StdEncoding.DecodeString(fake.users[testToken].setup.PublicKey)
	itemKey, err := agentKeyPair.Open(sealed, ownerPublicKey, passwords.ShareData(item.id, "user-ada", passwords.AgentRecipientID))
	if err != nil || len(itemKey) != passwords.KeyLength {
		test.Fatalf("the agent key cannot open the item key: %v", err)
	}
	otherKeyPair, _ := passwords.GenerateKeyPair()
	if _, err := agentKeyPair.Open(sealed, otherKeyPair.PublicKey(), passwords.ShareData(item.id, "user-ada", passwords.AgentRecipientID)); err == nil {
		test.Error("the agent key opened an item key for the wrong owner key")
	}
	// The same key opens the fields, and only those.
	sealedFields, _ := passwords.ParseSealed(item.encryptedFields)
	plain, err := passwords.Open(itemKey, sealedFields, passwords.ItemFieldsData(item.id, passwords.ItemKindLogin))
	if err != nil || !strings.Contains(string(plain), "agent-secret-example") {
		test.Fatalf("the item key does not open the item: %v", err)
	}
	if listed := fake.list(testToken, ""); len(listed) != 1 || !listed[0].IsUsableByAgent {
		test.Errorf("list does not mark the agent: %+v", listed)
	}
	fake.mustRun(testToken, "", "agent", "off", "example.com")
	if item.isUsableByAgent || item.itemKeyByAgentKey != "" {
		test.Error("turning the agent off left its key")
	}

	// Without a tap the server refuses, and the words say where to go.
	fake.isTapRefused = true
	_, err = fake.run(testToken, "", "agent", "on", "example.com", "--force")
	if err == nil || !strings.Contains(err.Error(), "iPhone app") {
		test.Fatalf("a refused agent: %v", err)
	}
	if want := "Letting the agent use an item is done on the Passwords page of the dashboard or in the iPhone app; a command line cannot do this, but turning it off works here."; err.Error() != want {
		test.Errorf("a refused agent said %q, want %q", err.Error(), want)
	}
	// Locked, with nothing on standard input, it is still refused at once
	// rather than asking for a passphrase first; turning it off is not refused.
	fake.mustRun(testToken, "", "lock")
	_, err = fake.run(testToken, "", "agent", "on", "example.com", "--force")
	if err == nil || strings.Contains(err.Error(), "terminal") || !strings.Contains(err.Error(), "iPhone app") {
		test.Errorf("a refused agent, locked: %v", err)
	}
	fake.mustRun(testToken, testPassphrase+"\n", "agent", "off", "example.com", "--passphrase-stdin")
}

// Changing the passphrase, replacing the recovery key and erasing are refused
// for want of a tap before they ask for a passphrase or a yes, and change
// nothing.
func TestPasswordsTapOnlyCommandsRefuseAtOnce(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.isTapRefused = true
	for _, testCase := range []struct {
		arguments []string
		start     string
	}{
		{[]string{"change-passphrase"}, "Changing the passphrase"},
		{[]string{"change-passphrase", "--recovery-key"}, "Changing the passphrase"},
		{[]string{"new-recovery-key"}, "Replacing the recovery key"},
		{[]string{"erase"}, "Erasing Passwords"},
	} {
		// No terminal and no flag: asking for anything first would fail with
		// a complaint about the terminal instead.
		_, err := fake.run(testToken, "", testCase.arguments...)
		if err == nil || !strings.HasPrefix(err.Error(), testCase.start) {
			test.Errorf("%v said: %v", testCase.arguments, err)
			continue
		}
		if !strings.Contains(err.Error(), "a command line cannot do this") || strings.Count(err.Error(), "dashboard") != 1 || strings.Contains(err.Error(), api.TapRequiredPhrase) {
			test.Errorf("%v did not say it in one sentence: %v", testCase.arguments, err)
		}
		if strings.Contains(err.Error(), "terminal") {
			test.Errorf("%v asked before it asked the server: %v", testCase.arguments, err)
		}
	}
	if fake.users[testToken].setup == nil {
		test.Fatal("a refused erase erased Passwords")
	}
	// Where a tap is not needed they go on to ask.
	fake.isTapRefused = false
	if _, err := fake.run(testToken, "", "change-passphrase"); err == nil || !strings.Contains(err.Error(), "terminal") {
		test.Errorf("change-passphrase without the refusal did not go on to ask: %v", err)
	}
}

// A new passphrase opens the same items, and the old one no longer opens
// anything.
func TestPasswordsChangePassphraseAndRecoveryKey(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	oldRecoveryKey := fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--password", "still-here-example")
	fake.mustRun(testToken, "", "lock")

	fake.mustRun(testToken, testPassphrase+"\nnew passphrase example\n", "change-passphrase", "--passphrase-stdin")
	if _, err := fake.run(testToken, testPassphrase+"\n", "list", "--passphrase-stdin"); err == nil {
		test.Error("the old passphrase still opens Passwords")
	}
	fake.mustRun(testToken, "new passphrase example\n", "unlock", "--passphrase-stdin")
	if items := fake.list(testToken, ""); len(items) != 1 {
		test.Errorf("after the change: %+v", items)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "example.com")); output != "still-here-example" {
		test.Errorf("after the change copy printed %q", output)
	}
	fake.mustRun(testToken, "", "lock")

	// The recovery key proves the change when the passphrase is forgotten.
	fake.mustRun(testToken, oldRecoveryKey+"\nthird passphrase example\n", "change-passphrase", "--recovery-key", "--passphrase-stdin")
	if items := fake.list(testToken, "third passphrase example\n"); len(items) != 1 {
		test.Errorf("after the change by recovery key: %+v", items)
	}
	fake.mustRun(testToken, "", "lock")

	// A new recovery key replaces the old one.
	output := fake.mustRun(testToken, "third passphrase example\n", "new-recovery-key", "--passphrase-stdin", "--json")
	var replaced struct {
		RecoveryKey string `json:"recoveryKey"`
	}
	if err := json.Unmarshal([]byte(output), &replaced); err != nil || replaced.RecoveryKey == oldRecoveryKey {
		test.Fatalf("new-recovery-key: %v %s", err, output)
	}
	if _, err := fake.run(testToken, oldRecoveryKey+"\n", "unlock", "--recovery-key", "--passphrase-stdin"); err == nil {
		test.Error("the old recovery key still opens Passwords")
	}
	fake.mustRun(testToken, replaced.RecoveryKey+"\n", "unlock", "--recovery-key", "--passphrase-stdin")
}

// Erase asks first, and then forgets the unlock too.
func TestPasswordsErase(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	if _, err := fake.run(testToken, "", "erase"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Errorf("erase without confirmation: %v", err)
	}
	if fake.users[testToken].setup == nil {
		test.Fatal("erase went ahead without confirmation")
	}
	fake.mustRun(testToken, "", "erase", "--force")
	if fake.users[testToken].setup != nil {
		test.Error("erase did not erase")
	}
}

// Settings set the lock time within its bounds.
func TestPasswordsSettings(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	if output := fake.mustRun(testToken, "", "settings"); !strings.Contains(output, "not set up") {
		test.Errorf("settings before setup: %s", output)
	}
	fake.setUpAs(testToken, testPassphrase)
	if _, err := fake.run(testToken, "", "settings", "--lock-after-minutes", "0"); err == nil {
		test.Error("a lock time of 0")
	}
	if _, err := fake.run(testToken, "", "settings", "--lock-after-minutes", "20000"); err == nil {
		test.Error("a lock time past a week")
	}
	fake.mustRun(testToken, "", "settings", "--lock-after-minutes", "60")
	if output := fake.mustRun(testToken, "", "settings", "--json"); !strings.Contains(output, `"lockAfterMinutes": 60`) {
		test.Errorf("settings: %s", output)
	}
}

// testGeneratorSymbols are the symbols of the generator in internal/passwords.
const testGeneratorSymbols = "!@#$%^&*()-_=+[]{}.,:;?"

// A generated password has the length asked for and a character of each class,
// and no two are alike.
func TestGeneratePassword(test *testing.T) {
	seen := map[string]bool{}
	for index := 0; index < 50; index++ {
		password, err := generatePassword(24, true)
		if err != nil || len(password) != 24 || seen[password] {
			test.Fatalf("generatePassword: %q %v", password, err)
		}
		seen[password] = true
		var hasLower, hasUpper, hasDigit, hasSymbol bool
		for _, character := range password {
			hasLower = hasLower || unicode.IsLower(character)
			hasUpper = hasUpper || unicode.IsUpper(character)
			hasDigit = hasDigit || unicode.IsDigit(character)
			hasSymbol = hasSymbol || strings.ContainsRune(testGeneratorSymbols, character)
		}
		if !hasLower || !hasUpper || !hasDigit || !hasSymbol {
			test.Fatalf("a class is missing from %q", password)
		}
	}
	plain, err := generatePassword(16, false)
	if err != nil || strings.ContainsAny(plain, testGeneratorSymbols) {
		test.Errorf("without symbols: %q %v", plain, err)
	}
	for _, length := range []int{0, 7, 129} {
		if _, err := generatePassword(length, true); err == nil {
			test.Errorf("a length of %d", length)
		}
	}
}

// A copy goes to the clipboard program on standard input, never in an argument,
// and is cleared after the time given.
func TestPasswordsCopyUsesTheClipboardProgram(test *testing.T) {
	useConfigurationDirectory(test)
	directory := test.TempDir()
	record := filepath.Join(directory, "record")
	program := filepath.Join(directory, "wl-copy")
	script := "#!/bin/sh\nprintf 'arguments=%s input=' \"$*\" >> " + record + "\ncat >> " + record + "\nprintf '\\n' >> " + record + "\n"
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		test.Fatal(err)
	}
	clipboardLookPath = func(name string) (string, error) {
		if name == "wl-copy" {
			return program, nil
		}
		return "", errors.New("not here")
	}

	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--password", "clipboard-secret-example")

	output := fake.mustRun(testToken, "", "copy", "example.com", "--clear-after", "1")
	if strings.Contains(output, "clipboard-secret-example") || !strings.Contains(output, "clearing it in 1 seconds") {
		test.Errorf("copy said:\n%s", output)
	}
	content, err := os.ReadFile(record)
	if err != nil {
		test.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 || lines[0] != "arguments= input=clipboard-secret-example" || lines[1] != "arguments=--clear input=" {
		test.Errorf("the clipboard program was run as:\n%s", content)
	}
}

// fakeClipboard installs a wl-copy and a wl-paste that keep the clipboard in a
// file and record every call, and returns the two files. wl-paste prints the
// clipboard as it is.
func fakeClipboard(test *testing.T) (clipboardPath, recordPath string) {
	test.Helper()
	directory := test.TempDir()
	clipboardPath, recordPath = filepath.Join(directory, "clipboard"), filepath.Join(directory, "record")
	copyScript := "#!/bin/sh\nprintf 'copy %s\\n' \"$*\" >> " + recordPath + "\nif [ \"$1\" = \"--clear\" ]; then : > " + clipboardPath + "; else cat > " + clipboardPath + "; fi\n"
	pasteScript := "#!/bin/sh\nprintf 'paste\\n' >> " + recordPath + "\ncat " + clipboardPath + "\n"
	copyProgram, pasteProgram := filepath.Join(directory, "wl-copy"), filepath.Join(directory, "wl-paste")
	for path, script := range map[string]string{copyProgram: copyScript, pasteProgram: pasteScript} {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			test.Fatal(err)
		}
	}
	original := clipboardLookPath
	clipboardLookPath = func(name string) (string, error) {
		switch name {
		case "wl-copy":
			return copyProgram, nil
		case "wl-paste":
			return pasteProgram, nil
		}
		return "", errors.New("not here")
	}
	test.Cleanup(func() { clipboardLookPath = original })
	return clipboardPath, recordPath
}

// The clipboard is cleared only if it still holds the secret: something copied
// since is left alone.
func TestPasswordsCopyClearsOnlyWhatItCopied(test *testing.T) {
	useConfigurationDirectory(test)
	clipboardPath, _ := fakeClipboard(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "example.com", "--password", "clipboard-secret-example")

	// Still holding the secret: cleared.
	fake.mustRun(testToken, "", "copy", "example.com", "--clear-after", "1")
	if content, _ := os.ReadFile(clipboardPath); len(content) != 0 {
		test.Errorf("the secret was left on the clipboard: %q", content)
	}

	// The person copied something else while the command waited: kept.
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if content, _ := os.ReadFile(clipboardPath); string(content) == "clipboard-secret-example" {
				_ = os.WriteFile(clipboardPath, []byte("something the person copied later"), 0o600)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	fake.mustRun(testToken, "", "copy", "example.com", "--clear-after", "1")
	<-done
	if content, _ := os.ReadFile(clipboardPath); string(content) != "something the person copied later" {
		test.Errorf("a later copy was cleared: %q", content)
	}
}

// An iteration count the server sends that is outside the range every client
// accepts is refused before the passphrase is used for anything.
func TestPasswordsRefuseAnIterationCountOutsideTheRange(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	for _, count := range []int{1000, passwords.MinimumIterationCount - 1, passwords.MaximumIterationCount + 1} {
		fake.users[testToken].setup.PassphraseIterationCount = count
		_, err := fake.run(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
		if err == nil || !strings.Contains(err.Error(), "passphrase iterations") {
			test.Errorf("%d iterations: %v", count, err)
		}
	}
	if fake.operationCount("UnlockPasswords") != 0 {
		test.Error("a proof was sent for an iteration count the client refuses")
	}
}

// A one-time code typed as a bare secret becomes an address with the name and
// the secret escaped, the same address the importers make.
func TestPasswordsOneTimeCodeAddress(test *testing.T) {
	address, err := oneTimeCodeAddress("jbsw y3dp ehpk 3pxp", "Ada's shop & café")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.HasPrefix(address, "otpauth://totp/Ada%27s%20shop%20&%20caf%C3%A9?secret=") && !strings.HasPrefix(address, "otpauth://totp/Ada's%20shop%20&%20caf%C3%A9?secret=") {
		test.Errorf("the name is not escaped: %s", address)
	}
	if !strings.HasSuffix(address, "secret=jbswy3dpehpk3pxp") {
		test.Errorf("the secret is not as typed without its spaces: %s", address)
	}
	if _, err := passwords.ParseOneTimeCodeURI(address); err != nil {
		test.Errorf("the address does not parse: %v", err)
	}
	if kept, err := oneTimeCodeAddress("otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP", "x"); err != nil || kept != "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP" {
		test.Errorf("an address is kept as it is: %q %v", kept, err)
	}
	if _, err := oneTimeCodeAddress("not base32 !!", "x"); err == nil {
		test.Error("a secret that is not base32")
	}
}
