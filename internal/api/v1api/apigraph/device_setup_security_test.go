package apigraph

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestDeviceProfileRefusesUnsafeSelectionsWithoutCredentials(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		user, err := transaction.GetUser(fixture.owner.User.ID)
		if err != nil {
			test.Fatal(err)
		}
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}, {Permission: models.PermissionCalendarUse}})}
		request := httptest.NewRequest(http.MethodPost, "https://device.example.com"+api.PathGraphQL, nil)
		ctx := api.ContextWithRequest(api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction), request)
		arguments := CreateDeviceSetupProfileArguments{DeviceName: "Test device", MailboxID: &fixture.mailboxId, HasMailAccess: true}
		anonymous := api.ContextWithPrincipal(ctx, nil)
		if _, err := fixture.world.CreateDeviceSetupProfile(anonymous, arguments); !errors.Is(err, api.ErrNotLoggedIn) {
			test.Fatal("anonymous setup was not refused", err)
		}
		for _, invalidArguments := range []CreateDeviceSetupProfileArguments{
			{DeviceName: "Test device"},
			{DeviceName: strings.Repeat("x", 129), HasCalendarAccess: true},
			{DeviceName: "Test device", HasMailAccess: true},
			{DeviceName: "Test device", MailboxID: &fixture.mailboxId, HasCalendarAccess: true},
		} {
			if _, err := fixture.world.CreateDeviceSetupProfile(ctx, invalidArguments); !errors.Is(err, api.ErrInvalidArguments) {
				test.Fatal("invalid selection was not refused", err)
			}
		}
		foreignId := dbtest.CreateUser(test, fixture.database, "other-device-owner")
		foreignMailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: foreignId, Name: "Other account"})
		if err != nil {
			test.Fatal(err)
		}
		arguments.MailboxID = &foreignMailbox.ID
		if _, err := fixture.world.CreateDeviceSetupProfile(ctx, arguments); !errors.Is(err, api.ErrNotFound) {
			test.Fatal("another account's mailbox was not refused", err)
		}
		arguments = CreateDeviceSetupProfileArguments{DeviceName: "Test device", HasCalendarAccess: true}
		insecure := httptest.NewRequest(http.MethodPost, "http://device.example.com"+api.PathGraphQL, nil)
		if _, err := fixture.world.CreateDeviceSetupProfile(api.ContextWithRequest(ctx, insecure), arguments); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatal("remote plain HTTP setup was not refused", err)
		}
		principal.Impersonator = &models.User{ID: foreignId}
		if _, err := fixture.world.CreateDeviceSetupProfile(ctx, arguments); !errors.Is(err, api.ErrPermissionDenied) {
			test.Fatal("impersonated sync setup was not refused", err)
		}
		principal.Impersonator = nil
		if err := fixture.world.config.Update(func(configuration *config.Configuration) error {
			configuration.Services.Calendar.IsEnabled = new(false)
			return nil
		}); err != nil {
			test.Fatal(err)
		}
		var unavailable *models.ServiceUnavailableError
		if _, err := fixture.world.CreateDeviceSetupProfile(ctx, arguments); !errors.As(err, &unavailable) {
			test.Fatal("disabled service setup did not retain its existing error", err)
		}
		arguments.HasCalendarAccess, arguments.HasContactsAccess = false, true
		if _, err := fixture.world.CreateDeviceSetupProfile(ctx, arguments); !errors.Is(err, api.ErrNotFound) {
			test.Fatal("ungranted collection setup was not refused", err)
		}
		passwords, err := transaction.ListAppPasswords(fixture.mailboxId)
		if err != nil || len(passwords) != 0 {
			test.Fatal("refused setup minted a mail credential", err)
		}
		credentials, err := transaction.ListSyncCredentials(user.ID)
		if err != nil || len(credentials) != 0 {
			test.Fatal("refused setup minted a sync credential", err)
		}
	})
}
