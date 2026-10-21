package db_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Ada shares a note with Ben. Whether Ben leaves the circle they share or his
// account is deleted, the share and its key end, the note is flagged for a new
// key and gets a new revision, so that Ada's devices see it and seal a new key
// that leaves him out.
func TestDeletingAnAccountEndsItsPasswordsSharesAsLeavingACircleDoes(test *testing.T) {
	for _, scenario := range []string{"the recipient leaves the circle", "the recipient's account is deleted"} {
		test.Run(scenario, func(test *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(test)
			defer closeDatabase()
			var world *shareWorld
			var item *models.PasswordItem
			dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
				world = newPasswordsWorld(test, tx)
				setUpPasswordsFor(test, tx, world.ada)
				setUpPasswordsFor(test, tx, world.ben)
				item = shareItemWith(test, tx, world.ada, world.ben, 1, 100)
			})
			var before *models.PasswordItem
			dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
				var err error
				if before, err = tx.GetPasswordItem(world.ada.ID, item.ID); err != nil {
					test.Fatal(err)
				}
			})
			if len(before.Shares) != 1 || before.NeedsNewKey {
				test.Fatalf("before: %d shares, needsNewKey %v", len(before.Shares), before.NeedsNewKey)
			}
			dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
				var err error
				if scenario == "the recipient leaves the circle" {
					_, err = tx.RemoveCircleMember(world.home.ID, world.ben.ID)
				} else {
					err = tx.DeleteUser(world.ben.ID)
				}
				if err != nil {
					test.Fatal(err)
				}
			})
			dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
				after, err := tx.GetPasswordItem(world.ada.ID, item.ID)
				if err != nil {
					test.Fatal(err)
				}
				if len(after.Shares) != 0 {
					test.Errorf("%d shares are left", len(after.Shares))
				}
				if !after.NeedsNewKey {
					test.Error("the item is not flagged for a new key")
				}
				if after.Revision <= before.Revision {
					test.Errorf("the revision stayed at %d", after.Revision)
				}
				list := mustListPasswordItems(test, tx, world.ada, before.Revision)
				if len(list.Items) != 1 {
					test.Errorf("%d items changed since the old revision, expected the one", len(list.Items))
				}
			})
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM password_share_key WHERE item_id = '`+item.ID+`'`); count != "0" {
				test.Errorf("%s keys are left for the item", count)
			}
		})
	}
}

// The deleted account's own items go with it, and the people they were shared
// with no longer see them or hold a key.
func TestDeletingAnOwnerRemovesWhatWasSharedWithOthers(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item = shareItemWith(test, tx, world.ada, world.ben, 1, 100)
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if err := tx.DeleteUser(world.ada.ID); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if list := mustListPasswordItems(test, tx, world.ben, 0); len(list.Items) != 0 {
			test.Errorf("Ben still lists %d items", len(list.Items))
		}
	})
	for _, table := range []string{"password_share", "password_share_key"} {
		if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM `+table+` WHERE item_id = '`+item.ID+`'`); count != "0" {
			test.Errorf("%s rows are left in %s", count, table)
		}
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM password_item WHERE id = '`+item.ID+`'`); count != "0" {
		test.Error("the deleted owner's item is still there")
	}
}

// A person who has nothing in Passwords (no items, shares or keys) leaves a
// circle without waiting for the revision lock, which a listing in flight
// holds shared. A person who has an item still waits and is told Passwords is
// busy, as before.
func TestLeavingACircleWithNothingInPasswordsDoesNotWaitForTheLock(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.cora)
		setUpPasswordsFor(test, tx, world.ben)
		savePasswordItemFor(test, tx, world.ben, 1, models.PasswordItemKindNote)
	})
	holding, release := make(chan struct{}), make(chan struct{})
	var holder sync.WaitGroup
	holder.Add(1)
	go func() {
		defer holder.Done()
		_ = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.ListPasswordItems(world.cora.ID, 0)
			close(holding)
			<-release
			return err
		})
	}()
	<-holding
	defer func() {
		close(release)
		holder.Wait()
	}()

	// Dave has never used Passwords: the circle change goes through at once.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.AddCircleMember(world.home.ID, world.dave.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatal(err)
		}
	})
	started := time.Now()
	var leaveErr error
	if err := database.Transaction(func(tx db.Transaction) error {
		_, leaveErr = tx.RemoveCircleMember(world.home.ID, world.dave.ID)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	if leaveErr != nil {
		test.Errorf("leaving with nothing in Passwords: %v", leaveErr)
	}
	if waited := time.Since(started); waited > 2*time.Second {
		test.Errorf("leaving waited %s for a lock it does not need", waited)
	}

	// Ben has an item, so ending his shares takes the lock and waits.
	var benErr error
	if err := database.Transaction(func(tx db.Transaction) error {
		_, benErr = tx.RemoveCircleMember(world.home.ID, world.ben.ID)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	if !errors.Is(benErr, db.ErrPasswordsBusy) {
		test.Errorf("leaving with an item behind a held lock: %v, expected ErrPasswordsBusy", benErr)
	}
}
