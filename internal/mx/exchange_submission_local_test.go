package mx

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func TestSubmissionToLocalMailboxKeepsInboxAndSentTogether(test *testing.T) {
	for _, isSelfRecipient := range []bool{true, false} {
		database, closeDatabase := dbtest.AcquireDatabase(test)
		test.Cleanup(closeDatabase)
		localStorage, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
		if err != nil {
			test.Fatal(err)
		}
		test.Cleanup(func() { _ = localStorage.Close() })
		spool := &submissionStorage{Storage: localStorage}
		exchange, mailbox := submissionExchange(test, database, spool)
		recipient := "sender@example.com"
		recipientMailbox := mailbox
		if !isSelfRecipient {
			recipient = "recipient@example.com"
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				recipientMailbox, err = transaction.CreateMailbox(&models.Mailbox{UserID: mailbox.UserID, Name: "Recipient fixture"})
				if err != nil {
					test.Fatal(err)
				}
				domain, err := transaction.GetDomainByName("example.com")
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "^recipient$", Kind: models.AliasKindMailbox, MailboxID: recipientMailbox.ID}); err != nil {
					test.Fatal(err)
				}
			})
		}
		for _, shouldFail := range []bool{true, false} {
			spool.putError = nil
			if shouldFail {
				spool.putError = errors.New("storage unavailable")
			}
			err := database.Transaction(func(transaction db.Transaction) error {
				_, err := exchange.AcceptSubmission(context.Background(), transaction, &mailparse.Envelope{
					ID: "local-fixture", MailboxID: mailbox.ID, Sender: "sender@example.com",
					Recipients: []string{recipient}, IP: net.IPv4(127, 0, 0, 1), ReceivedAt: time.Now(),
					Headers: []string{"From: sender@example.com\r\n", "Message-ID: <local-submission@example.com>\r\n", "Content-Type: text/plain\r\n"},
					Body:    []byte("Local fixture\r\n"), Size: 128,
				})
				if shouldFail {
					if !errors.Is(err, spool.putError) {
						test.Fatalf("local acceptance did not reach storage: %v", err)
					}
					return nil
				}
				return err
			})
			if err != nil {
				test.Fatal(err)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				for _, target := range []struct {
					mailboxId string
					kind      models.MailboxFolderKind
				}{{mailbox.ID, models.MailboxFolderKindSent}, {recipientMailbox.ID, models.MailboxFolderKindInbox}} {
					folder, err := transaction.GetFolderByKind(target.mailboxId, target.kind)
					if err != nil || folder == nil {
						test.Fatalf("folder unavailable: %v", err)
					}
					items, err := transaction.ListItems(folder.ID, nil)
					expectedCount := 1
					if shouldFail {
						expectedCount = 0
					}
					if err != nil || len(items) != expectedCount {
						test.Fatalf("folder %s contains %d items, want %d: %v", target.kind, len(items), expectedCount, err)
					}
				}
				pending, err := transaction.ListDeliveriesToRetry(nil)
				if err != nil || len(pending) != 0 {
					test.Fatalf("local recipients queued externally: %+v, %v", pending, err)
				}
			})
		}
	}
}
