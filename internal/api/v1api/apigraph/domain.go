package apigraph

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/dns"
	"github.com/ziyan/teanode/internal/models"
)

type DomainQuery interface {
	// List every Domain the caller may see: the ones they manage, and the
	// ones whose mail they may audit
	ListDomains(ctx context.Context) ([]*Domain, error)

	// Get a particular Domain
	GetDomain(ctx context.Context, arguments GetDomainArguments) (*Domain, error)

	// Get the addresses the outside world reaches this server on, which is
	// what its DNS records have to point at
	GetServerAddresses(ctx context.Context) (*dns.ExternalAddresses, error)

	// Get how this server's outgoing mail identifies itself: the address it
	// leaves from, whether that address confirms against its reverse name,
	// and whether the greeting agrees. These decide whether large receivers
	// accept the mail at all, and are not the same question as the records a
	// domain publishes.
	GetOutgoingIdentity(ctx context.Context) (*dns.OutgoingIdentity, error)
}

type DomainMutation interface {
	// Add a Domain, with a signing key generated for it. Needs the
	// domain:manage-all permission: a domain that does not exist yet belongs
	// to no group.
	CreateDomain(ctx context.Context, arguments CreateDomainArguments) (*Domain, error)

	// Replace a Domain's signing key. Mail signed with the old key stops
	// verifying as soon as the DNS record is changed.
	RegenerateDomainKey(ctx context.Context, arguments RegenerateDomainKeyArguments) (*Domain, error)

	// Change a Domain
	UpdateDomain(ctx context.Context, arguments UpdateDomainArguments) (*Domain, error)

	// Remove a Domain, along with its Aliases and Credentials
	DeleteDomain(ctx context.Context, arguments DeleteDomainArguments) error

	// Check a Domain's DNS records now rather than waiting for the next
	// scheduled check
	CheckDomain(ctx context.Context, arguments CheckDomainArguments) (*Domain, error)
}

// Domain is a mail domain this server accepts mail for, as the web UI sees
// it: the stored settings, plus the live state of its DNS records.
type Domain struct {
	// ID of the Domain, stable for its lifetime
	ID string `json:"id"`

	// The mail domain itself, for example example.com
	Domain string `json:"domain"`

	// Label whose record points at this server, so bounces and DMARC reports
	// have somewhere to arrive
	Subdomain string `json:"subdomain"`

	// Note for the operator; never used in mail handling
	Comment string `json:"comment,omitempty"`

	// SpamAssassin score at or above which mail is rejected
	SpamFilterScoreThreshold float64 `json:"spamFilterScoreThreshold"`

	// Aliases deciding where mail for this Domain goes
	Aliases []*Alias `json:"aliases"`

	// Credentials that may send mail as this Domain
	Credentials []*Credential `json:"credentials"`

	// The DNS records this Domain needs and whether they are published; null
	// until the first check has run
	Records *dns.RecordSet `json:"records,omitempty"`

	// The names this Domain's MX records point at. Empty means the default,
	// one name derived from the domain, which MailHosts below spells out.
	MailServers []string `json:"mailServers,omitempty"`

	// The names this Domain's mail actually arrives at, configured or
	// derived. This is what the MX records must name.
	MailHosts []string `json:"mailHosts,omitempty"`

	// The name written into addresses this server puts in mail it sends, such
	// as a picture in a template. Empty means the first mail host.
	LinkHost string `json:"linkHost,omitempty"`

	// The name actually used for those addresses, configured or derived.
	LinkHostname string `json:"linkHostname,omitempty"`

	// The selector this Domain's mail is signed under
	DKIMSelector string `json:"dkimSelector,omitempty"`

	// Whether this Domain has a signing key at all. Without one its outgoing
	// mail is unsigned and receivers may distrust it.
	HasDKIMKey bool `json:"hasDkimKey"`

	// Whether the caller may change this Domain, as opposed to only reading
	// its mail. What the web UI shows the settings tab on.
	Manageable bool `json:"manageable"`
}

// Alias matches recipient addresses and says where the mail goes.
type Alias struct {
	// ID of the Alias, stable for its lifetime
	ID string `json:"id"`

	// Regular expression matched against the part of the address before the @
	Pattern string `json:"pattern"`

	// Note for the operator
	Comment string `json:"comment,omitempty"`

	// One of null, email, webhook, mailServer or mailbox
	Kind string `json:"kind"`

	// Destination address, when kind is email
	Email string `json:"email,omitempty"`

	// Destination URL, when kind is webhook
	Webhook string `json:"webhook,omitempty"`

	// Destination server, when kind is mailServer
	MailServer *MailServer `json:"mailServer,omitempty"`

	// Destination mailbox on this server, when kind is mailbox
	MailboxID string `json:"mailboxId,omitempty"`

	// Whether this Alias is ignored without being deleted
	Disabled bool `json:"disabled"`
}

