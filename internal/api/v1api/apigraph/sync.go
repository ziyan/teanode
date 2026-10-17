package apigraph

import (
	"context"
	"fmt"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/dav"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/qrcode"
	"github.com/ziyan/teanode/internal/util/security"
	"net/url"
	"strings"
	"time"
)

type SyncQuery interface {
	ListSyncCredentials(context.Context) ([]*models.SyncCredential, error)
}
type SyncMutation interface {
	CreateSyncCredential(context.Context, CreateSyncCredentialArguments) (*CreatedSyncCredential, error)
	RevokeSyncCredential(context.Context, RevokeSyncCredentialArguments) (bool, error)
	CreateSyncSetupProfile(context.Context, CreateSyncCredentialArguments) (*CreatedSyncProfile, error)
}

type CreateSyncCredentialArguments struct {
	CredentialName     string `json:"credentialName"`
	HasCalendarAccess  bool   `json:"hasCalendarAccess"`
	HasRemindersAccess bool   `json:"hasRemindersAccess"`
	HasContactsAccess  bool   `json:"hasContactsAccess"`
	// HasPhotosAccess lets the device upload to the photo library. Optional,
	// so that the clients written before photos existed keep working.
	HasPhotosAccess *bool `json:"hasPhotosAccess" graphapi:"nullable"`
}
type RevokeSyncCredentialArguments struct {
	CredentialID string `json:"credentialId"`
}
type CreatedSyncCredential struct {
	Credential    *models.SyncCredential `json:"credential"`
	Username      string                 `json:"username"`
	Password      string                 `json:"password"`
	PrincipalPath string                 `json:"principalPath"`
}
type CreatedSyncProfile struct {
	DownloadLink string                 `json:"downloadLink"`
	QRCodeSVG    string                 `json:"qrCodeSvg"`
	ExpiresAt    time.Time              `json:"expiresAt"`
	Credential   *models.SyncCredential `json:"credential"`
}

func (self *graph) ListSyncCredentials(ctx context.Context) ([]*models.SyncCredential, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	return self.transaction(ctx).ListSyncCredentials(user.ID)
}
func (self *graph) RevokeSyncCredential(ctx context.Context, arguments RevokeSyncCredentialArguments) (bool, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).DeleteSyncCredential(user.ID, arguments.CredentialID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}
func (self *graph) CreateSyncCredential(ctx context.Context, arguments CreateSyncCredentialArguments) (*CreatedSyncCredential, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	hasPhotosAccess := arguments.HasPhotosAccess != nil && *arguments.HasPhotosAccess
	if strings.TrimSpace(arguments.CredentialName) == "" || len(arguments.CredentialName) > 128 || (!arguments.HasCalendarAccess && !arguments.HasRemindersAccess && !arguments.HasContactsAccess && !hasPhotosAccess) {
		return nil, fmt.Errorf("%w: give the device a name and select at least one collection", api.ErrInvalidArguments)
	}
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.IsImpersonation() {
		return nil, api.ErrPermissionDenied
	}
	for _, scope := range []struct {
		isRequested bool
		service     models.Service
		permission  models.Permission
	}{{arguments.HasCalendarAccess, models.ServiceCalendar, models.PermissionCalendarUse}, {arguments.HasRemindersAccess, models.ServiceReminders, models.PermissionCalendarUse}, {arguments.HasContactsAccess, models.ServiceContacts, models.PermissionContactsUse}, {hasPhotosAccess, models.ServicePhotos, models.PermissionPhotosUse}} {
		if !scope.isRequested {
			continue
		}
		if !principal.Permissions.Has(scope.permission) {
			return nil, api.ErrNotFound
		}
		if err := self.requireService(scope.service); err != nil {
			return nil, err
		}
	}
	selector := security.GenerateRandomString(12, appPasswordAlphabet)
	password := "sync-" + selector + "-" + security.GenerateRandomString(24, appPasswordAlphabet)
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	credential, err := self.transaction(ctx).CreateSyncCredential(&models.SyncCredential{UserID: user.ID, CredentialName: strings.TrimSpace(arguments.CredentialName), Selector: selector, PasswordHash: string(hash), HasCalendarAccess: arguments.HasCalendarAccess, HasRemindersAccess: arguments.HasRemindersAccess, HasContactsAccess: arguments.HasContactsAccess, HasPhotosAccess: hasPhotosAccess})
	if err != nil {
		return nil, translateError(err)
	}
	return &CreatedSyncCredential{Credential: credential, Username: user.Username, Password: password, PrincipalPath: dav.Prefix + "/" + user.ID + "/"}, nil
}
func (self *graph) CreateSyncSetupProfile(ctx context.Context, arguments CreateSyncCredentialArguments) (*CreatedSyncProfile, error) {
	if _, err := self.requireAccount(ctx); err != nil {
		return nil, err
	}
	request := api.ContextRequest(ctx)
	if request == nil || !api.IsSecure(request, self.config.Current().Server.TrustedProxies) {
		return nil, fmt.Errorf("%w: open the dashboard over HTTPS to set up device sync", api.ErrInvalidArguments)
	}
	origin, err := self.setupProfileOrigin(request, false)
	if err != nil {
		return nil, err
	}
	address, err := url.Parse(origin)
	if err != nil || address.Scheme != "https" {
		return nil, api.ErrInvalidArguments
	}
	box, err := self.setupProfileBox()
	if err != nil {
		return nil, err
	}
	var profile *CreatedSyncProfile
	err = self.transaction(ctx).TransactionContext(ctx, func(transaction db.Transaction) error {
		created, err := self.CreateSyncCredential(api.ContextWithTransaction(ctx, transaction), arguments)
		if err != nil {
			return err
		}
		configuration := &mailbox.SetupProfile{IsSyncOnly: true, DeviceName: arguments.CredentialName, Address: created.Username, Password: created.Password, DAVHost: address.Host, PrincipalPath: created.PrincipalPath, Identifier: "com.teanode.sync." + created.Credential.ID, IsCalendarEnabled: new(arguments.HasCalendarAccess || arguments.HasRemindersAccess), IsContactsEnabled: new(arguments.HasContactsAccess)}
		written, err := configuration.Plist()
		if err != nil {
			return err
		}
		sealed, err := box.Seal(written)
		if err != nil {
			return err
		}
		token, err := self.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
		if err != nil {
			return err
		}
		link := origin + strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1)
		drawn, err := qrcode.SVG(link)
		if err != nil {
			return err
		}
		profile = &CreatedSyncProfile{DownloadLink: link, QRCodeSVG: drawn, ExpiresAt: time.Now().Add(ceremony.Lifetime), Credential: created.Credential}
		return nil
	})
	return profile, err
}
