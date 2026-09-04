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

// The one case where migrating is the wrong thing to do.
//
// Migrate reverts what it does not recognise, which is how a downgrade works
// here. When a newer binary is staged beside this one and was refused — a
// release that migrated the database and then crashed before serving, say —
// carrying on would drop the columns it added and everything in them, and the
// upgrade is sitting right there waiting to be run again.
func TestMigrateRefusesToRevertAStagedUpgrade(t *testing.T) {
	directory := stageBinary(t)
	database := &fakeMigrator{unknown: []string{"0042_something_new"}}

	err := migrate(database, directory)
	if err == nil {
		t.Fatal("it reverted an upgrade that is installed and waiting")
	}
	if database.migrated {
		t.Error("it migrated anyway")
	}
	// The message has to be enough to act on: which migrations, where the
	// binary is, and the two ways out.
	for _, want := range []string{"0042_something_new", directory, "pending"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// A downgrade somebody asked for still works. Nothing is staged, so there is
// no upgrade to undo — only an older binary that was deliberately started.
func TestMigrateStillRevertsADeliberateDowngrade(t *testing.T) {
	database := &fakeMigrator{unknown: []string{"0042_something_new"}}

	if err := migrate(database, t.TempDir()); err != nil {
		t.Fatalf("a deliberate downgrade was refused: %s", err)
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

// Not reachable through the fake above, but the shape matters: a database that
// cannot say what it holds must not be migrated on the assumption that it
// holds nothing.
func TestMigrateStopsWhenItCannotAsk(t *testing.T) {
	database := &refusingMigrator{}
	if err := migrate(database, stageBinary(t)); err == nil {
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