// MailServer is a downstream server to relay to.
type MailServer struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Username string `json:"username,omitempty"`
}

// Credential is an SMTP identity that may send mail through this server. The
// key is never returned; the password derived from it is shown once, when the
// credential is created.
type Credential struct {
	// ID of the Credential, which is also the SMTP username
	ID string `json:"id"`

	// Note naming the device or service holding it
	Comment string `json:"comment,omitempty"`

	// When set, restricts this Credential to sending as that local part
	Alias string `json:"alias,omitempty"`

	// Whether this Credential is refused without being deleted
	Disabled bool `json:"disabled"`
}

func (self *graph) ListDomains(ctx context.Context) ([]*Domain, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	all, err := self.transaction(ctx).ListDomains()
	if err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	status := self.verifier.Status()

	domains := make([]*Domain, 0, len(all))
	for _, domain := range all {
		manageable := principal.Permissions.HasOverDomain(models.PermissionDomainManage, domain.ID)
		if !manageable && !principal.Permissions.HasOverDomain(models.PermissionMailAudit, domain.ID) {
			continue
		}
		described := describeDomain(configuration, domain, all, status[domain.ID])
		described.Manageable = manageable
		domains = append(domains, described)
	}
	return domains, nil
}

func (self *graph) GetServerAddresses(ctx context.Context) (*dns.ExternalAddresses, error) {
	if _, err := self.requireManagement(ctx); err != nil {
		return nil, err
	}
	addresses := self.verifier.ExternalAddresses(ctx)
	return &addresses, nil
}

func (self *graph) GetOutgoingIdentity(ctx context.Context) (*dns.OutgoingIdentity, error) {
	if _, err := self.requireManagement(ctx); err != nil {
		return nil, err
	}
	return self.verifier.OutgoingIdentity(ctx), nil
}

type GetDomainArguments struct {
	// ID of the Domain to look up
	DomainID string `json:"domainId"`
}

func (self *graph) GetDomain(ctx context.Context, arguments GetDomainArguments) (*Domain, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	manageable := principal.Permissions.HasOverDomain(models.PermissionDomainManage, arguments.DomainID)
	if !manageable && !principal.Permissions.HasOverDomain(models.PermissionMailAudit, arguments.DomainID) {
		return nil, api.ErrNotFound
	}
	described, err := self.describeDomainById(ctx, arguments.DomainID, self.verifier.StatusFor(arguments.DomainID))
	if err != nil {
		return nil, err
	}
	described.Manageable = manageable
	return described, nil
}

// describeDomainById reads a domain, and every other, and renders it.
func (self *graph) describeDomainById(ctx context.Context, domainId string, records *dns.RecordSet) (*Domain, error) {
	domains, err := self.transaction(ctx).ListDomains()
	if err != nil {
		return nil, err
	}
	for _, domain := range domains {
		if domain.ID == domainId {
			return describeDomain(self.config.Current(), domain, domains, records), nil
		}
	}
	return nil, api.ErrNotFound
}

// DomainParameters are the settings of a Domain that an operator can change.
//
// Every field is a pointer, including the domain name, so that a caller can
// send the one setting it is changing.
type DomainParameters struct {
	// The mail domain itself. Required when creating; omit it to leave the
	// name alone.
	Domain *string `json:"domain"`

	// Label whose record points at this server; defaults to "mail"
	Subdomain *string `json:"subdomain"`

	// Note for the operator
	Comment *string `json:"comment"`

	// SpamAssassin score at or above which mail is rejected
	SpamFilterScoreThreshold *float64 `json:"spamFilterScoreThreshold"`

	// Names this domain's MX records point at. Empty restores the default,
	// which is one name derived from the domain: "mx." in front of it.
	MailServers *[]string `json:"mailServers"`

	// Name to write into addresses this server puts in mail it sends, such as
	// the pictures in a template. Empty restores the default, the first mail
	// host. Has to be under this domain.
	LinkHost *string `json:"linkHost"`

	// Label the signing key is published under, as
	// <selector>._domainkey.<domain>. Changing it moves the record: mail is
	// signed under the new label from the moment it is saved, so publish the
	// new record first or signatures fail until you do.
	DKIMSelector *string `json:"dkimSelector"`
}

type CreateDomainArguments struct {
	DomainParameters *DomainParameters `json:"domainParameters"`
}

