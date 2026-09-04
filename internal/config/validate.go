package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"regexp/syntax"
	"strings"
	"time"

	"github.com/op/go-logging"
	"golang.org/x/crypto/bcrypt"
)

// Error is a single problem found in the configuration file. Path is the YAML
// location, so that the operator can find it without counting lines.
type Error struct {
	Path    string
	Message string
}

func (self *Error) Error() string {
	return fmt.Sprintf("%s: %s", self.Path, self.Message)
}

// Errors is every problem found in one pass, so that fixing the file is not a
// game of whack-a-mole.
type Errors []*Error

func (self Errors) Error() string {
	messages := make([]string, 0, len(self))
	for _, err := range self {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "\n")
}

type validator struct {
	errors Errors
}

func (self *validator) add(path, format string, arguments ...interface{}) {
	self.errors = append(self.errors, &Error{Path: path, Message: fmt.Sprintf(format, arguments...)})
}

// Validate checks the shape of the configuration and reports every problem it
// finds at once, so that fixing the file is not a game of whack-a-mole.
//
// It deliberately does not look at the filesystem. Commands that create the
// files the configuration refers to, such as "teanode dkim generate", have to
// be able to load a configuration whose key does not exist yet. Call
// ValidateFiles as well before actually running the server.
func (self *Configuration) Validate() error {
	validator := &validator{}

	self.validateServer(validator)
	self.validateListen(validator)
	self.validateTLS(validator)
	self.validateDatabase(validator)
	self.validateSMTP(validator)
	self.validateDKIM(validator)
	self.validateDomains(validator)
	self.validateUsers(validator)
	self.validateIntegrations(validator)

	if len(validator.errors) == 0 {
		return nil
	}
	return validator.errors
}

func (self *Configuration) validateServer(validator *validator) {
	if self.Server.Name == "" {
		validator.add("server.name", "required: the host name this server announces over SMTP, for example mail.example.com")
	} else if !isHostname(self.Server.Name) {
		validator.add("server.name", "%q is not a host name", self.Server.Name)
	}
	for index, host := range self.Server.MailServers {
		path := fmt.Sprintf("server.mailServers[%d]", index)
		if strings.TrimSpace(host) == "" {
			validator.add(path, "cannot be empty; remove the entry instead")
		} else if !isHostname(host) {
			validator.add(path, "%q is not a host name", host)
		}
	}
	if self.Server.DataDirectory == "" {
		validator.add("server.dataDirectory", "required: a writable directory for keys, certificates and the message spool")
	}
	if self.Server.LogLevel != "" {
		if _, err := logging.LogLevel(strings.ToUpper(self.Server.LogLevel)); err != nil {
			validator.add("server.logLevel", "%q is not a log level, expected one of DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL", self.Server.LogLevel)
		}
	}
}

func (self *Configuration) validateListen(validator *validator) {
	addresses := map[string]string{
		"listen.smtpIncoming": self.Listen.SMTPIncoming,
		"listen.smtpOutgoing": self.Listen.SMTPOutgoing,
		"listen.http":         self.Listen.HTTP,
		"listen.https":        self.Listen.HTTPS,
		"listen.debug":        self.Listen.Debug,
	}
	required := map[string]bool{
		"listen.smtpIncoming": true,
		"listen.smtpOutgoing": true,
	}
	seen := make(map[string]string, len(addresses))
	for path, address := range addresses {
		if address == "" {
			if required[path] {
				validator.add(path, "required: an address to listen on, for example :25")
			}
			continue
		}
		if _, _, err := net.SplitHostPort(address); err != nil {
			validator.add(path, "%q is not a listen address, expected something like :25 or 127.0.0.1:2525", address)
			continue
		}
		if other, ok := seen[address]; ok {
			validator.add(path, "%q is already used by %s; two listeners cannot share an address", address, other)
			continue
		}
		seen[address] = path
	}
}

