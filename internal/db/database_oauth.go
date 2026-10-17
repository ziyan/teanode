package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// OAuthOperation is what authorizing a program needs from the database.
type OAuthOperation interface {
	CreateOAuthClient(client *models.OAuthClient) (*models.OAuthClient, error)
	GetOAuthClient(clientId string) (*models.OAuthClient, error)
	TouchOAuthClientApproval(clientId string, at time.Time) error

	CreateOAuthAuthorization(authorization *models.OAuthAuthorization, keyHash string) (*models.OAuthAuthorization, error)

	// SpendOAuthAuthorization takes an unexpired approval and deletes it,
	// returning it and the hash of its secret for the caller to check.
	//
	// Taking and deleting are one statement because they have to be one
	// decision: two programs collecting the same approval at once must not
	// both be told yes, and an approval that is read, checked and then
	// deleted has a gap between the reading and the deleting where exactly
	// that happens. The secret is checked by the caller, the way GetToken
	// hands back a hash rather than comparing one here.
	SpendOAuthAuthorization(authorizationId string, now time.Time) (*models.OAuthAuthorization, string, error)

	CreateOAuthConnectCode(code *models.OAuthConnectCode, codeHash string) (*models.OAuthConnectCode, error)

	// SpendOAuthConnectCode takes an unexpired connect code and deletes it, in
	// one statement, so a code signs in one phone however many present it at
	// once. Nil when there was nothing to spend.
	SpendOAuthConnectCode(codeHash string, now time.Time) (*models.OAuthConnectCode, error)

	ScavengeOAuth(now time.Time) (int64, error)
}

type oauthClientModel struct {
	ID         string `gorm:"primary_key:true;size:32"`
	CreatedAt  time.Time
	ModifiedAt time.Time

	Name         string `gorm:"size:256"`
	RedirectURIs string `gorm:"column:redirect_uris;type:text"`

	ApprovedAt *time.Time
}

func (self *oauthClientModel) TableName() string {
	return "oauth_client"
}

type oauthAuthorizationModel struct {
	ID        string `gorm:"primary_key:true;size:32"`
	CreatedAt time.Time

	ClientID string `gorm:"column:client_id;size:32"`
	UserID   string `gorm:"column:user_id;size:32"`

	RedirectURI   string  `gorm:"column:redirect_uri;type:text"`
	CodeChallenge string  `gorm:"column:code_challenge;size:128"`
	Resource      string  `gorm:"type:text"`
	KeyHash       string  `gorm:"column:key_hash;size:64"`
	ClientKind    *string `gorm:"column:client_kind;size:16"`

	ExpiresAt time.Time
}

func (self *oauthAuthorizationModel) TableName() string {
	return "oauth_authorization"
}

type oauthConnectCodeModel struct {
	ID         string `gorm:"primary_key:true;size:32"`
	CreatedAt  time.Time
	UserID     string  `gorm:"column:user_id;size:32"`
	CodeHash   string  `gorm:"column:code_hash;size:64"`
	ClientKind *string `gorm:"column:client_kind;size:16"`
	ExpiresAt  time.Time
}

func (self *oauthConnectCodeModel) TableName() string {
	return "oauth_connect_code"
}

func oauthConnectCodeFromModel(model *oauthConnectCodeModel) *models.OAuthConnectCode {
	return &models.OAuthConnectCode{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt.In(time.Local),
		UserID:     model.UserID,
		ClientKind: models.TokenClientKind(stringOrEmpty(model.ClientKind)),
		ExpiresAt:  model.ExpiresAt.In(time.Local),
	}
}

// Addresses are kept one per line. They are opaque to SQL, there are rarely
// more than two, and a line is something a person reading the row can see.
func joinRedirects(addresses []string) string {
	joined := ""
	for index, address := range addresses {
		if index > 0 {
			joined += "\n"
		}
		joined += address
	}
	return joined
}

func splitRedirects(joined string) []string {
	if joined == "" {
		return nil
	}
	addresses := []string{}
	start := 0
	for index := 0; index <= len(joined); index++ {
		if index == len(joined) || joined[index] == '\n' {
			if index > start {
				addresses = append(addresses, joined[start:index])
			}
			start = index + 1
		}
	}
	return addresses
}

func oauthClientFromModel(model *oauthClientModel) *models.OAuthClient {
	return &models.OAuthClient{
		ID:           model.ID,
		CreatedAt:    model.CreatedAt.In(time.Local),
		ModifiedAt:   model.ModifiedAt.In(time.Local),
		Name:         model.Name,
		RedirectURIs: splitRedirects(model.RedirectURIs),
		ApprovedAt:   timeOrZero(model.ApprovedAt),
	}
}

