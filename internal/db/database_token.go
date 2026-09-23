package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// TokenOperation is the same for API tokens.
type TokenOperation interface {
	CreateToken(token *models.Token, keyHash string) (*models.Token, error)

	// CreateAuthorizedToken mints a token a program holds, with the hash of
	// the secret it refreshes with.
	CreateAuthorizedToken(token *models.Token, keyHash, refreshHash string) (*models.Token, error)

	GetToken(tokenId string) (*models.Token, string, error)

	// GetTokenRefresh is the hash a refresh secret is checked against.
	GetTokenRefresh(tokenId string) (*models.Token, string, error)
	ListTokens(userId string, options *SessionOptions) ([]*models.Token, error)
	TouchToken(tokenId string, at time.Time, ip, userAgent string) error
	RevokeToken(tokenId string, at time.Time) error

	// UpdateToken renames a token or moves when it expires, leaving the
	// secret alone, and is the token as it now stands.
	UpdateToken(tokenId string, change *TokenChange, at time.Time) (*models.Token, error)

	// RetireToken revokes a token and says whether this call was the one that
	// did it, so that two callers racing to retire the same token cannot both
	// go on as though they had.
	RetireToken(tokenId string, at time.Time) (bool, error)
	RevokeTokensByUser(userId string, at time.Time) (int64, error)

	// RenameClientTokens renames every token one app holds for a person.
	// A renewal carries the name over, so the name lasts.
	RenameClientTokens(userId, clientId, name string, at time.Time) (int64, error)

	// RevokeClientTokens ends every token one app holds for a person, which
	// is disconnecting it: with no token left it has nothing to renew.
	RevokeClientTokens(userId, clientId string, at time.Time) (int64, error)

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

	ClientID    *string `gorm:"column:client_id;size:32"`
	Resource    string  `gorm:"type:text"`
	RefreshHash *string `gorm:"column:refresh_hash;size:64"`
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
		ClientID:   stringOrEmpty(model.ClientID),
		Resource:   model.Resource,
	}
}

// stringOrEmpty is the nullable columns read back. Null and empty mean the
// same thing here -- no client, no resource -- and the column is nullable
// only so that every token issued before this existed keeps its meaning.
func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// stringOrNil is the reverse, so that "no client" is stored as null rather
// than as an empty string that a foreign key would have to explain.
func stringOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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

func (self *database) CreateAuthorizedToken(token *models.Token, keyHash, refreshHash string) (*models.Token, error) {
	now := time.Now()
	model := &tokenModel{
		ID:          token.ID,
		CreatedAt:   now,
		ModifiedAt:  now,
		UserID:      token.UserID,
		Name:        token.Name,
		KeyHash:     keyHash,
		ExpiresAt:   timeOrNil(token.ExpiresAt),
		ClientID:    stringOrNil(token.ClientID),
		Resource:    token.Resource,
		RefreshHash: stringOrNil(refreshHash),
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return tokenFromModel(model), nil
}

func (self *database) GetTokenRefresh(tokenId string) (*models.Token, string, error) {
	var model tokenModel
	if err := self.db.First(&model, "\"id\" = ?", tokenId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", nil
		}
		return nil, "", err
	}
	return tokenFromModel(&model), stringOrEmpty(model.RefreshHash), nil
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

// TokenChange is what UpdateToken changes: a name when Name is set, and
// the expiry when ShouldSetExpiry is, to ExpiresAt, or to never where that
// is zero.
type TokenChange struct {
	Name            *string
	ShouldSetExpiry bool
	ExpiresAt       time.Time
}

func (self *database) UpdateToken(tokenId string, change *TokenChange, at time.Time) (*models.Token, error) {
	updates := map[string]any{"modified_at": at.UTC()}
	if change.Name != nil {
		updates["name"] = *change.Name
	}
	if change.ShouldSetExpiry {
		if change.ExpiresAt.IsZero() {
			updates["expires_at"] = nil
		} else {
			updates["expires_at"] = change.ExpiresAt.UTC()
		}
	}
	if err := self.db.Model(&tokenModel{}).Where("\"id\" = ?", tokenId).Updates(updates).Error; err != nil {
		return nil, err
	}
	token, _, err := self.GetToken(tokenId)
	return token, err
}

func (self *database) RetireToken(tokenId string, at time.Time) (bool, error) {
	result := self.db.Model(&tokenModel{}).
		Where("\"id\" = ? AND \"revoked_at\" IS NULL", tokenId).
		Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()})
	return result.RowsAffected == 1, result.Error
}

func (self *database) RevokeTokensByUser(userId string, at time.Time) (int64, error) {
	result := self.db.Model(&tokenModel{}).
		Where("\"user_id\" = ? AND \"revoked_at\" IS NULL", userId).
		Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()})
	return result.RowsAffected, result.Error
}

func (self *database) RenameClientTokens(userId, clientId, name string, at time.Time) (int64, error) {
	result := self.db.Model(&tokenModel{}).
		Where("\"user_id\" = ? AND \"client_id\" = ? AND \"revoked_at\" IS NULL", userId, clientId).
		Updates(map[string]any{"name": name, "modified_at": at.UTC()})
	return result.RowsAffected, result.Error
}

func (self *database) RevokeClientTokens(userId, clientId string, at time.Time) (int64, error) {
	result := self.db.Model(&tokenModel{}).
		Where("\"user_id\" = ? AND \"client_id\" = ? AND \"revoked_at\" IS NULL", userId, clientId).
		Updates(map[string]any{"revoked_at": at.UTC(), "modified_at": at.UTC()})
	return result.RowsAffected, result.Error
}

func (self *database) ScavengeTokens(now time.Time) (int64, error) {
	// A token a program can renew is kept through its refresh window, however
	// long ago its access expired: deleting it at the usual day would make the
	// window a day long.
	result := self.db.Where(
		"(\"expires_at\" IS NOT NULL AND \"refresh_hash\" IS NULL AND \"expires_at\" < ?)"+
			" OR (\"expires_at\" IS NOT NULL AND \"refresh_hash\" IS NOT NULL AND \"expires_at\" < ?)"+
			" OR (\"revoked_at\" IS NOT NULL AND \"revoked_at\" < ?)",
		now.Add(-expiredRetention), now.Add(-models.RefreshWindow), now.Add(-revokedRetention),
	).Delete(&tokenModel{})
	return result.RowsAffected, result.Error
}
