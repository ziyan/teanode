package apigraph

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/util/security"
)

type CredentialQuery interface {
	// List the Credentials that may send mail as a Domain
	ListCredentials(ctx context.Context, arguments ListCredentialsArguments) ([]*Credential, error)

	// Get the SMTP settings for a Credential, including its password.
	//
	// A separate query rather than fields on Credential, so that showing a
	// list of credentials does not put every password on the wire when
	// nobody asked to see one.
	GetCredentialSettings(ctx context.Context, arguments GetCredentialSettingsArguments) (*CredentialSettings, error)
}

type CredentialMutation interface {
	// Create a Credential and return its username and password. The password
	// is shown only in this reply.
	CreateCredential(ctx context.Context, arguments CreateCredentialArguments) (*CreatedCredential, error)

	// Change a Credential
	UpdateCredential(ctx context.Context, arguments UpdateCredentialArguments) (*Credential, error)

	// Remove a Credential
	DeleteCredential(ctx context.Context, arguments DeleteCredentialArguments) error
}

// CreatedCredential is a newly created Credential together with the SMTP
// settings to enter into a mail client.
type CreatedCredential struct {
	Credential *Credential `json:"credential"`

	// Host to connect to
	Host string `json:"host"`

	// Port for submission, which offers STARTTLS
	Port string `json:"port"`

	// SMTP username
	Username string `json:"username"`

	// SMTP password. Derived from the stored key and the server secret, so it
	// can be shown again, but the dashboard does not offer that by default.
	Password string `json:"password"`
}

// CredentialSettings is what to enter into a mail client.
type CredentialSettings struct {
	// Host to connect to
	Host string `json:"host"`

	// Port for submission, which offers STARTTLS
	Port string `json:"port"`

	// SMTP username
	Username string `json:"username"`

	// SMTP password. Derived from the stored key and the server secret, so
	// it can be shown again rather than only once.
	Password string `json:"password"`
}

type GetCredentialSettingsArguments struct {
	// ID of the Domain the Credential belongs to
	DomainID string `json:"domainId"`

	// ID of the Credential
	CredentialID string `json:"credentialId"`
}

func (self *graph) GetCredentialSettings(ctx context.Context, arguments GetCredentialSettingsArguments) (*CredentialSettings, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	for _, credential := range domain.Credentials {
		if credential == nil || credential.ID != arguments.CredentialID {
			continue
		}
		username, password, err := security.EncodeCredential(credential.ID, credential.Key, self.settings.Secret)
		if err != nil {
			return nil, err
		}
		configuration := self.config.Current()
		return &CredentialSettings{
			Host:     configuration.SubmissionHost(),
			Port:     configuration.SubmissionPort(),
			Username: username,
			Password: password,
		}, nil
	}
	return nil, api.ErrNotFound
}

type ListCredentialsArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`
}

func (self *graph) ListCredentials(ctx context.Context, arguments ListCredentialsArguments) ([]*Credential, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	credentials := make([]*Credential, 0, len(domain.Credentials))
	for _, credential := range domain.Credentials {
		credentials = append(credentials, describeCredential(credential))
	}
	return credentials, nil
}

// CredentialParameters are the settings of a Credential an operator can set.
type CredentialParameters struct {
	// Note naming the device or service that will hold it
	Comment *string `json:"comment"`

	// When set, restricts this Credential to sending as that local part,
	// which limits the damage if it leaks
	Alias *string `json:"alias"`

	// Whether to refuse this Credential without deleting it
	Disabled *bool `json:"disabled"`
}

type CreateCredentialArguments struct {
	// ID of the Domain the Credential may send as
	DomainID string `json:"domainId"`

	CredentialParameters *CredentialParameters `json:"credentialParameters"`
}

func (self *graph) CreateCredential(ctx context.Context, arguments CreateCredentialArguments) (*CreatedCredential, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	created := &config.Credential{
		ID:  config.NewID(),
		Key: generateCredentialKey(),
	}
	if arguments.CredentialParameters != nil {
		applyCredentialParameters(created, arguments.CredentialParameters)
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		target := configuration.FindDomainByID(arguments.DomainID)
		if target == nil {
			return api.ErrNotFound
		}
		target.Credentials = append(target.Credentials, created)
		return nil
	}); err != nil {
		return nil, err
	}

	username, password, err := security.EncodeCredential(created.ID, created.Key, self.settings.Secret)
	if err != nil {
		return nil, err
	}

	configuration := self.config.Current()
	log.Noticef("%s created a credential for %q", operatorName(ctx), domain.Domain)

	return &CreatedCredential{
		Credential: describeCredential(created),
		Host:       configuration.SubmissionHost(),
		Port:       configuration.SubmissionPort(),
		Username:   username,
		Password:   password,
	}, nil
}

type UpdateCredentialArguments struct {
	// ID of the Credential to change
	CredentialID string `json:"credentialId"`

	CredentialParameters *CredentialParameters `json:"credentialParameters"`
}

func (self *graph) UpdateCredential(ctx context.Context, arguments UpdateCredentialArguments) (*Credential, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	if arguments.CredentialParameters == nil {
		return nil, api.ErrInvalidArguments
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		_, credential := configuration.FindCredential(arguments.CredentialID)
		if credential == nil {
			return api.ErrNotFound
		}
		// The key is never changed here: the password derives from it, and
		// rotating it silently would break a client that is still using it.
		applyCredentialParameters(credential, arguments.CredentialParameters)
		return nil
	}); err != nil {
		return nil, err
	}

	_, credential := self.config.Current().FindCredential(arguments.CredentialID)
	log.Noticef("%s changed credential %q", operatorName(ctx), credential.ID)
	return describeCredential(credential), nil
}

type DeleteCredentialArguments struct {
	// ID of the Credential to remove
	CredentialID string `json:"credentialId"`
}

func (self *graph) DeleteCredential(ctx context.Context, arguments DeleteCredentialArguments) error {
	if err := self.requireOperator(ctx); err != nil {
		return err
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		for _, domain := range configuration.Domains {
			for index, credential := range domain.Credentials {
				if credential.ID != arguments.CredentialID {
					continue
				}
				domain.Credentials = append(domain.Credentials[:index], domain.Credentials[index+1:]...)
				return nil
			}
		}
		return api.ErrNotFound
	}); err != nil {
		return err
	}

	log.Noticef("%s removed credential %q", operatorName(ctx), arguments.CredentialID)
	return nil
}

func applyCredentialParameters(credential *config.Credential, parameters *CredentialParameters) {
	if parameters.Comment != nil {
		credential.Comment = *parameters.Comment
	}
	if parameters.Alias != nil {
		credential.Alias = strings.TrimSpace(*parameters.Alias)
	}
	if parameters.Disabled != nil {
		credential.Disabled = *parameters.Disabled
	}
}

// credentialKeyLength is fixed by the wire format: the SMTP password is the
// key followed by a 16 character signature over it.
const credentialKeyLength = 16

func generateCredentialKey() string {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand does not fail on any platform this runs on. Continuing
		// with a weak credential would be worse than stopping.
		panic("api: cannot generate a credential key: " + err.Error())
	}
	return strings.ToLower(base32.StdEncoding.EncodeToString(buffer))[:credentialKeyLength]
}
