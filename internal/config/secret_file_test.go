package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretFileFor writes a secret to a file of its own and reads it back, as
// a server started with --secret-file would.
func secretFileFor(t *testing.T, secret string) *SecretFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.secret")
	if secret != "" {
		if err := WriteSecretFile(path, []byte(secret)); err != nil {
			t.Fatal(err)
		}
	}
	secretFile, err := ReadSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return secretFile
}

// With a secret file the rows hold no secret, only its check, and they read
// back with the file's secret; the agent's keys still open.
func TestTheSecretFileKeepsTheSecretOutOfTheRows(t *testing.T) {
	secret := strings.Repeat("f", 40)
	secretFile := secretFileFor(t, secret)
	configuration := Default()
	configuration.Server.Secret = secret
	configuration.Agent.Search.APIKey = "search-key-1"

	rows, err := ToRows(configuration, 1, secretFile)
	if err != nil {
		t.Fatal(err)
	}
	for key, stored := range rows.Settings {
		if strings.Contains(stored, secret) {
			t.Fatalf("the %s row holds the secret:\n%s", key, stored)
		}
	}
	if !strings.Contains(rows.Settings[settingServer], "secretCheck: ") {
		t.Fatalf("the server row has no check:\n%s", rows.Settings[settingServer])
	}

	read, err := FromRows(rows, secretFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(read.Secret()) != secret || read.Agent.Search.APIKey != "search-key-1" {
		t.Fatalf("read back %q and %q", read.Secret(), read.Agent.Search.APIKey)
	}

	// Without the file, the rows are refused rather than a new secret made.
	if _, err := FromRows(rows, nil); err == nil || !strings.Contains(err.Error(), "--secret-file") {
		t.Fatalf("rows whose secret is in a file were read without it: %v", err)
	}

	// With a file holding another secret, refused too.
	if _, err := FromRows(rows, secretFileFor(t, strings.Repeat("g", 40))); err == nil {
		t.Fatal("rows sealed with one secret were read with another")
	}
}

// A database that still holds its secret reads with a file holding the same
// one, and is refused with a file holding another.
func TestAStoredSecretMustMatchTheFile(t *testing.T) {
	secret := strings.Repeat("h", 40)
	configuration := Default()
	configuration.Server.Secret = secret
	rows, err := ToRows(configuration, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromRows(rows, secretFileFor(t, secret)); err != nil {
		t.Fatalf("the same secret was refused: %s", err)
	}
	if _, err := FromRows(rows, secretFileFor(t, strings.Repeat("i", 40))); err == nil {
		t.Fatal("a file with another secret was accepted over the stored one")
	}
}

// A missing file is filled only for a server with no secret anywhere; one
// whose secret is still stored, or was sealed with a file now gone, is
// refused.
func TestAMissingSecretFileIsWrittenOnlyForANewServer(t *testing.T) {
	fresh := secretFileFor(t, "")
	if err := prepareSecretFile(Default(), fresh); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(fresh.Path)
	if err != nil || len(written) < 32 || string(written) != string(fresh.Secret) {
		t.Fatalf("a new server's secret file was not written: %v", err)
	}
	if info, err := os.Stat(fresh.Path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the secret file is not private: %v", info.Mode())
	}

	stored := Default()
	stored.Server.Secret = strings.Repeat("j", 40)
	if err := prepareSecretFile(stored, secretFileFor(t, "")); err == nil || !strings.Contains(err.Error(), "holds a secret already") {
		t.Fatalf("a stored secret was replaced by a new one: %v", err)
	}

	sealed := Default()
	sealed.Server.SecretCheck = secretCheckOf([]byte(strings.Repeat("k", 40)))
	if err := prepareSecretFile(sealed, secretFileFor(t, "")); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("a lost secret file was replaced by a new secret: %v", err)
	}

	// And never written over an existing file.
	if err := WriteSecretFile(fresh.Path, []byte(strings.Repeat("l", 40))); err == nil {
		t.Fatal("an existing secret file was overwritten")
	}
}
