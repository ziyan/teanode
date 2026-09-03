// Package configdb keeps the configuration in the database, so that several
// instances share one copy of it.
//
// It sits between internal/config, which owns the shape the program works
// with, and internal/db, which owns the rows — a package of its own because
// models already depends on config, so config cannot depend on db.
package configdb

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// domainKeyLabel scopes the key that encrypts a domain's signing key.
//
// One label per encrypted column, fixed for the lifetime of the rows sealed
// under it: nothing sealed under this opens under another, so changing it
// would be a re-encryption of every domain rather than an edit. The suffix is
// there for the day that is deliberate.
const domainKeyLabel = "teanode-domain-dkim-privatekey-v1"

// domainCertificateLabel scopes the key that encrypts a domain's TLS private
// key. A second label rather than the one above, because a label is what stops
// one compromised column from opening another.
const domainCertificateLabel = "teanode-domain-tls-privatekey-v1"

// domainKeyBox builds the cipher that a domain's signing key is stored under,
// or nil when there is no server secret to derive it from.
//
// Nil rather than an error, and the two callers below treat it as "store the
// key as it stands". The one moment it happens is the first save of a brand
// new installation, where the seed is written before a secret has been
// generated; config.EnsureSecrets generates one immediately afterwards and
// that save seals what this one could not. Refusing to save at all there
// would mean a server that cannot start, in exchange for a window of a few
// milliseconds.
func domainKeyBox(configuration *config.Configuration) (*secretbox.Box, error) {
	return boxFor(configuration, domainKeyLabel, "signing keys")
}

// domainCertificateBox is the same for a domain's TLS private key.
func domainCertificateBox(configuration *config.Configuration) (*secretbox.Box, error) {
	return boxFor(configuration, domainCertificateLabel, "certificate keys")
}

func boxFor(configuration *config.Configuration, label, what string) (*secretbox.Box, error) {
	secret := configuration.Secret()
	if len(secret) == 0 {
		return nil, nil
	}
	box, err := secretbox.New(secret, label)
	if err != nil {
		return nil, fmt.Errorf("config: cannot derive the key that protects the %s: %w", what, err)
	}
	return box, nil
}

// Mapping between the configuration the program works with and the rows the
// database holds.
//
// Both directions in one file, next to each other, because the only thing
// that can go wrong here is the two disagreeing — a field written by one and
// not read by the other is a setting that silently resets. TestRowsRoundTrip
// is what holds them together.

// settingSections are the parts of the configuration that are not lists.
// Each is one row, as JSON: they are read together and never queried by their
// parts, so a column apiece would be a hundred columns and a migration for
// every new option.
const (
	settingServer    = "server"
	settingListen    = "listen"
	settingTLS       = "tls"
	settingSMTP      = "smtp"
	settingDKIM      = "dkim"
	settingSession   = "session"
	settingDNS       = "dns"
	settingAntivirus = "antivirus"
	settingAntispam  = "antispam"
	settingGeoIP     = "geoip"
	settingStorage   = "storage"
	settingPasskey   = "passkey"
	settingUpgrade   = "upgrade"
)

