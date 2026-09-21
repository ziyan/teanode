package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

type submissionAPIMailer struct {
	mailer.Mailer
	acceptCount int
	domainId    string
}

func (self *submissionAPIMailer) Send(context.Context, *mailparse.Envelope, *mailer.Message) error {
	return errors.New("legacy Send must not be used")
}

func (self *submissionAPIMailer) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *mailer.Message) (*models.Mail, error) {
	self.acceptCount++
	mail, err := transaction.CreateMail(&models.Mail{DomainID: self.domainId, Subject: message.Subject, MessageID: "<" + security.NewULID() + "@example.com>"}, nil)
	if err != nil {
		return nil, err
	}
	sent, err := transaction.GetFolderByKind(envelope.MailboxID, models.MailboxFolderKindSent)
	if err != nil {
		return nil, err
	}
	if _, err := transaction.AddItem(sent.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
		return nil, err
	}
	return mail, nil
}

func submissionAPIFixture(test *testing.T) (db.Database, *graph, *api.Principal, SendMailboxMessageArguments, *submissionAPIMailer) {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	userId := dbtest.CreateUser(test, database, "submission-owner")
	principal := &api.Principal{User: &models.User{ID: userId}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailSend}})}
	sender := &submissionAPIMailer{}
	arguments := SendMailboxMessageArguments{SubmissionID: "fixture-send", Message: MailboxMessageParameters{From: "sender@example.com", To: []string{"recipient@example.net"}, Subject: "Fixture", TextContent: "Fixture content"}}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: userId, Name: "Send fixture"})
		if err != nil {
			test.Fatal(err)
		}
		arguments.MailboxID = mailbox.ID
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		sender.domainId = domain.ID
		if _, err := transaction.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "^sender$", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			test.Fatal(err)
		}
		drafts, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindDrafts)
		if err != nil {
			test.Fatal(err)
		}
		mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindDraft}, nil)
		if err != nil {
			test.Fatal(err)
		}
		draft, err := transaction.AddItem(drafts.ID, mail.ID, "", models.MailboxItemFlags{Draft: new(true)})
		if err != nil {
			test.Fatal(err)
		}
		arguments.Message.DraftItemID = draft.ID
	})
	return database, &graph{database: database, config: config.NewMemoryStore(config.Default()), mailer: sender}, principal, arguments, sender
}

