package db_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// passwordsWorld is the people the Passwords tests use: ada and ben in "home",
// ben and cora in "club", dave in none, all of them allowed to use Passwords.
func newPasswordsWorld(test *testing.T, tx db.Transaction) *shareWorld {
	test.Helper()
	world := newShareWorld(test, tx)
	for _, person := range []*models.User{world.ada, world.ben, world.cora, world.dave} {
		dbtest.GrantPermission(test, tx, person.ID, models.PermissionPasswordsUse)
	}
	return world
}

// passwordsProofOf is a proof as the client sends it, and the hash the server
// stores of it.
func passwordsProofOf(text string) (string, string) {
	proof := base64.StdEncoding.EncodeToString([]byte(text))
	digest := sha256.Sum256([]byte(text))
	return proof, base64.StdEncoding.EncodeToString(digest[:])
}

// passwordsPublicKeyOf is the made-up public key a test person sets up.
func passwordsPublicKeyOf(person *models.User) string {
	return "public-key-of-" + person.Username
}

func setUpPasswordsFor(test *testing.T, tx db.Transaction, person *models.User) *models.PasswordSetup {
	test.Helper()
	_, passphraseHash := passwordsProofOf("passphrase of " + person.Username)
	_, recoveryHash := passwordsProofOf("recovery key of " + person.Username)
	setup, err := tx.CreatePasswordsSetup(&models.PasswordSetup{
		UserID: person.ID, AccountKeyByPassphrase: `{"sealed":"a"}`, PassphraseSalt: "c2FsdA==", PassphraseIterationCount: 600000,
		PassphraseProofHash: passphraseHash, AccountKeyByRecoveryKey: `{"sealed":"b"}`, RecoveryKeyProofHash: recoveryHash,
		PublicKey: passwordsPublicKeyOf(person), PrivateKeyByAccountKey: `{"sealed":"c"}`,
	})
	if err != nil {
		test.Fatalf("CreatePasswordsSetup for %s: %s", person.Username, err)
	}
	return setup
}

// passwordItemIdOf is a lower case UUID that differs for each number.
func passwordItemIdOf(number int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", number)
}

func savePasswordItemFor(test *testing.T, tx db.Transaction, owner *models.User, number int, kind models.PasswordItemKind) *models.PasswordItem {
	test.Helper()
	item, err := tx.SavePasswordItem(&models.PasswordItem{
		ID: passwordItemIdOf(number), OwnerUserID: owner.ID, ItemKind: kind,
		EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
	}, nil)
	if err != nil {
		test.Fatalf("SavePasswordItem %d: %s", number, err)
	}
	return item
}

func passwordKeyFor(item *models.PasswordItem, recipient *models.User) *models.PasswordShareKey {
	return &models.PasswordShareKey{
		ItemID: item.ID, RecipientUserID: recipient.ID,
		ItemKeyByRecipientKey: "key-of-" + recipient.Username, RecipientPublicKey: passwordsPublicKeyOf(recipient),
		// The item key savePasswordItemFor saves items with, as the device opened it.
		ItemKeyByAccountKey: `{"sealed":"key"}`,
	}
}

func mustListPasswordItems(test *testing.T, tx db.Transaction, viewer *models.User, sinceRevision int64) *models.PasswordItemList {
	test.Helper()
	list, err := tx.ListPasswordItems(viewer.ID, sinceRevision)
	if err != nil {
		test.Fatalf("ListPasswordItems for %s: %s", viewer.Username, err)
	}
	return list
}

func mustSetPasswordItemShares(test *testing.T, tx db.Transaction, owner *models.User, itemId string, grantees []models.PasswordShareGrantee, keys []*models.PasswordShareKey) *models.PasswordItem {
	test.Helper()
	item, err := tx.SetPasswordItemShares(owner.ID, itemId, grantees, keys)
	if err != nil {
		test.Fatalf("SetPasswordItemShares: %s", err)
	}
	return item
}

// A person sets up Passwords once. The proofs are compared by their hash, the
// hashes are not part of what the model sends anywhere, and a changed
// passphrase replaces the first and leaves the recovery key alone.
func TestPasswordsSetupIsOnceAndProofsAreChecked(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		if setup, err := tx.GetPasswordsSetup(world.ada.ID); err != nil || setup != nil {
			test.Fatalf("a person who set nothing up: %+v %v", setup, err)
		}
		if _, err := tx.CheckPasswordsProof(world.ada.ID, "AAAA", false); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a proof without a setup: %v", err)
		}
		setup := setUpPasswordsFor(test, tx, world.ada)
		if setup.LockAfterMinutes != models.PasswordsLockAfterMinutesDefault || setup.PublicKey != passwordsPublicKeyOf(world.ada) {
			test.Fatalf("a new setup: %+v", setup)
		}
		if _, err := tx.CreatePasswordsSetup(&models.PasswordSetup{
			UserID: world.ada.ID, AccountKeyByPassphrase: "a", PassphraseSalt: "b", PassphraseIterationCount: 1, PassphraseProofHash: "c",
			AccountKeyByRecoveryKey: "d", RecoveryKeyProofHash: "e", PublicKey: "f", PrivateKeyByAccountKey: "g",
		}); !errors.Is(err, db.ErrAlreadyExists) {
			test.Fatalf("setting up twice: %v", err)
		}
		if _, err := tx.CreatePasswordsSetup(&models.PasswordSetup{UserID: world.ben.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a setup with nothing in it: %v", err)
		}

		passphraseProof, _ := passwordsProofOf("passphrase of ada")
		recoveryProof, _ := passwordsProofOf("recovery key of ada")
		wrongProof, _ := passwordsProofOf("something else")
		for name, testCase := range map[string]struct {
			proof         string
			isRecoveryKey bool
			want          bool
		}{
			"passphrase":             {passphraseProof, false, true},
			"recovery":               {recoveryProof, true, true},
			"passphrase as recovery": {passphraseProof, true, false},
			"recovery as passphrase": {recoveryProof, false, false},
			"wrong":                  {wrongProof, false, false},
			"not base64":             {"%%%", false, false},
			"empty":                  {"", false, false},
		} {
			got, err := tx.CheckPasswordsProof(world.ada.ID, testCase.proof, testCase.isRecoveryKey)
			if err != nil || got != testCase.want {
				test.Errorf("%s: %v %v", name, got, err)
			}
		}

		newProof, newHash := passwordsProofOf("a new passphrase")
		changed, err := tx.ChangePasswordsPassphrase(world.ada.ID, &models.PasswordPassphraseChange{
			AccountKeyByPassphrase: `{"sealed":"new"}`, PassphraseSalt: "bmV3", PassphraseIterationCount: 700000, PassphraseProofHash: newHash,
		})
		if err != nil || changed.PassphraseIterationCount != 700000 || changed.AccountKeyByRecoveryKey != `{"sealed":"b"}` {
			test.Fatalf("a new passphrase: %+v %v", changed, err)
		}
		if isRight, _ := tx.CheckPasswordsProof(world.ada.ID, passphraseProof, false); isRight {
			test.Error("the old passphrase still opens")
		}
		if isRight, _ := tx.CheckPasswordsProof(world.ada.ID, newProof, false); !isRight {
			test.Error("the new passphrase does not open")
		}
		if isRight, _ := tx.CheckPasswordsProof(world.ada.ID, recoveryProof, true); !isRight {
			test.Error("the recovery key stopped opening with the passphrase change")
		}
		if _, err := tx.ChangePasswordsPassphrase(world.ben.ID, &models.PasswordPassphraseChange{
			AccountKeyByPassphrase: "a", PassphraseSalt: "b", PassphraseIterationCount: 1, PassphraseProofHash: "c",
		}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a passphrase change without a setup: %v", err)
		}

		newRecoveryProof, newRecoveryHash := passwordsProofOf("a new recovery key")
		if _, err := tx.ReplacePasswordsRecoveryKey(world.ada.ID, `{"sealed":"recovered"}`, newRecoveryHash); err != nil {
			test.Fatalf("a new recovery key: %s", err)
		}
		if isRight, _ := tx.CheckPasswordsProof(world.ada.ID, recoveryProof, true); isRight {
			test.Error("the old recovery key still opens")
		}
		if isRight, _ := tx.CheckPasswordsProof(world.ada.ID, newRecoveryProof, true); !isRight {
			test.Error("the new recovery key does not open")
		}

		for _, minutes := range []int{0, -1, 10081} {
			if _, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, minutes); !errors.Is(err, db.ErrInvalidArguments) {
				test.Errorf("locking after %d minutes: %v", minutes, err)
			}
		}
		if locked, err := tx.SetPasswordsLockAfterMinutes(world.ada.ID, 10080); err != nil || locked.LockAfterMinutes != 10080 {
			test.Fatalf("locking after a week: %+v %v", locked, err)
		}
	})
}

