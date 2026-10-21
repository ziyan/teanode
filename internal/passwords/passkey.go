package passwords

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
)

// A passkey made by TeaNode is a synced one: Apple returns no device-bound
// keys from a provider, and the extension keeps them in the login, so the
// backup-eligible and backup-state flags are set. Only ES256 (COSE algorithm
// -7) is supported.

// The flags of the authenticator data (WebAuthn section 6.1).
const (
	flagUserPresent        byte = 0x01
	flagUserVerified       byte = 0x04
	flagBackupEligible     byte = 0x08
	flagBackupState        byte = 0x10
	flagAttestedCredential byte = 0x40

	// CredentialIDLength is the length of a credential id TeaNode makes.
	CredentialIDLength = 16
	// maximumCredentialIDLength is WebAuthn's limit on a credential id.
	maximumCredentialIDLength = 1023
	// coseAlgorithmES256 is ECDSA with P-256 and SHA-256.
	coseAlgorithmES256 = -7
)

// AAGUID is TeaNode's authenticator id: the ASCII of "TEANODEPASSWORDS", which
// reads 5445414e-4f44-4550-4153-53574f524453.
var AAGUID = [16]byte{
	0x54, 0x45, 0x41, 0x4e, 0x4f, 0x44, 0x45, 0x50,
	0x41, 0x53, 0x53, 0x57, 0x4f, 0x52, 0x44, 0x53,
}

// NewPasskey makes the credential id and the key of a new passkey. The private
// key is PKCS#8 DER, which is how it is kept in a login.
func NewPasskey() (credentialID, privateKey []byte, err error) {
	credentialID, err = randomBytes(CredentialIDLength)
	if err != nil {
		return nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("passwords: generate passkey: %w", err)
	}
	privateKey, err = x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("passwords: encode passkey: %w", err)
	}
	return credentialID, privateKey, nil
}

// passkeyFromScalar is a passkey private key from its 32 byte scalar, so the
// test vectors are fixed.
func passkeyFromScalar(scalar []byte) ([]byte, error) {
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return nil, fmt.Errorf("passwords: read passkey scalar: %w", err)
	}
	return x509.MarshalPKCS8PrivateKey(key)
}

func parsePasskeyPrivateKey(privateKey []byte) (*ecdsa.PrivateKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("passwords: read passkey: %w", err)
	}
	key, isECDSA := parsed.(*ecdsa.PrivateKey)
	if !isECDSA || key.Curve != elliptic.P256() {
		return nil, errors.New("passwords: a passkey is an ES256 (P-256) key")
	}
	return key, nil
}

// PasskeyPublicKey is the X and Y coordinates, 32 bytes each, of a passkey's
// public key.
func PasskeyPublicKey(privateKey []byte) (xCoordinate, yCoordinate []byte, err error) {
	key, err := parsePasskeyPrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("passwords: encode passkey public key: %w", err)
	}
	return point[1:33], point[33:65], nil
}

// COSEPublicKey is the CBOR (COSE_Key) form of a passkey's public key, the map
// {1: 2, 3: -7, -1: 1, -2: X, -3: Y} with the keys in that order.
func COSEPublicKey(privateKey []byte) ([]byte, error) {
	xCoordinate, yCoordinate, err := PasskeyPublicKey(privateKey)
	if err != nil {
		return nil, err
	}
	var encoded []byte
	encoded = appendCBORHead(encoded, cborMap, 5)
	encoded = appendCBORInteger(encoded, 1) // key type
	encoded = appendCBORInteger(encoded, 2) // EC2
	encoded = appendCBORInteger(encoded, 3) // algorithm
	encoded = appendCBORInteger(encoded, coseAlgorithmES256)
	encoded = appendCBORInteger(encoded, -1) // curve
	encoded = appendCBORInteger(encoded, 1)  // P-256
	encoded = appendCBORInteger(encoded, -2) // X
	encoded = appendCBORBytes(encoded, xCoordinate)
	encoded = appendCBORInteger(encoded, -3) // Y
	encoded = appendCBORBytes(encoded, yCoordinate)
	return encoded, nil
}

