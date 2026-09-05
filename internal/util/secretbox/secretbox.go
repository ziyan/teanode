// Package secretbox encrypts a secret so it can be stored somewhere less
// trusted than the secret itself — in practice, a column of the database.
//
// Plaintext goes in and a tagged, base64 string comes out, short enough and
// plain enough ASCII to live in a text column, and recognisable on sight in a
// database dump. The same box reverses it.
//
// The key is derived with HKDF-SHA256 from a long-lived master secret, under
// an info label naming what is being encrypted. The label scopes the derived
// key: two boxes built from the same master secret with different labels
// cannot read each other's values, so one column being compromised does not
// hand over another. Pick a label per column and never change it — changing
// it is a re-encryption of every row, because nothing sealed under the old
// label opens under the new one, which is why the labels in this program
// carry a version suffix.
//
// Every Seal draws a fresh nonce, so sealing the same key twice gives two
// different values and nobody can tell from the outside that two rows hold
// the same secret.
//
// What this is worth is worth being honest about. It is not a defence against
// somebody who has everything: the master secret is itself in the database
// here, so a full dump discloses both halves. What it does is stop a private
// key from leaving in a partial one — a copy of a single table, a support
// query, a row in a log, a replica of the domains and not the settings — and
// it puts the boundary in place for a master secret that one day comes from
// outside the database.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("secretbox") //nolint:unused

// keySize is 32 bytes, which makes it AES-256. The nonce is whatever GCM asks
// for, which is 12.
const keySize = 32

// prefix marks a sealed value.
//
// It is what lets a column hold both: a value written before this existed is
// plaintext and has no prefix, so a reader can tell the two apart with
// certainty rather than by guessing at the shape of the bytes, and a column
// converts as its rows are rewritten instead of in a migration that would
// need the master secret to run.
//
// It is also the courtesy of saying so. Somebody who meets one of these in a
// dump of the database can see that it is deliberate, and grep for it.
const prefix = "sealed:"

// ErrCipher is what every failure to open a value comes back as: truncated,
// altered, or sealed under a different secret or label. Wrapped, so a caller
// can match it with errors.Is without the underlying message reaching a log.
var ErrCipher = errors.New("secretbox: cannot open the sealed value")

// Box is an authenticated cipher bound to one master secret and one label.
// Build one and keep it; Seal and Open are safe to call concurrently.
type Box struct {
	aead cipher.AEAD
}

// New derives the key and returns a box.
//
// An empty secret is refused rather than accepted: HKDF is perfectly willing
// to take one, and the result is a key derived from the label alone — the
// same on every installation in the world, which is not encryption. A caller
// that might not have a secret yet has to decide what to do about that, and
// deciding is not something this package can do for it.
func New(secret []byte, info string) (*Box, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("secretbox: no secret to derive a key from")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, info, keySize)
	if err != nil {
		return nil, fmt.Errorf("secretbox: cannot derive a key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: cannot build the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: cannot build the cipher: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Sealed reports whether a stored value was written by Seal.
//
// A value that is not sealed is one written before the column was encrypted,
// and the reader is expected to take it as it stands.
func Sealed(value string) bool {
	return strings.HasPrefix(value, prefix)
}

// Seal encrypts plaintext. The nonce is stored in front of the ciphertext,
// because opening needs it and it is not a secret.
func (self *Box) Seal(plaintext []byte) (string, error) {
	nonce := make([]byte, self.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secretbox: cannot read a nonce: %w", err)
	}
	return prefix + base64.StdEncoding.EncodeToString(self.aead.Seal(nonce, nonce, plaintext, nil)), nil
}

// Open reverses Seal.
//
// A value with no prefix is an error and not a plaintext passed through: this
// package cannot know whether the caller's column is allowed to hold one, and
// quietly returning unencrypted bytes as though they had been decrypted is
// how a column stops being encrypted without anybody noticing. Ask Sealed
// first.
func (self *Box) Open(value string) ([]byte, error) {
	if !Sealed(value) {
		return nil, fmt.Errorf("%w: it is not sealed", ErrCipher)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCipher, err)
	}
	if len(raw) < self.aead.NonceSize() {
		return nil, fmt.Errorf("%w: it is shorter than a nonce", ErrCipher)
	}
	nonce, ciphertext := raw[:self.aead.NonceSize()], raw[self.aead.NonceSize():]
	plaintext, err := self.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCipher, err)
	}
	return plaintext, nil
}
