package apigraph

import (
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/config"
)

// The rest of the configuration, as settings.
//
// The groups in settings.go are the optional integrations — the services this
// server talks to when it is told to. These are the server itself: what it
// accepts, how it resolves names, how long somebody stays signed in, what it
// listens on and what it calls itself. They lived only in the configuration
// file, which on this program is a table in PostgreSQL, so changing one meant
// exporting the configuration, editing YAML and importing it back.
//
// Two things they share with the groups next door. Secrets never cross: the
// ACME account key and the session signing key are marked secret in the
// configuration and appear here not at all, not even as a redaction. And
// nothing here validates: config.Update runs the whole configuration through
// internal/config/validate.go before it commits, so a refused value comes back
// as the same error an edited file would have produced.
//
// Durations and sizes cross as text — "6h", "50MB" — parsed by the
// configuration's own parsers. A number of seconds would be a second spelling
// of the same setting, and two spellings drift.

// SMTPSettings are the limits the mail path applies to every message and
// every connection. All of them are read per message, so a change takes
// effect on the next one with no restart.
type SMTPSettings struct {
	// The largest message this server accepts, as text: "50MB".
	MaxMessageSize string `json:"maxMessageSize"`

	// How many recipients one message may name, arriving and leaving.
	MaxRecipientsIncoming int `json:"maxRecipientsIncoming"`
	MaxRecipientsOutgoing int `json:"maxRecipientsOutgoing"`

	// How long an unknown sender is asked to wait before being accepted, as
	// text: "5m". "0s" turns greylisting off.
	GreylistDelay string `json:"greylistDelay"`

	// Failed sign-in attempts allowed per minute, and the burst allowed
	// before that rate applies.
	AuthRateLimit int `json:"authRateLimit"`
	AuthRateBurst int `json:"authRateBurst"`

	// Addresses and ranges exempt from the checks a stranger faces.
	TrustedSenders []string `json:"trustedSenders,omitempty"`
}

// ResolverSettings are how this server asks DNS questions when it checks what
// a domain has published.
type ResolverSettings struct {
	// The resolver to ask, as host:port. Empty uses the system's.
	Nameserver string `json:"nameserver"`

	// How often each domain's records are checked, as text: "6h".
	CheckInterval string `json:"checkInterval"`

	// Services asked what this machine's public address is, in order.
	ExternalAddressServices []string `json:"externalAddressServices,omitempty"`
}

// SessionSettings are how long a sign-in lasts. The key that signs one is a
// secret and is not here.
type SessionSettings struct {
	// How long a session lives before it has to be made again, as text:
	// "720h". Shortening it does not end the sessions already issued.
	Lifetime string `json:"lifetime"`
}

// PasskeySettings are how this server presents itself to an authenticator.
type PasskeySettings struct {
	Enabled bool `json:"enabled"`

	// The domain a passkey is bound to. A passkey made under one of these is
	// useless under another, so changing it invalidates every passkey
	// registered so far.
	RelyingPartyID string `json:"relyingPartyId,omitempty"`

	// What an authenticator shows the person deciding whether to allow it.
	DisplayName string `json:"displayName,omitempty"`

	// The origins a ceremony may come from. Empty derives one from the
	// relying party.
	Origins []string `json:"origins,omitempty"`

	// How many passkeys one account may register. Zero means no limit.
	MaximumPerUser int `json:"maximumPerUser"`
}

// ListenSettings are the addresses this server binds. Every one of them is
// read once at startup, so a change here is stored and does nothing until the
// process restarts — which the pending list on the Server page reports.
type ListenSettings struct {
	SMTPIncoming string `json:"smtpIncoming"`
	SMTPOutgoing string `json:"smtpOutgoing"`
	IMAP         string `json:"imap"`
	IMAPS        string `json:"imaps"`
	HTTP         string `json:"http"`
	HTTPS        string `json:"https"`

	// The profiling listener, off when empty.
	Debug string `json:"debug,omitempty"`
}

// IdentitySettings are what this server calls itself and how loudly it talks.
type IdentitySettings struct {
	// The name this server gives in its SMTP banner and derives its own
	// certificate from.
	Name string `json:"name"`

	// The hosts every domain's MX records name, unless a domain names its
	// own.
	MailServers []string `json:"mailServers,omitempty"`

	// How much is logged: debug, info, notice, warning, error.
	LogLevel string `json:"logLevel"`

	// Where mail and state are written. Reported and not settable: a server
	// restarted against a different directory does not find what is in the
	// old one, and the mail is simply gone from the dashboard's point of
	// view. Moving it is a deliberate operation with the server stopped, not
	// a field on a page.
	DataDirectory string `json:"dataDirectory"`
}

// StorageSettings are where mail is kept on this machine, beside the S3 group
// that mirrors it elsewhere.
type StorageSettings struct {
	// The directory under the data directory that holds stored messages.
	Directory string `json:"directory"`

	// How long a message waits in the spool for a delivery that keeps
	// failing, as text: "30d".
	SpoolRetention string `json:"spoolRetention"`
}

