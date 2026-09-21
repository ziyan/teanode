package mx

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type submissionStorage struct {
	storage.Storage
	putError error
	mailId   string
}

func (self *submissionStorage) Put(ctx context.Context, id string, headers []string, body []byte) error {
	self.mailId = id
	if self.putError != nil {
		return self.putError
	}
	return self.Storage.Put(ctx, id, headers, body)
}

func submissionExchange(test *testing.T, database db.Database, spool storage.Storage) (*exchange, *models.Mailbox) {
	test.Helper()
	var mailbox *models.Mailbox
	var domain *models.Domain
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		user, err := transaction.CreateUser(&models.User{Username: "submission-fixture"})
		if err != nil {
			test.Fatal(err)
		}
		role, err := transaction.CreateRole(&models.Role{Name: "Submission fixture", Permissions: []models.Permission{models.PermissionMailSend}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Submission fixture", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
			test.Fatal(err)
		}
		mailbox, err = transaction.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Fixture mailbox"})
		if err != nil {
			test.Fatal(err)
		}
		domain, err = transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "^sender$", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			test.Fatal(err)
		}
	})
	return &exchange{
		database: database, storage: spool, config: config.NewMemoryStore(config.Default()),
		settings:  &Settings{Server: "mail.example.com", Service: "fixture"},
		directory: directory{source: staticDomains{domain}},
	}, mailbox
}

func TestSubmissionStorageAndSQLAcceptanceAreAtomic(test *testing.T) {
	for _, failurePoint := range []string{"storage", "parent", "none"} {
		test.Run(failurePoint, func(test *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(test)
			defer closeDatabase()
			localStorage, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() { _ = localStorage.Close() })
			spool := &submissionStorage{Storage: localStorage}
			injectedErr := errors.New("submission interrupted")
			if failurePoint == "storage" {
				spool.putError = injectedErr
			}
			exchange, mailbox := submissionExchange(test, database, spool)
			exchange.deliveryWake = make(chan struct{}, 1)
			envelope := &mailparse.Envelope{
				ID: "fixture-envelope", MailboxID: mailbox.ID, Sender: "sender@example.com",
				Recipients: []string{"recipient@example.net"}, IP: net.IPv4(127, 0, 0, 1), ReceivedAt: time.Now(),
				Headers: []string{"From: sender@example.com\r\n", "To: recipient@example.net\r\n", "Message-ID: <submission@example.com>\r\n", "Content-Type: text/plain\r\n"},
				Body:    []byte("Fixture body\r\n"), Size: 128,
			}
			err = database.Transaction(func(transaction db.Transaction) error {
				accepted, err := exchange.AcceptSubmission(context.Background(), transaction, envelope)
				if failurePoint == "storage" {
					if !errors.Is(err, injectedErr) || accepted != nil {
						test.Fatalf("storage failure accepted mail: %+v, %v", accepted, err)
					}
					// Committing the enclosing transaction must not retain partial
					// acceptance when its command reported a storage failure.
					return nil
				}
				if err != nil || accepted == nil {
					test.Fatalf("prepare acceptance: %+v, %v", accepted, err)
				}
				if len(exchange.deliveryWake) != 0 {
					test.Fatal("delivery woke before acceptance committed")
				}
				if len(exchange.domainUsagesMap) != 0 {
					test.Fatal("usage changed before acceptance commit")
				}
				if failurePoint == "parent" {
					return injectedErr
				}
				return nil
			})
			if (failurePoint == "parent" && !errors.Is(err, injectedErr)) || (failurePoint != "parent" && err != nil) {
				test.Fatal(err)
			}
			if spool.mailId == "" {
				test.Fatal("submission never reached storage")
			}
			hasAccepted := failurePoint == "none"
			if (len(exchange.deliveryWake) > 0) != hasAccepted {
				test.Fatal("delivery wake does not match committed acceptance")
			}
			expectedCount := 0
			if hasAccepted {
				expectedCount = 1
				_, body, err := spool.Get(context.Background(), spool.mailId)
				if err != nil || string(body) != string(envelope.Body) {
					test.Fatalf("accepted bytes unavailable: %q, %v", body, err)
				}
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				mail, err := transaction.GetMail(spool.mailId, nil)
				if err != nil || (mail != nil) != hasAccepted {
					test.Fatalf("persisted mail: %+v, %v", mail, err)
				}
				items, err := transaction.ListItemsByMail(spool.mailId)
				if err != nil || len(items) != expectedCount {
					test.Fatalf("Sent copy: %+v, %v", items, err)
				}
				deliveries, err := transaction.ListDeliveriesToRetry(nil)
				if err != nil || len(deliveries) != expectedCount {
					test.Fatalf("durable dispatch: %+v, %v", deliveries, err)
				}
				if hasAccepted && (deliveries[0].MailID != spool.mailId || deliveries[0].Attempts != 0) {
					test.Fatal("acceptance dispatched externally or queued a different message")
				}
			})
			if (len(exchange.domainUsagesMap) > 0) != hasAccepted {
				test.Fatalf("usage survived rolled-back acceptance: %+v", exchange.domainUsagesMap)
			}
		})
	}
}