func (self *graph) CreateDomain(ctx context.Context, arguments CreateDomainArguments) (*Domain, error) {
	if _, err := self.requirePermission(ctx, models.PermissionDomainManageAll); err != nil {
		return nil, err
	}
	// Optional in the type, because an update sends only what it changes;
	// required here, because a domain with no name is not a domain.
	if arguments.DomainParameters == nil || arguments.DomainParameters.Domain == nil ||
		strings.TrimSpace(*arguments.DomainParameters.Domain) == "" {
		return nil, api.ErrInvalidArguments
	}

	name := strings.ToLower(strings.TrimSpace(*arguments.DomainParameters.Domain))
	created := &models.Domain{
		// The domain name is the identifier. It is already unique, it never
		// changes for a given domain — renaming one means deleting it and
		// adding another — and it makes every reference to it legible: a URL
		// reads /domains/example.com, and a row of stored mail says which
		// domain it arrived for without a lookup.
		ID:                       name,
		Domain:                   name,
		Subdomain:                "mail",
		SpamFilterScoreThreshold: models.DefaultSpamFilterScoreThreshold,
	}
	applyDomainParameters(created, arguments.DomainParameters)

	// Every domain gets a signing key the moment it is created. Making this a
	// separate step people have to know about is how domains end up sending
	// unsigned mail for months.
	generated, err := models.GenerateDomainKey(self.config.Current().DKIM.Selector)
	if err != nil {
		return nil, err
	}
	if created.DKIM.Selector != "" {
		generated.Selector = created.DKIM.Selector
	}
	created.DKIM = generated

	stored, err := self.transaction(ctx).CreateDomain(created)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s added domain %q", operatorName(ctx), stored.Domain)

	// Check its records straight away, so the web UI can show what is left
	// to publish without waiting for the next scheduled check. After the
	// commit: the check reads the domain table from another transaction.
	if err := self.transaction(ctx).Commit(); err != nil {
		return nil, err
	}
	records, err := self.verifier.Check(ctx, stored.ID)
	if err != nil {
		log.Warningf("failed to check the records for %q: %s", stored.Domain, err)
	}
	described, err := self.describeDomainById(ctx, stored.ID, records)
	if err != nil {
		return nil, err
	}
	described.Manageable = true
	return described, nil
}

type UpdateDomainArguments struct {
	// ID of the Domain to change
	DomainID string `json:"domainId"`

	DomainParameters *DomainParameters `json:"domainParameters"`
}

func (self *graph) UpdateDomain(ctx context.Context, arguments UpdateDomainArguments) (*Domain, error) {
	if _, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID); err != nil {
		return nil, err
	}
	if arguments.DomainParameters == nil {
		return nil, api.ErrInvalidArguments
	}
	updated, err := self.transaction(ctx).UpdateDomain(arguments.DomainID, func(domain *models.Domain) error {
		// The name is left alone on purpose: it is the identifier, and stored
		// mail names it.
		parameters := *arguments.DomainParameters
		parameters.Domain = nil
		applyDomainParameters(domain, &parameters)
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s changed domain %q", operatorName(ctx), updated.Domain)
	described, err := self.describeDomainById(ctx, updated.ID, self.verifier.StatusFor(updated.ID))
	if err != nil {
		return nil, err
	}
	described.Manageable = true
	return described, nil
}

type DeleteDomainArguments struct {
	// ID of the Domain to remove
	DomainID string `json:"domainId"`
}

func (self *graph) DeleteDomain(ctx context.Context, arguments DeleteDomainArguments) error {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return err
	}
	if err := self.transaction(ctx).DeleteDomain(domain.ID); err != nil {
		return translateError(err)
	}
	// Mail already received for this domain stays in the database and keeps
	// its identifier. The web UI renders it as belonging to a deleted domain
	// rather than losing it.
	log.Noticef("%s removed domain %q; mail already received for it is kept", operatorName(ctx), domain.Domain)
	return nil
}

type CheckDomainArguments struct {
	// ID of the Domain to check
	DomainID string `json:"domainId"`
}

func (self *graph) CheckDomain(ctx context.Context, arguments CheckDomainArguments) (*Domain, error) {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	records, err := self.verifier.Check(ctx, domain.ID)
	if err != nil {
		return nil, err
	}
	described, err := self.describeDomainById(ctx, domain.ID, records)
	if err != nil {
		return nil, err
	}
	described.Manageable = true
	return described, nil
}

