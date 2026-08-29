package config

import (
	"os"
	"strings"
)

// Fields that older releases wrote and this one no longer uses.
//
// They are accepted rather than rejected, because refusing to start on a
// configuration that was valid yesterday is a poor way to treat somebody
// upgrading. Where the old value still means something it is migrated; either
// way the field is reported once and disappears the next time the file is
// written.
//
// There is one struct per place rather than one shared struct, because an
// inlined field that collides with a real one is a duplicate YAML key and the
// encoder refuses it — which is exactly what happened when a deprecated
// "enabled" met tls.acme.enabled.

// deprecatedSession covers what used to be the dashboard section.
type deprecatedSession struct {
	// The dashboard is always served now; listen.http and listen.https decide
	// where.
	Enabled *bool `yaml:"enabled,omitempty"`

	// The key lives in session.key.
	SessionKeyFile string `yaml:"sessionKeyFile,omitempty"`

	// Renamed to session.lifetime.
	SessionLifetime Duration `yaml:"sessionLifetime,omitempty"`

	// Moved to the top level, because they administer the whole server rather
	// than only the dashboard.
	Users []*User `yaml:"users,omitempty"`
}

func (self *deprecatedSession) empty() bool {
	return self.Enabled == nil && self.SessionKeyFile == "" &&
		self.SessionLifetime == 0 && len(self.Users) == 0
}

// deprecatedDKIM covers the single signing key that every domain shared.
type deprecatedDKIM struct {
	PrivateKeyFile string   `yaml:"privateKeyFile,omitempty"`
	Domain         string   `yaml:"domain,omitempty"`
	Selectors      []string `yaml:"selectors,omitempty"`
}

func (self *deprecatedDKIM) empty() bool {
	return self.PrivateKeyFile == "" && self.Domain == "" && len(self.Selectors) == 0
}

// deprecatedACME covers the account key and certificate that were files.
type deprecatedACME struct {
	AccountKeyFile  string `yaml:"accountKeyFile,omitempty"`
	CertificateFile string `yaml:"certificateFile,omitempty"`
	PrivateKeyFile  string `yaml:"privateKeyFile,omitempty"`
}

func (self *deprecatedACME) empty() bool {
	return self.AccountKeyFile == "" && self.CertificateFile == "" && self.PrivateKeyFile == ""
}

// migrateDeprecated adopts what it can from the older layout and says what it
// did, so an operator is not left wondering where a setting went.
func (self *Configuration) migrateDeprecated() {
	self.migrateSession()
	self.migrateDomainKey()
	self.migrateACME()
}

func (self *Configuration) migrateSession() {
	outdated := &self.Session.deprecatedSession
	if outdated.empty() {
		return
	}

	if len(outdated.Users) > 0 {
		log.Warningf("dashboard.users has moved to the top level as users; they administer the whole server, not only the dashboard")
		if len(self.Users) == 0 {
			self.Users = outdated.Users
		}
	}
	if outdated.SessionLifetime > 0 {
		log.Warningf("dashboard.sessionLifetime is now session.lifetime")
		if self.Session.Lifetime == 0 {
			self.Session.Lifetime = outdated.SessionLifetime
		}
	}
	if outdated.Enabled != nil {
		log.Warningf("dashboard.enabled is no longer used; the dashboard is always served, and listen.http and listen.https decide where")
	}
	if filename := outdated.SessionKeyFile; filename != "" {
		log.Warningf("dashboard.sessionKeyFile is no longer used; the key now lives in session.key")
		if self.Session.Key == "" {
			if content := readFile(self.Path(filename)); content != "" {
				self.Session.Key = strings.TrimSpace(content)
				log.Noticef("adopted the session key from %s; that file can be deleted", self.Path(filename))
			}
		}
	}

	self.Session.deprecatedSession = deprecatedSession{}
}

func (self *Configuration) migrateDomainKey() {
	outdated := &self.DKIM.deprecatedDKIM
	if outdated.empty() {
		return
	}

	// The old key was one file shared by every domain. Give it to any domain
	// that has none of its own, so signing keeps working with the DNS records
	// already published.
	if filename := outdated.PrivateKeyFile; filename != "" {
		log.Warningf("dkim.privateKeyFile is no longer used; each domain now has its own key in domains[].dkim")
		if content := readFile(self.Path(filename)); content != "" {
			adopted := 0
			for _, domain := range self.Domains {
				if domain == nil || domain.DKIM.PrivateKey != "" {
					continue
				}
				domain.DKIM = DomainKey{Selector: self.DKIM.Selector, PrivateKey: content}
				adopted++
			}
			if adopted > 0 {
				log.Noticef("adopted the signing key from %s for %d domains; that file can be deleted, and the DNS records you already published still match", self.Path(filename), adopted)
			}
		}
	}
	if len(outdated.Selectors) > 0 {
		log.Warningf("dkim.selectors is no longer used; a domain publishes the one selector in domains[].dkim.selector")
	}
	if outdated.Domain != "" {
		log.Warningf("dkim.domain is no longer used; the server signs each domain's mail with that domain's own key")
	}

	self.DKIM.deprecatedDKIM = deprecatedDKIM{}
}

func (self *Configuration) migrateACME() {
	outdated := &self.TLS.ACME.deprecatedACME
	if outdated.empty() {
		return
	}

	if filename := outdated.AccountKeyFile; filename != "" {
		log.Warningf("tls.acme.accountKeyFile is no longer used; the account key now lives in tls.acme.accountKey")
		if self.TLS.ACME.AccountKey == "" {
			if content := readFile(self.Path(filename)); content != "" {
				self.TLS.ACME.AccountKey = content
				log.Noticef("adopted the ACME account key from %s; that file can be deleted", self.Path(filename))
			}
		}
	}

	// The issued certificate is adopted too, so an upgrade does not ask the
	// certificate authority for another one it does not need.
	if outdated.CertificateFile != "" && outdated.PrivateKeyFile != "" {
		log.Warningf("tls.acme.certificateFile and privateKeyFile are no longer used; the issued certificate now lives in tls.acme.certificate")
		certificate := readFile(self.Path(outdated.CertificateFile))
		privateKey := readFile(self.Path(outdated.PrivateKeyFile))
		if certificate != "" && privateKey != "" && self.TLS.ACME.Certificate == "" {
			self.TLS.ACME.Certificate = certificate
			self.TLS.ACME.PrivateKey = privateKey
			log.Noticef("adopted the issued certificate; those files can be deleted")
		}
	}

	self.TLS.ACME.deprecatedACME = deprecatedACME{}
}

func readFile(filename string) string {
	content, err := os.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(content)
}
