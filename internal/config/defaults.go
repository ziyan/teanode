package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// DefaultFilename is where the configuration lives unless told otherwise.
const DefaultFilename = "/opt/teanode/teanode.yaml"

// LetsEncryptDirectoryURL is the production ACME directory.
const LetsEncryptDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

// LetsEncryptStagingDirectoryURL issues untrusted certificates but has far
// looser rate limits. Use it while getting a new deployment working.
const LetsEncryptStagingDirectoryURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

// Default returns a configuration with every field set to a sensible value.
// It is not usable as-is: server.name, tls.hosts and at least one domain
// still have to be filled in, and Validate says so.
func Default() *Configuration {
	return &Configuration{
		Server: Server{
			DataDirectory: "/opt/teanode/data",
			LogLevel:      "INFO",
		},
		Listen: Listen{
			SMTPIncoming: ":25",
			SMTPOutgoing: ":587",
			HTTP:         ":80",
			HTTPS:        ":443",
		},
		TLS: TLS{
			ACME: ACME{
				Enabled:      true,
				DirectoryURL: LetsEncryptDirectoryURL,
				Challenge:    ChallengeHTTP01,
				Route53: Route53{
					Region: "us-east-1",
				},
			},
		},
		Database: Database{
			Host:    "127.0.0.1",
			Port:    5432,
			User:    "teanode",
			Name:    "teanode",
			SSLMode: "disable",
		},
		SMTP: SMTP{
			// Off. A server that can reach port 25 should use it: delivering
			// straight to the recipient is fewer moving parts and nobody
			// else's rate limit.
			Relay:                 Relay{Security: RelaySecurityStartTLS, Port: 587},
			TrustedSenders:        []string{"google.com", "outlook.com", "yahoo.com"},
			MaxMessageSize:        70 * 1024 * 1024,
			MaxRecipientsIncoming: 3,
			MaxRecipientsOutgoing: 50,
			GreylistDelay:         Duration(3 * time.Second),
			AuthRateLimit:         10,
			AuthRateBurst:         20,
			RequireReverseDNS:     true,
		},
		DKIM: DKIM{
			Selector: "teanode1",
		},
		Session: Session{
			Lifetime: Duration(30 * 24 * time.Hour),
		},
		DNS: DNS{
			Nameserver:    "1.1.1.1:53",
			CheckInterval: Duration(30 * time.Minute),
			ExternalAddressServices: []string{
				"https://api.ipify.org",
				"https://icanhazip.com",
			},
		},
		Antivirus: Antivirus{
			Host: "127.0.0.1",
			Port: 3310,
		},
		Antispam: Antispam{
			// On by default, which it could not be while scoring required a
			// second program to be running. The built-in filter needs
			// nothing, so a new server scores its mail rather than silently
			// not doing it.
			//
			// This changes nothing for an existing deployment: the stored
			// configuration wins, and a server that had it off keeps it off.
			Enabled: true,
			Engine:  AntispamEngineBuiltin,
			Spamd: AntispamSpamd{
				Host: "127.0.0.1",
				Port: 783,
			},
			Builtin: AntispamBuiltin{
				Signals: AntispamSignals{Enabled: true},
				DNS: AntispamDNS{
					Enabled: true,
					Timeout: Duration(5 * time.Second),
					AddressLists: []AntispamList{
						{Zone: "zen.spamhaus.org", Weight: 3.0},
					},
					DomainLists: []AntispamList{
						{Zone: "dbl.spamhaus.org", Weight: 3.0},
					},
					MaximumDomains: 10,
				},
				Bayes: AntispamBayes{
					Enabled:         true,
					MinimumMessages: 200,
					Weight:          3.0,
				},
				Rules: AntispamRules{
					Enabled:               false,
					Channels:              []string{"updates.spamassassin.org"},
					UpdateInterval:        Duration(24 * time.Hour),
					MaximumEvaluationTime: Duration(2 * time.Second),
				},
			},
		},
		Storage: Storage{
			Directory:      "mail",
			SpoolRetention: Duration(30 * 24 * time.Hour),
			S3: S3{
				Region: "us-east-1",
			},
		},
		Upgrade: Upgrade{
			// Checking is on, installing is not. Being told that a release
			// exists costs one request every six hours; installing one
			// without being asked is a decision about somebody's mail.
			Enabled:       true,
			CheckInterval: Duration(6 * time.Hour),
		},
	}
}

// Example returns a configuration for a fictional operator, used by
// "teanode config init" as the starting point a human then edits.
func Example() *Configuration {
	configuration := Default()
	configuration.Server.Name = "mail.example.com"
	configuration.TLS.Hosts = []string{"mail.example.com"}
	configuration.TLS.ACME.Email = "you@example.com"
	configuration.Domains = []*Domain{
		{
			ID:                       NewID(),
			Domain:                   "example.com",
			Subdomain:                "mail",
			Comment:                  "replace with your own domain",
			SpamFilterScoreThreshold: 5,
			Aliases: []*Alias{
				{
					ID:      NewID(),
					Pattern: "^hello$",
					Comment: "hello@example.com goes to your real mailbox",
					Kind:    AliasKindEmail,
					Email:   "you@example.net",
				},
				{
					ID:      NewID(),
					Pattern: "^.*$",
					Comment: "catch-all; every other address goes here too",
					Kind:    AliasKindEmail,
					Email:   "you@example.net",
				},
			},
		},
	}
	return configuration
}

// NewID generates the identifier used for domains, aliases, credentials and
// accounts. Identifiers are stable for the life of the object because stored
// mail, deliveries, sessions and tokens reference them.
func NewID() string {
	return security.NewULID()
}

// Challenge kinds understood by tls.acme.challenge.
const (
	ChallengeHTTP01    = "http-01"
	ChallengeTLSALPN01 = "tls-alpn-01"
	ChallengeDNS01     = "dns-01"
)

// DataDirectory returns the absolute data directory. A relative
// server.dataDirectory resolves against the directory holding the
// configuration file, never against the process working directory: the secret
// and the keys must be the same files whichever directory a command is run
// from.
func (self *Configuration) DataDirectory() string {
	if self.Server.DataDirectory == "" {
		return ""
	}
	if filepath.IsAbs(self.Server.DataDirectory) || self.baseDirectory == "" {
		return self.Server.DataDirectory
	}
	return filepath.Join(self.baseDirectory, self.Server.DataDirectory)
}

// SetBaseDirectory records where the configuration file lives, so that
// relative paths inside it can be resolved. Load does this; callers that build
// a configuration in memory may do it themselves.
func (self *Configuration) SetBaseDirectory(directory string) {
	self.baseDirectory = directory
}

// Path resolves a filename against the data directory. Absolute paths are
// returned unchanged, so an operator can point at a key kept elsewhere.
func (self *Configuration) Path(filename string) string {
	if filename == "" {
		return ""
	}
	if filepath.IsAbs(filename) {
		return filename
	}
	return filepath.Join(self.DataDirectory(), filename)
}

// EnsureDataDirectory creates the data directory if it is missing. The
// directory holds keys and the message spool, so it is created private to the
// user running the server.
func (self *Configuration) EnsureDataDirectory() error {
	directory := self.DataDirectory()
	if directory == "" {
		return nil
	}
	return os.MkdirAll(directory, 0o700)
}
