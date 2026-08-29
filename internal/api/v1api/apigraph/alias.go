package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

type AliasQuery interface {
	// List the Aliases of a Domain, in the order they are evaluated
	ListAliases(ctx context.Context, arguments ListAliasesArguments) ([]*Alias, error)

	// Work out which Aliases a given address would match, without sending
	// anything
	MatchAliases(ctx context.Context, arguments MatchAliasesArguments) ([]*Alias, error)
}

type AliasMutation interface {
	// Add an Alias to a Domain
	CreateAlias(ctx context.Context, arguments CreateAliasArguments) (*Alias, error)

	// Change an Alias
	UpdateAlias(ctx context.Context, arguments UpdateAliasArguments) (*Alias, error)

	// Remove an Alias
	DeleteAlias(ctx context.Context, arguments DeleteAliasArguments) error
}

type ListAliasesArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`
}

func (self *graph) ListAliases(ctx context.Context, arguments ListAliasesArguments) ([]*Alias, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	aliases := make([]*Alias, 0, len(domain.Aliases))
	for _, alias := range domain.Aliases {
		aliases = append(aliases, describeAlias(alias))
	}
	return aliases, nil
}

type MatchAliasesArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`

	// The address to test, either a full address or just the part before the @
	Address string `json:"address"`
}

// MatchAliases answers "where would mail to this address actually go?". More
// than one alias can match, and every match produces a delivery, so a
// catch-all listed after a specific alias means two copies.
func (self *graph) MatchAliases(ctx context.Context, arguments MatchAliasesArguments) ([]*Alias, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	localPart := arguments.Address
	if index := strings.Index(localPart, "@"); index >= 0 {
		localPart = localPart[:index]
	}
	if localPart == "" {
		return nil, api.ErrInvalidArguments
	}

	matched := self.config.Current().MatchAliases(domain, localPart)
	aliases := make([]*Alias, 0, len(matched))
	for _, alias := range matched {
		aliases = append(aliases, describeAlias(alias))
	}
	return aliases, nil
}

// AliasParameters are the settings of an Alias that an operator can change.
type AliasParameters struct {
	// Regular expression matched against the part of the address before the @
	Pattern string `json:"pattern"`

	// Note for the operator
	Comment *string `json:"comment"`

	// One of null, email, webhook or mailServer
	Kind string `json:"kind"`

	// Destination address, when kind is email
	Email *string `json:"email"`

	// Destination URL, when kind is webhook
	Webhook *string `json:"webhook"`

	// Destination server, when kind is mailServer
	MailServer *MailServerParameters `json:"mailServer"`

	// Whether to ignore this Alias without deleting it
	Disabled *bool `json:"disabled"`
}

// MailServerParameters describes a downstream server to relay to.
type MailServerParameters struct {
	Host     string  `json:"host"`
	Port     uint16  `json:"port"`
	Username *string `json:"username"`

	// Password is write only; it is never returned.
	Password *string `json:"password"`
}

type CreateAliasArguments struct {
	// ID of the Domain to add the Alias to
	DomainID string `json:"domainId"`

	AliasParameters *AliasParameters `json:"aliasParameters"`
}

func (self *graph) CreateAlias(ctx context.Context, arguments CreateAliasArguments) (*Alias, error) {
	if _, err := self.requireDomain(ctx, arguments.DomainID); err != nil {
		return nil, err
	}
	if arguments.AliasParameters == nil {
		return nil, api.ErrInvalidArguments
	}
	if err := validatePattern(arguments.AliasParameters.Pattern); err != nil {
		return nil, err
	}

	created := &config.Alias{ID: config.NewID()}
	applyAliasParameters(created, arguments.AliasParameters)

	if err := self.config.Update(func(configuration *config.Configuration) error {
		domain := configuration.FindDomainByID(arguments.DomainID)
		if domain == nil {
			return api.ErrNotFound
		}
		domain.Aliases = append(domain.Aliases, created)
		return nil
	}); err != nil {
		return nil, err
	}

	log.Noticef("%s added alias %q", operatorName(ctx), created.Pattern)
	return describeAlias(created), nil
}

