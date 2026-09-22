package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type domainAPIMailer struct {
	mailer.Mailer
	acceptCount int
}

func (self *domainAPIMailer) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *mailer.Message) (*models.Mail, error) {
	self.acceptCount++
	return transaction.CreateMail(&models.Mail{DomainID: envelope.DomainID, Subject: message.Subject}, nil)
}

func TestDomainSendReplaysBeforeTemplateReadsAndPreservesAcceptanceAfterRetention(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	sender := &domainAPIMailer{}
	resolver := &graph{mailer: sender, config: config.NewMemoryStore(config.Default())}
	var arguments SendMailArguments
	var principal *api.Principal
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		domain, err := transaction.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			test.Fatal(err)
		}
		template, err := transaction.CreateTemplate(&models.Template{DomainID: domain.ID, Name: "fixture", Subject: "Fixture", TextContent: "Content"}, nil)
		if err != nil {
			test.Fatal(err)
		}
		arguments = SendMailArguments{DomainID: domain.ID, SubmissionID: "fixture-send", MessageParameters: MessageParameters{From: "sender@example.com", To: []string{"recipient@example.net"}, TemplateID: template.ID}}
		principal = &api.Principal{Console: true, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionDomainManage, DomainID: domain.ID}})}
	})
	var original *SendMailReturnValue
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		var err error
		original, err = resolver.SendMail(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteTemplate(arguments.MessageParameters.TemplateID, nil); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeleteMail(original.MailID, nil); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
		replay, err := resolver.SendMail(ctx, arguments)
		if err != nil || replay == nil || replay.Mail != nil || replay.MailID != original.MailID || replay.SubmissionID != arguments.SubmissionID || sender.acceptCount != 1 {
			test.Fatalf("replay=%+v, %v", replay, err)
		}
		lookup, err := resolver.GetDomainSubmission(ctx, GetDomainSubmissionArguments{DomainID: arguments.DomainID, SubmissionID: arguments.SubmissionID})
		if err != nil || lookup == nil || lookup.MailID != original.MailID {
			test.Fatalf("lookup after retention=%+v, %v", lookup, err)
		}
		arguments.MessageParameters.Subject = "Changed"
		if _, err := resolver.SendMail(ctx, arguments); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("changed request=%v", err)
		}
	})
}

func TestDomainSendSchemaSupportsLegacyAndIdentifiedRequests(test *testing.T) {
	resolver := &graph{schema: buildSchemaForValidation(test)}
	for _, document := range []string{
		`query ($domainId: String!, $submissionId: String!) { GetDomainSubmission(domainId: $domainId, submissionId: $submissionId) { submissionId mailId acceptedAt } }`,
		`mutation ($domainId: String!, $messageParameters: MessageParametersInput!) { SendMail(domainId: $domainId, messageParameters: $messageParameters) { mail { id } } }`,
		`mutation ($domainId: String!, $submissionId: String, $messageParameters: MessageParametersInput!) { SendMail(domainId: $domainId, submissionId: $submissionId, messageParameters: $messageParameters) { submissionId mailId mail { id } } }`,
	} {
		if _, rejected := resolver.prepareGraphRequest(&graphRequest{Query: document}); rejected != nil {
			test.Fatalf("schema rejected send: %+v", rejected)
		}
	}
}
