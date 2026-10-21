// Package passwords is the part of Passwords that both the server and the
// command line need: the encryption format, the fields inside an item, the
// one-time code, the passkey authenticator and the importers.
//
// Everything a person keeps in Passwords is encrypted on their own devices.
// The server never calls the functions that open an item (except for an item
// the person let the agent use); it only checks and stores sealed values. The
// format is pinned by testdata/vectors.json, which the dashboard and the
// iPhone app's tests read too, so a change here is a change to three
// implementations.
package passwords

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// KeyLength is the length of an account key, an item key, a wrap key, a
	// proof key and a recovery key: 32 bytes.
	KeyLength = 32
	// NonceLength is the length of the random nonce of a sealed value.
	NonceLength = 12
	// SaltLength is the length of the random salt of the passphrase key.
	SaltLength = 16
	// SealedVersion is the only version of a sealed value this code writes
	// and the only one it opens.
	SealedVersion = 1
	// DefaultIterationCount is the PBKDF2 iteration count of a new
	// passphrase. The count is stored with the wrapped key, so it can be
	// raised later without breaking a passphrase already set.
	DefaultIterationCount = 600000
	// MinimumIterationCount is the lowest count DerivePassphraseKey accepts.
	// The count comes back from the server, and a server that answered with
	// a tiny one would get a proof that is cheap to guess the passphrase from.
	MinimumIterationCount = 600000
	// MaximumIterationCount is the highest count DerivePassphraseKey accepts.
	// A server that answered with an enormous one would make a device spin
	// for hours (or, on a phone, be killed) before it could unlock.
	MaximumIterationCount = 10000000
	// PublicKeyLength is the length of a public key: the uncompressed P-256
	// point, 04 then X then Y.
	PublicKeyLength = 65
	// AgentRecipientID is the recipient id in the associated data of an item
	// key sealed for the server's agent key.
	AgentRecipientID = "agent"
)

// The labels that tell the keys derived from one secret apart, and the
// shared secret of a share from the key it makes.
const (
	wrapKeyLabel  = "teanode passwords wrap v1"
	proofKeyLabel = "teanode passwords proof v1"
	shareKeyLabel = "teanode passwords share v1"
)

// recoveryKeyEncoding is RFC 4648 base32 without padding.
var recoveryKeyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Errors a caller can tell apart.
var (
	// ErrCannotOpen is returned when a sealed value does not open: the key
	// or the associated data is wrong, or the value was changed.
	ErrCannotOpen = errors.New("passwords: the sealed value does not open")
	// ErrUnsupportedVersion is returned for a sealed value of a version this
	// code does not know.
	ErrUnsupportedVersion = errors.New("passwords: unsupported sealed value version")
	// ErrIterationCountTooLow is returned when the passphrase key would be
	// derived with fewer iterations than MinimumIterationCount.
	ErrIterationCountTooLow = errors.New("passwords: the iteration count is too low")
	// ErrIterationCountTooHigh is returned when the passphrase key would be
	// derived with more iterations than MaximumIterationCount.
	ErrIterationCountTooHigh = errors.New("passwords: the iteration count is too high")
	// ErrInvalidPublicKey is returned for bytes that are not an uncompressed
	// P-256 point.
	ErrInvalidPublicKey = errors.New("passwords: that is not a public key")
	// ErrInvalidRecoveryKey is returned for text that is not a recovery key.
	ErrInvalidRecoveryKey = errors.New("passwords: that is not a recovery key")
)