// A save is based on the revision the device read. A stale one is refused,
// another person's item is not found, an item keeps its kind, and a deleted
// item can be restored and not saved.
func TestSavingAPasswordItemChecksTheRevision(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		first := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		second := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindNote)
		if first.Revision <= 0 || second.Revision <= first.Revision {
			test.Fatalf("revisions rise: %d then %d", first.Revision, second.Revision)
		}

		edit := func(owner *models.User, kind models.PasswordItemKind, baseRevision *int64) (*models.PasswordItem, error) {
			return tx.SavePasswordItem(&models.PasswordItem{
				ID: first.ID, OwnerUserID: owner.ID, ItemKind: kind, EncryptedFields: `{"sealed":"changed"}`, ItemKeyByAccountKey: first.ItemKeyByAccountKey,
			}, baseRevision)
		}
		if _, err := edit(world.ada, models.PasswordItemKindLogin, nil); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a new item with the id of one that exists: %v", err)
		}
		stale := first.Revision - 1
		if _, err := edit(world.ada, models.PasswordItemKindLogin, &stale); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a stale revision: %v", err)
		}
		if _, err := edit(world.ben, models.PasswordItemKindLogin, &first.Revision); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's item: %v", err)
		}
		if _, err := edit(world.ada, models.PasswordItemKindNote, &first.Revision); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a change of kind: %v", err)
		}
		saved, err := edit(world.ada, models.PasswordItemKindLogin, &first.Revision)
		if err != nil || saved.Revision <= second.Revision || saved.EncryptedFields != `{"sealed":"changed"}` {
			test.Fatalf("a save on the current revision: %+v %v", saved, err)
		}
		if _, err := edit(world.ada, models.PasswordItemKindLogin, &first.Revision); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("the same base twice: %v", err)
		}
		missing := int64(5)
		if _, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: passwordItemIdOf(99), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote, EncryptedFields: "x", ItemKeyByAccountKey: "y",
		}, &missing); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a base revision for an item that is not there: %v", err)
		}
		if _, err := tx.SavePasswordItem(&models.PasswordItem{ID: "x", OwnerUserID: world.ada.ID, ItemKind: "folder", EncryptedFields: "x", ItemKeyByAccountKey: "y"}, nil); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a kind nobody knows: %v", err)
		}

		deleted, err := tx.DeletePasswordItem(world.ada.ID, first.ID)
		if err != nil || deleted.DeletedAt == nil || deleted.Revision <= saved.Revision {
			test.Fatalf("a delete: %+v %v", deleted, err)
		}
		if _, err := edit(world.ada, models.PasswordItemKindLogin, &deleted.Revision); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("saving a deleted item: %v", err)
		}
		if count, _ := tx.CountPasswordItems(world.ada.ID); count != 1 {
			test.Fatalf("a deleted item is not counted: %d", count)
		}
		again, err := tx.DeletePasswordItem(world.ada.ID, first.ID)
		if err != nil || again.Revision != deleted.Revision {
			test.Fatalf("deleting twice changes nothing: %+v %v", again, err)
		}
		if _, err := tx.DeletePasswordItem(world.ben.ID, first.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("deleting somebody else's: %v", err)
		}
		restored, err := tx.RestorePasswordItem(world.ada.ID, first.ID)
		if err != nil || restored.DeletedAt != nil || restored.Revision <= deleted.Revision {
			test.Fatalf("a restore: %+v %v", restored, err)
		}
		if _, err := edit(world.ada, models.PasswordItemKindLogin, &restored.Revision); err != nil {
			test.Fatalf("saving a restored item: %s", err)
		}
	})
}

// The owner lists deleted items too, with the revisions since, and every
// identifier of what is not deleted.
func TestListingPasswordItemsSinceARevision(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		first := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		second := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindCard)
		savePasswordItemFor(test, tx, world.ben, 3, models.PasswordItemKindNote)

		everything := mustListPasswordItems(test, tx, world.ada, 0)
		if len(everything.Items) != 2 || everything.Items[0].ID != first.ID || everything.Items[1].ID != second.ID ||
			!slices.Equal(everything.ItemIDs, []string{first.ID, second.ID}) {
			test.Fatalf("everything: %+v", everything)
		}
		if everything.Items[0].OwnerName != "ada" || everything.Items[0].IsSharedWithMe || len(everything.Items[0].Shares) != 0 {
			test.Fatalf("an owned item: %+v", everything.Items[0])
		}
		later := mustListPasswordItems(test, tx, world.ada, first.Revision)
		if len(later.Items) != 1 || later.Items[0].ID != second.ID || len(later.ItemIDs) != 2 || later.Revision != second.Revision {
			test.Fatalf("since the first: %+v", later)
		}
		if _, err := tx.DeletePasswordItem(world.ada.ID, second.ID); err != nil {
			test.Fatal(err)
		}
		afterDelete := mustListPasswordItems(test, tx, world.ada, second.Revision)
		if len(afterDelete.Items) != 1 || afterDelete.Items[0].DeletedAt == nil || !slices.Equal(afterDelete.ItemIDs, []string{first.ID}) {
			test.Fatalf("a deleted item comes once with its time, and not among the identifiers: %+v", afterDelete)
		}
		if none := mustListPasswordItems(test, tx, world.dave, 0); len(none.Items) != 0 || len(none.ItemIDs) != 0 || none.Revision != 0 {
			test.Fatalf("a person with nothing: %+v", none)
		}
	})
}

