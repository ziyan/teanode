package mailbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func folderFixture(test *testing.T) (db.Database, *access.Principal, *models.Mailbox) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "folder-owner")
	principal := &access.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}})}
	var ownedMailbox *models.Mailbox
	if err := database.Transaction(func(transaction db.Transaction) error {
		var err error
		ownedMailbox, err = transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Mailbox"})
		return err
	}); err != nil {
		test.Fatal(err)
	}
	return database, principal, ownedMailbox
}

func TestFolderCommandsEnforceOwnershipAndBuiltInFolders(test *testing.T) {
	database, principal, ownedMailbox := folderFixture(test)
	commands := mailbox.New(database)
	ctx := context.Background()
	created, err := commands.CreateFolder(ctx, principal, mailbox.CreateFolderRequest{MailboxID: ownedMailbox.ID, Name: "  Projects  "})
	if err != nil || created.Name != "Projects" {
		test.Fatalf("create folder: %v, %v", created, err)
	}
	for _, refused := range []*access.Principal{
		nil,
		{User: principal.User, Permissions: models.NewEffectivePermissions(nil)},
		{User: &models.User{ID: "another-owner"}, Permissions: principal.Permissions},
		{Console: true, Permissions: principal.Permissions},
	} {
		if _, err := commands.CreateFolder(ctx, refused, mailbox.CreateFolderRequest{MailboxID: ownedMailbox.ID, Name: "Refused"}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("create authorization = %v", err)
		}
		if _, err := commands.UpdateFolder(ctx, refused, mailbox.UpdateFolderRequest{FolderID: created.ID, Name: new("Renamed")}); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("update authorization = %v", err)
		}
		if _, err := commands.SetFolderPinned(ctx, refused, created.ID, true); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("pin authorization = %v", err)
		}
		if err := commands.DeleteFolder(ctx, refused, created.ID); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("delete authorization = %v", err)
		}
	}
	pinned, err := commands.SetFolderPinned(ctx, principal, created.ID, true)
	if err != nil || pinned.PinnedAt == nil {
		test.Fatalf("pin failed: %v", err)
	}
	repeated, err := commands.SetFolderPinned(ctx, principal, created.ID, true)
	if err != nil || !repeated.PinnedAt.Equal(*pinned.PinnedAt) {
		test.Fatalf("repeated pin changed its time: %v", err)
	}
	var inbox *models.MailboxFolder
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		inbox, err = transaction.GetFolderByKind(ownedMailbox.ID, models.MailboxFolderKindInbox)
		if err != nil {
			test.Fatal(err)
		}
	})
	if _, err := commands.UpdateFolder(ctx, principal, mailbox.UpdateFolderRequest{FolderID: inbox.ID, Name: new("Renamed")}); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("renamed Inbox: %v", err)
	}
	if _, err := commands.SetFolderPinned(ctx, principal, inbox.ID, false); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("unpinned Inbox: %v", err)
	}
	if err := commands.DeleteFolder(ctx, principal, inbox.ID); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("deleted Inbox: %v", err)
	}
}

func TestFolderCommandReusesLocksHeldByItsCaller(test *testing.T) {
	database, principal, ownedMailbox := folderFixture(test)
	created, err := mailbox.New(database).CreateFolder(context.Background(), principal, mailbox.CreateFolderRequest{MailboxID: ownedMailbox.ID, Name: "Locked folder"})
	if err != nil {
		test.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if _, err := transaction.UpdateFolder(created.ID, func(folder *models.MailboxFolder) error {
			folder.Name = "Held by caller"
			return nil
		}); err != nil {
			return err
		}
		_, err := mailbox.New(transaction).SetFolderPinned(ctx, principal, created.ID, true)
		return err
	}); err != nil {
		test.Fatalf("command waited on its caller's folder lock: %v", err)
	}
}

var errDeletionFailure = errors.New("failure after folder deletion")

type failingDeletionScope struct{ mailbox.TransactionScope }

func (self failingDeletionScope) TransactionContext(ctx context.Context, function func(db.Transaction) error) error {
	return self.TransactionScope.TransactionContext(ctx, func(transaction db.Transaction) error {
		return function(failingDeletionTransaction{Transaction: transaction})
	})
}

type failingDeletionTransaction struct{ db.Transaction }

func (self failingDeletionTransaction) DeleteFolder(folderId string) error {
	if err := self.Transaction.DeleteFolder(folderId); err != nil {
		return err
	}
	return errDeletionFailure
}

func TestFailedFolderDeletionRestoresDescendantsAndItems(test *testing.T) {
	for _, isNested := range []bool{false, true} {
		mode := "standalone"
		if isNested {
			mode = "existing transaction"
		}
		test.Run(mode, func(test *testing.T) {
			database, principal, ownedMailbox := folderFixture(test)
			var parent, child *models.MailboxFolder
			var item *models.MailboxItem
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				parent, err = transaction.CreateFolder(&models.MailboxFolder{MailboxID: ownedMailbox.ID, Name: "Parent"})
				if err != nil {
					test.Fatal(err)
				}
				child, err = transaction.CreateFolder(&models.MailboxFolder{MailboxID: ownedMailbox.ID, Name: "Child", ParentID: parent.ID})
				if err != nil {
					test.Fatal(err)
				}
				mail, err := transaction.CreateMail(&models.Mail{Subject: "Synthetic message", Kind: models.MailKindIncoming}, nil)
				if err != nil {
					test.Fatal(err)
				}
				item, err = transaction.AddItem(child.ID, mail.ID, "", models.MailboxItemFlags{})
				if err != nil {
					test.Fatal(err)
				}
			})
			run := func(scope mailbox.TransactionScope) {
				err := mailbox.New(failingDeletionScope{TransactionScope: scope}).DeleteFolder(context.Background(), principal, parent.ID)
				if !errors.Is(err, errDeletionFailure) {
					test.Fatalf("delete error = %v", err)
				}
				if _, err := mailbox.New(scope).CreateFolder(context.Background(), principal, mailbox.CreateFolderRequest{MailboxID: ownedMailbox.ID, Name: "After failure"}); err != nil {
					test.Fatalf("subsequent command failed: %v", err)
				}
			}
			if isNested {
				dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) { run(transaction) })
			} else {
				run(database)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				for _, folderId := range []string{parent.ID, child.ID} {
					folder, err := transaction.GetFolder(folderId)
					if err != nil || folder == nil {
						test.Fatalf("folder deletion survived rollback: %v", err)
					}
				}
				stored, err := transaction.GetItem(item.ID)
				if err != nil || stored == nil {
					test.Fatalf("item deletion survived rollback: %v", err)
				}
			})
		})
	}
}
