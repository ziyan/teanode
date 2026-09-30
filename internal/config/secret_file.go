package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The server secret can be kept in a file named at start rather than with
// the rest of the configuration. Everything sealed in the database -- the
// agent's keys, the domains' signing keys, every source's and skill's
// secrets -- is sealed with it, and every SMTP password is derived from it,
// so a database that holds it holds the means to open everything else. Kept
// in a file, a dump of the database is ciphertext and hashes, and the file
// is backed up, and guarded, apart from it.
//
// The database then keeps only a check derived from the secret, so that an
// instance handed the wrong file refuses to start rather than sealing new
// values with a key nothing else can open.

// secretCheckLabel binds the check to its use: it is not a value any other
// part of the server derives from the secret.
const secretCheckLabel = "teanode configuration: server secret check"

// SecretFile is the server secret as a file holds it.
type SecretFile struct {
	// Path is where the file is, as the operator named it.
	Path string

	// Secret is what the file holds, trimmed as the configuration's own
	// copy always was; nil while the file does not exist yet.
	Secret []byte
}

// ReadSecretFile reads the server secret from a file. An empty path is no
// file at all, and the secret stays in the database. A file that does not
// exist yet is not an error: a new server writes one there, and a server
// whose secret is still in the database is told how to move it.
func ReadSecretFile(path string) (*SecretFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &SecretFile{Path: path}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: cannot read the server secret file: %w", err)
	}
	secret := []byte(strings.TrimSpace(string(content)))
	if len(secret) < 16 {
		return nil, fmt.Errorf("config: the server secret file %s holds %d bytes, too few to be a secret", path, len(secret))
	}
	return &SecretFile{Path: path, Secret: secret}, nil
}

// WriteSecretFile writes a secret to a file that must not exist yet, readable
// by its owner only. Never over an existing file: that file may be the only
// copy of the secret something was sealed with.
func WriteSecretFile(path string, secret []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: cannot create the directory for the server secret file: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("config: cannot create the server secret file: %w", err)
	}
	if _, err := file.Write(secret); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("config: cannot write the server secret file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("config: cannot write the server secret file: %w", err)
	}
	return file.Close()
}

// secretCheckOf is what the database keeps in place of the secret: enough to
// tell the right file from a wrong one, and nothing that opens anything.
func secretCheckOf(secret []byte) string {
	if len(secret) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(secretCheckLabel))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// isSecretStored says the stored rows hold the secret itself, as every
// server did before the secret could be kept in a file.
func isSecretStored(configuration *Configuration) bool {
	return strings.TrimSpace(configuration.Server.Secret) != ""
}

// applySecretFile puts the file's secret on a configuration just read from
// the database, and refuses a file that does not belong to it: a secret the
// database holds that differs from the file's, or a check that the file's
// secret does not produce. Without a file, a database whose secret was moved
// out is refused too, because generating a new one would lose everything the
// old one sealed.
func applySecretFile(configuration *Configuration, secretFile *SecretFile) error {
	check := strings.TrimSpace(configuration.Server.SecretCheck)
	if secretFile == nil {
		if !isSecretStored(configuration) && check != "" {
			return fmt.Errorf("config: this database's server secret is kept in a file; " +
				"start with --secret-file (or TEANODE_SECRET_FILE) naming it")
		}
		return nil
	}
	if secretFile.Secret == nil {
		return fmt.Errorf("config: the server secret file %s does not exist", secretFile.Path)
	}
	if isSecretStored(configuration) && !hmac.Equal(configuration.Secret(), secretFile.Secret) {
		return fmt.Errorf("config: the server secret file %s does not hold the secret this database holds; "+
			"refusing to start rather than seal anything with the wrong one", secretFile.Path)
	}
	if check != "" && !hmac.Equal([]byte(check), []byte(secretCheckOf(secretFile.Secret))) {
		return fmt.Errorf("config: the server secret file %s does not hold the secret this database was sealed with; "+
			"refusing to start rather than seal anything with the wrong one", secretFile.Path)
	}
	configuration.Server.Secret = string(secretFile.Secret)
	return nil
}

// prepareSecretFile makes sure the named file exists before the store opens,
// for a new server: one with no secret anywhere gets a new secret written to
// the file. A server whose secret is still in the database is told to move it
// with "teanode-server config export-secret", rather than having it written
// out here: a file written to a path that does not survive the container
// would take the only copy with it once the database's copy was removed.
func prepareSecretFile(stored *Configuration, secretFile *SecretFile) error {
	if secretFile == nil || secretFile.Secret != nil {
		return nil
	}
	if isSecretStored(stored) {
		return fmt.Errorf("config: the server secret file %s does not exist, and the secret is still in the database; "+
			"write it there first with: teanode-server config export-secret --output %s", secretFile.Path, secretFile.Path)
	}
	if strings.TrimSpace(stored.Server.SecretCheck) != "" {
		return fmt.Errorf("config: the server secret file %s does not exist, and this database was sealed with the secret it held; "+
			"restore the file from its backup", secretFile.Path)
	}
	generated, err := generateSecret()
	if err != nil {
		return err
	}
	if err := WriteSecretFile(secretFile.Path, []byte(generated)); err != nil {
		return err
	}
	secretFile.Secret = []byte(generated)
	log.Noticef("generated a server secret in %s; back it up apart from the database, because nothing sealed in the database opens without it", secretFile.Path)
	return nil
}
