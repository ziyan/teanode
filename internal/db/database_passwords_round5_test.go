package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// holdTransaction runs the first step in a transaction that stays open (its
// locks held) until release is closed, then runs the second in another and
// answers both errors. The second is started once the first holds its locks,
// and given half a second to wait behind them.
func holdTransaction(test *testing.T, database db.Database, first, second func(tx db.Transaction) error) (firstErr, secondErr error) {
	test.Helper()
	holding := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() {
		firstDone <- database.Transaction(func(tx db.Transaction) error {
			if err := first(tx); err != nil {
				close(holding)
				return err
			}
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	go func() {
		secondDone <- database.Transaction(second)
	}()
	time.Sleep(500 * time.Millisecond)
	close(release)
	for _, wait := range []struct {
		name string
		done chan error
		err  *error
	}{{"the first transaction", firstDone, &firstErr}, {"the second transaction", secondDone, &secondErr}} {
		select {
		case *wait.err = <-wait.done:
		case <-time.After(20 * time.Second):
			test.Fatalf("%s did not finish in 20 seconds", wait.name)
		}
	}
	return firstErr, secondErr
}

// A key row a token stored is replaced by a later tap, and never by another
// call that is not a tap; a row a tap stored is replaced by nobody. A row
// sealed for a public key the recipient no longer has is replaced only by a
// tap too.
func TestATokenCannotFillAWaitingRecipientsKeyPlace(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var world *shareWorld
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil)
	})
	keyFor := func(sealed string) *models.PasswordShareKey {
		key := passwordKeyFor(item, world.ben)
		key.ItemKeyByRecipientKey = sealed
		return key
	}
	bensKey := func(tx db.Transaction) string {
		for _, listed := range mustListPasswordItems(test, tx, world.ben, 0).Items {
			if listed.ID == item.ID {
				return listed.ItemKeyByRecipientKey
			}
		}
		return ""
	}
	add := func(sealed string, isByPerson bool) (stored int, key string) {
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			var err error
			if stored, err = tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{keyFor(sealed)}, isByPerson); err != nil {
				test.Fatalf("AddPasswordShareKeys: %v", err)
			}
			key = bensKey(tx)
		})
		return stored, key
	}

	// A token fills the empty place; nothing but a tap replaces what it put.
	if stored, key := add("garbage from a token", false); stored != 1 || key != "garbage from a token" {
		test.Fatalf("a token's key in an empty place: stored %d, ben's key %q", stored, key)
	}
	if stored, key := add("another from a token", false); stored != 0 || key != "garbage from a token" {
		test.Fatalf("a token replacing a token's key: stored %d, ben's key %q", stored, key)
	}
	// The owner's tap replaces it, and the item's revision moves so that ben's
	// devices read the key again.
	revisionBefore := lastPasswordRevision(test, database)
	if stored, key := add("the real key", true); stored != 1 || key != "the real key" {
		test.Fatalf("a tap replacing a token's key: stored %d, ben's key %q", stored, key)
	}
	if lastPasswordRevision(test, database) == revisionBefore {
		test.Fatalf("replacing a key drew no revision")
	}
	// What a tap stored is replaced by nobody.
	for _, isByPerson := range []bool{false, true} {
		if stored, key := add("overwritten", isByPerson); stored != 0 || key != "the real key" {
			test.Fatalf("a call (tap %v) replacing a tap's key: stored %d, ben's key %q", isByPerson, stored, key)
		}
	}

	// Ben sets up again with a new key pair: the stored key opens nothing. A
	// token cannot replace it either; the owner's tap can.
	dbtest.Exec(test, database, `UPDATE "password_setup" SET "public_key" = 'a-new-public-key-of-ben' WHERE "user_id" = '`+world.ben.ID+`'`)
	renewed := func(sealed string, isByPerson bool) (stored int) {
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			key := keyFor(sealed)
			key.RecipientPublicKey = "a-new-public-key-of-ben"
			var err error
			if stored, err = tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{key}, isByPerson); err != nil {
				test.Fatalf("AddPasswordShareKeys for the new key: %v", err)
			}
		})
		return stored
	}
	if stored := renewed("from a token", false); stored != 0 {
		test.Fatalf("a token replaced a key sealed for an old public key: %d", stored)
	}
	if stored := renewed("from a tap", true); stored != 1 {
		test.Fatalf("a tap did not replace a key sealed for an old public key: %d", stored)
	}

	// A re-key deletes every key and stores the given ones as its caller's.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		current, err := tx.GetPasswordItem(world.ada.ID, item.ID)
		if err != nil {
			test.Fatal(err)
		}
		key := keyFor("after a re-key")
		key.RecipientPublicKey = "a-new-public-key-of-ben"
		if _, err := tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
			ItemID: item.ID, BaseRevision: current.Revision, EncryptedFields: `{"sealed":"fields 2"}`, ItemKeyByAccountKey: `{"sealed":"key 2"}`,
			ShareKeys: []*models.PasswordShareKey{key},
		}); err != nil {
			test.Fatalf("RekeyPasswordItem: %v", err)
		}
	})
	if got := dbtest.QueryString(test, database, `SELECT is_stored_by_person::text FROM "password_share_key" WHERE "item_id" = '`+item.ID+`'`); got != "false" {
		test.Fatalf("a re-key by a token stored a key as a tap's: %s", got)
	}
}