// FromRows builds a configuration from what the database holds.
//
// Defaults first, then the stored values on top, so a setting added in a new
// release has its default on a database written by an older one rather than
// its zero value — which for a timeout or a port is not a default, it is a
// server that does not work.
func FromRows(rows *db.ConfigurationRows) (*config.Configuration, error) {
	configuration := config.Default()

	sections := map[string]any{
		settingServer:    &configuration.Server,
		settingListen:    &configuration.Listen,
		settingTLS:       &configuration.TLS,
		settingSMTP:      &configuration.SMTP,
		settingDKIM:      &configuration.DKIM,
		settingSession:   &configuration.Session,
		settingDNS:       &configuration.DNS,
		settingAntivirus: &configuration.Antivirus,
		settingAntispam:  &configuration.Antispam,
		settingGeoIP:     &configuration.GeoIP,
		settingStorage:   &configuration.Storage,
		settingPasskey:   &configuration.Passkey,
		settingUpgrade:   &configuration.Upgrade,
	}
	for key, target := range sections {
		stored, ok := rows.Settings[key]
		if !ok || len(stored) == 0 {
			continue
		}
		if err := yaml.Unmarshal([]byte(stored), target); err != nil {
			return nil, fmt.Errorf("config: cannot read the %q settings: %w", key, err)
		}
	}

	// The signing keys are stored encrypted, so the box has to be built
	// before the domains are read — which it can be, because the section that
	// holds the server secret has just been read above.
	box, err := domainKeyBox(configuration)
	if err != nil {
		return nil, err
	}
	certificateBox, err := domainCertificateBox(configuration)
	if err != nil {
		return nil, err
	}

	aliasesByDomain := map[string][]*config.Alias{}
	for _, row := range rows.Aliases {
		alias := &config.Alias{
			ID:       row.ID,
			Pattern:  row.Pattern,
			Comment:  row.Comment,
			Kind:     config.AliasKind(row.Kind),
			Email:    row.Email,
			Webhook:  row.Webhook,
			Disabled: row.Disabled,
		}
		if len(row.MailServer) > 0 {
			alias.MailServer = &config.MailServer{}
			if err := yaml.Unmarshal([]byte(row.MailServer), alias.MailServer); err != nil {
				return nil, fmt.Errorf("config: cannot read the mail server of alias %q: %w", row.ID, err)
			}
		}
		aliasesByDomain[row.DomainID] = append(aliasesByDomain[row.DomainID], alias)
	}

	credentialsByDomain := map[string][]*config.Credential{}
	for _, row := range rows.Credentials {
		credentialsByDomain[row.DomainID] = append(credentialsByDomain[row.DomainID], &config.Credential{
			ID:       row.ID,
			Key:      row.Key,
			Comment:  row.Comment,
			Alias:    row.Alias,
			Disabled: row.Disabled,
		})
	}

	configuration.Domains = make([]*config.Domain, 0, len(rows.Domains))
	for _, row := range rows.Domains {
		// A key written before the column was encrypted has no seal on it and
		// is taken as it stands; it is sealed the next time anything is
		// saved. A sealed one that will not open is fatal and not skipped: a
		// domain silently losing its key signs nothing and says nothing, and
		// the operator finds out from a receiver weeks later.
		privateKey := row.DKIMPrivateKey
		if secretbox.Sealed(privateKey) {
			if box == nil {
				return nil, fmt.Errorf("config: the signing key of domain %q is encrypted and there is no server secret to open it with", row.Domain)
			}
			opened, err := box.Open(privateKey)
			if err != nil {
				return nil, fmt.Errorf("config: cannot open the signing key of domain %q: %w", row.Domain, err)
			}
			privateKey = string(opened)
		}

		certificateKey := row.CertificatePrivateKey
		if secretbox.Sealed(certificateKey) {
			if certificateBox == nil {
				return nil, fmt.Errorf("config: the certificate key of domain %q is encrypted and there is no server secret to open it with", row.Domain)
			}
			opened, err := certificateBox.Open(certificateKey)
			if err != nil {
				return nil, fmt.Errorf("config: cannot open the certificate key of domain %q: %w", row.Domain, err)
			}
			certificateKey = string(opened)
		}

		configuration.Domains = append(configuration.Domains, &config.Domain{
			ID:                       row.ID,
			Domain:                   row.Domain,
			Subdomain:                row.Subdomain,
			Comment:                  row.Comment,
			SpamFilterScoreThreshold: row.SpamFilterScoreThreshold,
			MailServers:              splitHosts(row.MailServers),
			LinkHost:                 row.LinkHost,
			DKIM:                     config.DomainKey{Selector: row.DKIMSelector, PrivateKey: privateKey},
			TLS:                      config.DomainCertificate{Certificate: row.Certificate, PrivateKey: certificateKey},
			Aliases:                  aliasesByDomain[row.ID],
			Credentials:              credentialsByDomain[row.ID],
		})
	}

	configuration.Users = make([]*config.User, 0, len(rows.Users))
	for _, row := range rows.Users {
		configuration.Users = append(configuration.Users, &config.User{
			ID:           row.ID,
			Username:     row.Username,
			Name:         row.Name,
			PasswordHash: row.PasswordHash,
			Email:        row.Email,
		})
	}

	return configuration, nil
}

