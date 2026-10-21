package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/passwords"
)

// A name somebody else chose, such as an item shared with you, cannot start a
// row or a column of its own in a table: the line break and the tab are shown
// as their escapes, as the other control characters are.
func TestATableCellCannotForgeARowOrAColumn(test *testing.T) {
	output := captureStandardOutput(test, func() {
		if err := printTable([]string{"name", "kind"}, [][]string{
			{"Real\nForged item\tlogin\r\x1b[2K", "login"},
			{"Plain", "note"},
		}); err != nil {
			test.Error(err)
		}
	})
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 3 {
		test.Fatalf("the table has %d lines, expected a header and two rows:\n%q", len(lines), output)
	}
	if !strings.Contains(lines[1], `Real\nForged item\tlogin\r\x1b[2K`) {
		test.Errorf("the cell was not shown with its escapes: %q", lines[1])
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\x1b") {
			test.Errorf("a control character reached the terminal: %q", line)
		}
	}
	if columns := strings.Fields(lines[1]); columns[len(columns)-1] != "login" {
		test.Errorf("the row's last column is %q", columns[len(columns)-1])
	}
}

// Every flag that puts a secret or a note in the command line says so in its
// help, and a card's number and security code can be read from standard input
// as a login's password can.
func TestEverySecretFlagWarnsAboutTheProcessList(test *testing.T) {
	flagsByName := map[string]cli.Flag{}
	for _, flag := range passwordsFieldFlags() {
		flagsByName[flag.Names()[0]] = flag
	}
	for _, name := range []string{"number", "security-code", "notes", "text", "one-time-code"} {
		usage := flagsByName[name].(*cli.StringFlag).Usage
		if !strings.Contains(usage, "process list") {
			test.Errorf("--%s does not warn about the process list: %q", name, usage)
		}
	}
	for _, name := range []string{"number-stdin", "security-code-stdin"} {
		if _, ok := flagsByName[name]; !ok {
			test.Errorf("there is no --%s", name)
		}
		if got := passwordsFlagKinds[name]; len(got) != 1 || got[0] != passwords.ItemKindCard {
			test.Errorf("--%s is for %v, expected a card only", name, got)
		}
	}
}

func TestACardsNumberAndSecurityCodeComeFromStandardInput(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "4111111111111111\n123\n", "add", "Travel card", "--kind", "card", "--number-stdin", "--security-code-stdin")
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Travel card")); output != "4111111111111111" {
		test.Errorf("the card number: %q", output)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Travel card", "--security-code")); output != "123" {
		test.Errorf("the card security code: %q", output)
	}
	// Changing one leaves the other.
	fake.mustRun(testToken, "5500000000000004\n", "edit", "Travel card", "--number-stdin")
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Travel card")); output != "5500000000000004" {
		test.Errorf("the changed card number: %q", output)
	}
	if output := strings.TrimSpace(fake.mustRun(testToken, "", "copy", "Travel card", "--security-code")); output != "123" {
		test.Errorf("the security code after changing the number: %q", output)
	}
	// One way for each, and only on a card.
	if _, err := fake.run(testToken, "4111111111111111\n", "add", "Bad card", "--kind", "card", "--number", "1", "--number-stdin"); err == nil || !strings.Contains(err.Error(), "one way") {
		test.Errorf("both ways to give the number: %v", err)
	}
	if _, err := fake.run(testToken, "123\n", "add", "Bad login", "--security-code-stdin"); err == nil || !strings.Contains(err.Error(), "not a field of a login") {
		test.Errorf("a security code on a login: %v", err)
	}
}

// With no file, an export is written under a name the agent's file tool
// refuses, in the current directory, in the format asked for.
func TestAnExportWithNoFileGetsAFileToolRefusedName(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "Spare key", "--kind", "note", "--text", "under the blue pot")
	directory := test.TempDir()
	test.Chdir(directory)

	for format, suffix := range map[string]string{"json": ".json", "csv": ".csv"} {
		fake.mustRun(testToken, "", "export", "--force", "--format", format)
		matches, err := filepath.Glob(filepath.Join(directory, "passwords-export-*"+suffix))
		if err != nil || len(matches) != 1 {
			test.Fatalf("the %s export with no file made %v (%v)", format, matches, err)
		}
		if !passwords.IsLocalFile(matches[0]) {
			test.Errorf("%s is not refused by the file tool", matches[0])
		}
		content, _ := os.ReadFile(matches[0])
		if !passwords.HasExportHeader(content) {
			test.Errorf("the %s export does not begin as an export: %.80q", format, content)
		}
	}
	// And an export written under any other name is still told by its first bytes.
	other := filepath.Join(directory, "backup.txt")
	fake.mustRun(testToken, "", "export", other, "--force")
	if !passwords.IsLocalFile(other) {
		test.Error("an export named backup.txt is not refused by the file tool")
	}
}
