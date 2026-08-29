package mailparse

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
)

type Verifier interface {
	Public() crypto.PublicKey
	Verify(hash crypto.Hash, digest []byte, signature []byte) error
}

type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

func QueryVerifier(ctx context.Context, domain, selector string, resolver Resolver) (string, Verifier, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	txts, err := resolver.LookupTXT(ctx, fmt.Sprintf("%s._domainkey.%s", selector, domain))
	if err != nil {
		return "", nil, err
	}
	parameters, err := ParseParameters(strings.Join(txts, ""))
	if err != nil {
		return "", nil, err
	}

	if value, ok := parameters["v"]; ok && value != "DKIM1" {
		return "", nil, fmt.Errorf("mailparse: incompatible domain key version %q", value)
	}

	p := strings.ReplaceAll(parameters["p"], " ", "")
	if p == "" {
		return "", nil, fmt.Errorf("mailparse: missing \"p\" tag in domain key")
	}

	b, err := DecodeBase64String(p)
	if err != nil {
		return "", nil, err
	}

	switch parameters["k"] {
	case "rsa", "":
		publicKey, err := x509.ParsePKIXPublicKey(b)
		if err != nil {
			// RFC 6376 is inconsistent about whether RSA public keys should
			// be formatted as RSAPublicKey or SubjectPublicKeyInfo.
			// Erratum 3017 (https://www.rfc-editor.org/errata/eid3017) proposes
			// allowing both.
			publicKey, err = x509.ParsePKCS1PublicKey(b)
		}
		if err != nil {
			return "", nil, err
		}
		rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
		if !ok {
			return "", nil, fmt.Errorf("mailparse: not a valid rsa public key")
		}
		// RFC 8301 section 3.2: verifiers MUST NOT consider signatures using
		// RSA keys of less than 1024 bits as valid signatures.
		if rsaPublicKey.Size()*8 < 1024 {
			return "", nil, fmt.Errorf("mailparse: rsa public key too weak, has %d bits", rsaPublicKey.Size()*8)
		}
		// log.Debugf("loaded rsa public key: %v", rsaPublicKey)
		return "rsa", rsaVerifier{rsaPublicKey}, nil
	case "ed25519":
		if len(b) != ed25519.PublicKeySize {
			return "", nil, fmt.Errorf("mailparse: invlaid ed25519 public key")
		}
		ed25519PublicKey := ed25519.PublicKey(b)
		// log.Debugf("loaded ed25519 public key: %v", ed25519PublicKey)
		return "ed25519", ed25519Verifier{ed25519PublicKey}, nil
	}
	return "", nil, fmt.Errorf("mailparse: unsupported public key")
}

type rsaVerifier struct {
	*rsa.PublicKey
}

func (self rsaVerifier) Public() crypto.PublicKey {
	return self.PublicKey
}

func (self rsaVerifier) Verify(hash crypto.Hash, digest, signature []byte) error {
	return rsa.VerifyPKCS1v15(self.PublicKey, hash, digest, signature)
}

type ed25519Verifier struct {
	ed25519.PublicKey
}

func (self ed25519Verifier) Public() crypto.PublicKey {
	return self.PublicKey
}

func (self ed25519Verifier) Verify(hash crypto.Hash, digest, signature []byte) error {
	if !ed25519.Verify(self.PublicKey, digest, signature) {
		return fmt.Errorf("mailparse: invalid dd25519 signature")
	}
	return nil
}

func GenerateKeyPair() ([]byte, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return nil, "", err
	}
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, "", err
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		return nil, "", err
	}
	return privateKeyBytes, EncodeBase64String(publicKeyBytes), nil
}