// A share reaches a person in a circle with the owner and gives nothing until
// a key sealed for the person's public key is stored. The recipient then reads
// the item with their own key and no list of shares. A person the owner does
// not share a circle with is not found, a key for somebody not reached or for
// an old public key is refused, and taking the grantee away takes the key.
func TestSharingAPasswordItemNeedsACircleAndAKey(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		setUpPasswordsFor(test, tx, world.cora)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)

		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: stranger.ID}}, nil); !errors.Is(err, db.ErrNotFound) {
				test.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeCircleID: world.club.ID}}, nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a share with a circle she is not in: %v", err)
		}
		for name, grantees := range map[string][]models.PasswordShareGrantee{
			"both":    {{GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID}},
			"neither": {{}},
			"herself": {{GranteeUserID: world.ada.ID}},
		} {
			if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, grantees, nil); !errors.Is(err, db.ErrInvalidArguments) {
				test.Fatalf("%s: %v", name, err)
			}
		}
		if _, err := tx.SetPasswordItemShares(world.ben.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ada.ID}}, nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("sharing somebody else's item: %v", err)
		}

		// A key for somebody the shares do not reach, and for a key that is
		// not theirs now.
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}},
			[]*models.PasswordShareKey{passwordKeyFor(item, world.cora)}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a key for a person not reached: %v", err)
		}
		oldKey := passwordKeyFor(item, world.ben)
		oldKey.RecipientPublicKey = "an older public key"
		if _, err := tx.SetPasswordItemShares(world.ada.ID, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}},
			[]*models.PasswordShareKey{oldKey}); !errors.Is(err, db.ErrPasswordKeyStale) {
			test.Fatalf("a key for an old public key: %v", err)
		}
		// None of the refusals above left a share or a key behind.
		if untouched, err := tx.GetPasswordItem(world.ada.ID, item.ID); err != nil || len(untouched.Shares) != 0 || untouched.Revision != item.Revision {
			test.Fatalf("a refused share left something behind: %+v %v", untouched, err)
		}

		// A share first, then the key: ben is waiting until he has it.
		shared := mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil)
		if shared.Revision <= item.Revision || len(shared.Shares) != 1 || len(shared.Shares[0].Recipients) != 1 {
			test.Fatalf("a share: %+v", shared)
		}
		recipient := shared.Shares[0].Recipients[0]
		if recipient.UserID != world.ben.ID || recipient.PublicKey != passwordsPublicKeyOf(world.ben) || recipient.HasKey || shared.Shares[0].GranteeName != "ben" {
			test.Fatalf("the recipient before a key: %+v", recipient)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 0 || len(got.ItemIDs) != 0 {
			test.Fatalf("a share without a key opens nothing: %+v", got)
		}
		waiting, err := tx.ListWaitingPasswordShares(world.ada.ID)
		if err != nil || len(waiting) != 1 || waiting[0].ItemID != item.ID || waiting[0].Recipient.UserID != world.ben.ID {
			test.Fatalf("waiting: %+v %v", waiting, err)
		}

		added, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if err != nil || added != 1 {
			test.Fatalf("adding a key: %d %v", added, err)
		}
		if waiting, _ := tx.ListWaitingPasswordShares(world.ada.ID); len(waiting) != 0 {
			test.Fatalf("nobody waits now: %+v", waiting)
		}
		reading := mustListPasswordItems(test, tx, world.ben, 0)
		if len(reading.Items) != 1 || reading.Items[0].ID != item.ID || !reading.Items[0].IsSharedWithMe ||
			reading.Items[0].ItemKeyByRecipientKey != "key-of-ben" || reading.Items[0].ItemKeyByAccountKey != "" ||
			len(reading.Items[0].Shares) != 0 || reading.Items[0].OwnerName != "ada" || reading.Items[0].Revision <= shared.Revision {
			test.Fatalf("what ben reads: %+v", reading)
		}
		if !slices.Equal(reading.ItemIDs, []string{item.ID}) {
			test.Fatalf("what ben can open: %v", reading.ItemIDs)
		}
		if owner := mustListPasswordItems(test, tx, world.ada, 0); !owner.Items[0].Shares[0].Recipients[0].HasKey {
			test.Fatalf("the owner sees that ben has a key: %+v", owner.Items[0].Shares[0])
		}

		// Adding keys for people nobody shared with, and for a stranger's
		// item, is refused.
		if _, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{passwordKeyFor(item, world.cora)}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a key for somebody not reached: %v", err)
		}
		if _, err := tx.AddPasswordShareKeys(world.ben.ID, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a key for somebody else's item: %v", err)
		}

		// Ben erases and sets up again: the key is for a key he no longer
		// has, he reads nothing and waits.
		if _, err := tx.ErasePasswords(world.ben.ID); err != nil {
			test.Fatal(err)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.ItemIDs) != 0 {
			test.Fatalf("after an erase: %+v", got)
		}
		setup, err := tx.CreatePasswordsSetup(&models.PasswordSetup{
			UserID: world.ben.ID, AccountKeyByPassphrase: "a", PassphraseSalt: "b", PassphraseIterationCount: 1, PassphraseProofHash: "c",
			AccountKeyByRecoveryKey: "d", RecoveryKeyProofHash: "e", PublicKey: "a new public key of ben", PrivateKeyByAccountKey: "g",
		})
		if err != nil || setup == nil {
			test.Fatal(err)
		}
		if waiting, _ := tx.ListWaitingPasswordShares(world.ada.ID); len(waiting) != 1 || waiting[0].Recipient.PublicKey != "a new public key of ben" {
			test.Fatalf("ben waits with his new key: %+v", waiting)
		}
		if _, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)}); !errors.Is(err, db.ErrPasswordKeyStale) {
			test.Fatalf("a key for his old public key: %v", err)
		}
		fresh := passwordKeyFor(item, world.ben)
		fresh.RecipientPublicKey = "a new public key of ben"
		if _, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{fresh}); err != nil {
			test.Fatalf("a key for his new public key: %s", err)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 1 {
			test.Fatalf("after he is given the new key: %+v", got)
		}

		// Taking the grantee away takes the key and the item with it.
		unshared := mustSetPasswordItemShares(test, tx, world.ada, item.ID, nil, nil)
		if len(unshared.Shares) != 0 {
			test.Fatalf("no shares: %+v", unshared.Shares)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.ItemIDs) != 0 {
			test.Fatalf("after the share ends: %+v", got)
		}
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share_key"`); got != "0" {
		test.Fatalf("the key of a recipient no longer reached is still stored: %s", got)
	}
}

// The people a share reaches are the person, if the owner shares a circle with
// them, or the other members of a circle the owner is in, and only those who
// may use Passwords, each with the public key they set up.
func TestListingThePeopleAPasswordShareReaches(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ben)

		recipients, err := tx.ListPasswordRecipients(world.ada.ID, world.ben.ID, "")
		if err != nil || len(recipients) != 1 || recipients[0].UserID != world.ben.ID || recipients[0].PublicKey != passwordsPublicKeyOf(world.ben) || recipients[0].HasKey {
			test.Fatalf("a person in a circle with the caller: %+v %v", recipients, err)
		}
		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.ListPasswordRecipients(world.ada.ID, stranger.ID, ""); !errors.Is(err, db.ErrNotFound) {
				test.Fatalf("%s shares no circle with ada: %v", stranger.Username, err)
			}
		}
		members, err := tx.ListPasswordRecipients(world.ben.ID, "", world.club.ID)
		if err != nil || len(members) != 1 || members[0].UserID != world.cora.ID || members[0].PublicKey != "" {
			test.Fatalf("the other members of a circle, one of whom has set up nothing: %+v %v", members, err)
		}
		if _, err := tx.ListPasswordRecipients(world.ada.ID, "", world.club.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a circle the caller is not in: %v", err)
		}
		for name, arguments := range map[string][2]string{
			"both":    {world.ben.ID, world.home.ID},
			"neither": {"", ""},
			"herself": {world.ada.ID, ""},
		} {
			if _, err := tx.ListPasswordRecipients(world.ada.ID, arguments[0], arguments[1]); !errors.Is(err, db.ErrInvalidArguments) {
				test.Fatalf("%s: %v", name, err)
			}
		}

		// Somebody an operator took Passwords from is not offered, and is
		// not found as a person.
		role, err := tx.CreateRole(&models.Role{Name: "Nothing", Permissions: []models.Permission{models.PermissionCircleUse}})
		if err != nil {
			test.Fatal(err)
		}
		outsider, err := tx.CreateUser(&models.User{Username: "eve"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "Eve", RoleIDs: []string{role.ID}, UserIDs: []string{outsider.ID}}); err != nil {
			test.Fatal(err)
		}
		if _, err := tx.AddCircleMember(world.home.ID, outsider.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := tx.ListPasswordRecipients(world.ada.ID, outsider.ID, ""); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a person without passwords:use: %v", err)
		}
		homeMembers, err := tx.ListPasswordRecipients(world.ada.ID, "", world.home.ID)
		if err != nil || len(homeMembers) != 1 || homeMembers[0].UserID != world.ben.ID {
			test.Fatalf("the members of home who may use Passwords: %+v %v", homeMembers, err)
		}
	})
}

