package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/op/go-logging"
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
	self.validateTls(validator)
	self.validateDatabase(validator)
	self.validateSmtp(validator)
	self.validateDkim(validator)
	self.validateSession(validator)
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
		"listen.imap":         self.Listen.IMAP,
		"listen.imaps":        self.Listen.IMAPS,
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

func (self *Configuration) validateTls(validator *validator) {
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
		validator.add("tls.acme.directoryUrl", "required when ACME is enabled")
	} else if parsed, err := url.Parse(self.TLS.ACME.DirectoryURL); err != nil || parsed.Scheme != "https" {
		validator.add("tls.acme.directoryUrl", "%q is not an https URL", self.TLS.ACME.DirectoryURL)
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
			validator.add("tls.acme.route53.zoneId", "required when the Route53 solver is enabled")
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

func (self *Configuration) validateSmtp(validator *validator) {
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

func (self *Configuration) validateDkim(validator *validator) {
	if self.DKIM.Selector == "" {
		validator.add("dkim.selector", "required: the selector to give a new domain's signing key, for example teanode1")
	} else if !isHostLabel(self.DKIM.Selector) {
		validator.add("dkim.selector", "%q is not usable as a DNS label", self.DKIM.Selector)
	}
}

// validateSession checks how long a login lasts.
func (self *Configuration) validateSession(validator *validator) {
	if self.Session.Lifetime <= 0 {
		validator.add("session.lifetime", "must be positive, for example 30d")
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
		switch self.Antispam.Engine {
		case "", AntispamEngineBuiltin, AntispamEngineSpamd:
		default:
			validator.add("antispam.engine",
				`must be "builtin" for the filter inside this server, "spamd" for an external SpamAssassin daemon, or empty to keep what this server already does`)
		}
		if self.Antispam.ResolvedEngine() == AntispamEngineSpamd {
			if self.Antispam.SpamdHost() == "" {
				validator.add("antispam.spamd.host", "required when antispam.engine is spamd: where the daemon is listening")
			}
			if self.Antispam.SpamdPort() == 0 {
				validator.add("antispam.spamd.port", "required when antispam.engine is spamd, usually 783")
			}
		}
		for index, list := range self.Antispam.Builtin.DNS.AddressLists {
			if list.Zone == "" {
				validator.add(fmt.Sprintf("antispam.builtin.dns.addressLists[%d].zone", index), "required: the suffix queries are built with, for example zen.spamhaus.org")
			}
		}
		for index, list := range self.Antispam.Builtin.DNS.DomainLists {
			if list.Zone == "" {
				validator.add(fmt.Sprintf("antispam.builtin.dns.domainLists[%d].zone", index), "required: the suffix queries are built with, for example dbl.spamhaus.org")
			}
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

var hostLabelPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

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
		return 0, 0, fmt.Errorf("config: a window looks like 02:00-04:00")
	}
	var minutes [2]int
	for index, half := range halves {
		parsed, err := time.Parse("15:04", strings.TrimSpace(half))
		if err != nil {
			return 0, 0, fmt.Errorf("config: %q is not a time of day", strings.TrimSpace(half))
		}
		minutes[index] = parsed.Hour()*60 + parsed.Minute()
	}
	if minutes[0] == minutes[1] {
		return 0, 0, fmt.Errorf("config: a window that starts and ends at the same minute is not a window")
	}
	return minutes[0], minutes[1], nil
}