func (self *Configuration) validateTLS(validator *validator) {
	hasFiles := self.TLS.CertificateFile != "" || self.TLS.PrivateKeyFile != ""
	if hasFiles {
		if self.TLS.CertificateFile == "" {
			validator.add("tls.certificateFile", "required when tls.privateKeyFile is set")
		}
		if self.TLS.PrivateKeyFile == "" {
			validator.add("tls.privateKeyFile", "required when tls.certificateFile is set")
		}
	}

	if !self.TLS.ACME.Enabled {
		// With no certificate at all the server cannot serve HTTPS and cannot
		// offer STARTTLS, which is fine on a development box and wrong
		// everywhere else. It is only an error when something actually needs a
		// certificate to start; otherwise the server warns at startup.
		if !hasFiles && self.Listen.HTTPS != "" {
			validator.add("tls.acme.enabled", "no certificate source, but listen.https is set: either enable ACME, set tls.certificateFile and tls.privateKeyFile, or clear listen.https")
		}
		return
	}

	if len(self.TLS.Hosts) == 0 {
		validator.add("tls.hosts", "required when ACME is enabled: the host names to obtain a certificate for")
	}
	for index, host := range self.TLS.Hosts {
		if strings.HasPrefix(host, "*.") {
			if self.TLS.ACME.Challenge != ChallengeDNS01 {
				validator.add(fmt.Sprintf("tls.hosts[%d]", index), "wildcard %q requires tls.acme.challenge: dns-01", host)
			}
			continue
		}
		if !isHostname(host) {
			validator.add(fmt.Sprintf("tls.hosts[%d]", index), "%q is not a host name", host)
		}
	}
	if self.TLS.ACME.Email == "" {
		validator.add("tls.acme.email", "required when ACME is enabled: the address the certificate authority contacts about expiry")
	} else if !strings.Contains(self.TLS.ACME.Email, "@") {
		validator.add("tls.acme.email", "%q is not an email address", self.TLS.ACME.Email)
	}
	if self.TLS.ACME.DirectoryURL == "" {
		validator.add("tls.acme.directoryURL", "required when ACME is enabled")
	} else if parsed, err := url.Parse(self.TLS.ACME.DirectoryURL); err != nil || parsed.Scheme != "https" {
		validator.add("tls.acme.directoryURL", "%q is not an https URL", self.TLS.ACME.DirectoryURL)
	}

	switch self.TLS.ACME.Challenge {
	case ChallengeHTTP01:
		if self.Listen.HTTP == "" {
			validator.add("listen.http", "required for tls.acme.challenge http-01: the certificate authority connects to port 80")
		}
	case ChallengeTLSALPN01:
		if self.Listen.HTTPS == "" {
			validator.add("listen.https", "required for tls.acme.challenge tls-alpn-01: the certificate authority connects to port 443")
		}
	case ChallengeDNS01:
		if !self.TLS.ACME.Route53.Enabled {
			validator.add("tls.acme.route53.enabled", "required for tls.acme.challenge dns-01: no other DNS provider is implemented")
		} else if self.TLS.ACME.Route53.ZoneID == "" {
			validator.add("tls.acme.route53.zoneID", "required when the Route53 solver is enabled")
		}
	case "":
		validator.add("tls.acme.challenge", "required when ACME is enabled, one of http-01, tls-alpn-01 or dns-01")
	default:
		validator.add("tls.acme.challenge", "%q is not a challenge, expected http-01, tls-alpn-01 or dns-01", self.TLS.ACME.Challenge)
	}
}

func (self *Configuration) validateDatabase(validator *validator) {
	if self.Database.Host == "" {
		validator.add("database.host", "required: the PostgreSQL host")
	}
	if self.Database.Port == 0 {
		validator.add("database.port", "required: the PostgreSQL port, usually 5432")
	}
	if self.Database.User == "" {
		validator.add("database.user", "required: the PostgreSQL user")
	}
	if self.Database.Name == "" {
		validator.add("database.name", "required: the PostgreSQL database name")
	}
	switch self.Database.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	case "":
		validator.add("database.sslMode", "required, one of disable, allow, prefer, require, verify-ca, verify-full")
	default:
		validator.add("database.sslMode", "%q is not an SSL mode, expected one of disable, allow, prefer, require, verify-ca, verify-full", self.Database.SSLMode)
	}
}