// A share with a circle lists a member who set up Passwords after the share
// was made as waiting, and a member who leaves the circle is no longer
// reached, loses the key and is not waited for.
func TestACircleShareWaitsForMembersAndEndsWhenTheyLeave(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var world *shareWorld
	var item *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		item = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)

		// Ben has not set up Passwords: the share reaches him, with no key to
		// seal, and he is not waited for yet.
		shared := mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}}, nil)
		if len(shared.Shares) != 1 || shared.Shares[0].GranteeCircleID != world.home.ID || shared.Shares[0].GranteeName != "Home" ||
			len(shared.Shares[0].Recipients) != 1 || shared.Shares[0].Recipients[0].PublicKey != "" {
			test.Fatalf("a circle share: %+v", shared.Shares)
		}
		if waiting, _ := tx.ListWaitingPasswordShares(world.ada.ID); len(waiting) != 0 {
			test.Fatalf("a member who has not set up cannot be sealed for: %+v", waiting)
		}
		setUpPasswordsFor(test, tx, world.ben)
		waiting, err := tx.ListWaitingPasswordShares(world.ada.ID)
		if err != nil || len(waiting) != 1 || waiting[0].ItemID != item.ID || waiting[0].Recipient.UserID != world.ben.ID {
			test.Fatalf("a member who set up after the share waits: %+v %v", waiting, err)
		}
		if _, err := tx.AddPasswordShareKeys(world.ada.ID, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)}); err != nil {
			test.Fatal(err)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 1 {
			test.Fatalf("ben reads the item: %+v", got)
		}
		// A direct share to ben too: either alone reaches him, and the key
		// stays while one does.
		mustSetPasswordItemShares(test, tx, world.ada, item.ID,
			[]models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}, {GranteeUserID: world.ben.ID}}, nil)
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil)
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 1 {
			test.Fatalf("ben still reads the item through the direct share: %+v", got)
		}
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share_key"`); got != "1" {
		test.Fatalf("the key stays while a share reaches him: %s", got)
	}

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		before := mustListPasswordItems(test, tx, world.ada, 0)
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			test.Fatalf("RemoveCircleMember: %s", err)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 0 || len(got.ItemIDs) != 0 {
			test.Fatalf("a person who left reads nothing: %+v", got)
		}
		after := mustListPasswordItems(test, tx, world.ada, 0)
		if len(after.Items) != 1 || len(after.Items[0].Shares) != 0 || after.Items[0].Revision <= before.Items[0].Revision {
			test.Fatalf("the owner sees the share gone, with a new revision: %+v", after.Items)
		}
		if waiting, _ := tx.ListWaitingPasswordShares(world.ada.ID); len(waiting) != 0 {
			test.Fatalf("nobody waits: %+v", waiting)
		}
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share" `); got != "0" {
		test.Fatalf("the direct share ended with the circle: %s", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share_key"`); got != "0" {
		test.Fatalf("the key ended with the circle: %s", got)
	}
}

// A person who loses passwords:use is not reached, and gets the access back
// with the same key when they have it again.
func TestAPasswordShareNeedsPasswordsUseOnBothSides(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindNote)
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 1 {
			test.Fatalf("ben reads: %+v", got)
		}
		// Taking Passwords from either side ends the reach, and giving it
		// back brings the same key back to life.
		groupsOf := func(person *models.User) []string {
			var groupIds []string
			groups, err := tx.ListGroups()
			if err != nil {
				test.Fatal(err)
			}
			for _, group := range groups {
				if group.Name == "Group of "+person.ID {
					groupIds = append(groupIds, group.ID)
				}
			}
			return groupIds
		}
		for _, person := range []*models.User{world.ben, world.ada} {
			for _, groupId := range groupsOf(person) {
				if _, err := tx.UpdateGroup(groupId, func(group *models.Group) error { group.UserIDs = nil; return nil }); err != nil {
					test.Fatal(err)
				}
			}
			if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 0 || len(got.ItemIDs) != 0 {
				test.Fatalf("without passwords:use for %s ben reads: %+v", person.Username, got)
			}
			for _, groupId := range groupsOf(person) {
				if _, err := tx.UpdateGroup(groupId, func(group *models.Group) error { group.UserIDs = []string{person.ID}; return nil }); err != nil {
					test.Fatal(err)
				}
			}
			if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 1 {
				test.Fatalf("with it back for %s ben reads again: %+v", person.Username, got)
			}
		}
	})
}

