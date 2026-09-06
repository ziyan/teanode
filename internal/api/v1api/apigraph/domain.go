package apigraph

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/dns"
)

type DomainQuery interface {
	// List every Domain this server accepts mail for
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
	// Add a Domain to the configuration, with a signing key generated for it
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

// Domain is a mail domain this server accepts mail for, as the dashboard sees
// it: the configured settings, plus the live state of its DNS records.
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
}

// Alias matches recipient addresses and says where the mail goes.
type Alias struct {
	// ID of the Alias, stable for its lifetime
	ID string `json:"id"`

	// Regular expression matched against the part of the address before the @
	Pattern string `json:"pattern"`

	// Note for the operator
	Comment string `json:"comment,omitempty"`

	// One of null, email, webhook or mailServer
	Kind string `json:"kind"`

	// Destination address, when kind is email
	Email string `json:"email,omitempty"`

	// Destination URL, when kind is webhook
	Webhook string `json:"webhook,omitempty"`

	// Destination server, when kind is mailServer
	MailServer *MailServer `json:"mailServer,omitempty"`

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
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	configuration := self.config.Current()
	status := self.verifier.Status()

	domains := make([]*Domain, 0, len(configuration.Domains))
	for _, domain := range configuration.Domains {
		domains = append(domains, describeDomain(configuration, domain, status[domain.ID]))
	}
	return domains, nil
}

func (self *graph) GetServerAddresses(ctx context.Context) (*dns.ExternalAddresses, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	addresses := self.verifier.ExternalAddresses(ctx)
	return &addresses, nil
}

func (self *graph) GetOutgoingIdentity(ctx context.Context) (*dns.OutgoingIdentity, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	return self.verifier.OutgoingIdentity(ctx), nil
}

type GetDomainArguments struct {
	// ID of the Domain to look up
	DomainID string `json:"domainId"`
}

func (self *graph) GetDomain(ctx context.Context, arguments GetDomainArguments) (*Domain, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	return describeDomain(self.config.Current(), domain, self.verifier.StatusFor(domain.ID)), nil
}

// DomainParameters are the settings of a Domain that an operator can change.
//
// Every field is a pointer, including the domain name, so that a caller can
// send the one setting it is changing. It was not, and the effect was worse
// than a clumsy API: the name became a required argument of the schema, so an
// update carrying anything else was refused before it reached this code. Every
// partial update the dashboard makes — the mail server names, the signing
// selector — failed that way, and the failure looked like a save that did
// nothing.
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
	// host. Has to be under this domain, and to reach this server over HTTPS
	// with a certificate a mail program accepts.
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
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	// Optional in the type, because an update sends only what it changes;
	// required here, because a domain with no name is not a domain.
	if arguments.DomainParameters == nil || arguments.DomainParameters.Domain == nil ||
		strings.TrimSpace(*arguments.DomainParameters.Domain) == "" {
		return nil, api.ErrInvalidArguments
	}

	name := strings.ToLower(strings.TrimSpace(*arguments.DomainParameters.Domain))
	created := &config.Domain{
		// The domain name is the identifier. It is already unique across the
		// configuration, it never changes for a given domain — renaming one
		// means deleting it and adding another — and it makes every reference
		// to it legible: a URL reads /domains/example.com, and a row of stored
		// mail says which domain it arrived for without a lookup.
		ID:                       name,
		Domain:                   name,
		Subdomain:                "mail",
		SpamFilterScoreThreshold: defaultSpamFilterScoreThreshold,
	}
	applyDomainParameters(created, arguments.DomainParameters)

	// Every domain gets a signing key the moment it is created. Making this a
	// separate step people have to know about is how domains end up sending
	// unsigned mail for months.
	generated, err := config.GenerateDomainKey(self.config.Current().DKIM.Selector)
	if err != nil {
		return nil, err
	}
	created.DKIM = generated

	if err := self.config.Update(func(configuration *config.Configuration) error {
		if configuration.FindDomain(created.Domain) != nil {
			return api.ErrAlreadyExists
		}
		configuration.Domains = append(configuration.Domains, created)
		return nil
	}); err != nil {
		return nil, err
	}

	log.Noticef("%s added domain %q", operatorName(ctx), created.Domain)

	// Check its records straight away, so the dashboard can show what is left
	// to publish without waiting for the next scheduled check.
	if _, err := self.verifier.Check(ctx, created.ID); err != nil {
		log.Warningf("failed to check the records for %q: %s", created.Domain, err)
	}
	return describeDomain(self.config.Current(), created, self.verifier.StatusFor(created.ID)), nil
}

type UpdateDomainArguments struct {
	// ID of the Domain to change
	DomainID string `json:"domainId"`

	DomainParameters *DomainParameters `json:"domainParameters"`
}

func (self *graph) UpdateDomain(ctx context.Context, arguments UpdateDomainArguments) (*Domain, error) {
	if _, err := self.requireDomain(ctx, arguments.DomainID); err != nil {
		return nil, err
	}
	if arguments.DomainParameters == nil {
		return nil, api.ErrInvalidArguments
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		domain := configuration.FindDomainByID(arguments.DomainID)
		if domain == nil {
			return api.ErrNotFound
		}
		applyDomainParameters(domain, arguments.DomainParameters)
		return nil
	}); err != nil {
		return nil, err
	}

	domain := self.config.Current().FindDomainByID(arguments.DomainID)
	log.Noticef("%s changed domain %q", operatorName(ctx), domain.Domain)
	return describeDomain(self.config.Current(), domain, self.verifier.StatusFor(domain.ID)), nil
}

type DeleteDomainArguments struct {
	// ID of the Domain to remove
	DomainID string `json:"domainId"`
}

