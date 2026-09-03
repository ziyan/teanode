// Package config defines the TeaNode configuration file and the store that
// owns it.
//
// The configuration file, by convention /opt/teanode/teanode.yaml, is the
// single source of truth for everything an operator can change: which domains
// are served, which aliases forward where, which credentials may relay mail,
// who may log into the dashboard, and which optional integrations are enabled.
// The running server may rewrite the file when configuration is changed
// through the dashboard, so the file is both hand-editable and machine
// written. Comments written by hand do not survive a machine write; a fixed
// explanatory header is re-emitted on every write.
package config

import (
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("config")

// Configuration is the whole of teanode.yaml.
type Configuration struct {
	// Server identity and where runtime state is kept
	Server Server `yaml:"server"`

	// Addresses to listen on
	Listen Listen `yaml:"listen"`

	// Certificates for SMTP STARTTLS and the dashboard
	TLS TLS `yaml:"tls"`

	// Where mail metadata is stored
	Database Database `yaml:"database"`

	// SMTP behaviour shared by the incoming and outgoing listeners
	SMTP SMTP `yaml:"smtp"`

	// The key used to sign outgoing mail
	DKIM DKIM `yaml:"dkim"`

	// Domains served by this server, with their aliases and credentials
	Domains []*Domain `yaml:"domains"`

	// People who may administer this server, through the dashboard or the
	// API. Not a dashboard setting: they are the server's operators, and the
	// dashboard is only one of the things they use.
	Users []*User `yaml:"users"`

	// How long a login lasts, and the key that signs it
	Session Session `yaml:"session"`

	// How the server resolves DNS when checking a domain's records
	DNS DNS `yaml:"dns"`

	// Optional virus scanning through ClamAV
	Antivirus Antivirus `yaml:"antivirus"`

	// Optional spam scoring through SpamAssassin
	Antispam Antispam `yaml:"antispam"`

	// Optional sender geolocation through a MaxMind database
	GeoIP GeoIP `yaml:"geoip"`

	// Optional off-box copies of stored messages
	Storage Storage `yaml:"storage"`

	// Signing in with a passkey, and where the half-finished ceremonies wait
	Passkey Passkey `yaml:"passkey"`

	// Checking for new releases, and installing them
	Upgrade Upgrade `yaml:"upgrade"`

	// Directory holding the configuration file. Relative paths in the file
	// resolve against it rather than against the process working directory,
	// so that "teanode credential list" run from a different directory reads
	// the same secret the server does. Set by Load; not part of the file.
	baseDirectory string

	// Derived lookup tables, built on first use. Not part of the file: an
	// unexported field is invisible to the YAML encoder. A snapshot is
	// immutable once it is active, so the tables never go stale; Store
	// replaces the whole Configuration rather than editing one in place.
	index index
}

// Upgrade is how this server learns about new releases, and what it may do
// about them.
//
// Checking is on by default and installing is not. Knowing that a version
// exists is not the same as installing it, and an operator who is never told
// is an operator running last year's bugs — but a release can change how mail
// is handled, and nobody installs a mail server expecting it to change
// underneath them.
type Upgrade struct {
	// Enabled asks the release list what the newest version is, on
	// CheckInterval, and shows it in the dashboard. One HTTPS request to a
	// public endpoint, carrying nothing about this deployment.
	Enabled bool `yaml:"enabled"`

	// Automatic installs what it finds without being asked: download, verify
	// against the release's checksums, replace this binary, restart.
	//
	// It takes any newer release, minor and major alike. A rule that stopped
	// at a minor version would be a rule that quietly stopped upgrading, and
	// somebody who turns this on has said they would rather not think about
	// it. Read the changelog before turning it on, not after.
	//
	// It is refused, with the reason on the dashboard, where an upgrade could
	// not work: in a container, whose image is the thing to replace, and
	// where nothing would start the process again after it exits.
	Automatic bool `yaml:"automatic"`

	// CheckInterval is how often to look. Six hours by default: often enough
	// that a security release is noticed the same day, rarely enough that it
	// is not a request anybody would notice.
	CheckInterval Duration `yaml:"checkInterval"`

	// Window restricts automatic upgrades to a time of day, in local time,
	// as "02:00-04:00". It may cross midnight. Empty means any time.
	//
	// An upgrade restarts the server, which takes a few seconds during which
	// mail is not accepted — senders retry, but a busy hour is still a worse
	// time than a quiet one.
	Window string `yaml:"window"`
}

// Server describes this instance's identity and its state directory.
type Server struct {
	// Name announced in the SMTP greeting and used as the HELO name when
	// sending, for example "mail.example.com". It must resolve to this host
	// and its reverse DNS should match, or receiving servers will distrust
	// mail from here.
	Name string `yaml:"name"`

	// DataDirectory holds everything the server writes that is not in the
	// database: keys, certificates, the message spool and the secret.
	// Relative paths elsewhere in this file resolve against it.
	DataDirectory string `yaml:"dataDirectory"`

	// MailServers are the hosts to publish in every domain's MX records, in
	// order of preference. Optional; when empty the MX record names this
	// server, which is right for the single-host deployment most people run.
	//
	// Set it when mail for these domains arrives at more than one name — a
	// pair like mx1 and mx2 pointing at the same host is common, and gives
	// somewhere to move to without every domain having to change its DNS. The
	// dashboard then asks each domain for one MX record per name, at
	// preference 10, 20 and so on in the order given.
	//
	// These are names mail arrives at. They are unrelated to tls.hosts, which
	// is the names this server holds a certificate for.
	MailServers []string `yaml:"mailServers,omitempty"`

	// LogLevel is one of DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL.
	LogLevel string `yaml:"logLevel"`

	// LogDirectory, when set, receives a copy of every received message as a
	// .eml file. Useful when debugging; it grows without bound.
	LogDirectory string `yaml:"logDirectory,omitempty"`

	// Secret signs the bounce return path on outgoing mail and the passwords
	// derived from SMTP credential keys. Generated on first run.
	//
	// Changing it invalidates every SMTP password and orphans bounces for
	// mail already in flight, so when moving a server to a new machine this
	// has to come with it.
	Secret string `yaml:"secret" secret:"true"`
}

// Listen holds the addresses the server binds.
type Listen struct {
	// SMTPIncoming receives mail from the internet. Port 25 in production.
	SMTPIncoming string `yaml:"smtpIncoming"`

	// SMTPOutgoing receives authenticated mail from your own devices for
	// relaying. Port 587 in production.
	SMTPOutgoing string `yaml:"smtpOutgoing"`

	// HTTP serves the dashboard and answers ACME http-01 challenges. Port 80
	// must be reachable from the internet when tls.acme.challenge is http-01.
	HTTP string `yaml:"http"`

	// HTTPS serves the dashboard over TLS.
	HTTPS string `yaml:"https"`

	// Debug, when set, serves Go pprof endpoints. Bind it to localhost only.
	Debug string `yaml:"debug,omitempty"`
}

// TLS configures the certificate presented for STARTTLS and HTTPS.
type TLS struct {
	// Hosts to obtain certificates for. The first is the primary name.
	Hosts []string `yaml:"hosts"`

	// CertificateFile and PrivateKeyFile point at PEM files you manage
	// yourself. When both are set, ACME is not used.
	CertificateFile string `yaml:"certificateFile,omitempty"`
	PrivateKeyFile  string `yaml:"privateKeyFile,omitempty"`

	// ACME obtains certificates automatically from Let's Encrypt or another
	// ACME provider.
	ACME ACME `yaml:"acme"`
}

// ACME configures automatic certificate issuance.
type ACME struct {
	deprecatedACME `yaml:",inline"`

	Enabled bool `yaml:"enabled"`

	// Email is the contact address registered with the ACME provider; it
	// receives expiry warnings.
	Email string `yaml:"email"`

	// DirectoryURL is the ACME provider. Point it at the Let's Encrypt
	// staging directory while testing to avoid rate limits.
	DirectoryURL string `yaml:"directoryUrl"`

	// Challenge is how domain control is proven: "http-01" needs port 80
	// reachable, "tls-alpn-01" needs port 443, "dns-01" needs a DNS provider
	// below and is the only way to obtain a wildcard certificate.
	Challenge string `yaml:"challenge"`

	// PerDomain obtains a certificate for each domain's own mail server name,
	// as well as the server's own. Without it every domain is served the
	// server's certificate, which names a domain the sender did not ask for.
	//
	// Off by default, so that upgrading a server does not make it ask a
	// certificate authority for one certificate per domain it serves without
	// anybody deciding to.
	PerDomain bool `yaml:"perDomain"`

	// AccountKey identifies this server to the certificate authority.
	// Generated on first use and kept here with the other secrets; losing it
	// means registering again, which works but spends rate limit.
	AccountKey string `yaml:"accountKey" secret:"true"`

	// Certificate and PrivateKey hold the issued certificate, in PEM. They
	// are written here by the renewal, so a configuration file is the whole
	// of a working server: restoring one elsewhere keeps the certificate
	// instead of asking the authority for another and spending rate limit.
	//
	// This is the opposite choice from tls.certificateFile above, and for a
	// reason: these are written by this server and read by nothing else,
	// whereas a certificate you manage yourself is written by something else
	// and has to stay where that something else puts it.
	Certificate string `yaml:"certificate,omitempty"`
	PrivateKey  string `yaml:"privateKey,omitempty" secret:"true"`

	// Route53 solves dns-01 challenges using an AWS hosted zone.
	Route53 Route53 `yaml:"route53"`
}

// Route53 configures the optional AWS Route53 dns-01 solver.
type Route53 struct {
	Enabled bool `yaml:"enabled"`

	// ZoneID is the hosted zone that contains the records for tls.hosts.
	ZoneID string `yaml:"zoneId"`

	Region string `yaml:"region"`

	// AccessKeyID and SecretAccessKey are AWS credentials kept here with the
	// other secrets, so that one file is the whole of a working server.
	//
	// Leave both empty to use the default AWS credential chain instead: the
	// environment, a shared credentials file, or an instance role. On EC2 an
	// instance role is the better answer, because there is no long-lived
	// secret to leak.
	AccessKeyID     string `yaml:"accessKeyId,omitempty"`
	SecretAccessKey string `yaml:"secretAccessKey,omitempty" secret:"true"`

	// CredentialsFile is an AWS shared credentials file, as an alternative to
	// the two fields above.
	CredentialsFile string `yaml:"credentialsFile,omitempty"`

	// Nameservers to query when checking that a challenge record has
	// propagated, for example "ns-1.example.net:53".
	Nameservers []string `yaml:"nameservers,omitempty"`
}

// Database points at the PostgreSQL server that stores mail, deliveries,
// DMARC reports, usage counters and mail templates. Configuration is not
// stored there; this file is.
type Database struct {
	Host     string `yaml:"host"`
	Port     uint16 `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password" secret:"true"`
	Name     string `yaml:"name"`

	// SSLMode is passed to the PostgreSQL driver: disable, allow, prefer,
	// require, verify-ca or verify-full.
	SSLMode string `yaml:"sslMode"`

	// LogQueries echoes every SQL statement to the log. Very noisy.
	LogQueries bool `yaml:"logQueries"`
}

// SMTP holds behaviour shared by both SMTP listeners.
type SMTP struct {
	// TrustedSenders are domains whose mail skips the greylisting delay
	// applied to unknown senders.
	TrustedSenders []string `yaml:"trustedSenders"`

	// MaxMessageSize is the largest message accepted, for example "70MB".
	MaxMessageSize ByteSize `yaml:"maxMessageSize"`

	// MaxRecipientsIncoming limits recipients per inbound message; a low
	// value frustrates address harvesting.
	MaxRecipientsIncoming int `yaml:"maxRecipientsIncoming"`

	// MaxRecipientsOutgoing limits recipients per relayed message.
	MaxRecipientsOutgoing int `yaml:"maxRecipientsOutgoing"`

	// GreylistDelay is how long an unknown sender is stalled before the
	// message is accepted. Zero disables the delay.
	GreylistDelay Duration `yaml:"greylistDelay"`

	// AuthRateLimit is how many authentication attempts one address may make
	// per minute on the submission port, and AuthRateBurst how many it may
	// make at once before that rate applies.
	//
	// Verifying a credential is an HMAC and a comparison, which is fast, so
	// without a limit an address can guess at whatever rate the network
	// allows. The defaults let a mail client retry a mistyped password
	// several times and stop a program working through a list.
	//
	// Zero for either disables the limit.
	AuthRateLimit int `yaml:"authRateLimit"`
	AuthRateBurst int `yaml:"authRateBurst"`

	// RequireReverseDNS refuses incoming mail from an address with no reverse
	// DNS record that resolves back to it. On by default: it is cheap, and
	// most spam comes from hosts that have none.
	//
	// Turn it off when this server does not see the real client address —
	// behind a load balancer, on a private network, or in a container network
	// during a test — because there the check refuses everything.
	RequireReverseDNS bool `yaml:"requireReverseDns"`

	// SOCKS5Proxy, when set, routes outbound SMTP through a proxy. Useful
	// where the host's own IP address has a poor reputation or port 25 is
	// blocked outbound.
	SOCKS5Proxy string `yaml:"socks5Proxy,omitempty"`

	// Relay hands outgoing mail to one mail server instead of delivering it
	// by MX lookup.
	Relay Relay `yaml:"relay"`

	// Submission is the address to tell a mail client to use, when it is not
	// the one this server listens on.
	Submission Submission `yaml:"submission"`

	// DisableSend stops the server from actually delivering mail. Deliveries
	// are recorded and left undelivered. Use on a development machine.
	DisableSend bool `yaml:"disableSend"`
}

// Submission is what a mail client should be told to connect to.
//
// Normally that is server.name and the port in listen.smtpOutgoing, and this
// can be left empty. It exists for the deployment where those are not the
// same thing: a container publishing 10587, or a firewall forwarding 587 to
// something else. The server binds what it binds; the person setting up their
// phone needs the number they can actually reach, and getting it wrong sends
// them to a port nothing answers on.
//
// It changes nothing about what the server does — only what it says.
type Submission struct {
	// Host to connect to. Empty means server.name.
	Host string `yaml:"host,omitempty"`

	// Port to connect to. Zero means the port in listen.smtpOutgoing.
	Port uint16 `yaml:"port,omitempty"`
}

// Relay is a mail server that outgoing mail is handed to, instead of being
// delivered by looking up the recipient's MX and connecting to it on port 25.
//
// The reason most people need one is that their connection blocks outbound 25
// — almost every domestic ISP and many hosting providers do, precisely because
// that is how a compromised machine sends spam. A relay is reached on a
// submission port instead: 587 and 2525 with STARTTLS, or 465 with TLS from
// the first byte.
//
// It is also how this server sends through a provider. Amazon SES, Postmark,
// Resend and the rest all offer an SMTP endpoint on those ports, so pointing
// this at one needs no code that knows anything about them, and the message
// arrives carrying the DKIM signature this server already applied.
//
// Mail forwarded by an alias of kind "mailServer" is not affected: that names
// its own destination, which is the point of it.
type Relay struct {
	Enabled bool `yaml:"enabled"`

	// Host and Port of the relay, for example smtp.gmail.com and 587.
	Host string `yaml:"host"`
	Port uint16 `yaml:"port"`

	// Security is how TLS is used: "starttls" for 587 and 2525, "tls" for
	// 465, or "none" on a trusted network.
	//
	// Unlike delivering to a stranger's MX, the certificate is checked
	// against Host for both encrypted modes. There is a name to check and a
	// password about to be sent, so accepting any certificate would mean
	// handing that password to whoever answered.
	Security RelaySecurity `yaml:"security"`

	// Username and Password authenticate to it. Leave both empty for a relay
	// that authorises by address instead.
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty" secret:"true"`
}

// RelaySecurity is how a relay connection is encrypted.
type RelaySecurity string

const (
	// RelaySecurityStartTLS negotiates TLS with STARTTLS and refuses to
	// continue if the relay does not offer it. Ports 587 and 2525.
	RelaySecurityStartTLS RelaySecurity = "starttls"

	// RelaySecurityTLS expects TLS from the first byte. Port 465.
	RelaySecurityTLS RelaySecurity = "tls"

	// RelaySecurityNone does not insist. STARTTLS is still used when the
	// relay offers it — there is no reason to refuse encryption that is
	// there — but the certificate is not checked and a relay without it is
	// accepted. For one on the same machine or a private network, and
	// refused when a password is set.
	RelaySecurityNone RelaySecurity = "none"
)

// DKIM holds the defaults applied to a domain's signing key when one is
// created. The keys themselves are per domain, in domains[].dkim.
type DKIM struct {
	deprecatedDKIM `yaml:",inline"`

	// Selector to give a newly created domain's key. It appears in DNS as
	// <selector>._domainkey.<domain>, so it only has to be unique within the
	// domain, and changing it here does not affect domains already created.
	Selector string `yaml:"selector"`
}

// DomainCertificate is the TLS certificate for one domain's mail server name.
//
// Kept with the rest of the configuration rather than in a file beside it, for
// the same reason the signing key is: one thing to back up, and restoring it
// elsewhere restores a working server rather than one that has to obtain
// everything again.
type DomainCertificate struct {
	// Certificate is the issued chain, in PEM.
	Certificate string `yaml:"certificate,omitempty"`

	// PrivateKey is its private half, in PEM. Encrypted in the database the
	// same way the signing key is.
	PrivateKey string `yaml:"privateKey,omitempty" secret:"true"`
}

// DomainKey is a domain's DKIM signing key.
//
// The key lives in this file rather than in a separate PEM alongside it. A
// self-hoster then has one file to back up and one to copy to a new machine,
// and creating a domain in the dashboard can generate a key without also
// having to manage files. It does mean this file is secret; it is written
// with mode 600 and should be treated like any other private key.
type DomainKey struct {
	// Selector this key is published under.
	Selector string `yaml:"selector"`

	// PrivateKey is a PKCS#8 RSA key in PEM form.
	PrivateKey string `yaml:"privateKey" secret:"true"`
}

// Domain is a mail domain served by this instance.
type Domain struct {
	// ID identifies the domain in stored mail, deliveries and usage rows, and
	// in dashboard URLs. It is the domain name: unique already, stable for as
	// long as the domain is configured, and readable wherever it appears.
	//
	// Older configurations carry a generated identifier here instead, and
	// keep working — the rows that reference it still match. Changing it means
	// updating those rows too, so nothing rewrites it automatically.
	ID string `yaml:"id"`

	// Domain is the mail domain itself, for example "example.com".
	Domain string `yaml:"domain"`

	// Subdomain is the label whose CNAME points at this server, so that
	// bounces and DMARC reports have somewhere to arrive. Usually "mail",
	// making mail.example.com a CNAME to server.name.
	Subdomain string `yaml:"subdomain"`

	// MailServers are the names this domain's MX records point at, and so
	// the names a sender connects to and is handed a certificate for.
	//
	// Empty means one name derived from the domain itself, "mx." in front of
	// it, which is what most people want and what the DNS panel then asks
	// for. Set it to publish something else: a pair of names for the look of
	// redundancy, a name that is not "mx", or the server's own name — which
	// is how a domain points at a host in somebody else's zone, the
	// arrangement this defaulted to before it was configurable.
	//
	// A name inside this domain is one the operator publishes an address
	// record for, and one this server can obtain a certificate for. A name
	// outside it is neither: it belongs to whoever owns that zone.
	MailServers []string `yaml:"mailServers,omitempty"`

	// LinkHost is the name in the addresses this server writes into mail it
	// sends — today the pictures in a template, each one an address belonging
	// to a single message.
	//
	// Empty means the domain's first mail server name, which is right when
	// this server answers HTTPS on it. It often does not: a mail server name
	// resolves to a host whose port 443 belongs to something else entirely,
	// and then every picture in every message is broken while the mail itself
	// is fine. The name here is a way to say where this domain's HTTPS
	// actually is — the site on the apex behind a CDN, a name pointed at this
	// server for the purpose — without moving where its mail arrives.
	//
	// It has to be a name that reaches this server over HTTPS with a
	// certificate a mail program will accept, and it has to be under this
	// domain or under one of its own: an address in somebody else's domain
	// tells the reader who runs the server, which is the thing per-domain
	// names exist to stop.
	LinkHost string `yaml:"linkHost,omitempty"`

	// Comment is a note for the operator; it is never used in mail handling.
	Comment string `yaml:"comment,omitempty"`

	// DKIM is the key that signs mail sent from this domain. It is generated
	// when the domain is created; the matching public key has to be published
	// in DNS, which the dashboard shows you.
	DKIM DomainKey `yaml:"dkim"`

	// TLS is the certificate served to a sender connecting to this domain's
	// own mail server name, obtained automatically when tls.acme.perDomain is
	// on. Without one, a sender is served the server's own certificate, which
	// works — almost every sender accepts a name that does not match — but
	// tells it the name of a domain it did not ask for.
	TLS DomainCertificate `yaml:"tls,omitempty"`

	// SpamFilterScoreThreshold is the SpamAssassin score at or above which
	// mail is rejected. Only meaningful when antispam is enabled.
	SpamFilterScoreThreshold float64 `yaml:"spamFilterScoreThreshold"`

	// Aliases decide where mail for this domain goes. Every alias whose
	// pattern matches produces a delivery, so one address can forward to
	// several places; an alias with an empty pattern is a catch-all and
	// receives only what nothing else matched.
	Aliases []*Alias `yaml:"aliases"`

	// Credentials may authenticate to the submission port and send mail as
	// this domain.
	Credentials []*Credential `yaml:"credentials,omitempty"`
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

	// AliasKindMailServer relays the mail to a specific mail server, which is
	// how you put a real mailbox server behind TeaNode.
	AliasKindMailServer AliasKind = "mailServer"
)

// Alias matches recipient addresses and says where the mail goes.
type Alias struct {
	// ID is generated once and never changes; stored deliveries reference it.
	ID string `yaml:"id"`

	// Pattern is a Go regular expression matched against the local part of the
	// recipient address, the part before the "@", without regard to case.
	// Anchor it: "^hello$" matches only hello@, while "hello" also matches
	// say-hello-now@.
	//
	// An empty pattern makes this a catch-all. Catch-alls are a fallback: they
	// receive mail only for addresses that no pattern matched, so adding one
	// does not duplicate mail that already has somewhere to go.
	Pattern string `yaml:"pattern"`

	// Comment is a note for the operator.
	Comment string `yaml:"comment,omitempty"`

	// Kind is one of null, email, webhook or mailServer.
	Kind AliasKind `yaml:"kind"`

	// Email is the destination address when kind is email.
	Email string `yaml:"email,omitempty"`

	// Webhook is the destination URL when kind is webhook.
	Webhook string `yaml:"webhook,omitempty"`

	// MailServer is the destination server when kind is mailServer.
	MailServer *MailServer `yaml:"mailServer,omitempty"`

	// Disabled stops the alias from matching without deleting it.
	Disabled bool `yaml:"disabled,omitempty"`
}

// IsCatchAll reports whether this alias receives everything that no other
// alias matched. An empty pattern means exactly that.
func (self *Alias) IsCatchAll() bool {
	return self.Pattern == ""
}

// MailServer is a downstream SMTP server to relay to.
type MailServer struct {
	Host     string `yaml:"host"`
	Port     uint16 `yaml:"port"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty" secret:"true"`
}

// Credential is an SMTP AUTH identity for the submission port. The username
// and password an SMTP client uses are derived from the identifier and key
// together with the server secret; "teanode credential" prints them.
type Credential struct {
	// ID identifies the domain in stored mail, deliveries and usage rows, and
	// in dashboard URLs. It is the domain name: unique already, stable for as
	// long as the domain is configured, and readable wherever it appears.
	//
	// Older configurations carry a generated identifier here instead, and
	// keep working — the rows that reference it still match. Changing it means
	// updating those rows too, so nothing rewrites it automatically.
	ID string `yaml:"id"`

	// Key is the secret half of the credential.
	Key string `yaml:"key" secret:"true"`

	// Alias, when set, restricts this credential to sending as exactly that
	// local part of the domain. A credential for "noreply" cannot then send
	// as anybody else, which limits the damage if it leaks.
	Alias string `yaml:"alias,omitempty"`

	// Comment names the device or service that holds this credential.
	Comment string `yaml:"comment,omitempty"`

	// Disabled refuses authentication without deleting the credential.
	Disabled bool `yaml:"disabled,omitempty"`
}

// Passkey configures signing in with an authenticator rather than a password.
//
// WebAuthn binds a credential to one origin, permanently: a passkey registered
// against the wrong name is one that will never work again and cannot be
// repaired, only deleted. So the relying party is stated rather than guessed
// from whatever Host header a request happened to carry.
type Passkey struct {
	// Enabled offers passkeys on the sign-in form and in the account's
	// settings. Off by default: a server reached over plain HTTP, or on a
	// name that is about to change, is one where registering a passkey would
	// create something that cannot be used later.
	Enabled bool `yaml:"enabled"`

	// RelyingPartyID is the domain a credential is bound to, without a scheme
	// or a port — "mail.example.com", or "example.com" to let the same
	// passkey work on every subdomain. Empty means server.name.
	RelyingPartyID string `yaml:"relyingPartyId,omitempty"`

	// DisplayName is what the browser shows when it asks somebody to create
	// or use a passkey. Empty means server.name.
	DisplayName string `yaml:"displayName,omitempty"`

	// Origins the dashboard is served from, each with its scheme and any
	// non-default port: "https://mail.example.com". An assertion from an
	// origin that is not listed is refused, which is what stops a page on
	// another site from using these credentials. Empty means https:// and
	// the relying party.
	Origins []string `yaml:"origins,omitempty"`

	// MaximumPerUser bounds how many an account may register. Zero means
	// five, which is a phone, a laptop, a security key and room to replace
	// one before removing it.
	MaximumPerUser int `yaml:"maximumPerUser,omitempty"`

	// Redis, when set, is where half-finished ceremonies wait. Without it
	// they are kept in this process, which is correct for one instance and
	// wrong behind a load balancer: WebAuthn is two requests and the browser
	// has no reason to come back to the instance it started with.
	Redis Redis `yaml:"redis"`
}

// Redis is a shared place for short-lived state. Nothing durable is kept
// there: everything has a lifetime measured in minutes, and losing it costs
// one retry.
type Redis struct {
	// Address as host:port, for example "redis:6379". Empty disables it.
	Address string `yaml:"address,omitempty"`

	// Username and Password, when the server requires them.
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty" secret:"true"`

	// Database number, zero unless something else shares the server.
	Database int `yaml:"database,omitempty"`
}

// Session configures how a login is remembered.
//
// There is no setting for whether the dashboard runs: it always does, and
// where it is reachable is decided by listen.http and listen.https. Clearing
// both is how to serve no web interface at all.
type Session struct {
	// Key signs session cookies. Generated on first run; replacing it logs
	// everybody out, which is the way to do that deliberately.
	Key string `yaml:"key" secret:"true"`

	// Lifetime is how long a login lasts.
	Lifetime Duration `yaml:"lifetime"`

	deprecatedSession `yaml:",inline"`
}

// User is somebody who may administer this server.
type User struct {
	// ID is stable for the account's lifetime; the username is not, because
	// it can be changed. Sessions, API tokens and passkeys name the
	// identifier, so a rename does not invalidate them. Generated when the
	// account is stored, so an account written into a file by hand does not
	// have to invent one.
	ID string `yaml:"id,omitempty"`

	Username string `yaml:"username"`

	// Name is what to call this person, when they have said. The username is
	// what they sign in with, which is not always something to greet somebody
	// by. Optional.
	Name string `yaml:"name,omitempty"`

	// PasswordHash is a bcrypt hash. Generate one with "teanode password".
	PasswordHash string `yaml:"passwordHash" secret:"true"`

	// Email receives notifications, such as a domain whose DNS records have
	// stopped resolving. Optional.
	Email string `yaml:"email,omitempty"`
}

// DNS configures the resolver used to check that configured domains publish
// the records they need. It is not used for mail authentication, which has its
// own caching resolver.
type DNS struct {
	// Nameserver to query, as host:port. A public resolver is a reasonable
	// default because these are public records.
	Nameserver string `yaml:"nameserver"`

	// CheckInterval is how often every configured domain is re-checked.
	CheckInterval Duration `yaml:"checkInterval"`

	// ExternalAddressServices are asked what address this server appears to
	// come from, which is what its DNS records have to point at. A server
	// cannot work this out alone: the address on its interface is usually
	// private, and only something outside can say what a sending mail server
	// sees.
	//
	// Each is tried in turn until one answers. Empty disables the lookup, and
	// the dashboard then asks the operator for the address instead.
	ExternalAddressServices []string `yaml:"externalAddressServices"`
}

// Antivirus configures the optional ClamAV integration. Mail is scanned only
// when this is enabled; when disabled no connection is attempted.
type Antivirus struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    uint16 `yaml:"port"`
}