// ToRows turns a configuration back into rows.
func ToRows(self *config.Configuration, version int64) (*db.ConfigurationRows, error) {
	rows := &db.ConfigurationRows{Version: version, Settings: map[string]string{}}

	box, err := domainKeyBox(self)
	if err != nil {
		return nil, err
	}
	certificateBox, err := domainCertificateBox(self)
	if err != nil {
		return nil, err
	}

	sections := map[string]any{
		settingServer:    self.Server,
		settingListen:    self.Listen,
		settingTLS:       self.TLS,
		settingSMTP:      self.SMTP,
		settingDKIM:      self.DKIM,
		settingSession:   self.Session,
		settingDNS:       self.DNS,
		settingAntivirus: self.Antivirus,
		settingAntispam:  self.Antispam,
		settingGeoIP:     self.GeoIP,
		settingStorage:   self.Storage,
		settingPasskey:   self.Passkey,
		settingUpgrade:   self.Upgrade,
	}
	for key, value := range sections {
		encoded, err := yaml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("config: cannot write the %q settings: %w", key, err)
		}
		rows.Settings[key] = string(encoded)
	}

	for _, domain := range self.Domains {
		if domain == nil {
			continue
		}

		privateKey := domain.DKIM.PrivateKey
		if box != nil && privateKey != "" {
			sealed, err := box.Seal([]byte(privateKey))
			if err != nil {
				return nil, fmt.Errorf("config: cannot encrypt the signing key of domain %q: %w", domain.Domain, err)
			}
			privateKey = sealed
		}

		certificateKey := domain.TLS.PrivateKey
		if certificateBox != nil && certificateKey != "" {
			sealed, err := certificateBox.Seal([]byte(certificateKey))
			if err != nil {
				return nil, fmt.Errorf("config: cannot encrypt the certificate key of domain %q: %w", domain.Domain, err)
			}
			certificateKey = sealed
		}

		rows.Domains = append(rows.Domains, &db.DomainRow{
			ID:                       domain.ID,
			Domain:                   domain.Domain,
			Subdomain:                domain.Subdomain,
			Comment:                  domain.Comment,
			SpamFilterScoreThreshold: domain.SpamFilterScoreThreshold,
			DKIMSelector:             domain.DKIM.Selector,
			DKIMPrivateKey:           privateKey,
			MailServers:              strings.Join(domain.MailServers, ","),
			LinkHost:                 domain.LinkHost,
			Certificate:              domain.TLS.Certificate,
			CertificatePrivateKey:    certificateKey,
		})

		for position, alias := range domain.Aliases {
			if alias == nil {
				continue
			}
			row := &db.AliasRow{
				ID:       alias.ID,
				DomainID: domain.ID,
				Position: position,
				Pattern:  alias.Pattern,
				Comment:  alias.Comment,
				Kind:     string(alias.Kind),
				Email:    alias.Email,
				Webhook:  alias.Webhook,
				Disabled: alias.Disabled,
			}
			if alias.MailServer != nil {
				encoded, err := yaml.Marshal(alias.MailServer)
				if err != nil {
					return nil, fmt.Errorf("config: cannot write the mail server of alias %q: %w", alias.ID, err)
				}
				row.MailServer = string(encoded)
			}
			rows.Aliases = append(rows.Aliases, row)
		}

		for position, credential := range domain.Credentials {
			if credential == nil {
				continue
			}
			rows.Credentials = append(rows.Credentials, &db.CredentialRow{
				ID:       credential.ID,
				DomainID: domain.ID,
				Position: position,
				Key:      credential.Key,
				Comment:  credential.Comment,
				Alias:    credential.Alias,
				Disabled: credential.Disabled,
			})
		}
	}

	for _, user := range self.Users {
		if user == nil {
			continue
		}
		// An account that arrived from a configuration file has no identifier
		// yet; one is minted here so it has one the moment it is stored.
		id := user.ID
		if id == "" {
			id = config.NewID()
		}
		rows.Users = append(rows.Users, &db.UserRow{
			ID:           id,
			Username:     user.Username,
			Name:         user.Name,
			PasswordHash: user.PasswordHash,
			Email:        user.Email,
		})
	}

	return rows, nil
}

// splitHosts reads back the comma separated list of mail server names. Empty
// means the default, which is derived rather than stored.
func splitHosts(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	hosts := make([]string, 0, 2)
	for _, host := range strings.Split(value, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	return hosts
}

// NeedsSealing reports whether the database still holds a signing key as an
// earlier release wrote it: in the clear.
func NeedsSealing(rows *db.ConfigurationRows) bool {
	for _, row := range rows.Domains {
		if row == nil {
			continue
		}
		if row.DKIMPrivateKey != "" && !secretbox.Sealed(row.DKIMPrivateKey) {
			return true
		}
		if row.CertificatePrivateKey != "" && !secretbox.Sealed(row.CertificatePrivateKey) {
			return true
		}
	}
	return false
}

// EnsureSealed encrypts the signing keys of an installation upgraded from a
// release that stored them in the clear.
//
// Reading tolerates a key with no seal on it and sealing happens on the way
// out, so the column was always going to convert — but only when something
// else caused a save, which on a server that is simply running could be
// months. That is a column documented as encrypted and sitting in plaintext,
// which is worse than one that was never encrypted at all, because it is
// believed. So the upgrade does the write itself.
//
// The mutation is empty on purpose: every row is rewritten on any save, so
// changing nothing changes all of them.
func EnsureSealed(database db.Database, store config.Store) error {
	rows, err := database.LoadConfiguration()
	if err != nil {
		return err
	}
	if !NeedsSealing(rows) {
		return nil
	}

	// A first run has no secret yet. Nothing is lost by waiting: the save
	// that generates one seals what this could not.
	if len(store.Current().Secret()) == 0 {
		return nil
	}

	log.Noticef("encrypting the signing keys stored by an earlier release")
	return store.Update(func(*config.Configuration) error { return nil })
}
