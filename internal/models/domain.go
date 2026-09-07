package models

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultSpamFilterScoreThreshold is the score at which mail is rejected for
// a domain that never chose one. SpamAssassin's own default.
const DefaultSpamFilterScoreThreshold = 5.0

// Domain is a mail domain this server accepts mail for, with the aliases that
// decide where its mail goes and the credentials that may send as it.
//
// A row in the domain table. Its identifier is what stored mail, deliveries
// and usage rows name it by, and is stable for as long as it exists.
type Domain struct {
	ID string `json:"id"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	// Domain is the mail domain itself, for example "example.com".
	Domain string `json:"domain"`

	// Subdomain is the label whose CNAME points at this server, so that
	// bounces and DMARC reports have somewhere to arrive. Usually "mail",
	// making mail.example.com a CNAME to the server's name.
	Subdomain string `json:"subdomain,omitempty"`

	// MailServers are the names this domain's MX records point at, and so
	// the names a sender connects to and is handed a certificate for. Empty
	// means one name derived from the domain itself, "mx." in front of it.
	MailServers []string `json:"mailServers,omitempty"`

	// LinkHost is the name in the addresses this server writes into mail it
	// sends. Empty means the domain's first mail server name.
	LinkHost string `json:"linkHost,omitempty"`

	// Comment is a note for the operator; it is never used in mail handling.
	Comment string `json:"comment,omitempty"`

	// DKIM is the key that signs mail sent from this domain.
	DKIM DomainKey `json:"-"`

	// TLS is the certificate served to a sender connecting to this domain's
	// own mail server name, obtained automatically when tls.acme.perDomain is
	// on.
	TLS DomainCertificate `json:"-"`

	// SpamFilterScoreThreshold is the spam score above which mail for this
	// domain is rejected. Read it with SpamThreshold(), which supplies the
	// default for a domain that has never been given one.
	SpamFilterScoreThreshold float64 `json:"spamFilterScoreThreshold"`

	// Aliases decide where mail for this domain goes, in the order the
	// operator arranged them. Loaded with the domain.
	Aliases []*Alias `json:"aliases,omitempty"`

	// Credentials may authenticate to the submission port and send mail as
	// this domain. Loaded with the domain.
	Credentials []*Credential `json:"credentials,omitempty"`
}

// SpamThreshold is the score above which mail for this domain is refused.
func (self *Domain) SpamThreshold() float64 {
	if self == nil || self.SpamFilterScoreThreshold <= 0 {
		return DefaultSpamFilterScoreThreshold
	}
	return self.SpamFilterScoreThreshold
}

// Hostname returns the fully qualified name that mail for this domain arrives
// at, for example "mail.example.com", used as the bounce and DMARC report
// domain. It falls back to the bare domain when no subdomain is configured.
func (self *Domain) Hostname() string {
	if self.Subdomain == "" {
		return self.Domain
	}
	return self.Subdomain + "." + self.Domain
}

// InThisDomain reports whether a name belongs to this domain's own zone, and
// so whether its address record is this operator's to publish and its
// certificate this server's to obtain.
func (self *Domain) InThisDomain(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	name := strings.ToLower(self.Domain)
	return host == name || strings.HasSuffix(host, "."+name)
}

// FindAlias returns the alias with this identifier, or nil.
func (self *Domain) FindAlias(aliasId string) *Alias {
	if self == nil {
		return nil
	}
	for _, alias := range self.Aliases {
		if alias != nil && alias.ID == aliasId {
			return alias
		}
	}
	return nil
}

// FindCredential returns the credential with this identifier, or nil.
func (self *Domain) FindCredential(credentialId string) *Credential {
	if self == nil {
		return nil
	}
	for _, credential := range self.Credentials {
		if credential != nil && credential.ID == credentialId {
			return credential
		}
	}
	return nil
}

// Validate reports everything wrong with the domain itself. Its aliases and
// credentials are validated by their own writers.
func (self *Domain) Validate() error {
	var errors ValidationErrors
	if self.Domain == "" {
		errors.add("domain", "required: the mail domain, for example example.com")
	} else if !IsHostname(self.Domain) {
		errors.add("domain", "%q is not a domain name", self.Domain)
	}
	if self.Subdomain != "" && !IsHostLabel(self.Subdomain) {
		errors.add("subdomain", "%q is not a single host label, for example mail", self.Subdomain)
	}
	for index, host := range self.MailServers {
		if strings.TrimSpace(host) == "" {
			continue
		}
		if !IsHostname(strings.TrimSuffix(strings.TrimSpace(host), ".")) {
			errors.add(fmt.Sprintf("mailServers[%d]", index), "%q is not a host name, for example mx.%s", host, self.Domain)
		}
	}
	if host := strings.TrimSuffix(strings.TrimSpace(self.LinkHost), "."); host != "" {
		if !IsHostname(host) {
			errors.add("linkHost", "%q is not a host name, for example %s", self.LinkHost, self.Domain)
		} else if !self.InThisDomain(host) {
			errors.add("linkHost", "%q is not under %s; an address in another domain names whoever runs that one", host, self.Domain)
		}
	}
	if self.SpamFilterScoreThreshold < 0 {
		errors.add("spamFilterScoreThreshold", "must not be negative")
	}
	if self.DKIM.Selector != "" || self.DKIM.PrivateKey != "" {
		if self.DKIM.Selector == "" {
			errors.add("dkim.selector", "required when a signing key is set")
		} else if !IsHostLabel(self.DKIM.Selector) {
			errors.add("dkim.selector", "%q is not usable as a DNS label", self.DKIM.Selector)
		}
		if self.DKIM.PrivateKey == "" {
			errors.add("dkim.privateKey", "required when a selector is set")
		} else if _, err := self.DKIM.Signer(); err != nil {
			errors.add("dkim.privateKey", "cannot be used: %s", err)
		}
	}
	return errors.ErrOrNil()
}

// DomainKey is the key a domain signs its outgoing mail with.
type DomainKey struct {
	// Selector this key is published under.
	Selector string `json:"selector,omitempty"`

	// PrivateKey is a PKCS#8 RSA key in PEM form. Encrypted at rest, and
	// never shown.
	PrivateKey string `json:"-"`
}

// domainKeyBits is the size of a generated signing key.
//
// 2048 is the practical maximum rather than a compromise: a 4096-bit key
// produces a DNS TXT value long enough that some providers mangle it, and
// several large receivers do not check keys that big anyway. 1024 is now
// treated as weak.
const domainKeyBits = 2048

// GenerateDomainKey creates a signing key for a domain.
func GenerateDomainKey(selector string) (DomainKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, domainKeyBits)
	if err != nil {
		return DomainKey{}, fmt.Errorf("models: cannot generate a signing key: %w", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return DomainKey{}, fmt.Errorf("models: cannot encode a signing key: %w", err)
	}
	return DomainKey{
		Selector:   selector,
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})),
	}, nil
}

// Signer parses the private key so it can sign a message.
func (self *DomainKey) Signer() (crypto.Signer, error) {
	if strings.TrimSpace(self.PrivateKey) == "" {
		return nil, fmt.Errorf("models: no signing key")
	}
	block, _ := pem.Decode([]byte(self.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("models: the signing key is not valid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// A key written by an older release, or exported by another tool,
		// may be in the older format.
		legacy, legacyError := x509.ParsePKCS1PrivateKey(block.Bytes)
		if legacyError != nil {
			return nil, fmt.Errorf("models: cannot parse the signing key: %w", err)
		}
		return legacy, nil
	}
	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("models: the signing key cannot sign")
	}
	return signer, nil
}

// PublicKeyRecord returns the TXT value to publish at
// <selector>._domainkey.<domain>, which is what makes the signature checkable.
func (self *DomainKey) PublicKeyRecord() (string, error) {
	signer, err := self.Signer()
	if err != nil {
		return "", err
	}
	encoded, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return "", fmt.Errorf("models: cannot encode the public key: %w", err)
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(encoded), nil
}

// DomainKeyName is where a domain's key is published.
func DomainKeyName(selector, domain string) string {
	return fmt.Sprintf("%s._domainkey.%s", selector, domain)
}

// DomainCertificate is a certificate obtained for a domain's own mail server
// name.
type DomainCertificate struct {
	// Certificate is the issued chain, in PEM.
	Certificate string `json:"-"`

	// PrivateKey is its private half, in PEM. Encrypted at rest the same way
	// the signing key is.
	PrivateKey string `json:"-"`
}

// AliasKind selects what an alias does with matching mail.
type AliasKind string

const (
	// AliasKindNull accepts the mail and discards it.
	AliasKindNull AliasKind = "null"

	// AliasKindEmail forwards the mail to another email address.
	AliasKindEmail AliasKind = "email"

	// AliasKindWebhook posts the mail to an HTTP endpoint.
	AliasKindWebhook AliasKind = "webhook"

	// AliasKindMailServer relays the mail to a specific mail server.
	AliasKindMailServer AliasKind = "mailServer"

	// AliasKindMailbox delivers the mail into a mailbox on this server: an
	// item in the mailbox's Inbox, referencing the one stored message.
	AliasKindMailbox AliasKind = "mailbox"
)

// MailServer is where an alias of kind mailServer relays to.
type MailServer struct {
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"-"`
}

