package security

import (
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidCredential is returned for every way a credential can fail to
// verify. It carries no detail on purpose: the caller logs it, and a log that
// says which half was wrong — or worse, what the right answer was — is a
// credential store for anyone who can read it.
var ErrInvalidCredential = errors.New("security: invalid credential")

func DecodeCredential(username, password string, secret []byte) (string, string, error) {
	if err := ValidateULID(username); err != nil {
		return "", "", fmt.Errorf("security: cannot decode username %q: %w", username, err)
	}
	if len(password) < 16 {
		return "", "", ErrInvalidCredential
	}
	credentialId := strings.ToLower(username)
	credentialKey := password[:16]
	contentToSign := fmt.Sprintf("%s:%s", credentialId, credentialKey)
	credentialHash := strings.ToLower(base32.StdEncoding.EncodeToString(SignString(contentToSign, secret))[:16])
	expectedPassword := credentialKey + credentialHash
	// Constant time: the attacker supplies the first half and the server
	// derives the second, so a comparison that stops at the first wrong byte
	// tells them how much of their guess was right.
	if subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) != 1 {
		return "", "", ErrInvalidCredential
	}
	return credentialId, credentialKey, nil
}

func EncodeCredential(credentialId, credentialKey string, secret []byte) (string, string, error) {
	contentToSign := fmt.Sprintf("%s:%s", credentialId, credentialKey)
	credentialHash := strings.ToLower(base32.StdEncoding.EncodeToString(SignString(contentToSign, secret))[:16])
	return credentialId, credentialKey + credentialHash, nil
}
