package db_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// insertPasswordItemRows adds this many small items for the owner straight
// into the table, numbered from firstNumber, past every check the API makes.
// The ids are in the form of passwordItemIdOf but in another block, so that
// they do not meet the ones the tests save.
func insertPasswordItemRows(test *testing.T, database db.Database, owner *models.User, block, firstNumber, count int, isDeleted bool) {
	test.Helper()
	deletedAt := "NULL"
	if isDeleted {
		deletedAt = "now()"
	}
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at, deleted_at, revision)
		SELECT '%08d-0000-4000-8000-' || lpad(n::text, 12, '0'), '%s', 'note', '{"sealed":"fields"}', '{"sealed":"key"}', now(), now(), %s, nextval('password_item_revision')
		FROM generate_series(%d, %d) n`, block, owner.ID, deletedAt, firstNumber, firstNumber+count-1))
}

// PostgreSQL takes at most 65,535 parameters in a statement. A person with
// more deleted rows than that must still read their list, and erase their
// Passwords.
func TestAListAndAnEraseAnswerMoreDeletedRowsThanAStatementTakesParameters(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	insertPasswordItemRows(test, database, world.ada, 9, 1, 70000, true)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		list := mustListPasswordItems(test, tx, world.ada, 0)
		if len(list.Items) != 70000 {
			test.Fatalf("the first read answered %d items, want 70000", len(list.Items))
		}
		if erased, err := tx.ErasePasswords(world.ada.ID); err != nil || erased != 70000 {
			test.Fatalf("erasing 70000 rows: %d, %v", erased, err)
		}
	})
}

// The same for the items other people share with a person: more than a
// statement takes parameters, small enough that the bytes bound does not stop
// them. The list answers the first models.PasswordsSharedItemsMost, and
// leaving the circle and erasing work.
func TestAListAnswersAtMostTheSharedItemsMostAndAnEraseStillWorks(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	var owners []*models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ben)
		owners = append(owners, world.ada)
		for index := 0; index < 3; index++ {
			user, err := tx.CreateUser(&models.User{Username: fmt.Sprintf("sharer%d", index)})
			if err != nil {
				test.Fatal(err)
			}
			grantPasswordsUseToAll(test, tx, []string{user.ID})
			if _, err := tx.AddCircleMember(world.home.ID, user.ID, models.MemberRoleMember, world.ada.ID); err != nil {
				test.Fatal(err)
			}
			owners = append(owners, user)
		}
	})
	const perOwner = 17500
	for ownerIndex, owner := range owners {
		insertPasswordItemRows(test, database, owner, 10+ownerIndex, 1, perOwner, false)
		prefix := fmt.Sprintf("%08d-0000-4000-8000-", 10+ownerIndex)
		dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share (id, item_id, owner_user_id, grantee_user_id, created_at)
			SELECT 'sh%02d' || lpad(n::text, 27, '0'), '%s' || lpad(n::text, 12, '0'), '%s', '%s', now()
			FROM generate_series(1, %d) n`, ownerIndex, prefix, owner.ID, world.ben.ID, perOwner))
		dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share_key (item_id, recipient_user_id, item_key_by_recipient_key, recipient_public_key, is_stored_by_person, created_at)
			SELECT '%s' || lpad(n::text, 12, '0'), '%s', '%s', '%s', true, now()
			FROM generate_series(1, %d) n`, prefix, world.ben.ID, strings.Repeat("r", 270), passwordsPublicKeyOf(world.ben), perOwner))
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		list := mustListPasswordItems(test, tx, world.ben, 0)
		if len(list.Items) != models.PasswordsSharedItemsMost {
			test.Fatalf("the list answered %d items, want %d", len(list.Items), models.PasswordsSharedItemsMost)
		}
	})
	if err := database.Transaction(func(tx db.Transaction) error {
		_, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID)
		return err
	}); err != nil {
		test.Fatalf("leaving the circle with %d items shared: %s", 4*perOwner, err)
	}
	if err := database.Transaction(func(tx db.Transaction) error {
		_, err := tx.ErasePasswords(world.ben.ID)
		return err
	}); err != nil {
		test.Fatalf("erasing: %s", err)
	}
}

// A person keeps at most models.PasswordRowsMost items, deleted ones included,
// so a program that saves and deletes cannot make the first read of the list
// grow without bound. Deleting and deleting forever are never refused for it,
// so the person makes room.
func TestANewItemIsRefusedAtTheMostRowsAndDeletingForeverMakesRoom(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	insertPasswordItemRows(test, database, world.ada, 9, 1, models.PasswordRowsMost-10, true)
	insertPasswordItemRows(test, database, world.ada, 8, 1, 8, false)
	save := func(tx db.Transaction, number int, isByPerson bool) error {
		_, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: passwordItemIdOf(number), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: isByPerson,
		}, nil)
		return err
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		for number := 1; number <= 2; number++ {
			if err := save(tx, number, true); err != nil {
				test.Fatalf("save %d below the limit: %s", number, err)
			}
		}
	})
	for _, isByPerson := range []bool{false, true} {
		err := database.Transaction(func(tx db.Transaction) error { return save(tx, 3, isByPerson) })
		if !errors.Is(err, db.ErrLimitReached) {
			test.Fatalf("a new item at the most rows (by a tap: %v): %v", isByPerson, err)
		}
	}
	liveId := "00000008-0000-4000-8000-000000000001"
	deletedId := "00000009-0000-4000-8000-000000000001"
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.DeletePasswordItem(world.ada.ID, liveId, true); err != nil {
			test.Fatalf("a tap deletes at the most rows: %s", err)
		}
		if err := save(tx, 3, true); !errors.Is(err, db.ErrLimitReached) {
			test.Fatalf("deleting made no room, the row is still there: %v", err)
		}
		if err := tx.PurgePasswordItem(world.ada.ID, deletedId); err != nil {
			test.Fatalf("a tap deletes forever at the most rows: %s", err)
		}
		if err := save(tx, 3, true); err != nil {
			test.Fatalf("a new item after deleting forever: %s", err)
		}
	})
}

// The owner's list holds the shared revision lock that every other person's
// Passwords write waits for, so it must be linear in the shares and the pairs
// they reach. Three contacts and 16,000 items, 48,000 shares, once took nine
// seconds.
func TestAnOwnersListOfManySharesIsFast(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	const peopleCount, itemCount = 3, 16000
	var world *shareWorld
	var peopleIds []string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		for index := 0; index < peopleCount; index++ {
			user, err := tx.CreateUser(&models.User{Username: fmt.Sprintf("person%03d", index)})
			if err != nil {
				test.Fatal(err)
			}
			if _, err := tx.AddCircleMember(world.home.ID, user.ID, models.MemberRoleMember, world.ada.ID); err != nil {
				test.Fatal(err)
			}
			peopleIds = append(peopleIds, user.ID)
		}
		grantPasswordsUseToAll(test, tx, peopleIds)
	})
	insertPasswordItemRows(test, database, world.ada, 8, 1, itemCount, false)
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share (id, item_id, owner_user_id, grantee_user_id, created_at)
		SELECT 'share' || lpad((n * 1000 + m)::text, 27, '0'), '00000008-0000-4000-8000-' || lpad(n::text, 12, '0'), '%s', person.id, now()
		FROM generate_series(1, %d) n CROSS JOIN (SELECT id, row_number() OVER (ORDER BY id) AS m FROM "user" WHERE username LIKE 'person%%') person`,
		world.ada.ID, itemCount))
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		started := time.Now()
		list := mustListPasswordItems(test, tx, world.ada, 0)
		elapsed := time.Since(started)
		if len(list.Items) != itemCount {
			test.Fatalf("the list answered %d items, want %d", len(list.Items), itemCount)
		}
		for _, item := range list.Items {
			if len(item.Shares) != peopleCount {
				test.Fatalf("an item has %d shares, want %d", len(item.Shares), peopleCount)
			}
		}
		test.Logf("listing 48000 shares took %s", elapsed)
		// Linear takes under a second; the quadratic loop this guards against
		// took about ten. The race detector makes either several times slower,
		// so its bound is wider and still far below the quadratic one.
		bound := 4 * time.Second
		if isRaceDetectorOn {
			bound = 20 * time.Second
		}
		if elapsed > bound {
			test.Fatalf("listing 48000 shares took %s", elapsed)
		}
	})
}

