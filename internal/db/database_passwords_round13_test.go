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

// holdPasswordsLocks keeps the locks every change of Passwords takes in a
// transaction of its own, until the returned function is called.
func holdPasswordsLocks(test *testing.T, database db.Database, holder *models.User) (release func()) {
	test.Helper()
	holding, released := make(chan struct{}), make(chan struct{})
	var finished sync.WaitGroup
	finished.Add(1)
	go func() {
		defer finished.Done()
		_ = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(900), OwnerUserID: holder.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			close(holding)
			<-released
			return err
		})
	}()
	<-holding
	return func() {
		close(released)
		finished.Wait()
	}
}

// A resolver that is refused answers with an error, and the request's own
// transaction still commits. Every change of Passwords that waits for the
// locks and gives up must leave that transaction able to commit: it is
// ErrPasswordsBusy ("Passwords is busy with other changes"), not a failed
// commit that reads as a server fault.
func TestEveryChangeThatTimesOutOnTheLocksLeavesTheTransactionAbleToCommit(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	var item *models.PasswordItem
	var deletedItem *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		deletedItem = savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindNote)
		if _, err := tx.DeletePasswordItem(world.ada.ID, deletedItem.ID, true); err != nil {
			test.Fatal(err)
		}
	})
	release := holdPasswordsLocks(test, database, world.ben)

	changes := map[string]func(tx db.Transaction) error{
		"save": func(tx db.Transaction) error {
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(3), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			return err
		},
		"delete": func(tx db.Transaction) error {
			_, err := tx.DeletePasswordItem(world.ada.ID, item.ID, true)
			return err
		},
		"restore": func(tx db.Transaction) error {
			_, err := tx.RestorePasswordItem(world.ada.ID, deletedItem.ID, true)
			return err
		},
		"purge": func(tx db.Transaction) error {
			return tx.PurgePasswordItem(world.ada.ID, deletedItem.ID)
		},
		"restore a version": func(tx db.Transaction) error {
			_, err := tx.RestorePasswordItemVersion(world.ada.ID, item.ID, 1, item.Revision, true)
			return err
		},
		"let the agent use it": func(tx db.Transaction) error {
			_, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, item.ID, false, "")
			return err
		},
		"erase": func(tx db.Transaction) error {
			_, err := tx.ErasePasswords(world.ada.ID)
			return err
		},
	}
	var waiters sync.WaitGroup
	var mutex sync.Mutex
	changeErrs, commitErrs := map[string]error{}, map[string]error{}
	for name, change := range changes {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			var changeErr error
			commitErr := database.Transaction(func(tx db.Transaction) error {
				// The resolver answers with the error, and the transaction commits.
				changeErr = change(tx)
				return nil
			})
			mutex.Lock()
			defer mutex.Unlock()
			changeErrs[name], commitErrs[name] = changeErr, commitErr
		}()
	}
	waiters.Wait()
	release()
	for name := range changes {
		if !errors.Is(changeErrs[name], db.ErrPasswordsBusy) {
			test.Errorf("%s behind a held lock: %v, want ErrPasswordsBusy", name, changeErrs[name])
		}
		if commitErrs[name] != nil {
			test.Errorf("%s: the transaction could not commit after the refusal: %v", name, commitErrs[name])
		}
	}
	// Nothing was changed, and with the locks free the same changes work.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.DeletePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatalf("a delete with the locks free: %v", err)
		}
	})
}

// Once one change of a request gave up on the locks, the rest of its changes
// are refused at once. A document of three changes behind a held lock takes
// about one wait, not three.
func TestAfterOneTimeoutTheRestOfTheTransactionIsRefusedAtOnce(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
	})
	release := holdPasswordsLocks(test, database, world.ben)

	var changeErrs []error
	started := time.Now()
	commitErr := database.Transaction(func(tx db.Transaction) error {
		for index := 0; index < 3; index++ {
			_, err := tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
				ItemID: item.ID, BaseRevision: item.Revision, EncryptedFields: `{"sealed":"again"}`,
				ItemKeyByAccountKey: `{"sealed":"again key"}`, IsSavedByPerson: true,
			})
			changeErrs = append(changeErrs, err)
		}
		_, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: passwordItemIdOf(2), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
		}, nil)
		changeErrs = append(changeErrs, err)
		return nil
	})
	took := time.Since(started)
	release()
	if commitErr != nil {
		test.Fatalf("the transaction could not commit: %v", commitErr)
	}
	for index, err := range changeErrs {
		if !errors.Is(err, db.ErrPasswordsBusy) {
			test.Errorf("change %d: %v, want ErrPasswordsBusy", index, err)
		}
	}
	if took < 4*time.Second || took > 8*time.Second {
		test.Errorf("four changes behind a held lock took %s, want about five seconds", took)
	}

	// A new transaction starts with a clean slate.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
			ItemID: item.ID, BaseRevision: item.Revision, EncryptedFields: `{"sealed":"again"}`,
			ItemKeyByAccountKey: `{"sealed":"again key"}`, IsSavedByPerson: true,
		}); err != nil {
			test.Fatalf("a re-key with the locks free: %v", err)
		}
	})
}