func (self *graph) DeleteDomain(ctx context.Context, arguments DeleteDomainArguments) error {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return err
	}
	name := domain.Domain

	if err := self.config.Update(func(configuration *config.Configuration) error {
		for index, candidate := range configuration.Domains {
			if candidate.ID != arguments.DomainID {
				continue
			}
			configuration.Domains = append(configuration.Domains[:index], configuration.Domains[index+1:]...)
			return nil
		}
		return api.ErrNotFound
	}); err != nil {
		return err
	}

	// Mail already received for this domain stays in the database and keeps
	// its identifier. The dashboard renders it as belonging to a deleted
	// domain rather than losing it.
	log.Noticef("%s removed domain %q; mail already received for it is kept", operatorName(ctx), name)
	return nil
}

type CheckDomainArguments struct {
	// ID of the Domain to check
	DomainID string `json:"domainId"`
}

func (self *graph) CheckDomain(ctx context.Context, arguments CheckDomainArguments) (*Domain, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	records, err := self.verifier.Check(ctx, domain.ID)
	if err != nil {
		return nil, err
	}
	return describeDomain(self.config.Current(), domain, records), nil
}

// defaultSpamFilterScoreThreshold is what a new domain is given. Defined
// once, in internal/config, because the exchange has to agree with the
// dashboard about what an unset threshold means.
const defaultSpamFilterScoreThreshold = config.DefaultSpamFilterScoreThreshold

func applyDomainParameters(domain *config.Domain, parameters *DomainParameters) {
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
		// Same shape as a mail server name, and empty restores the default
		// rather than being an error.
		domain.LinkHost = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(*parameters.LinkHost), ".")))
	}
	if parameters.DKIMSelector != nil {
		// Lowercased because it is a DNS label and DNS is case-insensitive,
		// so the record an operator publishes would not match what the panel
		// asks for if the two were spelled differently. Whether it is usable
		// as a label at all is the configuration's own check, which runs on
		// the way in and refuses the change.
		domain.DKIM.Selector = strings.ToLower(strings.TrimSpace(*parameters.DKIMSelector))
	}
}

type RegenerateDomainKeyArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`
}

func (self *graph) RegenerateDomainKey(ctx context.Context, arguments RegenerateDomainKeyArguments) (*Domain, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
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
	generated, err := config.GenerateDomainKey(selector)
	if err != nil {
		return nil, err
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		target := configuration.FindDomainByID(arguments.DomainID)
		if target == nil {
			return api.ErrNotFound
		}
		target.DKIM = generated
		return nil
	}); err != nil {
		return nil, err
	}

	log.Noticef("%s replaced the signing key for %q; mail signed with the old key stops verifying once the DNS record is changed", operatorName(ctx), domain.Domain)

	updated := self.config.Current().FindDomainByID(arguments.DomainID)
	records, err := self.verifier.Check(ctx, updated.ID)
	if err != nil {
		log.Warningf("failed to check the records for %q: %s", updated.Domain, err)
	}
	return describeDomain(self.config.Current(), updated, records), nil
}

// describeDomain renders a domain for the API.
//
// The configuration comes with it because one of the answers is not a property
// of the domain alone: which names its mail arrives at depends on what the
// domain says and, when it says nothing, on what the server is called.
func describeDomain(configuration *config.Configuration, domain *config.Domain, records *dns.RecordSet) *Domain {
	described := &Domain{
		MailHosts:                configuration.MailHostsFor(domain),
		ID:                       domain.ID,
		Domain:                   domain.Domain,
		Subdomain:                domain.Subdomain,
		Comment:                  domain.Comment,
		SpamFilterScoreThreshold: domain.SpamThreshold(),
		Aliases:                  make([]*Alias, 0, len(domain.Aliases)),
		Credentials:              make([]*Credential, 0, len(domain.Credentials)),
		Records:                  records,
		MailServers:              domain.MailServers,
		LinkHost:                 domain.LinkHost,
		LinkHostname:             configuration.LinkHostFor(domain),
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

func describeAlias(alias *config.Alias) *Alias {
	described := &Alias{
		ID:       alias.ID,
		Pattern:  alias.Pattern,
		Comment:  alias.Comment,
		Kind:     string(alias.Kind),
		Email:    alias.Email,
		Webhook:  alias.Webhook,
		Disabled: alias.Disabled,
	}
	if alias.MailServer != nil {
		// The password is deliberately not returned.
		described.MailServer = &MailServer{
			Host:     alias.MailServer.Host,
			Port:     alias.MailServer.Port,
			Username: alias.MailServer.Username,
		}
	}
	return described
}

func describeCredential(credential *config.Credential) *Credential {
	return &Credential{
		ID:       credential.ID,
		Comment:  credential.Comment,
		Alias:    credential.Alias,
		Disabled: credential.Disabled,
	}
}

// validatePattern rejects an alias pattern that would not compile, so the
// operator hears about it here rather than discovering that an address
// silently matches nothing.
// validatePattern checks that an alias pattern is usable.
//
// An empty pattern is a catch-all, not a missing value: it takes whatever no
// other alias matched, which is how config.Alias.IsCatchAll has always read it
// and what the configuration file documents. Refusing it here contradicted the
// configuration layer, the documentation, and the file the server was already
// running — creating one through the API was impossible while the same server
// held two dozen of them.
//
// Updating an alias already allowed it, so the two paths disagreed as well.
func validatePattern(pattern string) error {
	if pattern == "" {
		return nil
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("%w: %s is not a valid regular expression: %s", api.ErrInvalidArguments, pattern, err)
	}
	return nil
}

// operatorName is who to name in the log for a change. It is empty when no
// dashboard users are configured.
func operatorName(ctx context.Context) string {
	if username := api.ContextAuthenticatedUsername(ctx); username != "" {
		return username
	}
	return "an unauthenticated operator"
}
