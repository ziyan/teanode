package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A permission granted over one domain used to open these resolvers for
// every domain: they checked that the caller held domain:manage or
// mail:audit somewhere, then read whichever row was named. A manager of
// one domain could read, rewrite and delete another domain's templates and
// layouts by identifier, and an auditor of one domain could read and
// resend another's deliveries. Each now checks the permission over the
// row's own domain, and answers not found otherwise.
func TestRowsAreScopedToTheDomainThePermissionIsHeldOver(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var mine, theirs *models.Domain
	var myTemplate, theirTemplate *models.Template
	var myLayout, theirLayout *models.Layout
	var myDelivery, theirDelivery *models.Delivery
	var myMail, theirMail *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if mine, err = tx.CreateDomain(&models.Domain{Domain: "mine.example"}); err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		if theirs, err = tx.CreateDomain(&models.Domain{Domain: "theirs.example"}); err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		for _, entry := range []struct {
			domain   *models.Domain
			template **models.Template
			layout   **models.Layout
			mail     **models.Mail
			delivery **models.Delivery
		}{
			{mine, &myTemplate, &myLayout, &myMail, &myDelivery},
			{theirs, &theirTemplate, &theirLayout, &theirMail, &theirDelivery},
		} {
			if *entry.layout, err = tx.CreateLayout(&models.Layout{DomainID: entry.domain.ID, HTMLContent: "<p>{{ content }}</p>"}, nil); err != nil {
				t.Fatalf("CreateLayout: %s", err)
			}
			if *entry.template, err = tx.CreateTemplate(&models.Template{DomainID: entry.domain.ID, Name: "welcome", Subject: "hi", TextContent: "hi"}, nil); err != nil {
				t.Fatalf("CreateTemplate: %s", err)
			}
			if *entry.mail, err = tx.CreateMail(&models.Mail{DomainID: entry.domain.ID, Sender: "sender@example.net"}, nil); err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if *entry.delivery, err = tx.CreateDelivery(&models.Delivery{MailID: (*entry.mail).ID, Recipient: "to@example.net", Kind: models.DeliveryKindForward, Status: models.DeliveryStatusFailed}, nil); err != nil {
				t.Fatalf("CreateDelivery: %s", err)
			}
		}
	})

	principal := &api.Principal{
		User: &models.User{ID: "u1", Username: "manager"},
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionDomainManage, DomainID: mine.ID},
			{Permission: models.PermissionMailAudit, DomainID: mine.ID},
		}),
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		notFound := func(name string, err error) {
			t.Helper()
			if !errors.Is(err, api.ErrNotFound) {
				t.Errorf("%s over another domain answered %v, want %v", name, err, api.ErrNotFound)
			}
		}
		allowed := func(name string, err error) {
			t.Helper()
			if err != nil {
				t.Errorf("%s over the caller's own domain failed: %s", name, err)
			}
		}

		_, err := resolver.GetTemplate(ctx, GetTemplateArguments{TemplateID: &theirTemplate.ID})
		notFound("GetTemplate", err)
		_, err = resolver.GetTemplate(ctx, GetTemplateArguments{DomainID: &theirs.ID, Name: &theirTemplate.Name})
		notFound("GetTemplate by name", err)
		_, err = resolver.GetTemplate(ctx, GetTemplateArguments{TemplateID: &myTemplate.ID})
		allowed("GetTemplate", err)
		notFound("DeleteTemplate", resolver.DeleteTemplate(ctx, DeleteTemplateArguments{TemplateID: theirTemplate.ID}))

		_, err = resolver.GetLayout(ctx, GetLayoutArguments{LayoutID: theirLayout.ID})
		notFound("GetLayout", err)
		_, err = resolver.GetLayout(ctx, GetLayoutArguments{LayoutID: myLayout.ID})
		allowed("GetLayout", err)
		_, err = resolver.ModifyLayout(ctx, ModifyLayoutArguments{LayoutID: theirLayout.ID, LayoutParameters: LayoutParameters{HTMLContent: "<p>phishing {{ content }}</p>"}})
		notFound("ModifyLayout", err)
		notFound("DeleteLayout", resolver.DeleteLayout(ctx, DeleteLayoutArguments{LayoutID: theirLayout.ID}))

		_, err = resolver.GetDelivery(ctx, GetDeliveryArguments{DeliveryID: theirDelivery.ID})
		notFound("GetDelivery", err)
		_, err = resolver.GetDelivery(ctx, GetDeliveryArguments{DeliveryID: myDelivery.ID})
		allowed("GetDelivery", err)
		_, err = resolver.RetryDelivery(ctx, RetryDeliveryArguments{DeliveryID: theirDelivery.ID})
		notFound("RetryDelivery", err)
		_, err = resolver.ListDeliveriesByMail(ctx, ListDeliveriesByMailArguments{MailID: theirMail.ID})
		notFound("ListDeliveriesByMail", err)
		_, err = resolver.GetMailOpens(ctx, GetMailOpensArguments{MailID: theirMail.ID})
		notFound("GetMailOpens", err)

		opens, err := resolver.ListMailOpens(ctx, ListMailOpensArguments{MailIDs: []string{myMail.ID, theirMail.ID}})
		allowed("ListMailOpens", err)
		if len(opens) != 1 || opens[0].MailID != myMail.ID {
			t.Errorf("ListMailOpens answered for %+v, want only the caller's own", opens)
		}
	})

	// The row is still there for whoever does hold the permission.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		template, err := tx.GetTemplate(theirTemplate.ID, nil)
		if err != nil || template == nil {
			t.Errorf("the other domain's template is gone: %v, %v", template, err)
		}
	})
}
