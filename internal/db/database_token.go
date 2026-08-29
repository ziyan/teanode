package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// TokenOperation is the same for API tokens.
type TokenOperation interface {
	CreateToken(token *models.Token, keyHash string) (*models.Token, error)
	GetToken(tokenId string) (*models.Token, string, error)
	ListTokens(userId string, options *SessionOptions) ([]*models.Token, error)
	TouchToken(tokenId string, at time.Time, ip, userAgent string) error
	RevokeToken(tokenId string, at time.Time) error
	RevokeTokensByUser(userId string, at time.Time) (int64, error)

	ScavengeTokens(now time.Time) (int64, error)
}

type tokenModel struct {
	ID string `gorm:"primary_key:true;size:32"`

	CreatedAt  time.Time
	ModifiedAt time.Time

	UserID  string `gorm:"column:user_id;size:32;index"`
	Name    string `gorm:"size:256"`
	KeyHash string `gorm:"size:64"`

	ExpiresAt *time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time

	IP        string `gorm:"column:ip;size:64"`
	UserAgent string `gorm:"type:text"`
}

func (self *tokenModel) TableName() string {
	return "token"
}

func tokenFromModel(model *tokenModel) *models.Token {
	return &models.Token{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		ModifiedAt: model.ModifiedAt.In(time.Local),
		UserID:     model.UserID,
		Name:       model.Name,
		ExpiresAt:  timeOrZero(model.ExpiresAt),
		UsedAt:     timeOrZero(model.UsedAt),
		RevokedAt:  timeOrZero(model.RevokedAt),
		IP:         model.IP,
		UserAgent:  model.UserAgent,
	}
}

func (self *database) CreateToken(token *models.Token, keyHash string) (*models.Token, error) {
	now := time.Now()
	model := &tokenModel{
		ID:         token.ID,
		CreatedAt:  now,
		ModifiedAt: now,
		UserID:     token.UserID,
		Name:       token.Name,
		KeyHash:    keyHash,
		ExpiresAt:  timeOrNil(token.ExpiresAt),
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return tokenFromModel(model), nil
}

func (self *database) GetToken(tokenId string) (*models.Token, string, error) {
	var model tokenModel
	if err := self.db.First(&model, "\"id\" = ?", tokenId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", nil
		}
		return nil, "", err
	}
	return tokenFromModel(&model), model.KeyHash, nil
}

func (self *database) ListTokens(userId string, options *SessionOptions) ([]*models.Token, error) {
	query := self.db.Where("\"user_id\" = ?", userId)
	if !options.includeRevoked() {
		query = query.Where("\"revoked_at\" IS NULL")
	}

	var found []tokenModel
	if err := query.Order("\"created_at\" DESC").Limit(options.limit()).Find(&found).Error; err != nil {
		return nil, err
	}

	tokens := make([]*models.Token, 0, len(found))
	for index := range found {
		tokens = append(tokens, tokenFromModel(&found[index]))
	}
	return tokens, nil
}

func (self *database) TouchToken(tokenId string, at time.Time, ip, userAgent string) error {
	return self.db.Model(&tokenModel{}).
		Where("\"id\" = ? AND (\"used_at\" IS NULL OR \"used_at\" <= ?)", tokenId, at.Add(-TouchInterval)).
		Updates(map[string]any{
			"used_at":     at.UTC(),
			"ip":          ip,
			"user_agent":  userAgent,
			"modified_at": at.UTC(),
		}).Error
}

func (self *database) RevokeToken(tokenId string, at time.Time) error {
	return self.db.Model(&tokenModel{}).
		Where("\"id\" = ? AND \"revoked_at\" IS NULL", tokenId).
		Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()}).Error
}

func (self *database) RevokeTokensByUser(userId string, at time.Time) (int64, error) {
	result := self.db.Model(&tokenModel{}).
		Where("\"user_id\" = ? AND \"revoked_at\" IS NULL", userId).
		Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()})
	return result.RowsAffected, result.Error
}

func (self *database) ScavengeTokens(now time.Time) (int64, error) {
	result := self.db.Where(
		"(\"expires_at\" IS NOT NULL AND \"expires_at\" < ?) OR (\"revoked_at\" IS NOT NULL AND \"revoked_at\" < ?)",
		now.Add(-expiredRetention), now.Add(-revokedRetention),
	).Delete(&tokenModel{})
	return result.RowsAffected, result.Error
}
