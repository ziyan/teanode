package db

import (
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"
)

// openDeadlineTestTransaction is a bare transaction on the test server, for
// looking at the settings a change of Passwords sets and puts back.
func openDeadlineTestTransaction(test *testing.T) *gorm.DB {
	test.Helper()
	host := os.Getenv("TEANODE_TEST_DATABASE_HOST")
	if host == "" {
		test.Skipf("environment variable TEANODE_TEST_DATABASE_HOST is not set, skipping tests that require database")
	}
	opened, err := Open(&Settings{Host: host, Port: 5432, User: "teanode", Password: "teanode", DBName: "teanode"})
	if err != nil {
		test.Fatalf("Open: %s", err)
	}
	test.Cleanup(func() { _ = opened.Close() })
	tx := opened.(*database).db.Begin()
	if tx.Error != nil {
		test.Fatalf("Begin: %s", tx.Error)
	}
	test.Cleanup(func() { tx.Rollback() })
	return tx
}

func lockTimeoutOf(test *testing.T, tx *gorm.DB) string {
	test.Helper()
	var setting string
	if err := tx.Raw(`SELECT current_setting('lock_timeout')`).Scan(&setting).Error; err != nil {
		test.Fatalf("lock_timeout: %s", err)
	}
	return setting
}

// A change of Passwords sets the time it may wait for locks and puts the
// setting back, whether it succeeded or was refused, so that the rest of the
// transaction (a circle change that ended shares) does not wait under whatever
// was left of it.
func TestAChangeOfPasswordsPutsBackTheLockTimeout(test *testing.T) {
	tx := openDeadlineTestTransaction(test)
	before := lockTimeoutOf(test, tx)

	inside := ""
	if err := runPasswordsChange(tx, func() error {
		time.Sleep(50 * time.Millisecond)
		if err := limitPasswordsLockWait(tx); err != nil {
			return err
		}
		inside = lockTimeoutOf(test, tx)
		return nil
	}); err != nil {
		test.Fatalf("runPasswordsChange: %s", err)
	}
	if inside == before || inside == "5s" {
		test.Errorf("inside the change lock_timeout was %q (before it %q), expected what was left of 5 s", inside, before)
	}
	if got := lockTimeoutOf(test, tx); got != before {
		test.Errorf("after the change lock_timeout is %q, expected %q", got, before)
	}

	refusal := errors.New("refused")
	if err := runPasswordsChange(tx, func() error {
		if err := limitPasswordsLockWait(tx); err != nil {
			return err
		}
		return refusal
	}); !errors.Is(err, refusal) {
		test.Fatalf("runPasswordsChange: %v, expected the refusal", err)
	}
	if got := lockTimeoutOf(test, tx); got != before {
		test.Errorf("after a refused change lock_timeout is %q, expected %q", got, before)
	}
	// And the transaction can go on.
	if got := lockTimeoutOf(test, tx); got == "" {
		test.Error("the transaction cannot go on")
	}
}

// The deadline is one for the whole change: a wait later in it gets only what
// is left, never a fresh five seconds, and never zero, which would be no limit.
func TestTheLockWaitsOfOneChangeShareOneDeadline(test *testing.T) {
	tx := openDeadlineTestTransaction(test)
	waitOf := func() time.Duration {
		if err := limitPasswordsLockWait(tx); err != nil {
			test.Fatalf("limitPasswordsLockWait: %s", err)
		}
		wait, err := time.ParseDuration(lockTimeoutOf(test, tx))
		if err != nil {
			test.Fatalf("lock_timeout: %s", err)
		}
		return wait
	}
	if err := runPasswordsChange(tx, func() error {
		first := waitOf()
		time.Sleep(300 * time.Millisecond)
		// A change inside this one shares the outer deadline.
		return runPasswordsChange(tx, func() error {
			second := waitOf()
			if first > passwordsLockTimeout || first < passwordsLockTimeout-200*time.Millisecond {
				test.Errorf("the first wait was limited to %s, expected about %s", first, passwordsLockTimeout)
			}
			if second > first-250*time.Millisecond {
				test.Errorf("the second wait was limited to %s, expected at least 250 ms less than the first (%s)", second, first)
			}
			time.Sleep(passwordsLockTimeout)
			if third := waitOf(); third != time.Millisecond {
				test.Errorf("a wait after the deadline was limited to %s, expected 1ms", third)
			}
			return nil
		})
	}); err != nil {
		test.Fatalf("runPasswordsChange: %s", err)
	}
}
