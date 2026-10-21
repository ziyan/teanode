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

// savePasswordItemOfSize saves an item whose fields are this many bytes, with
// the item key savePasswordItemFor uses.
func savePasswordItemOfSize(test *testing.T, tx db.Transaction, owner *models.User, number, fieldsBytes int) *models.PasswordItem {
	test.Helper()
	item, err := tx.SavePasswordItem(&models.PasswordItem{
		ID: passwordItemIdOf(number), OwnerUserID: owner.ID, ItemKind: models.PasswordItemKindNote,
		EncryptedFields: strings.Repeat("x", fieldsBytes), ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: true,
	}, nil)
	if err != nil {
		test.Fatalf("SavePasswordItem %d of %d bytes: %s", number, fieldsBytes, err)
	}
	return item
}

// grantPasswordsUseToAll lets all these people use Passwords with one role and
// one group, which is much cheaper than a role for each.
func grantPasswordsUseToAll(test *testing.T, tx db.Transaction, userIds []string) {
	test.Helper()
	role, err := tx.CreateRole(&models.Role{Name: "Passwords for " + userIds[0], Permissions: []models.Permission{models.PermissionPasswordsUse}})
	if err != nil {
		test.Fatal(err)
	}
	if _, err := tx.CreateGroup(&models.Group{Name: "Group of " + userIds[0], RoleIDs: []string{role.ID}, UserIDs: userIds}); err != nil {
		test.Fatal(err)
	}
}

// Restoring a deleted item lets the shares it had reach their recipients again,
// waiting for the owner's devices to seal for them: that is disclosing the item,
// so a call that is not the person's own tap does not do it.
func TestRestoringAPasswordItemNeedsATap(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if _, err := tx.DeletePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatal(err)
		}
		if _, err := tx.RestorePasswordItem(world.ada.ID, item.ID, false); !errors.Is(err, db.ErrPasswordTapRequired) {
			test.Fatalf("a restore that is not a tap: %v", err)
		}
		if waiting, err := tx.ListWaitingPasswordShares(world.ada.ID); err != nil || len(waiting) != 0 {
			test.Fatalf("after the refused restore, ben waits on %d items (%v)", len(waiting), err)
		}
		if stored, err := tx.GetPasswordItem(world.ada.ID, item.ID); err != nil || stored.DeletedAt == nil {
			test.Fatalf("the item after the refused restore: %+v %v", stored, err)
		}
		if _, err := tx.RestorePasswordItem(world.ada.ID, item.ID, true); err != nil {
			test.Fatalf("a restore by a tap: %v", err)
		}
		if waiting, err := tx.ListWaitingPasswordShares(world.ada.ID); err != nil || len(waiting) != 1 {
			test.Fatalf("after the restore, ben waits on %d items (%v)", len(waiting), err)
		}
	})
}

