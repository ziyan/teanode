package apigraph

import (
	"context"
	"github.com/ziyan/teanode/internal/models"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

type SettingsQuery interface {
	// Get the optional integrations and how they are configured. Secrets are
	// never returned; a field that is set reads back as "(redacted)".
	GetSettings(ctx context.Context) (*Settings, error)
}

type SettingsMutation interface {
	// Change the optional integrations. A secret left out is kept as it is,
	// so a form can be saved without re-entering one; an explicitly empty
	// secret clears it.
	UpdateSettings(ctx context.Context, arguments UpdateSettingsArguments) (*Settings, error)
}

// Settings are the optional integrations, as the dashboard sees them.
//
// Everything a secret would appear in is redacted here. The dashboard needs to
// know whether a secret is set, not what it is, and a management API that can
// read back the AWS key it was given is one leak away from being the leak.
type Settings struct {
	// Off-box copies of stored messages in S3
	S3 *S3Settings `json:"s3"`

	// The Route53 solver, needed only for a dns-01 challenge
	Route53 *Route53Settings `json:"route53"`

	// Virus scanning through ClamAV
	Antivirus *ServiceSettings `json:"antivirus"`

	// Spam scoring through SpamAssassin
	Antispam *AntispamSettings `json:"antispam"`

	// The mail server outgoing mail is handed to, instead of delivering it
	// by MX lookup
	Relay *RelaySettings `json:"relay"`

	// What a mail client is told to connect to
	Submission *SubmissionSettings `json:"submission"`
	SSO        *SSOSettings        `json:"sso"`

	// How outgoing mail leaves this machine, when not directly
	Proxy *ProxySettings `json:"proxy"`

	// How certificates are obtained
	Certificates *CertificateSettings `json:"certificates"`

	// The server itself, rather than the services it talks to. These live in
	// settings_server.go, which says why they are a separate group.
	SMTP     *SMTPSettings     `json:"smtp"`
	Resolver *ResolverSettings `json:"resolver"`
	Session  *SessionSettings  `json:"session"`
	Passkey  *PasskeySettings  `json:"passkey"`
	Listen   *ListenSettings   `json:"listen"`
	Identity *IdentitySettings `json:"identity"`
	Storage  *StorageSettings  `json:"storage"`
	GeoIP    *GeoIPSettings    `json:"geoip"`
}

// CertificateSettings is what this server obtains certificates for, and how.
//
// The ACME account key is not here and never will be: it is the credential
// that proves this server is the one that asked for the certificates, and a
// management API that can read it back is one leak away from being the leak.
type CertificateSettings struct {
	// Whether each domain gets a certificate in its own name, rather than
	// every domain being served the server's own.
	PerDomain bool `json:"perDomain"`

	// The names the server's own certificate covers
	Hosts []string `json:"hosts,omitempty"`

	// Whether certificates are obtained automatically at all. Off means the
	// two files below are the only ones this server has.
	ACMEEnabled bool `json:"acmeEnabled"`

	// The address the certificate authority writes to about expiry.
	ACMEEmail string `json:"acmeEmail,omitempty"`

	// Which authority to ask. Empty is Let's Encrypt.
	ACMEDirectoryURL string `json:"acmeDirectoryUrl,omitempty"`

	// How ownership is proved: http-01 or dns-01. A wildcard name needs
	// dns-01, which needs the Route53 solver beside this.
	ACMEChallenge string `json:"acmeChallenge,omitempty"`

	// A certificate and key supplied by hand, used when ACME is off.
	CertificateFile string `json:"certificateFile,omitempty"`
	PrivateKeyFile  string `json:"privateKeyFile,omitempty"`
}

// SubmissionSettings is the address a mail client should use.
type SubmissionSettings struct {
	// What is configured, empty when it is left to follow the server
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`

	// What a mail client is actually told, once the blanks above are filled
	// in from the server name and the listen address
	EffectiveHost string `json:"effectiveHost"`
	EffectivePort string `json:"effectivePort"`
}

// RelaySettings is the smarthost outgoing mail goes through.
type RelaySettings struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`

	// How TLS is used: starttls, tls, or none
	Security string `json:"security"`

	// The account it authenticates as, which is not a secret
	Username string `json:"username,omitempty"`

	// Whether a password is configured. Never the password itself.
	HasPassword bool `json:"hasPassword"`
}

// ProxySettings is how outgoing mail leaves this machine, when it does not go
// out of it directly.
//
// A domestic connection with port 25 blocked, or one whose address is on every
// blocklist, needs the traffic to surface somewhere else. A SOCKS5 proxy is
// one way — the relay above is the other, and they answer the same question
// differently: the relay hands the message to somebody else's mail server, and
// this carries this server's own SMTP conversation out through another
// address.
type ProxySettings struct {
	// Address of the SOCKS5 proxy, as host:port. Empty means outgoing mail
	// goes out of this machine directly.
	SOCKS5 string `json:"socks5,omitempty"`
}

// S3Settings is the object store mirror.
type S3Settings struct {
	Enabled bool   `json:"enabled"`
	Bucket  string `json:"bucket"`
	Region  string `json:"region"`

	// Endpoint of an S3-compatible service that is not AWS, for example a
	// MinIO on the next machine. Empty means AWS.
	Endpoint string `json:"endpoint,omitempty"`

	// Whether the bucket is addressed as endpoint/bucket rather than
	// bucket.endpoint, which anything self-hosted needs.
	PathStyle bool `json:"pathStyle"`

	// Identifier of the AWS credentials in use, which is not itself a secret
	AccessKeyID string `json:"accessKeyId,omitempty"`

	// Whether a secret access key is configured. Never the key itself.
	HasSecretAccessKey bool `json:"hasSecretAccessKey"`

	// Path to an AWS shared credentials file, when one is used instead
	CredentialsFile string `json:"credentialsFile,omitempty"`
}

// Route53Settings is the dns-01 challenge solver.
type Route53Settings struct {
	Enabled bool   `json:"enabled"`
	ZoneID  string `json:"zoneId"`
	Region  string `json:"region"`

	// Identifier of the AWS credentials in use, which is not itself a secret
	AccessKeyID string `json:"accessKeyId,omitempty"`

	// Whether a secret access key is configured. Never the key itself.
	HasSecretAccessKey bool `json:"hasSecretAccessKey"`

	// Path to an AWS shared credentials file, when one is used instead
	CredentialsFile string `json:"credentialsFile,omitempty"`
}

// ServiceSettings is an optional scanner reached over the network.
type ServiceSettings struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    uint16 `json:"port"`
}

