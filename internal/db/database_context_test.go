package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCancelledTransactionDoesNotStart(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hasStarted := false
	err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		hasStarted = true
		_, err := transaction.CreateUser(&models.User{Username: "cancelled-fixture"})
		return err
	})
	if err == nil || hasStarted {
		test.Fatalf("cancelled transaction started: %v, error: %v", hasStarted, err)
	}
}

func TestCancellationRollsBackWrites(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if _, err := transaction.CreateUser(&models.User{Username: "cancelled-fixture"}); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if err == nil {
		test.Fatal("cancelled write committed")
	}
	found, err := database.GetUserByUsername("cancelled-fixture")
	if err != nil {
		test.Fatal(err)
	}
	if found != nil {
		test.Fatal("cancelled transaction left a user behind")
	}
}

func TestReopenedTransactionKeepsCancellation(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if _, err := transaction.CreateUser(&models.User{Username: "committed-fixture"}); err != nil {
			return err
		}
		if err := transaction.Commit(); err != nil {
			return err
		}
		cancel()
		_, err := transaction.CreateUser(&models.User{Username: "cancelled-fixture"})
		return err
	})
	if err == nil {
		test.Fatal("reopened transaction ignored cancellation")
	}
	committed, err := database.GetUserByUsername("committed-fixture")
	if err != nil || committed == nil {
		test.Fatalf("explicit commit was lost: %v", err)
	}
	cancelled, err := database.GetUserByUsername("cancelled-fixture")
	if err != nil || cancelled != nil {
		test.Fatalf("cancelled write survived: %v", err)
	}
}

func TestCancellationInterruptsLockWait(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "locked-fixture")
	hasLocked := make(chan struct{})
	releaseLock := make(chan struct{})
	lockOutcome := make(chan error, 1)
	go func() {
		lockOutcome <- database.Transaction(func(transaction db.Transaction) error {
			if _, err := transaction.UpdateUser(userId, func(user *models.User) error { user.Name = "held"; return nil }); err != nil {
				return err
			}
			close(hasLocked)
			<-releaseLock
			return nil
		})
	}()
	defer func() {
		close(releaseLock)
		if err := <-lockOutcome; err != nil {
			test.Errorf("lock holder: %v", err)
		}
	}()
	select {
	case <-hasLocked:
	case <-time.After(5 * time.Second):
		test.Fatal("lock holder did not become ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		completed <- database.TransactionContext(ctx, func(transaction db.Transaction) error {
			_, err := transaction.UpdateUser(userId, func(user *models.User) error { user.Name = "waiting"; return nil })
			return err
		})
	}()
	select {
	case err := <-completed:
		if err == nil {
			test.Fatal("locked write unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		test.Fatal("cancelled SQL kept waiting for the lock")
	}
}
