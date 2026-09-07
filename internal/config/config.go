// Package config defines the TeaNode configuration and the store that owns it.
//
// The configuration is the server's settings: how it is reached, how it
// speaks SMTP, which optional integrations are on, and the secrets it signs
// with. It is stored in the database, one section per row, so several
// instances share one answer and a change made in the web UI reaches all of
// them.
//
// Domains, aliases, credentials, users, roles, groups and mailboxes are not
// settings. They are rows in tables of their own, in internal/models and
// internal/db.
//
// The YAML in this package is what "config export" writes and "config import"
// reads, which is how the settings are described in a file, reviewed and
// loaded. It is not what the running server reads.
package config

import (
	"github.com/op/go-logging"
	"time"
)

var log = logging.MustGetLogger("config")

// Configuration is every setting an operator can change.
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
	// A release that crashes before it finishes starting is recovered from
	// automatically only where the binary is staged: a container runs the one
	// in its image again. Where it was replaced in place there is nothing to
	// fall back to on its own, and recovery is renaming the .previous copy
	// back by hand. See docs/configuration.md.
	//
	// It is refused, with the reason on the dashboard, where there is nowhere
	// to put the new binary: a deployment whose executable it cannot write
	// over and which has not been given a writable staging directory in
	// TEANODE_UPGRADE_DIRECTORY. A container is not refused — it stages onto
	// its volume and runs that at the next start, so an upgrade survives a
	// recreate — but a container that was never given such a volume is, and
	// there "docker compose pull" is the answer.
	Automatic bool `yaml:"automatic"`

	// CheckInterval is how often to look. Six hours by default: often enough
	// that a security release is noticed the same day, rarely enough that it
	// is not a request anybody would notice. The one part of this section
	// that a restart is needed for: the others are re-read every time the
	// loop wakes. At least
	// MinimumUpgradeCheckInterval, because the endpoint allows sixty requests
	// an hour to an address that is not signed in and this is not the only
	// thing behind that address.
	CheckInterval Duration `yaml:"checkInterval"`

	// Window restricts automatic upgrades to a time of day, in local time,
	// as "02:00-04:00". It may cross midnight. Empty means any time.
	//
	// An upgrade restarts the server, which takes a few seconds during which
	// mail is not accepted — senders retry, but a busy hour is still a worse
	// time than a quiet one.
	Window string `yaml:"window"`
}

// MinimumUpgradeCheckInterval is the shortest upgrade.checkInterval that is
// accepted.
const MinimumUpgradeCheckInterval = Duration(15 * time.Minute)

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

	// IMAP serves mailboxes to mail programs, with STARTTLS required before
	// signing in. Port 143 in production.
	IMAP string `yaml:"imap"`

	// IMAPS serves the same over TLS from the first byte. Port 993 in
	// production, which is what most mail programs try first.
	IMAPS string `yaml:"imaps"`

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
	deprecatedAcme `yaml:",inline"`

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

