package config

import (
	"crypto/rand"
	"encoding/base64"

	"fmt"
	"os"
	"strings"
)

// legacySecretFilename is where the server secret used to live, before secrets
// moved into the configuration file. It is still read, once, so that moving an
// existing installation does not silently invalidate every SMTP password.
const legacySecretFilename = "teanode.secret"

// legacySessionKeyFilename is the same for the session signing key.
const legacySessionKeyFilename = "session.key"

// secretBytes is the length of a generated secret before encoding.
const secretBytes = 32

// EnsureSecrets fills in the generated secrets that a new installation does
// not have yet, writing them into the stored configuration.
//
// Keeping them with the rest of the configuration rather than in files
// alongside means there is one thing to back up, one to copy to a new
// machine, and one to protect. It also means that thing is as sensitive as a
// private key.
//
// Every decision is made inside the mutation, against the configuration the
// mutation is handed. That matters once more than one instance can start at
// the same moment: the store re-runs the mutation after losing a race, and a
// secret generated beforehand would be written over the one the winner just
// stored — leaving two instances deriving SMTP passwords from different keys.
// Deciding inside means the loser sees the winner's secret and keeps it.
func EnsureSecrets(store Store) error {
	// Nothing to do is the common case — every start after the first — and
	// it is worth answering without a write, because a write bumps the
	// version and wakes every other instance up to reload.
	if current := store.Current(); strings.TrimSpace(current.Server.Secret) != "" &&
		strings.TrimSpace(current.Session.Key) != "" && !current.missingUserIdentifiers() &&
		!current.missingDomainKeys() {
		return nil
	}

	return store.Update(func(configuration *Configuration) error {
		secret := strings.TrimSpace(configuration.Server.Secret)
		sessionKey := strings.TrimSpace(configuration.Session.Key)

		// Adopt anything left over from the older layout before generating,
		// or an upgrade would quietly change every credential's password.
		if secret == "" {
			if adopted := readLegacyFile(configuration, legacySecretFilename); adopted != "" {
				log.Noticef("adopting the server secret from %s into the configuration; the file can now be deleted", configuration.Path(legacySecretFilename))
				secret = adopted
			}
		}
		if sessionKey == "" {
			if adopted := readLegacyFile(configuration, legacySessionKeyFilename); adopted != "" {
				log.Noticef("adopting the session key from %s into the configuration; the file can now be deleted", configuration.Path(legacySessionKeyFilename))
				sessionKey = adopted
			}
		}

		if secret == "" {
			generated, err := generateSecret()
			if err != nil {
				return err
			}
			secret = generated
			log.Noticef("generated a server secret; back it up, because SMTP passwords are derived from it")
		}
		if sessionKey == "" {
			generated, err := generateSecret()
			if err != nil {
				return err
			}
			sessionKey = generated
		}

		// An account written into a file by hand has no identifier, and
		// sessions, tokens and passkeys all name one. Filled in here, in the
		// same mutation and for the same reason: two instances starting
		// together must agree on what it is.
		for _, user := range configuration.Users {
			if user != nil && user.ID == "" {
				user.ID = NewID()
				log.Noticef("gave the account %q an identifier", user.Username)
			}
		}

		// Likewise a domain that arrived without a signing key — from a
		// configuration file written by hand, from an import, or from a
		// release that only signed for some of them. Every domain has its own
		// key and publishes it at its own name, so a domain without one is a
		// domain sending unsigned mail, which is a domain whose mail is
		// treated as suspicious for no reason the operator can see.
		//
		// Filled in, never replaced. A key already here matches a DNS record
		// already published, and generating over it would break signing for
		// that domain until somebody noticed and republished.
		for _, domain := range configuration.Domains {
			if domain == nil || strings.TrimSpace(domain.DKIM.PrivateKey) != "" {
				continue
			}
			generated, err := GenerateDomainKey(configuration.DKIM.Selector)
			if err != nil {
				return err
			}
			if domain.DKIM.Selector != "" {
				generated.Selector = domain.DKIM.Selector
			}
			domain.DKIM = generated
			log.Noticef("generated a signing key for %q; publish %s before its mail can be verified", domain.Domain, DomainKeyName(generated.Selector, domain.Domain))
		}

		configuration.Server.Secret = secret
		configuration.Session.Key = sessionKey
		return nil
	})
}

// missingDomainKeys reports whether any domain still has to be given a
// signing key.
func (self *Configuration) missingDomainKeys() bool {
	for _, domain := range self.Domains {
		if domain != nil && strings.TrimSpace(domain.DKIM.PrivateKey) == "" {
			return true
		}
	}
	return false
}

// missingUserIdentifiers reports whether any account still has to be given one.
func (self *Configuration) missingUserIdentifiers() bool {
	for _, user := range self.Users {
		if user != nil && user.ID == "" {
			return true
		}
	}
	return false
}

// Secret returns the key that signs bounce addresses and credential passwords.
func (self *Configuration) Secret() []byte {
	return []byte(strings.TrimSpace(self.Server.Secret))
}

// SessionKey returns the key that signs session cookies.
func (self *Configuration) SessionKey() []byte {
	return []byte(strings.TrimSpace(self.Session.Key))
}

// GenerateSecret returns a new random secret, encoded for a YAML file.
func GenerateSecret() (string, error) {
	return generateSecret()
}

func generateSecret() (string, error) {
	buffer := make([]byte, secretBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("config: cannot generate a secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// readLegacyFile returns the contents of an old secret file, or empty when
// there is none to adopt.
func readLegacyFile(configuration *Configuration, name string) string {
	content, err := os.ReadFile(configuration.Path(name))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(content))
	if len(value) < 16 {
		return ""
	}
	return value
}