func (self *Configuration) validateSMTP(validator *validator) {
	if self.SMTP.MaxMessageSize == 0 {
		validator.add("smtp.maxMessageSize", "required: the largest message to accept, for example 70MB")
	}
	if self.SMTP.MaxRecipientsIncoming <= 0 {
		validator.add("smtp.maxRecipientsIncoming", "must be at least 1")
	}
	if self.SMTP.MaxRecipientsOutgoing <= 0 {
		validator.add("smtp.maxRecipientsOutgoing", "must be at least 1")
	}
	if self.SMTP.SOCKS5Proxy != "" {
		if _, _, err := net.SplitHostPort(self.SMTP.SOCKS5Proxy); err != nil {
			validator.add("smtp.socks5Proxy", "%q is not a host:port address", self.SMTP.SOCKS5Proxy)
		}
	}
	for index, sender := range self.SMTP.TrustedSenders {
		if !isHostname(sender) {
			validator.add(fmt.Sprintf("smtp.trustedSenders[%d]", index), "%q is not a domain name", sender)
		}
	}

	self.validateRelay(validator)

	// Only checked when set: empty means "use what this server listens on",
	// which is the common case and needs nothing.
	if host := strings.TrimSpace(self.SMTP.Submission.Host); host != "" {
		if !isHostname(host) && net.ParseIP(host) == nil {
			validator.add("smtp.submission.host", "%q is not a host name or an address", host)
		}
	}
}

func (self *Configuration) validateRelay(validator *validator) {
	relay := self.SMTP.Relay
	if !relay.Enabled {
		return
	}

	if relay.Host == "" {
		validator.add("smtp.relay.host", "required when the relay is enabled: the mail server to hand outgoing mail to")
	} else if !isHostname(relay.Host) && net.ParseIP(relay.Host) == nil {
		validator.add("smtp.relay.host", "%q is not a host name or an address", relay.Host)
	}
	if relay.Port == 0 {
		validator.add("smtp.relay.port", "required when the relay is enabled, usually 587, 465 or 2525")
	}

	switch relay.Security {
	case RelaySecurityStartTLS, RelaySecurityTLS:
	case RelaySecurityNone:
		// A password in the clear is a password given away to anybody on the
		// path. Refused rather than warned about, because the failure is
		// silent and the fix is one word.
		if relay.Password != "" {
			validator.add("smtp.relay.security",
				"cannot be \"none\" when a password is set: it would be sent in the clear. Use \"starttls\" or \"tls\"")
		}
	default:
		validator.add("smtp.relay.security", "%q is not one of starttls, tls, none", relay.Security)
	}

	// A username with no password, or the other way around, authenticates as
	// nobody and fails at AUTH with a message about credentials rather than
	// about configuration.
	if (relay.Username == "") != (relay.Password == "") {
		validator.add("smtp.relay.username", "set both the username and the password, or neither")
	}
}

func (self *Configuration) validateDKIM(validator *validator) {
	if self.DKIM.Selector == "" {
		validator.add("dkim.selector", "required: the selector to give a new domain's signing key, for example teanode1")
	} else if !isHostLabel(self.DKIM.Selector) {
		validator.add("dkim.selector", "%q is not usable as a DNS label", self.DKIM.Selector)
	}
}

func (self *Configuration) validateDomains(validator *validator) {
	if len(self.Domains) == 0 {
		validator.add("domains", "required: at least one domain to receive mail for")
	}
	seenDomains := make(map[string]int, len(self.Domains))
	seenIds := make(map[string]string)
	for index, domain := range self.Domains {
		path := fmt.Sprintf("domains[%d]", index)
		if domain == nil {
			validator.add(path, "is empty")
			continue
		}
		if domain.ID == "" {
			validator.add(path+".id", "required: a stable identifier; stored mail references it")
		} else if other, ok := seenIds[domain.ID]; ok {
			validator.add(path+".id", "%q is already used by %s", domain.ID, other)
		} else {
			seenIds[domain.ID] = path
		}
		if domain.Domain == "" {
			validator.add(path+".domain", "required: the mail domain, for example example.com")
		} else if !isHostname(domain.Domain) {
			validator.add(path+".domain", "%q is not a domain name", domain.Domain)
		} else if previous, ok := seenDomains[strings.ToLower(domain.Domain)]; ok {
			validator.add(path+".domain", "%q is already configured as domains[%d]", domain.Domain, previous)
		} else {
			seenDomains[strings.ToLower(domain.Domain)] = index
		}
		if domain.Subdomain != "" && !isHostLabel(domain.Subdomain) {
			validator.add(path+".subdomain", "%q is not a single host label, for example mail", domain.Subdomain)
		}
		for index, host := range domain.MailServers {
			// A blank entry is a trailing comma in a form, which means
			// nothing and is dropped rather than refused.
			if strings.TrimSpace(host) == "" {
				continue
			}
			if !isHostname(strings.TrimSuffix(strings.TrimSpace(host), ".")) {
				validator.add(fmt.Sprintf("%s.mailServers[%d]", path, index),
					"%q is not a host name, for example mx.%s", host, domain.Domain)
			}
		}
		if host := strings.TrimSuffix(strings.TrimSpace(domain.LinkHost), "."); host != "" {
			if !isHostname(host) {
				validator.add(path+".linkHost", "%q is not a host name, for example %s", domain.LinkHost, domain.Domain)
			} else if !domain.InThisDomain(host) {
				// A name in somebody else's domain would put that domain in
				// front of every recipient who reads where a picture came
				// from, which is exactly what a per-domain name exists to
				// avoid. It is also a name this server cannot get a
				// certificate for.
				validator.add(path+".linkHost", "%q is not under %s; an address in another domain names whoever runs that one", host, domain.Domain)
			}
		}
		if domain.SpamFilterScoreThreshold < 0 {
			validator.add(path+".spamFilterScoreThreshold", "must not be negative")
		}
		self.validateDomainKey(validator, path, domain)
		self.validateAliases(validator, path, domain, seenIds)
		self.validateCredentials(validator, path, domain, seenIds)
	}
}

