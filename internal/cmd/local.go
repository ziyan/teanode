package cmd

import (
	"fmt"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/configdb"
	"github.com/ziyan/teanode/internal/db"
)

// OpenLocalStore opens the configuration where the server keeps it: the
// database named by the environment.
//
// This is what "on the server itself, nothing has to be set up first" means
// now. The command reads the same variables the server reads, so running it
// in the same container, or with the same env file, reaches the same
// configuration. From anywhere else, use --url and a token.
//
// The caller closes the returned store and then calls the returned function,
// which closes the database under it.
func OpenLocalStore() (config.Store, func(), error) {
	bootstrapped, err := bootstrap.Load()
	if err != nil {
		return nil, nil, err
	}

	database, closeDatabase, err := OpenBootstrapDatabase(bootstrapped)
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

// OpenBootstrapDatabase connects to the database the environment names,
// without migrating it. The caller calls the returned function to close it.
func OpenBootstrapDatabase(bootstrapped *bootstrap.Bootstrap) (db.Database, func(), error) {
	database, err := db.Open(&db.Settings{
		Host:     bootstrapped.Database.Host,
		Port:     bootstrapped.Database.Port,
		User:     bootstrapped.Database.User,
		Password: bootstrapped.Database.Password,
		DBName:   bootstrapped.Database.Name,
		SSLMode:  bootstrapped.Database.SSLMode,

		SSLRootCertificate: bootstrapped.Database.SSLRootCertificate,
		LogQueries:         bootstrapped.Database.LogQueries,
		BackendID:          bootstrapped.InstanceID,
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

// LoadLocalConfiguration reads the stored configuration for a command that
// only looks at it.
func LoadLocalConfiguration() (*config.Configuration, error) {
	store, closeDatabase, err := OpenLocalStore()
	if err != nil {
		return nil, err
	}
	defer closeDatabase()
	defer func() {
		_ = store.Close()
	}()
	return store.Current(), nil
}

// UpdateLocalConfiguration changes the stored configuration directly, for the
// commands that have to work when the server will not start or nobody can log
// in.
//
// Writing straight to the database while a server is running used to be the
// thing that lost a change, back when the server held the whole configuration
// in memory and rewrote a file from it. It is safe now: the write is checked
// against the version it was based on, and every running instance notices the
// new version within seconds and reloads. That is the same path the dashboard
// takes, one layer lower down.
func UpdateLocalConfiguration(mutate func(*config.Configuration) error) error {
	store, closeDatabase, err := OpenLocalStore()
	if err != nil {
		return err
	}
	defer closeDatabase()
	defer func() {
		_ = store.Close()
	}()
	return store.Update(mutate)
}
