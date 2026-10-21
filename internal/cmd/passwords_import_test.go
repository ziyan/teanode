package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/passwords"
)

// throwawayGPG makes a GPG home with a key that has no passphrase, so that a
// test can encrypt a password store and decrypt it without asking anybody. It
// returns the recipient to encrypt for.
func throwawayGPG(test *testing.T) string {
	test.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		test.Skip("gpg is not installed")
	}
	home := test.TempDir()
	test.Setenv("GNUPGHOME", home)
	test.Cleanup(func() {
		// The agent started for this home would otherwise outlive the test.
		_ = exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run()
	})
	recipient := "pass-test@example.test"
	command := exec.Command("gpg", "--batch", "--quiet", "--pinentry-mode", "loopback", "--passphrase", "",
		"--quick-generate-key", "Pass Test <"+recipient+">", "default", "default", "never")
	if output, err := command.CombinedOutput(); err != nil {
		test.Skipf("cannot make a throwaway GPG key here: %s: %s", err, output)
	}
	return recipient
}

// writeStoreEntry encrypts text into the store at path.gpg, as pass does.
func writeStoreEntry(test *testing.T, store, recipient, path, text string) {
	test.Helper()
	target := filepath.Join(store, filepath.FromSlash(path)+".gpg")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		test.Fatal(err)
	}
	command := exec.Command("gpg", "--batch", "--quiet", "--yes", "--trust-model", "always", "--encrypt", "--recipient", recipient, "--output", target)
	command.Stdin = strings.NewReader(text)
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatalf("gpg encrypt %s: %s: %s", path, err, output)
	}
}

type importResult struct {
	FoundCount    int      `json:"foundCount"`
	ImportedCount int      `json:"importedCount"`
	SkippedCount  int      `json:"skippedCount"`
	SkippedNames  []string `json:"skippedNames"`
	FailedCount   int      `json:"failedCount"`
	LeftOutNames  []string `json:"leftOutNames"`
	IsDryRun      bool     `json:"isDryRun"`
}

func (self *fakePasswordsServer) importCounts(standardInput string, arguments ...string) (importResult, string) {
	self.test.Helper()
	output := self.mustRun(testToken, standardInput, append([]string{"import"}, append(arguments, "--json")...)...)
	var counts importResult
	if err := json.Unmarshal([]byte(output), &counts); err != nil {
		self.test.Fatalf("import output: %s: %s", err, output)
	}
	return counts, output
}

// A pass store of invented entries, encrypted with a throwaway key, is
// decrypted entry by entry, saved sealed, and importing it again adds nothing.
func TestPasswordsImportPassStore(test *testing.T) {
	useConfigurationDirectory(test)
	recipient := throwawayGPG(test)
	store := filepath.Join(test.TempDir(), "password-store")
	writeStoreEntry(test, store, recipient, "web/example.com/ada@example.com", "pw-one-example\nurl: https://example.com/login\nrecovery phrase: first pet\n")
	writeStoreEntry(test, store, recipient, "work/intranet", "pw-two-example\nlogin: ada\notpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP&issuer=Example\n")
	writeStoreEntry(test, store, recipient, "blog", "pw-three-example\n")
	// A git directory and the key list are not entries, and a file that is
	// not encrypted is an entry that cannot be read.
	writeStoreEntry(test, store, recipient, ".git/leftover", "pw-ignored-example\n")
	if err := os.WriteFile(filepath.Join(store, ".gpg-id"), []byte(recipient+"\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store, "broken"), 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "broken", "entry.gpg"), []byte("this is not encrypted"), 0o600); err != nil {
		test.Fatal(err)
	}

	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	counts, _ := fake.importCounts("", "pass", "--store", store, "--dry-run")
	if !counts.IsDryRun || counts.FoundCount != 4 || counts.ImportedCount != 3 || counts.FailedCount != 1 || counts.SkippedCount != 0 {
		test.Errorf("dry run: %+v", counts)
	}
	if fake.operationCount("SavePasswordItem") != 0 {
		test.Fatal("a dry run saved an item")
	}

	counts, output := fake.importCounts("", "pass", "--store", store)
	if counts.ImportedCount != 3 || counts.FailedCount != 1 {
		test.Errorf("import: %+v", counts)
	}
	for _, secret := range []string{"pw-one-example", "pw-two-example", "pw-three-example", "first pet"} {
		if strings.Contains(output, secret) {
			test.Errorf("the import printed a secret: %s", output)
		}
	}
	for _, item := range fake.items {
		for _, plain := range []string{"pw-", "intranet", "example.com", "first pet"} {
			if strings.Contains(item.encryptedFields, plain) {
				test.Fatalf("the server holds %q in the clear", plain)
			}
		}
	}

	items := fake.list(testToken, "")
	byName := map[string]listedItem{}
	for _, item := range items {
		byName[item.Name] = item
	}
	if len(items) != 3 || byName["example.com"].UserName != "ada@example.com" || byName["example.com"].Folder != "web" ||
		byName["intranet"].UserName != "ada" || byName["blog"].ItemKind != "login" {
		test.Fatalf("imported items: %+v", items)
	}
	revealed := fake.mustRun(testToken, "", "show", "intranet", "--reveal", "--json")
	if !strings.Contains(revealed, "pw-two-example") || !strings.Contains(revealed, "s left)") {
		test.Errorf("an imported login:\n%s", revealed)
	}
	if code := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "intranet", "--code")); len(code) != 6 {
		test.Errorf("the one-time code of an imported login: %q", code)
	}
	notes := fake.mustRun(testToken, "", "show", "example.com", "--json")
	if !strings.Contains(notes, "recovery phrase: first pet") {
		test.Errorf("the notes of an imported login:\n%s", notes)
	}

	counts, _ = fake.importCounts("", "pass", "--store", store)
	if counts.ImportedCount != 0 || counts.SkippedCount != 3 || counts.FailedCount != 1 {
		test.Errorf("a second import: %+v", counts)
	}
	if len(counts.SkippedNames) != 3 || !slices.Contains(counts.SkippedNames, "example.com") || !slices.Contains(counts.SkippedNames, "intranet") || !slices.Contains(counts.SkippedNames, "blog") {
		test.Errorf("the skipped items are not named: %+v", counts.SkippedNames)
	}
	if text := fake.mustRun(testToken, "", "import", "pass", "--store", store); !strings.Contains(text, "already there, so skipped:") || !strings.Contains(text, "intranet") || strings.Contains(text, "pw-") {
		test.Errorf("the skipped items are not named in the text:\n%s", text)
	}
	if len(fake.list(testToken, "")) != 3 {
		test.Error("a second import added items")
	}

	if _, err := fake.run(testToken, "", "import", "pass", "--store", filepath.Join(store, "nothing-here")); err == nil {
		test.Error("a store that does not exist")
	}
}