// An item is the agent's to use only when the person turns it on with the key
// sealed for the server's agent key, never a card, and not after a delete.
func TestLettingTheAgentUseAPasswordItem(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		login := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		card := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindCard)

		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, card.ID, true, "sealed for the agent"); !errors.Is(err, db.ErrPasswordCardNotUsableByAgent) {
			test.Fatalf("a card: %v", err)
		}
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, login.ID, true, ""); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("on without a key: %v", err)
		}
		if _, err := tx.SetPasswordItemUsableByAgent(world.ben.ID, login.ID, true, "sealed for the agent"); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody else's item: %v", err)
		}
		usable, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, login.ID, true, "sealed for the agent")
		if err != nil || !usable.IsUsableByAgent || usable.Revision <= card.Revision {
			test.Fatalf("turned on: %+v %v", usable, err)
		}
		listed, err := tx.ListAgentUsablePasswordItems(world.ada.ID)
		if err != nil || len(listed) != 1 || listed[0].ID != login.ID || listed[0].ItemKeyByAgentKey != "sealed for the agent" {
			test.Fatalf("what the agent may use: %+v %v", listed, err)
		}
		usedAt := time.Now().Add(-time.Minute)
		if err := tx.RecordPasswordItemAgentUse(world.ada.ID, login.ID, "example.test", usedAt); err != nil {
			test.Fatal(err)
		}
		if err := tx.RecordPasswordItemAgentUse(world.ada.ID, card.ID, "example.test", usedAt); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("an item the agent may not use: %v", err)
		}
		// The use does not raise the revision, which a device saves against, so
		// the changes after the revision do not list the item; a full read does,
		// with the time of the use and the revision the device already has.
		if later := mustListPasswordItems(test, tx, world.ada, usable.Revision); len(later.Items) != 0 {
			test.Fatalf("an agent's use raised the item's revision: %+v", later.Items)
		}
		used := mustListPasswordItems(test, tx, world.ada, 0)
		var usedItem *models.PasswordItem
		for _, listedItem := range used.Items {
			if listedItem.ID == login.ID {
				usedItem = listedItem
			}
		}
		if usedItem == nil || usedItem.LastUsedByAgentAt == nil || usedItem.Revision != usable.Revision ||
			usedItem.LastUsedByAgentAt.Sub(usedAt).Abs() > time.Second {
			test.Fatalf("the use is on the item, at the same revision: %+v", usedItem)
		}
		baseRevision := usable.Revision
		if _, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: login.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
			EncryptedFields: `{"sealed":"edited fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
		}, &baseRevision); err != nil {
			test.Fatalf("a save from the revision the device has, after the agent used the item: %v", err)
		}

		off, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, login.ID, false, "")
		if err != nil || off.IsUsableByAgent || off.ItemKeyByAgentKey != "" {
			test.Fatalf("turned off: %+v %v", off, err)
		}
		if listed, _ := tx.ListAgentUsablePasswordItems(world.ada.ID); len(listed) != 0 {
			test.Fatalf("nothing for the agent: %+v", listed)
		}
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, login.ID, true, "sealed again"); err != nil {
			test.Fatal(err)
		}
		deleted, err := tx.DeletePasswordItem(world.ada.ID, login.ID)
		if err != nil || deleted.IsUsableByAgent || deleted.ItemKeyByAgentKey != "" {
			test.Fatalf("a delete ends the agent's use: %+v %v", deleted, err)
		}
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, login.ID, true, "sealed again"); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a deleted item: %v", err)
		}
	})
}

// The server's agent key is made once.
func TestThePasswordAgentKeyIsMadeOnce(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if key, err := tx.GetPasswordAgentKey(); err != nil || key != nil {
			test.Fatalf("before one is made: %+v %v", key, err)
		}
		first, err := tx.CreatePasswordAgentKey(&models.PasswordAgentKey{PublicKey: "first public", PrivateKeySealed: "first sealed"})
		if err != nil || first.PublicKey != "first public" {
			test.Fatalf("the first: %+v %v", first, err)
		}
		second, err := tx.CreatePasswordAgentKey(&models.PasswordAgentKey{PublicKey: "second public", PrivateKeySealed: "second sealed"})
		if err != nil || second.PublicKey != "first public" || second.PrivateKeySealed != "first sealed" {
			test.Fatalf("a second keeps the first: %+v %v", second, err)
		}
		if _, err := tx.CreatePasswordAgentKey(&models.PasswordAgentKey{}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("an empty key: %v", err)
		}
	})
}

// Erasing Passwords takes the setup, the items with their shares and keys, and
// the keys sealed for the person; their items shared with others go, and what
// others shared with them waits for a new key.
func TestErasingPasswords(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		adaItem := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		benItem := savePasswordItemFor(test, tx, world.ben, 2, models.PasswordItemKindLogin)
		mustSetPasswordItemShares(test, tx, world.ada, adaItem.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(adaItem, world.ben)})
		mustSetPasswordItemShares(test, tx, world.ben, benItem.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ada.ID}}, []*models.PasswordShareKey{passwordKeyFor(benItem, world.ada)})

		if erased, err := tx.ErasePasswords(world.dave.ID); err != nil || erased != 0 {
			test.Fatalf("erasing nothing: %d %v", erased, err)
		}
		erased, err := tx.ErasePasswords(world.ben.ID)
		if err != nil || erased != 1 {
			test.Fatalf("erasing: %d %v", erased, err)
		}
		if setup, _ := tx.GetPasswordsSetup(world.ben.ID); setup != nil {
			test.Fatalf("the setup is still there: %+v", setup)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 0 || len(got.ItemIDs) != 0 {
			test.Fatalf("ben has nothing: %+v", got)
		}
		if got := mustListPasswordItems(test, tx, world.ada, 0); len(got.Items) != 1 || len(got.ItemIDs) != 1 || got.Items[0].ID != adaItem.ID {
			test.Fatalf("ada keeps her own and loses his: %+v", got)
		}
		// Ada's item is shared with ben, who has no key now and no setup, so
		// nobody waits until he sets up again.
		if waiting, _ := tx.ListWaitingPasswordShares(world.ada.ID); len(waiting) != 0 {
			test.Fatalf("ben cannot be sealed for: %+v", waiting)
		}
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share_key"`); got != "0" {
		test.Fatalf("keys left after the erase: %s", got)
	}
}

// Items deleted before the time are swept, with their shares, and no others.
func TestSweepingDeletedPasswordItems(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var oldItem, recentItem, liveItem *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		oldItem = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		recentItem = savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindLogin)
		liveItem = savePasswordItemFor(test, tx, world.ada, 3, models.PasswordItemKindLogin)
		mustSetPasswordItemShares(test, tx, world.ada, oldItem.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, nil)
		for _, item := range []*models.PasswordItem{oldItem, recentItem} {
			if _, err := tx.DeletePasswordItem(world.ada.ID, item.ID); err != nil {
				test.Fatal(err)
			}
		}
	})
	dbtest.Exec(test, database, `UPDATE "password_item" SET "deleted_at" = now() - interval '31 days' WHERE "id" = '`+oldItem.ID+`'`)
	dbtest.Exec(test, database, `UPDATE "password_item" SET "deleted_at" = now() - interval '29 days' WHERE "id" = '`+recentItem.ID+`'`)

	swept, err := database.ScavengePasswordItems(time.Now().AddDate(0, 0, -models.PasswordItemDeletedDays))
	if err != nil || swept != 1 {
		test.Fatalf("sweeping: %d %v", swept, err)
	}
	if got := dbtest.QueryString(test, database, `SELECT string_agg(id, ',' ORDER BY id) FROM "password_item"`); got != recentItem.ID+","+liveItem.ID {
		test.Fatalf("what is left: %s", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_share"`); got != "0" {
		test.Fatalf("the swept item's shares: %s", got)
	}
}

// Every item a reader gets carries its owner's current public key: the owner's
// own, for the owner and for the agent's listing, and the owner's for a person
// the item is shared with, who needs it to open the key sealed for them.
func TestPasswordItemsCarryTheirOwnersPublicKey(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		mustSetPasswordItemShares(test, tx, world.ada, item.ID, []models.PasswordShareGrantee{{GranteeUserID: world.ben.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		if _, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, item.ID, true, "key-for-the-agent"); err != nil {
			test.Fatal(err)
		}

		owned := mustListPasswordItems(test, tx, world.ada, 0)
		if len(owned.Items) != 1 || owned.Items[0].OwnerPublicKey != passwordsPublicKeyOf(world.ada) {
			test.Fatalf("the owner's own item: %+v", owned.Items)
		}
		shared := mustListPasswordItems(test, tx, world.ben, 0)
		if len(shared.Items) != 1 || !shared.Items[0].IsSharedWithMe || shared.Items[0].OwnerPublicKey != passwordsPublicKeyOf(world.ada) {
			test.Fatalf("the item shared with Ben: %+v", shared.Items)
		}
		usable, err := tx.ListAgentUsablePasswordItems(world.ada.ID)
		if err != nil || len(usable) != 1 || usable[0].OwnerPublicKey != passwordsPublicKeyOf(world.ada) {
			test.Fatalf("the agent's items: %+v %v", usable, err)
		}
		got, err := tx.GetPasswordItem(world.ada.ID, item.ID)
		if err != nil || got.OwnerPublicKey != passwordsPublicKeyOf(world.ada) {
			test.Fatalf("an item read by its owner: %+v %v", got, err)
		}
	})
}

// passwordItemFieldsOf is a sealed value of a given number of bytes, for the
// tests of what a person may keep.
func passwordItemFieldsOf(label string, size int) string {
	return `{"sealed":"` + label + strings.Repeat("a", size) + `"}`
}

// saveChangedPasswordItem changes the item once, from the revision it holds.
func saveChangedPasswordItem(test *testing.T, tx db.Transaction, owner *models.User, current *models.PasswordItem, fields string) *models.PasswordItem {
	test.Helper()
	saved, err := tx.SavePasswordItem(&models.PasswordItem{
		ID: current.ID, OwnerUserID: owner.ID, ItemKind: current.ItemKind,
		EncryptedFields: fields, ItemKeyByAccountKey: `{"sealed":"key"}`,
	}, &current.Revision)
	if err != nil {
		test.Fatalf("SavePasswordItem: %s", err)
	}
	return saved
}

func lastPasswordRevision(test *testing.T, database db.Database) string {
	test.Helper()
	return dbtest.QueryString(test, database, `SELECT last_value FROM "password_item_revision"`)
}