type UpdateAliasArguments struct {
	// ID of the Alias to change
	AliasID string `json:"aliasId"`

	AliasParameters *AliasParameters `json:"aliasParameters"`
}

func (self *graph) UpdateAlias(ctx context.Context, arguments UpdateAliasArguments) (*Alias, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	if arguments.AliasParameters == nil {
		return nil, api.ErrInvalidArguments
	}
	if err := validatePattern(arguments.AliasParameters.Pattern); err != nil {
		return nil, err
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		alias := configuration.FindAliasByID(arguments.AliasID)
		if alias == nil {
			return api.ErrNotFound
		}
		// The identifier is left alone on purpose: deliveries already stored
		// point at it, and changing it would orphan them.
		applyAliasParameters(alias, arguments.AliasParameters)
		return nil
	}); err != nil {
		return nil, err
	}

	alias := self.config.Current().FindAliasByID(arguments.AliasID)
	log.Noticef("%s changed alias %q", operatorName(ctx), alias.Pattern)
	return describeAlias(alias), nil
}

type DeleteAliasArguments struct {
	// ID of the Alias to remove
	AliasID string `json:"aliasId"`
}

func (self *graph) DeleteAlias(ctx context.Context, arguments DeleteAliasArguments) error {
	if err := self.requireOperator(ctx); err != nil {
		return err
	}

	var pattern string
	if err := self.config.Update(func(configuration *config.Configuration) error {
		for _, domain := range configuration.Domains {
			for index, alias := range domain.Aliases {
				if alias.ID != arguments.AliasID {
					continue
				}
				pattern = alias.Pattern
				domain.Aliases = append(domain.Aliases[:index], domain.Aliases[index+1:]...)
				return nil
			}
		}
		return api.ErrNotFound
	}); err != nil {
		return err
	}

	log.Noticef("%s removed alias %q", operatorName(ctx), pattern)
	return nil
}

func applyAliasParameters(alias *config.Alias, parameters *AliasParameters) {
	if parameters.Pattern != "" {
		alias.Pattern = parameters.Pattern
	}
	if parameters.Comment != nil {
		alias.Comment = *parameters.Comment
	}
	if parameters.Kind != "" {
		alias.Kind = config.AliasKind(parameters.Kind)
	}
	if parameters.Email != nil {
		alias.Email = strings.TrimSpace(*parameters.Email)
	}
	if parameters.Webhook != nil {
		alias.Webhook = strings.TrimSpace(*parameters.Webhook)
	}
	if parameters.Disabled != nil {
		alias.Disabled = *parameters.Disabled
	}
	if parameters.MailServer != nil {
		if alias.MailServer == nil {
			alias.MailServer = &config.MailServer{}
		}
		if parameters.MailServer.Host != "" {
			alias.MailServer.Host = parameters.MailServer.Host
		}
		if parameters.MailServer.Port != 0 {
			alias.MailServer.Port = parameters.MailServer.Port
		}
		if parameters.MailServer.Username != nil {
			alias.MailServer.Username = *parameters.MailServer.Username
		}
		// An omitted password keeps the existing one, so that editing a
		// relay's port does not silently clear its password.
		if parameters.MailServer.Password != nil {
			alias.MailServer.Password = *parameters.MailServer.Password
		}
	}

	// Keep only the destination the kind actually uses, so a configuration
	// file never carries a stale address on a webhook alias.
	switch alias.Kind {
	case config.AliasKindEmail:
		alias.Webhook = ""
		alias.MailServer = nil
	case config.AliasKindWebhook:
		alias.Email = ""
		alias.MailServer = nil
	case config.AliasKindMailServer:
		alias.Email = ""
		alias.Webhook = ""
	case config.AliasKindNull:
		alias.Email = ""
		alias.Webhook = ""
		alias.MailServer = nil
	}
}