// Alias decides where mail for a domain goes. Every alias whose pattern
// matches produces a delivery, so one address can forward to several places;
// an alias with an empty pattern is a catch-all and receives only what nothing
// else matched.
type Alias struct {
	// ID is generated once and never changes; stored deliveries reference it.
	ID string `json:"id"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	DomainID string `json:"domainId"`

	// Position is the alias's place among the domain's aliases, which is the
	// order the operator arranged and the order they are matched in.
	Position int `json:"position"`

	// Pattern is a Go regular expression matched against the local part of
	// the recipient address, the part before the "@", without regard to
	// case. Anchor it: "^hello$" matches only hello@, while "hello" also
	// matches say-hello-now@. An empty pattern makes this a catch-all.
	Pattern string `json:"pattern"`

	// Comment is a note for the operator.
	Comment string `json:"comment,omitempty"`

	// Kind is one of null, email, webhook, mailServer or mailbox.
	Kind AliasKind `json:"kind"`

	// Email is the destination address when kind is email.
	Email string `json:"email,omitempty"`

	// Webhook is the destination URL when kind is webhook.
	Webhook string `json:"webhook,omitempty"`

	// MailServer is the destination server when kind is mailServer.
	MailServer *MailServer `json:"mailServer,omitempty"`

	// MailboxID is the mailbox delivered into when kind is mailbox.
	MailboxID string `json:"mailboxId,omitempty"`

	// Disabled stops the alias from matching without deleting it.
	Disabled bool `json:"disabled,omitempty"`
}

// IsCatchAll reports whether this alias receives everything nothing else
// matched.
func (self *Alias) IsCatchAll() bool {
	return self.Pattern == ""
}

// Validate reports everything wrong with the alias.
func (self *Alias) Validate() error {
	var errors ValidationErrors
	if self.Pattern != "" {
		if _, err := regexp.Compile(self.Pattern); err != nil {
			errors.add("pattern", "invalid regular expression: %s", RegexpErrorMessage(err))
		}
	}
	switch self.Kind {
	case AliasKindNull:
	case AliasKindEmail:
		if self.Email == "" {
			errors.add("email", "required when kind is email: the address to forward to")
		} else if !IsEmailAddress(self.Email) {
			errors.add("email", "%q is not an email address", self.Email)
		}
	case AliasKindWebhook:
		if self.Webhook == "" {
			errors.add("webhook", "required when kind is webhook: the URL to post to")
		} else if parsed, err := url.Parse(self.Webhook); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			errors.add("webhook", "%q is not an http or https URL", self.Webhook)
		}
	case AliasKindMailServer:
		if self.MailServer == nil || self.MailServer.Host == "" {
			errors.add("mailServer.host", "required when kind is mailServer: the server to relay to")
		} else if !IsRelayHost(self.MailServer.Host) {
			errors.add("mailServer.host", "%q is not a host name or address", self.MailServer.Host)
		}
		if self.MailServer != nil && self.MailServer.Port == 0 {
			errors.add("mailServer.port", "required when kind is mailServer, usually 25")
		}
	case AliasKindMailbox:
		if self.MailboxID == "" {
			errors.add("mailboxId", "required when kind is mailbox: the mailbox to deliver into")
		}
	case "":
		errors.add("kind", "required, one of null, email, webhook, mailServer or mailbox")
	default:
		errors.add("kind", "%q is not a kind, expected null, email, webhook, mailServer or mailbox", self.Kind)
	}
	return errors.ErrOrNil()
}

// Credential may authenticate to the submission port and send mail as its
// domain.
type Credential struct {
	// ID is what the SMTP username encodes; stored mail references it.
	ID string `json:"id"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	DomainID string `json:"domainId"`
	Position int    `json:"position"`

	// Key is the secret half of the credential. Never shown after creation.
	Key string `json:"-"`

	// Alias, when set, restricts this credential to sending as exactly that
	// local part of the domain.
	Alias string `json:"alias,omitempty"`

	// Comment names the device or service that holds this credential.
	Comment string `json:"comment,omitempty"`

	// Disabled refuses authentication without deleting the credential.
	Disabled bool `json:"disabled,omitempty"`
}

// Validate reports everything wrong with the credential.
func (self *Credential) Validate() error {
	var errors ValidationErrors
	if self.Key == "" {
		errors.add("key", "required: the secret half of the credential")
	}
	return errors.ErrOrNil()
}