// shareItemWith saves a note of this many bytes for the owner and shares it
// with the recipient, answering the item as the owner then sees it.
func shareItemWith(test *testing.T, tx db.Transaction, owner, recipient *models.User, number, fieldsBytes int) *models.PasswordItem {
	test.Helper()
	item := savePasswordItemOfSize(test, tx, owner, number, fieldsBytes)
	return mustSetPasswordItemShares(test, tx, owner, item.ID, []models.PasswordShareGrantee{{GranteeUserID: recipient.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, recipient)})
}

// One member must not take all of another person's shared allowance: what one
// owner shares with one person stops at models.PasswordsSharedBytesFromOneOwnerMost,
// and another owner still reaches them.
func TestOneOwnerCannotFillAPersonsSharedAllowance(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	const fieldsBytes = 1_000_000
	storedCount := 0
	var lastShared *models.PasswordItem
	for number := 1; number <= 10; number++ {
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			lastShared = shareItemWith(test, tx, world.ada, world.ben, number, fieldsBytes)
			if lastShared.Shares[0].Recipients[0].HasKey {
				storedCount++
			}
		})
	}
	wantCount := models.PasswordsSharedBytesFromOneOwnerMost / (fieldsBytes + len("key-of-ben"))
	if storedCount != wantCount {
		test.Fatalf("%d of ada's items were stored for ben, want %d", storedCount, wantCount)
	}
	if recipient := lastShared.Shares[0].Recipients[0]; recipient.HasKey || !recipient.HasTooMuchShared {
		test.Fatalf("the item past the bound: hasKey %v, hasTooMuchShared %v", recipient.HasKey, recipient.HasTooMuchShared)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item := shareItemWith(test, tx, world.cora, world.ben, 100, 1000)
		if recipient := item.Shares[0].Recipients[0]; !recipient.HasKey || recipient.HasTooMuchShared {
			test.Fatalf("another owner's item for ben: hasKey %v, hasTooMuchShared %v", recipient.HasKey, recipient.HasTooMuchShared)
		}
		if list := mustListPasswordItems(test, tx, world.ben, 0); len(list.Items) != storedCount+1 {
			test.Fatalf("ben lists %d items, want %d", len(list.Items), storedCount+1)
		}
	})
}