// AntispamSettings is how mail is scored for spam.
//
// Unlike the antivirus, there are two ways to do this — the filter inside
// this server, or an external SpamAssassin daemon — so the host and port are
// only half the answer and this cannot share ServiceSettings.
type AntispamSettings struct {
	Enabled bool `json:"enabled"`

	// Engine is what the operator chose: "builtin", "spamd", or empty.
	Engine string `json:"engine"`

	// EffectiveEngine is what that resolves to, which is what the server is
	// actually doing. An operator who never set the field should still be
	// able to see which filter is running.
	EffectiveEngine string `json:"effectiveEngine"`

	// Host and Port are where the external daemon listens.
	Host string `json:"host"`
	Port uint16 `json:"port"`

	// The built-in filter's parts, each independently switchable.
	SignalsEnabled bool `json:"signalsEnabled"`
	DNSEnabled     bool `json:"dnsEnabled"`
	BayesEnabled   bool `json:"bayesEnabled"`
	RulesEnabled   bool `json:"rulesEnabled"`

	// BayesMinimumMessages is how many learned messages the classifier waits
	// for before it contributes.
	BayesMinimumMessages int64 `json:"bayesMinimumMessages"`

	// BayesLearnedSpam and BayesLearnedHam are how many it has, so the
	// dashboard can say how far along it is.
	BayesLearnedSpam int64 `json:"bayesLearnedSpam"`
	BayesLearnedHam  int64 `json:"bayesLearnedHam"`
}

func (self *graph) GetSettings(ctx context.Context) (*Settings, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	settings := describeSettings(self.config.Current())

	// The learned counts come from the database rather than the
	// configuration, and are what tells an operator whether the classifier
	// has seen enough to say anything yet.
	if settings.Antispam != nil {
		spam, ham, err := self.database.CountSpamTraining()
		if err != nil {
			return nil, err
		}
		settings.Antispam.BayesLearnedSpam = spam
		settings.Antispam.BayesLearnedHam = ham
	}
	return settings, nil
}

