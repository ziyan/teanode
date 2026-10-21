package db_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A token (never a tap) cannot take a person to the most rows: it stops
// models.PasswordRowsMost - models.PasswordRowsForProgramsMost places short, and
// the person's own tap goes on adding items there.
func TestAProgramStopsMakingItemsBeforeTheLastPlacesThePersonKeeps(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	// 17,000 live and 19,000 deleted rows a token left, 36,000 in all.
	insertPasswordItemRows(test, database, world.ada, 8, 1, 17000, false)
	insertPasswordItemRows(test, database, world.ada, 9, 1, models.PasswordRowsForProgramsMost-17000, true)
	save := func(tx db.Transaction, number int, isByPerson bool) error {
		_, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: passwordItemIdOf(number), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: isByPerson,
		}, nil)
		return err
	}
	if err := database.Transaction(func(tx db.Transaction) error { return save(tx, 1, false) }); !errors.Is(err, db.ErrLimitReached) {
		test.Fatalf("a token adds an item at %d rows: %v", models.PasswordRowsForProgramsMost, err)
	}
	// Below the line a token still adds one, and the tap adds past it.
	dbtest.Exec(test, database, `DELETE FROM password_item WHERE id = '00000009-0000-4000-8000-000000000001'`)
	if err := database.Transaction(func(tx db.Transaction) error { return save(tx, 2, false) }); err != nil {
		test.Fatalf("a token adds an item below the line: %v", err)
	}
	for number := 3; number <= 6; number++ {
		if err := database.Transaction(func(tx db.Transaction) error { return save(tx, number, true) }); err != nil {
			test.Fatalf("the person's tap adds item %d past the line: %v", number, err)
		}
	}
	if err := database.Transaction(func(tx db.Transaction) error { return save(tx, 7, false) }); !errors.Is(err, db.ErrLimitReached) {
		test.Fatalf("a token adds an item past the line: %v", err)
	}
}

func countOf(test *testing.T, database db.Database, table string) int64 {
	test.Helper()
	var count int64
	if _, err := fmt.Sscan(dbtest.QueryString(test, database, `SELECT count(*) FROM "`+table+`"`), &count); err != nil {
		test.Fatal(err)
	}
	return count
}

// sharedFromOneOwner stores keys straight into the table for the recipient, for
// this many of the owner's items (a block of ids as insertPasswordItemRows
// makes), past every check the API makes.
func sharedFromOneOwner(test *testing.T, database db.Database, owner, recipient *models.User, block, count int) {
	test.Helper()
	insertPasswordItemRows(test, database, owner, block, 1, count, false)
	prefix := fmt.Sprintf("%08d-0000-4000-8000-", block)
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share (id, item_id, owner_user_id, grantee_user_id, created_at)
		SELECT 'sh%02d' || lpad(n::text, 27, '0'), '%s' || lpad(n::text, 12, '0'), '%s', '%s', now()
		FROM generate_series(1, %d) n`, block, prefix, owner.ID, recipient.ID, count))
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share_key (item_id, recipient_user_id, item_key_by_recipient_key, recipient_public_key, is_stored_by_person, created_at)
		SELECT '%s' || lpad(n::text, 12, '0'), '%s', 'key', '%s', true, now()
		FROM generate_series(1, %d) n`, prefix, recipient.ID, passwordsPublicKeyOf(recipient), count))
}

