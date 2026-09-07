package db

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// IdentityOperation is the user_identity table: who a person is at an
// identity provider.
type IdentityOperation interface {
	GetIdentity(provider, subject string) (*models.UserIdentity, error)
	ListIdentities(userId string) ([]*models.UserIdentity, error)
	CreateIdentity(identity *models.UserIdentity) (*models.UserIdentity, error)
	TouchIdentity(identityId string, email string, at time.Time) error
	DeleteIdentity(identityId string) error
}

type userIdentityModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UserID     string    `gorm:"column:user_id"`
	Provider   string    `gorm:"column:provider"`
	Subject    string    `gorm:"column:subject"`
	Email      string    `gorm:"column:email"`
	LastSeenAt time.Time `gorm:"column:last_seen_at"`
}

func (userIdentityModel) TableName() string { return "user_identity" }

func identityFromModel(model *userIdentityModel) *models.UserIdentity {
	return &models.UserIdentity{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		UserID:     model.UserID,
		Provider:   model.Provider,
		Subject:    model.Subject,
		Email:      model.Email,
		LastSeenAt: model.LastSeenAt.In(time.Local),
	}
}

func (self *transaction) GetIdentity(provider, subject string) (*models.UserIdentity, error) {
	var model userIdentityModel
	result := self.tx.Where("\"provider\" = ? AND \"subject\" = ?", provider, subject).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return identityFromModel(&model), nil
}

func (self *transaction) ListIdentities(userId string) ([]*models.UserIdentity, error) {
	var rows []userIdentityModel
	if err := self.tx.Where("\"user_id\" = ?", userId).Order("\"created_at\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	identities := make([]*models.UserIdentity, 0, len(rows))
	for index := range rows {
		identities = append(identities, identityFromModel(&rows[index]))
	}
	return identities, nil
}

func (self *transaction) CreateIdentity(identity *models.UserIdentity) (*models.UserIdentity, error) {
	now := time.Now()
	model := &userIdentityModel{
		ID:         newID(),
		CreatedAt:  now,
		UserID:     identity.UserID,
		Provider:   identity.Provider,
		Subject:    identity.Subject,
		Email:      identity.Email,
		LastSeenAt: now,
	}
	if err := self.tx.Create(model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return identityFromModel(model), nil
}

func (self *transaction) TouchIdentity(identityId string, email string, at time.Time) error {
	return self.tx.Model(&userIdentityModel{}).Where("\"id\" = ?", identityId).Updates(map[string]any{"email": email, "last_seen_at": at}).Error
}

func (self *transaction) DeleteIdentity(identityId string) error {
	return self.tx.Where("\"id\" = ?", identityId).Delete(&userIdentityModel{}).Error
}