func describeSettings(configuration *config.Configuration) *Settings {
	route53 := configuration.TLS.ACME.Route53
	s3 := configuration.Storage.S3
	relay := configuration.SMTP.Relay
	settings := &Settings{
		S3: &S3Settings{
			Enabled:            s3.Enabled,
			Bucket:             s3.Bucket,
			Region:             s3.Region,
			Endpoint:           s3.Endpoint,
			PathStyle:          s3.PathStyle,
			AccessKeyID:        s3.AccessKeyID,
			HasSecretAccessKey: s3.SecretAccessKey != "",
			CredentialsFile:    s3.CredentialsFile,
		},
		Route53: &Route53Settings{
			Enabled:            route53.Enabled,
			ZoneID:             route53.ZoneID,
			Region:             route53.Region,
			AccessKeyID:        route53.AccessKeyID,
			HasSecretAccessKey: route53.SecretAccessKey != "",
			CredentialsFile:    route53.CredentialsFile,
		},
		Antivirus: &ServiceSettings{
			Enabled: configuration.Antivirus.Enabled,
			Host:    configuration.Antivirus.Host,
			Port:    configuration.Antivirus.Port,
		},
		Proxy: &ProxySettings{SOCKS5: configuration.SMTP.SOCKS5Proxy},
		Certificates: &CertificateSettings{
			PerDomain:        configuration.TLS.ACME.PerDomain,
			Hosts:            configuration.TLS.Hosts,
			ACMEEnabled:      configuration.TLS.ACME.Enabled,
			ACMEEmail:        configuration.TLS.ACME.Email,
			ACMEDirectoryURL: configuration.TLS.ACME.DirectoryURL,
			ACMEChallenge:    configuration.TLS.ACME.Challenge,
			CertificateFile:  configuration.TLS.CertificateFile,
			PrivateKeyFile:   configuration.TLS.PrivateKeyFile,
		},
		Antispam: &AntispamSettings{
			Enabled:              configuration.Antispam.Enabled,
			Engine:               configuration.Antispam.Engine,
			EffectiveEngine:      configuration.Antispam.ResolvedEngine(),
			Host:                 configuration.Antispam.SpamdHost(),
			Port:                 configuration.Antispam.SpamdPort(),
			SignalsEnabled:       configuration.Antispam.Builtin.Signals.Enabled,
			DNSEnabled:           configuration.Antispam.Builtin.DNS.Enabled,
			BayesEnabled:         configuration.Antispam.Builtin.Bayes.Enabled,
			RulesEnabled:         configuration.Antispam.Builtin.Rules.Enabled,
			BayesMinimumMessages: configuration.Antispam.Builtin.Bayes.MinimumMessages,
		},
		SSO: ssoSettingsOf(configuration),
		Submission: &SubmissionSettings{
			Host:          configuration.SMTP.Submission.Host,
			Port:          int(configuration.SMTP.Submission.Port),
			EffectiveHost: configuration.SubmissionHost(),
			EffectivePort: configuration.SubmissionPort(),
		},
		Relay: &RelaySettings{
			Enabled:     relay.Enabled,
			Host:        relay.Host,
			Port:        int(relay.Port),
			Security:    string(relay.Security),
			Username:    relay.Username,
			HasPassword: relay.Password != "",
		},
	}
	describeServerSettings(configuration, settings)
	return settings
}

// S3Parameters are the S3 settings an operator can change.
type S3Parameters struct {
	Enabled *bool   `json:"enabled"`
	Bucket  *string `json:"bucket"`
	Region  *string `json:"region"`

	// Endpoint of an S3-compatible service that is not AWS. Setting one and
	// leaving pathStyle alone is enough: an endpoint implies path style,
	// because a self-hosted store rarely has the wildcard DNS that the other
	// addressing needs.
	Endpoint  *string `json:"endpoint"`
	PathStyle *bool   `json:"pathStyle"`

	AccessKeyID *string `json:"accessKeyId"`

	// SecretAccessKey is write only. Omit it to keep the current one; pass an
	// empty string to clear it and fall back to the default AWS chain.
	SecretAccessKey *string `json:"secretAccessKey"`

	CredentialsFile *string `json:"credentialsFile"`
}

// Route53Parameters are the Route53 settings an operator can change.
type Route53Parameters struct {
	Enabled *bool   `json:"enabled"`
	ZoneID  *string `json:"zoneId"`
	Region  *string `json:"region"`

	AccessKeyID *string `json:"accessKeyId"`

	// SecretAccessKey is write only. Omit it to keep the current one; pass an
	// empty string to clear it and fall back to the default AWS chain.
	SecretAccessKey *string `json:"secretAccessKey"`

	CredentialsFile *string `json:"credentialsFile"`
}