// One member cannot push every tiny item they have at one person: what one
// owner shares with one person stops at the most items, however small, and the
// recipient shows that they have too much shared with them. Another owner's
// item still reaches them. The waiting list leaves the recipient out, since no
// key would be stored for them, and still names a recipient who has room.
func TestOneOwnerCannotShareMoreThanTheMostItemsWithOnePerson(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	var erin *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
		var err error
		if erin, err = tx.CreateUser(&models.User{Username: "erin"}); err != nil {
			test.Fatal(err)
		}
		grantPasswordsUseToAll(test, tx, []string{erin.ID})
		if _, err := tx.AddCircleMember(world.home.ID, erin.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatal(err)
		}
		setUpPasswordsFor(test, tx, erin)
	})
	sharedFromOneOwner(test, database, world.ada, world.ben, 7, models.PasswordsSharedItemsFromOneOwnerMost)

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindNote)
		shared := mustSetPasswordItemShares(test, tx, world.ada, item.ID,
			[]models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}, {GranteeUserID: erin.ID}},
			[]*models.PasswordShareKey{passwordKeyFor(item, world.ben), passwordKeyFor(item, erin)})
		byName := map[string]*models.PasswordRecipient{}
		for _, share := range shared.Shares {
			for _, recipient := range share.Recipients {
				byName[recipient.Username] = recipient
			}
		}
		if ben := byName["ben"]; ben == nil || ben.HasKey || !ben.HasTooMuchShared {
			test.Fatalf("ben, past %d items from ada: %+v", models.PasswordsSharedItemsFromOneOwnerMost, ben)
		}
		if erinRecipient := byName["erin"]; erinRecipient == nil || !erinRecipient.HasKey || erinRecipient.HasTooMuchShared {
			test.Fatalf("erin, with nothing shared yet: %+v", erinRecipient)
		}
		// More of ada's items for erin wait for nothing: only ben is full.
		second := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindNote)
		mustSetPasswordItemShares(test, tx, world.ada, second.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}, {GranteeUserID: erin.ID}}, nil)
		waiting, err := tx.ListWaitingPasswordShares(world.ada.ID)
		if err != nil {
			test.Fatal(err)
		}
		if len(waiting) != 1 || waiting[0].ItemID != second.ID || waiting[0].Recipient.UserID != erin.ID {
			test.Fatalf("the waiting list names %d pairs, want only erin for the second item: %+v", len(waiting), waiting)
		}
		// Another owner reaches ben all the same.
		other := savePasswordItemFor(test, tx, world.cora, 3, models.PasswordItemKindNote)
		sharedByCora := mustSetPasswordItemShares(test, tx, world.cora, other.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(other, world.ben)})
		if recipient := sharedByCora.Shares[0].Recipients[0]; !recipient.HasKey || recipient.HasTooMuchShared {
			test.Fatalf("another owner's item for ben: %+v", recipient)
		}
	})
}

// The recipients of a list of waiting pairs who have too much shared with them
// are left out of it, so a device does not try for them on every pass; a pair
// that fits, listed beside many that do not, is not lost to them.
func TestTheWaitingListSkipsRecipientsWhoHaveTooMuchShared(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	var erin *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben} {
			setUpPasswordsFor(test, tx, person)
		}
		var err error
		if erin, err = tx.CreateUser(&models.User{Username: "erin"}); err != nil {
			test.Fatal(err)
		}
		grantPasswordsUseToAll(test, tx, []string{erin.ID})
		if _, err := tx.AddCircleMember(world.home.ID, erin.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatal(err)
		}
		setUpPasswordsFor(test, tx, erin)
	})
	// Ben holds as many of ada's items as he may. Ada then has more items than
	// the list answers, all of them shared with the circle: ben is full for
	// every one, erin waits for every one, and erin's pairs come last, behind
	// the pairs of ben that are skipped.
	sharedFromOneOwner(test, database, world.ada, world.ben, 7, models.PasswordsSharedItemsFromOneOwnerMost)
	const waitingItems = 30
	insertPasswordItemRows(test, database, world.ada, 6, 1, waitingItems, false)
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share (id, item_id, owner_user_id, grantee_circle_id, created_at)
		SELECT 'ci' || lpad(n::text, 30, '0'), '00000006-0000-4000-8000-' || lpad(n::text, 12, '0'), '%s', '%s', now()
		FROM generate_series(1, %d) n`, world.ada.ID, world.home.ID, waitingItems))
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		waiting, err := tx.ListWaitingPasswordShares(world.ada.ID)
		if err != nil {
			test.Fatal(err)
		}
		if len(waiting) != waitingItems {
			test.Fatalf("%d pairs are waiting, want erin's %d", len(waiting), waitingItems)
		}
		for _, share := range waiting {
			if share.Recipient.UserID != erin.ID || share.Recipient.HasTooMuchShared || share.HasMoreWaiting {
				test.Fatalf("a pair of the waiting list: %+v (%+v)", share, share.Recipient)
			}
		}
	})
}

// A key replaces a stored row only when it is no longer than the row: the
// bounds on what is shared with a person count the row as stored, and a longer
// replacement would take the person past them without being counted.
func TestAReplacementKeyIsNeverLongerThanTheRowItReplaces(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindNote)
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil)
		// A token stores the first key, fifty bytes.
		key := passwordKeyFor(item, world.ben)
		key.ItemKeyByRecipientKey = strings.Repeat("k", 50)
		if stored, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{key}, false); err != nil || stored != 1 {
			test.Fatalf("a token stores a key: %d %v", stored, err)
		}
	})
	storedKey := func() string {
		return dbtest.QueryString(test, database, `SELECT item_key_by_recipient_key FROM password_share_key WHERE item_id = '`+item.ID+`'`)
	}
	replaceWith := func(size int) {
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			current, err := tx.GetPasswordItem(world.ada.ID, item.ID)
			if err != nil {
				test.Fatal(err)
			}
			key := passwordKeyFor(current, world.ben)
			key.ItemKeyByRecipientKey = strings.Repeat("r", size)
			if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{key}, true); err != nil {
				test.Fatalf("a tap replaces with %d bytes: %s", size, err)
			}
		})
	}
	replaceWith(60)
	if storedKey() != strings.Repeat("k", 50) {
		test.Fatalf("a longer key replaced the stored row: %d bytes", len(storedKey()))
	}
	replaceWith(40)
	if storedKey() != strings.Repeat("r", 40) {
		test.Fatalf("a shorter key did not replace the token's row: %q", storedKey())
	}
}

// putItemVersion stores a version of the item straight into the table.
func putItemVersion(test *testing.T, database db.Database, itemId string, revision int64, fields string, isSavedByPerson bool, replacedAgo time.Duration) {
	test.Helper()
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_item_version (item_id, revision, encrypted_fields, item_key_by_account_key, is_saved_by_person, replaced_at)
		VALUES ('%s', %d, '%s', '{"sealed":"key of %d"}', %v, now() - interval '%d seconds')`, itemId, revision, fields, revision, isSavedByPerson, int(replacedAgo.Seconds())))
}

