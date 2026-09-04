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

	if allowed, _ := strconv.ParseBool(os.Getenv(AllowMigrationRevert)); allowed {
		log.Warningf("reverting %d migration(s) this version does not have, because %s is set: %s",
			len(unknown), AllowMigrationRevert, strings.Join(unknown, ", "))
		if err := database.Migrate(); err != nil {
			return fmt.Errorf("cannot migrate the database: %w", err)
		}
		return nil
	}

	return fmt.Errorf("this database was migrated by a newer version of teanode (%s), and going back "+
		"means reverting those migrations and losing what is in the columns they added. Nothing has "+
		"been changed and nothing has been opened.%s To go back to this version anyway, set %s=true",
		strings.Join(unknown, ", "), stagedAdvice(upgradeDirectory), AllowMigrationRevert)
}

// stagedAdvice adds the way out that does not lose anything, when there is
// one: a newer binary is sitting in the staging directory and this start
// refused to run it, so running it again is what the operator actually wants.
func stagedAdvice(upgradeDirectory string) string {
	if !upgrade.Waiting(upgradeDirectory) {
		return ""
	}
	return fmt.Sprintf(" %s holds an upgraded binary that this start refused to run — the reason is "+
		"above; removing %s makes it try again, and that keeps everything.",
		upgradeDirectory, upgrade.PendingMarker(upgradeDirectory))
}

// migrator is the two things migrate needs of a database. Narrow so that the
// refusal above can be exercised without one.
type migrator interface {
	UnknownMigrations() ([]string, error)
	Migrate() error
}
