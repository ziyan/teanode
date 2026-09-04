package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/configdb"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/version"
)

// openLocalStore opens the configuration where the server keeps it: the
// database named by the environment.
//
// This is what "on the server itself, nothing has to be set up first" means
// now. The command reads the same variables the server reads, so running it
// in the same container, or with the same env file, reaches the same
// configuration. From anywhere else, use --url and a token.
//
// The caller closes the returned store and then calls the returned function,
// which closes the database under it.
func openLocalStore() (config.Store, func(), error) {
	bootstrapped, err := bootstrap.Load()
	if err != nil {
		return nil, nil, err
	}

	database, closeDatabase, err := openBootstrapDatabase(bootstrapped)
	if err != nil {
		return nil, nil, err
	}

	// Deliberately not migrating. A command line tool that silently changes
	// the schema of a database a server is running against is a way to be
	// surprised; "teanode run" and "teanode config init" do it, at a moment
	// when the operator is expecting a deployment.
	store, err := configdb.Open(database, bootstrapped.Database)
	if err != nil {
		closeDatabase()
		return nil, nil, fmt.Errorf("cannot read the configuration from the database: %w", err)
	}
	return store, closeDatabase, nil
}

// openBootstrapDatabase connects to the database the environment names,
// without migrating it. The caller calls the returned function to close it.
func openBootstrapDatabase(bootstrapped *bootstrap.Bootstrap) (db.Database, func(), error) {
	database, err := db.Open(&db.Settings{
		Host:       bootstrapped.Database.Host,
		Port:       bootstrapped.Database.Port,
		User:       bootstrapped.Database.User,
		Password:   bootstrapped.Database.Password,
		DBName:     bootstrapped.Database.Name,
		SSLMode:    bootstrapped.Database.SSLMode,
		LogQueries: bootstrapped.Database.LogQueries,
		BackendID:  bootstrapped.InstanceID,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("cannot connect to the database at %s:%d: %w",
			bootstrapped.Database.Host, bootstrapped.Database.Port, err)
	}
	return database, func() {
		if err := database.Close(); err != nil {
			log.Errorf("failed to close database: %s", err)
		}
	}, nil
}

// loadLocalConfiguration reads the stored configuration for a command that
// only looks at it.
func loadLocalConfiguration() (*config.Configuration, error) {
	store, closeDatabase, err := openLocalStore()
	if err != nil {
		return nil, err
	}
	defer closeDatabase()
	defer func() {
		_ = store.Close()
	}()
	return store.Current(), nil
}

// updateLocalConfiguration changes the stored configuration directly, for the
// commands that have to work when the server will not start or nobody can log
// in.
//
// Writing straight to the database while a server is running used to be the
// thing that lost a change, back when the server held the whole configuration
// in memory and rewrote a file from it. It is safe now: the write is checked
// against the version it was based on, and every running instance notices the
// new version within seconds and reloads. That is the same path the dashboard
// takes, one layer lower down.
func updateLocalConfiguration(mutate func(*config.Configuration) error) error {
	store, closeDatabase, err := openLocalStore()
	if err != nil {
		return err
	}
	defer closeDatabase()
	defer func() {
		_ = store.Close()
	}()
	return store.Update(mutate)
}

// AllowMigrationRevert is the variable that says an older binary may undo what
// a newer one did to the database.
const AllowMigrationRevert = bootstrap.Prefix + "ALLOW_MIGRATION_REVERT"

// migrate brings the database up to date, and refuses to bring it backwards
// unless somebody has said to.
//
// Migrate reverts every migration it does not recognise. That is how a
// deliberate downgrade works here — see docs/coding/database-migrations.md —
// and the trouble is that it cannot tell a deliberate downgrade from an
// accidental one, while the two are told apart by what happens next: one loses
// nothing anybody wanted, and the other drops columns and everything in them.
//
// The accidental one has three roads into this program and they are all
// ordinary. A release installed from the dashboard migrates the database and
// then crashes before serving, so the next start refuses it by design and the
// image's older binary carries on. A second instance sharing the database
// never got the upgrade — its own was refused — and restarts for some
// unrelated reason. An operator pulls last week's image to test something.
//
// So it is opt-in now. Unknown migrations stop the program, and the message
// says which, what would be lost, and the variable to set. That turns a silent
// loss into a start that does not happen, which is the trade a mail server
// should make: the queue is on disk and senders retry, and a dropped column
// does not come back.
func migrate(database migrator, upgradeDirectory string) error {
	unknown, err := database.UnknownMigrations()
	if err != nil {
		return fmt.Errorf("cannot read which migrations this database has: %w", err)
	}
	if len(unknown) == 0 {
		if err := database.Migrate(); err != nil {
			return fmt.Errorf("cannot migrate the database: %w", err)
		}
		return nil
	}

	allowed, err := revertAllowed()
	if err != nil {
		return err
	}
	if allowed {
		log.Warningf("reverting %d migration(s) this version does not have, because %s is set: %s",
			len(unknown), AllowMigrationRevert, strings.Join(unknown, ", "))
		if err := database.Migrate(); err != nil {
			return fmt.Errorf("cannot migrate the database: %w", err)
		}
		return nil
	}

	return fmt.Errorf("this database was migrated by a newer version of teanode (%s), and this one does "+
		"not have those migrations. Nothing has been changed and nothing has been opened.\n\n"+
		"The way out that loses nothing is to run that newer version here — upgrade this instance, or "+
		"pull the image the rest of the deployment is on.%s\n\n"+
		"To go back to this version instead, set %s=true. Read that as what it is: those migrations are "+
		"reverted and whatever is in the columns they added is gone. If another instance is sharing this "+
		"database and is already running the newer version, do not do it at all — the columns would go "+
		"out from under it while it is serving",
		strings.Join(unknown, ", "), stagedAdvice(upgradeDirectory, version.Version()), AllowMigrationRevert)
}

// revertAllowed reads the variable that permits going backwards.
//
// A value it cannot read is an error rather than a false. The refusal below is
// the only guidance anybody has at that point, and it ends by naming this
// variable — so somebody who sets it to "yes" and is handed the same three
// paragraphs back has been told to do the thing they just did.
func revertAllowed() (bool, error) {
	value, ok := os.LookupEnv(AllowMigrationRevert)
	if !ok || strings.TrimSpace(value) == "" {
		return false, nil
	}
	allowed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s is %q, which is not true or false", AllowMigrationRevert, value)
	}
	return allowed, nil
}

// stagedAdvice adds the way out that does not lose anything, when this start
// is one remedy away from it: a newer binary is sitting in the staging
// directory and was held back only by the marker saying an earlier attempt did
// not get as far as serving.
//
// Only for that one reason. A staged binary is left in place when its version
// or checksum cannot be read, when the checksum does not match, when it is not
// executable, and when the permissions are wrong — and in every one of those,
// removing the marker changes nothing and the server still will not start.
// Telling somebody that is the way out, when it is not, is worse than saying
// nothing: they do it, it fails again, and now they distrust the message that
// was going to tell them the truth about reverting.
func stagedAdvice(upgradeDirectory, current string) string {
	if !upgrade.HeldBackByMarker(upgradeDirectory, current) {
		return ""
	}
	return fmt.Sprintf(" There is one at %s: it was tried and did not get as far as serving, so this "+
		"start left it alone. Remove %s to let it try again.",
		upgrade.Staged(upgradeDirectory), upgrade.PendingMarker(upgradeDirectory))
}

// migrator is the two things migrate needs of a database. Narrow so that the
// refusal above can be exercised without one.
type migrator interface {
	UnknownMigrations() ([]string, error)
	Migrate() error
}