// ServiceParameters are the settings of an optional scanner.
type ServiceParameters struct {
	Enabled *bool   `json:"enabled"`
	Host    *string `json:"host"`
	Port    *uint16 `json:"port"`
}

// AntispamParameters are the spam settings an operator can change.
type AntispamParameters struct {
	Enabled *bool   `json:"enabled"`
	Engine  *string `json:"engine"`
	Host    *string `json:"host"`
	Port    *uint16 `json:"port"`

	SignalsEnabled *bool `json:"signalsEnabled"`
	DNSEnabled     *bool `json:"dnsEnabled"`
	BayesEnabled   *bool `json:"bayesEnabled"`
	RulesEnabled   *bool `json:"rulesEnabled"`

	BayesMinimumMessages *int64 `json:"bayesMinimumMessages"`
}

// RelayParameters are the relay settings an operator can change.
type RelayParameters struct {
	Enabled  *bool   `json:"enabled"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Security *string `json:"security"`
	Username *string `json:"username"`

	// Password is write only. Omit it to keep the current one; pass an empty
	// string to clear it.
	Password *string `json:"password"`
}

// SubmissionParameters are the advertised submission settings an operator can
// change. Both empty means "follow the server", which is the default.
type SubmissionParameters struct {
	Host *string `json:"host"`
	Port *int    `json:"port"`
}

// CertificateParameters are the certificate settings an operator can change.
//
// Hosts was readable and not writable, which made the one field somebody
// actually comes here to change — the names on this server's own certificate
// — a thing you could see and not edit.
type CertificateParameters struct {
	// PerDomain obtains a certificate for each domain's own mail server name.
	// Turning it off stops renewing them; the ones already issued stay in
	// place and keep being served until they expire.
	PerDomain *bool `json:"perDomain"`

	// Hosts replaces the names the server's own certificate covers.
	Hosts *[]string `json:"hosts"`

	ACMEEnabled      *bool   `json:"acmeEnabled"`
	ACMEEmail        *string `json:"acmeEmail"`
	ACMEDirectoryURL *string `json:"acmeDirectoryUrl"`
	ACMEChallenge    *string `json:"acmeChallenge"`

	CertificateFile *string `json:"certificateFile"`
	PrivateKeyFile  *string `json:"privateKeyFile"`
}

// UpgradeParameters are the release settings an operator can change from the
// dashboard.
//
// checkInterval is not here: it is read once, when the checker is built, and a
// setting that appears to save and does nothing is the thing the startup-only
// warning exists to prevent.
//
// enabled was left out too, on the reasoning that reaching out to somebody
// else's endpoint is a deployment's decision and belongs with the settings
// written where the server is installed. On this program there is no such
// place: the settings are the database, there is no "config set", and it is on
// by default — so leaving it out meant an operator who wanted the checking off
// had to export the configuration, edit the file, and import it back. A
// default that can only be turned off by hand is not a default anybody chose.
type UpgradeParameters struct {
	// Whether to ask the release list what has been published
	Enabled *bool `json:"enabled"`

	// Whether a new release is installed without being asked
	Automatic *bool `json:"automatic"`

	// The hours an automatic upgrade may run in, local time, as
	// "02:00-04:00". Empty means any time.
	Window *string `json:"window"`
}

// ProxyParameters are the outbound proxy settings an operator can change.
type ProxyParameters struct {
	// Address of the SOCKS5 proxy, as host:port. Empty clears it.
	SOCKS5 *string `json:"socks5"`
}

type UpdateSettingsArguments struct {
	S3         *S3Parameters         `json:"s3"`
	Route53    *Route53Parameters    `json:"route53"`
	Antivirus  *ServiceParameters    `json:"antivirus"`
	Antispam   *AntispamParameters   `json:"antispam"`
	Relay      *RelayParameters      `json:"relay"`
	Submission *SubmissionParameters `json:"submission"`
	SSO        *SSOParameters        `json:"sso"`
	Proxy      *ProxyParameters      `json:"proxy"`
	Upgrade    *UpgradeParameters    `json:"upgrade"`

	// Certificates changes what this server obtains certificates for.
	Certificates *CertificateParameters `json:"certificates"`

	// The server itself: see settings_server.go.
	SMTP     *SMTPParameters     `json:"smtp"`
	Resolver *ResolverParameters `json:"resolver"`
	Session  *SessionParameters  `json:"session"`
	Passkey  *PasskeyParameters  `json:"passkey"`
	Listen   *ListenParameters   `json:"listen"`
	Identity *IdentityParameters `json:"identity"`
	Storage  *StorageParameters  `json:"storage"`
	GeoIP    *GeoIPParameters    `json:"geoip"`
}

func (self *graph) UpdateSettings(ctx context.Context, arguments UpdateSettingsArguments) (*Settings, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}

	if err := self.config.Update(func(configuration *config.Configuration) error {
		if parameters := arguments.S3; parameters != nil {
			s3 := &configuration.Storage.S3
			applyBool(&s3.Enabled, parameters.Enabled)
			applyString(&s3.Bucket, parameters.Bucket)
			applyString(&s3.Region, parameters.Region)
			applyString(&s3.Endpoint, parameters.Endpoint)
			applyBool(&s3.PathStyle, parameters.PathStyle)
			applyString(&s3.AccessKeyID, parameters.AccessKeyID)
			applySecret(&s3.SecretAccessKey, parameters.SecretAccessKey)
			applyString(&s3.CredentialsFile, parameters.CredentialsFile)
		}
		if parameters := arguments.Route53; parameters != nil {
			route53 := &configuration.TLS.ACME.Route53
			applyBool(&route53.Enabled, parameters.Enabled)
			applyString(&route53.ZoneID, parameters.ZoneID)
			applyString(&route53.Region, parameters.Region)
			applyString(&route53.AccessKeyID, parameters.AccessKeyID)
			applySecret(&route53.SecretAccessKey, parameters.SecretAccessKey)
			applyString(&route53.CredentialsFile, parameters.CredentialsFile)
		}
		if parameters := arguments.Antivirus; parameters != nil {
			applyBool(&configuration.Antivirus.Enabled, parameters.Enabled)
			applyString(&configuration.Antivirus.Host, parameters.Host)
			applyPort(&configuration.Antivirus.Port, parameters.Port)
		}
		if parameters := arguments.Antispam; parameters != nil {
			applyBool(&configuration.Antispam.Enabled, parameters.Enabled)
			applyString(&configuration.Antispam.Engine, parameters.Engine)
			// Written to the current field, never the deprecated one, so a
			// server edited through the dashboard stops depending on it.
			applyString(&configuration.Antispam.Spamd.Host, parameters.Host)
			applyPort(&configuration.Antispam.Spamd.Port, parameters.Port)
			applyBool(&configuration.Antispam.Builtin.Signals.Enabled, parameters.SignalsEnabled)
			applyBool(&configuration.Antispam.Builtin.DNS.Enabled, parameters.DNSEnabled)
			applyBool(&configuration.Antispam.Builtin.Bayes.Enabled, parameters.BayesEnabled)
			applyBool(&configuration.Antispam.Builtin.Rules.Enabled, parameters.RulesEnabled)
			if parameters.BayesMinimumMessages != nil {
				configuration.Antispam.Builtin.Bayes.MinimumMessages = *parameters.BayesMinimumMessages
			}
		}
		if parameters := arguments.Upgrade; parameters != nil {
			applyBool(&configuration.Upgrade.Enabled, parameters.Enabled)
			applyBool(&configuration.Upgrade.Automatic, parameters.Automatic)
			applyString(&configuration.Upgrade.Window, parameters.Window)
		}
		if parameters := arguments.SSO; parameters != nil {
			// The list as given replaces the list stored, except that a
			// provider whose secret is left blank keeps the one it had:
			// the page never sees the secret and cannot send it back.
			previous := map[string]string{}
			for _, provider := range configuration.SSO.Providers {
				previous[provider.ID] = provider.ClientSecret
			}
			providers := make([]config.SSOProvider, 0, len(parameters.Providers))
			for _, given := range parameters.Providers {
				if given == nil {
					continue
				}
				secret := given.ClientSecret
				if secret == "" {
					secret = previous[given.ID]
				}
				providers = append(providers, config.SSOProvider{
					ID:           strings.TrimSpace(given.ID),
					Name:         strings.TrimSpace(given.Name),
					Issuer:       strings.TrimSpace(given.Issuer),
					ClientID:     strings.TrimSpace(given.ClientID),
					ClientSecret: secret,
					GroupsClaim:  strings.TrimSpace(given.GroupsClaim),
					CreateUsers:  given.CreateUsers,
				})
			}
			configuration.SSO.Providers = providers
		}
		if parameters := arguments.Submission; parameters != nil {
			applyString(&configuration.SMTP.Submission.Host, parameters.Host)
			if parameters.Port != nil {
				configuration.SMTP.Submission.Port = uint16(*parameters.Port)
			}
		}
		if parameters := arguments.Relay; parameters != nil {
			relay := &configuration.SMTP.Relay
			applyBool(&relay.Enabled, parameters.Enabled)
			applyString(&relay.Host, parameters.Host)
			if parameters.Port != nil {
				relay.Port = uint16(*parameters.Port)
			}
			if parameters.Security != nil {
				relay.Security = config.RelaySecurity(strings.TrimSpace(*parameters.Security))
			}
			applyString(&relay.Username, parameters.Username)
			applySecret(&relay.Password, parameters.Password)
		}
		if parameters := arguments.Certificates; parameters != nil {
			applyBool(&configuration.TLS.ACME.PerDomain, parameters.PerDomain)
			applyStrings(&configuration.TLS.Hosts, parameters.Hosts)
			applyBool(&configuration.TLS.ACME.Enabled, parameters.ACMEEnabled)
			applyString(&configuration.TLS.ACME.Email, parameters.ACMEEmail)
			applyString(&configuration.TLS.ACME.DirectoryURL, parameters.ACMEDirectoryURL)
			applyString(&configuration.TLS.ACME.Challenge, parameters.ACMEChallenge)
			applyString(&configuration.TLS.CertificateFile, parameters.CertificateFile)
			applyString(&configuration.TLS.PrivateKeyFile, parameters.PrivateKeyFile)
		}
		if parameters := arguments.Proxy; parameters != nil {
			applyString(&configuration.SMTP.SOCKS5Proxy, parameters.SOCKS5)
		}
		return applyServerSettings(configuration, arguments)
	}); err != nil {
		return nil, err
	}

	// Not every setting here needs one any more — the limits, the resolver and
	// the session take effect on the next message — so this no longer claims
	// they all do. What does need a restart is reported by the pending list,
	// which watches the configuration rather than guessing from this call.
	log.Noticef("%s changed the settings", api.ContextAuthenticatedUsername(ctx))
	return describeSettings(self.config.Current()), nil
}

func applyBool(target *bool, value *bool) {
	if value != nil {
		*target = *value
	}
}

func applyString(target *string, value *string) {
	if value != nil {
		*target = strings.TrimSpace(*value)
	}
}

func applyPort(target *uint16, value *uint16) {
	if value != nil {
		*target = *value
	}
}

// applySecret refuses to store the placeholder a redacted reply carries, so
// that reading the settings and posting them straight back cannot overwrite a
// real key with the word "(redacted)".
func applySecret(target *string, value *string) {
	if value == nil || strings.TrimSpace(*value) == config.Redacted {
		return
	}
	*target = strings.TrimSpace(*value)
}

// SSOParameters replace the identity providers offered on the sign-in page.
type SSOParameters struct {
	Providers []*SSOProviderParameters `json:"providers"`
}

// SSOProviderParameters is one provider as the settings page sends it.
type SSOProviderParameters struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`

	// Blank keeps the secret already stored for this id.
	ClientSecret string `json:"clientSecret" graphapi:"nullable"`

	GroupsClaim string `json:"groupsClaim" graphapi:"nullable"`
	CreateUsers bool   `json:"createUsers"`
}

// SSOSettings are the identity providers as the settings page sees them:
// everything but the secret, and whether there is one.
type SSOSettings struct {
	Providers []*SSOProviderSettings `json:"providers"`
}

type SSOProviderSettings struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Issuer          string `json:"issuer"`
	ClientID        string `json:"clientId"`
	HasClientSecret bool   `json:"hasClientSecret"`
	GroupsClaim     string `json:"groupsClaim,omitempty"`
	CreateUsers     bool   `json:"createUsers"`
}

func ssoSettingsOf(configuration *config.Configuration) *SSOSettings {
	settings := &SSOSettings{Providers: []*SSOProviderSettings{}}
	for _, provider := range configuration.SSO.Providers {
		settings.Providers = append(settings.Providers, &SSOProviderSettings{
			ID:              provider.ID,
			Name:            provider.Name,
			Issuer:          provider.Issuer,
			ClientID:        provider.ClientID,
			HasClientSecret: provider.ClientSecret != "",
			GroupsClaim:     provider.GroupsClaim,
			CreateUsers:     provider.CreateUsers,
		})
	}
	return settings
}
