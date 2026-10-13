package apigraph

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/qrcode"
)

// DeviceSetupQuery offers only services the account can configure on a device.
type DeviceSetupQuery interface {
	GetDeviceSetupCapabilities(context.Context) (*DeviceSetupCapabilities, error)
}

// DeviceSetupMutation composes selected services using their existing credentials.
type DeviceSetupMutation interface {
	CreateDeviceSetupProfile(context.Context, CreateDeviceSetupProfileArguments) (*CreatedDeviceSetupProfile, error)
}

type DeviceSetupCapabilities struct {
	CanSetUpMail      bool              `json:"canSetUpMail"`
	CanSetUpCalendar  bool              `json:"canSetUpCalendar"`
	CanSetUpReminders bool              `json:"canSetUpReminders"`
	CanSetUpContacts  bool              `json:"canSetUpContacts"`
	Mailboxes         []*models.Mailbox `json:"mailboxes"`
}

type CreateDeviceSetupProfileArguments struct {
	DeviceName         string  `json:"deviceName"`
	MailboxID          *string `json:"mailboxId" graphapi:"nullable"`
	HasMailAccess      bool    `json:"hasMailAccess"`
	HasCalendarAccess  bool    `json:"hasCalendarAccess"`
	HasRemindersAccess bool    `json:"hasRemindersAccess"`
	HasContactsAccess  bool    `json:"hasContactsAccess"`
	IsDownloadOnly     *bool   `json:"isDownloadOnly" graphapi:"nullable"`
}

// CreatedDeviceSetupProfile exposes revocation metadata, never credential secrets.
type CreatedDeviceSetupProfile struct {
	DownloadPath       string                     `json:"downloadPath"`
	DownloadLink       string                     `json:"downloadLink"`
	QRCodeSVG          string                     `json:"qrCodeSvg"`
	ExpiresAt          time.Time                  `json:"expiresAt"`
	MailboxAppPassword *models.MailboxAppPassword `json:"mailboxAppPassword" graphapi:"nullable"`
	SyncCredential     *models.SyncCredential     `json:"syncCredential" graphapi:"nullable"`
}

func (self *graph) GetDeviceSetupCapabilities(ctx context.Context) (*DeviceSetupCapabilities, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	principal := api.ContextPrincipal(ctx)
	capabilities := &DeviceSetupCapabilities{Mailboxes: []*models.Mailbox{}}
	if !principal.IsImpersonation() {
		capabilities.CanSetUpCalendar = self.isServiceEnabled(models.ServiceCalendar) && principal.Permissions.Has(models.PermissionCalendarUse)
		capabilities.CanSetUpReminders = self.isServiceEnabled(models.ServiceReminders) && principal.Permissions.Has(models.PermissionCalendarUse)
		capabilities.CanSetUpContacts = self.isServiceEnabled(models.ServiceContacts) && principal.Permissions.Has(models.PermissionContactsUse)
	}
	if !self.isServiceEnabled(models.ServiceMailboxes) || !principal.Permissions.Has(models.PermissionMailboxManage) {
		return capabilities, nil
	}
	settings, err := self.GetMailProgramSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.IMAPHost == "" || settings.SubmissionHost == "" || (settings.IMAPPort == 0 && settings.IMAPSPort == 0) {
		return capabilities, nil
	}
	mailboxes, err := self.transaction(ctx).ListMailboxes(user.ID)
	if err != nil {
		return nil, err
	}
	for _, ownedMailbox := range mailboxes {
		if len(ownedMailbox.Addresses) > 0 {
			capabilities.Mailboxes = append(capabilities.Mailboxes, ownedMailbox)
		}
	}
	capabilities.CanSetUpMail = len(capabilities.Mailboxes) > 0
	return capabilities, nil
}