// Database points at the PostgreSQL server, which holds everything: the
// configuration, the signing keys, mail, deliveries, DMARC reports, usage
// counters and mail templates. It is the one thing worth backing up.
//
// These settings are the exception to configuration living in the database,
// for the obvious reason: they are how it is reached. They come from
// TEANODE_DATABASE_URL and are read on every start.
type Database struct {
	Host     string `yaml:"host"`
	Port     uint16 `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password" secret:"true"`
	Name     string `yaml:"name"`

	// SSLMode is passed to the PostgreSQL driver: disable, allow, prefer,
	// require, verify-ca or verify-full.
	//
	// "require" encrypts the connection but believes whatever answers on the
	// port. "verify-full" also checks that the certificate is signed by
	// SSLRootCertificate and names the host being dialled, which is what stops
	// something else on the network from answering as the database.
	SSLMode string `yaml:"sslMode"`

	// SSLRootCertificate is the PEM file the server's certificate is checked
	// against, for the two verify modes. The compose file generates one and
	// mounts it at /certs/server.crt; a managed PostgreSQL will publish its
	// own. Empty means the system trust store, which a self-signed certificate
	// is not in.
	SSLRootCertificate string `yaml:"sslRootCertificate,omitempty"`

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
	deprecatedDkim `yaml:",inline"`

	// Selector to give a newly created domain's key. It appears in DNS as
	// <selector>._domainkey.<domain>, so it only has to be unique within the
	// domain, and changing it here does not affect domains already created.
	Selector string `yaml:"selector"`
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

// Antispam configures spam scoring. The score a message is compared against
// is the domain's spamFilterScoreThreshold, not a setting here.
type Antispam struct {
	Enabled bool `yaml:"enabled"`

	// Engine chooses what does the scoring: "builtin" for the filter inside
	// this server, or "spamd" for an external SpamAssassin daemon.
	//
	// Empty is resolved rather than defaulted, so that an existing deployment
	// keeps working without being edited: empty with a host configured means
	// "spamd", and empty with no host means "builtin". Read it with
	// ResolvedEngine(), never directly.
	Engine string `yaml:"engine,omitempty"`

	// Spamd is where the external daemon listens, used when Engine is
	// "spamd".
	Spamd AntispamSpamd `yaml:"spamd"`

	// Builtin configures the filter inside this server.
	Builtin AntispamBuiltin `yaml:"builtin"`

	// Host and Port are where the external daemon listens.
	//
	// Deprecated: use Spamd. Kept because it is what deployments in the field
	// have stored, and it still works.
	Host string `yaml:"host,omitempty"`
	Port uint16 `yaml:"port,omitempty"`
}

// AntispamEngineBuiltin and AntispamEngineSpamd are the values Engine takes.
const (
	AntispamEngineBuiltin = "builtin"
	AntispamEngineSpamd   = "spamd"
)

// ResolvedEngine says which filter scores messages, resolving the empty
// value.
//
// An upgrade must not move a server that is talking to a daemon onto a
// different filter behind the operator's back, and must not ask a new
// installation to configure a daemon it does not have. So an unset engine
// means "spamd" when a host is configured and "builtin" when none is.
func (self *Antispam) ResolvedEngine() string {
	switch self.Engine {
	case AntispamEngineBuiltin, AntispamEngineSpamd:
		return self.Engine
	}
	if self.SpamdHost() != "" {
		return AntispamEngineSpamd
	}
	return AntispamEngineBuiltin
}

// SpamdHost is where the daemon listens, preferring the current setting over
// the deprecated one.
func (self *Antispam) SpamdHost() string {
	if self.Spamd.Host != "" {
		return self.Spamd.Host
	}
	return self.Host
}

// SpamdPort is the port the daemon listens on, preferring the current
// setting over the deprecated one.
func (self *Antispam) SpamdPort() uint16 {
	if self.Spamd.Port != 0 {
		return self.Spamd.Port
	}
	return self.Port
}

// AntispamSpamd points at an external SpamAssassin daemon.
type AntispamSpamd struct {
	Host string `yaml:"host,omitempty"`
	Port uint16 `yaml:"port,omitempty"`
}

// AntispamBuiltin configures the filter inside this server.
type AntispamBuiltin struct {
	// Signals scores what the server already established about a message:
	// its authentication results, the sending host's confirmed reverse DNS
	// name, and the name it gave in HELO. Costs no lookups, because all of
	// it is computed before scoring begins.
	Signals AntispamSignals `yaml:"signals"`

	// DNS scores reputation lookups in public block lists.
	DNS AntispamDNS `yaml:"dns"`

	// Bayes scores a classifier trained on this server's own mail.
	Bayes AntispamBayes `yaml:"bayes"`

	// Rules scores public pattern rules, downloaded into the database.
	Rules AntispamRules `yaml:"rules"`
}

// AntispamSignals configures scoring from what the server already knows.
type AntispamSignals struct {
	Enabled bool `yaml:"enabled"`
}

// AntispamDNS configures block list lookups.
type AntispamDNS struct {
	Enabled bool `yaml:"enabled"`

	// Timeout bounds the whole set of lookups for one message.
	Timeout Duration `yaml:"timeout,omitempty"`

	// AddressLists are consulted about the connecting address.
	AddressLists []AntispamList `yaml:"addressLists,omitempty"`

	// DomainLists are consulted about domains found in the message.
	DomainLists []AntispamList `yaml:"domainLists,omitempty"`

	// MaximumDomains caps how many domains from one message are looked up,
	// so that a message full of links is not a burst of DNS queries.
	MaximumDomains int `yaml:"maximumDomains,omitempty"`
}

// AntispamList is one block list.
type AntispamList struct {
	// Zone is the suffix queries are built with, for example
	// zen.spamhaus.org.
	Zone string `yaml:"zone"`

	// Weight is the points a listing contributes.
	Weight float64 `yaml:"weight"`
}

// AntispamBayes configures the classifier.
type AntispamBayes struct {
	Enabled bool `yaml:"enabled"`

	// MinimumMessages is how many messages must have been learned before the
	// classifier is allowed to contribute. A classifier trained on four
	// messages is confidently wrong.
	MinimumMessages int64 `yaml:"minimumMessages,omitempty"`

	// Weight scales its opinion, which it expresses between -1 and 1.
	Weight float64 `yaml:"weight,omitempty"`
}

// AntispamRules configures the public pattern rules.
type AntispamRules struct {
	// Enabled is off by default: an upgrade should not begin downloading and
	// running rule files nobody asked for.
	Enabled bool `yaml:"enabled"`

	// Channels are the update channels to fetch, by name.
	Channels []string `yaml:"channels,omitempty"`

	// UpdateInterval is how often to look for a new version.
	UpdateInterval Duration `yaml:"updateInterval,omitempty"`

	// MaximumEvaluationTime bounds one message's whole rule pass. Thousands
	// of patterns run over text an attacker chose, so this is a limit rather
	// than a target.
	MaximumEvaluationTime Duration `yaml:"maximumEvaluationTime,omitempty"`
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
