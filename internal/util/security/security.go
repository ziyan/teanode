// Package security provides cryptographic utilities for random generation and signing.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("security") //nolint:unused

const (
	UpperAlpha        = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	LowerAlpha        = "abcdefghijklmnopqrstuvwxyz"
	Digits            = "0123456789"
	Alpha             = UpperAlpha + LowerAlpha
	AlphaNumeric      = Alpha + Digits
	LowerAlphaNumeric = LowerAlpha + Digits
)

// GenerateRandom generates a random binary string.
func GenerateRandom(length int) []byte {
	data := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, data); err != nil {
		panic(err)
	}
	return data
}

// GenerateRandomHexString generates a random hex encoded string.
func GenerateRandomHexString(length int) string {
	return hex.EncodeToString(GenerateRandom(length))
}

// GenerateRandomBase64String generates a random base64 encoded string.
func GenerateRandomBase64String(length int) string {
	return base64.URLEncoding.EncodeToString(GenerateRandom(length))
}

// GenerateRandomString generates a random string from the given alphabet.
func GenerateRandomString(length int, alphabet string) string {
	choices := []byte(alphabet)
	size := len(choices)
	randomString := make([]byte, length)
	for index, data := range GenerateRandom(length) {
		randomString[index] = choices[int(data)%size]
	}
	return string(randomString)
}

func IsRandomString(value string, length int, alphabet string) bool {
	if len(value) != length {
		return false
	}
	return strings.Trim(value, alphabet) == ""
}

func SignString(value string, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