// Sealed is a value encrypted with AES-256-GCM. Its JSON form is what is
// stored in a text column: the nonce and the ciphertext (with the 16 byte tag
// at its end) are standard base64 with padding.
type Sealed struct {
	Version    int    `json:"version"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// sealedText is the JSON of a sealed value with the bytes still as text, so
// the base64 can be checked to be the canonical form before it is decoded.
type sealedText struct {
	Version            int    `json:"version"`
	EphemeralPublicKey string `json:"ephemeralPublicKey"`
	Nonce              string `json:"nonce"`
	Ciphertext         string `json:"ciphertext"`
}

// parseSealedText reads the JSON of a sealed value strictly, so that one value
// has exactly one spelling: one object, the key names exactly as written here
// (the standard decoder would also take any capitalization), no key twice (it
// would keep the last), no key that is not in the format, every key there
// (ephemeralPublicKey only on the form sealed for a key pair, and then
// required), a whole number for the version, text for the rest, and nothing
// after the object.
func parseSealedText(text string, isForKeyPair bool) (*sealedText, error) {
	refused := func() (*sealedText, error) {
		return nil, fmt.Errorf("passwords: read sealed value: %w", ErrCannotOpen)
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	if opening, err := decoder.Token(); err != nil || opening != json.Delim('{') {
		return refused()
	}
	allowed := map[string]bool{"version": true, "nonce": true, "ciphertext": true}
	if isForKeyPair {
		allowed["ephemeralPublicKey"] = true
	}
	parsed := &sealedText{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		name, isText := token.(string)
		if err != nil || !isText || !allowed[name] || seen[name] {
			return refused()
		}
		seen[name] = true
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return refused()
		}
		if name == "version" {
			if err := json.Unmarshal(raw, &parsed.Version); err != nil {
				return refused()
			}
			continue
		}
		var value string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
			return refused()
		}
		switch name {
		case "nonce":
			parsed.Nonce = value
		case "ciphertext":
			parsed.Ciphertext = value
		case "ephemeralPublicKey":
			parsed.EphemeralPublicKey = value
		}
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return refused()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return refused()
	}
	if len(seen) != len(allowed) {
		return refused()
	}
	return parsed, nil
}

// DecodeCanonicalBase64 reads standard base64 with padding and refuses any
// other spelling of the same bytes: line breaks, missing padding, or unused
// bits that are not zero. The server uses it for the bytes it checks outside a
// sealed value too: salts, hashes and public keys.
func DecodeCanonicalBase64(text string) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != text {
		return nil, ErrCannotOpen
	}
	return decoded, nil
}

// ParseSealed reads the JSON text of a sealed value and checks its shape:
// version 1, a nonce of 12 bytes, a ciphertext of at least the 16 byte tag,
// all in canonical base64.
func ParseSealed(text string) (*Sealed, error) {
	parsed, err := parseSealedText(text, false)
	if err != nil {
		return nil, err
	}
	sealed := &Sealed{Version: parsed.Version}
	if sealed.Nonce, err = DecodeCanonicalBase64(parsed.Nonce); err != nil {
		return nil, err
	}
	if sealed.Ciphertext, err = DecodeCanonicalBase64(parsed.Ciphertext); err != nil {
		return nil, err
	}
	if err := sealed.check(); err != nil {
		return nil, err
	}
	return sealed, nil
}

// String is the JSON text to store.
func (self *Sealed) String() string {
	text, err := json.Marshal(self)
	if err != nil {
		// Three plain fields cannot fail to marshal.
		panic(err)
	}
	return string(text)
}

func (self *Sealed) check() error {
	if self.Version != SealedVersion {
		return ErrUnsupportedVersion
	}
	if len(self.Nonce) != NonceLength || len(self.Ciphertext) < 16 {
		return ErrCannotOpen
	}
	return nil
}

// NewKey makes 32 random bytes, for an account key, an item key or a
// recovery key.
func NewKey() ([]byte, error) {
	return randomBytes(KeyLength)
}

// NewSalt makes the random salt of a passphrase key.
func NewSalt() ([]byte, error) {
	return randomBytes(SaltLength)
}

func randomBytes(length int) ([]byte, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("passwords: draw random bytes: %w", err)
	}
	return bytes, nil
}

// Seal encrypts plaintext under a 32 byte key with a fresh random nonce. The
// associated data is not stored: Open must be given the same text, which ties
// a sealed value to the place it belongs (see the ...Data functions).
func Seal(key, plaintext []byte, associatedData string) (*Sealed, error) {
	nonce, err := randomBytes(NonceLength)
	if err != nil {
		return nil, err
	}
	return sealWithNonce(key, nonce, plaintext, associatedData)
}

// sealWithNonce is Seal with the nonce given, so the test vectors are fixed.
// A nonce must never be used twice under one key.
func sealWithNonce(key, nonce, plaintext []byte, associatedData string) (*Sealed, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != NonceLength {
		return nil, fmt.Errorf("passwords: a nonce is %d bytes, not %d", NonceLength, len(nonce))
	}
	return &Sealed{
		Version:    SealedVersion,
		Nonce:      append([]byte(nil), nonce...),
		Ciphertext: aead.Seal(nil, nonce, plaintext, []byte(associatedData)),
	}, nil
}

// Open decrypts a sealed value. It returns ErrUnsupportedVersion for a version
// it does not know and ErrCannotOpen when the key or the associated data is
// wrong or the value was changed.
func Open(key []byte, sealed *Sealed, associatedData string) ([]byte, error) {
	if err := sealed.check(); err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, sealed.Nonce, sealed.Ciphertext, []byte(associatedData))
	if err != nil {
		return nil, ErrCannotOpen
	}
	return plaintext, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyLength {
		return nil, fmt.Errorf("passwords: a key is %d bytes, not %d", KeyLength, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("passwords: make cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("passwords: make GCM: %w", err)
	}
	return aead, nil
}

// The associated data of each kind of sealed value. A value moved to another
// item, kind or recipient no longer opens.

// AccountKeyData is the associated data of the account key wrapped by the
// passphrase key or by the recovery key.
func AccountKeyData() string {
	return "account-key"
}

// PrivateKeyData is the associated data of the person's private key sealed
// with the account key.
func PrivateKeyData() string {
	return "private-key"
}

// ItemKeyData is the associated data of an item key sealed with the account
// key.
func ItemKeyData(itemId string) string {
	return "item-key:" + itemId
}

// ItemFieldsData is the associated data of an item's fields sealed with the
// item key.
func ItemFieldsData(itemId string, itemKind ItemKind) string {
	return "item:" + itemId + ":" + string(itemKind)
}

// ShareData is the associated data of an item key sealed for a recipient's key
// pair. The owner id is the user id of the person who owns the item and
// sealed the key. The recipient id is the recipient's user id, or
// AgentRecipientID.
func ShareData(itemId, ownerId, recipientId string) string {
	return "share:" + itemId + ":" + ownerId + ":" + recipientId
}

// CheckPublicKey refuses bytes that are not a public key: 65 bytes, starting
// with 04, that are a point on the P-256 curve.
func CheckPublicKey(publicKey []byte) error {
	if len(publicKey) != PublicKeyLength || publicKey[0] != 0x04 {
		return ErrInvalidPublicKey
	}
	if _, err := ecdh.P256().NewPublicKey(publicKey); err != nil {
		return ErrInvalidPublicKey
	}
	return nil
}

// CheckIterationCount refuses an iteration count outside MinimumIterationCount
// to MaximumIterationCount. The server checks every count it is given with
// it, and so does every client before it derives a key.
func CheckIterationCount(iterationCount int) error {
	if iterationCount < MinimumIterationCount {
		return ErrIterationCountTooLow
	}
	if iterationCount > MaximumIterationCount {
		return ErrIterationCountTooHigh
	}
	return nil
}

// DerivePassphraseKey is PBKDF2-HMAC-SHA256 over the passphrase in Unicode
// NFC (so the same passphrase typed on any device gives the same key), with
// the stored salt and iteration count, giving 32 bytes.
func DerivePassphraseKey(passphrase string, salt []byte, iterationCount int) ([]byte, error) {
	if err := CheckIterationCount(iterationCount); err != nil {
		return nil, err
	}
	key, err := pbkdf2.Key(sha256.New, norm.NFC.String(passphrase), salt, iterationCount, KeyLength)
	if err != nil {
		return nil, fmt.Errorf("passwords: derive passphrase key: %w", err)
	}
	return key, nil
}

// WrapKey is the key that wraps the account key, made from a 32 byte secret:
// the passphrase key, or the recovery key's bytes.
func WrapKey(secret []byte) ([]byte, error) {
	return deriveFromSecret(secret, wrapKeyLabel)
}

// ProofKey is what a device sends to show it knows the secret, made from the
// same 32 bytes as WrapKey but unrelated to it, so the server learns nothing
// about the wrap key. The server stores ProofHash of it.
func ProofKey(secret []byte) ([]byte, error) {
	return deriveFromSecret(secret, proofKeyLabel)
}

func deriveFromSecret(secret []byte, label string) ([]byte, error) {
	if len(secret) != KeyLength {
		return nil, fmt.Errorf("passwords: a secret is %d bytes, not %d", KeyLength, len(secret))
	}
	key, err := hkdf.Key(sha256.New, secret, nil, label, KeyLength)
	if err != nil {
		return nil, fmt.Errorf("passwords: derive key: %w", err)
	}
	return key, nil
}

// ProofHash is the SHA-256 of a proof key, which is what the server stores.
func ProofHash(proofKey []byte) []byte {
	hash := sha256.Sum256(proofKey)
	return hash[:]
}

// IsProofValid reports whether a proof key sent by a device matches the stored
// hash, comparing in constant time.
func IsProofValid(proofKey, storedProofHash []byte) bool {
	return subtle.ConstantTimeCompare(ProofHash(proofKey), storedProofHash) == 1
}

// NewRecoveryKey makes the 32 random bytes of a recovery key. FormatRecoveryKey
// writes them for the person to keep.
func NewRecoveryKey() ([]byte, error) {
	return NewKey()
}

// FormatRecoveryKey writes a recovery key as RFC 4648 base32 without padding,
// 13 groups of 4 letters joined by single spaces.
func FormatRecoveryKey(recoveryKey []byte) (string, error) {
	if len(recoveryKey) != KeyLength {
		return "", fmt.Errorf("passwords: a recovery key is %d bytes, not %d", KeyLength, len(recoveryKey))
	}
	letters := recoveryKeyEncoding.EncodeToString(recoveryKey)
	groups := make([]string, 0, len(letters)/4)
	for start := 0; start < len(letters); start += 4 {
		groups = append(groups, letters[start:start+4])
	}
	return strings.Join(groups, " "), nil
}

// ParseRecoveryKey reads a recovery key as the person typed it, ignoring
// spaces, tabs, line breaks, hyphens and the case of the letters A to Z. Any
// other character is refused, so a letter that some languages turn into
// another (a sharp s into SS, a long s into S) is not quietly accepted, and so
// is a padding sign and a last letter whose unused bits are not zero. Every
// implementation refuses the same texts (see recoveryKeyInvalid in the test
// vectors).
func ParseRecoveryKey(text string) ([]byte, error) {
	letters := make([]byte, 0, len(text))
	for index := 0; index < len(text); index++ {
		character := text[index]
		switch {
		case character == ' ' || character == '\t' || character == '\n' || character == '\r' || character == '-':
		case character >= 'a' && character <= 'z':
			letters = append(letters, character-'a'+'A')
		case character >= 'A' && character <= 'Z', character >= '2' && character <= '7':
			letters = append(letters, character)
		default:
			return nil, ErrInvalidRecoveryKey
		}
	}
	recoveryKey, err := recoveryKeyEncoding.DecodeString(string(letters))
	// Writing the bytes back refuses a last letter whose unused bits are not
	// zero, which a typo in that letter often is.
	if err != nil || len(recoveryKey) != KeyLength || recoveryKeyEncoding.EncodeToString(recoveryKey) != string(letters) {
		return nil, ErrInvalidRecoveryKey
	}
	return recoveryKey, nil
}

// KeyPair is a P-256 key pair for ECDH: a person's, or the server's agent key.
type KeyPair struct {
	privateKey *ecdh.PrivateKey
}

// GenerateKeyPair makes a new key pair from the system's randomness.
func GenerateKeyPair() (*KeyPair, error) {
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("passwords: generate key pair: %w", err)
	}
	return &KeyPair{privateKey: privateKey}, nil
}

// KeyPairFromPrivateKey loads a key pair from its 32 byte private scalar.
func KeyPairFromPrivateKey(privateKey []byte) (*KeyPair, error) {
	parsed, err := ecdh.P256().NewPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("passwords: read private key: %w", err)
	}
	return &KeyPair{privateKey: parsed}, nil
}

// PrivateKey is the 32 byte private scalar, which is what is sealed with the
// account key.
func (self *KeyPair) PrivateKey() []byte {
	return self.privateKey.Bytes()
}

// PublicKey is the 65 byte uncompressed point: 04, then X, then Y.
func (self *KeyPair) PublicKey() []byte {
	return self.privateKey.PublicKey().Bytes()
}

// SafetyCode is the short code of this key pair's public key (see SafetyCode).
func (self *KeyPair) SafetyCode() string {
	return SafetyCode(self.PublicKey())
}

// safetyCodeLabel starts the bytes a safety code is made from, so the hash is
// used for nothing else and a change of the recipe changes every code.
const safetyCodeLabel = "teanode passwords safety code v2"

// safetyCodeGroupCount is how many groups of four digits a safety code has.
const safetyCodeGroupCount = 8

// SafetyCode is eight groups of four digits made from a public key, which two
// people can read out to each other to be sure a share goes to the key they
// think it does. It is the SHA-256 of the label, a zero byte and the 65 public
// key bytes. The first 16 bytes of the hash are read as eight big-endian 16 bit
// numbers, each taken modulo 10000. That is about 106 bits. Nothing the server
// supplies goes into the hash, so a server cannot pick an identifier to make a
// search for a matching code easier: matching a fixed code means finding a key
// whose hash matches it, about 2^106 key generations.
func SafetyCode(publicKey []byte) string {
	input := make([]byte, 0, len(safetyCodeLabel)+len(publicKey)+1)
	input = append(input, safetyCodeLabel...)
	input = append(input, 0)
	input = append(input, publicKey...)
	hash := sha256.Sum256(input)
	groups := make([]string, 0, safetyCodeGroupCount)
	for index := 0; index < safetyCodeGroupCount; index++ {
		number := binary.BigEndian.Uint16(hash[index*2:])
		groups = append(groups, fmt.Sprintf("%04d", number%10000))
	}
	return strings.Join(groups, " ")
}

// NormalizeSafetyCode reads a safety code a person typed or pasted: 32 digits,
// with spaces or other white space anywhere or nowhere. It returns the code in
// the form SafetyCode writes, eight groups of four digits, so two codes are
// compared as text. Anything else is an error.
func NormalizeSafetyCode(text string) (string, error) {
	digits := make([]byte, 0, safetyCodeGroupCount*4)
	for _, character := range text {
		switch {
		case unicode.IsSpace(character):
			continue
		case character >= '0' && character <= '9':
			digits = append(digits, byte(character))
		default:
			return "", errors.New("passwords: a safety code is 32 digits")
		}
	}
	if len(digits) != safetyCodeGroupCount*4 {
		return "", errors.New("passwords: a safety code is 32 digits")
	}
	groups := make([]string, 0, safetyCodeGroupCount)
	for index := 0; index < len(digits); index += 4 {
		groups = append(groups, string(digits[index:index+4]))
	}
	return strings.Join(groups, " "), nil
}

// SealedForKeyPair is a value sealed so that only the holder of a recipient's
// private key opens it, and only when it came from the owner: ECDH of the
// recipient's public key with a fresh ephemeral key and with the owner's key,
// HKDF, then AES-256-GCM.
type SealedForKeyPair struct {
	Version            int    `json:"version"`
	EphemeralPublicKey []byte `json:"ephemeralPublicKey"`
	Nonce              []byte `json:"nonce"`
	Ciphertext         []byte `json:"ciphertext"`
}

// ParseSealedForKeyPair reads the JSON text of a value sealed for a key pair
// and checks its shape as ParseSealed does, and that the ephemeral public key
// is 65 bytes starting with 04.
func ParseSealedForKeyPair(text string) (*SealedForKeyPair, error) {
	parsed, err := parseSealedText(text, true)
	if err != nil {
		return nil, err
	}
	sealed := &SealedForKeyPair{Version: parsed.Version}
	if sealed.EphemeralPublicKey, err = DecodeCanonicalBase64(parsed.EphemeralPublicKey); err != nil {
		return nil, err
	}
	if sealed.Nonce, err = DecodeCanonicalBase64(parsed.Nonce); err != nil {
		return nil, err
	}
	if sealed.Ciphertext, err = DecodeCanonicalBase64(parsed.Ciphertext); err != nil {
		return nil, err
	}
	if err := sealed.check(); err != nil {
		return nil, err
	}
	return sealed, nil
}

// String is the JSON text to store.
func (self *SealedForKeyPair) String() string {
	text, err := json.Marshal(self)
	if err != nil {
		panic(err)
	}
	return string(text)
}

func (self *SealedForKeyPair) check() error {
	if self.Version != SealedVersion {
		return ErrUnsupportedVersion
	}
	if len(self.Nonce) != NonceLength || len(self.Ciphertext) < 16 {
		return ErrCannotOpen
	}
	if len(self.EphemeralPublicKey) != PublicKeyLength || self.EphemeralPublicKey[0] != 0x04 {
		return ErrCannotOpen
	}
	return nil
}

// SealForKeyPair seals plaintext (an item key) for the holder of
// recipientPublicKey, sent by the owner of the item, with a fresh ephemeral key
// pair and nonce. The key mixes two secrets: one from the ephemeral key and one
// from the owner's own private key, so only a holder of the owner's private key
// can make a share that opens (the server cannot plant an item "shared by"
// somebody). The associated data is ShareData.
func SealForKeyPair(owner *KeyPair, recipientPublicKey, plaintext []byte, associatedData string) (*SealedForKeyPair, error) {
	ephemeral, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(NonceLength)
	if err != nil {
		return nil, err
	}
	return sealForKeyPairWithRandomness(owner, recipientPublicKey, ephemeral, nonce, plaintext, associatedData)
}

// sealForKeyPairWithRandomness is SealForKeyPair with the ephemeral key pair
// and nonce given, so the test vectors are fixed.
func sealForKeyPairWithRandomness(owner *KeyPair, recipientPublicKey []byte, ephemeral *KeyPair, nonce, plaintext []byte, associatedData string) (*SealedForKeyPair, error) {
	_, _, key, err := deriveShareKeyForSender(owner, ephemeral, recipientPublicKey)
	if err != nil {
		return nil, err
	}
	sealed, err := sealWithNonce(key, nonce, plaintext, associatedData)
	if err != nil {
		return nil, err
	}
	return &SealedForKeyPair{
		Version:            SealedVersion,
		EphemeralPublicKey: ephemeral.PublicKey(),
		Nonce:              sealed.Nonce,
		Ciphertext:         sealed.Ciphertext,
	}, nil
}

// Open decrypts a value sealed for this key pair by the owner whose public key
// is ownerPublicKey. A value sealed by anybody else (the server, say) does not
// open. It returns ErrUnsupportedVersion or ErrCannotOpen as Open does.
func (self *KeyPair) Open(sealed *SealedForKeyPair, ownerPublicKey []byte, associatedData string) ([]byte, error) {
	if err := sealed.check(); err != nil {
		return nil, err
	}
	if CheckPublicKey(ownerPublicKey) != nil {
		return nil, ErrCannotOpen
	}
	ephemeralShared, err := agreeOnSecret(self.privateKey, sealed.EphemeralPublicKey)
	if err != nil {
		return nil, ErrCannotOpen
	}
	ownerShared, err := agreeOnSecret(self.privateKey, ownerPublicKey)
	if err != nil {
		return nil, ErrCannotOpen
	}
	key, err := deriveShareKey(ephemeralShared, ownerShared, self.PublicKey())
	if err != nil {
		return nil, ErrCannotOpen
	}
	return Open(key, &Sealed{Version: sealed.Version, Nonce: sealed.Nonce, Ciphertext: sealed.Ciphertext}, associatedData)
}

// agreeOnSecret is ECDH between a private key and a peer's public key, which
// gives the 32 byte X coordinate (the shared secret).
func agreeOnSecret(privateKey *ecdh.PrivateKey, peerPublicKey []byte) ([]byte, error) {
	peer, err := ecdh.P256().NewPublicKey(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("passwords: read public key: %w", err)
	}
	sharedSecret, err := privateKey.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("passwords: agree on a shared secret: %w", err)
	}
	return sharedSecret, nil
}

// deriveShareKeyForSender gives the two shared secrets and the key the owner
// seals with: the ephemeral private key and the owner's private key, each
// agreed with the recipient's public key.
func deriveShareKeyForSender(owner, ephemeral *KeyPair, recipientPublicKey []byte) (ephemeralShared, ownerShared, key []byte, err error) {
	if ephemeralShared, err = agreeOnSecret(ephemeral.privateKey, recipientPublicKey); err != nil {
		return nil, nil, nil, err
	}
	if ownerShared, err = agreeOnSecret(owner.privateKey, recipientPublicKey); err != nil {
		return nil, nil, nil, err
	}
	if key, err = deriveShareKey(ephemeralShared, ownerShared, recipientPublicKey); err != nil {
		return nil, nil, nil, err
	}
	return ephemeralShared, ownerShared, key, nil
}

// deriveShareKey is HKDF-SHA256 of the ephemeral secret followed by the owner
// secret, with the recipient's public key as the salt. Both ends pass the
// recipient's public key: the sender knows it, and the recipient has it.
func deriveShareKey(ephemeralShared, ownerShared, recipientPublicKey []byte) ([]byte, error) {
	secrets := append(append([]byte(nil), ephemeralShared...), ownerShared...)
	key, err := hkdf.Key(sha256.New, secrets, recipientPublicKey, shareKeyLabel, KeyLength)
	if err != nil {
		return nil, fmt.Errorf("passwords: derive share key: %w", err)
	}
	return key, nil
}