// The keys of SetPasswordItemShares name the item key they were sealed from,
// like those of AddPasswordShareKeys, and a stale one is refused with nothing
// stored and no revision drawn.
func TestSharesWithKeysFromAnOldItemKeyAreRefused(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		grantees := []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}
		revisionBefore := lastPasswordRevision(test, database)

		stale := passwordKeyFor(item, world.ben)
		stale.ItemKeyByAccountKey = `{"sealed":"an older key"}`
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, grantees, []*models.PasswordShareKey{stale}, true); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a key sealed from an older item key: %v", err)
		}
		missing := passwordKeyFor(item, world.ben)
		missing.ItemKeyByAccountKey = ""
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, grantees, []*models.PasswordShareKey{missing}, true); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a key that names no item key: %v", err)
		}
		if got := lastPasswordRevision(test, database); got != revisionBefore {
			test.Fatalf("refused shares drew revisions: %s then %s", revisionBefore, got)
		}
		current, err := tx.GetPasswordItem(world.ada.ID, item.ID)
		if err != nil || len(current.Shares) != 0 {
			test.Fatalf("a refused call left shares: %+v %v", current, err)
		}
		// The current item key is accepted.
		shared := mustSetPasswordItemShares(test, tx, world.ada, item.ID, grantees, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if len(shared.Shares) != 1 || !shared.Shares[0].Recipients[0].HasKey {
			test.Fatalf("a key from the current item key: %+v", shared.Shares)
		}
	})
}

// Adding a grantee is a tap's, and making a device stay unlocked longer is
// too; both are judged on what is stored once the locks are held, so a call
// that waited behind another cannot act on a read made before.
func TestWhatNeedsATapIsJudgedUnderTheLocks(test *testing.T) {
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
	granteeBen := []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}

	// Plain refusals and what is open to a token.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, granteeBen, nil, false); !errors.Is(err, db.ErrPasswordTapRequired) {
			test.Fatalf("a token adding a person: %v", err)
		}
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}}, nil, false); !errors.Is(err, db.ErrPasswordTapRequired) {
			test.Fatalf("a token adding a circle: %v", err)
		}
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, granteeBen, nil, true); err != nil {
			test.Fatalf("a tap adding a person: %v", err)
		}
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, granteeBen, nil, false); err != nil {
			test.Fatalf("a token keeping the same: %v", err)
		}
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, nil, nil, false); err != nil {
			test.Fatalf("a token narrowing: %v", err)
		}
		if _, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, 60, false); !errors.Is(err, db.ErrPasswordTapRequired) {
			test.Fatalf("a token lengthening the lock time: %v", err)
		}
		if _, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, 60, true); err != nil {
			test.Fatalf("a tap lengthening the lock time: %v", err)
		}
		for _, minutes := range []int{60, 30} {
			if _, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, minutes, false); err != nil {
				test.Fatalf("a token keeping or shortening the lock time to %d: %v", minutes, err)
			}
		}
	})

	// A token waiting behind the owner's unshare does not add the person back.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, granteeBen, nil, true); err != nil {
			test.Fatal(err)
		}
	})
	unshareErr, readdErr := holdTransaction(test, database,
		func(tx db.Transaction) error {
			_, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, nil, nil, true)
			return err
		},
		func(tx db.Transaction) error {
			_, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, granteeBen, nil, false)
			return err
		})
	if unshareErr != nil || !errors.Is(readdErr, db.ErrPasswordTapRequired) {
		test.Fatalf("unshare: %v; the token's re-adding, waiting behind it: %v", unshareErr, readdErr)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share" WHERE "item_id" = '`+item.ID+`'`); got != "0" {
		test.Fatalf("%s shares after the owner unshared and the token tried to add the person back", got)
	}

	// The same for the lock time: a token's 10 minutes was longer than the 5
	// the person's tap set while it waited.
	tapErr, tokenErr := holdTransaction(test, database,
		func(tx db.Transaction) error {
			_, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, 5, true)
			return err
		},
		func(tx db.Transaction) error {
			_, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, 10, false)
			return err
		})
	if tapErr != nil || !errors.Is(tokenErr, db.ErrPasswordTapRequired) {
		test.Fatalf("shortening: %v; the token's lengthening, waiting behind it: %v", tapErr, tokenErr)
	}
	if got := dbtest.QueryString(test, database, `SELECT lock_after_minutes::text FROM "password_setup" WHERE "user_id" = '`+world.ada.ID+`'`); got != "5" {
		test.Fatalf("the lock time is %s minutes", got)
	}
}

// Renaming a circle takes the circle membership lock before the circle's row,
// as every other change to circles does. Taken the other way round, a request
// that renamed a circle and then changed Passwords waited for the membership
// lock while a person leaving the circle held it and waited for the row.
func TestRenamingACircleAndThenSavingPasswordsDoesNotDeadlockWithLeaving(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	renamed := make(chan struct{})
	leaving := make(chan struct{})
	renameDone := make(chan error, 1)
	leaveDone := make(chan error, 1)
	go func() {
		renameDone <- database.Transaction(func(tx db.Transaction) error {
			if _, err := tx.RenameCircle(world.home.ID, "Renamed"); err != nil {
				return err
			}
			close(renamed)
			<-leaving
			time.Sleep(500 * time.Millisecond)
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(1), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			return err
		})
	}()
	<-renamed
	go func() {
		close(leaving)
		leaveDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID)
			return err
		})
	}()
	for name, done := range map[string]chan error{"the rename and the save": renameDone, "the person leaving": leaveDone} {
		select {
		case err := <-done:
			if err != nil {
				test.Errorf("%s failed: %v", name, err)
			}
		case <-time.After(20 * time.Second):
			test.Errorf("%s did not finish in 20 seconds", name)
		}
	}
}
