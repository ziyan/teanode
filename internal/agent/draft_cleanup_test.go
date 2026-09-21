package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

func TestAgentDraftCleanupRollbackKeepsContent(test *testing.T) {
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
	var draft *models.MailboxItem
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Draft fixture"})
		if err != nil {
			test.Fatal(err)
		}
		folder, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		draft, err = transaction.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
	})
	if err := files.Put(context.Background(), draft.MailID, []string{"Subject: Held reply"}, []byte("Held content")); err != nil {
		test.Fatal(err)
	}
	worker := &Agent{settings: &Settings{Storage: files}}
	injected := errors.New("reply update failed")
	err = database.TransactionContext(context.Background(), func(transaction db.Transaction) error {
		if err := worker.discardDraft(context.Background(), transaction, draft.ID); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		item, err := transaction.GetItem(draft.ID)
		if err != nil || item == nil {
			test.Fatalf("rollback lost draft: %+v, %v", item, err)
		}
		mail, err := transaction.GetMail(draft.MailID, nil)
		if err != nil || mail == nil || mail.UnreferencedAt != nil {
			test.Fatalf("rollback changed mail: %+v, %v", mail, err)
		}
	})
	_, body, err := files.Get(context.Background(), draft.MailID)
	if err != nil || string(body) != "Held content" {
		test.Fatalf("rollback lost bytes: %q, %v", body, err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := worker.discardDraft(context.Background(), transaction, draft.ID); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		item, err := transaction.GetItem(draft.ID)
		if err != nil || item != nil {
			test.Fatalf("committed removal retained draft: %+v, %v", item, err)
		}
		mail, err := transaction.GetMail(draft.MailID, nil)
		if err != nil || mail == nil || mail.UnreferencedAt == nil {
			test.Fatalf("committed removal did not start retention: %+v, %v", mail, err)
		}
	})
}