// validateDomainKey checks a domain's signing key. A domain with no key is
// allowed: it can still receive mail, it just cannot sign what it sends, and
// the dashboard offers to generate one.
func (self *Configuration) validateDomainKey(validator *validator, domainPath string, domain *Domain) {
	if domain.DKIM.Selector == "" && domain.DKIM.PrivateKey == "" {
		return
	}
	if domain.DKIM.Selector == "" {
		validator.add(domainPath+".dkim.selector", "required when a signing key is set")
	} else if !isHostLabel(domain.DKIM.Selector) {
		validator.add(domainPath+".dkim.selector", "%q is not usable as a DNS label", domain.DKIM.Selector)
	}
	if domain.DKIM.PrivateKey == "" {
		validator.add(domainPath+".dkim.privateKey", "required when a selector is set; generate one in the dashboard or with 'teanode dkim generate'")
		return
	}
	if _, err := domain.DKIM.Signer(); err != nil {
		validator.add(domainPath+".dkim.privateKey", "cannot be used: %s", err)
	}
}

func (self *Configuration) validateAliases(validator *validator, domainPath string, domain *Domain, seenIds map[string]string) {
	for index, alias := range domain.Aliases {
		path := fmt.Sprintf("%s.aliases[%d]", domainPath, index)
		if alias == nil {
			validator.add(path, "is empty")
			continue
		}
		if alias.ID == "" {
			validator.add(path+".id", "required: a stable identifier; stored deliveries reference it")
		} else if other, ok := seenIds[alias.ID]; ok {
			validator.add(path+".id", "%q is already used by %s", alias.ID, other)
		} else {
			seenIds[alias.ID] = path
		}
		// An empty pattern is a catch-all rather than a mistake.
		if alias.Pattern != "" {
			if _, err := regexp.Compile(alias.Pattern); err != nil {
				validator.add(path+".pattern", "invalid regular expression: %s", regexpErrorMessage(err))
			}
		}
		switch alias.Kind {
		case AliasKindNull:
		case AliasKindEmail:
			if alias.Email == "" {
				validator.add(path+".email", "required when kind is email: the address to forward to")
			} else if !isEmailAddress(alias.Email) {
				validator.add(path+".email", "%q is not an email address", alias.Email)
			}
		case AliasKindWebhook:
			if alias.Webhook == "" {
				validator.add(path+".webhook", "required when kind is webhook: the URL to post to")
			} else if parsed, err := url.Parse(alias.Webhook); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				validator.add(path+".webhook", "%q is not an http or https URL", alias.Webhook)
			}
		case AliasKindMailServer:
			if alias.MailServer == nil || alias.MailServer.Host == "" {
				validator.add(path+".mailServer.host", "required when kind is mailServer: the server to relay to")
			} else if !isRelayHost(alias.MailServer.Host) {
				validator.add(path+".mailServer.host", "%q is not a host name or address", alias.MailServer.Host)
			}
			if alias.MailServer != nil && alias.MailServer.Port == 0 {
				validator.add(path+".mailServer.port", "required when kind is mailServer, usually 25")
			}
		case "":
			validator.add(path+".kind", "required, one of null, email, webhook or mailServer")
		default:
			validator.add(path+".kind", "%q is not a kind, expected null, email, webhook or mailServer", alias.Kind)
		}
	}
}

