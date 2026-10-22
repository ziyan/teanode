package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An operator signed in as a person makes no credential that outlives the
// visit: not a token, not a longer life for one, not a passkey, not an app
// password. The person makes them; the same calls work for them.
func TestAnImpersonatorMakesNoLastingCredential(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	operatorId := dbtest.CreateUser(test, fixture.database, "an-operator")
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		user, err := transaction.GetUser(fixture.owner.User.ID)
		if err != nil {
			test.Fatal(err)
		}
		principal := &api.Principal{
			User:           user,
			Impersonator:   &models.User{ID: operatorId},
			Permissions:    models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}}),
			CredentialKind: models.CredentialSession,
		}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		lifetime := "30d"

		if _, err := fixture.world.CreateToken(ctx, CreateTokenArguments{Name: "kept"}); !errors.Is(err, api.ErrPermissionDenied) {
			test.Fatal("a token was not refused while signed in as the person", err)
		}
		if _, err := fixture.world.UpdateToken(ctx, UpdateTokenArguments{TokenID: "any", Lifetime: &lifetime}); !errors.Is(err, api.ErrPermissionDenied) {
			test.Fatal("a longer token life was not refused while signed in as the person", err)
		}
		if _, err := fixture.world.BeginPasskeyRegistration(ctx); !errors.Is(err, api.ErrPermissionDenied) {
			test.Fatal("a passkey was not refused while signed in as the person", err)
		}
		if _, err := fixture.world.FinishPasskeyRegistration(ctx, FinishPasskeyRegistrationArguments{CeremonyID: "any", Response: "{}"}); !errors.Is(err, api.ErrPermissionDenied) {
			test.Fatal("finishing a passkey was not refused while signed in as the person", err)
		}
		if _, err := fixture.world.CreateMailboxAppPassword(ctx, CreateMailboxAppPasswordArguments{MailboxID: fixture.mailboxId, Name: "kept"}); !errors.Is(err, api.ErrPermissionDenied) {
			test.Fatal("an app password was not refused while signed in as the person", err)
		}
		passwords, err := transaction.ListAppPasswords(fixture.mailboxId)
		if err != nil {
			test.Fatal(err)
		}
		if len(passwords) != 0 {
			test.Fatalf("%d app passwords were made while signed in as the person", len(passwords))
		}

		principal.Impersonator = nil
		if _, err := fixture.world.CreateMailboxAppPassword(ctx, CreateMailboxAppPasswordArguments{MailboxID: fixture.mailboxId, Name: "own"}); err != nil {
			test.Fatal("the person could not make their own app password", err)
		}
	})
}
