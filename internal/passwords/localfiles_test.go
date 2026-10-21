//go:build !windows

package passwords

import (
	"os"
	"path/filepath"
	"testing"
)

// The files `teanode passwords` keeps on a computer are told by their names,
// whichever profile they are for, also through a link; other files are not.
func TestLocalFilesAreToldByName(test *testing.T) {
	directory := test.TempDir()
	for name, want := range map[string]bool{
		UnlockFileName("work"):          true,
		KnownKeysFileName("work"):       true,
		UnlockFileName("a_b-1.example"): true,
		"PASSWORDS-UNLOCK-X.JSON":       true,
		"passwords-unlock-work.txt":     false,
		"profiles.json":                 false,
		"my-passwords.json":             false,
	} {
		if got := IsLocalFileName(name); got != want {
			test.Errorf("IsLocalFileName(%q) = %v, want %v", name, got, want)
		}
	}
	real := filepath.Join(directory, UnlockFileName("work"))
	if err := os.WriteFile(real, []byte("{}"), 0o600); err != nil {
		test.Fatal(err)
	}
	link := filepath.Join(directory, "innocent.txt")
	if err := os.Symlink(real, link); err != nil {
		test.Fatal(err)
	}
	if !IsLocalFile(real) || !IsLocalFile(link) || IsLocalFile(filepath.Join(directory, "innocent2.txt")) {
		test.Error("IsLocalFile")
	}
	test.Setenv(AgentCommandEnvironmentName, "")
	if IsAgentCommand() {
		test.Error("an empty mark counts")
	}
	test.Setenv(AgentCommandEnvironmentName, "1")
	if !IsAgentCommand() {
		test.Error("the mark does not count")
	}
}

// An export holds every item in the clear. Its default name is one the file
// tool refuses, and so is a file the person named as they liked that begins as
// an export does, in either shape.
func TestAnExportIsToldByNameAndByItsFirstBytes(test *testing.T) {
	directory := test.TempDir()
	for name, want := range map[string]bool{
		ExportFileName("20261021T101500Z", "json"): true,
		ExportFileName("20261021T101500Z", "csv"):  true,
		"Passwords-Export-Backup.JSON":             true,
		"passwords-export-backup.txt":              false,
		"my-passwords-export.json":                 false,
	} {
		if got := IsLocalFileName(name); got != want {
			test.Errorf("IsLocalFileName(%q) = %v, want %v", name, got, want)
		}
	}
	for name, content := range map[string]string{
		"dashboard.txt": "{\n  \"version\": 1,\n  \"exportedAt\": \"2026-10-20T10:00:00.000Z\",\n  \"items\": []\n}",
		"minified.dat":  `{"version":1,"exportedAt":"2026-10-20T10:00:00Z","items":[]}`,
		"bom.dat":       "\xef\xbb\xbf  {\r\n\"version\": 1, \"exportedAt\": \"x\", \"items\": []}",
		// The keys in sorted order, so the items come before the version.
		"sorted.dat": `{"exportedAt":"2026-10-20T10:00:00Z","items":[{"fields":{"password":"p"},"id":"x","itemKind":"login"}],"version":1}`,
		// Any order, with no version or time in the part looked at.
		"reordered.dat":   `{"items":[{"itemKind":"note","id":"x","fields":{}}]}`,
		"utf16.dat":       "\xff\xfe{\x00\"\x00i\x00t\x00e\x00m\x00s\x00\"\x00:\x00[\x00]\x00,\x00\"\x00v\x00e\x00r\x00s\x00i\x00o\x00n\x00\"\x00:\x001\x00,\x00\"\x00e\x00x\x00p\x00o\x00r\x00t\x00e\x00d\x00A\x00t\x00\"\x00:\x000\x00}\x00",
		"chrome.dat":      "name,url,username,password,note\nexample,https://example.com,ada,hunter2,\n",
		"apple.dat":       "Title,URL,Username,Password,Notes,OTPAuth\r\nexample,https://example.com,ada,hunter2,,\r\n",
		"older.dat":       "\"name\",\"url\",\"username\",\"password\"\n",
		"spreadsheet.dat": "folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp\n",
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			test.Fatal(err)
		}
		if !IsLocalFile(path) {
			test.Errorf("%s begins as an export does and was not refused", name)
		}
	}
	for name, content := range map[string]string{
		"settings.json": "{\n  \"version\": 2,\n  \"theme\": \"dark\"\n}",
		"list.csv":      "name,url\nExample,https://example.com\n",
		// Begins with version and has exportedAt, but is not the Passwords shape.
		"notes-backup.dat": `{"version": 3, "app": "notes", "exportedAt": "2026-01-01T00:00:00Z", "notes": []}`,
		"bookmarks.dat":    "{\n  \"version\": 1,\n  \"title\": \"Bookmarks\",\n  \"exportedAt\": 1700000000\n}",
		"items-only.dat":   `{"items": [{"title": "a"}], "version": 1}`,
		"notes.txt":        "the word exportedAt, and \"version\", in the middle of a note",
		"empty.dat":        "",
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			test.Fatal(err)
		}
		if IsLocalFile(path) {
			test.Errorf("%s is not an export and was refused", name)
		}
	}
	if IsLocalFile(filepath.Join(directory, "missing.json")) || IsLocalFile(directory) {
		test.Error("a file that is not there, or a directory, counts as an export")
	}
}