// The person lists an item's earlier versions, newest first, and puts one back:
// its fields and item key become the item's, what it replaces is kept, the keys
// given to other people and the agent's copy are dropped, the item is marked as
// needing a new key, and it has a new revision. Only a tap may, and only on the
// revision the device read, for an item of their own that is not deleted.
func TestListingAndRestoringAnEarlierVersionOfAnItem(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		shared := mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if !shared.Shares[0].Recipients[0].HasKey {
			test.Fatal("ben has no key")
		}
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, item.ID, true, "sealed for the agent"); err != nil {
			test.Fatal(err)
		}
	})
	// Two earlier versions a day apart, and the item itself saved by a tap.
	putItemVersion(test, database, item.ID, 1001, `{"sealed":"oldest"}`, false, 48*time.Hour)
	putItemVersion(test, database, item.ID, 1002, `{"sealed":"older"}`, true, 24*time.Hour)
	var current *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if current, err = tx.GetPasswordItem(world.ada.ID, item.ID); err != nil {
			test.Fatal(err)
		}
		current, err = tx.SavePasswordItem(&models.PasswordItem{
			ID: item.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
			EncryptedFields: `{"sealed":"current"}`, ItemKeyByAccountKey: `{"sealed":"current key"}`, IsSavedByPerson: true,
		}, &current.Revision)
		if err != nil {
			test.Fatal(err)
		}
	})

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		versions, err := tx.ListPasswordItemVersions(world.ada.ID, item.ID)
		if err != nil || len(versions) != 3 {
			for _, version := range versions {
				test.Logf("version %d %s", version.Revision, version.EncryptedFields)
			}
			test.Fatalf("the versions: %d %v (item revision %d)", len(versions), err, current.Revision)
		}
		// Newest first: the content the save just replaced, then the two older.
		if versions[0].EncryptedFields != `{"sealed":"fields"}` || versions[1].Revision != 1002 || versions[2].Revision != 1001 {
			test.Fatalf("the order of the versions: %d %d %d", versions[0].Revision, versions[1].Revision, versions[2].Revision)
		}
		if !versions[1].IsSavedByPerson || versions[2].IsSavedByPerson || versions[1].ItemKind != models.PasswordItemKindLogin ||
			versions[1].ItemKeyByAccountKey != `{"sealed":"key of 1002"}` || versions[1].ReplacedAt.IsZero() {
			test.Fatalf("a version: %+v", versions[1])
		}
		if _, err := tx.ListPasswordItemVersions(world.ben.ID, item.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's item: %v", err)
		}
	})

	restore := func(tx db.Transaction, owner *models.User, versionRevision, baseRevision int64, isByPerson bool) (*models.PasswordItem, error) {
		return tx.RestorePasswordItemVersion(owner.ID, item.ID, versionRevision, baseRevision, isByPerson)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		revisionBefore := lastPasswordRevision(test, database)
		if _, err := restore(tx, world.ada, 1002, current.Revision, false); !errors.Is(err, db.ErrPasswordTapRequired) {
			test.Fatalf("a token restores: %v", err)
		}
		if _, err := restore(tx, world.ada, 1002, current.Revision-1, true); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a restore from an old revision: %v", err)
		}
		if _, err := restore(tx, world.ada, 99, current.Revision, true); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a version that is not kept: %v", err)
		}
		if _, err := restore(tx, world.ben, 1002, current.Revision, true); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's item: %v", err)
		}
		if got := lastPasswordRevision(test, database); got != revisionBefore {
			test.Fatalf("refused restores drew revisions: %s then %s", revisionBefore, got)
		}
	})

	var restored *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if restored, err = restore(tx, world.ada, 1002, current.Revision, true); err != nil {
			test.Fatalf("the person's tap restores: %s", err)
		}
	})
	if restored.EncryptedFields != `{"sealed":"older"}` || restored.ItemKeyByAccountKey != `{"sealed":"key of 1002"}` {
		test.Fatalf("the restored item holds %q and %q", restored.EncryptedFields, restored.ItemKeyByAccountKey)
	}
	if restored.Revision <= current.Revision || !restored.NeedsNewKey || !restored.IsSavedByPerson || restored.IsUsableByAgent || restored.ItemKeyByAgentKey != "" {
		test.Fatalf("the restored item: revision %d (was %d), needsNewKey %v, agent %v", restored.Revision, current.Revision, restored.NeedsNewKey, restored.IsUsableByAgent)
	}
	if recipient := restored.Shares[0].Recipients[0]; recipient.HasKey {
		test.Fatal("ben still holds a key sealed from the key that was replaced")
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM password_share_key WHERE item_id = '`+item.ID+`'`); got != "0" {
		test.Fatalf("%s keys are stored", got)
	}
	// What the restore replaced is a version now, and the one restored is still
	// there to go back to.
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM password_item_version WHERE item_id = '`+item.ID+`' AND encrypted_fields = '{"sealed":"current"}'`); got != "1" {
		test.Fatalf("the content the restore replaced is kept %s times", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM password_item_version WHERE item_id = '`+item.ID+`' AND revision = 1002`); got != "1" {
		test.Fatal("the version restored is gone")
	}
	// The device that read the old revision is refused.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := restore(tx, world.ada, 1001, current.Revision, true); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a second restore from the old revision: %v", err)
		}
		deleted, err := tx.DeletePasswordItem(world.ada.ID, item.ID, true)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := restore(tx, world.ada, 1001, deleted.Revision, true); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a deleted item: %v", err)
		}
		if versions, err := tx.ListPasswordItemVersions(world.ada.ID, item.ID); err != nil || len(versions) == 0 {
			test.Fatalf("a deleted item's versions: %d %v", len(versions), err)
		}
	})
}

// versionsBytesMatch fails when any item's versions_bytes is not the sealed text
// of its versions.
func versionsBytesMatch(test *testing.T, database db.Database) {
	test.Helper()
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM password_item item WHERE item.versions_bytes <> COALESCE((
		SELECT SUM(octet_length(version.encrypted_fields) + octet_length(version.item_key_by_account_key)) FROM password_item_version version WHERE version.item_id = item.id), 0)`); got != "0" {
		test.Fatalf("%s items count their versions' bytes wrongly", got)
	}
}

