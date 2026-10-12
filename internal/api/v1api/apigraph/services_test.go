package apigraph

import (
	"errors"
	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"testing"
)

func TestDisabledCollectionsDoNotProvisionAndSyncNeedsNoMailbox(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	userId := dbtest.CreateUser(test, database, "sync-owner")
	configuration := config.Default()
	configuration.Server.Name = "assistant.example.com"
	configuration.Listen.HTTPS = ""
	configuration.TLS.ACME.Enabled = false
	configuration.Services = config.AssistantServices()
	store := config.NewMemoryStore(configuration)
	defer func() { _ = store.Close() }()
	resolver := &graph{database: database, config: store}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		user, err := transaction.GetUser(userId)
		if err != nil {
			test.Fatal(err)
		}
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionContactsUse}, {Permission: models.PermissionCalendarUse}})}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.ListCalendars(ctx); err == nil {
			test.Fatal("disabled calendar was read")
		}
		if _, err := resolver.ListAddressBooks(ctx); err == nil {
			test.Fatal("disabled contacts were read")
		}
		if err := store.Update(func(configuration *config.Configuration) error {
			configuration.Services.Contacts.IsEnabled = new(true)
			return nil
		}); err != nil {
			test.Fatal(err)
		}
		created, err := resolver.CreateSyncCredential(ctx, CreateSyncCredentialArguments{CredentialName: "Test device", HasContactsAccess: true})
		if err != nil {
			test.Fatal(err)
		}
		verified, err := access.AuthenticateSyncCredential(transaction, user.Username, created.Password)
		if err != nil {
			test.Fatal(err)
		}
		if !verified.Allows(models.ServiceContacts) || verified.Allows(models.ServiceCalendar) {
			test.Fatal("credential scope changed")
		}
		if _, err := access.AuthenticateSyncCredential(transaction, "another-owner", created.Password); !errors.Is(err, access.ErrInvalidAppPassword) {
			test.Fatal("another account opened the credential")
		}
		if _, err := access.AuthenticateAppPassword(transaction, user.Username, created.Password); !errors.Is(err, access.ErrInvalidAppPassword) {
			test.Fatal("sync password opened mail")
		}
		if _, err := resolver.RevokeSyncCredential(ctx, RevokeSyncCredentialArguments{CredentialID: created.Credential.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := access.AuthenticateSyncCredential(transaction, user.Username, created.Password); !errors.Is(err, access.ErrInvalidAppPassword) {
			test.Fatal("revoked password still worked")
		}
	})
	for _, table := range []string{"mailbox", "calendar", "addressbook"} {
		if dbtest.QueryString(test, database, "SELECT count(*)::text FROM "+table) != "0" {
			test.Fatalf("disabled resource was provisioned: %s", table)
		}
	}
}