// A save keeps the version it replaces only when the item has none kept in
// the last hour, so that a burst of saves (a token needs no tap to make them)
// keeps the content from before the burst and cannot push it out of the
// history.
func TestABurstOfSavesKeepsTheContentFromBeforeTheBurst(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var itemID string
	var ada *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		ada = world.ada
		current := savePasswordItemFor(test, tx, ada, 1, models.PasswordItemKindLogin)
		itemID = current.ID
		for index := 1; index <= models.PasswordItemVersionsMost+5; index++ {
			current = saveChangedPasswordItem(test, tx, ada, current, fmt.Sprintf(`{"sealed":"garbage %d"}`, index))
		}
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "item_id" = '`+itemID+`'`); got != "1" {
		test.Fatalf("%s versions are kept after a burst, want 1", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT encrypted_fields FROM "password_item_version" WHERE "item_id" = '`+itemID+`'`); got != `{"sealed":"fields"}` {
		test.Fatalf("the version kept is %s, not the content from before the burst", got)
	}
	// An hour later the next save keeps what the burst left, and the original
	// is still there.
	dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = "replaced_at" - interval '2 hours'`)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		current, err := tx.GetPasswordItem(ada.ID, itemID)
		if err != nil {
			test.Fatal(err)
		}
		saveChangedPasswordItem(test, tx, ada, current, `{"sealed":"after the hour"}`)
	})
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "item_id" = '`+itemID+`'`); got != "2" {
		test.Fatalf("%s versions are kept an hour later, want 2", got)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "item_id" = '`+itemID+`' AND encrypted_fields = '{"sealed":"fields"}'`); got != "1" {
		test.Fatal("the original did not survive the next save")
	}
}

// Saves spread over hours are each kept, up to PasswordItemVersionsMost; past
// that the version before the newest goes, so the oldest stays for its 30
// days, and the sweep and a later save forget what is older than that.
func TestSavesSpreadOverHoursKeepTheOldestVersion(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var itemID string
	var ada *models.User
	var current *models.PasswordItem
	saveAnHourLater := func(index int) {
		dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = "replaced_at" - interval '2 hours'`)
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			if current == nil {
				ada = newPasswordsWorld(test, tx).ada
				current = savePasswordItemFor(test, tx, ada, 1, models.PasswordItemKindLogin)
				itemID = current.ID
			}
			current = saveChangedPasswordItem(test, tx, ada, current, fmt.Sprintf(`{"sealed":"fields %d"}`, index))
		})
	}
	for index := 0; index <= models.PasswordItemVersionsMost+10; index++ {
		saveAnHourLater(index)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "item_id" = '`+itemID+`'`); got != fmt.Sprint(models.PasswordItemVersionsMost) {
		test.Fatalf("%s versions are kept, want %d", got, models.PasswordItemVersionsMost)
	}
	if got := dbtest.QueryString(test, database, `SELECT encrypted_fields FROM "password_item_version" WHERE "item_id" = '`+itemID+`' ORDER BY "revision" ASC LIMIT 1`); got != `{"sealed":"fields"}` {
		test.Fatalf("the oldest kept version is %s, not the original", got)
	}
	want := fmt.Sprintf(`{"sealed":"fields %d"}`, models.PasswordItemVersionsMost+9)
	if got := dbtest.QueryString(test, database, `SELECT encrypted_fields FROM "password_item_version" WHERE "item_id" = '`+itemID+`' ORDER BY "revision" DESC LIMIT 1`); got != want {
		test.Fatalf("the newest kept version is %s, want %s", got, want)
	}

	// A save forgets what was replaced more than 30 days ago, and keeps what
	// it replaces.
	dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = now() - interval '31 days' WHERE "revision" IN (SELECT "revision" FROM "password_item_version" ORDER BY "revision" ASC LIMIT 5)`)
	saveAnHourLater(1000)
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "item_id" = '`+itemID+`'`); got != fmt.Sprint(models.PasswordItemVersionsMost-5+1) {
		test.Fatalf("%s versions are left after a save past 30 days, want %d", got, models.PasswordItemVersionsMost-5+1)
	}

	// The sweep removes versions replaced more than 30 days ago.
	dbtest.Exec(test, database, `UPDATE "password_item_version" SET "replaced_at" = now() - interval '31 days' WHERE "revision" IN (SELECT "revision" FROM "password_item_version" ORDER BY "revision" ASC LIMIT 5)`)
	if _, err := database.ScavengePasswordItems(time.Now().AddDate(0, 0, -models.PasswordItemDeletedDays)); err != nil {
		test.Fatal(err)
	}
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version"`); got != fmt.Sprint(models.PasswordItemVersionsMost-5+1-5) {
		test.Fatalf("%s versions are left after the sweep, want %d", got, models.PasswordItemVersionsMost-5+1-5)
	}
	// Versions go with their item.
	dbtest.Exec(test, database, `DELETE FROM "password_item" WHERE "id" = '`+itemID+`'`)
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version"`); got != "0" {
		test.Fatalf("%s versions outlived their item", got)
	}
}

// A person's items that are not deleted hold at most PasswordsBytesMost bytes
// of sealed text. Saving, re-keying and restoring are refused past it with
// ErrPasswordsFull and draw no revision number; anything that does not make an
// item larger is allowed, and deleting makes room.
func TestPasswordsHoldAtMostABoundedNumberOfBytes(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	const thirtyMebibytes = 30 << 20
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		setUpPasswordsFor(test, tx, world.ada)
		setUpPasswordsFor(test, tx, world.ben)
		save := func(owner *models.User, number int, fields string) (*models.PasswordItem, error) {
			return tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(number), OwnerUserID: owner.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: fields, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
		}
		first, err := save(world.ada, 1, passwordItemFieldsOf("first", thirtyMebibytes))
		if err != nil {
			test.Fatal(err)
		}
		second, err := save(world.ada, 2, passwordItemFieldsOf("second", thirtyMebibytes))
		if err != nil {
			test.Fatal(err)
		}
		// Another person's items do not count against Ada.
		if _, err := save(world.ben, 3, passwordItemFieldsOf("ben", thirtyMebibytes)); err != nil {
			test.Fatalf("ben's first note: %v", err)
		}
		revisionBefore := lastPasswordRevision(test, database)
		if _, err := save(world.ada, 4, passwordItemFieldsOf("third", 10<<20)); !errors.Is(err, db.ErrPasswordsFull) {
			test.Fatalf("a save past the limit: %v", err)
		}
		bigger := &models.PasswordItem{
			ID: second.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: passwordItemFieldsOf("second", thirtyMebibytes+(10<<20)), ItemKeyByAccountKey: `{"sealed":"key"}`,
		}
		if _, err := tx.SavePasswordItem(bigger, &second.Revision); !errors.Is(err, db.ErrPasswordsFull) {
			test.Fatalf("a change that makes an item larger past the limit: %v", err)
		}
		if _, err := tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
			ItemID: second.ID, BaseRevision: second.Revision, EncryptedFields: bigger.EncryptedFields, ItemKeyByAccountKey: `{"sealed":"key 2"}`,
		}); !errors.Is(err, db.ErrPasswordsFull) {
			test.Fatalf("a re-key that makes an item larger past the limit: %v", err)
		}
		if got := lastPasswordRevision(test, database); got != revisionBefore {
			test.Fatalf("refused changes drew revisions: %s then %s", revisionBefore, got)
		}

		// A smaller item still fits. Making an item shorter keeps the version it
		// replaces, which does not fit at the limit: a token's save is refused
		// (the content it would overwrite could not be kept), and the person's
		// own tap goes ahead without keeping it, which is how they make room.
		if _, err := save(world.ada, 5, passwordItemFieldsOf("small", 1<<20)); err != nil {
			test.Fatalf("a small note below the limit: %v", err)
		}
		shorter := &models.PasswordItem{
			ID: first.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
			EncryptedFields: passwordItemFieldsOf("first", 10<<20), ItemKeyByAccountKey: `{"sealed":"key"}`,
		}
		if _, err := tx.SavePasswordItem(shorter, &first.Revision); !errors.Is(err, db.ErrPasswordsFull) {
			test.Fatalf("a shorter note from a token at the limit: %v", err)
		}
		shorter.IsSavedByPerson = true
		if _, err := tx.SavePasswordItem(shorter, &first.Revision); err != nil {
			test.Fatalf("a shorter note from the person's tap: %v", err)
		}

		// A deleted item still counts until it is swept, so deleting makes no
		// room, and restoring it needs none.
		if _, err := tx.DeletePasswordItem(world.ada.ID, second.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := save(world.ada, 6, passwordItemFieldsOf("replacement", thirtyMebibytes)); !errors.Is(err, db.ErrPasswordsFull) {
			test.Fatalf("a save after a delete: %v", err)
		}
		restored, err := tx.RestorePasswordItem(world.ada.ID, second.ID)
		if err != nil || restored.DeletedAt != nil {
			test.Fatalf("a restore: %+v %v", restored, err)
		}
	})
}

