package config

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

// domainKeyBits is the size of a generated signing key.
//
// 2048 is the practical maximum rather than a compromise: a 4096-bit key
// produces a DNS TXT value long enough that some providers mangle it, and
// several large receivers do not check keys that big anyway. 1024 is now
// treated as weak.
const domainKeyBits = 2048

// GenerateDomainKey creates a signing key for a domain.
func GenerateDomainKey(selector string) (DomainKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, domainKeyBits)
	if err != nil {
		return DomainKey{}, fmt.Errorf("config: cannot generate a signing key: %w", err)
	}

	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return DomainKey{}, fmt.Errorf("config: cannot encode a signing key: %w", err)
	}

	return DomainKey{
		Selector:   selector,
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})),
	}, nil
}

// Signer parses the private key so it can sign a message.
//
// Parsing per message would be wasteful, so the mail path takes this from the
// configuration snapshot's cached index rather than calling it directly.
func (self *DomainKey) Signer() (crypto.Signer, error) {
	if strings.TrimSpace(self.PrivateKey) == "" {
		return nil, fmt.Errorf("config: no signing key")
	}

	block, _ := pem.Decode([]byte(self.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("config: the signing key is not valid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// A key written by an older release, or exported by another tool,
		// may be in the older format.
		legacy, legacyError := x509.ParsePKCS1PrivateKey(block.Bytes)
		if legacyError != nil {
			return nil, fmt.Errorf("config: cannot parse the signing key: %w", err)
		}
		return legacy, nil
	}

	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("config: the signing key cannot sign")
	}
	return signer, nil
}

// PublicKeyRecord returns the TXT value to publish at
// <selector>._domainkey.<domain>, which is what makes the signature checkable.
func (self *DomainKey) PublicKeyRecord() (string, error) {
	signer, err := self.Signer()
	if err != nil {
		return "", err
	}
	encoded, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return "", fmt.Errorf("config: cannot encode the public key: %w", err)
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(encoded), nil
}

// DomainKeyName is where a domain's key is published.
func DomainKeyName(selector, domain string) string {
	return fmt.Sprintf("%s._domainkey.%s", selector, domain)
}