func applyDomainParameters(domain *models.Domain, parameters *DomainParameters) {
	if parameters.Domain != nil && strings.TrimSpace(*parameters.Domain) != "" {
		domain.Domain = strings.ToLower(strings.TrimSpace(*parameters.Domain))
	}
	if parameters.Subdomain != nil {
		domain.Subdomain = strings.TrimSpace(*parameters.Subdomain)
	}
	if parameters.Comment != nil {
		domain.Comment = *parameters.Comment
	}
	if parameters.SpamFilterScoreThreshold != nil {
		domain.SpamFilterScoreThreshold = *parameters.SpamFilterScoreThreshold
	}
	if parameters.MailServers != nil {
		// Lowercased and trimmed, and the blanks dropped, because this
		// arrives from a form where a trailing comma is ordinary. Empty is
		// not an error: it restores the derived default.
		hosts := make([]string, 0, len(*parameters.MailServers))
		for _, host := range *parameters.MailServers {
			if host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(host), "."))); host != "" {
				hosts = append(hosts, host)
			}
		}
		domain.MailServers = hosts
	}
	if parameters.LinkHost != nil {
		domain.LinkHost = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(*parameters.LinkHost), ".")))
	}
	if parameters.DKIMSelector != nil {
		// Lowercased because it is a DNS label and DNS is case-insensitive,
		// so the record an operator publishes would not match what the panel
		// asks for if the two were spelled differently.
		domain.DKIM.Selector = strings.ToLower(strings.TrimSpace(*parameters.DKIMSelector))
	}
}

type RegenerateDomainKeyArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`
}

func (self *graph) RegenerateDomainKey(ctx context.Context, arguments RegenerateDomainKeyArguments) (*Domain, error) {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	// Under the selector this domain already uses, not the default a new
	// domain would be given. The selector is where the record is published,
	// and a rotation that silently moved it would ask for a new record at a
	// new name without saying so — which is not what "replace the key" means.
	selector := domain.DKIM.Selector
	if selector == "" {
		selector = self.config.Current().DKIM.Selector
	}
	generated, err := models.GenerateDomainKey(selector)
	if err != nil {
		return nil, err
	}
	updated, err := self.transaction(ctx).UpdateDomain(domain.ID, func(domain *models.Domain) error {
		domain.DKIM = generated
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s replaced the signing key for %q; mail signed with the old key stops verifying once the DNS record is changed", operatorName(ctx), updated.Domain)

	if err := self.transaction(ctx).Commit(); err != nil {
		return nil, err
	}
	records, err := self.verifier.Check(ctx, updated.ID)
	if err != nil {
		log.Warningf("failed to check the records for %q: %s", updated.Domain, err)
	}
	described, err := self.describeDomainById(ctx, updated.ID, records)
	if err != nil {
		return nil, err
	}
	described.Manageable = true
	return described, nil
}

// describeDomain renders a domain for the API.
//
// Every domain comes with it because one of the answers is not a property of
// the domain alone: which names its mail arrives at depends on what the domain
// says and, when it says nothing, on which domain owns the server's name.
func describeDomain(configuration *config.Configuration, domain *models.Domain, domains []*models.Domain, records *dns.RecordSet) *Domain {
	described := &Domain{
		ID:                       domain.ID,
		Domain:                   domain.Domain,
		Subdomain:                domain.Subdomain,
		Comment:                  domain.Comment,
		SpamFilterScoreThreshold: domain.SpamFilterScoreThreshold,
		Aliases:                  make([]*Alias, 0, len(domain.Aliases)),
		Credentials:              make([]*Credential, 0, len(domain.Credentials)),
		Records:                  records,
		MailServers:              domain.MailServers,
		MailHosts:                configuration.MailHostsFor(domain, domains),
		LinkHost:                 domain.LinkHost,
		LinkHostname:             configuration.LinkHostFor(domain, domains),
		DKIMSelector:             domain.DKIM.Selector,
		HasDKIMKey:               domain.DKIM.PrivateKey != "",
	}
	for _, alias := range domain.Aliases {
		described.Aliases = append(described.Aliases, describeAlias(alias))
	}
	for _, credential := range domain.Credentials {
		described.Credentials = append(described.Credentials, describeCredential(credential))
	}
	return described
}

func describeAlias(alias *models.Alias) *Alias {
	if alias == nil {
		return nil
	}
	described := &Alias{
		ID:        alias.ID,
		Pattern:   alias.Pattern,
		Comment:   alias.Comment,
		Kind:      string(alias.Kind),
		Email:     alias.Email,
		Webhook:   alias.Webhook,
		MailboxID: alias.MailboxID,
		Disabled:  alias.Disabled,
	}
	if alias.MailServer != nil {
		described.MailServer = &MailServer{
			Host:     alias.MailServer.Host,
			Port:     alias.MailServer.Port,
			Username: alias.MailServer.Username,
		}
	}
	return described
}

func describeCredential(credential *models.Credential) *Credential {
	if credential == nil {
		return nil
	}
	return &Credential{
		ID:       credential.ID,
		Comment:  credential.Comment,
		Alias:    credential.Alias,
		Disabled: credential.Disabled,
	}
}

// validatePattern checks that an alias pattern is usable.
//
// An empty pattern is a catch-all, not a missing value: it takes whatever no
// other alias matched.
func validatePattern(pattern string) error {
	if pattern == "" {
		return nil
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("%w: the pattern does not compile: %s", api.ErrInvalidArguments, models.RegexpErrorMessage(err))
	}
	return nil
}