// A refused save draws no revision number, which every person's items share:
// not for somebody else's item, a stale revision, a change of kind or a
// deleted item.
func TestRefusedPasswordChangesDrawNoRevision(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		deleted := savePasswordItemFor(test, tx, world.ada, 2, models.PasswordItemKindLogin)
		if _, err := tx.DeletePasswordItem(world.ada.ID, deleted.ID); err != nil {
			test.Fatal(err)
		}
		deleted, _ = tx.GetPasswordItem(world.ada.ID, deleted.ID)
		before := lastPasswordRevision(test, database)
		stale := item.Revision - 1
		for name, attempt := range map[string]func() error{
			"somebody else's item": func() error {
				_, err := tx.SavePasswordItem(&models.PasswordItem{ID: item.ID, OwnerUserID: world.ben.ID, ItemKind: item.ItemKind,
					EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`}, nil)
				return err
			},
			"a stale revision": func() error {
				_, err := tx.SavePasswordItem(&models.PasswordItem{ID: item.ID, OwnerUserID: world.ada.ID, ItemKind: item.ItemKind,
					EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`}, &stale)
				return err
			},
			"a new item that claims a revision": func() error {
				_, err := tx.SavePasswordItem(&models.PasswordItem{ID: passwordItemIdOf(9), OwnerUserID: world.ada.ID, ItemKind: item.ItemKind,
					EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`}, &stale)
				return err
			},
			"a change of kind": func() error {
				_, err := tx.SavePasswordItem(&models.PasswordItem{ID: item.ID, OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
					EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`}, &item.Revision)
				return err
			},
			"a deleted item": func() error {
				_, err := tx.SavePasswordItem(&models.PasswordItem{ID: deleted.ID, OwnerUserID: world.ada.ID, ItemKind: deleted.ItemKind,
					EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`}, &deleted.Revision)
				return err
			},
			"a re-key of somebody else's item": func() error {
				_, err := tx.RekeyPasswordItem(world.ben.ID, &models.PasswordItemRekey{ItemID: item.ID, BaseRevision: item.Revision, EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`})
				return err
			},
		} {
			if err := attempt(); err == nil {
				test.Fatalf("%s was not refused", name)
			}
			if got := lastPasswordRevision(test, database); got != before {
				test.Fatalf("%s drew a revision: %s then %s", name, before, got)
			}
		}
	})
}

// A re-key replaces the fields and the item key in one change, deletes every
// key stored for a recipient and stores the given ones for the recipients the
// item still reaches; a recipient reached and given none waits.
func TestRekeyingAPasswordItem(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var itemID string
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			setUpPasswordsFor(test, tx, person)
		}
		item := savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
		itemID = item.ID
		// Ada shares with the circle she and ben are in, and with ben alone: ben
		// is reached twice, and has a key.
		mustSetPasswordItemShares(test, tx, world.ada, item.ID,
			[]models.PasswordShareGrantee{{GranteeCircleID: world.home.ID}}, []*models.PasswordShareKey{passwordKeyFor(item, world.ben)})
		agent, err := tx.SetPasswordItemUsableByAgent(world.ada.ID, item.ID, true, "agent copy under the old key")
		if err != nil {
			test.Fatal(err)
		}
		rekey := func(base int64, keys []*models.PasswordShareKey, agentKey *string) (*models.PasswordItem, error) {
			return tx.RekeyPasswordItem(world.ada.ID, &models.PasswordItemRekey{
				ItemID: item.ID, BaseRevision: base, EncryptedFields: `{"sealed":"fields under the new key"}`,
				ItemKeyByAccountKey: `{"sealed":"new key by account key"}`, ShareKeys: keys, ItemKeyByAgentKey: agentKey,
			})
		}

		// Refusals leave everything as it was, and draw no revision.
		revisionBefore := lastPasswordRevision(test, database)
		if _, err := rekey(agent.Revision-1, nil, nil); !errors.Is(err, db.ErrPasswordRevisionStale) {
			test.Fatalf("a stale base revision: %v", err)
		}
		if _, err := rekey(agent.Revision, []*models.PasswordShareKey{passwordKeyFor(item, world.cora)}, nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("a key for a person the item does not reach: %v", err)
		}
		oldKey := passwordKeyFor(item, world.ben)
		oldKey.RecipientPublicKey = "an older public key"
		if _, err := rekey(agent.Revision, []*models.PasswordShareKey{oldKey}, nil); !errors.Is(err, db.ErrPasswordKeyStale) {
			test.Fatalf("a key for an old public key: %v", err)
		}
		other := passwordKeyFor(item, world.ben)
		other.ItemID = passwordItemIdOf(77)
		if _, err := rekey(agent.Revision, []*models.PasswordShareKey{other}, nil); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a key that names another item: %v", err)
		}
		if _, err := tx.RekeyPasswordItem(world.ben.ID, &models.PasswordItemRekey{
			ItemID: item.ID, BaseRevision: agent.Revision, EncryptedFields: `{"sealed":"f"}`, ItemKeyByAccountKey: `{"sealed":"k"}`,
		}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("somebody who is not the owner: %v", err)
		}
		if got := lastPasswordRevision(test, database); got != revisionBefore {
			test.Fatalf("refused re-keys drew revisions: %s then %s", revisionBefore, got)
		}
		untouched, err := tx.GetPasswordItem(world.ada.ID, item.ID)
		if err != nil || untouched.Revision != agent.Revision || untouched.EncryptedFields != item.EncryptedFields ||
			!untouched.Shares[0].Recipients[0].HasKey || untouched.ItemKeyByAgentKey != "agent copy under the old key" {
			test.Fatalf("a refused re-key changed the item: %+v %v", untouched, err)
		}

		// A re-key that keeps the agent: a new copy replaces the old.
		agentKey := "agent copy under the new key"
		kept, err := rekey(agent.Revision, nil, &agentKey)
		if err != nil {
			test.Fatal(err)
		}
		if kept.Revision <= agent.Revision || kept.EncryptedFields != `{"sealed":"fields under the new key"}` || kept.ItemKeyByAccountKey != `{"sealed":"new key by account key"}` ||
			!kept.IsUsableByAgent || kept.ItemKeyByAgentKey != agentKey {
			test.Fatalf("the re-keyed item: %+v", kept)
		}
		// Ben's old key is gone: nobody has a key, so ben waits.
		if len(kept.Shares) != 1 || len(kept.Shares[0].Recipients) != 1 || kept.Shares[0].Recipients[0].HasKey {
			test.Fatalf("the old key of a recipient survived: %+v", kept.Shares)
		}
		if waiting, err := tx.ListWaitingPasswordShares(world.ada.ID); err != nil || len(waiting) != 1 || waiting[0].Recipient.UserID != world.ben.ID {
			test.Fatalf("the recipient without a key waits: %+v %v", waiting, err)
		}
		if got := mustListPasswordItems(test, tx, world.ben, 0); len(got.Items) != 0 {
			test.Fatalf("a recipient without a key reads: %+v", got.Items)
		}

		// A re-key with a key for ben stores it, and an agent of nil ends the
		// agent's use.
		given := passwordKeyFor(item, world.ben)
		given.ItemKeyByRecipientKey = "new key for ben"
		given.ItemID = item.ID
		again, err := rekey(kept.Revision, []*models.PasswordShareKey{given}, nil)
		if err != nil {
			test.Fatal(err)
		}
		if again.Revision <= kept.Revision || again.IsUsableByAgent || again.ItemKeyByAgentKey != "" || !again.Shares[0].Recipients[0].HasKey {
			test.Fatalf("the second re-key: %+v", again)
		}
		if usable, _ := tx.ListAgentUsablePasswordItems(world.ada.ID); len(usable) != 0 {
			test.Fatalf("the agent still uses the item: %+v", usable)
		}
		read := mustListPasswordItems(test, tx, world.ben, 0)
		if len(read.Items) != 1 || read.Items[0].ItemKeyByRecipientKey != "new key for ben" {
			test.Fatalf("ben reads with the new key: %+v", read.Items)
		}

		// An item the agent does not use cannot be given an agent copy.
		agentKey = "an agent copy"
		if _, err := rekey(again.Revision, nil, &agentKey); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("an agent copy for an item the agent does not use: %v", err)
		}
		// A deleted item can be re-keyed, so that a restore can follow.
		if _, err := tx.DeletePasswordItem(world.ada.ID, item.ID); err != nil {
			test.Fatal(err)
		}
		deleted, _ := tx.GetPasswordItem(world.ada.ID, item.ID)
		if rekeyedDeleted, err := rekey(deleted.Revision, nil, nil); err != nil || rekeyedDeleted.DeletedAt == nil || rekeyedDeleted.NeedsNewKey {
			test.Fatalf("a deleted item: %+v %v", rekeyedDeleted, err)
		}
	})
	// Each re-key kept what it replaced, as a save does, once in the hour.
	if got := dbtest.QueryString(test, database, `SELECT count(*) FROM "password_item_version" WHERE "item_id" = '`+itemID+`'`); got != "1" {
		test.Fatalf("%s versions are kept after two re-keys in one hour, want 1", got)
	}
}