const chromeExport = "name,url,username,password,note\n" +
	"example.com,https://example.com/login,ada@example.com,csv-pw-one-example,\"two\nline note\"\n" +
	"example.org,https://example.org,ben,csv-pw-two-example,\n"

const bitwardenExport = "folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp\n" +
	"Work,,login,intranet,,,0,https://intranet.example.test,ada,csv-pw-three-example,JBSWY3DPEHPK3PXP\n" +
	",,note,Spare key,under the blue pot,,0,,,,\n" +
	",,card,Travel card,,,0,,,,\n"

func TestPasswordsImportCSV(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	directory := test.TempDir()
	chromePath := filepath.Join(directory, "chrome.csv")
	if err := os.WriteFile(chromePath, []byte(chromeExport), 0o600); err != nil {
		test.Fatal(err)
	}
	counts, output := fake.importCounts("", "csv", chromePath)
	if counts.FoundCount != 2 || counts.ImportedCount != 2 {
		test.Errorf("chrome import: %+v", counts)
	}
	if strings.Contains(output, "csv-pw") {
		test.Errorf("the import printed a password: %s", output)
	}
	counts, _ = fake.importCounts("", "csv", chromePath)
	if counts.ImportedCount != 0 || counts.SkippedCount != 2 {
		test.Errorf("a second chrome import: %+v", counts)
	}

	// Standard input, a note and a card that is left out.
	counts, _ = fake.importCounts(bitwardenExport, "csv", "-")
	if counts.ImportedCount != 2 || len(counts.LeftOutNames) != 1 || counts.LeftOutNames[0] != "Travel card" {
		test.Errorf("bitwarden import: %+v", counts)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Spare key")); output != "under the blue pot" {
		test.Errorf("an imported note: %q", output)
	}
	if output := fake.mustRun(testToken, "", "show", "intranet", "--json"); !strings.Contains(output, `"folder"`) || !strings.Contains(output, "Work") {
		test.Errorf("an imported folder:\n%s", output)
	}

	if _, err := fake.run(testToken, "", "import", "csv", filepath.Join(directory, "missing.csv")); err == nil {
		test.Error("a file that does not exist")
	}
	notCSV := filepath.Join(directory, "other.csv")
	if err := os.WriteFile(notCSV, []byte("a,b,c\n1,2,3\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	if _, err := fake.run(testToken, "", "import", "csv", notCSV); err == nil {
		test.Error("a CSV file from no password manager")
	}
}

// An export is asked about, is readable by its owner only, does not overwrite,
// and a CSV one imports back.
func TestPasswordsExport(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	directory := test.TempDir()
	chromePath := filepath.Join(directory, "chrome.csv")
	if err := os.WriteFile(chromePath, []byte(bitwardenExport), 0o600); err != nil {
		test.Fatal(err)
	}
	fake.importCounts("", "csv", chromePath)
	fake.mustRun(testToken, "", "add", "Travel card", "--kind", "card", "--number", "4111111111111111", "--security-code", "123")

	target := filepath.Join(directory, "everything.json")
	if _, err := fake.run(testToken, "", "export", target); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Errorf("export without confirmation: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		test.Fatal("export wrote a file before it was confirmed")
	}
	fake.mustRun(testToken, "", "export", target, "--force")
	information, err := os.Stat(target)
	if err != nil || information.Mode().Perm() != 0o600 {
		test.Fatalf("the export: %v %v", err, information)
	}
	content, _ := os.ReadFile(target)
	var exported struct {
		Items []struct {
			ItemKind string         `json:"itemKind"`
			Fields   map[string]any `json:"fields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(content, &exported); err != nil || len(exported.Items) != 3 {
		test.Fatalf("the JSON export: %v\n%s", err, content)
	}
	if !bytes.Contains(content, []byte("csv-pw-three-example")) || !bytes.Contains(content, []byte("4111111111111111")) {
		test.Error("the JSON export has not everything")
	}
	if _, err := fake.run(testToken, "", "export", target, "--force"); err == nil || !strings.Contains(err.Error(), "already exists") {
		test.Errorf("export over a file: %v", err)
	}

	csvTarget := filepath.Join(directory, "logins.csv")
	output := fake.mustRun(testToken, "", "export", csvTarget, "--force")
	if !strings.Contains(output, "left out 1 item") {
		test.Errorf("the card was not reported as left out:\n%s", output)
	}
	file, err := os.Open(csvTarget)
	if err != nil {
		test.Fatal(err)
	}
	defer func() {
		_ = file.Close()
	}()
	parsed, err := passwords.ParseCSV(file)
	if err != nil || parsed.Format != passwords.CSVFormatBitwarden || len(parsed.Items) != 2 {
		test.Fatalf("reading the CSV export back: %v %+v", err, parsed)
	}
	var login *passwords.LoginFields
	for _, item := range parsed.Items {
		if item.Login != nil {
			login = item.Login
		}
	}
	if login == nil || login.Name != "intranet" || login.Password != "csv-pw-three-example" || login.Folder != "Work" ||
		len(login.Sites) != 1 || login.OneTimeCodeURI == "" {
		test.Errorf("the login in the CSV export: %+v", login)
	}
}

// Two logins with the same name, site and user name are the same item only when
// their folder and password are too: a changed password, or the same name in
// another folder, is kept as a second item, not dropped.
func TestPasswordsImportKeepsWhatDiffers(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")

	export := func(folder, password string) string {
		return "folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp\n" +
			folder + ",,login,example.com,,,0,https://example.com,ada," + password + ",\n"
	}
	counts, _ := fake.importCounts(export("Work", "first-password-example"), "csv", "-")
	if counts.ImportedCount != 1 {
		test.Fatalf("the first import: %+v", counts)
	}
	for name, text := range map[string]string{
		"the same":           export("Work", "first-password-example"),
		"the same, any case": strings.ReplaceAll(export("work", "first-password-example"), "example.com,", "Example.com,"),
	} {
		counts, _ = fake.importCounts(text, "csv", "-")
		if counts.ImportedCount != 0 || counts.SkippedCount != 1 || len(counts.SkippedNames) != 1 {
			test.Errorf("%s: %+v", name, counts)
		}
	}
	for name, text := range map[string]string{
		"another password": export("Work", "second-password-example"),
		"another folder":   export("Home", "first-password-example"),
	} {
		counts, _ = fake.importCounts(text, "csv", "-")
		if counts.ImportedCount != 1 || counts.SkippedCount != 0 {
			test.Errorf("%s: %+v", name, counts)
		}
	}
	if len(fake.list(testToken, "")) != 3 {
		test.Errorf("the items: %+v", fake.list(testToken, ""))
	}
}

// gpg is given the path after "--", so that a store whose name begins with a
// dash is a path and not an option.
func TestPasswordsImportPassCallsGPGAfterTwoDashes(test *testing.T) {
	useConfigurationDirectory(test)
	directory := test.TempDir()
	record := filepath.Join(directory, "record")
	program := filepath.Join(directory, "gpg")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + record + "\nprintf 'pw-example\\n'\n"
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		test.Fatal(err)
	}
	original := passwordsGPGProgram
	passwordsGPGProgram = program
	test.Cleanup(func() { passwordsGPGProgram = original })

	// A relative store named like an option, with one entry in it.
	workingDirectory := test.TempDir()
	test.Chdir(workingDirectory)
	if err := os.MkdirAll("-store/web", 0o700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile("-store/web/example.com.gpg", []byte("not read by the fake gpg"), 0o600); err != nil {
		test.Fatal(err)
	}
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	counts, _ := fake.importCounts("", "pass", "--store", "./-store")
	if counts.ImportedCount != 1 {
		test.Fatalf("import: %+v", counts)
	}
	counts, _ = fake.importCounts("", "pass", "--store", "-store")
	if counts.ImportedCount != 0 || counts.SkippedCount != 1 {
		test.Fatalf("import from a store named like an option: %+v", counts)
	}
	content, err := os.ReadFile(record)
	if err != nil {
		test.Fatal(err)
	}
	arguments := strings.Fields(string(content))
	if len(arguments) < 2 || arguments[len(arguments)-2] != "--" || !strings.HasSuffix(arguments[len(arguments)-1], "example.com.gpg") {
		test.Errorf("gpg was run with %q", arguments)
	}
}