// Antispam configures the optional SpamAssassin integration.
type Antispam struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    uint16 `yaml:"port"`
}

// GeoIP configures optional sender geolocation. Supply your own MaxMind
// database; none is bundled.
type GeoIP struct {
	Enabled bool `yaml:"enabled"`

	// DatabaseFile is a MaxMind .mmdb file.
	DatabaseFile string `yaml:"databaseFile,omitempty"`
}

// Storage configures where raw messages are kept. The local spool under
// server.dataDirectory is always used; S3 is an optional mirror.
type Storage struct {
	// Directory holds the raw messages, relative to server.dataDirectory.
	// They are kept out of the database because they are large, are never
	// queried, and would make a backup expensive.
	Directory string `yaml:"directory"`

	// SpoolRetention is how long a message is kept, which is how far back the
	// dashboard can show content and how long a stalled delivery can still be
	// retried. Zero keeps messages forever and eventually fills the disk.
	SpoolRetention Duration `yaml:"spoolRetention"`

	S3 S3 `yaml:"s3"`
}

// S3 configures optional off-box message copies, in AWS S3 or anything that
// speaks the same protocol.
type S3 struct {
	Enabled bool   `yaml:"enabled"`
	Bucket  string `yaml:"bucket"`
	Region  string `yaml:"region"`

	// Endpoint points at an S3-compatible service that is not AWS, for
	// example "http://minio:9000". Empty means AWS.
	//
	// Self-hosting the object store is what makes several instances able to
	// share a spool without sharing a filesystem, and without an account
	// anywhere.
	Endpoint string `yaml:"endpoint,omitempty"`

	// PathStyle addresses the bucket as endpoint/bucket. Implied when an
	// endpoint is set, and only worth stating to be explicit.
	PathStyle bool `yaml:"pathStyle,omitempty"`

	// AccessKeyID and SecretAccessKey are AWS credentials kept here with the
	// other secrets, so that one file is the whole of a working server.
	//
	// Leave both empty to use the default AWS credential chain instead: the
	// environment, a shared credentials file, or an instance role. On EC2 an
	// instance role is the better answer, because there is no long-lived
	// secret to leak.
	AccessKeyID     string `yaml:"accessKeyId,omitempty"`
	SecretAccessKey string `yaml:"secretAccessKey,omitempty" secret:"true"`

	// CredentialsFile is an AWS shared credentials file, as an alternative to
	// the two fields above.
	CredentialsFile string `yaml:"credentialsFile,omitempty"`
}