// The number of items shared with one person has a bound beside the bytes.
func TestTooManySharedItemsAreNotStoredForAPerson(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	insertPasswordItemRows(test, database, world.cora, 7, 1, models.PasswordsSharedItemsMost, false)
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share (id, item_id, owner_user_id, grantee_user_id, created_at)
		SELECT 'sh' || lpad(n::text, 29, '0'), '00000007-0000-4000-8000-' || lpad(n::text, 12, '0'), '%s', '%s', now()
		FROM generate_series(1, %d) n`, world.cora.ID, world.ben.ID, models.PasswordsSharedItemsMost))
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share_key (item_id, recipient_user_id, item_key_by_recipient_key, recipient_public_key, is_stored_by_person, created_at)
		SELECT '00000007-0000-4000-8000-' || lpad(n::text, 12, '0'), '%s', 'key', '%s', true, now()
		FROM generate_series(1, %d) n`, world.ben.ID, passwordsPublicKeyOf(world.ben), models.PasswordsSharedItemsMost))
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item := shareItemWith(test, tx, world.ada, world.ben, 1, 100)
		if recipient := item.Shares[0].Recipients[0]; recipient.HasKey || !recipient.HasTooMuchShared {
			test.Fatalf("the item past the count: hasKey %v, hasTooMuchShared %v", recipient.HasKey, recipient.HasTooMuchShared)
		}
	})
}

