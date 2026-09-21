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
		strings.TrimSpace(current.Session.Key) != "" {
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

		configuration.Server.Secret = secret
		configuration.Session.Key = sessionKey
		return nil
	})
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
