package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AgentProviderSignIn is a model provider's sign-in waiting for its code to be
// entered: what the instance that began it was given, so that the instance the
// dashboard's next request reaches can wait on it too.
type AgentProviderSignIn struct {
	ID                  string
	ProviderName        string
	UserCode            string
	VerificationAddress string

	// DeviceAuthID is what the service asks for again when it is polled.
	// Sealed with the server secret in the table.
	DeviceAuthID string

	PollInterval time.Duration
	ExpiresAt    time.Time
}

// AgentProviderAccessToken is the access token a provider's sign-in was last
// traded for, and the refresh token it was traded with, so that an instance
// that finds its own access token run out can use the one another instance
// fetched instead of rotating the refresh token again.
type AgentProviderAccessToken struct {
	ProviderName string

	// AccessToken is sealed with the server secret in the table.
	AccessToken string

	// ForRefreshToken is the refresh token held when the access token was
	// fetched. The access token is only usable by an instance whose refresh
	// token is this one: after a new sign-in it may belong to another account.
	ForRefreshToken string
	ExpiresAt       time.Time
}

// AgentProviderSignInOperation is the model provider sign-ins waiting for
// their code (agent_provider_sign_in), and the access tokens signed-in
// providers were last given (agent_provider_access_token).
type AgentProviderSignInOperation interface {
	// CreateAgentProviderSignIn stores a sign-in that waits for its code, and
	// removes the ones that expired before now on the way.
	CreateAgentProviderSignIn(signIn *AgentProviderSignIn, now time.Time) error

	// GetAgentProviderSignIn returns a sign-in by its id, expired or not.
	// ErrNotFound when there is none.
	GetAgentProviderSignIn(signInID string) (*AgentProviderSignIn, error)

	// DeleteAgentProviderSignIn removes a sign-in, finished or expired. Not an
	// error when there is none.
	DeleteAgentProviderSignIn(signInID string) error

	// PutAgentProviderAccessToken stores the access token of a provider,
	// replacing the one before it.
	PutAgentProviderAccessToken(accessToken *AgentProviderAccessToken) error

	// GetAgentProviderAccessToken returns the access token stored for a
	// provider. ErrNotFound when there is none.
	GetAgentProviderAccessToken(providerName string) (*AgentProviderAccessToken, error)
}

type agentProviderSignInModel struct {
	ID                  string    `gorm:"column:id;primaryKey"`
	ProviderName        string    `gorm:"column:provider_name"`
	UserCode            string    `gorm:"column:user_code"`
	VerificationAddress string    `gorm:"column:verification_address"`
	SealedDeviceAuthID  string    `gorm:"column:sealed_device_auth_id"`
	PollIntervalMS      int64     `gorm:"column:poll_interval_ms"`
	ExpiresAt           time.Time `gorm:"column:expires_at"`
}

func (agentProviderSignInModel) TableName() string { return "agent_provider_sign_in" }

type agentProviderAccessTokenModel struct {
	ProviderName      string    `gorm:"column:provider_name;primaryKey"`
	SealedAccessToken string    `gorm:"column:sealed_access_token"`
	ExpiresAt         time.Time `gorm:"column:expires_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (agentProviderAccessTokenModel) TableName() string { return "agent_provider_access_token" }

// sealedAccessToken is what the access token's row seals: both halves
// together, so that neither can be read apart from the other.
type sealedAccessToken struct {
	AccessToken     string `json:"accessToken"`
	ForRefreshToken string `json:"forRefreshToken"`
}

func (self *transaction) CreateAgentProviderSignIn(signIn *AgentProviderSignIn, now time.Time) error {
	if err := self.tx.Where(`"expires_at" < ?`, now).Delete(&agentProviderSignInModel{}).Error; err != nil {
		return err
	}
	sealer := self.database.currentSealer()
	sealedDeviceAuthID, err := sealer.seal(sealerKeyBox(sealer), signIn.DeviceAuthID)
	if err != nil {
		return fmt.Errorf("db: cannot encrypt the code of a sign-in: %w", err)
	}
	return self.tx.Create(&agentProviderSignInModel{
		ID: signIn.ID, ProviderName: signIn.ProviderName, UserCode: signIn.UserCode,
		VerificationAddress: signIn.VerificationAddress, SealedDeviceAuthID: sealedDeviceAuthID,
		PollIntervalMS: signIn.PollInterval.Milliseconds(), ExpiresAt: signIn.ExpiresAt,
	}).Error
}

func (self *transaction) GetAgentProviderSignIn(signInID string) (*AgentProviderSignIn, error) {
	var model agentProviderSignInModel
	err := self.tx.Where(`"id" = ?`, signInID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sealer := self.database.currentSealer()
	deviceAuthID, err := sealer.open(sealerKeyBox(sealer), model.SealedDeviceAuthID, "code of a sign-in")
	if err != nil {
		return nil, err
	}
	return &AgentProviderSignIn{
		ID: model.ID, ProviderName: model.ProviderName, UserCode: model.UserCode,
		VerificationAddress: model.VerificationAddress, DeviceAuthID: deviceAuthID,
		PollInterval: time.Duration(model.PollIntervalMS) * time.Millisecond, ExpiresAt: model.ExpiresAt.In(time.Local),
	}, nil
}

func (self *transaction) DeleteAgentProviderSignIn(signInID string) error {
	return self.tx.Where(`"id" = ?`, signInID).Delete(&agentProviderSignInModel{}).Error
}

func (self *transaction) PutAgentProviderAccessToken(accessToken *AgentProviderAccessToken) error {
	encoded, err := json.Marshal(&sealedAccessToken{AccessToken: accessToken.AccessToken, ForRefreshToken: accessToken.ForRefreshToken})
	if err != nil {
		return err
	}
	sealer := self.database.currentSealer()
	sealed, err := sealer.seal(sealerKeyBox(sealer), string(encoded))
	if err != nil {
		return fmt.Errorf("db: cannot encrypt the access token of provider %q: %w", accessToken.ProviderName, err)
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"sealed_access_token", "expires_at", "updated_at"}),
	}).Create(&agentProviderAccessTokenModel{
		ProviderName: accessToken.ProviderName, SealedAccessToken: sealed, ExpiresAt: accessToken.ExpiresAt, UpdatedAt: time.Now(),
	}).Error
}

func (self *transaction) GetAgentProviderAccessToken(providerName string) (*AgentProviderAccessToken, error) {
	var model agentProviderAccessTokenModel
	err := self.tx.Where(`"provider_name" = ?`, providerName).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sealer := self.database.currentSealer()
	opened, err := sealer.open(sealerKeyBox(sealer), model.SealedAccessToken, fmt.Sprintf("access token of provider %q", providerName))
	if err != nil {
		return nil, err
	}
	var held sealedAccessToken
	if err := json.Unmarshal([]byte(opened), &held); err != nil {
		return nil, fmt.Errorf("db: cannot read the access token of provider %q: %w", providerName, err)
	}
	return &AgentProviderAccessToken{
		ProviderName: providerName, AccessToken: held.AccessToken, ForRefreshToken: held.ForRefreshToken,
		ExpiresAt: model.ExpiresAt.In(time.Local),
	}, nil
}