func (self *graph) CreateDeviceSetupProfile(ctx context.Context, arguments CreateDeviceSetupProfileArguments) (*CreatedDeviceSetupProfile, error) {
	_, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(arguments.DeviceName)
	hasCollectionAccess := arguments.HasCalendarAccess || arguments.HasRemindersAccess || arguments.HasContactsAccess
	if deviceName == "" || len(deviceName) > 128 || (!arguments.HasMailAccess && !hasCollectionAccess) {
		return nil, fmt.Errorf("%w: give the device a name and select at least one service", api.ErrInvalidArguments)
	}
	if !arguments.HasMailAccess && arguments.MailboxID != nil && *arguments.MailboxID != "" {
		return nil, fmt.Errorf("%w: select Mail to choose a mailbox", api.ErrInvalidArguments)
	}
	request := api.ContextRequest(ctx)
	if request == nil || (!api.IsSecure(request, self.config.Current().Server.TrustedProxies) && (hasCollectionAccess || !api.IsLocalRequest(request))) {
		return nil, fmt.Errorf("%w: open the dashboard over HTTPS to set up this device", api.ErrInvalidArguments)
	}
	origin, err := self.setupProfileOrigin(request, arguments.IsDownloadOnly != nil && *arguments.IsDownloadOnly)
	if err != nil {
		return nil, err
	}
	address, err := url.Parse(origin)
	if err != nil || (hasCollectionAccess && address.Scheme != "https") {
		return nil, api.ErrInvalidArguments
	}
	capabilities, err := self.GetDeviceSetupCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	principal := api.ContextPrincipal(ctx)
	if hasCollectionAccess && principal.IsImpersonation() {
		return nil, api.ErrPermissionDenied
	}
	for _, scope := range []struct {
		isRequested bool
		service     models.Service
		permission  models.Permission
	}{{arguments.HasCalendarAccess, models.ServiceCalendar, models.PermissionCalendarUse}, {arguments.HasRemindersAccess, models.ServiceReminders, models.PermissionCalendarUse}, {arguments.HasContactsAccess, models.ServiceContacts, models.PermissionContactsUse}} {
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
	configuration := &mailbox.SetupProfile{DeviceName: deviceName, IsSyncOnly: !arguments.HasMailAccess, IsCalendarEnabled: new(arguments.HasCalendarAccess || arguments.HasRemindersAccess), IsContactsEnabled: new(arguments.HasContactsAccess)}
	var ownedMailbox *models.Mailbox
	if arguments.HasMailAccess {
		if arguments.MailboxID == nil || *arguments.MailboxID == "" {
			return nil, fmt.Errorf("%w: choose a mailbox for Mail", api.ErrInvalidArguments)
		}
		ownedMailbox, err = self.requireMailbox(ctx, models.PermissionMailboxManage, *arguments.MailboxID)
		if err != nil {
			return nil, err
		}
		if !capabilities.CanSetUpMail || len(ownedMailbox.Addresses) == 0 {
			return nil, fmt.Errorf("%w: this mailbox has no available mail setup", api.ErrInvalidArguments)
		}
		settings, err := self.GetMailProgramSettings(ctx)
		if err != nil {
			return nil, err
		}
		configuration.MailboxName, configuration.IMAPHost, configuration.SubmissionHost = ownedMailbox.Name, settings.IMAPHost, settings.SubmissionHost
		configuration.IMAPPort, configuration.SubmissionPort = settings.IMAPSPort, settings.SubmissionPort
		if configuration.IMAPPort == 0 {
			configuration.IMAPPort = settings.IMAPPort
		}
	}
	box, err := self.setupProfileBox()
	if err != nil {
		return nil, err
	}
	var profile *CreatedDeviceSetupProfile
	err = self.transaction(ctx).TransactionContext(ctx, func(transaction db.Transaction) error {
		nestedContext := api.ContextWithTransaction(ctx, transaction)
		profile = &CreatedDeviceSetupProfile{}
		if ownedMailbox != nil {
			created, err := self.CreateMailboxAppPassword(nestedContext, CreateMailboxAppPasswordArguments{MailboxID: ownedMailbox.ID, Name: deviceName})
			if err != nil {
				return err
			}
			profile.MailboxAppPassword = created.AppPassword
			configuration.Address, configuration.Password = created.Username, created.Password
			configuration.Identifier = "com.teanode.device." + created.AppPassword.ID
		}
		if hasCollectionAccess {
			created, err := self.CreateSyncCredential(nestedContext, CreateSyncCredentialArguments{CredentialName: deviceName, HasCalendarAccess: arguments.HasCalendarAccess, HasRemindersAccess: arguments.HasRemindersAccess, HasContactsAccess: arguments.HasContactsAccess})
			if err != nil {
				return err
			}
			profile.SyncCredential = created.Credential
			configuration.DAVHost, configuration.PrincipalPath = address.Host, created.PrincipalPath
			configuration.DAVUsername, configuration.DAVPassword = created.Username, created.Password
			if configuration.IsSyncOnly {
				configuration.Address, configuration.Password = created.Username, created.Password
				configuration.Identifier = "com.teanode.device." + created.Credential.ID
			}
		}
		written, err := configuration.Plist()
		if err != nil {
			return err
		}
		sealed, err := box.Seal(written)
		if err != nil {
			return err
		}
		profile.ExpiresAt = time.Now().Add(ceremony.Lifetime)
		token, err := self.ceremonies.Park(ctx, &ceremony.Ceremony{Kind: ceremony.KindSetupProfile, SealedSetupProfile: sealed})
		if err != nil {
			return err
		}
		profile.DownloadPath = strings.Replace(api.PathMailboxSetupProfile, "{token}", token, 1)
		profile.DownloadLink = origin + profile.DownloadPath
		profile.QRCodeSVG, err = qrcode.SVG(profile.DownloadLink)
		return err
	})
	if err != nil {
		return nil, err
	}
	return profile, nil
}
