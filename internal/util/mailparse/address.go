package mailparse

import (
	"encoding/base32"
	"fmt"
	"net/mail"
	"strings"

	"github.com/ziyan/teanode/internal/util/security"
)

func ParseAddress(address string) (string, error) {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(parsed.Address, "+") {
		return "", fmt.Errorf("mailparse: email address cannot start with a plus sign")
	}
	return CanonicalizeAddress(parsed.Address), nil
}

// SplitAddress splits a user@domain address into user and domain.
func SplitAddress(address string) (string, string) {
	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return address, ""
	}

	return parts[0], strings.ToLower(parts[1])
}

func UnsplitAddress(user, domain string) string {
	return fmt.Sprintf("%s@%s", user, strings.ToLower(domain))
}

func CanonicalizeAddress(address string) string {
	alias, domain := SplitAddress(address)
	return UnsplitAddress(alias, domain)
}

func UniqueAddress(address string) string {
	alias, domain := SplitAddress(address)
	alias = strings.ToLower(strings.Split(alias, "+")[0])
	return UnsplitAddress(alias, domain)
}

// SignAddress creates a signed address in the format dsn-id-signature@domain.
func SignAddress(prefix, id, domain string, secret []byte) (string, error) {
	parts := make([]string, 0, 3)
	parts = append(parts, strings.ToLower(prefix), id)

	// sign
	contentToSign := UnsplitAddress(strings.Join(parts, "-"), domain)
	hash := security.SignString(contentToSign, secret)
	signature := strings.ToLower(base32.HexEncoding.EncodeToString(hash)[:16])

	// add signature
	parts = append(parts, signature)
	return UnsplitAddress(strings.Join(parts, "-"), domain), nil
}

// ValidateAddress validates a signed address in the format dsn-id-signature@domain.
func ValidateAddress(address string, secret []byte) (string, string, error) {
	alias, domain := SplitAddress(address)
	parts := strings.Split(strings.ToLower(alias), "-")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("mailparse: invalid address")
	}

	// validate signature
	contentToSign := UnsplitAddress(strings.Join(parts[:2], "-"), domain)
	hash := security.SignString(contentToSign, secret)
	expectedSignature := strings.ToLower(base32.HexEncoding.EncodeToString(hash)[:16])
	if parts[2] != expectedSignature {
		return "", "", fmt.Errorf("mailparse: invalid address, signature does not match")
	}

	// validate id
	if err := security.ValidateULID(parts[1]); err != nil {
		return "", "", fmt.Errorf("mailparse: invalid address, invalid id: %w", err)
	}

	return parts[0], strings.ToLower(parts[1]), nil
}
