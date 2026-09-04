package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeMigrator struct {
	unknown  []string
	migrated bool
}

func (self *fakeMigrator) UnknownMigrations() ([]string, error) {
	return self.unknown, nil
}

func (self *fakeMigrator) Migrate() error {
	self.migrated = true
	return nil
}

// stageBinary leaves the directory looking the way an upgrade that has been
// installed and refused looks: a binary, and a marker saying it was tried.
func stageBinary(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "teanode"), []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "pending"), []byte("started\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

// Reverting is what Migrate does with a migration it does not recognise, and
// it cannot tell an accident from an intention. So it stops and asks.
func TestMigrateRefusesToRevertUnasked(t *testing.T) {
	database := &fakeMigrator{unknown: []string{"0042_something_new"}}

	err := migrate(database, t.TempDir())
	if err == nil {
		t.Fatal("it reverted a newer version's migrations without being asked")
	}
	if database.migrated {
		t.Error("it migrated anyway")
	}
	for _, want := range []string{"0042_something_new", AllowMigrationRevert} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// And when there is an upgrade sitting staged that this start refused, the
// message says so: running it again is the way out that loses nothing, and it
// is not the one the operator would think of.
func TestMigrateNamesTheStagedBinaryItCouldRunInstead(t *testing.T) {
	directory := stageBinary(t)

	err := migrate(&fakeMigrator{unknown: []string{"0042_something_new"}}, directory)
	if err == nil {
		t.Fatal("it reverted an upgrade that is installed and waiting")
	}
	for _, want := range []string{directory, "pending"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// A downgrade somebody asked for still works, which is the point of asking.
func TestMigrateRevertsWhenTold(t *testing.T) {
	t.Setenv(AllowMigrationRevert, "true")
	database := &fakeMigrator{unknown: []string{"0042_something_new"}}

	if err := migrate(database, t.TempDir()); err != nil {
		t.Fatalf("a downgrade somebody asked for was refused: %s", err)
	}
	if !database.migrated {
		t.Error("it did not migrate")
	}
}

// And a staged binary on its own is not a reason to stop: the schema is one
// this binary knows, so there is nothing to revert and nothing to lose.
func TestMigrateRunsWithAStagedBinaryAndNothingToRevert(t *testing.T) {
	if err := migrate(&fakeMigrator{}, stageBinary(t)); err != nil {
		t.Fatalf("migrate: %s", err)
	}
}

// A database that cannot say what it holds must not be migrated on the
// assumption that it holds nothing.
func TestMigrateStopsWhenItCannotAsk(t *testing.T) {
	database := &refusingMigrator{}
	if err := migrate(database, t.TempDir()); err == nil {
		t.Fatal("it migrated a database it could not read")
	}
	if database.migrated {
		t.Error("it migrated anyway")
	}
}

type refusingMigrator struct {
	migrated bool
}

func (self *refusingMigrator) UnknownMigrations() ([]string, error) {
	return nil, errors.New("no")
}

func (self *refusingMigrator) Migrate() error {
	self.migrated = true
	return nil
}