// The limit of items counts the items that are not deleted: a token that
// fills the places with tiny junk leaves the person able to delete some and
// save again, and a deleted item does not take a place back until it is
// restored.
func TestDeletedPasswordItemsDoNotUseUpItemPlaces(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	dbtest.Exec(test, database, `INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at)
		SELECT '00000001-0000-4000-8000-' || lpad(n::text, 12, '0'), '`+world.ada.ID+`', 'login', '{"sealed":"x"}', '{"sealed":"k"}', now(), now() FROM generate_series(1, 20000) n`)
	junkId := func(number int) string { return fmt.Sprintf("00000001-0000-4000-8000-%012d", number) }
	save := func(number int, isByPerson bool) error {
		var saveErr error
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			_, saveErr = tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(number), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`, IsSavedByPerson: isByPerson,
			}, nil)
		})
		return saveErr
	}
	if err := save(1, true); !errors.Is(err, db.ErrLimitReached) {
		test.Fatalf("a save with 20000 items: %v", err)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.DeletePasswordItem(world.ada.ID, junkId(1), true); err != nil {
			test.Fatal(err)
		}
		if got, err := tx.CountPasswordItems(world.ada.ID); err != nil || got != 19999 {
			test.Fatalf("the items that are not deleted: %d %v", got, err)
		}
	})
	if err := save(1, true); err != nil {
		test.Fatalf("a save after deleting one item: %v", err)
	}
	// The deleted item cannot come back while the places are all used, and can
	// once one is free.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.RestorePasswordItem(world.ada.ID, junkId(1), true); !errors.Is(err, db.ErrLimitReached) {
			test.Fatalf("a restore with 20000 items: %v", err)
		}
		if _, err := tx.DeletePasswordItem(world.ada.ID, junkId(2), true); err != nil {
			test.Fatal(err)
		}
		if _, err := tx.RestorePasswordItem(world.ada.ID, junkId(1), true); err != nil {
			test.Fatalf("a restore with a place free: %v", err)
		}
	})
}

// holdCircleThenSavePasswords runs a request that does the first step, waits
// for a person to start leaving the circle, then saves an item of Passwords,
// and answers how both ended. The two used to wait for each other: the first
// step held the circle's row, the leaving held the circle membership lock and
// waited for that row, and the save wanted the membership lock.
func holdCircleThenSavePasswords(test *testing.T, database db.Database, world *shareWorld, firstStep func(tx db.Transaction) error) {
	test.Helper()
	started := make(chan struct{})
	leaving := make(chan struct{})
	requestDone := make(chan error, 1)
	go func() {
		requestDone <- database.Transaction(func(tx db.Transaction) error {
			if err := firstStep(tx); err != nil {
				close(started)
				return err
			}
			close(started)
			<-leaving
			time.Sleep(500 * time.Millisecond)
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(1), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			return err
		})
	}()
	<-started
	leaveDone := make(chan error, 1)
	go func() {
		close(leaving)
		leaveDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID)
			return err
		})
	}()
	for name, done := range map[string]chan error{"the request": requestDone, "leaving the circle": leaveDone} {
		select {
		case err := <-done:
			if err != nil {
				test.Fatalf("%s: %v", name, err)
			}
		case <-time.After(20 * time.Second):
			test.Fatalf("%s did not finish in 20 seconds: the two wait for each other", name)
		}
	}
}

// Making a circle's calendar takes the circle membership lock before the
// circle's row, as every change to circles does.
func TestMakingACircleCalendarThenSavingPasswordsDoesNotDeadlockWithLeaving(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	holdCircleThenSavePasswords(test, database, world, func(tx db.Transaction) error {
		_, err := tx.CreateCircleCalendar(world.home.ID, "Shared", "UTC")
		return err
	})
}

// Making the chats of a person's circles takes the circle membership lock
// before the circles' rows.
func TestSyncingChatsThenSavingPasswordsDoesNotDeadlockWithLeaving(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
	})
	// A circle with no chat, as one from before circles had chats is.
	dbtest.Exec(test, database, `DELETE FROM chat WHERE circle_id = '`+world.home.ID+`'`)
	holdCircleThenSavePasswords(test, database, world, func(tx db.Transaction) error {
		return tx.SyncChatsOf(world.ada.ID)
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM chat WHERE circle_id = '`+world.home.ID+`'`); got == "0" {
		test.Fatalf("the sync did not make the circle's chat")
	}
}

