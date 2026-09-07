package apigraph

import (
	"context"
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
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
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
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
// than one alias can match, and every match produces a delivery.
func (self *graph) MatchAliases(ctx context.Context, arguments MatchAliasesArguments) ([]*Alias, error) {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
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
	matched := matchAliases(domain, localPart)
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

	// One of null, email, webhook, mailServer or mailbox
	Kind string `json:"kind"`

	// Destination address, when kind is email
	Email *string `json:"email"`

	// Destination URL, when kind is webhook
	Webhook *string `json:"webhook"`

	// Destination server, when kind is mailServer
	MailServer *MailServerParameters `json:"mailServer"`

	// Destination mailbox on this server, when kind is mailbox
	MailboxID *string `json:"mailboxId"`

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
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	if arguments.AliasParameters == nil {
		return nil, api.ErrInvalidArguments
	}
	if err := validatePattern(arguments.AliasParameters.Pattern); err != nil {
		return nil, err
	}
	created := &models.Alias{DomainID: domain.ID}
	applyAliasParameters(created, arguments.AliasParameters)
	stored, err := self.transaction(ctx).CreateAlias(created)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s added alias %q to %q", operatorName(ctx), stored.Pattern, domain.Domain)
	return describeAlias(stored), nil
}

type UpdateAliasArguments struct {
	// ID of the Alias to change
	AliasID string `json:"aliasId"`

	AliasParameters *AliasParameters `json:"aliasParameters"`
}

// requireAlias finds an alias the caller may manage: not found when the
// alias does not exist or belongs to a domain they may not touch.
func (self *graph) requireAlias(ctx context.Context, aliasId string) (*models.Alias, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}
	alias, err := self.transaction(ctx).GetAlias(aliasId)
	if err != nil {
		return nil, err
	}
	if alias == nil {
		return nil, api.ErrNotFound
	}
	if _, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, alias.DomainID); err != nil {
		return nil, err
	}
	return alias, nil
}

func (self *graph) UpdateAlias(ctx context.Context, arguments UpdateAliasArguments) (*Alias, error) {
	if _, err := self.requireAlias(ctx, arguments.AliasID); err != nil {
		return nil, err
	}
	if arguments.AliasParameters == nil {
		return nil, api.ErrInvalidArguments
	}
	if err := validatePattern(arguments.AliasParameters.Pattern); err != nil {
		return nil, err
	}
	updated, err := self.transaction(ctx).UpdateAlias(arguments.AliasID, func(alias *models.Alias) error {
		// The identifier is left alone on purpose: deliveries already stored
		// point at it, and changing it would orphan them.
		applyAliasParameters(alias, arguments.AliasParameters)
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s changed alias %q", operatorName(ctx), updated.Pattern)
	return describeAlias(updated), nil
}

type DeleteAliasArguments struct {
	// ID of the Alias to remove
	AliasID string `json:"aliasId"`
}

func (self *graph) DeleteAlias(ctx context.Context, arguments DeleteAliasArguments) error {
	alias, err := self.requireAlias(ctx, arguments.AliasID)
	if err != nil {
		return err
	}
	if err := self.transaction(ctx).DeleteAlias(alias.ID); err != nil {
		return translateError(err)
	}
	log.Noticef("%s removed alias %q", operatorName(ctx), alias.Pattern)
	return nil
}

// matchAliases is the exchange's rule, applied here for the dry run: every
// enabled alias whose pattern matches, or the catch-alls when none does.
func matchAliases(domain *models.Domain, localPart string) []*models.Alias {
	var matched, catchAll []*models.Alias
	for _, alias := range domain.Aliases {
		if alias == nil || alias.Disabled {
			continue
		}
		if alias.IsCatchAll() {
			catchAll = append(catchAll, alias)
			continue
		}
		compiled, err := regexp.Compile("(?i)" + alias.Pattern)
		if err != nil {
			continue
		}
		if compiled.MatchString(localPart) {
			matched = append(matched, alias)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return catchAll
}

func applyAliasParameters(alias *models.Alias, parameters *AliasParameters) {
	if parameters.Pattern != "" {
		alias.Pattern = parameters.Pattern
	}
	if parameters.Comment != nil {
		alias.Comment = *parameters.Comment
	}
	if parameters.Kind != "" {
		alias.Kind = models.AliasKind(parameters.Kind)
	}
	if parameters.Email != nil {
		alias.Email = strings.TrimSpace(*parameters.Email)
	}
	if parameters.Webhook != nil {
		alias.Webhook = strings.TrimSpace(*parameters.Webhook)
	}
	if parameters.MailboxID != nil {
		alias.MailboxID = strings.TrimSpace(*parameters.MailboxID)
	}
	if parameters.Disabled != nil {
		alias.Disabled = *parameters.Disabled
	}
	if parameters.MailServer != nil {
		if alias.MailServer == nil {
			alias.MailServer = &models.MailServer{}
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

	// Keep only the destination the kind actually uses, so a row never
	// carries a stale address on a webhook alias.
	switch alias.Kind {
	case models.AliasKindEmail:
		alias.Webhook, alias.MailServer, alias.MailboxID = "", nil, ""
	case models.AliasKindWebhook:
		alias.Email, alias.MailServer, alias.MailboxID = "", nil, ""
	case models.AliasKindMailServer:
		alias.Email, alias.Webhook, alias.MailboxID = "", "", ""
	case models.AliasKindMailbox:
		alias.Email, alias.Webhook, alias.MailServer = "", "", nil
	case models.AliasKindNull:
		alias.Email, alias.Webhook, alias.MailServer, alias.MailboxID = "", "", nil, ""
	}
}
