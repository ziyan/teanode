package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The list of every mailbox names every account on the server, so the
// operator and whoever manages accounts see it and a manager of one domain
// does not.
func TestOnlyTheOperatorListsEveryMailbox(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	list := func(principal *api.Principal) ([]*MailboxSummary, error) {
		var summaries []*MailboxSummary
		var err error
		dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			summaries, err = fixture.world.ListAllMailboxes(ctx)
		})
		return summaries, err
	}
	caller := &models.User{ID: fixture.owner.User.ID}

	domainManager := &api.Principal{User: caller, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionDomainManage, DomainID: "a-domain"}})}
	if _, err := list(domainManager); !errors.Is(err, api.ErrNotFound) {
		test.Fatal("a domain manager listed every mailbox", err)
	}
	for name, permission := range map[string]models.Permission{"the operator": models.PermissionServerManage, "an account manager": models.PermissionUserManage} {
		principal := &api.Principal{User: caller, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: permission}})}
		summaries, err := list(principal)
		if err != nil {
			test.Fatalf("%s could not list every mailbox: %s", name, err)
		}
		if len(summaries) != 1 || summaries[0].ID != fixture.mailboxId {
			test.Fatalf("%s saw %+v", name, summaries)
		}
	}
}