// An owner's items may reach at most models.PasswordSharePairsMost pairs of an
// item and a person: a change of shares that adds to the number past it is
// refused, with the numbers of the audit (about 100 people, 500 items). One
// that does not add is always allowed, so that an owner whose circle grew
// beyond the bound can narrow.
func TestOneOwnersItemsReachABoundedNumberOfPairs(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	const memberCount, itemCount = 100, 500
	var world *shareWorld
	var memberIds []string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		for index := 0; index < memberCount-1; index++ {
			user, err := tx.CreateUser(&models.User{Username: fmt.Sprintf("member%03d", index)})
			if err != nil {
				test.Fatal(err)
			}
			if _, err := tx.AddCircleMember(world.home.ID, user.ID, models.MemberRoleMember, world.ada.ID); err != nil {
				test.Fatal(err)
			}
			memberIds = append(memberIds, user.ID)
		}
		grantPasswordsUseToAll(test, tx, memberIds)
	})
	// The home circle has ada, ben and 99 more: 100 people besides ada.
	dbtest.Exec(test, database, `INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at)
		SELECT '00000002-0000-4000-8000-' || lpad(n::text, 12, '0'), '`+world.ada.ID+`', 'login', '{"sealed":"x"}', '{"sealed":"k"}', now(), now() FROM generate_series(1, `+fmt.Sprint(itemCount)+`) n`)
	dbtest.Exec(test, database, `INSERT INTO password_share (id, item_id, owner_user_id, grantee_circle_id, created_at)
		SELECT 'share' || lpad(n::text, 27, '0'), '00000002-0000-4000-8000-' || lpad(n::text, 12, '0'), '`+world.ada.ID+`', '`+world.home.ID+`', now() FROM generate_series(1, `+fmt.Sprint(itemCount)+`) n`)
	if want := memberCount * itemCount; want != models.PasswordSharePairsMost {
		test.Fatalf("the test's numbers reach %d pairs, not the bound %d", want, models.PasswordSharePairsMost)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		// Sharing one more item with the circle adds 100 pairs: 50100.
		_, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}}, nil, true)
		if !errors.Is(err, db.ErrPasswordSharesFull) {
			test.Fatalf("a share that takes the owner over the bound: %v", err)
		}
		// Nothing was written: the item has no share and its revision did not move.
		stored, err := tx.GetPasswordItem(world.ada.ID, item.ID)
		if err != nil || len(stored.Shares) != 0 || stored.Revision != item.Revision {
			test.Fatalf("the item after the refused share: %+v %v", stored, err)
		}
		// Sharing with one person adds one pair, past the bound too.
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil, true); !errors.Is(err, db.ErrPasswordSharesFull) {
			test.Fatalf("a share with one person: %v", err)
		}
		// Taking one of the 500 items out of the circle narrows: allowed.
		if _, err := tx.SetPasswordItemShares(world.ada.ID, fmt.Sprintf("00000002-0000-4000-8000-%012d", 1), nil, nil, true); err != nil {
			test.Fatalf("narrowing at the bound: %v", err)
		}
		// With room again for 100 pairs, the share goes ahead.
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}}, nil, true); err != nil {
			test.Fatalf("a share that fits: %v", err)
		}
	})
	// A person joining the circle takes the owner over the bound with no
	// change of shares. The owner may still narrow, and may not add.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		joiner, err := tx.CreateUser(&models.User{Username: "joiner"})
		if err != nil {
			test.Fatal(err)
		}
		grantPasswordsUseToAll(test, tx, []string{joiner.ID})
		if _, err := tx.AddCircleMember(world.home.ID, joiner.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatal(err)
		}
		other := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindLogin)
		if _, err := tx.SetPasswordItemShares(world.ada.ID, other.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil, true); !errors.Is(err, db.ErrPasswordSharesFull) {
			test.Fatalf("adding while over the bound: %v", err)
		}
		if _, err := tx.SetPasswordItemShares(world.ada.ID, passwordItemIdOf(1), nil, nil, true); err != nil {
			test.Fatalf("narrowing while over the bound: %v", err)
		}
	})
}

