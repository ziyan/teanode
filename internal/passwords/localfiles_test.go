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
