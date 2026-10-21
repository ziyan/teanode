package db_test

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// savePasswordItemAs saves the item with the given content, from the person's
// tap or not, and answers the item as stored.
func savePasswordItemAs(test *testing.T, tx db.Transaction, owner *models.User, current *models.PasswordItem, fields string, isByPerson bool) *models.PasswordItem {
	test.Helper()
	var baseRevision *int64
	if current != nil {
		baseRevision = &current.Revision
	}
	saved, err := tx.SavePasswordItem(&models.PasswordItem{
		ID: passwordItemIdOf(1), OwnerUserID: owner.ID, ItemKind: models.PasswordItemKindLogin,
		EncryptedFields: fields, ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: isByPerson,
	}, baseRevision)
	if err != nil {
		test.Fatalf("SavePasswordItem %s: %s", fields, err)
	}
	return saved
}

// A save that is not the person's tap always keeps the version it replaces
// when that content was the person's, even when a version was kept a moment
// ago: otherwise a token saving within the hour of an edit would lose it.
func TestAPersonsEditIsKeptWhenATokenSavesOverItWithinTheHour(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var ada *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		ada = world.ada
		setUpPasswordsFor(test, tx, ada)
		current := savePasswordItemAs(test, tx, ada, nil, `{"sealed":"first password"}`, true)
		current = savePasswordItemAs(test, tx, ada, current, `{"sealed":"new password the person just chose"}`, true)
		current = savePasswordItemAs(test, tx, ada, current, `{"sealed":"garbage one"}`, false)
		current = savePasswordItemAs(test, tx, ada, current, `{"sealed":"garbage two"}`, false)
		savePasswordItemAs(test, tx, ada, current, `{"sealed":"garbage three"}`, false)
	})
	kept := func(fields string) string {
		return dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "encrypted_fields" = '`+fields+`'`)
	}
	if got := kept(`{"sealed":"new password the person just chose"}`); got != "1" {
		test.Fatalf("the person's newest edit was kept %s times, want once", got)
	}
	if got := kept(`{"sealed":"first password"}`); got != "1" {
		test.Fatalf("the first password was kept %s times, want once", got)
	}
	// The garbage after it is a burst: none of it is kept.
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version"`); got != "2" {
		test.Fatalf("%s versions are kept, want 2", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT is_saved_by_person::text FROM "password_item_version" WHERE "encrypted_fields" = '{"sealed":"new password the person just chose"}'`); got != "true" {
		test.Fatalf("the kept version does not say the person saved it: %s", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT is_saved_by_person::text FROM "password_item" WHERE "id" = '`+passwordItemIdOf(1)+`'`); got != "false" {
		test.Fatalf("the item says the person saved the token's content: %s", got)
	}
}

// The versions of content the person saved have a count of their own: a flood
// of saves cannot push them out, and they cannot push out the others.
func TestTheVersionsOfThePersonsContentHaveACountOfTheirOwn(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var ada *models.User
	var current *models.PasswordItem
	const cycleCount = models.PasswordItemVersionsByPersonMost + 5
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		ada = world.ada
		setUpPasswordsFor(test, tx, ada)
		current = savePasswordItemAs(test, tx, ada, nil, `{"sealed":"edit 0"}`, true)
		for index := 1; index <= cycleCount; index++ {
			current = savePasswordItemAs(test, tx, ada, current, fmt.Sprintf(`{"sealed":"garbage %d"}`, index), false)
			current = savePasswordItemAs(test, tx, ada, current, fmt.Sprintf(`{"sealed":"edit %d"}`, index), true)
		}
		// The last edit is replaced by a token too.
		current = savePasswordItemAs(test, tx, ada, current, `{"sealed":"last garbage"}`, false)
	})
	byPerson := func() string {
		return dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "is_saved_by_person"`)
	}
	if got := byPerson(); got != fmt.Sprint(models.PasswordItemVersionsByPersonMost) {
		test.Fatalf("%s versions of the person's content are kept, want %d", got, models.PasswordItemVersionsByPersonMost)
	}
	// The oldest of them went first; the newest edit is there.
	if got := dbtest.QueryString(test, database, fmt.Sprintf(`SELECT count(*) FROM "password_item_version" WHERE "encrypted_fields" = '{"sealed":"edit %d"}'`, cycleCount)); got != "1" {
		test.Fatalf("the newest edit was kept %s times", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "encrypted_fields" = '{"sealed":"edit 0"}'`); got != "0" {
		test.Fatalf("the oldest edit is still there")
	}

	// A flood of saves an hour apart keeps up to PasswordItemVersionsMost of
	// the others and does not touch the person's.
	for index := 0; index < models.PasswordItemVersionsMost+20; index++ {
		dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = "replaced_at" - interval '2 hours'`)
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			current = savePasswordItemAs(test, tx, ada, current, fmt.Sprintf(`{"sealed":"flood %d"}`, index), false)
		})
	}
	if got := byPerson(); got != fmt.Sprint(models.PasswordItemVersionsByPersonMost) {
		test.Fatalf("after a flood %s versions of the person's content are kept, want %d", got, models.PasswordItemVersionsByPersonMost)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE NOT "is_saved_by_person"`); got != fmt.Sprint(models.PasswordItemVersionsMost) {
		test.Fatalf("%s other versions are kept after a flood, want %d", got, models.PasswordItemVersionsMost)
	}
	if got := dbtest.QueryString(test, database, fmt.Sprintf(`SELECT count(*) FROM "password_item_version" WHERE "encrypted_fields" = '{"sealed":"edit %d"}'`, cycleCount)); got != "1" {
		test.Fatalf("the newest edit did not survive the flood")
	}
}

