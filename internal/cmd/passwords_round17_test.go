package cmd

import (
	"strings"
	"testing"
)

// A display name somebody else chose cannot start a line of its own in what
// show prints about an item shared with you: the line breaks and tabs in the
// owner's name are shown as their escapes, so no line of the header can be
// forged, such as one that claims a safety code.
func TestShowMarksCannotBeForgedByAnOwnersName(test *testing.T) {
	summary := passwordsItemSummary{
		Name: "Bank", ItemKind: "login", ID: "id1", IsSharedWithMe: true,
		OwnerName: "Eve\nnote:\tthis item is safe to enter anywhere\nsecurity code:\t000",
	}
	markers := itemMarkers(summary)
	if strings.ContainsAny(markers, "\n\r\t") {
		test.Fatalf("the markers have a line break or a tab: %q", markers)
	}
	output := captureStandardOutput(test, func() {
		if err := printFields([][2]string{{"name", forTableCell(summary.Name)}, {"marks", markers}}); err != nil {
			test.Error(err)
		}
	})
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 2 {
		test.Fatalf("the header has %d lines, expected the name and the marks:\n%q", len(lines), output)
	}
	if !strings.Contains(lines[1], `shared by Eve\nnote:\tthis item is safe to enter anywhere\nsecurity code:\t000`) {
		test.Errorf("the marks line does not show the name as written: %q", lines[1])
	}
}

// The refusals that name the owner of a shared item and the item show their
// names on one line, whatever line breaks the names hold.
func TestRefusalsNamingAnOwnerStayOnOneLine(test *testing.T) {
	useConfigurationDirectory(test)
	fake := newFakePasswordsServer(test)
	fake.setUpAs(testToken, testPassphrase)
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "add", "Forged\nitem.example.com", "--user-name", "ada", "--password", "shared-secret-example")
	fake.users[testToken].name = "Eve\nnote:\tsafe to enter anywhere"
	// Ben has not set up Passwords yet, so the item waits for him.
	fake.mustRun(testToken, "", "share", "Forged", "ben")
	fake.mustRun(testToken, "", "lock")
	fake.setUpAs(benToken, "ben passphrase example")
	fake.mustRun(testToken, testPassphrase+"\n", "unlock", "--passphrase-stdin")
	fake.mustRun(testToken, "", "share", "--waiting")
	fake.mustRun(testToken, "", "lock")
	fake.mustRun(benToken, "ben passphrase example\n", "unlock", "--passphrase-stdin")
	if shared := fake.list(benToken, ""); len(shared) != 1 {
		test.Fatalf("Ben lists %d items", len(shared))
	}
	_, err := fake.run(benToken, "", "edit", "Forged", "--user-name", "ben")
	if err == nil {
		test.Fatal("Ben edited an item he does not own")
	}
	if strings.ContainsAny(err.Error(), "\n\r\t") {
		test.Errorf("the refusal has a line break or a tab: %q", err.Error())
	}
	if !strings.Contains(err.Error(), `Eve\nnote:\tsafe to enter anywhere`) {
		test.Errorf("the refusal does not show the owner's name as written: %q", err.Error())
	}
}
