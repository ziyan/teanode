package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

func TestDraftCleanupRollbackPreservesMessageBytes(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		if err := files.Close(); err != nil {
			test.Error(err)
		}
	})
	userId := dbtest.CreateUser(test, database, "draft-owner")
	var mailbox *models.Mailbox
	var draft *models.MailboxItem
	var stored *models.Mail
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mailbox, err = transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Draft fixture"})
		if err != nil {
			test.Fatal(err)
		}
		folder, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		stored, err = transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		draft, err = transaction.AddItem(folder.ID, stored.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
	})
	if err := files.Put(context.Background(), stored.ID, []string{"Subject: Draft fixture"}, []byte("Unsaved work")); err != nil {
		test.Fatal(err)
	}
	resolver := &graph{database: database, storage: files}
	injected := errors.New("later command failed")
	err = database.TransactionContext(context.Background(), func(transaction db.Transaction) error {
		ctx := api.ContextWithTransaction(context.Background(), transaction)
		if err := resolver.removeDraft(ctx, transaction, mailbox, draft.ID); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		test.Fatalf("rollback returned %v", err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		item, err := transaction.GetItem(draft.ID)
		if err != nil || item == nil {
			test.Fatalf("rollback lost draft: %+v, %v", item, err)
		}
		mail, err := transaction.GetMail(stored.ID, nil)
		if err != nil || mail == nil || mail.UnreferencedAt != nil {
			test.Fatalf("rollback changed retained mail: %+v, %v", mail, err)
		}
	})
	assertReadable := func() {
		test.Helper()
		_, body, err := files.Get(context.Background(), stored.ID)
		if err != nil || string(body) != "Unsaved work" {
			test.Fatalf("draft bytes lost: %q, %v", body, err)
		}
	}
	assertReadable()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(context.Background(), transaction)
		if err := resolver.removeDraft(ctx, transaction, mailbox, draft.ID); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		item, err := transaction.GetItem(draft.ID)
		if err != nil || item != nil {
			test.Fatalf("committed cleanup retained draft: %+v, %v", item, err)
		}
		mail, err := transaction.GetMail(stored.ID, nil)
		if err != nil || mail == nil || mail.UnreferencedAt == nil {
			test.Fatalf("committed cleanup did not start retention: %+v, %v", mail, err)
		}
		ctx := api.ContextWithTransaction(context.Background(), transaction)
		if err := resolver.removeDraft(ctx, transaction, mailbox, draft.ID); err != nil {
			test.Fatalf("repeated cleanup: %v", err)
		}
	})
	assertReadable()
}