// An item that grows after it was shared would silently push out the items
// other people shared with the same person, so a save or a re-key that makes
// it larger is refused when a person with a key would go past a bound.
func TestAnItemSharedWithAPersonCannotGrowPastTheirAllowance(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	const wantMessage = "ben has too much shared with them: make this item smaller or stop sharing it with them"
	var small *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		small = shareItemWith(test, tx, world.ada, world.ben, 1, 100_000)
		for number := 2; number <= 8; number++ {
			shareItemWith(test, tx, world.ada, world.ben, number, 1_000_000)
		}
	})
	grow := func(tx db.Transaction, item *models.PasswordItem, fieldsBytes int) (*models.PasswordItem, error) {
		return tx.SavePasswordItem(&models.PasswordItem{
			ID: item.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: strings.Repeat("x", fieldsBytes), ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: true,
		}, &item.Revision)
	}
	revisionBefore := lastPasswordRevision(test, database)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		_, err := grow(tx, small, 2_000_000)
		var full *db.PasswordRecipientFullError
		if !errors.Is(err, db.ErrPasswordRecipientFull) || !errors.As(err, &full) || err.Error() != wantMessage {
			test.Fatalf("a save past the owner's bound: %v", err)
		}
		_, err = tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
			ItemID: small.ID, BaseRevision: small.Revision, EncryptedFields: strings.Repeat("x", 2_000_000), ItemKeyByAccountKey: `{"sealed":"key 2"}`,
			ShareKeys:       []*models.PasswordShareKey{{ItemID: small.ID, RecipientUserID: world.ben.ID, ItemKeyByRecipientKey: "key-of-ben", RecipientPublicKey: passwordsPublicKeyOf(world.ben)}},
			IsSavedByPerson: true,
		})
		if !errors.Is(err, db.ErrPasswordRecipientFull) || err.Error() != wantMessage {
			test.Fatalf("a re-key past the owner's bound: %v", err)
		}
	})
	if got := lastPasswordRevision(test, database); got != revisionBefore {
		test.Fatalf("refused changes drew revisions: %s then %s", revisionBefore, got)
	}
	// Growing within the bound, and making the item smaller, go ahead.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		grown, err := grow(tx, small, 1_000_000)
		if err != nil {
			test.Fatalf("a save within the bound: %s", err)
		}
		if _, err := grow(tx, grown, 1000); err != nil {
			test.Fatalf("a smaller save: %s", err)
		}
	})
}

// The same for the total, which other owners' items also count toward.
func TestAnItemCannotGrowPastTheTotalSharedAllowance(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	// Cora holds a key for ben on one large item, put in past the checks of the
	// API, so that ada's items fit one at a time and not together.
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at, revision)
		VALUES ('00000007-0000-4000-8000-000000000001', '%s', 'note', repeat('x', %d), '{"sealed":"key"}', now(), now(), nextval('password_item_revision'))`,
		world.cora.ID, models.PasswordsSharedBytesMost-3_000_000))
	dbtest.Exec(test, database, fmt.Sprintf(`INSERT INTO password_share_key (item_id, recipient_user_id, item_key_by_recipient_key, recipient_public_key, is_stored_by_person, created_at)
		VALUES ('00000007-0000-4000-8000-000000000001', '%s', 'key', '%s', true, now())`, world.ben.ID, passwordsPublicKeyOf(world.ben)))
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item = shareItemWith(test, tx, world.ada, world.ben, 1, 1_000_000)
		if !item.Shares[0].Recipients[0].HasKey {
			test.Fatal("the item of 1 MB was not stored for ben")
		}
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		_, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: item.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: strings.Repeat("x", 5_000_000), ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: true,
		}, &item.Revision)
		if !errors.Is(err, db.ErrPasswordRecipientFull) {
			test.Fatalf("a save past the total: %v", err)
		}
	})
}