func (self *Configuration) validateCredentials(validator *validator, domainPath string, domain *Domain, seenIds map[string]string) {
	for index, credential := range domain.Credentials {
		path := fmt.Sprintf("%s.credentials[%d]", domainPath, index)
		if credential == nil {
			validator.add(path, "is empty")
			continue
		}
		if credential.ID == "" {
			validator.add(path+".id", "required: a stable identifier; stored mail references it")
		} else if other, ok := seenIds[credential.ID]; ok {
			validator.add(path+".id", "%q is already used by %s", credential.ID, other)
		} else {
			seenIds[credential.ID] = path
		}
		if credential.Key == "" {
			validator.add(path+".key", "required: the secret half of the credential")
		}
	}
}

// validateUsers checks the people who may administer this server. An empty
// list is allowed and means the server has not been claimed yet: it then shows
// a setup screen asking the first visitor to create an account.
func (self *Configuration) validateUsers(validator *validator) {
	if self.Session.Lifetime <= 0 {
		validator.add("session.lifetime", "must be positive, for example 30d")
	}

	seenUsernames := make(map[string]int, len(self.Users))
	for index, user := range self.Users {
		path := fmt.Sprintf("users[%d]", index)
		if user == nil {
			validator.add(path, "is empty")
			continue
		}
		if user.Username == "" {
			validator.add(path+".username", "required")
		} else if previous, ok := seenUsernames[user.Username]; ok {
			validator.add(path+".username", "%q is already used by users[%d]", user.Username, previous)
		} else {
			seenUsernames[user.Username] = index
		}
		if user.PasswordHash == "" {
			validator.add(path+".passwordHash", "required: a bcrypt hash, generate one with 'teanode password'")
		} else if _, err := bcrypt.Cost([]byte(user.PasswordHash)); err != nil {
			validator.add(path+".passwordHash", "is not a bcrypt hash (%s); generate one with 'teanode password' and do not paste the plain password here", err)
		}
		if user.Email != "" && !isEmailAddress(user.Email) {
			validator.add(path+".email", "%q is not an email address", user.Email)
		}
	}
}

func (self *Configuration) validateIntegrations(validator *validator) {
	if self.DNS.Nameserver == "" {
		validator.add("dns.nameserver", "required: a resolver to check domain records with, for example 1.1.1.1:53")
	} else if _, _, err := net.SplitHostPort(self.DNS.Nameserver); err != nil {
		validator.add("dns.nameserver", "%q is not a host:port address", self.DNS.Nameserver)
	}
	if self.DNS.CheckInterval <= 0 {
		validator.add("dns.checkInterval", "must be positive, for example 30m")
	}

	if self.Upgrade.Automatic && !self.Upgrade.Enabled {
		// Installing what has been released requires knowing what has been
		// released. Refused rather than ignored: an operator who turns
		// checking off and leaves this on believes upgrades still happen.
		validator.add("upgrade.automatic", "requires upgrade.enabled: nothing can be installed without checking for it")
	}
	if self.Upgrade.CheckInterval < MinimumUpgradeCheckInterval {
		// Whether or not checking is on. It was only checked when it was on,
		// and the loop is built either way — so a configuration with checking
		// off and an interval of zero validated cleanly and then woke as fast
		// as the scheduler allowed, for the life of the process, doing
		// nothing each time.
		//
		// Zero is not "as often as possible": the loop would ask the release
		// list again the moment it finished. A minute is not much better —
		// the endpoint allows sixty requests an hour to an address that is
		// not signed in, and this is not the only thing behind that address.
		validator.add("upgrade.checkInterval", "must be at least %s, for example 6h", MinimumUpgradeCheckInterval)
	}
	if window := strings.TrimSpace(self.Upgrade.Window); window != "" {
		if _, _, err := parseUpgradeWindow(window); err != nil {
			validator.add("upgrade.window", "%q is not a window, for example 02:00-04:00", self.Upgrade.Window)
		}
	}

	if self.Antivirus.Enabled {
		if self.Antivirus.Host == "" {
			validator.add("antivirus.host", "required when antivirus is enabled: where clamd is listening")
		}
		if self.Antivirus.Port == 0 {
			validator.add("antivirus.port", "required when antivirus is enabled, usually 3310")
		}
	}
	if self.Antispam.Enabled {
		if self.Antispam.Host == "" {
			validator.add("antispam.host", "required when antispam is enabled: where spamd is listening")
		}
		if self.Antispam.Port == 0 {
			validator.add("antispam.port", "required when antispam is enabled, usually 783")
		}
	}
	if self.GeoIP.Enabled {
		if self.GeoIP.DatabaseFile == "" {
			validator.add("geoip.databaseFile", "required when geoip is enabled: a MaxMind .mmdb file, which you supply yourself")
		}
	}
	if self.Storage.S3.Enabled {
		if self.Storage.S3.Bucket == "" {
			validator.add("storage.s3.bucket", "required when S3 storage is enabled")
		}
		if self.Storage.S3.Region == "" {
			validator.add("storage.s3.region", "required when S3 storage is enabled")
		}
	}
	if self.Storage.Directory == "" {
		validator.add("storage.directory", "required: where raw messages are kept, for example mail")
	}
	if self.Storage.SpoolRetention <= 0 {
		validator.add("storage.spoolRetention", "must be positive, for example 30d")
	}
}