// Items other people share with a person hold at most
// models.PasswordsSharedBytesMost of sealed text together, whoever shared them:
// the person cannot decline a share. A key that would take the recipient past
// it is not stored for them, the other recipients of the call get theirs, the
// owner can see who has too much shared with them, and the room freed by a
// deleted item can be used again.
func TestWhatOthersShareWithAPersonIsBounded(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	const fieldsBytes = 1_000_000
	var world *shareWorld
	var eve *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
		var err error
		if eve, err = tx.CreateUser(&models.User{Username: "eve"}); err != nil {
			test.Fatal(err)
		}
		grantPasswordsUseToAll(test, tx, []string{eve.ID})
		setUpPasswordsFor(test, tx, eve)
		if _, err := tx.AddCircleMember(world.home.ID, eve.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatal(err)
		}
	})
	benBytes := func() int64 {
		return mustParseInt(test, dbtest.QueryString(test, database, `SELECT COALESCE(SUM(octet_length(item.encrypted_fields) + octet_length(share_key.item_key_by_recipient_key)), 0)::text
			FROM password_share_key share_key JOIN password_item item ON item.id = share_key.item_id WHERE share_key.recipient_user_id = '`+world.ben.ID+`'`))
	}
	// Ada shares 40 items with ben (about 40 MB), cora 30 (about 30 MB): 70 MB
	// is more than 64 MiB, so the last three of cora's are not stored for ben.
	var coraItems []*models.PasswordItem
	for number := 1; number <= 40; number++ {
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			item := savePasswordItemOfSize(test, tx, world.ada, number, fieldsBytes)
			mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		})
	}
	for number := 1; number <= 30; number++ {
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			item := savePasswordItemOfSize(test, tx, world.cora, 1000+number, fieldsBytes)
			coraItems = append(coraItems, item)
			shared := mustSetPasswordItemShares(test, tx, world.cora, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
			isFull := false
			for _, share := range shared.Shares {
				for _, recipient := range share.Recipients {
					isFull = isFull || recipient.HasTooMuchShared
				}
			}
			if isFull != !shared.Shares[0].Recipients[0].HasKey {
				test.Fatalf("item %d: hasKey %v and hasTooMuchShared %v disagree", number, shared.Shares[0].Recipients[0].HasKey, isFull)
			}
		})
	}
	if got := benBytes(); got > models.PasswordsSharedBytesMost || got < models.PasswordsSharedBytesMost-fieldsBytes-1000 {
		test.Fatalf("ben holds %d bytes shared with him, the most is %d", got, models.PasswordsSharedBytesMost)
	}
	storedForBen := mustParseInt(test, dbtest.QueryString(test, database, `SELECT count(*)::text FROM password_share_key WHERE recipient_user_id = '`+world.ben.ID+`'`))
	if storedForBen != 67 {
		test.Fatalf("ben holds keys for %d items, want 67 of 70", storedForBen)
	}

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		// Cora is told who has too much shared with them: the three newest
		// items wait, with a flag, and the newest come first.
		waiting, err := tx.ListWaitingPasswordShares(world.cora.ID)
		if err != nil || len(waiting) != 3 {
			test.Fatalf("cora's waiting list: %d entries, %v", len(waiting), err)
		}
		for index, entry := range waiting {
			if !entry.Recipient.HasTooMuchShared || entry.Recipient.UserID != world.ben.ID || entry.HasMoreWaiting {
				test.Fatalf("waiting entry %d: %+v %+v", index, entry, entry.Recipient)
			}
			if want := coraItems[len(coraItems)-1-index].ID; entry.ItemID != want {
				test.Fatalf("waiting entry %d is for %s, the newest first would be %s", index, entry.ItemID, want)
			}
		}
		// Ben's list holds the 67 and stays within the bound.
		list := mustListPasswordItems(test, tx, world.ben, 0)
		if len(list.Items) != 67 || len(list.ItemIDs) != 67 {
			test.Fatalf("ben lists %d items and %d identifiers", len(list.Items), len(list.ItemIDs))
		}
		// A call that is not a tap, or a later one, stores nothing more for him.
		last := coraItems[len(coraItems)-1]
		stored, err := tx.AddPasswordShareKeys(world.cora.ID, []*models.PasswordShareKey{passwordKeyFor(last, world.ben)}, true)
		if err != nil || stored != 0 {
			test.Fatalf("a key for the recipient with too much shared with them: %d %v", stored, err)
		}
	})

	// A circle share with a person who is full and one who is not: the second
	// gets the key, the first does not.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		item := savePasswordItemOfSize(test, tx, world.ada, 100, fieldsBytes)
		keys := []*models.PasswordShareKey{passwordKeyFor(item, world.ben), passwordKeyFor(item, eve)}
		shared := mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}}, keys)
		for _, recipient := range shared.Shares[0].Recipients {
			switch recipient.UserID {
			case world.ben.ID:
				if recipient.HasKey || !recipient.HasTooMuchShared {
					test.Fatalf("ben after the circle share: %+v", recipient)
				}
			case eve.ID:
				if !recipient.HasKey || recipient.HasTooMuchShared {
					test.Fatalf("eve after the circle share: %+v", recipient)
				}
			}
		}
	})

	// Room freed by deleting one of the items shared with ben is room for the next.
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.DeletePasswordItem(world.ada.ID, passwordItemIdOf(1), true); err != nil {
			test.Fatal(err)
		}
		last := coraItems[len(coraItems)-3]
		stored, err := tx.AddPasswordShareKeys(world.cora.ID, []*models.PasswordShareKey{passwordKeyFor(last, world.ben)}, true)
		if err != nil || stored != 1 {
			test.Fatalf("a key after room was freed: %d %v", stored, err)
		}
	})
}

