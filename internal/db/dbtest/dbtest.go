// Package dbtest provides test helpers for database operations.
package dbtest

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/security"
)

func closeDatabase(d *gorm.DB) error {
	sqlDb, err := d.DB()
	if err != nil {
		return err
	}
	return sqlDb.Close()
}

func createDatabase(settings *db.Settings) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", settings.Host, settings.Port, settings.User, settings.Password)
	d, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	if err := d.Exec(fmt.Sprintf("CREATE DATABASE %q", settings.DBName)).Error; err != nil {
		return err
	}
	if err := closeDatabase(d); err != nil {
		return err
	}
	return nil
}

func dropDatabase(settings *db.Settings) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", settings.Host, settings.Port, settings.User, settings.Password)
	d, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	if err := d.Exec(fmt.Sprintf("DROP DATABASE %q", settings.DBName)).Error; err != nil {
		return err
	}
	if err := closeDatabase(d); err != nil {
		return err
	}
	return nil
}

// AcquireDatabase acquires a db.Database for testing.
func AcquireDatabase(t *testing.T) (db.Database, func()) {
	t.Helper()

	host := os.Getenv("TEANODE_TEST_DATABASE_HOST")
	if host == "" {
		t.Skipf("environment variable TEANODE_TEST_DATABASE_HOST is not set, skipping tests that require database")
	}
	settings := &db.Settings{
		Host:      host,
		Port:      5432,
		User:      "teanode",
		Password:  "teanode",
		DBName:    fmt.Sprintf("teanodetest%s", security.NewULID()),
		BackendID: "test1",
	}
	if err := createDatabase(settings); err != nil {
		t.Fatalf("failed to create database: %s", err)
	}
	database, err := db.Open(settings)
	if err != nil {
		t.Fatalf("failed to open test database: %s", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("failed to migrate database: %s", err)
	}
	return database, func() {
		if err := database.Close(); err != nil {
			t.Fatalf("failed to close database: %s", err)
		}
		if err := dropDatabase(settings); err != nil {
			t.Fatalf("failed to drop database: %s", err)
		}
	}
}

// RunTransaction runs a db.Transaction for testing.
func RunTransaction(t *testing.T, f func(db.Transaction)) {
	t.Helper()

	database, releaseDatabase := AcquireDatabase(t)
	defer releaseDatabase()

	if err := database.Transaction(func(tx db.Transaction) error {
		f(tx)
		return nil
	}); err != nil {
		t.Fatalf("failed to run transaction: %s", err)
	}
}

// CreateUser stores one account and returns its identifier.
//
// Sessions, tokens and passkeys all reference an account, and the database
// enforces it, so a test that stores one needs an account for it to belong to.
// Through SaveConfiguration because that is the only writer of the table: the
// accounts are part of the configuration, and a test inserting behind its back
// would be testing a schema nothing else writes.
func CreateUser(t *testing.T, database db.Database, username string) string {
	t.Helper()

	rows, err := database.LoadConfiguration()
	if err != nil {
		t.Fatalf("failed to load the configuration: %s", err)
	}
	id := security.NewULID()
	rows.Users = append(rows.Users, &db.UserRow{
		ID:           id,
		Username:     username,
		PasswordHash: "$2a$12$notarealhashbutthecolumnisnotnull.....................",
	})
	if _, err := database.SaveConfiguration(rows); err != nil {
		t.Fatalf("failed to store the account %q: %s", username, err)
	}
	return id
}