// The listing answers a revision that no change can have passed. A save that
// has not committed yet makes the listing wait for it, so the revision it
// answers includes the save; and a listing in progress makes a save wait, so a
// change cannot commit between the statements of one listing. In both the
// device that continues from the answer misses nothing.
func TestListingPasswordItemsNeverPassesAChangeInFlight(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var world *shareWorld
	var first *models.PasswordItem
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world = newPasswordsWorld(test, tx)
		first = savePasswordItemFor(test, tx, world.ada, 1, models.PasswordItemKindLogin)
	})

	// A save in flight, then a listing that must wait for it.
	savedInFlight := make(chan *models.PasswordItem, 1)
	commitSave := make(chan struct{})
	saveDone := make(chan error, 1)
	go func() {
		saveDone <- database.Transaction(func(tx db.Transaction) error {
			item, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(2), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindLogin,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			savedInFlight <- item
			<-commitSave
			return err
		})
	}()
	second := <-savedInFlight
	listed := make(chan *models.PasswordItemList, 1)
	listDone := make(chan error, 1)
	go func() {
		listDone <- database.Transaction(func(tx db.Transaction) error {
			list, err := tx.ListPasswordItems(world.ada.ID, first.Revision)
			listed <- list
			return err
		})
	}()
	select {
	case <-listed:
		test.Fatal("the listing answered while a save was not committed")
	case <-time.After(500 * time.Millisecond):
	}
	close(commitSave)
	if err := <-saveDone; err != nil {
		test.Fatal(err)
	}
	list := <-listed
	if err := <-listDone; err != nil {
		test.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != second.ID || list.Revision != second.Revision {
		test.Fatalf("the listing after the save: %+v (the save has revision %d)", list, second.Revision)
	}

	// A listing in progress, then a save that must wait for it.
	listStarted := make(chan *models.PasswordItemList, 1)
	finishList := make(chan struct{})
	listDone = make(chan error, 1)
	go func() {
		listDone <- database.Transaction(func(tx db.Transaction) error {
			list, err := tx.ListPasswordItems(world.ada.ID, 0)
			listStarted <- list
			<-finishList
			again, againErr := tx.ListPasswordItems(world.ada.ID, 0)
			if againErr == nil && again.Revision != list.Revision {
				test.Errorf("two listings of one transaction answered %d then %d", list.Revision, again.Revision)
			}
			return err
		})
	}()
	before := <-listStarted
	saveDone = make(chan error, 1)
	go func() {
		saveDone <- database.Transaction(func(tx db.Transaction) error {
			_, err := tx.SavePasswordItem(&models.PasswordItem{
				ID: passwordItemIdOf(3), OwnerUserID: world.ada.ID, ItemKind: models.PasswordItemKindNote,
				EncryptedFields: `{"sealed":"fields"}`, ItemKeyByAccountKey: `{"sealed":"key"}`,
			}, nil)
			return err
		})
	}()
	select {
	case <-saveDone:
		test.Fatal("a save committed while a listing was in progress")
	case <-time.After(500 * time.Millisecond):
	}
	close(finishList)
	if err := <-listDone; err != nil {
		test.Fatal(err)
	}
	if err := <-saveDone; err != nil {
		test.Fatal(err)
	}
	if before.Revision != second.Revision {
		test.Fatalf("the first listing answered %d, want %d", before.Revision, second.Revision)
	}
}

// An owner who no longer holds passwords:use, or whose account was disabled,
// has no item the agent can use, whatever is switched on; the item comes back
// with the permission.
func TestTheAgentUsesNothingOfAnOwnerWhoMayNotUsePasswords(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		world := newPasswordsWorld(test, tx)
		eve, err := tx.CreateUser(&models.User{Username: "eve", Name: "Eve Example"})
		if err != nil {
			test.Fatal(err)
		}
		setUpPasswordsFor(test, tx, eve)
		login := savePasswordItemFor(test, tx, eve, 21, models.PasswordItemKindLogin)
		if _, err := tx.SetPasswordItemUsableByAgent(eve.ID, login.ID, true, "sealed for the agent"); err != nil {
			test.Fatal(err)
		}
		// Eve holds no role that gives her Passwords.
		listed, err := tx.ListAgentUsablePasswordItems(eve.ID)
		if err != nil || len(listed) != 0 {
			test.Fatalf("without the permission: %+v %v", listed, err)
		}
		dbtest.GrantPermission(test, tx, eve.ID, models.PermissionPasswordsUse)
		listed, err = tx.ListAgentUsablePasswordItems(eve.ID)
		if err != nil || len(listed) != 1 || listed[0].ID != login.ID {
			test.Fatalf("with the permission: %+v %v", listed, err)
		}
		// A disabled account has none either.
		now := time.Now()
		if _, err := tx.UpdateUser(eve.ID, func(user *models.User) error { user.DisabledAt = &now; return nil }); err != nil {
			test.Fatal(err)
		}
		listed, err = tx.ListAgentUsablePasswordItems(eve.ID)
		if err != nil || len(listed) != 0 {
			test.Fatalf("a disabled account: %+v %v", listed, err)
		}
		// Others are not touched by it.
		if listed, err := tx.ListAgentUsablePasswordItems(world.ada.ID); err != nil || listed == nil {
			test.Fatalf("ada: %+v %v", listed, err)
		}
	})
}
