package apigraph

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ceremony"
)

func deviceProfileStrings(test *testing.T, written []byte) map[string]string {
	test.Helper()
	fields := map[string]string{}
	decoder := xml.NewDecoder(bytes.NewReader(written))
	fieldName := ""
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return fields
		}
		if err != nil {
			test.Fatal(err)
		}
		start, isStart := token.(xml.StartElement)
		if !isStart {
			continue
		}
		switch start.Name.Local {
		case "key":
			if err := decoder.DecodeElement(&fieldName, &start); err != nil {
				test.Fatal(err)
			}
		case "string":
			var fieldText string
			if err := decoder.DecodeElement(&fieldText, &start); err != nil {
				test.Fatal(err)
			}
			fields[fieldName] = fieldText
		}
	}
}

func TestDeviceProfileUsesSeparateCredentialsAndSelectedPayloads(test *testing.T) {
	for _, selection := range []struct {
		selectionName      string
		hasMailAccess      bool
		hasCalendarAccess  bool
		hasRemindersAccess bool
		hasContactsAccess  bool
	}{{"mail-only", true, false, false, false}, {"mailbox-free", false, true, false, true}, {"reminders-only", false, false, true, false}, {"combined", true, true, true, true}} {
		test.Run(selection.selectionName, func(test *testing.T) {
			fixture := newSetupProfileFixture(test)
			dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
				role, err := transaction.CreateRole(&models.Role{Name: "Device profile fixture", Permissions: []models.Permission{models.PermissionMailRead}})
				if err != nil {
					test.Fatal(err)
				}
				if _, err := transaction.CreateGroup(&models.Group{Name: "Device profile fixture", UserIDs: []string{fixture.owner.User.ID}, RoleIDs: []string{role.ID}}); err != nil {
					test.Fatal(err)
				}
				user, err := transaction.GetUser(fixture.owner.User.ID)
				if err != nil {
					test.Fatal(err)
				}
				principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}, {Permission: models.PermissionCalendarUse}, {Permission: models.PermissionContactsUse}})}
				request := httptest.NewRequest(http.MethodPost, "https://device.example.com"+api.PathGraphQL, nil)
				ctx := api.ContextWithRequest(api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction), request)
				arguments := CreateDeviceSetupProfileArguments{DeviceName: "Test device", HasMailAccess: selection.hasMailAccess, HasCalendarAccess: selection.hasCalendarAccess, HasRemindersAccess: selection.hasRemindersAccess, HasContactsAccess: selection.hasContactsAccess}
				if selection.hasMailAccess {
					arguments.MailboxID = &fixture.mailboxId
				} else {
					if err := fixture.world.config.Update(func(configuration *config.Configuration) error {
						configuration.Services.Mailboxes.IsEnabled = new(false)
						return nil
					}); err != nil {
						test.Fatal(err)
					}
				}
				created, err := fixture.world.CreateDeviceSetupProfile(ctx, arguments)
				if err != nil {
					test.Fatal(err)
				}
				pathParts := strings.Split(api.PathMailboxSetupProfile, "{token}")
				token := strings.TrimSuffix(strings.TrimPrefix(created.DownloadPath, pathParts[0]), pathParts[1])
				written, err := fixture.world.takeSetupProfile(ctx, token)
				if err != nil || len(written) == 0 {
					test.Fatal("profile did not download", err)
				}
				if again, err := fixture.world.takeSetupProfile(ctx, token); err != nil || len(again) != 0 {
					test.Fatal("profile was not single-use", err)
				}
				fields := deviceProfileStrings(test, written)
				if strings.Contains(string(written), "com.apple.mail.managed") != selection.hasMailAccess || strings.Contains(string(written), "com.apple.caldav.account") != (selection.hasCalendarAccess || selection.hasRemindersAccess) || strings.Contains(string(written), "com.apple.carddav.account") != selection.hasContactsAccess {
					test.Fatal("profile included unselected services")
				}
				if selection.hasMailAccess {
					if created.MailboxAppPassword == nil {
						test.Fatal("mail credential missing")
					}
					if _, err := access.AuthenticateAppPassword(transaction, fields["IncomingMailServerUsername"], fields["IncomingPassword"]); err != nil {
						test.Fatal("mail credential did not authenticate", err)
					}
				} else if created.MailboxAppPassword != nil {
					test.Fatal("mailbox-free setup minted a mail credential")
				}
				if !selection.hasCalendarAccess && !selection.hasRemindersAccess && !selection.hasContactsAccess {
					if created.SyncCredential != nil {
						test.Fatal("mail-only setup minted a sync credential")
					}
					return
				}
				passwordField := "CalDAVPassword"
				if !selection.hasCalendarAccess && !selection.hasRemindersAccess {
					passwordField = "CardDAVPassword"
				}
				credential, err := access.AuthenticateSyncCredential(transaction, user.Username, fields[passwordField])
				if err != nil || credential.HasCalendarAccess != selection.hasCalendarAccess || credential.HasRemindersAccess != selection.hasRemindersAccess || credential.HasContactsAccess != selection.hasContactsAccess {
					test.Fatal("sync scope changed", err)
				}
				if _, err := access.AuthenticateAppPassword(transaction, user.Username, fields[passwordField]); !errors.Is(err, access.ErrInvalidAppPassword) {
					test.Fatal("sync credential authenticated mail")
				}
				if err := transaction.DeleteSyncCredential(user.ID, created.SyncCredential.ID); err != nil {
					test.Fatal(err)
				}
				if selection.hasMailAccess {
					if _, err := access.AuthenticateAppPassword(transaction, fields["IncomingMailServerUsername"], fields["IncomingPassword"]); err != nil {
						test.Fatal("sync revocation revoked mail", err)
					}
				}
			})
		})
	}
}

type failingDeviceProfileStore struct{ ceremony.Store }

func (self *failingDeviceProfileStore) Park(context.Context, *ceremony.Ceremony) (string, error) {
	return "", errors.New("synthetic profile parking failure")
}

func TestDeviceProfileFailureRollsBackBothCredentials(test *testing.T) {
	fixture := newSetupProfileFixture(test)
	fixture.world.ceremonies = &failingDeviceProfileStore{Store: fixture.world.ceremonies}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		user, err := transaction.GetUser(fixture.owner.User.ID)
		if err != nil {
			test.Fatal(err)
		}
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailboxManage}, {Permission: models.PermissionCalendarUse}})}
		request := httptest.NewRequest(http.MethodPost, "https://device.example.com"+api.PathGraphQL, nil)
		ctx := api.ContextWithRequest(api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction), request)
		if created, err := fixture.world.CreateDeviceSetupProfile(ctx, CreateDeviceSetupProfileArguments{DeviceName: "Test device", MailboxID: &fixture.mailboxId, HasMailAccess: true, HasCalendarAccess: true}); err == nil || created != nil {
			test.Fatal("profile parking failure was swallowed")
		}
		passwords, err := transaction.ListAppPasswords(fixture.mailboxId)
		if err != nil || len(passwords) != 0 {
			test.Fatal("failed profile retained a mail credential", err)
		}
		credentials, err := transaction.ListSyncCredentials(user.ID)
		if err != nil || len(credentials) != 0 {
			test.Fatal("failed profile retained a sync credential", err)
		}
	})
}