// How much a person holds is summed from counts the database keeps, so it
// stays right through every way a version comes and goes: a save, the thinning
// of old ones, the sweep, deleting an item forever and erasing everything.
func TestTheBytesOfKeptVersionsAreCountedByTheDatabase(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	var current *models.PasswordItem
	for index := 0; index < models.PasswordItemVersionsMost+5; index++ {
		dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = "replaced_at" - interval '2 hours'`)
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			if current == nil {
				current = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindNote)
			}
			current = saveChangedPasswordItem(test, tx, world.ada, current, passwordItemFieldsOf(fmt.Sprint(index), 100+index))
		})
	}
	versionsBytesMatch(test, database)
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM password_item_version`); got != fmt.Sprint(models.PasswordItemVersionsMost) {
		test.Fatalf("%s versions are kept", got)
	}
	// The sweep.
	dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = now() - interval '31 days' WHERE "revision" IN (SELECT "revision" FROM "password_item_version" ORDER BY "revision" ASC LIMIT 7)`)
	if _, err := database.ScavengePasswordItems(time.Now().AddDate(0, 0, -models.PasswordItemDeletedDays)); err != nil {
		test.Fatal(err)
	}
	versionsBytesMatch(test, database)
	// The usage a change asks for adds the versions to the deleted items.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.DeletePasswordItem(world.ada.ID, current.ID, true); err != nil {
			test.Fatal(err)
		}
	})
	versionsBytesMatch(test, database)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if err := tx.PurgePasswordItem(world.ada.ID, current.ID); err != nil {
			test.Fatal(err)
		}
	})
	if got := countOf(test, database, "password_item_version"); got != 0 {
		test.Fatalf("%d versions outlived their item", got)
	}
}

// A change waits at most a few seconds for the locks every change of Passwords
// holds until it commits, then gives up with db.ErrPasswordsBusy having changed
// nothing, instead of piling up behind a request that holds them for minutes. A
// listing waits for the same lock the same way.
func TestAChangeThatCannotGetThePasswordsLocksGivesUp(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
	})
	holding, release := make(chan struct{}), make(chan struct{})
	var holder sync.WaitGroup
	holder.Add(1)
	go func() {
		defer holder.Done()
		_ = database.Transaction(func(tx db.Transaction) error {
			// A save takes the locks and keeps them until the transaction ends.
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(100), OwnerUserID: world.ben.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			close(holding)
			<-release
			return err
		})
	}()
	<-holding

	var waiters sync.WaitGroup
	var saveErr, listErr error
	var saveTook, listTook time.Duration
	waiters.Add(2)
	go func() {
		defer waiters.Done()
		started := time.Now()
		saveErr = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(200), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: true,
			}, nil)
			return err
		})
		saveTook = time.Since(started)
	}()
	go func() {
		defer waiters.Done()
		started := time.Now()
		listErr = database.Transaction(func(tx db.Transaction) error {
			_, err := tx.ListPasswordItems(world.ada.ID, 0)
			return err
		})
		listTook = time.Since(started)
	}()
	waiters.Wait()
	close(release)
	holder.Wait()
	if !errors.Is(saveErr, db.ErrPasswordsBusy) || !errors.Is(listErr, db.ErrPasswordsBusy) {
		test.Fatalf("a save and a listing behind a held lock: %v, %v", saveErr, listErr)
	}
	for name, took := range map[string]time.Duration{"save": saveTook, "listing": listTook} {
		if took < 4*time.Second || took > 9*time.Second {
			test.Errorf("the %s waited %s, want about five seconds", name, took)
		}
	}
	if got := countOf(test, database, "password_item"); got != 1 {
		test.Fatalf("%d items are stored, want only the holder's", got)
	}
	// With the lock free again the same save goes through.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		savePasswordItemFor(test, tx, world.ada, 200, models.PasswordItemKindNote)
	})
}

// How much a person holds is summed without reading their versions, so a
// change by a token to a person with the most versions the allowance holds is
// as cheap as to a person with none: twenty-five deletes (a whole document's
// worth) of a person with 39,000 items and about 300,000 kept versions.
func TestAPersonWithManyVersionsCostsAChangeNothingExtra(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	insertPasswordItemRows(test, database, world.ada, 8, 1, 20000, false)
	insertPasswordItemRows(test, database, world.ada, 9, 1, 19000, true)
	// The trigger would count the versions one row at a time; count them once
	// after the bulk insert instead, which is what it would have arrived at.
	dbtest.Exec(test, database, `ALTER TABLE password_item_version DISABLE TRIGGER password_item_version_bytes`)
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_item_version (item_id, revision, encrypted_fields, item_key_by_account_key, is_saved_by_person, replaced_at)
		SELECT item.id, v, '{"sealed":"fields"}', '{"sealed":"key"}', false, now() - (v || ' hours')::interval
		FROM password_item item CROSS JOIN generate_series(1, 8) v WHERE item.owner_user_id = '%s'`, world.ada.ID))
	dbtest.Exec(test, database, `ALTER TABLE password_item_version ENABLE TRIGGER password_item_version_bytes`)
	dbtest.Exec(test, database, `UPDATE password_item item SET versions_bytes = (SELECT SUM(octet_length(version.encrypted_fields) + octet_length(version.item_key_by_account_key)) FROM password_item_version version WHERE version.item_id = item.id)`)
	dbtest.Exec(test, database, `ANALYZE password_item`)
	versionsBytesMatch(test, database)
	test.Logf("%d versions", countOf(test, database, "password_item_version"))

	started := time.Now()
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		for number := 1; number <= 25; number++ {
			if _, err := tx.DeletePasswordItem(world.ada.ID, fmt.Sprintf("00000008-0000-4000-8000-%012d", number), false); err != nil {
				test.Fatalf("a token deletes item %d: %s", number, err)
			}
		}
	})
	elapsed := time.Since(started)
	test.Logf("25 token deletes: %s", elapsed)
	// Summing every row per delete took minutes; the race detector only
	// multiplies the fast path, so its bound is wider.
	bound := 3 * time.Second
	if isRaceDetectorOn {
		bound = 15 * time.Second
	}
	if elapsed > bound {
		test.Fatalf("25 token deletes took %s", elapsed)
	}
}
