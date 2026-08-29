package security

import (
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidToken is returned for every way a token can fail to verify, and
// says nothing about which way, for the same reason as ErrInvalidCredential.
var ErrInvalidToken = errors.New("security: invalid token")

func DecodeToken(kind, value string, secret []byte) (string, string, error) {
	if len(value) != 26+32 {
		return "", "", ErrInvalidToken
	}
	if err := ValidateULID(value[:26]); err != nil {
		return "", "", ErrInvalidToken
	}
	id := strings.ToLower(value[:26])
	key := value[26 : 26+16]
	expectedValue := EncodeToken(kind, id, key, secret)
	if subtle.ConstantTimeCompare([]byte(value), []byte(expectedValue)) != 1 {
		return "", "", ErrInvalidToken
	}
	return id, key, nil
}

func EncodeToken(kind, id, key string, secret []byte) string {
	contentToSign := strings.Join([]string{kind, id, key}, ":")
	tokenHash := strings.ToLower(base32.StdEncoding.EncodeToString(SignString(contentToSign, secret))[:16])
	return strings.Join([]string{id, key, tokenHash}, "")
}

func EncodeTokenAsCode(kind, id, key string, secret []byte) string {
	contentToSign := strings.Join([]string{kind, id, key}, ":")
	code := binary.LittleEndian.Uint64(SignString(contentToSign, secret)[:8])
	return fmt.Sprintf("%06d", code%1000000)
}