// The list answers what is shared with a person only up to the bound, the keys
// stored first first, even when an item grew after its key was stored (the
// owner's own allowance is no bound on what reaches somebody else).
func TestAListAnswersWhatIsSharedWithAPersonOnlyUpToTheBound(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
	})
	var first, second, third *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		first = savePasswordItemOfSize(test, tx, world.ada, 1, 40_000_000)
		second = savePasswordItemOfSize(test, tx, world.ada, 2, 20_000_000)
		first = mustSetPasswordItemShares(test, tx, world.ada, first.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(first, world.ben)})
		second = mustSetPasswordItemShares(test, tx, world.ada, second.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(second, world.ben)})
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		third = savePasswordItemOfSize(test, tx, world.cora, 1003, 7_000_000)
		mustSetPasswordItemShares(test, tx, world.cora, third.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(third, world.ben)})
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 3 {
			test.Fatalf("ben lists %d items before the growth, want 3", len(got.Items))
		}
		// Ada's second item grows to 27 MB, which her own allowance permits: ben
		// would hold 74 MB, so the key stored last (cora's) is left out.
		if _, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: second.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: strings.Repeat("y", 27_000_000), ItemKeyByAccountKey: second.ItemKeyByAccountKey, IsSavedByPerson: true,
		}, &second.Revision); err != nil {
			test.Fatal(err)
		}
		got := mustListPasswordItems(test, tx, world.ben, 0)
		var ids []string
		for _, item := range got.Items {
			ids = append(ids, item.ID)
		}
		if len(got.Items) != 2 || len(got.ItemIDs) != 2 {
			test.Fatalf("ben lists %d items and %d identifiers after the growth, want ada's two: %v", len(got.Items), len(got.ItemIDs), ids)
		}
		for _, item := range got.Items {
			if item.OwnerUserID != world.ada.ID {
				test.Fatalf("ben lists an item of %s", item.OwnerUserID)
			}
		}
	})
}

// A waiting list answers at most models.PasswordWaitingSharesMost entries, the
// newest items first, and every entry says when more wait. The numbers are
// those of the audit (a circle of 40 people and 300 items).
func TestTheWaitingListIsBounded(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	const memberCount, itemCount = 40, 300
	var world *shareWorld
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		var memberIds []string
		for index := 0; index < memberCount-1; index++ {
			user, err := tx.CreateUser(&models.User{Username: fmt.Sprintf("member%03d", index)})
			if err != nil {
				test.Fatal(err)
			}
			if _, err := tx.AddCircleMember(world.home.ID, user.ID, models.MemberRoleMember, world.ada.ID); err != nil {
				test.Fatal(err)
			}
			memberIds = append(memberIds, user.ID)
		}
		grantPasswordsUseToAll(test, tx, memberIds)
		for _, userId := range memberIds {
			setUpPasswordsFor(test, tx, &models.User{ID: userId, Username: "of-" + userId})
		}
		// Ben is in the circle too, and has not set Passwords up: he never waits.
	})
	dbtest.Exec(test, database, `INSERT INTO password_item (id, owner_user_id, item_kind, encrypted_fields, item_key_by_account_key, created_at, updated_at)
		SELECT '00000003-0000-4000-8000-' || lpad(n::text, 12, '0'), '`+world.ada.ID+`', 'login', '{"sealed":"x"}', '{"sealed":"k"}', now() + n * interval '1 second', now() FROM generate_series(1, `+fmt.Sprint(itemCount)+`) n`)
	dbtest.Exec(test, database, `INSERT INTO password_share (id, item_id, owner_user_id, grantee_circle_id, created_at)
		SELECT 'share' || lpad(n::text, 27, '0'), '00000003-0000-4000-8000-' || lpad(n::text, 12, '0'), '`+world.ada.ID+`', '`+world.home.ID+`', now() FROM generate_series(1, `+fmt.Sprint(itemCount)+`) n`)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		waiting, err := tx.ListWaitingPasswordShares(world.ada.ID)
		if err != nil {
			test.Fatal(err)
		}
		if len(waiting) != models.PasswordWaitingSharesMost {
			test.Fatalf("the waiting list has %d entries, want %d (of %d)", len(waiting), models.PasswordWaitingSharesMost, (memberCount-1)*itemCount)
		}
		for _, entry := range waiting {
			if !entry.HasMoreWaiting {
				test.Fatalf("an entry of a cut list does not say more wait: %+v", entry)
			}
		}
		if want := fmt.Sprintf("00000003-0000-4000-8000-%012d", itemCount); waiting[0].ItemID != want {
			test.Fatalf("the first entry is for %s, the newest item is %s", waiting[0].ItemID, want)
		}
	})
}
