// Package dbtest provides test helpers for database operations.
package dbtest

import (
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("dbtest") //nolint:unused

func closeDatabase(database *gorm.DB) error {
	sqlDb, err := database.DB()
	if err != nil {
		return err
	}
	return sqlDb.Close()
}

func createDatabase(settings *db.Settings) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", settings.Host, settings.Port, settings.User, settings.Password)
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	if err := database.Exec(fmt.Sprintf("CREATE DATABASE %q", settings.DBName)).Error; err != nil {
		return err
	}
	if err := closeDatabase(database); err != nil {
		return err
	}
	return nil
}

func dropDatabase(settings *db.Settings) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", settings.Host, settings.Port, settings.User, settings.Password)
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	if err := database.Exec(fmt.Sprintf("DROP DATABASE %q", settings.DBName)).Error; err != nil {
		return err
	}
	if err := closeDatabase(database); err != nil {
		return err
	}
	return nil
}

// AcquireDatabase acquires a db.Database for testing.
func AcquireDatabase(test testing.TB) (db.Database, func()) {
	test.Helper()

	host := os.Getenv("TEANODE_TEST_DATABASE_HOST")
	if host == "" {
		test.Skipf("environment variable TEANODE_TEST_DATABASE_HOST is not set, skipping tests that require database")
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
		test.Fatalf("failed to create database: %s", err)
	}
	database, err := db.Open(settings)
	if err != nil {
		test.Fatalf("failed to open test database: %s", err)
	}
	if err := database.Migrate(); err != nil {
		test.Fatalf("failed to migrate database: %s", err)
	}
	return database, func() {
		if err := database.Close(); err != nil {
			test.Fatalf("failed to close database: %s", err)
		}
		if err := dropDatabase(settings); err != nil {
			test.Fatalf("failed to drop database: %s", err)
		}
	}
}

// RunTransaction runs a db.Transaction for testing.
func RunTransaction(test *testing.T, run func(db.Transaction)) {
	test.Helper()

	database, releaseDatabase := AcquireDatabase(test)
	defer releaseDatabase()

	if err := database.Transaction(func(tx db.Transaction) error {
		run(tx)
		return nil
	}); err != nil {
		test.Fatalf("failed to run transaction: %s", err)
	}
}

// CreateUser stores one account and returns its identifier.
//
// Sessions, tokens and passkeys all reference an account, and the database
// enforces it, so a test that stores one needs an account for it to belong to.
func CreateUser(test *testing.T, database db.Database, username string) string {
	test.Helper()

	var id string
	if err := database.Transaction(func(tx db.Transaction) error {
		user, err := tx.CreateUser(&models.User{
			Username:     username,
			PasswordHash: "$2a$12$notarealhashbutthecolumnisnotnull.....................",
		})
		if err != nil {
			return err
		}
		id = user.ID
		return nil
	}); err != nil {
		test.Fatalf("failed to store the account %q: %s", username, err)
	}
	return id
}

// QueryString reads one string cell straight from the database, for a test
// that has to see what is stored rather than what is read back.
func QueryString(test testing.TB, database db.Database, query string) string {
	test.Helper()
	value, err := database.(interface{ RawQueryString(string) (string, error) }).RawQueryString(query)
	if err != nil {
		test.Fatalf("query failed: %s", err)
	}
	return value
}

// RunTransactionOn runs a db.Transaction against a database the test already
// holds, so several transactions can see each other's commits.
func RunTransactionOn(test *testing.T, database db.Database, run func(db.Transaction)) {
	test.Helper()

	if err := database.Transaction(func(tx db.Transaction) error {
		run(tx)
		return nil
	}); err != nil {
		test.Fatalf("failed to run transaction: %s", err)
	}
}

// Exec runs one statement straight against the database, for a test that
// has to put a row there that nothing else would write.
func Exec(test testing.TB, database db.Database, statement string) {
	test.Helper()
	if err := database.(interface{ RawExec(string) error }).RawExec(statement); err != nil {
		test.Fatalf("statement failed: %s", err)
	}
}

// photoClaimedAt is when the photo was last claimed by a worker, claiming it
// first by a statement when nobody has, as the claim a worker makes would.
func photoClaimedAt(test testing.TB, database db.Database, photoId string) time.Time {
	test.Helper()
	read := func() string {
		return QueryString(test, database, `SELECT COALESCE(to_char(processing_claimed_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '') FROM photo WHERE id = '`+photoId+`'`)
	}
	if read() == "" {
		Exec(test, database, `UPDATE photo SET processing_claimed_at = date_trunc('microseconds', now()) WHERE id = '`+photoId+`'`)
	}
	claimedAt, err := time.Parse(time.RFC3339Nano, read())
	if err != nil {
		test.Fatalf("the claim of photo %s could not be read: %s", photoId, err)
	}
	return claimedAt
}

// MarkPhotoReady records that a photo is ready, as the worker that claimed it
// does; a photo nobody claimed is claimed first.
func MarkPhotoReady(test *testing.T, database db.Database, photoId string) {
	test.Helper()
	claimedAt := photoClaimedAt(test, database, photoId)
	RunTransactionOn(test, database, func(transaction db.Transaction) {
		if isCurrent, err := transaction.MarkPhotoReady(photoId, claimedAt); err != nil || !isCurrent {
			test.Fatalf("photo %s was not marked ready: %v %v", photoId, isCurrent, err)
		}
	})
}

// MarkPhotoProcessingFailed records that processing a photo failed, as the
// worker that claimed it does; a photo nobody claimed is claimed first.
func MarkPhotoProcessingFailed(test *testing.T, database db.Database, photoId, errorMessage string, isFileAtFault bool, maximumAttemptCount int) {
	test.Helper()
	claimedAt := photoClaimedAt(test, database, photoId)
	RunTransactionOn(test, database, func(transaction db.Transaction) {
		if isCurrent, err := transaction.MarkPhotoProcessingFailed(photoId, claimedAt, errorMessage, isFileAtFault, maximumAttemptCount); err != nil || !isCurrent {
			test.Fatalf("photo %s was not marked failed: %v %v", photoId, isCurrent, err)
		}
	})
}
