package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type recordingSubmissionExchange struct {
	mx.Exchange
	acceptanceError error
	mailId          string
}

func (self *recordingSubmissionExchange) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope) (*models.Mail, error) {
	mail, err := transaction.CreateMail(&models.Mail{EnvelopeID: envelope.ID, DomainID: envelope.DomainID}, nil)
	if err != nil {
		return nil, err
	}
	self.mailId = mail.ID
	if self.acceptanceError != nil {
		return nil, self.acceptanceError
	}
	return mail, nil
}

func TestSubmissionCompositionUsesCallerTransactionForMedia(test *testing.T) {
	for _, failurePoint := range []string{"none", "acceptance", "media"} {
		test.Run(failurePoint, func(test *testing.T) {
			database, closeDatabase := dbtest.AcquireDatabase(test)
			defer closeDatabase()
			var domain *models.Domain
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				var err error
				domain, err = transaction.CreateDomain(&models.Domain{Domain: "example.com", LinkHost: "images.example.com"})
				if err != nil {
					test.Fatal(err)
				}
			})
			media, err := database.CreateMedia(&models.Media{ID: "fixtureimage", DomainID: domain.ID, Filename: "fixture.png", ContentType: "image/png"})
			if err != nil {
				test.Fatal(err)
			}
			if failurePoint == "media" {
				dbtest.Exec(test, database, `ALTER TABLE media_link ADD CONSTRAINT fixture_refuses_links CHECK (false)`)
			}
			exchange := &recordingSubmissionExchange{}
			if failurePoint == "acceptance" {
				exchange.acceptanceError = errors.New("acceptance refused")
			}
			mailer := &mailer{database: database, config: config.NewMemoryStore(config.Default()), exchange: exchange}
			envelope := &mailparse.Envelope{MailboxID: "fixture-mailbox"}
			err = database.Transaction(func(transaction db.Transaction) error {
				// Composition must see this uncommitted change on the same
				// connection, rather than looking up the old domain independently.
				if _, err := transaction.UpdateDomain(domain.ID, func(domain *models.Domain) error {
					domain.Domain = "example.net"
					domain.LinkHost = "images.example.net"
					return nil
				}); err != nil {
					return err
				}
				accepted, err := mailer.AcceptSubmission(context.Background(), transaction, envelope, &Message{
					From: "sender@example.net", To: []string{"recipient@example.org"}, Bcc: []string{"blind@example.org"},
					Subject: "Fixture", HTML: `<img src="/media/` + media.ID + `">`,
				})
				if failurePoint == "acceptance" {
					if !errors.Is(err, exchange.acceptanceError) || accepted != nil {
						test.Fatalf("acceptance failure = %+v, %v", accepted, err)
					}
					return nil
				}
				if err != nil || accepted == nil {
					test.Fatalf("submission = %+v, %v", accepted, err)
				}
				return nil
			})
			if err != nil {
				test.Fatal(err)
			}
			if exchange.mailId == "" || len(envelope.Recipients) != 2 || envelope.Sender != "sender@example.net" {
				test.Fatal("composition did not produce the intended envelope")
			}
			if strings.Contains(strings.Join(envelope.Headers, ""), "blind@example.org") {
				test.Fatal("blind recipient leaked into headers")
			}
			links, err := database.ListMediaLinksForEnvelope(envelope.ID)
			expectedLinkCount := 0
			if failurePoint == "none" {
				expectedLinkCount = 1
			}
			if err != nil || len(links) != expectedLinkCount {
				test.Fatalf("media links = %d, want %d: %v", len(links), expectedLinkCount, err)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				mail, err := transaction.GetMail(exchange.mailId, nil)
				if err != nil || (mail != nil) != (failurePoint != "acceptance") {
					test.Fatalf("mail acceptance escaped its scope: %+v, %v", mail, err)
				}
			})
		})
	}
}
