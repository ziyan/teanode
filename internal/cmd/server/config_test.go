package server

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The compose file used to create PostgreSQL with the password "teanode",
// written into this repository for everybody to read, and the connection
// string beside it said the same word. The database listens on the loopback
// address of a machine that may have other things on it.
//
// So the file this command writes carries a password nobody else has, in the
// two places that have to agree: the URL the server signs in with, and the
// variable the compose file creates the database with.
func TestTheGeneratedEnvironmentCarriesAPasswordNobodyElseHas(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "env")

	command := newConfigEnvCommand()
	if err := command.Run(context.Background(), []string{
		"env", "--output", filename, "--hostname", "mail.example.com",
	}); err != nil {
		t.Fatalf("writing it: %s", err)
	}
	written, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	content := string(written)

	if strings.Contains(content, "postgres://teanode:teanode@") {
		t.Fatal("the connection string still carries the password this repository publishes")
	}
	inURL := regexp.MustCompile(`postgres://teanode:([^@]+)@`).FindStringSubmatch(content)
	inVariable := regexp.MustCompile(`(?m)^POSTGRES_PASSWORD=(.+)$`).FindStringSubmatch(content)
	if inURL == nil || inVariable == nil {
		t.Fatalf("both places name it:\n%s", content)
	}
	if inURL[1] != inVariable[1] {
		t.Fatalf("and they are the same password: %q and %q", inURL[1], inVariable[1])
	}
	if len(inURL[1]) < 24 {
		t.Fatalf("which is long enough to be one: %q", inURL[1])
	}

	// Two files written a moment apart do not carry the same password.
	second := filepath.Join(directory, "env2")
	if err := newConfigEnvCommand().Run(context.Background(), []string{
		"env", "--output", second, "--hostname", "mail.example.com",
	}); err != nil {
		t.Fatalf("writing another: %s", err)
	}
	other, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	if strings.Contains(string(other), inURL[1]) {
		t.Fatal("every file carries its own")
	}

	// It carries a password, so nobody else on the machine may read it.
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("stat: %s", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the file's mode: %o", mode)
	}
}

// An operator who brings their own database said what its password is. A
// second one generated here would be a line that means nothing, and the
// compose file's PostgreSQL is not the one being used.
func TestABroughtConnectionStringIsLeftAlone(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "env")
	theirs := "postgres://somebody:their-own-password@elsewhere:5432/teanode?sslmode=require"

	if err := newConfigEnvCommand().Run(context.Background(), []string{
		"env", "--output", filename, "--database-url", theirs,
	}); err != nil {
		t.Fatalf("writing it: %s", err)
	}
	written, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("reading it back: %s", err)
	}
	content := string(written)
	if !strings.Contains(content, theirs) {
		t.Fatalf("their own connection string:\n%s", content)
	}
	if strings.Contains(content, "POSTGRES_PASSWORD=") {
		t.Fatalf("and nothing about a database this deployment does not create:\n%s", content)
	}
}