// A request that saves and then shares takes the circle membership lock before
// the revision lock, as a circle change does, so that the two never wait for
// each other: the save holds the circle lock from its first statement, and a
// person leaving a circle in between waits for the whole request.
func TestASaveThenAShareDoesNotDeadlockWithLeavingACircle(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
	})

	saved := make(chan struct{})
	leaving := make(chan struct{})
	requestDone := make(chan error, 1)
	go func() {
		requestDone <- database.Transaction(func(tx db.Transaction) error {
			item, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(1), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			if err != nil {
				return err
			}
			close(saved)
			// Ben starts leaving the circle now, and waits for this request.
			<-leaving
			time.Sleep(500 * time.Millisecond)
			_, err = tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)}, true)
			return err
		})
	}()
	<-saved
	leaveDone := make(chan error, 1)
	go func() {
		close(leaving)
		leaveDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID)
			return err
		})
	}()
	for name, done := range map[string]chan error{"the save and share": requestDone, "leaving the circle": leaveDone} {
		select {
		case err := <-done:
			if err != nil {
				test.Fatalf("%s: %v", name, err)
			}
		case <-time.After(20 * time.Second):
			test.Fatalf("%s did not finish: the two wait for each other", name)
		}
	}
	// Ben left after the share was made: it is gone, and the item is marked
	// as needing a new key because ben held one.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item, err := tx.GetPasswordItem(world.ada.ID, passwordItemIdOf(1))
		if err != nil || len(item.Shares) != 0 || !item.NeedsNewKey {
			test.Fatalf("the item after ben left: %+v %v", item, err)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 0 {
			test.Fatalf("ben still reads: %+v", got)
		}
	})
}

// The database marks an item that needs a new key in the same transaction that
// narrows access, and only a re-key clears the mark; a restore keeps it.
func TestAnItemIsMarkedWhenAccessToItNarrows(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		needsNewKey := func() bool {
			stored, err := tx.GetPasswordItem(world.ada.ID, item.ID)
			if err != nil {
				test.Fatal(err)
			}
			item = stored
			return stored.NeedsNewKey
		}
		if needsNewKey() {
			test.Fatal("a new item needs a new key")
		}
		rekey := func(shareKeys []*models.PasswordShareKey) {
			test.Helper()
			if _, err := tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
				ItemID: item.ID, BaseRevision: item.Revision, EncryptedFields: `{"sealed":"fields 2"}`,
				ItemKeyByAccountKey: fmt.Sprintf(`{"sealed":"key %d"}`, item.Revision), ShareKeys: shareKeys,
			}); err != nil {
				test.Fatalf("RekeyPasswordItem: %s", err)
			}
		}

		// Adding a grantee, and giving a key, narrow nothing. Removing the
		// grantee does, and so does only a re-key clear it.
		granted := []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, granted, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if needsNewKey() {
			test.Fatal("sharing marked the item")
		}
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, nil, nil)
		if !needsNewKey() {
			test.Fatal("removing a grantee did not mark the item")
		}
		rekey(nil)
		if needsNewKey() {
			test.Fatal("a re-key did not clear the mark")
		}

		// Turning the agent off marks it; so does deleting an item the agent
		// or a recipient held a key for, and a restore keeps the mark.
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, item.ID, true, "agent copy"); err != nil {
			test.Fatal(err)
		}
		if needsNewKey() {
			test.Fatal("turning the agent on marked the item")
		}
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, item.ID, false, ""); err != nil {
			test.Fatal(err)
		}
		if !needsNewKey() {
			test.Fatal("turning the agent off did not mark the item")
		}
		rekey(nil)
		if _, err := tx.DeletePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatal(err)
		}
		if needsNewKey() {
			test.Fatal("deleting an item nobody held a key for marked it")
		}
		if _, err := tx.RestorePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatal(err)
		}
		// The item has been re-keyed since it was saved: the key is sealed from
		// the item key it has now.
		currentKey := passwordKeyFor(item, world.ben)
		currentKey.ItemKeyByAccountKey = item.ItemKeyByAccountKey
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, granted, []*models.PasswordShareKey{currentKey})
		if _, err := tx.DeletePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatal(err)
		}
		if !needsNewKey() {
			test.Fatal("deleting an item with a recipient's key did not mark it")
		}
		if _, err := tx.RestorePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatal(err)
		}
		if !needsNewKey() {
			test.Fatal("a restore cleared the mark")
		}
		if mustListPasswordItems(test, tx, world.ben, 0); len(mustListPasswordItems(test, tx, world.ben, 0).Items) != 0 {
			test.Fatal("ben reads after the delete")
		}
	})
}

