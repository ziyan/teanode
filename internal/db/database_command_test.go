package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCommandFailurePreservesSuccessfulSiblings(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	if err := database.Transaction(func(transaction db.Transaction) error {
		if _, err := transaction.CreateUser(&models.User{Username: "before-command"}); err != nil {
			return err
		}
		err := transaction.TransactionContext(context.Background(), func(command db.Transaction) error {
			if _, err := command.CreateUser(&models.User{Username: "failed-command"}); err != nil {
				return err
			}
			// A SQL constraint error aborts the PostgreSQL subtransaction,
			// so this also checks that rollback restores a usable connection.
			_, err := command.CreateUser(&models.User{Username: "before-command"})
			return err
		})
		if !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("command error = %v", err)
		}
		_, err = transaction.CreateUser(&models.User{Username: "after-command"})
		return err
	}); err != nil {
		test.Fatal(err)
	}
	for username, shouldExist := range map[string]bool{"before-command": true, "failed-command": false, "after-command": true} {
		user, err := database.GetUserByUsername(username)
		if err != nil || (user != nil) != shouldExist {
			test.Fatalf("user %q exists = %v, want %v; error = %v", username, user != nil, shouldExist, err)
		}
	}
}

func TestCancelledCommandRollsBackWithoutCancellingParent(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	if err := database.Transaction(func(transaction db.Transaction) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := transaction.TransactionContext(ctx, func(command db.Transaction) error {
			if _, err := command.CreateUser(&models.User{Username: "cancelled-command"}); err != nil {
				return err
			}
			cancel()
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			test.Fatalf("command error = %v", err)
		}
		_, err = transaction.CreateUser(&models.User{Username: "surviving-command"})
		return err
	}); err != nil {
		test.Fatal(err)
	}
	user, err := database.GetUserByUsername("cancelled-command")
	if err != nil || user != nil {
		test.Fatalf("cancelled command survived: %v, %v", user, err)
	}
}

func TestSuccessfulCommandStillBelongsToOuterTransaction(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	outerFailure := errors.New("outer failure")
	err := database.Transaction(func(transaction db.Transaction) error {
		if err := transaction.TransactionContext(context.Background(), func(command db.Transaction) error {
			if err := command.Commit(); err == nil {
				test.Fatal("command committed the parent transaction")
			}
			_, err := command.CreateUser(&models.User{Username: "uncommitted-command"})
			return err
		}); err != nil {
			return err
		}
		return outerFailure
	})
	if !errors.Is(err, outerFailure) {
		test.Fatal(err)
	}
	user, err := database.GetUserByUsername("uncommitted-command")
	if err != nil || user != nil {
		test.Fatalf("command escaped parent rollback: %v, %v", user, err)
	}
}

func TestPanickingCommandRollsBackBeforeParentContinues(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	if err := database.Transaction(func(transaction db.Transaction) error {
		func() {
			defer func() {
				if recovered := recover(); recovered != "command failure" {
					test.Fatalf("unexpected panic: %v", recovered)
				}
			}()
			_ = transaction.TransactionContext(context.Background(), func(command db.Transaction) error {
				if _, err := command.CreateUser(&models.User{Username: "panicking-command"}); err != nil {
					test.Fatal(err)
				}
				panic("command failure")
			})
		}()
		_, err := transaction.CreateUser(&models.User{Username: "after-panic"})
		return err
	}); err != nil {
		test.Fatal(err)
	}
	user, err := database.GetUserByUsername("panicking-command")
	if err != nil || user != nil {
		test.Fatalf("panicking command survived: %v, %v", user, err)
	}
}
