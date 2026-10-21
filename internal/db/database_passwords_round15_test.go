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

// One change of Passwords waits for its locks 5 s in all, not 5 s for each.
// Here the circle membership lock is held for 4 s by a rename and the revision
// lock by a listing in flight for longer than the limit: a save waits the 4 s
// for the first, and has one second left for the second.
func TestOneChangeOfPasswordsWaitsFiveSecondsInAllForItsLocks(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
	})
	var holders sync.WaitGroup
	circleHeld, listingHeld, releaseListing := make(chan struct{}), make(chan struct{}), make(chan struct{})
	holders.Add(2)
	go func() {
		defer holders.Done()
		_ = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.RenameCircle(world.home.ID, "Home again")
			close(circleHeld)
			time.Sleep(4 * time.Second)
			return err
		})
	}()
	go func() {
		defer holders.Done()
		_ = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.ListPasswordItems(world.ben.ID, 0)
			close(listingHeld)
			<-releaseListing
			return err
		})
	}()
	<-circleHeld
	<-listingHeld

	started := time.Now()
	var saveErr error
	if err := database.Transaction(func(tx db.Transaction) error {
		_, saveErr = tx.SavePasswordItem(&models.PasswordItem{
			ID: passwordItemIdOf(5), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
		}, nil)
		return nil
	}); err != nil {
		test.Errorf("the transaction could not commit after the refusal: %v", err)
	}
	waited := time.Since(started)
	close(releaseListing)
	holders.Wait()
	if !errors.Is(saveErr, db.ErrPasswordsBusy) {
		test.Errorf("the save: %v, expected ErrPasswordsBusy", saveErr)
	}
	if waited < 4500*time.Millisecond || waited > 6500*time.Millisecond {
		test.Errorf("the save waited %s for two locks, expected about 5 s in all", waited)
	}
}

// A person leaving a circle behind a listing in flight (which holds the
// revision lock) waits for it and gives up with ErrPasswordsBusy. The member is
// still in the circle, the transaction can go on and commit, and the same
// request goes through once the listing is done.
func TestLeavingACircleWhilePasswordsIsBusyAnswersBusyAndChangesNothing(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		// Ending the shares of a person with nothing in Passwords takes no lock,
		// so Ben has an item.
		savePasswordItemFor(test, tx, world.ben, 1, models.PasswordItemKindNote)
	})
	var holder sync.WaitGroup
	listingHeld, releaseListing := make(chan struct{}), make(chan struct{})
	holder.Add(1)
	go func() {
		defer holder.Done()
		_ = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.ListPasswordItems(world.cora.ID, 0)
			close(listingHeld)
			<-releaseListing
			return err
		})
	}()
	<-listingHeld

	var leaveErr error
	var circlesAfter []*models.Circle
	var circlesErr error
	started := time.Now()
	if err := database.Transaction(func(tx db.Transaction) error {
		_, leaveErr = tx.RemoveCircleMember(world.home.ID, world.ben.ID)
		// The resolver answers with the error, the request goes on to read, and
		// the transaction commits.
		circlesAfter, circlesErr = tx.ListCircles(world.ben.ID)
		return nil
	}); err != nil {
		test.Errorf("the transaction could not commit after the refusal: %v", err)
	}
	waited := time.Since(started)
	close(releaseListing)
	holder.Wait()
	if !errors.Is(leaveErr, db.ErrPasswordsBusy) {
		test.Errorf("leaving: %v, expected ErrPasswordsBusy", leaveErr)
	}
	if waited < 4500*time.Millisecond || waited > 6500*time.Millisecond {
		test.Errorf("leaving waited %s, expected about 5 s", waited)
	}
	if circlesErr != nil {
		test.Fatalf("reading the circles after the refusal: %v", circlesErr)
	}
	isInHome := false
	for _, circle := range circlesAfter {
		isInHome = isInHome || circle.ID == world.home.ID
	}
	if !isInHome {
		test.Error("the refused leave took the person out of the circle all the same")
	}

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			test.Errorf("leaving with nothing held: %v", err)
		}
	})
}