// A key already stored for a recipient's public key is never overwritten by
// SetPasswordItemShares or AddPasswordShareKeys; one stored for another public
// key is replaced; and AddPasswordShareKeys answers only the keys it stored,
// raises only the revisions of the items it changed, and refuses a key sealed
// from an item key the item no longer has.
func TestShareKeysAreStoredOnlyWhereNoneIsWorking(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		other := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindLogin)
		granted := []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, granted, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		mustSetPasswordItemShares(test, tx, world.ada, other.ID, granted, nil)
		readKey := func() string {
			list := mustListPasswordItems(test, tx, world.ben, 0)
			for _, listed := range list.Items {
				if listed.ID == item.ID {
					return listed.ItemKeyByRecipientKey
				}
			}
			return ""
		}
		if readKey() != "key-of-ben" {
			test.Fatalf("ben's key: %q", readKey())
		}

		// A second key for the same public key changes nothing, by either call.
		replacement := passwordKeyFor(item, world.ben)
		replacement.ItemKeyByRecipientKey = "garbage"
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, granted, []*models.PasswordShareKey{replacement})
		before := mustListPasswordItems(test, tx, world.ada, 0).Revision
		stored, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{replacement}, true)
		if err != nil || stored != 0 {
			test.Fatalf("adding a key that is already there: %d %v", stored, err)
		}
		if got := mustListPasswordItems(test, tx, world.ada, 0).Revision; got != before {
			test.Fatalf("a key that was already there raised a revision: %d then %d", before, got)
		}
		if readKey() != "key-of-ben" {
			test.Fatalf("ben's working key was replaced: %q", readKey())
		}

		// The one for another item is stored, and only that item is raised.
		sequenceBefore := lastPasswordRevision(test, database)
		stored, err = tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{replacement, passwordKeyFor(other, world.ben)}, true)
		if err != nil || stored != 1 {
			test.Fatalf("adding one new key of two: %d %v", stored, err)
		}
		if got, want := lastPasswordRevision(test, database), fmt.Sprint(mustParseInt(test, sequenceBefore)+1); got != want {
			test.Fatalf("the revision sequence moved from %s to %s, want %s", sequenceBefore, got, want)
		}

		// A key sealed from another item key is refused, and draws nothing.
		stale := passwordKeyFor(other, world.ben)
		stale.ItemKeyByAccountKey = `{"sealed":"an older key"}`
		sequenceBefore = lastPasswordRevision(test, database)
		if _, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{stale}, true); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a key from another item key: %v", err)
		}
		missing := passwordKeyFor(other, world.ben)
		missing.ItemKeyByAccountKey = ""
		if _, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{missing}, true); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a key that names no item key: %v", err)
		}
		if got := lastPasswordRevision(test, database); got != sequenceBefore {
			test.Fatalf("refused keys drew revisions: %s then %s", sequenceBefore, got)
		}
	})
}

func mustParseInt(test *testing.T, text string) int64 {
	test.Helper()
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		test.Fatalf("%q is not a number: %s", text, err)
	}
	return number
}