// GeoIPSettings are the optional MaxMind database used to say where a sender
// connected from.
type GeoIPSettings struct {
	Enabled bool `json:"enabled"`

	// Path to the .mmdb file. Read at startup.
	DatabaseFile string `json:"databaseFile,omitempty"`
}

// SMTPParameters are the limits an operator can change.
type SMTPParameters struct {
	MaxMessageSize        *string   `json:"maxMessageSize"`
	MaxRecipientsIncoming *int      `json:"maxRecipientsIncoming"`
	MaxRecipientsOutgoing *int      `json:"maxRecipientsOutgoing"`
	GreylistDelay         *string   `json:"greylistDelay"`
	AuthRateLimit         *int      `json:"authRateLimit"`
	AuthRateBurst         *int      `json:"authRateBurst"`
	TrustedSenders        *[]string `json:"trustedSenders"`
}

// ResolverParameters are the DNS settings an operator can change.
type ResolverParameters struct {
	Nameserver              *string   `json:"nameserver"`
	CheckInterval           *string   `json:"checkInterval"`
	ExternalAddressServices *[]string `json:"externalAddressServices"`
}

// SessionParameters are the session settings an operator can change.
type SessionParameters struct {
	Lifetime *string `json:"lifetime"`
}

// PasskeyParameters are the passkey settings an operator can change.
type PasskeyParameters struct {
	Enabled        *bool     `json:"enabled"`
	RelyingPartyID *string   `json:"relyingPartyId"`
	DisplayName    *string   `json:"displayName"`
	Origins        *[]string `json:"origins"`
	MaximumPerUser *int      `json:"maximumPerUser"`
}

// ListenParameters are the addresses an operator can change. Each takes effect
// on the next restart and not before.
type ListenParameters struct {
	SMTPIncoming *string `json:"smtpIncoming"`
	SMTPOutgoing *string `json:"smtpOutgoing"`
	IMAP         *string `json:"imap"`
	IMAPS        *string `json:"imaps"`
	HTTP         *string `json:"http"`
	HTTPS        *string `json:"https"`
	Debug        *string `json:"debug"`
}

// IdentityParameters are what an operator can change about the server itself.
// dataDirectory is deliberately absent: see IdentitySettings.
type IdentityParameters struct {
	Name        *string   `json:"name"`
	MailServers *[]string `json:"mailServers"`
	LogLevel    *string   `json:"logLevel"`
}

// StorageParameters are the on-disk storage settings an operator can change.
type StorageParameters struct {
	Directory      *string `json:"directory"`
	SpoolRetention *string `json:"spoolRetention"`
}

// GeoIPParameters are the geolocation settings an operator can change.
type GeoIPParameters struct {
	Enabled      *bool   `json:"enabled"`
	DatabaseFile *string `json:"databaseFile"`
}

// describeServerSettings fills in the groups above. Held by describeSettings,
// which owns the struct being filled.
func describeServerSettings(configuration *config.Configuration, settings *Settings) {
	smtp := configuration.SMTP
	settings.SMTP = &SMTPSettings{
		MaxMessageSize:        smtp.MaxMessageSize.String(),
		MaxRecipientsIncoming: smtp.MaxRecipientsIncoming,
		MaxRecipientsOutgoing: smtp.MaxRecipientsOutgoing,
		GreylistDelay:         smtp.GreylistDelay.String(),
		AuthRateLimit:         smtp.AuthRateLimit,
		AuthRateBurst:         smtp.AuthRateBurst,
		TrustedSenders:        smtp.TrustedSenders,
	}
	settings.Resolver = &ResolverSettings{
		Nameserver:              configuration.DNS.Nameserver,
		CheckInterval:           configuration.DNS.CheckInterval.String(),
		ExternalAddressServices: configuration.DNS.ExternalAddressServices,
	}
	settings.Session = &SessionSettings{
		Lifetime: configuration.Session.Lifetime.String(),
	}
	settings.Passkey = &PasskeySettings{
		Enabled:        configuration.Passkey.Enabled,
		RelyingPartyID: configuration.Passkey.RelyingPartyID,
		DisplayName:    configuration.Passkey.DisplayName,
		Origins:        configuration.Passkey.Origins,
		MaximumPerUser: configuration.Passkey.MaximumPerUser,
	}
	settings.Listen = &ListenSettings{
		SMTPIncoming: configuration.Listen.SMTPIncoming,
		SMTPOutgoing: configuration.Listen.SMTPOutgoing,
		IMAP:         configuration.Listen.IMAP,
		IMAPS:        configuration.Listen.IMAPS,
		HTTP:         configuration.Listen.HTTP,
		HTTPS:        configuration.Listen.HTTPS,
		Debug:        configuration.Listen.Debug,
	}
	settings.Identity = &IdentitySettings{
		Name:          configuration.Server.Name,
		MailServers:   configuration.Server.MailServers,
		LogLevel:      configuration.Server.LogLevel,
		DataDirectory: configuration.Server.DataDirectory,
	}
	settings.Storage = &StorageSettings{
		Directory:      configuration.Storage.Directory,
		SpoolRetention: configuration.Storage.SpoolRetention.String(),
	}
	settings.GeoIP = &GeoIPSettings{
		Enabled:      configuration.GeoIP.Enabled,
		DatabaseFile: configuration.GeoIP.DatabaseFile,
	}
}

