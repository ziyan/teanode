package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestAfterCommitDiscardsFailedCommandsAndWaitsForParent(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var calls []string
	injectedErr := errors.New("command failed")
	if err := database.Transaction(func(transaction db.Transaction) error {
		transaction.AfterCommit(func() { calls = append(calls, "parent") })
		if err := transaction.TransactionContext(context.Background(), func(command db.Transaction) error {
			command.AfterCommit(func() { calls = append(calls, "failed") })
			return injectedErr
		}); !errors.Is(err, injectedErr) {
			return errors.New("command failure was lost")
		}
		if err := transaction.TransactionContext(context.Background(), func(command db.Transaction) error {
			command.AfterCommit(func() { calls = append(calls, "child") })
			return nil
		}); err != nil {
			return err
		}
		if err := transaction.TransactionContext(context.Background(), func(command db.Transaction) error {
			if err := command.TransactionContext(context.Background(), func(nested db.Transaction) error {
				nested.AfterCommit(func() { calls = append(calls, "discarded grandchild") })
				return nil
			}); err != nil {
				return err
			}
			return injectedErr
		}); !errors.Is(err, injectedErr) {
			return errors.New("outer command failure was lost")
		}
		transaction.AfterCommit(func() { calls = append(calls, "last") })
		if len(calls) != 0 {
			return errors.New("callbacks ran before the parent committed")
		}
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	if strings.Join(calls, ",") != "parent,child,last" {
		test.Fatalf("commit callbacks = %v", calls)
	}
}

func TestAfterCommitDoesNotRunOnRollbackOrCancelledCommit(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	for _, shouldCancel := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		callbackCount := 0
		err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
			if err := transaction.TransactionContext(ctx, func(command db.Transaction) error {
				command.AfterCommit(func() { callbackCount++ })
				return nil
			}); err != nil {
				return err
			}
			if shouldCancel {
				cancel()
				return nil
			}
			return errors.New("parent rolled back")
		})
		cancel()
		if err == nil || callbackCount != 0 {
			test.Fatalf("failed transaction ran %d callbacks, error = %v", callbackCount, err)
		}
	}
}

func TestAfterCommitRunsOnceAcrossManualCommitAndReopen(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	callbackCount := 0
	injectedErr := errors.New("later transaction failed")
	err := database.Transaction(func(transaction db.Transaction) error {
		transaction.AfterCommit(func() { callbackCount++ })
		if err := transaction.Commit(); err != nil {
			return err
		}
		if callbackCount != 1 {
			return errors.New("manual commit did not flush callbacks")
		}
		transaction.AfterCommit(func() { callbackCount++ })
		return injectedErr
	})
	if !errors.Is(err, injectedErr) || callbackCount != 1 {
		test.Fatalf("callbacks = %d, error = %v", callbackCount, err)
	}
}