// RegistrationAuthenticatorData is the authenticator data of a new passkey: the
// SHA-256 of the relying party id, the flags, a sign count of 0, TeaNode's
// AAGUID, the credential id and the COSE public key. The flags are 0x5D (user
// present, user verified, backup eligible, backup state, attested credential
// data) when the person was verified, 0x59 when not.
func RegistrationAuthenticatorData(relyingPartyID string, credentialID, cosePublicKey []byte, isVerified bool) ([]byte, error) {
	if len(credentialID) == 0 || len(credentialID) > maximumCredentialIDLength {
		return nil, errors.New("passwords: a credential id is 1 to 1023 bytes")
	}
	flags := flagUserPresent | flagBackupEligible | flagBackupState | flagAttestedCredential
	if isVerified {
		flags |= flagUserVerified
	}
	relyingPartyHash := sha256.Sum256([]byte(relyingPartyID))
	authenticatorData := append([]byte(nil), relyingPartyHash[:]...)
	authenticatorData = append(authenticatorData, flags, 0, 0, 0, 0)
	authenticatorData = append(authenticatorData, AAGUID[:]...)
	authenticatorData = append(authenticatorData, byte(len(credentialID)>>8), byte(len(credentialID)))
	authenticatorData = append(authenticatorData, credentialID...)
	authenticatorData = append(authenticatorData, cosePublicKey...)
	return authenticatorData, nil
}

// AttestationObject is the CBOR map {"fmt": "none", "attStmt": {}, "authData":
// authenticatorData} in that key order, which is what a registration returns
// to the page. TeaNode does not attest.
func AttestationObject(authenticatorData []byte) []byte {
	var encoded []byte
	encoded = appendCBORHead(encoded, cborMap, 3)
	encoded = appendCBORText(encoded, "fmt")
	encoded = appendCBORText(encoded, "none")
	encoded = appendCBORText(encoded, "attStmt")
	encoded = appendCBORHead(encoded, cborMap, 0)
	encoded = appendCBORText(encoded, "authData")
	encoded = appendCBORBytes(encoded, authenticatorData)
	return encoded
}

// AssertionAuthenticatorData is the authenticator data of a sign-in: the
// SHA-256 of the relying party id, the flags and a sign count of 0, 37 bytes.
// The flags are 0x1D (user present, user verified, backup eligible, backup
// state) when the person was verified, 0x19 when not.
func AssertionAuthenticatorData(relyingPartyID string, isVerified bool) []byte {
	flags := flagUserPresent | flagBackupEligible | flagBackupState
	if isVerified {
		flags |= flagUserVerified
	}
	relyingPartyHash := sha256.Sum256([]byte(relyingPartyID))
	authenticatorData := append([]byte(nil), relyingPartyHash[:]...)
	return append(authenticatorData, flags, 0, 0, 0, 0)
}

// SignAssertion is the ECDSA P-256 SHA-256 signature, DER encoded, over the
// authenticator data followed by the client data hash. ECDSA signatures are
// randomized, so two calls give two different signatures that both verify.
func SignAssertion(privateKey, authenticatorData, clientDataHash []byte) ([]byte, error) {
	key, err := parsePasskeyPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	signedBytes := append(append([]byte(nil), authenticatorData...), clientDataHash...)
	digest := sha256.Sum256(signedBytes)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("passwords: sign assertion: %w", err)
	}
	return signature, nil
}

// The few pieces of CBOR (RFC 8949) a passkey needs, written by hand: maps,
// byte and text strings, and integers.
const (
	cborUnsigned   byte = 0
	cborNegative   byte = 1
	cborByteString byte = 2
	cborTextString byte = 3
	cborMap        byte = 5
)

// appendCBORHead writes the first byte or bytes of an item: the major type and
// the shortest form of the argument.
func appendCBORHead(encoded []byte, majorType byte, argument uint64) []byte {
	prefix := majorType << 5
	switch {
	case argument < 24:
		return append(encoded, prefix|byte(argument))
	case argument < 1<<8:
		return append(encoded, prefix|24, byte(argument))
	case argument < 1<<16:
		return append(encoded, prefix|25, byte(argument>>8), byte(argument))
	default:
		return append(encoded, prefix|26, byte(argument>>24), byte(argument>>16), byte(argument>>8), byte(argument))
	}
}

func appendCBORInteger(encoded []byte, number int64) []byte {
	if number >= 0 {
		return appendCBORHead(encoded, cborUnsigned, uint64(number))
	}
	return appendCBORHead(encoded, cborNegative, uint64(-1-number))
}

func appendCBORBytes(encoded, content []byte) []byte {
	return append(appendCBORHead(encoded, cborByteString, uint64(len(content))), content...)
}

func appendCBORText(encoded []byte, text string) []byte {
	return append(appendCBORHead(encoded, cborTextString, uint64(len(text))), text...)
}
