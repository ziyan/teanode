package models

import "time"

// SyncCredential authorizes a device to synchronize selected account collections.
type SyncCredential struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"userId"`
	CreatedAt          time.Time  `json:"createdAt"`
	CredentialName     string     `json:"credentialName"`
	Selector           string     `json:"-"`
	PasswordHash       string     `json:"-"`
	HasCalendarAccess  bool       `json:"hasCalendarAccess"`
	HasRemindersAccess bool       `json:"hasRemindersAccess"`
	HasContactsAccess  bool       `json:"hasContactsAccess"`
	HasPhotosAccess    bool       `json:"hasPhotosAccess"`
	LastUsedAt         *time.Time `json:"lastUsedAt,omitempty"`
}

// Allows reports the credential's scope, independently of deployment policy.
func (self *SyncCredential) Allows(service Service) bool {
	switch service {
	case ServiceCalendar:
		return self.HasCalendarAccess
	case ServiceReminders:
		return self.HasRemindersAccess
	case ServiceContacts:
		return self.HasContactsAccess
	case ServicePhotos:
		return self.HasPhotosAccess
	default:
		return false
	}
}

// RedactForAudit never records credential hashes.
func (self *SyncCredential) RedactForAudit() any {
	if self == nil {
		return nil
	}
	redacted := *self
	redacted.PasswordHash = ""
	return &redacted
}
