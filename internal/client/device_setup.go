package client

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

const DocumentDeviceSetupCapabilities = `query {
  GetDeviceSetupCapabilities {
    canSetUpMail canSetUpCalendar canSetUpReminders canSetUpContacts
    mailboxes { id name addresses { address } }
  }
}`

const DocumentCreateDeviceSetupProfile = `mutation (
  $deviceName: String!, $mailboxId: String, $hasMailAccess: Boolean!,
  $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!,
  $hasContactsAccess: Boolean!, $isDownloadOnly: Boolean
) {
  CreateDeviceSetupProfile(
    deviceName: $deviceName, mailboxId: $mailboxId, hasMailAccess: $hasMailAccess,
    hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess,
    hasContactsAccess: $hasContactsAccess, isDownloadOnly: $isDownloadOnly
  ) {
    downloadPath downloadLink qrCodeSvg expiresAt
    mailboxAppPassword { id name createdAt }
    syncCredential { id credentialName createdAt hasCalendarAccess hasRemindersAccess hasContactsAccess }
  }
}`

// DeviceSetupCapabilities lists the account's available services and mailboxes.
type DeviceSetupCapabilities struct {
	CanSetUpMail      bool       `json:"canSetUpMail"`
	CanSetUpCalendar  bool       `json:"canSetUpCalendar"`
	CanSetUpReminders bool       `json:"canSetUpReminders"`
	CanSetUpContacts  bool       `json:"canSetUpContacts"`
	Mailboxes         []*Mailbox `json:"mailboxes"`
}

// CreatedDeviceSetupProfile holds the one-use download and separate revocation metadata.
type CreatedDeviceSetupProfile struct {
	DownloadPath       string                 `json:"downloadPath"`
	DownloadLink       string                 `json:"downloadLink"`
	QRCodeSVG          string                 `json:"qrCodeSvg"`
	ExpiresAt          time.Time              `json:"expiresAt"`
	MailboxAppPassword *MailboxAppPassword    `json:"mailboxAppPassword"`
	SyncCredential     *models.SyncCredential `json:"syncCredential"`
}

func (self *Client) DeviceSetupCapabilities(ctx context.Context) (*DeviceSetupCapabilities, error) {
	var response struct {
		GetDeviceSetupCapabilities *DeviceSetupCapabilities `json:"GetDeviceSetupCapabilities"`
	}
	if err := self.Execute(ctx, DocumentDeviceSetupCapabilities, nil, &response); err != nil {
		return nil, err
	}
	return response.GetDeviceSetupCapabilities, nil
}

func (self *Client) CreateDeviceSetupProfile(ctx context.Context, selectedServices map[string]any) (*CreatedDeviceSetupProfile, error) {
	var response struct {
		CreateDeviceSetupProfile *CreatedDeviceSetupProfile `json:"CreateDeviceSetupProfile"`
	}
	if err := self.Execute(ctx, DocumentCreateDeviceSetupProfile, selectedServices, &response); err != nil {
		return nil, err
	}
	return response.CreateDeviceSetupProfile, nil
}