var (
	hostLabelPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	emailPattern     = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func isHostLabel(value string) bool {
	return hostLabelPattern.MatchString(value)
}

func isHostname(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(value, "."), ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !isHostLabel(label) {
			return false
		}
	}
	return true
}

// isRelayHost reports whether a value names something this server can open an
// SMTP connection to.
//
// Looser than isHostname on purpose. A relay target is reached over whatever
// network this server is on, not looked up in public DNS: an address, a
// single-label name from a container network or a search domain, or a fully
// qualified name are all ordinary. isHostname is for the names this server
// publishes and obtains certificates for, which do have to be qualified.
func isRelayHost(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	for _, label := range strings.Split(strings.TrimSuffix(value, "."), ".") {
		if !isHostLabel(label) {
			return false
		}
	}
	return true
}

func isEmailAddress(value string) bool {
	return emailPattern.MatchString(value)
}

// regexpErrorMessage strips the "error parsing regexp: " prefix that
// regexp.Compile adds, because the surrounding message already says that.
func regexpErrorMessage(err error) string {
	var parseError *syntax.Error
	if errors.As(err, &parseError) {
		return fmt.Sprintf("%s in %s", parseError.Code, parseError.Expr)
	}
	return err.Error()
}

// ValidateFiles checks that the files the configuration refers to exist and
// can be read. It is separate from Validate because the commands that create
// those files must be able to load a configuration that still lacks them.
//
// The server calls both before starting; "teanode config validate" calls both
// so that an operator sees the whole picture.
func (self *Configuration) ValidateFiles() error {
	validator := &validator{}

	if self.Server.LogDirectory != "" {
		if info, err := os.Stat(self.Server.LogDirectory); err != nil {
			validator.add("server.logDirectory", "%q cannot be used: %s", self.Server.LogDirectory, err)
		} else if !info.IsDir() {
			validator.add("server.logDirectory", "%q is not a directory", self.Server.LogDirectory)
		}
	}

	for path, filename := range map[string]string{
		"tls.certificateFile": self.TLS.CertificateFile,
		"tls.privateKeyFile":  self.TLS.PrivateKeyFile,
	} {
		if filename == "" {
			continue
		}
		if _, err := os.Stat(self.Path(filename)); err != nil {
			validator.add(path, "%q cannot be read: %s", self.Path(filename), err)
		}
	}

	if self.GeoIP.Enabled && self.GeoIP.DatabaseFile != "" {
		if _, err := os.Stat(self.Path(self.GeoIP.DatabaseFile)); err != nil {
			validator.add("geoip.databaseFile", "%q cannot be read: %s", self.Path(self.GeoIP.DatabaseFile), err)
		}
	}

	if len(validator.errors) == 0 {
		return nil
	}
	return validator.errors
}

// parseUpgradeWindow reads "HH:MM-HH:MM" so that a window can be refused when
// the configuration is checked rather than ignored with a warning at three in
// the morning. internal/upgrade parses it again to use it; this exists so a
// typo is a validation error at the point somebody types it.
func parseUpgradeWindow(window string) (int, int, error) {
	halves := strings.SplitN(window, "-", 2)
	if len(halves) != 2 {
		return 0, 0, fmt.Errorf("a window looks like 02:00-04:00")
	}
	var minutes [2]int
	for index, half := range halves {
		parsed, err := time.Parse("15:04", strings.TrimSpace(half))
		if err != nil {
			return 0, 0, fmt.Errorf("%q is not a time of day", strings.TrimSpace(half))
		}
		minutes[index] = parsed.Hour()*60 + parsed.Minute()
	}
	if minutes[0] == minutes[1] {
		return 0, 0, fmt.Errorf("a window that starts and ends at the same minute is not a window")
	}
	return minutes[0], minutes[1], nil
}
