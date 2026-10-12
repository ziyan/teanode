package db

import (
	"errors"
	"github.com/ziyan/teanode/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

// SyncOperation manages account-owned collection credentials.
type SyncOperation interface {
	ListSyncCredentials(userId string) ([]*models.SyncCredential, error)
	GetSyncCredential(selector string) (*models.SyncCredential, error)
	CreateSyncCredential(credential *models.SyncCredential) (*models.SyncCredential, error)
	DeleteSyncCredential(userId, credentialId string) error
	TouchSyncCredential(credentialId string, at time.Time) error
}

type syncCredentialModel models.SyncCredential

func (syncCredentialModel) TableName() string { return "sync_credential" }

func (self *transaction) ListSyncCredentials(userId string) ([]*models.SyncCredential, error) {
	var rows []syncCredentialModel
	if err := self.tx.Where("user_id = ?", userId).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	credentials := make([]*models.SyncCredential, 0, len(rows))
	for _, row := range rows {
		credential := models.SyncCredential(row)
		credentials = append(credentials, &credential)
	}
	return credentials, nil
}
func (self *transaction) GetSyncCredential(selector string) (*models.SyncCredential, error) {
	var row syncCredentialModel
	err := self.tx.Where("selector = ?", selector).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	credential := models.SyncCredential(row)
	return &credential, nil
}
func (self *transaction) CreateSyncCredential(credential *models.SyncCredential) (*models.SyncCredential, error) {
	if credential == nil || credential.UserID == "" || strings.TrimSpace(credential.CredentialName) == "" || len(credential.CredentialName) > 128 || len(credential.Selector) < 12 || len(credential.Selector) > 32 || (!credential.HasCalendarAccess && !credential.HasRemindersAccess && !credential.HasContactsAccess) {
		return nil, ErrInvalidArguments
	}
	if _, err := bcrypt.Cost([]byte(credential.PasswordHash)); err != nil {
		return nil, ErrInvalidArguments
	}

	var user userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", credential.UserID).Take(&user).Error; err != nil {
		return nil, err
	}
	var credentialCount int64
	if err := self.tx.Model(&syncCredentialModel{}).Where("user_id = ?", credential.UserID).Count(&credentialCount).Error; err != nil {
		return nil, err
	}
	if credentialCount >= 64 {
		return nil, ErrInvalidArguments
	}
	created := *credential
	created.ID = newID()
	created.CreatedAt = time.Now()
	row := syncCredentialModel(created)
	if err := self.applyMutation(models.AuditResourceSyncCredential, created.ID, models.AuditActionCreate, nil, &created, func(transaction *gorm.DB) error { return transaction.Create(&row).Error }); err != nil {
		return nil, err
	}
	return &created, nil
}
func (self *transaction) DeleteSyncCredential(userId, credentialId string) error {
	var row syncCredentialModel
	if err := self.tx.Where("id = ? AND user_id = ?", credentialId, userId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	before := models.SyncCredential(row)
	return self.applyMutation(models.AuditResourceSyncCredential, credentialId, models.AuditActionDelete, &before, nil, func(transaction *gorm.DB) error {
		return transaction.Where("id = ? AND user_id = ?", credentialId, userId).Delete(&row).Error
	})
}
func (self *transaction) TouchSyncCredential(credentialId string, at time.Time) error {
	return self.tx.Model(&syncCredentialModel{}).Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", credentialId, at.Add(-TouchInterval)).Update("last_used_at", at).Error
}
