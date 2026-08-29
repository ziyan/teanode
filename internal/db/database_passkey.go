package db

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// PasskeyOperation is what the WebAuthn resolvers need of the passkey table.
//
// Narrow, like the session and token operations, and for the same reason:
// what is stored here decides who can sign in, so there is no general update
// that would let a caller move a credential to another account.
type PasskeyOperation interface {
	// CreatePasskey stores a newly registered credential.
	CreatePasskey(passkey *models.Passkey) (*models.Passkey, error)

	// GetPasskey returns one by identifier, or nil.
	GetPasskey(passkeyId string) (*models.Passkey, error)

	// GetPasskeyByCredentialID returns the credential an authenticator named,
	// or nil. This is the sign-in lookup: a discoverable credential means the
	// server is never told who is signing in, so the credential identifier is
	// all there is to go on.
	GetPasskeyByCredentialID(credentialId []byte) (*models.Passkey, error)

	// ListPasskeysForUser returns an account's credentials, newest first.
	ListPasskeysForUser(userId string) ([]*models.Passkey, error)

	// RenamePasskey changes what somebody calls an authenticator.
	RenamePasskey(passkeyId, name string) error

	// RecordPasskeyUse stores the counter the authenticator reported and when
	// it was used. The counter is how a cloned credential is noticed, so it
	// has to be written on every successful assertion.
	RecordPasskeyUse(passkeyId string, signCount int64, backupState bool, at time.Time, ip, userAgent string) error

	// DeletePasskey removes one credential.
	DeletePasskey(passkeyId string) error
}

type passkeyModel struct {
	ID string `gorm:"column:id;primaryKey;size:32"`

	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`

	UserID string `gorm:"column:user_id;size:32;index"`
	Name   string `gorm:"column:name;size:128"`

	CredentialID    []byte `gorm:"column:credential_id"`
	PublicKey       []byte `gorm:"column:public_key"`
	AttestationType string `gorm:"column:attestation_type;size:64"`

	// Stored as one comma-separated string rather than an array: there are at
	// most a handful, they are never queried, and an array type would be one
	// more thing for the driver to get right.
	Transports string `gorm:"column:transports;type:text"`

	AAGUID         []byte `gorm:"column:aaguid"`
	SignCount      int64  `gorm:"column:sign_count"`
	BackupEligible bool   `gorm:"column:backup_eligible"`
	BackupState    bool   `gorm:"column:backup_state"`

	UsedAt    *time.Time `gorm:"column:used_at"`
	IP        string     `gorm:"column:ip;size:64"`
	UserAgent string     `gorm:"column:user_agent;type:text"`
}

func (passkeyModel) TableName() string { return "passkey" }

func passkeyFromModel(model *passkeyModel) *models.Passkey {
	return &models.Passkey{
		ID:              model.ID,
		CreatedAt:       model.CreatedAt.In(time.Local),
		ModifiedAt:      model.ModifiedAt.In(time.Local),
		UserID:          model.UserID,
		Name:            model.Name,
		CredentialID:    model.CredentialID,
		PublicKey:       model.PublicKey,
		AttestationType: model.AttestationType,
		Transports:      splitTransports(model.Transports),
		AAGUID:          model.AAGUID,
		SignCount:       model.SignCount,
		BackupEligible:  model.BackupEligible,
		BackupState:     model.BackupState,
		UsedAt:          timeOrZero(model.UsedAt),
		IP:              model.IP,
		UserAgent:       model.UserAgent,
	}
}

func splitTransports(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func (self *database) CreatePasskey(passkey *models.Passkey) (*models.Passkey, error) {
	now := time.Now()
	model := &passkeyModel{
		ID:              passkey.ID,
		CreatedAt:       now,
		ModifiedAt:      now,
		UserID:          passkey.UserID,
		Name:            passkey.Name,
		CredentialID:    passkey.CredentialID,
		PublicKey:       passkey.PublicKey,
		AttestationType: passkey.AttestationType,
		Transports:      strings.Join(passkey.Transports, ","),
		AAGUID:          passkey.AAGUID,
		SignCount:       passkey.SignCount,
		BackupEligible:  passkey.BackupEligible,
		BackupState:     passkey.BackupState,
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return passkeyFromModel(model), nil
}

func (self *database) GetPasskey(passkeyId string) (*models.Passkey, error) {
	var model passkeyModel
	if err := self.db.First(&model, "\"id\" = ?", passkeyId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return passkeyFromModel(&model), nil
}

func (self *database) GetPasskeyByCredentialID(credentialId []byte) (*models.Passkey, error) {
	if len(credentialId) == 0 {
		return nil, nil
	}
	var model passkeyModel
	if err := self.db.First(&model, "\"credential_id\" = ?", credentialId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return passkeyFromModel(&model), nil
}

func (self *database) ListPasskeysForUser(userId string) ([]*models.Passkey, error) {
	var found []passkeyModel
	if err := self.db.Where("\"user_id\" = ?", userId).
		Order("\"created_at\" DESC").Find(&found).Error; err != nil {
		return nil, err
	}
	passkeys := make([]*models.Passkey, 0, len(found))
	for index := range found {
		passkeys = append(passkeys, passkeyFromModel(&found[index]))
	}
	return passkeys, nil
}

func (self *database) RenamePasskey(passkeyId, name string) error {
	return self.db.Model(&passkeyModel{}).
		Where("\"id\" = ?", passkeyId).
		Updates(map[string]any{"name": name, "modified_at": time.Now().UTC()}).Error
}

func (self *database) RecordPasskeyUse(passkeyId string, signCount int64, backupState bool, at time.Time, ip, userAgent string) error {
	return self.db.Model(&passkeyModel{}).
		Where("\"id\" = ?", passkeyId).
		Updates(map[string]any{
			"sign_count":   signCount,
			"backup_state": backupState,
			"used_at":      at.UTC(),
			"ip":           ip,
			"user_agent":   userAgent,
			"modified_at":  at.UTC(),
		}).Error
}

func (self *database) DeletePasskey(passkeyId string) error {
	return self.db.Where("\"id\" = ?", passkeyId).Delete(&passkeyModel{}).Error
}