func TestMailboxSendRetryRecoversBookkeepingWithoutResending(test *testing.T) {
	database, resolver, principal, arguments, sender := submissionAPIFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE mail_submission ADD CONSTRAINT fixture_reconcile CHECK (reconciled_at IS NULL)`)
	var first *SendMailboxMessageReturnValue
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		var err error
		first, err = resolver.SendMailboxMessage(ctx, arguments)
		if err != nil || first == nil || first.Mail == nil || first.Item == nil {
			test.Fatalf("acceptance = %+v, %v", first, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		accepted, err := transaction.LockSubmission(principal.User.ID, arguments.SubmissionID)
		if err != nil || accepted == nil || accepted.ReconciledAt != nil || accepted.MailID != first.Mail.ID {
			test.Fatalf("pending acceptance = %+v, %v", accepted, err)
		}
		draft, err := transaction.GetItem(arguments.Message.DraftItemID)
		if err != nil || draft == nil {
			test.Fatalf("failed reconciliation did not roll back cleanup: %+v, %v", draft, err)
		}
	})
	dbtest.Exec(test, database, `ALTER TABLE mail_submission DROP CONSTRAINT fixture_reconcile`)
	if err := mailer.NewSubmissionReconciler(database).RunOnce(context.Background()); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		replay, err := resolver.SendMailboxMessage(ctx, arguments)
		if err != nil || replay == nil || replay.Mail == nil || replay.Item == nil || replay.Mail.ID != first.Mail.ID || replay.Item.ID != first.Item.ID {
			test.Fatalf("replay = %+v, %v", replay, err)
		}
		changed := arguments
		changed.Message.TextContent = "Different content"
		if _, err := resolver.SendMailboxMessage(ctx, changed); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("changed request was not refused: %v", err)
		}
		draft, err := transaction.GetItem(arguments.Message.DraftItemID)
		if err != nil || draft != nil {
			test.Fatalf("draft was not recovered: %+v, %v", draft, err)
		}
		if err := transaction.DeleteMail(first.Mail.ID, nil); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		replay, err := resolver.SendMailboxMessage(ctx, arguments)
		if err != nil || replay == nil || replay.Mail != nil {
			test.Fatalf("replay after retention = %+v, %v", replay, err)
		}
	})
	if sender.acceptCount != 1 {
		test.Fatalf("accepted %d times", sender.acceptCount)
	}
}

func TestMailboxSendRollsBackWithEnclosingCommand(test *testing.T) {
	database, resolver, principal, arguments, sender := submissionAPIFixture(test)
	injected := errors.New("parent command failed")
	var acceptedMailId string
	err := database.TransactionContext(context.Background(), func(transaction db.Transaction) error {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		response, err := resolver.SendMailboxMessage(ctx, arguments)
		if err != nil {
			return err
		}
		acceptedMailId = response.Mail.ID
		return injected
	})
	if !errors.Is(err, injected) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		accepted, err := transaction.LockSubmission(principal.User.ID, arguments.SubmissionID)
		if err != nil || accepted != nil {
			test.Fatalf("rolled back identity survived: %+v, %v", accepted, err)
		}
		stored, err := transaction.GetMail(acceptedMailId, nil)
		if err != nil || stored != nil {
			test.Fatalf("rolled back acceptance survived: %+v, %v", stored, err)
		}
		draft, err := transaction.GetItem(arguments.Message.DraftItemID)
		if err != nil || draft == nil {
			test.Fatalf("rollback lost original draft: %+v, %v", draft, err)
		}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		if _, err := resolver.SendMailboxMessage(ctx, arguments); err != nil {
			test.Fatal(err)
		}
	})
	if sender.acceptCount != 2 {
		test.Fatalf("retry after rollback did not prepare again: %d", sender.acceptCount)
	}
}

func TestMailboxSendRejectsInvalidIdentityBeforeAcceptance(test *testing.T) {
	database, resolver, principal, arguments, sender := submissionAPIFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		for _, submissionId := range []string{strings.Repeat("x", 129), "invalid\x00identity"} {
			arguments.SubmissionID = submissionId
			if _, err := resolver.SendMailboxMessage(ctx, arguments); !errors.Is(err, api.ErrInvalidArguments) {
				test.Fatalf("invalid identifier returned %v", err)
			}
		}
	})
	if sender.acceptCount != 0 {
		test.Fatal("invalid identity reached acceptance")
	}
}

func TestMailboxSendSchemaAcceptsLegacyAndIdentifiedRequests(test *testing.T) {
	resolver := &graph{schema: buildSchemaForValidation(test)}
	for _, document := range []string{
		`query ($mailboxId: String!, $submissionId: String!) { GetMailboxSubmission(mailboxId: $mailboxId, submissionId: $submissionId) { submissionId mailId sentItemId acceptedAt isReconciled } }`,
		`mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) { SendMailboxMessage(mailboxId: $mailboxId, message: $message) { mail { id } item { id } } }`,
		`mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!, $submissionId: String) { SendMailboxMessage(mailboxId: $mailboxId, message: $message, submissionId: $submissionId) { mail { id } item { id } } }`,
	} {
		if _, rejected := resolver.prepareGraphRequest(&graphRequest{Query: document}); rejected != nil {
			test.Fatalf("send schema rejected document: %v", rejected.Errors)
		}
	}
}

func TestMailboxSubmissionLookupPreservesIdentityAndEnforcesOwnership(test *testing.T) {
	database, resolver, principal, arguments, _ := submissionAPIFixture(test)
	var mailId, sentItemId, otherMailboxId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		response, err := resolver.SendMailboxMessage(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
		mailId, sentItemId = response.Mail.ID, response.Item.ID
		otherMailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: principal.User.ID, Name: "Other mailbox"})
		if err != nil {
			test.Fatal(err)
		}
		otherMailboxId = otherMailbox.ID
		if err := transaction.DeleteMail(mailId, nil); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		query := GetMailboxSubmissionArguments{MailboxID: arguments.MailboxID, SubmissionID: arguments.SubmissionID}
		accepted, err := resolver.GetMailboxSubmission(ctx, query)
		if err != nil || accepted == nil || accepted.MailID != mailId || accepted.SentItemID != sentItemId || !accepted.IsReconciled || accepted.AcceptedAt.IsZero() {
			test.Fatalf("retained identity = %+v, %v", accepted, err)
		}
		query.MailboxID = otherMailboxId
		if accepted, err := resolver.GetMailboxSubmission(ctx, query); err != nil || accepted != nil {
			test.Fatalf("wrong mailbox returned identity: %+v, %v", accepted, err)
		}
		query.MailboxID = arguments.MailboxID
		stranger := &api.Principal{User: &models.User{ID: "other-owner"}, Permissions: principal.Permissions}
		strangersContext := api.ContextWithPrincipal(ctx, stranger)
		if _, err := resolver.GetMailboxSubmission(strangersContext, query); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("other owner lookup = %v", err)
		}
		reader := &api.Principal{User: principal.User, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
		if _, err := resolver.GetMailboxSubmission(api.ContextWithPrincipal(ctx, reader), query); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("lookup without send permission = %v", err)
		}
		query.SubmissionID = "unknown-request"
		if accepted, err := resolver.GetMailboxSubmission(ctx, query); err != nil || accepted != nil {
			test.Fatalf("unknown identity = %+v, %v", accepted, err)
		}
	})
}