func (self *database) CreateOAuthClient(client *models.OAuthClient) (*models.OAuthClient, error) {
	now := time.Now()
	model := &oauthClientModel{
		ID:           client.ID,
		CreatedAt:    now,
		ModifiedAt:   now,
		Name:         client.Name,
		RedirectURIs: joinRedirects(client.RedirectURIs),
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return oauthClientFromModel(model), nil
}

func (self *database) GetOAuthClient(clientId string) (*models.OAuthClient, error) {
	var model oauthClientModel
	if err := self.db.First(&model, "\"id\" = ?", clientId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return oauthClientFromModel(&model), nil
}

func (self *database) TouchOAuthClientApproval(clientId string, at time.Time) error {
	return self.db.Model(&oauthClientModel{}).Where("\"id\" = ?", clientId).
		Updates(map[string]any{"approved_at": at, "modified_at": at}).Error
}

func (self *database) CreateOAuthAuthorization(authorization *models.OAuthAuthorization, keyHash string) (*models.OAuthAuthorization, error) {
	model := &oauthAuthorizationModel{
		ID:            authorization.ID,
		CreatedAt:     time.Now(),
		ClientID:      authorization.ClientID,
		UserID:        authorization.UserID,
		RedirectURI:   authorization.RedirectURI,
		CodeChallenge: authorization.CodeChallenge,
		Resource:      authorization.Resource,
		KeyHash:       keyHash,
		ClientKind:    stringOrNil(string(authorization.ClientKind)),
		ExpiresAt:     authorization.ExpiresAt,
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return &models.OAuthAuthorization{
		ID:            model.ID,
		CreatedAt:     model.CreatedAt.In(time.Local),
		ClientID:      model.ClientID,
		UserID:        model.UserID,
		RedirectURI:   model.RedirectURI,
		CodeChallenge: model.CodeChallenge,
		Resource:      model.Resource,
		ClientKind:    authorization.ClientKind,
		ExpiresAt:     model.ExpiresAt.In(time.Local),
	}, nil
}

func (self *database) SpendOAuthAuthorization(authorizationId string, now time.Time) (*models.OAuthAuthorization, string, error) {
	var model oauthAuthorizationModel
	// One statement: the delete decides, and returns the row only to whoever
	// won it. Reading first and deleting after leaves a gap in which two
	// collections of the same approval both see it present.
	result := self.db.Raw(
		"DELETE FROM \"oauth_authorization\" WHERE \"id\" = ? AND \"expires_at\" > ? RETURNING *",
		authorizationId, now,
	).Scan(&model)
	if result.Error != nil {
		return nil, "", result.Error
	}
	if result.RowsAffected == 0 || model.ID == "" {
		return nil, "", nil
	}
	// The row is gone either way, so a wrong secret spends the approval too.
	// Somebody guessing gets one guess per approval rather than as many as
	// they like.
	return &models.OAuthAuthorization{
		ID:            model.ID,
		CreatedAt:     model.CreatedAt.In(time.Local),
		ClientID:      model.ClientID,
		UserID:        model.UserID,
		RedirectURI:   model.RedirectURI,
		CodeChallenge: model.CodeChallenge,
		Resource:      model.Resource,
		ClientKind:    models.TokenClientKind(stringOrEmpty(model.ClientKind)),
		ExpiresAt:     model.ExpiresAt.In(time.Local),
	}, model.KeyHash, nil
}

func (self *database) CreateOAuthConnectCode(code *models.OAuthConnectCode, codeHash string) (*models.OAuthConnectCode, error) {
	model := &oauthConnectCodeModel{
		ID:         code.ID,
		CreatedAt:  time.Now(),
		UserID:     code.UserID,
		CodeHash:   codeHash,
		ClientKind: stringOrNil(string(code.ClientKind)),
		ExpiresAt:  code.ExpiresAt,
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return oauthConnectCodeFromModel(model), nil
}

func (self *database) SpendOAuthConnectCode(codeHash string, now time.Time) (*models.OAuthConnectCode, error) {
	var model oauthConnectCodeModel
	result := self.db.Raw(
		"DELETE FROM \"oauth_connect_code\" WHERE \"code_hash\" = ? AND \"expires_at\" > ? RETURNING *",
		codeHash, now,
	).Scan(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 || model.ID == "" {
		return nil, nil
	}
	return oauthConnectCodeFromModel(&model), nil
}

// ScavengeOAuth removes approvals nobody collected and registrations nobody
// ever approved.
//
// Registration is open, so this is what stops a table of strangers' names
// growing without bound. A client somebody has approved is kept however old
// it is: it is in service.
func (self *database) ScavengeOAuth(now time.Time) (int64, error) {
	expired := self.db.Delete(&oauthAuthorizationModel{}, "\"expires_at\" < ?", now)
	if expired.Error != nil {
		return 0, expired.Error
	}
	expiredCodes := self.db.Delete(&oauthConnectCodeModel{}, "\"expires_at\" < ?", now)
	if expiredCodes.Error != nil {
		return expired.RowsAffected, expiredCodes.Error
	}
	expired.RowsAffected += expiredCodes.RowsAffected
	unused := self.db.Delete(&oauthClientModel{},
		"\"approved_at\" IS NULL AND \"created_at\" < ?", now.Add(-24*time.Hour))
	if unused.Error != nil {
		return expired.RowsAffected, unused.Error
	}
	return expired.RowsAffected + unused.RowsAffected, nil
}