// applyServerSettings applies whichever of the groups above were sent. Called
// inside config.Update, so returning an error abandons the whole change rather
// than committing half of it.
func applyServerSettings(configuration *config.Configuration, arguments UpdateSettingsArguments) error {
	if parameters := arguments.SMTP; parameters != nil {
		smtp := &configuration.SMTP
		if err := applyByteSize(&smtp.MaxMessageSize, parameters.MaxMessageSize, "smtp.maxMessageSize"); err != nil {
			return err
		}
		applyInt(&smtp.MaxRecipientsIncoming, parameters.MaxRecipientsIncoming)
		applyInt(&smtp.MaxRecipientsOutgoing, parameters.MaxRecipientsOutgoing)
		if err := applyDuration(&smtp.GreylistDelay, parameters.GreylistDelay, "smtp.greylistDelay"); err != nil {
			return err
		}
		applyInt(&smtp.AuthRateLimit, parameters.AuthRateLimit)
		applyInt(&smtp.AuthRateBurst, parameters.AuthRateBurst)
		applyStrings(&smtp.TrustedSenders, parameters.TrustedSenders)
	}

	if parameters := arguments.Resolver; parameters != nil {
		applyString(&configuration.DNS.Nameserver, parameters.Nameserver)
		if err := applyDuration(&configuration.DNS.CheckInterval, parameters.CheckInterval, "dns.checkInterval"); err != nil {
			return err
		}
		applyStrings(&configuration.DNS.ExternalAddressServices, parameters.ExternalAddressServices)
	}

	if parameters := arguments.Session; parameters != nil {
		if err := applyDuration(&configuration.Session.Lifetime, parameters.Lifetime, "session.lifetime"); err != nil {
			return err
		}
	}

	if parameters := arguments.Passkey; parameters != nil {
		passkey := &configuration.Passkey
		applyBool(&passkey.Enabled, parameters.Enabled)
		applyString(&passkey.RelyingPartyID, parameters.RelyingPartyID)
		applyString(&passkey.DisplayName, parameters.DisplayName)
		applyStrings(&passkey.Origins, parameters.Origins)
		applyInt(&passkey.MaximumPerUser, parameters.MaximumPerUser)
	}

	if parameters := arguments.Listen; parameters != nil {
		listen := &configuration.Listen
		applyString(&listen.SMTPIncoming, parameters.SMTPIncoming)
		applyString(&listen.SMTPOutgoing, parameters.SMTPOutgoing)
		applyString(&listen.IMAP, parameters.IMAP)
		applyString(&listen.IMAPS, parameters.IMAPS)
		applyString(&listen.HTTP, parameters.HTTP)
		applyString(&listen.HTTPS, parameters.HTTPS)
		applyString(&listen.Debug, parameters.Debug)
	}

	if parameters := arguments.Identity; parameters != nil {
		applyString(&configuration.Server.Name, parameters.Name)
		applyStrings(&configuration.Server.MailServers, parameters.MailServers)
		applyString(&configuration.Server.LogLevel, parameters.LogLevel)
	}

	if parameters := arguments.Storage; parameters != nil {
		applyString(&configuration.Storage.Directory, parameters.Directory)
		if err := applyDuration(&configuration.Storage.SpoolRetention, parameters.SpoolRetention, "storage.spoolRetention"); err != nil {
			return err
		}
	}

	if parameters := arguments.GeoIP; parameters != nil {
		applyBool(&configuration.GeoIP.Enabled, parameters.Enabled)
		applyString(&configuration.GeoIP.DatabaseFile, parameters.DatabaseFile)
	}

	return nil
}

// applyInt sets a whole number, when one was sent.
func applyInt(target *int, value *int) {
	if value != nil {
		*target = *value
	}
}

// applyStrings replaces a list, when one was sent. Empty entries are dropped
// and each is trimmed, because a list typed into a field arrives with the
// spaces somebody put after the commas.
func applyStrings(target *[]string, value *[]string) {
	if value == nil {
		return
	}
	kept := make([]string, 0, len(*value))
	for _, entry := range *value {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	*target = kept
}

// applyDuration parses "6h" the way the configuration file does, and says
// which setting was wrong rather than only that something was.
func applyDuration(target *config.Duration, value *string, name string) error {
	if value == nil {
		return nil
	}
	parsed, err := config.ParseDuration(strings.TrimSpace(*value))
	if err != nil {
		return fmt.Errorf("apigraph: %s: %w", name, err)
	}
	*target = parsed
	return nil
}

// applyByteSize parses "50MB" the same way.
func applyByteSize(target *config.ByteSize, value *string, name string) error {
	if value == nil {
		return nil
	}
	parsed, err := config.ParseByteSize(strings.TrimSpace(*value))
	if err != nil {
		return fmt.Errorf("apigraph: %s: %w", name, err)
	}
	*target = parsed
	return nil
}
